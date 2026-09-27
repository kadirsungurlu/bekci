package check

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func normalizeHTTP(t *testing.T, cfg map[string]any) (HTTPConfig, error) {
	t.Helper()
	raw, _ := json.Marshal(cfg)
	out, err := httpChecker{}.Normalize(raw)
	if err != nil {
		return HTTPConfig{}, err
	}
	var c HTTPConfig
	json.Unmarshal(out, &c)
	return c, nil
}

func TestHTTPExtrasNormalize(t *testing.T) {
	// Gelişmiş alanlar boşken kayıtlı ayara hiç yazılmaz.
	raw, _ := httpChecker{}.Normalize(json.RawMessage(`{"url":"https://example.com"}`))
	for _, k := range []string{"proxy", "tls_", "oauth"} {
		if strings.Contains(string(raw), k) {
			t.Errorf("boş gelişmiş alan kaydedildi (%s): %s", k, raw)
		}
	}

	c, err := normalizeHTTP(t, map[string]any{"url": "https://example.com", "proxy_url": " SOCKS5://ali:s%C3%BCper@10.0.0.5:1080 "})
	if err != nil {
		t.Fatal(err)
	}
	if c.ProxyURL != "socks5://10.0.0.5:1080" || c.ProxyUser != "ali" || c.ProxyPass != "süper" {
		t.Errorf("proxy kimlik bilgileri ayrılmadı: %+v", c)
	}
	c, _ = normalizeHTTP(t, map[string]any{"url": "https://example.com", "proxy_user": "x", "proxy_pass": "y"})
	if c.ProxyUser != "" || c.ProxyPass != "" {
		t.Errorf("proxy adresi yokken kimlik bilgisi kaldı: %+v", c)
	}

	cert, key := selfSignedPEM(t)
	bad := []struct {
		name string
		cfg  map[string]any
		msg  string
	}{
		{"proxy şeması", map[string]any{"proxy_url": "ftp://x:21"}, "Proxy adresi"},
		{"proxy yolu", map[string]any{"proxy_url": "http://x:3128/yol"}, "yalnızca sunucu"},
		{"proxy şifresiz kullanıcı", map[string]any{"proxy_url": "http://x:3128", "proxy_pass": "p"}, "kullanıcı adı"},
		{"sertifika anahtarsız", map[string]any{"tls_cert": cert}, "birlikte"},
		{"anahtar uyuşmuyor", map[string]any{"tls_cert": cert, "tls_key": mustKeyPEM(t)}, "geçersiz"},
		{"CA bozuk", map[string]any{"tls_ca": "çöp"}, "Kök sertifika"},
		{"oauth adressiz", map[string]any{"oauth_client_id": "a", "oauth_client_secret": "b"}, "token adresi"},
		{"oauth sırsız", map[string]any{"oauth_token_url": "https://auth/token", "oauth_client_id": "a"}, "client secret"},
		{"oauth stil", map[string]any{"oauth_token_url": "https://auth/token", "oauth_client_id": "a", "oauth_client_secret": "b", "oauth_auth_style": "x"}, "header veya body"},
		{"oauth + basic", map[string]any{"oauth_token_url": "https://auth/token", "oauth_client_id": "a", "oauth_client_secret": "b", "basic_user": "u"}, "birlikte kullanılamaz"},
	}
	for _, b := range bad {
		b.cfg["url"] = "https://example.com"
		if _, err := normalizeHTTP(t, b.cfg); err == nil || !strings.Contains(err.Error(), b.msg) {
			t.Errorf("%s: hata %v, %q içermeliydi", b.name, err, b.msg)
		}
	}

	c, err = normalizeHTTP(t, map[string]any{"url": "https://example.com", "tls_cert": cert, "tls_key": key,
		"oauth_token_url": "https://auth.example/token", "oauth_client_id": "id", "oauth_client_secret": "s",
		"oauth_scopes": "read, write\nadmin"})
	if err != nil {
		t.Fatal(err)
	}
	if c.OAuthAuthStyle != "header" || c.OAuthScopes != "read write admin" || !strings.HasSuffix(c.TLSKey, "\n") {
		t.Errorf("normalize: %+v", c)
	}
}

