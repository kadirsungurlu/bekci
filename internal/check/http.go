package check

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// UserAgent kontrol isteklerinde varsayılan User-Agent.
const UserAgent = "Mozilla/5.0 (compatible; Uptime-Kadir/1.0; +https://uptime.kadir.app)"

var errTooManyRedirects = errors.New("çok fazla yönlendirme")

// maxBody keyword/JSON kontrolü için okunacak en fazla gövde boyutu.
const maxBody = 10 << 20

type HTTPConfig struct {
	URL           string   `json:"url"`
	Method        string   `json:"method"`
	Headers       string   `json:"headers"` // her satır "Ad: değer"
	Body          string   `json:"body"`
	BasicUser     string   `json:"basic_user"`
	BasicPass     string   `json:"basic_pass"`
	AcceptedCodes []string `json:"accepted_codes"`
	MaxRedirects  *int     `json:"max_redirects"`
	IgnoreTLS     bool     `json:"ignore_tls"`
	CertExpiry    *bool    `json:"cert_expiry"` // SSL bitiş uyarısı

	Keyword       string `json:"keyword"`
	KeywordInvert bool   `json:"keyword_invert"` // kelime VARSA hata
	KeywordCase   bool   `json:"keyword_case"`   // büyük/küçük harf duyarlı

	JSONPath     string `json:"json_path"`
	JSONOp       string `json:"json_op"`
	JSONExpected string `json:"json_expected"`
}

var httpMethods = map[string]bool{"GET": true, "HEAD": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "OPTIONS": true}

var jsonOps = map[string]bool{"exists": true, "==": true, "!=": true, "contains": true, ">": true, ">=": true, "<": true, "<=": true}

type httpChecker struct{}

func init() { Register("http", httpChecker{}) }

func (httpChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c HTTPConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimSpace(c.URL)
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, invalid("Geçerli bir http:// veya https:// adresi girin")
	}
	// GET varsayılan: HEAD'i desteklemeyen uygulamalar (405) yanlış alarm üretmesin.
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	if c.Method == "" {
		c.Method = "GET"
	}
	if !httpMethods[c.Method] {
		return nil, invalid("Desteklenmeyen HTTP metodu: %s", c.Method)
	}
	if _, err := parseHeaders(c.Headers); err != nil {
		return nil, err
	}
	if len(c.AcceptedCodes) == 0 {
		c.AcceptedCodes = []string{"200-399"}
	}
	for i, code := range c.AcceptedCodes {
		c.AcceptedCodes[i] = strings.TrimSpace(code)
		if _, _, err := parseCodeRange(c.AcceptedCodes[i]); err != nil {
			return nil, err
		}
	}
	if c.MaxRedirects == nil {
		n := 10
		c.MaxRedirects = &n
	}
	if *c.MaxRedirects < 0 || *c.MaxRedirects > 30 {
		return nil, invalid("Yönlendirme sayısı 0-30 arasında olmalı")
	}
	if c.CertExpiry == nil {
		t := true
		c.CertExpiry = &t
	}
	c.JSONPath = strings.TrimSpace(c.JSONPath)
	if c.JSONPath != "" {
		if c.JSONOp == "" {
			c.JSONOp = "=="
		}
		if !jsonOps[c.JSONOp] {
			return nil, invalid("Geçersiz JSON karşılaştırması: %s", c.JSONOp)
		}
	} else {
		c.JSONOp, c.JSONExpected = "", ""
	}
	if c.Method == "HEAD" && (c.Keyword != "" || c.JSONPath != "") {
		return nil, invalid("HEAD isteği gövde döndürmez; kelime/JSON kontrolü için GET kullanın")
	}
	return encode(c), nil
}

func (httpChecker) Target(raw json.RawMessage) string {
	var c HTTPConfig
	json.Unmarshal(raw, &c)
	return c.URL
}

