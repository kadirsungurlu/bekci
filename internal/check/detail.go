package check

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Olay ayrıntıları (docs/PLAN.md §13) ---------------------------------------------------
//
// Başarısız bir HTTP kontrolünün isteği ve yanıtı olay sayfasında gösterilir.
// Detail yalnızca başarısız kontrolde doldurulur (başarılı kontrolde ek maliyet
// yok) ve gizli bilgiler yakalama anında, saklanmadan önce maskelenir.

// DetailBodyMax yakalanan yanıt gövdesinin en fazla boyutu.
const DetailBodyMax = 16 << 10

// Detail sınırları (Sanitize).
const (
	detailMaxHeaders  = 64
	detailMaxName     = 128
	detailMaxValue    = 2048
	detailMaxURL      = 2048
	detailMaxError    = 500
	detailMaxShort    = 100 // metot, protokol, durum metni, içerik türü
	detailMinSecret   = 4   // bundan kısa gizli değerler metin içinde aranmaz (yanlış eşleşme)
	detailMaskedValue = "••••••"
)

// Header tek bir HTTP başlığı (sıra korunur, aynı ad birden çok kez olabilir).
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Detail başarısız kontrolün isteği ve (alındıysa) yanıtı.
type Detail struct {
	Method         string   `json:"method"`
	URL            string   `json:"url"`
	RequestHeaders []Header `json:"request_headers,omitempty"`

	Status          int      `json:"status,omitempty"` // 0: yanıt alınamadı (bkz. Error)
	StatusText      string   `json:"status_text,omitempty"`
	Proto           string   `json:"proto,omitempty"`
	FinalURL        string   `json:"final_url,omitempty"` // yönlendirme sonrası adres (farklıysa)
	ResponseHeaders []Header `json:"response_headers,omitempty"`
	ContentType     string   `json:"content_type,omitempty"`
	Body            string   `json:"body,omitempty"`           // en fazla DetailBodyMax; ikili içerikte açıklama
	BodySize        int64    `json:"body_size,omitempty"`      // bilinen toplam boyut (bayt); 0: bilinmiyor
	BodyTruncated   bool     `json:"body_truncated,omitempty"` // gövde kırpıldı
	BodyBinary      bool     `json:"body_binary,omitempty"`    // ikili içerik: gövde gösterilmez
	Error           string   `json:"error,omitempty"`          // yanıt alınamadıysa neden
}

// Sanitize uzak kontrol noktasından gelen (güvenilmeyen) ayrıntıyı sınırlar:
// geçersiz UTF-8 ve kontrol karakterleri temizlenir, metinler kırpılır.
func (d *Detail) Sanitize() {
	d.Method = cleanLine(d.Method, 16)
	d.URL = cleanLine(d.URL, detailMaxURL)
	d.FinalURL = cleanLine(d.FinalURL, detailMaxURL)
	d.StatusText = cleanLine(d.StatusText, detailMaxShort)
	d.Proto = cleanLine(d.Proto, detailMaxShort)
	d.ContentType = cleanLine(d.ContentType, detailMaxShort*2)
	d.Error = cleanLine(d.Error, detailMaxError)
	if d.Status < 0 || d.Status > 999 {
		d.Status = 0
	}
	if d.BodySize < 0 {
		d.BodySize = 0
	}
	d.RequestHeaders = cleanHeaders(d.RequestHeaders)
	d.ResponseHeaders = cleanHeaders(d.ResponseHeaders)
	body := strings.ToValidUTF8(d.Body, "�")
	if len(body) > DetailBodyMax {
		body = strings.ToValidUTF8(body[:DetailBodyMax], "")
		d.BodyTruncated = true
	}
	d.Body = body
}

