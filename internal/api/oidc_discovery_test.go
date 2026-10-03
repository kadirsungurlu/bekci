package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// discoveryServer yalnızca keşif belgesi veren sahte sunucu. issuer: belgeye
// yazılacak issuer ("" kendi adresi, "/" kendi adresi + sondaki eğik çizgi);
// status 200 dışındaysa belge yerine o kod döner.
func discoveryServer(t *testing.T, issuer string, status int) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		if status != 200 {
			http.Error(w, "yok", status)
			return
		}
		iss := issuer
		switch issuer {
		case "":
			iss = srv.URL
		case "/":
			iss = srv.URL + "/"
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": iss, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token",
			"jwks_uri": srv.URL + "/jwks", "scopes_supported": []string{"openid", "email"},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// doLang oturumlu JSON isteği; yanıt dili başlıkla seçilir (boş: varsayılan).
func (e *env) doLang(method, path, lang string, body any) (int, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	if lang != "" {
		req.Header.Set(langHeader, lang)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data
}

// Açık OIDC ayarı kaydedilirken issuer keşifle doğrulanır; kapalı ayar ve
// issuer'ı değişmeyen açık ayar sağlayıcıya gitmeden kaydedilir.
func TestOIDCSettingsDiscovery(t *testing.T) {
	admin := newEnv(t, func(s *Server) { s.BaseURL = "" })
	admin.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	cfg := func(issuer string, enabled bool) map[string]any {
		return map[string]any{"enabled": enabled, "issuer": issuer, "client_id": "client-1", "client_secret": "secret-1"}
	}
	errOf := func(code int, body []byte, want int) string {
		t.Helper()
		if code != want {
			t.Fatalf("%d bekleniyordu, %d geldi: %s", want, code, body)
		}
		var e struct {
			Error string `json:"error"`
		}
		json.Unmarshal(body, &e)
		return e.Error
	}
	put := func(lang string, body map[string]any, want int) string {
		t.Helper()
		code, data := admin.doLang("PUT", "/api/settings/oidc", lang, body)
		return errOf(code, data, want)
	}

	// Ulaşılamayan adres (kapatılmış sunucu): 400, TR ve EN mesaj.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	if msg := put("", cfg(deadURL, true), 400); !strings.HasPrefix(msg, "Issuer adresine ulaşılamadı: ") {
		t.Errorf("ulaşılamayan issuer mesajı: %q", msg)
	}
	if msg := put("en", cfg(deadURL, true), 400); !strings.HasPrefix(msg, "The issuer could not be reached: ") {
		t.Errorf("İngilizce mesaj: %q", msg)
	}
	// Aynı adres kapalı ayarla keşifsiz kaydedilir; giriş ekranında SSO yok.
	admin.mustDo("PUT", "/api/settings/oidc", cfg(deadURL, false), nil, 200)
	var st map[string]any
	admin.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["oidc"].(map[string]any)["enabled"] != false {
		t.Fatalf("kapalı ayar: %v", st["oidc"])
	}

	// OIDC sağlayıcısı olmayan adres (keşif belgesi 404).
	plain := discoveryServer(t, "", 404)
	if msg := put("", cfg(plain.URL, true), 400); msg != "Bu adres bir OpenID Connect sağlayıcısı değil (keşif belgesi 404 döndü)" {
		t.Errorf("OIDC olmayan adres mesajı: %q", msg)
	}
	if msg := put("en", cfg(plain.URL, true), 400); msg != "This address is not an OpenID Connect provider (the discovery document returned 404)" {
		t.Errorf("İngilizce mesaj: %q", msg)
	}
	// Belge var ama OIDC alanları yok (HTML sayfası).
	junk := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>merhaba</html>"))
	}))
	t.Cleanup(junk.Close)
	if msg := put("", cfg(junk.URL, true), 400); msg != "Bu adres bir OpenID Connect sağlayıcısı değil (keşif belgesi okunamadı)" {
		t.Errorf("bozuk belge mesajı: %q", msg)
	}

	// Sağlayıcının bildirdiği issuer girilenle uyuşmuyor.
	wrong := discoveryServer(t, "https://baska.example.com", 200)
	if msg := put("", cfg(wrong.URL, true), 400); msg != "Issuer uyuşmuyor: sağlayıcı kendini https://baska.example.com olarak tanıtıyor" {
		t.Errorf("uyuşmazlık mesajı: %q", msg)
	}
	if msg := put("en", cfg(wrong.URL, true), 400); msg != "Issuer mismatch: the provider identifies itself as https://baska.example.com" {
		t.Errorf("İngilizce uyuşmazlık mesajı: %q", msg)
	}
	// "Keşfi dene" ucu da aynı sınıflandırmayı yapar (422).
	code, data := admin.doLang("POST", "/api/settings/oidc/test", "", map[string]any{"issuer": wrong.URL})
	if msg := errOf(code, data, 422); !strings.HasPrefix(msg, "Issuer uyuşmuyor") {
		t.Errorf("keşfi dene mesajı: %q", msg)
	}
	// Hiçbir hatalı deneme kaydedilmedi: ayar hâlâ kapalı, issuer ölü adres.
	var got struct {
		Settings OIDCSettings `json:"settings"`
	}
	admin.mustDo("GET", "/api/settings/oidc", nil, &got, 200)
	if got.Settings.Enabled || got.Settings.Issuer != deadURL {
		t.Fatalf("hatalı denemeler kaydedilmemeli: %+v", got.Settings)
	}

	// Başarı: geçerli sağlayıcı; sondaki "/" ile girilse de sağlayıcının
	// yazımı (eğik çizgisiz) saklanır. Keşfi dene de 200 ve issuer'ı döner.
	good := discoveryServer(t, "", 200)
	var saved struct {
		Settings OIDCSettings `json:"settings"`
	}
	admin.mustDo("PUT", "/api/settings/oidc", cfg(good.URL+"/", true), &saved, 200)
	if !saved.Settings.Enabled || saved.Settings.Issuer != good.URL {
		t.Fatalf("kayıt: %+v", saved.Settings)
	}
	admin.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["oidc"].(map[string]any)["enabled"] != true {
		t.Fatalf("SSO açılmalıydı: %v", st["oidc"])
	}
	var test map[string]any
	admin.mustDo("POST", "/api/settings/oidc/test", map[string]any{"issuer": good.URL}, &test, 200)
	if test["ok"] != true || test["issuer"] != good.URL || test["authorization_endpoint"] != good.URL+"/authorize" {
		t.Errorf("keşfi dene yanıtı: %v", test)
	}

	// Sağlayıcı issuer'ını sondaki "/" ile yazıyorsa o yazım saklanır
	// (go-oidc girişte birebir eşleşme ister).
	slashy := discoveryServer(t, "/", 200)
	admin.mustDo("PUT", "/api/settings/oidc", cfg(slashy.URL, true), &saved, 200)
	if saved.Settings.Issuer != slashy.URL+"/" {
		t.Fatalf("sağlayıcının yazımı saklanmalı: %q", saved.Settings.Issuer)
	}
	// Issuer değişmeden diğer alanlar güncellenirken sağlayıcıya gidilmez:
	// sunucu kapansa da kayıt 200 ve issuer yazımı korunur.
	slashy.Close()
	again := cfg(slashy.URL, true)
	again["name"] = "Şirket SSO"
	again["client_secret"] = "••••••"
	admin.mustDo("PUT", "/api/settings/oidc", again, &saved, 200)
	if saved.Settings.Name != "Şirket SSO" || saved.Settings.Issuer != slashy.URL+"/" {
		t.Fatalf("issuer değişmeden kayıt: %+v", saved.Settings)
	}
	// Issuer değişince (kapalı adrese) yine doğrulanır ve reddedilir; eski ayar durur.
	put("", cfg(deadURL, true), 400)
	admin.mustDo("GET", "/api/settings/oidc", nil, &got, 200)
	if got.Settings.Issuer != slashy.URL+"/" || got.Settings.Name != "Şirket SSO" {
		t.Fatalf("reddedilen kayıt eskisini bozmamalı: %+v", got.Settings)
	}
	// Kapatmak her zaman serbest.
	admin.mustDo("PUT", "/api/settings/oidc", cfg(deadURL, false), nil, 200)
}