// HTTPConfigOf kayıtlı ayarı çözer (motorun SSL uyarısı kararı için).
func HTTPConfigOf(raw json.RawMessage) HTTPConfig {
	var c HTTPConfig
	json.Unmarshal(raw, &c)
	return c
}

// CertExpiryEnabled CertExpiryChecker arayüzünü uygular.
func (httpChecker) CertExpiryEnabled(raw json.RawMessage) bool {
	c := HTTPConfigOf(raw)
	return c.CertExpiry == nil || *c.CertExpiry
}

func (httpChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c HTTPConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	maxRedirects := 10
	if c.MaxRedirects != nil {
		maxRedirects = *c.MaxRedirects
	}

	var body io.Reader
	if c.Body != "" {
		body = strings.NewReader(c.Body)
	}
	req, err := http.NewRequestWithContext(ctx, c.Method, c.URL, body)
	if err != nil {
		return down("İstek oluşturulamadı: " + err.Error())
	}
	headers, _ := parseHeaders(c.Headers)
	for _, h := range headers {
		if strings.EqualFold(h[0], "Host") {
			req.Host = h[1]
			continue
		}
		req.Header.Add(h[0], h[1])
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", UserAgent)
	}
	if c.Body != "" && req.Header.Get("Content-Type") == "" {
		if json.Valid([]byte(c.Body)) {
			req.Header.Set("Content-Type", "application/json")
		} else {
			req.Header.Set("Content-Type", "text/plain; charset=utf-8")
		}
	}
	if c.BasicUser != "" || c.BasicPass != "" {
		req.SetBasicAuth(c.BasicUser, c.BasicPass)
	}

	// Her kontrol yeni bağlantıyla yapılır: ölçülen süre gerçek bir ziyaretçininkine
	// benzer ve kopan bağlantılar bir sonraki kontrolü etkilemez.
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{KeepAlive: -1}).DialContext,
		TLSClientConfig:        &tls.Config{InsecureSkipVerify: c.IgnoreTLS},
		DisableKeepAlives:      true,
		ForceAttemptHTTP2:      true,
		MaxResponseHeaderBytes: 1 << 20,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if maxRedirects == 0 {
				return http.ErrUseLastResponse // yönlendirme takip edilmez, 3xx'in kendisi değerlendirilir
			}
			if len(via) > maxRedirects {
				return errTooManyRedirects
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Do(req)
	if errors.Is(err, errTooManyRedirects) {
		return down(fmt.Sprintf("Çok fazla yönlendirme (en fazla %d)", maxRedirects))
	}
	if err != nil {
		return down(describeErr(ctx, err))
	}
	defer resp.Body.Close()
	ping := msSince(start)

	res := Result{PingMs: ping, Message: statusLine(resp.StatusCode)}
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		leaf := resp.TLS.PeerCertificates[0]
		res.Cert = &CertInfo{NotAfter: leaf.NotAfter, Issuer: issuerName(leaf.Issuer.Organization, leaf.Issuer.CommonName), Subject: leaf.Subject.CommonName}
	}

	if !codeAccepted(resp.StatusCode, c.AcceptedCodes) {
		res.Message = "HTTP " + statusLine(resp.StatusCode)
		return res
	}

	if c.Keyword == "" && c.JSONPath == "" {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		res.Up = true
		return res
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		res.Message = "Yanıt gövdesi okunamadı: " + describeErr(ctx, err)
		return res
	}

	if c.Keyword != "" {
		found := containsKeyword(data, c.Keyword, c.KeywordCase)
		if found == c.KeywordInvert {
			if found {
				res.Message = fmt.Sprintf("Kelime bulundu (olmaması gerekiyordu): %q", c.Keyword)
			} else {
				res.Message = fmt.Sprintf("Kelime bulunamadı: %q", c.Keyword)
			}
			return res
		}
	}

	if c.JSONPath != "" {
		ok, msg := evalJSON(data, c.JSONPath, c.JSONOp, c.JSONExpected)
		if !ok {
			res.Message = msg
			return res
		}
	}

	res.Up = true
	return res
}

