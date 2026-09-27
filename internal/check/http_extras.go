package check

// HTTP monitörünün gelişmiş seçenekleri: proxy, istemci sertifikası (mTLS) ve
// OAuth2 istemci kimlik bilgileri (client credentials) akışı.

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxPEM sertifika/anahtar alanı başına en fazla boyut.
const maxPEM = 64 << 10

var proxySchemes = map[string]bool{"http": true, "https": true, "socks5": true, "socks5h": true}

// normalizeHTTPExtras proxy, mTLS ve OAuth2 alanlarını doğrular.
func normalizeHTTPExtras(c *HTTPConfig) error {
	// Proxy
	c.ProxyURL = strings.TrimSpace(c.ProxyURL)
	if c.ProxyURL == "" {
		c.ProxyUser, c.ProxyPass = "", ""
	} else {
		u, err := url.Parse(c.ProxyURL)
		if err != nil || !proxySchemes[strings.ToLower(u.Scheme)] || u.Hostname() == "" {
			return invalid("Proxy adresi http://, https://, socks5:// veya socks5h:// ile başlamalı (ör. socks5://10.0.0.5:1080)")
		}
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
			return invalid("Proxy adresi yalnızca sunucu ve port içermeli")
		}
		if u.User != nil {
			// Adresteki kullanıcı adı/şifre ayrı alanlara taşınır: şifre API'de maskelenir.
			c.ProxyUser = u.User.Username()
			if p, ok := u.User.Password(); ok {
				c.ProxyPass = p
			}
		}
		c.ProxyURL = strings.ToLower(u.Scheme) + "://" + u.Host
		c.ProxyUser = strings.TrimSpace(c.ProxyUser)
		if c.ProxyPass != "" && c.ProxyUser == "" {
			return invalid("Proxy şifresi için kullanıcı adı da gerekli")
		}
	}

	// mTLS
	c.TLSCert, c.TLSKey, c.TLSCA = cleanPEM(c.TLSCert), cleanPEM(c.TLSKey), cleanPEM(c.TLSCA)
	if len(c.TLSCert) > maxPEM || len(c.TLSKey) > maxPEM || len(c.TLSCA) > maxPEM {
		return invalid("Sertifika ve anahtar alanları en fazla 64 KB olabilir")
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		return invalid("İstemci sertifikası ve özel anahtarı birlikte girilmeli")
	}
	if c.TLSCert != "" {
		if _, err := tls.X509KeyPair([]byte(c.TLSCert), []byte(c.TLSKey)); err != nil {
			return invalid("İstemci sertifikası veya özel anahtar geçersiz (PEM biçiminde olmalı ve birbirine uymalı)")
		}
	}
	if c.TLSCA != "" && !x509.NewCertPool().AppendCertsFromPEM([]byte(c.TLSCA)) {
		return invalid("Kök sertifika (CA) PEM biçiminde geçerli bir sertifika içermeli")
	}

	// OAuth2
	c.OAuthTokenURL = strings.TrimSpace(c.OAuthTokenURL)
	c.OAuthClientID = strings.TrimSpace(c.OAuthClientID)
	c.OAuthScopes = strings.Join(strings.FieldsFunc(c.OAuthScopes, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	}), " ")
	if c.OAuthTokenURL == "" {
		if c.OAuthClientID != "" || c.OAuthClientSecret != "" {
			return invalid("OAuth2 için token adresi gerekli")
		}
		c.OAuthClientSecret, c.OAuthScopes, c.OAuthAuthStyle = "", "", ""
		return nil
	}
	u, err := url.Parse(c.OAuthTokenURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("OAuth2 token adresi geçerli bir http:// veya https:// adresi olmalı")
	}
	if c.OAuthClientID == "" || c.OAuthClientSecret == "" {
		return invalid("OAuth2 için istemci kimliği (client ID) ve istemci sırrı (client secret) gerekli")
	}
	switch c.OAuthAuthStyle {
	case "":
		c.OAuthAuthStyle = "header"
	case "header", "body":
	default:
		return invalid("OAuth2 kimlik gönderimi header veya body olmalı")
	}
	if c.BasicUser != "" || c.BasicPass != "" {
		return invalid("Temel kimlik doğrulama (kullanıcı adı/şifre) ile OAuth2 birlikte kullanılamaz")
	}
	return nil
}

func cleanPEM(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSpace(s)
	if s != "" {
		s += "\n"
	}
	return s
}