func TestHTTPProxy(t *testing.T) {
	var gotURI, gotAuth atomic.Value
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI.Store(r.RequestURI)
		gotAuth.Store(r.Header.Get("Proxy-Authorization"))
		if u, p, ok := parseProxyAuth(r.Header.Get("Proxy-Authorization")); !ok || u != "ali" || p != "gizli" {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		io.WriteString(w, "proxy üzerinden merhaba")
	}))
	defer proxy.Close()

	// Hedef çözülemeyen bir ad: istek ancak proxy üzerinden başarılı olabilir.
	res := run(t, "http", map[string]any{"url": "http://hedef.invalid/sayfa", "keyword": "merhaba",
		"proxy_url": proxy.URL, "proxy_user": "ali", "proxy_pass": "gizli"})
	if !res.Up {
		t.Fatalf("proxy ile kontrol başarısız: %+v", res)
	}
	if gotURI.Load() != "http://hedef.invalid/sayfa" {
		t.Errorf("proxy'ye giden istek: %v", gotURI.Load())
	}
	res = run(t, "http", map[string]any{"url": "http://hedef.invalid/sayfa", "proxy_url": proxy.URL})
	if res.Up || !strings.Contains(res.Message, "407") {
		t.Errorf("kimlik bilgisiz proxy: %+v", res)
	}

	// Kapalı proxy: hata mesajı proxy'yi işaret eder.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	l.Close()
	res = run(t, "http", map[string]any{"url": "http://hedef.invalid/", "proxy_url": "http://" + addr})
	if res.Up || !strings.Contains(res.Message, "Proxy") {
		t.Errorf("kapalı proxy mesajı: %+v", res)
	}
}

func parseProxyAuth(h string) (string, string, bool) {
	r := &http.Request{Header: http.Header{"Authorization": {h}}}
	return r.BasicAuth()
}

func TestHTTPSocks5Proxy(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "socks ok")
	}))
	defer site.Close()
	socks := startSocks5(t, "kadir", "parola", site.Listener.Addr().String())

	res := run(t, "http", map[string]any{"url": "http://uzak-sunucu.invalid:8080/", "keyword": "socks ok",
		"proxy_url": "socks5h://kadir:parola@" + socks.addr})
	if !res.Up {
		t.Fatalf("socks5 ile kontrol başarısız: %+v", res)
	}
	// socks5h: ad çözümü proxy'de yapılır.
	if got := socks.lastTarget(); got != "uzak-sunucu.invalid:8080" {
		t.Errorf("socks hedefi: %q", got)
	}
	res = run(t, "http", map[string]any{"url": "http://uzak-sunucu.invalid:8080/",
		"proxy_url": "socks5h://kadir:yanlis@" + socks.addr})
	if res.Up {
		t.Error("yanlış socks şifresiyle başarılı oldu")
	}
}

type socksServer struct {
	addr   string
	mu     sync.Mutex
	target string
}

func (s *socksServer) lastTarget() string { s.mu.Lock(); defer s.mu.Unlock(); return s.target }

// startSocks5 kullanıcı adı/şifre doğrulamalı en küçük SOCKS5 sunucusu; her
// CONNECT isteğini backend adresine bağlar ve istenen hedefi kaydeder.
func startSocks5(t *testing.T, user, pass, backend string) *socksServer {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	s := &socksServer{addr: l.Addr().String()}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go s.serve(c, user, pass, backend)
		}
	}()
	return s
}