func statusLine(code int) string {
	if t := http.StatusText(code); t != "" {
		return fmt.Sprintf("%d %s", code, t)
	}
	return strconv.Itoa(code)
}

func issuerName(org []string, cn string) string {
	if len(org) > 0 && org[0] != "" {
		return org[0]
	}
	return cn
}

// parseHeaders "Ad: değer" satırlarını çözer; boş satırlar atlanır.
func parseHeaders(s string) ([][2]string, error) {
	var out [][2]string
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return nil, invalid("Başlık satırı %d geçersiz; biçim \"Ad: değer\" olmalı", i+1)
		}
		out = append(out, [2]string{name, strings.TrimSpace(value)})
	}
	return out, nil
}

// parseCodeRange "200", "200-299" veya "2xx" biçimini [min, max] aralığına çevirir.
func parseCodeRange(s string) (int, int, error) {
	bad := invalid("Geçersiz durum kodu: %q (örnek: 200, 200-299, 2xx)", s)
	if len(s) == 3 && strings.HasSuffix(strings.ToLower(s), "xx") {
		d, err := strconv.Atoi(s[:1])
		if err != nil || d < 1 || d > 5 {
			return 0, 0, bad
		}
		return d * 100, d*100 + 99, nil
	}
	lo, hi, isRange := strings.Cut(s, "-")
	a, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil || a < 100 || a > 599 {
		return 0, 0, bad
	}
	if !isRange {
		return a, a, nil
	}
	b, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil || b < a || b > 599 {
		return 0, 0, bad
	}
	return a, b, nil
}

func codeAccepted(code int, accepted []string) bool {
	if len(accepted) == 0 {
		return code >= 200 && code < 400
	}
	for _, s := range accepted {
		if lo, hi, err := parseCodeRange(s); err == nil && code >= lo && code <= hi {
			return true
		}
	}
	return false
}

func containsKeyword(body []byte, kw string, caseSensitive bool) bool {
	if caseSensitive {
		return bytes.Contains(body, []byte(kw))
	}
	return strings.Contains(strings.ToLower(string(body)), strings.ToLower(kw))
}

// evalJSON gjson yolunu (ör. "data.status", "items.0.name") değerlendirir.
func evalJSON(data []byte, path, op, expected string) (bool, string) {
	if !gjson.ValidBytes(data) {
		return false, "Yanıt geçerli bir JSON değil"
	}
	v := gjson.GetBytes(data, path)
	if op == "exists" {
		if v.Exists() {
			return true, ""
		}
		return false, fmt.Sprintf("JSON alanı yok: %s", path)
	}
	if !v.Exists() {
		return false, fmt.Sprintf("JSON alanı yok: %s", path)
	}
	got := v.String()
	fail := fmt.Sprintf("JSON: %s = %s (beklenen: %s %s)", path, truncate(got, 80), op, expected)

	gotNum, errA := strconv.ParseFloat(strings.TrimSpace(got), 64)
	expNum, errB := strconv.ParseFloat(strings.TrimSpace(expected), 64)
	numeric := errA == nil && errB == nil

	var ok bool
	switch op {
	case "==":
		ok = got == expected || (numeric && gotNum == expNum)
	case "!=":
		ok = got != expected && !(numeric && gotNum == expNum)
	case "contains":
		ok = strings.Contains(got, expected)
	case ">", ">=", "<", "<=":
		if !numeric {
			return false, fmt.Sprintf("JSON: %s sayısal değil (%s)", path, truncate(got, 80))
		}
		switch op {
		case ">":
			ok = gotNum > expNum
		case ">=":
			ok = gotNum >= expNum
		case "<":
			ok = gotNum < expNum
		case "<=":
			ok = gotNum <= expNum
		}
	}
	if !ok {
		return false, fail
	}
	return true, ""
}