// clientTLSConfig izin verilen TLS ayarını kurar (sertifika doğrulama, özel
// kök sertifika, istemci sertifikası).
func clientTLSConfig(c HTTPConfig) (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: c.IgnoreTLS}
	if c.TLSCA != "" {
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM([]byte(c.TLSCA)) {
			return nil, errors.New("Kök sertifika (CA) okunamadı")
		}
		cfg.RootCAs = pool
	}
	if c.TLSCert != "" {
		cert, err := tls.X509KeyPair([]byte(c.TLSCert), []byte(c.TLSKey))
		if err != nil {
			return nil, errors.New("İstemci sertifikası yüklenemedi: " + err.Error())
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// proxyFunc monitörün proxy'sini döner; proxy yoksa nil (ortam değişkenleri
// bilerek dikkate alınmaz: her monitör kendi yolunu seçer).
func proxyFunc(c HTTPConfig) (func(*http.Request) (*url.URL, error), error) {
	if c.ProxyURL == "" {
		return nil, nil
	}
	u, err := url.Parse(c.ProxyURL)
	if err != nil {
		return nil, errors.New("Proxy adresi okunamadı")
	}
	if c.ProxyUser != "" {
		u.User = url.UserPassword(c.ProxyUser, c.ProxyPass)
	}
	return http.ProxyURL(u), nil
}

// describeHTTPErr proxy hatalarını ayırt eder; diğerleri için describeErr.
func describeHTTPErr(ctx context.Context, c HTTPConfig, err error) string {
	msg := describeErr(ctx, err)
	if c.ProxyURL == "" || msg == "Zaman aşımı" {
		return msg
	}
	var op *net.OpError
	if (errors.As(err, &op) && op.Op == "proxyconnect") || strings.Contains(err.Error(), "socks connect") {
		return "Proxy hatası: " + msg
	}
	return msg
}

// OAuth2 istemci kimlik bilgileri akışı --------------------------------------------

// oauthEntry tek bir kimlik bilgisi seti için önbellekteki token. sem aynı
// anda tek istek yapılmasını sağlar: aynı API'yi izleyen onlarca monitör
// süresi dolan token için token sunucusuna yığılmaz.
type oauthEntry struct {
	sem     chan struct{}
	token   string
	expires time.Time
}

var oauthCache = struct {
	sync.Mutex
	m map[string]*oauthEntry
}{m: map[string]*oauthEntry{}}

// oauthNow testlerde değiştirilebilir.
var oauthNow = time.Now

func oauthKey(c HTTPConfig) string {
	h := sha256.New()
	for _, s := range []string{c.OAuthTokenURL, c.OAuthClientID, c.OAuthClientSecret, c.OAuthScopes, c.OAuthAuthStyle, c.ProxyURL, c.ProxyUser} {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func oauthEntryFor(key string) *oauthEntry {
	oauthCache.Lock()
	defer oauthCache.Unlock()
	e := oauthCache.m[key]
	if e == nil {
		// Silinen monitörlerin eski kayıtları birikmesin.
		now := oauthNow()
		for k, old := range oauthCache.m {
			if len(old.sem) == 0 && now.Sub(old.expires) > time.Hour {
				delete(oauthCache.m, k)
			}
		}
		e = &oauthEntry{sem: make(chan struct{}, 1)}
		oauthCache.m[key] = e
	}
	return e
}

func forgetOAuthToken(c HTTPConfig) {
	e := oauthEntryFor(oauthKey(c))
	select {
	case e.sem <- struct{}{}:
		e.token = ""
		<-e.sem
	default: // başka bir kontrol zaten yeni token alıyor
	}
}

// oauthToken geçerli bir erişim token'ı döner; cached true ise önbellekten geldi.
func oauthToken(ctx context.Context, client *http.Client, c HTTPConfig) (string, bool, error) {
	e := oauthEntryFor(oauthKey(c))
	select {
	case e.sem <- struct{}{}:
	case <-ctx.Done():
		return "", false, errors.New("zaman aşımı")
	}
	defer func() { <-e.sem }()
	if e.token != "" && oauthNow().Before(e.expires) {
		return e.token, true, nil
	}
	token, ttl, err := fetchOAuthToken(ctx, client, c)
	if err != nil {
		return "", false, err
	}
	// Süre dolmadan biraz önce yenilenir (saat farkı ve istek süresi payı).
	margin := min(ttl/10, time.Minute)
	e.token, e.expires = token, oauthNow().Add(ttl-margin)
	return token, false, nil
}

func fetchOAuthToken(ctx context.Context, client *http.Client, c HTTPConfig) (string, time.Duration, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if c.OAuthScopes != "" {
		form.Set("scope", c.OAuthScopes)
	}
	if c.OAuthAuthStyle == "body" {
		form.Set("client_id", c.OAuthClientID)
		form.Set("client_secret", c.OAuthClientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.OAuthTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if c.OAuthAuthStyle != "body" {
		// RFC 6749 2.3.1: kimlik bilgileri form kodlamasıyla kodlanıp Basic'e konur.
		req.SetBasicAuth(url.QueryEscape(c.OAuthClientID), url.QueryEscape(c.OAuthClientSecret))
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, errors.New(describeHTTPErr(ctx, c, redactURL(err)))
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", 0, errors.New(describeErr(ctx, err))
	}
	var tr struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
		Error       string          `json:"error"`
		ErrorDesc   string          `json:"error_description"`
	}
	ct, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if ct == "application/x-www-form-urlencoded" || ct == "text/plain" {
		// Bazı eski sunucular (ör. GitHub) form biçiminde yanıt verir.
		q, _ := url.ParseQuery(string(data))
		tr.AccessToken, tr.Error, tr.ErrorDesc = q.Get("access_token"), q.Get("error"), q.Get("error_description")
		tr.ExpiresIn = json.RawMessage(strconv.Quote(q.Get("expires_in")))
	} else {
		json.Unmarshal(data, &tr)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 || tr.AccessToken == "" {
		detail := strings.TrimSpace(tr.Error + " " + tr.ErrorDesc)
		if detail == "" {
			detail = truncate(strings.TrimSpace(string(data)), 120)
		}
		if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
			return "", 0, fmt.Errorf("yanıtta access_token yok %s", detail)
		}
		return "", 0, fmt.Errorf("HTTP %d %s", resp.StatusCode, truncate(detail, 160))
	}
	ttl := 5 * time.Minute // süre bildirilmezse kısa tutulur
	s := strings.Trim(string(tr.ExpiresIn), `"`)
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		ttl = time.Duration(min(n, 86400)) * time.Second
	}
	return tr.AccessToken, ttl, nil
}

// redactURL ağ hatasındaki tam adresi (sorgu dizesi dahil) çıkarır.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