func (s *socksServer) serve(c net.Conn, user, pass, backend string) {
	defer c.Close()
	r := bufio.NewReader(c)
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(r, hdr); err != nil || hdr[0] != 5 {
		return
	}
	io.ReadFull(r, make([]byte, hdr[1]))
	c.Write([]byte{5, 2}) // kullanıcı adı/şifre
	ver, _ := r.ReadByte()
	ulen, _ := r.ReadByte()
	u := make([]byte, ulen)
	io.ReadFull(r, u)
	plen, _ := r.ReadByte()
	p := make([]byte, plen)
	io.ReadFull(r, p)
	if ver != 1 || string(u) != user || string(p) != pass {
		c.Write([]byte{1, 1})
		return
	}
	c.Write([]byte{1, 0})
	req := make([]byte, 4)
	if _, err := io.ReadFull(r, req); err != nil || req[1] != 1 {
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		io.ReadFull(r, b)
		host = net.IP(b).String()
	case 3:
		n, _ := r.ReadByte()
		b := make([]byte, n)
		io.ReadFull(r, b)
		host = string(b)
	case 4:
		b := make([]byte, 16)
		io.ReadFull(r, b)
		host = net.IP(b).String()
	}
	pb := make([]byte, 2)
	io.ReadFull(r, pb)
	s.mu.Lock()
	s.target = net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb))))
	s.mu.Unlock()
	b, err := net.Dial("tcp", backend)
	if err != nil {
		c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer b.Close()
	c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	go io.Copy(b, r)
	io.Copy(c, b)
}

// Sertifika yardımcıları ----------------------------------------------------------

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pem  string
}

func newTestCA(t *testing.T) testCA {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	return testCA{cert, key, string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))}
}

// issueClient CA'nın imzaladığı istemci sertifikası ve anahtarını PEM olarak döner.
func (ca testCA) issueClient(t *testing.T) (string, string) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "uptime-istemci"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, KeyUsage: x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

func selfSignedPEM(t *testing.T) (string, string) { return newTestCA(t).issueClient(t) }

func mustKeyPEM(t *testing.T) string {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	kb, _ := x509.MarshalECPrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}))
}

func TestHTTPMutualTLS(t *testing.T) {
	ca := newTestCA(t)
	pool := x509.NewCertPool()
	pool.AddCert(ca.cert)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "merhaba %s", r.TLS.PeerCertificates[0].Subject.CommonName)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}
	srv.StartTLS()
	defer srv.Close()
	serverCA := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}))
	cert, key := ca.issueClient(t)

	res := run(t, "http", map[string]any{"url": srv.URL, "tls_cert": cert, "tls_key": key, "tls_ca": serverCA, "keyword": "merhaba uptime-istemci"})
	if !res.Up {
		t.Fatalf("mTLS kontrolü başarısız: %+v", res)
	}
	if res.Cert == nil {
		t.Error("sertifika bilgisi yok")
	}
	res = run(t, "http", map[string]any{"url": srv.URL, "tls_ca": serverCA})
	if res.Up {
		t.Error("istemci sertifikası olmadan başarılı oldu")
	}
	res = run(t, "http", map[string]any{"url": srv.URL, "tls_cert": cert, "tls_key": key})
	if res.Up || !strings.Contains(res.Message, "sertifika") {
		t.Errorf("özel CA olmadan sunucu sertifikası kabul edilmemeliydi: %+v", res)
	}
	res = run(t, "http", map[string]any{"url": srv.URL, "tls_cert": cert, "tls_key": key, "ignore_tls": true})
	if !res.Up {
		t.Errorf("ignore_tls ile mTLS: %+v", res)
	}
}

// oauthServer sahte token sunucusu ve korumalı API.
type oauthServer struct {
	srv         *httptest.Server
	tokenCalls  atomic.Int32
	token       atomic.Value
	reject      atomic.Bool // API bir kez 401 döner
	lastForm    atomic.Value
	lastBasicID atomic.Value
}

func newOAuthServer(t *testing.T, clientID, secret string) *oauthServer {
	o := &oauthServer{}
	o.token.Store("tok-1")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		o.tokenCalls.Add(1)
		time.Sleep(20 * time.Millisecond) // eşzamanlı isteklerin yığılmasını görünür kılar
		r.ParseForm()
		o.lastForm.Store(r.PostForm.Encode())
		id, sec, ok := r.BasicAuth()
		if ok { // RFC 6749 2.3.1: form kodlamalı
			id, _ = url.QueryUnescape(id)
			sec, _ = url.QueryUnescape(sec)
		} else {
			id, sec = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		o.lastBasicID.Store(id)
		if r.PostForm.Get("grant_type") != "client_credentials" || id != clientID || sec != secret {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			io.WriteString(w, `{"error":"invalid_client","error_description":"kimlik hatalı"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":%q,"token_type":"Bearer","expires_in":3600}`, o.token.Load())
	})
	mux.HandleFunc("GET /api", func(w http.ResponseWriter, r *http.Request) {
		if o.reject.CompareAndSwap(true, false) || r.Header.Get("Authorization") != "Bearer "+o.token.Load().(string) {
			w.WriteHeader(401)
			return
		}
		io.WriteString(w, "yetkili")
	})
	o.srv = httptest.NewServer(mux)
	t.Cleanup(o.srv.Close)
	return o
}

