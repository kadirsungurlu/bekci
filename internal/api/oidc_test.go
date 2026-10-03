package api

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// fakeIdP sahte bir OpenID Connect sağlayıcısı: keşif belgesi, JWKS,
// yetkilendirme (hemen geri yönlendirir), token (RS256 id_token) ve userinfo.
type fakeIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	mu    sync.Mutex
	codes map[string]fakeCode
	// Sonraki girişte verilecek kimlik.
	sub, email, username, name string
	verified                   bool
	groups                     []string
	noEmailInToken             bool
}

type fakeCode struct{ nonce, challenge string }

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIdP{key: key, codes: map[string]fakeCode{}, sub: "sub-1", email: "ayse@ornek.com", username: "ayse", name: "Ayşe Yılmaz", verified: true}
	raw := http.NewServeMux()
	// Tüm JSON yanıtlar doğru içerik türüyle (oauth2 türü sezmez; text/plain form sayılır).
	mux := &jsonMux{raw}
	raw.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "userinfo_endpoint": f.srv.URL + "/userinfo",
			"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
			"id_token_signing_alg_values_supported": []string{"RS256"}, "scopes_supported": []string{"openid", "profile", "email"},
		})
	})
	raw.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		b64 := func(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }
		json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "k1", "use": "sig", "alg": "RS256",
			"n": b64(key.PublicKey.N.Bytes()), "e": b64(big.NewInt(int64(key.PublicKey.E)).Bytes()),
		}}})
	})
	raw.HandleFunc("/authorize", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("response_type") != "code" || q.Get("code_challenge_method") != "S256" || q.Get("nonce") == "" {
			http.Error(w, "bad request: "+r.URL.RawQuery, 400)
			return
		}
		code := fmt.Sprintf("code-%d", time.Now().UnixNano())
		f.mu.Lock()
		f.codes[code] = fakeCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
		f.mu.Unlock()
		u, _ := url.Parse(q.Get("redirect_uri"))
		rq := u.Query()
		rq.Set("code", code)
		rq.Set("state", q.Get("state"))
		u.RawQuery = rq.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	})
	raw.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		user, pass, _ := r.BasicAuth()
		if (user != "client-1" || pass != "secret-1") && (r.Form.Get("client_id") != "client-1" || r.Form.Get("client_secret") != "secret-1") {
			http.Error(w, "client auth", 401)
			return
		}
		f.mu.Lock()
		c, ok := f.codes[r.Form.Get("code")]
		delete(f.codes, r.Form.Get("code"))
		f.mu.Unlock()
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != c.challenge {
			http.Error(w, "bad code / pkce", 400)
			return
		}
		claims := map[string]any{"iss": f.srv.URL, "sub": f.sub, "aud": "client-1", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": c.nonce, "preferred_username": f.username, "name": f.name}
		if !f.noEmailInToken {
			claims["email"], claims["email_verified"] = f.email, f.verified
		}
		if f.groups != nil {
			claims["groups"] = f.groups
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at-1", "token_type": "Bearer", "expires_in": 3600, "id_token": f.sign(claims)})
	})
	raw.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"sub": f.sub, "email": f.email, "email_verified": f.verified, "preferred_username": f.username, "name": f.name})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

type jsonMux struct{ *http.ServeMux }

func (m *jsonMux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	m.ServeMux.ServeHTTP(w, r)
}

// sign RS256 JWT.
func (f *fakeIdP) sign(claims map[string]any) string {
	hdr, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"})
	pl, _ := json.Marshal(claims)
	msg := base64.RawURLEncoding.EncodeToString(hdr) + "." + base64.RawURLEncoding.EncodeToString(pl)
	sum := sha256.Sum256([]byte(msg))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

// ssoLogin yeni bir tarayıcı gibi SSO akışını baştan sona yürütür; son
// yanıtın adresini (giriş ekranına dönüşte sso_error taşır) ve istemciyi döner.
func ssoLogin(t *testing.T, srvURL string) (*http.Response, *env) {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 10 * time.Second}
	resp, err := c.Get(srvURL + "/api/auth/oidc/start")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp, &env{t: t, srv: &httptest.Server{URL: srvURL}, client: c}
}