func cleanHeaders(hs []Header) []Header {
	if len(hs) > detailMaxHeaders {
		hs = hs[:detailMaxHeaders]
	}
	out := make([]Header, 0, len(hs))
	for _, h := range hs {
		name := cleanLine(h.Name, detailMaxName)
		if name == "" {
			continue
		}
		out = append(out, Header{Name: name, Value: cleanLine(h.Value, detailMaxValue)})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// cleanLine tek satırlık metni temizler ve n karaktere kısaltır.
func cleanLine(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}

// MaskDetail ayrıntıdaki gizli bilgileri monitörün ayarına göre maskeler.
// Kontrol noktasından gelen ayrıntıya sunucu tarafında da uygulanır.
func MaskDetail(typ string, cfg json.RawMessage, d *Detail) {
	if d == nil {
		return
	}
	if typ == "http" {
		maskHTTPDetail(d, HTTPConfigOf(cfg))
	}
}

// Her zaman maskelenen istek başlıkları (küçük harf).
var (
	secretRequestHeaders = map[string]bool{"authorization": true, "proxy-authorization": true, "cookie": true}
	// safeHeaders kullanıcının girdiği başlıklardan değeri gizli sayılmayanlar.
	safeHeaders = map[string]bool{
		"accept": true, "accept-encoding": true, "accept-language": true, "accept-charset": true,
		"cache-control": true, "pragma": true, "content-type": true, "content-length": true,
		"user-agent": true, "host": true, "origin": true, "referer": true, "connection": true,
		"x-requested-with": true, "dnt": true, "if-none-match": true, "if-modified-since": true,
	}
	// safeResponseHeaders yanıt başlıklarında değeri gösterilebilecek standart
	// başlıkların izin listesi (allowlist). Bu listede olmayan her başlığın
	// (ör. X-Internal-Token) yalnızca ADI gösterilir, DEĞERİ maskelenir; böylece
	// iç servislerin özel başlıkları olay yakalamasından sızmaz.
	safeResponseHeaders = map[string]bool{
		"content-type": true, "content-length": true, "content-encoding": true, "date": true,
		"server": true, "cache-control": true, "expires": true, "last-modified": true,
		"etag": true, "location": true, "retry-after": true, "content-language": true,
		"age": true, "vary": true, "connection": true, "transfer-encoding": true,
		"strict-transport-security": true, "x-content-type-options": true, "x-frame-options": true,
	}
)

// maskHTTPDetail isteğin ve yanıtın gizli bilgilerini maskeler; extra ek gizli
// değerlerdir (ör. o anda alınan OAuth2 erişim token'ı).
func maskHTTPDetail(d *Detail, c HTTPConfig, extra ...string) {
	configured := map[string]bool{}
	var secrets []string
	if hs, err := parseHeaders(c.Headers); err == nil {
		for _, h := range hs {
			name := strings.ToLower(h[0])
			if safeHeaders[name] {
				continue
			}
			configured[name] = true
			secrets = append(secrets, h[1])
			// "Bearer abc…" gibi şemalı değerlerde token tek başına da yankılanabilir.
			if f := strings.Fields(h[1]); len(f) > 1 {
				secrets = append(secrets, f[len(f)-1])
			}
		}
	}
	secrets = append(secrets, c.BasicPass, c.ProxyPass, c.OAuthClientSecret)
	if c.BasicUser != "" || c.BasicPass != "" {
		// Yankılayan API'ler Authorization başlığını gövdede döndürebilir.
		secrets = append(secrets, base64.StdEncoding.EncodeToString([]byte(c.BasicUser+":"+c.BasicPass)))
	}
	if u, err := url.Parse(c.URL); err == nil && u.User != nil {
		if p, ok := u.User.Password(); ok {
			secrets = append(secrets, p)
		}
	}
	secrets = append(secrets, extra...)
	secrets = usableSecrets(secrets)
	redact := func(s string) string {
		for _, sec := range secrets {
			s = strings.ReplaceAll(s, sec, detailMaskedValue)
		}
		return s
	}

	for i, h := range d.RequestHeaders {
		name := strings.ToLower(h.Name)
		if secretRequestHeaders[name] || configured[name] {
			d.RequestHeaders[i].Value = detailMaskedValue
		} else {
			d.RequestHeaders[i].Value = redact(h.Value)
		}
	}
	// Yanıt başlıklarında izin listesi (allowlist): yalnızca standart başlıkların
	// değeri gösterilir (gizli değerler ayrıca maskelenir); listede olmayan her
	// başlığın adı kalır ama değeri maskelenir.
	for i, h := range d.ResponseHeaders {
		if safeResponseHeaders[strings.ToLower(h.Name)] {
			d.ResponseHeaders[i].Value = redact(h.Value)
		} else {
			d.ResponseHeaders[i].Value = detailMaskedValue
		}
	}
	d.URL = redact(redactedURL(d.URL))
	d.FinalURL = redact(redactedURL(d.FinalURL))
	d.Body = redact(d.Body)
	d.Error = redact(d.Error)
}

// usableSecrets boş/kısa değerleri atar ve uzunları önce gelecek şekilde sıralar
// (biri diğerini içeriyorsa önce uzunu değiştirilsin).
func usableSecrets(in []string) []string {
	out := in[:0]
	seen := map[string]bool{}
	for _, s := range in {
		if len(s) < detailMinSecret || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}

// redactedURL adresteki kullanıcı şifresini gizler.
func redactedURL(s string) string {
	if s == "" {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	if _, ok := u.User.Password(); ok {
		return u.Redacted()
	}
	return s
}

// headerList başlıkları ada göre sıralı listeye çevirir.
func headerList(h http.Header) []Header {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []Header
	for _, k := range names {
		for _, v := range h[k] {
			out = append(out, Header{Name: k, Value: v})
		}
	}
	return out
}

// textMediaTypes metin olarak gösterilen application/* türleri.
var textMediaTypes = map[string]bool{
	"application/json": true, "application/xml": true, "application/javascript": true,
	"application/x-javascript": true, "application/ecmascript": true, "application/x-www-form-urlencoded": true,
	"application/yaml": true, "application/x-yaml": true, "application/csv": true, "application/graphql": true,
	"application/x-ndjson": true, "application/sql": true,
}

// isTextBody gövde metin olarak gösterilebilir mi? İçerik türü yoksa gövdeden koklanır.
func isTextBody(contentType string, data []byte) bool {
	if contentType != "" {
		mt, _, err := mime.ParseMediaType(contentType)
		if err == nil {
			return strings.HasPrefix(mt, "text/") || strings.HasSuffix(mt, "+json") ||
				strings.HasSuffix(mt, "+xml") || textMediaTypes[mt]
		}
	}
	if len(data) == 0 {
		return true
	}
	if !strings.HasPrefix(http.DetectContentType(data), "text/") {
		return false
	}
	// Sınırda bölünmüş son karakter geçersiz sayılmasın.
	for i := 0; i < utf8.UTFMax && len(data) > 0; i++ {
		if utf8.Valid(data) {
			return true
		}
		data = data[:len(data)-1]
	}
	return utf8.Valid(data)
}

// setBody yakalanan gövdeyi ayrıntıya yazar. data gövdenin başıdır (en fazla
// DetailBodyMax+1 bayt); total gövdenin okunan toplam boyutu (-1: bilinmiyor).
func (d *Detail) setBody(resp *http.Response, data []byte, total int64) {
	d.ContentType = resp.Header.Get("Content-Type")
	truncated := len(data) > DetailBodyMax
	switch {
	case total >= 0:
		d.BodySize = total
	case resp.ContentLength >= 0:
		d.BodySize = resp.ContentLength
	}
	if truncated {
		data = data[:DetailBodyMax]
	}
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	if (enc != "" && enc != "identity") || !isTextBody(d.ContentType, data) {
		d.BodyBinary = true
		switch {
		case d.BodySize > 0:
			d.Body = fmt.Sprintf("(ikili içerik, %d bayt)", d.BodySize)
		case truncated:
			d.Body = fmt.Sprintf("(ikili içerik, %d bayttan büyük)", DetailBodyMax)
		default:
			d.Body = fmt.Sprintf("(ikili içerik, %d bayt)", len(data))
		}
		return
	}
	d.Body = strings.ToValidUTF8(string(data), "�")
	if truncated {
		d.Body = strings.TrimSuffix(d.Body, "�") // bölünmüş son karakter
		d.BodyTruncated = true
	}
}