func TestHTTPOAuth2(t *testing.T) {
	o := newOAuthServer(t, "istemci:1", "sır&değer")
	cfg := map[string]any{"url": o.srv.URL + "/api", "keyword": "yetkili", "oauth_token_url": o.srv.URL + "/token",
		"oauth_client_id": "istemci:1", "oauth_client_secret": "sır&değer", "oauth_scopes": "okuma yazma"}

	for i := 0; i < 3; i++ {
		if res := run(t, "http", cfg); !res.Up {
			t.Fatalf("oauth kontrolü %d başarısız: %+v", i, res)
		}
	}
	if n := o.tokenCalls.Load(); n != 1 {
		t.Errorf("token %d kez alındı, önbellekten gelmeliydi", n)
	}
	if f := o.lastForm.Load().(string); !strings.Contains(f, "scope=okuma+yazma") || strings.Contains(f, "client_secret") {
		t.Errorf("header stilinde form: %s", f)
	}

	// Sunucu token'ı iptal etti: ilk kontrol 401 alır ve önbelleği temizler, sonraki yenisini alır.
	o.token.Store("tok-2")
	if res := run(t, "http", cfg); res.Up || !strings.Contains(res.Message, "401") {
		t.Errorf("iptal edilen token: %+v", res)
	}
	if res := run(t, "http", cfg); !res.Up {
		t.Errorf("yeni token ile: %+v", res)
	}
	if n := o.tokenCalls.Load(); n != 2 {
		t.Errorf("token çağrısı %d, 2 bekleniyordu", n)
	}

	// Süre dolunca yenilenir.
	oauthNow = func() time.Time { return time.Now().Add(2 * time.Hour) }
	defer func() { oauthNow = time.Now }()
	if res := run(t, "http", cfg); !res.Up || o.tokenCalls.Load() != 3 {
		t.Errorf("süresi dolan token yenilenmedi: %+v, çağrı %d", res, o.tokenCalls.Load())
	}
	oauthNow = time.Now

	// body stili ve hatalı kimlik.
	cfg["oauth_auth_style"] = "body"
	cfg["oauth_client_id"] = "istemci:1"
	cfg["oauth_scopes"] = "baska"
	if res := run(t, "http", cfg); !res.Up {
		t.Errorf("body stili: %+v", res)
	}
	if f := o.lastForm.Load().(string); !strings.Contains(f, "client_secret=") {
		t.Errorf("body stilinde sır formda olmalı: %s", f)
	}
	cfg["oauth_client_secret"] = "yanlış"
	res := run(t, "http", cfg)
	if res.Up || !strings.Contains(res.Message, "OAuth2 token alınamadı") || !strings.Contains(res.Message, "invalid_client") {
		t.Errorf("hatalı kimlik: %+v", res)
	}
}

func TestHTTPOAuth2Concurrent(t *testing.T) {
	o := newOAuthServer(t, "eszamanli", "s")
	raw, _ := json.Marshal(map[string]any{"url": o.srv.URL + "/api", "oauth_token_url": o.srv.URL + "/token",
		"oauth_client_id": "eszamanli", "oauth_client_secret": "s"})
	norm, err := httpChecker{}.Normalize(raw)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var fails atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if !(httpChecker{}).Check(ctx, norm).Up {
				fails.Add(1)
			}
		}()
	}
	wg.Wait()
	if fails.Load() > 0 || o.tokenCalls.Load() != 1 {
		t.Errorf("eşzamanlı: %d hata, %d token çağrısı (1 bekleniyordu)", fails.Load(), o.tokenCalls.Load())
	}
}