// OIDC uçtan uca: ayar doğrulama, keşif, PKCE + state + nonce, e-postayla
// bağlama, otomatik hesap açma, rol eşlemesi, kapalı sağlayıcı ve bozuk state.
func TestOIDCLogin(t *testing.T) {
	idp := newFakeIdP(t)
	admin := newEnv(t, func(s *Server) { s.BaseURL = "" })
	admin.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	var st map[string]any
	admin.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if oidc := st["oidc"].(map[string]any); oidc["enabled"] != false {
		t.Fatalf("OIDC kapalı başlamalı: %v", st)
	}
	// Ayarlar: https dışı issuer reddedilir (localhost hariç), eksik alanlar 400.
	cfg := map[string]any{"enabled": true, "name": "Şirket SSO", "issuer": "http://idp.example.com", "client_id": "client-1", "client_secret": "secret-1"}
	admin.mustDo("PUT", "/api/settings/oidc", cfg, nil, 400)
	cfg["issuer"] = idp.srv.URL // 127.0.0.1: yerel adrese http serbest
	cfg["client_id"] = ""
	admin.mustDo("PUT", "/api/settings/oidc", cfg, nil, 400)
	cfg["client_id"] = "client-1"
	cfg["auto_provision"] = false
	cfg["link_email"] = true
	cfg["local_login"] = true
	var saved struct {
		Settings    OIDCSettings `json:"settings"`
		RedirectURI string       `json:"redirect_uri"`
	}
	admin.mustDo("PUT", "/api/settings/oidc", cfg, &saved, 200)
	if saved.Settings.ClientSecret != "••••••" || !strings.HasSuffix(saved.RedirectURI, "/api/auth/oidc/callback") || saved.Settings.Scopes != "openid profile email" {
		t.Fatalf("ayar yanıtı: %+v", saved)
	}
	// Maskeli anahtarla yeniden kaydet: kayıtlı anahtar korunur (test akışında kullanılır).
	cfg["client_secret"] = "••••••"
	admin.mustDo("PUT", "/api/settings/oidc", cfg, &saved, 200)
	admin.mustDo("POST", "/api/settings/oidc/test", map[string]any{"issuer": idp.srv.URL}, nil, 200)
	admin.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if oidc := st["oidc"].(map[string]any); oidc["enabled"] != true || oidc["name"] != "Şirket SSO" {
		t.Fatalf("giriş ekranı SSO bilgisi: %v", st)
	}

	// 1) Eşleşen hesap yok, auto_provision kapalı → no_account.
	resp, _ := ssoLogin(t, admin.srv.URL)
	if !strings.Contains(resp.Request.URL.Fragment, "sso_error=no_account") {
		t.Fatalf("hesap yokken no_account bekleniyordu: %s", resp.Request.URL)
	}
	// 2) E-postası eşleşen yerel hesap bağlanır; TOTP sorulmaz; oturum açılır.
	var ayse store.User
	admin.mustDo("POST", "/api/users", map[string]any{"username": "ayse", "role": "editor", "password": "gecici-sifre-1", "email": "ayse@ornek.com"}, &ayse, 201)
	resp, c := ssoLogin(t, admin.srv.URL)
	if resp.Request.URL.Fragment != "" || resp.StatusCode != 200 {
		t.Fatalf("SSO girişi başarılı olmalıydı: %s (%d)", resp.Request.URL, resp.StatusCode)
	}
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	u := st["user"].(map[string]any)
	if u["username"] != "ayse" || u["oidc"] != true || u["must_change_password"] != false {
		t.Fatalf("bağlanan hesap: %v", u)
	}
	var list []store.User
	admin.mustDo("GET", "/api/users", nil, &list, 200)
	for _, x := range list {
		if x.Username == "ayse" && !x.OIDC {
			t.Fatalf("kullanıcı listesinde SSO işareti yok: %+v", x)
		}
	}
	// Aynı subject ikinci girişte e-posta değişse de aynı hesaba düşer.
	idp.email = "baska@ornek.com"
	resp, c = ssoLogin(t, admin.srv.URL)
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["user"].(map[string]any)["username"] != "ayse" {
		t.Fatalf("subject bağı korunmalı: %v", st["user"])
	}
	// 3) Yeni kimlik + auto_provision + rol eşlemesi (groups claim).
	cfg["auto_provision"] = true
	cfg["role_claim"] = "groups"
	cfg["admin_values"] = "bekci-admins"
	cfg["editor_values"] = "bekci-editors, ops"
	cfg["default_role"] = "viewer"
	admin.mustDo("PUT", "/api/settings/oidc", cfg, nil, 200)
	idp.sub, idp.email, idp.username, idp.name, idp.groups = "sub-2", "mehmet@ornek.com", "Mehmet Öz", "Mehmet Öz", []string{"ops"}
	resp, c = ssoLogin(t, admin.srv.URL)
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	u = st["user"].(map[string]any)
	if u["username"] != "mehmetoz" || u["role"] != "editor" || u["email"] != "mehmet@ornek.com" {
		t.Fatalf("otomatik açılan hesap: %v", u)
	}
	// Grup değişince rol güncellenir; şifreyle giriş yapılamaz (rastgele özet).
	idp.groups = []string{"bekci-admins"}
	resp, c = ssoLogin(t, admin.srv.URL)
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["user"].(map[string]any)["role"] != "admin" {
		t.Fatalf("rol eşlemesi güncellenmeli: %v", st["user"])
	}
	admin.mustDo("POST", "/api/auth/login", map[string]string{"username": "mehmetoz", "password": "gecici-sifre-1"}, nil, 401)
	// Doğrulanmamış e-posta yerel hesaba bağlanmaz: auto_provision ile ayrı hesap açılır.
	idp.sub, idp.email, idp.username, idp.verified, idp.groups = "sub-3", "kadir@ornek.com", "kadir", false, nil
	admin.mustDo("POST", "/api/auth/email", map[string]string{"password": "cok-gizli-sifre", "email": "kadir@ornek.com"}, nil, 200)
	resp, c = ssoLogin(t, admin.srv.URL)
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if un := st["user"].(map[string]any)["username"]; un != "kadir-2" {
		t.Fatalf("doğrulanmamış e-posta yöneticiye bağlanmamalı; yeni hesap bekleniyordu: %v", un)
	}
	// 4) Bozuk state: çerezsiz callback ve yanlış state parametresi.
	jar, _ := cookiejar.New(nil)
	cl := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r1, _ := cl.Get(admin.srv.URL + "/api/auth/oidc/callback?code=x&state=y")
	if r1.StatusCode != 303 || !strings.Contains(r1.Header.Get("Location"), "sso_error=state") {
		t.Fatalf("çerezsiz callback state hatası vermeli: %d %s", r1.StatusCode, r1.Header.Get("Location"))
	}
	r1.Body.Close()
	r2, _ := cl.Get(admin.srv.URL + "/api/auth/oidc/start") // çerez alınır
	r2.Body.Close()
	r3, _ := cl.Get(admin.srv.URL + "/api/auth/oidc/callback?code=x&state=wrong")
	if !strings.Contains(r3.Header.Get("Location"), "sso_error=state") {
		t.Fatalf("yanlış state reddedilmeli: %s", r3.Header.Get("Location"))
	}
	r3.Body.Close()
	// 5) Kapalı sağlayıcı: start → disabled.
	cfg["enabled"] = false
	admin.mustDo("PUT", "/api/settings/oidc", cfg, nil, 200)
	resp, _ = ssoLogin(t, admin.srv.URL)
	if !strings.Contains(resp.Request.URL.Fragment, "sso_error=disabled") {
		t.Fatalf("kapalı sağlayıcı: %s", resp.Request.URL)
	}
	// İşlem kaydı.
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit?action=login.oidc", nil, &audit, 200)
	if len(audit) < 4 {
		t.Fatalf("login.oidc kayıtları: %d", len(audit))
	}
	admin.mustDo("GET", "/api/audit?action=user.oidc_provision", nil, &audit, 200)
	if len(audit) != 2 {
		t.Fatalf("user.oidc_provision kayıtları: %d", len(audit))
	}
}
