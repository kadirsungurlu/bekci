package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// testClock sunucunun saatini ileri almaya yarar (süre dolumu, TOTP adımları).
type testClock struct{ off atomic.Int64 }

func (c *testClock) now() time.Time          { return time.Now().Add(time.Duration(c.off.Load())) }
func (c *testClock) advance(d time.Duration) { c.off.Add(int64(d)) }

// fenv sunucuya ve veritabanına doğrudan erişilebilen test ortamı (newEnv'in
// bu dosyadaki özellikler için genişletilmiş hali).
type fenv struct {
	*env
	s   *Server
	st  *store.Store
	clk *testClock
}

func newFeatureEnv(t *testing.T) *fenv {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Millisecond, BaseURL: "https://uptime.test"})
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Uptime</title>")}}
	s := New(st, eng, hub, disp, log, static, "test")
	s.relaxTimeoutRule = true // aralıklar milisaniye biriminde (bkz. newEnv)
	clk := &testClock{}
	s.now = clk.now
	// Uzun yoklama: art arda iş listesi isteyen testler (istek sınırı vb.)
	// beklemesin. Uzun yoklama testleri kendi süresini kurar (probes_longpoll_test.go).
	s.probeHold = 10 * time.Millisecond
	srv := httptest.NewServer(s.Handler())
	jar, _ := cookiejar.New(nil)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		cancel()
		eng.Wait()
		disp.Wait(time.Second)
		st.Close()
	})
	e := &env{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return &fenv{env: e, s: s, st: st, clk: clk}
}

func (f *fenv) resetLimiter() {
	f.s.limiter.mu.Lock()
	f.s.limiter.keys = map[string]*attempts{}
	f.s.limiter.mu.Unlock()
}

// raw çerezsiz istek; hdr başlıkları eklenir. X-Uptime otomatik eklenmez.
func (e *env) rawReq(method, path string, hdr map[string]string, body any) (int, http.Header, []byte) {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header, data
}

func bearer(key string) map[string]string { return map[string]string{"Authorization": "Bearer " + key} }

type createdKey struct {
	Key    apiKeyView `json:"key"`
	Secret string     `json:"secret"`
}

func (e *env) newKey(name, role string, expiresAt int64) createdKey {
	e.t.Helper()
	var ck createdKey
	body := map[string]any{"name": name, "role": role}
	if expiresAt != 0 {
		body["expires_at"] = expiresAt
	}
	e.mustDo("POST", "/api/api-keys", body, &ck, 201)
	return ck
}

var keyFormat = regexp.MustCompile(`^upk_[0-9A-Za-z]{43}$`)

func TestAPIKeyAuth(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env

	ck := admin.newKey("CI", store.RoleEditor, 0)
	if !keyFormat.MatchString(ck.Secret) || !strings.HasPrefix(ck.Secret, ck.Key.Prefix) || len(ck.Key.Prefix) != 12 {
		t.Fatalf("anahtar biçimi hatalı: %q prefix %q", ck.Secret, ck.Key.Prefix)
	}
	if ck.Key.Status != "active" || ck.Key.Role != store.RoleEditor || ck.Key.Username != "kadir" {
		t.Fatalf("anahtar bilgisi hatalı: %+v", ck.Key)
	}
	// Liste ne anahtarı ne özetini içerir.
	if code, _, _ := admin.rawReq("GET", "/api/api-keys", nil, nil); code != 401 {
		t.Errorf("çerezsiz anahtar listesi: %d, 401 bekleniyordu", code)
	}
	var listRaw json.RawMessage
	admin.mustDo("GET", "/api/api-keys", nil, &listRaw, 200)
	if strings.Contains(string(listRaw), ck.Secret[4:]) || strings.Contains(string(listRaw), hashToken(ck.Secret)) {
		t.Fatalf("liste anahtarı veya özetini sızdırıyor: %s", listRaw)
	}

	// Okuma ve yazma; anahtarlı istek X-Uptime başlığı olmadan da geçer (CSRF muafiyeti).
	if code, _, body := admin.rawReq("GET", "/api/monitors", bearer(ck.Secret), nil); code != 200 {
		t.Fatalf("anahtarla liste: %d %s", code, body)
	}
	code, _, body := admin.rawReq("POST", "/api/monitors", bearer(ck.Secret),
		map[string]any{"name": "anahtarla", "type": "push", "interval": 5000, "config": map[string]any{}})
	if code != 201 {
		t.Fatalf("anahtarla monitör ekleme (X-Uptime yok): %d %s", code, body)
	}
	// Anahtar + çerez: CSRF muafiyeti yok.
	req, _ := http.NewRequest("POST", admin.srv.URL+"/api/monitors", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+ck.Secret)
	resp, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("çerezli + anahtarlı istek X-Uptime olmadan: %d, 403 bekleniyordu", resp.StatusCode)
	}
	// Anahtarsız ve X-Uptime'sız çerezsiz istek de CSRF'e takılır.
	if code, _, _ := admin.rawReq("POST", "/api/monitors", map[string]string{"Authorization": "Bearer başka"}, map[string]any{}); code != 403 {
		t.Errorf("upk_ olmayan Bearer X-Uptime olmadan: %d, 403 bekleniyordu", code)
	}

	// Anahtar yönetimi, şifre değişimi ve 2FA anahtarla yapılamaz.
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/api-keys"}, {"POST", "/api/api-keys"}, {"DELETE", fmt.Sprintf("/api/api-keys/%d", ck.Key.ID)},
		{"POST", "/api/auth/password"}, {"POST", "/api/auth/2fa/setup"}, {"POST", "/api/users/1/2fa/reset"},
	} {
		if code, _, body := admin.rawReq(c.method, c.path, bearer(ck.Secret), map[string]any{}); code != 401 {
			t.Errorf("anahtarla %s %s: %d %s, 401 bekleniyordu", c.method, c.path, code, body)
		}
	}

	// Geçersiz anahtar.
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer("upk_yanlis"), nil); code != 401 {
		t.Errorf("geçersiz anahtar: %d", code)
	}

	// Son kullanım zamanı yazılır.
	k, _ := f.st.GetAPIKey(context.Background(), ck.Key.ID)
	if k.LastUsedAt == 0 {
		t.Error("son kullanım zamanı yazılmadı")
	}

	// İptal.
	admin.mustDo("DELETE", fmt.Sprintf("/api/api-keys/%d", ck.Key.ID), nil, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(ck.Secret), nil); code != 401 {
		t.Errorf("iptal edilen anahtar: %d, 401 bekleniyordu", code)
	}
	var list []apiKeyView
	admin.mustDo("GET", "/api/api-keys", nil, &list, 200)
	if len(list) != 1 || list[0].Status != "revoked" || list[0].RevokedAt == 0 {
		t.Errorf("iptal listede görünmeli: %+v", list)
	}

	// Süre dolumu.
	exp := admin.newKey("geçici", store.RoleViewer, f.clk.now().Add(time.Hour).Unix())
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(exp.Secret), nil); code != 200 {
		t.Fatalf("süresi dolmamış anahtar: %d", code)
	}
	f.clk.advance(2 * time.Hour)
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(exp.Secret), nil); code != 401 {
		t.Errorf("süresi dolan anahtar: %d, 401 bekleniyordu", code)
	}
	admin.mustDo("GET", "/api/api-keys", nil, &list, 200)
	if list[0].ID != exp.Key.ID || list[0].Status != "expired" {
		t.Errorf("süresi dolan anahtar durumu: %+v", list[0])
	}

	// Doğrulama.
	for _, body := range []map[string]any{
		{"name": "", "role": "viewer"},
		{"name": "x", "role": "root"},
		{"name": "x", "role": "viewer", "expires_at": f.clk.now().Add(-time.Minute).Unix()},
		{"name": strings.Repeat("a", 101), "role": "viewer"},
	} {
		if code := admin.do("POST", "/api/api-keys", body, nil); code != 400 {
			t.Errorf("geçersiz anahtar isteği %v: %d, 400 bekleniyordu", body, code)
		}
	}
}

func TestAPIKeyRoleCapAndOwner(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	editor, eu := admin.newUser("editor1", store.RoleEditor, nil)

	// Kendi rolünden yüksek anahtar verilemez.
	if code := editor.do("POST", "/api/api-keys", map[string]any{"name": "x", "role": "admin"}, nil); code != 400 {
		t.Errorf("editör yönetici anahtarı: %d, 400 bekleniyordu", code)
	}
	ek := editor.newKey("editör", store.RoleEditor, 0)
	vk := editor.newKey("izleyici", store.RoleViewer, 0)
	newMon := map[string]any{"name": "x", "type": "push", "interval": 5000, "config": map[string]any{}}

	// Anahtar rolü üst sınırdır.
	if code, _, _ := admin.rawReq("POST", "/api/monitors", bearer(vk.Secret), newMon); code != 403 {
		t.Errorf("izleyici anahtarıyla monitör ekleme: %d, 403 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("GET", "/api/users", bearer(ek.Secret), nil); code != 403 {
		t.Errorf("editör anahtarıyla kullanıcı listesi: %d, 403 bekleniyordu", code)
	}
	// Yönetici anahtarı kullanıcıları görebilir.
	ak := admin.newKey("yönetici", store.RoleAdmin, 0)
	if code, _, _ := admin.rawReq("GET", "/api/users", bearer(ak.Secret), nil); code != 200 {
		t.Errorf("yönetici anahtarıyla kullanıcı listesi: %d", code)
	}

	// Sahibin rolü düşünce anahtar da düşer (min(sahip, anahtar)).
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", eu.ID), map[string]any{"role": "viewer"}, nil, 200)
	if code, _, _ := admin.rawReq("POST", "/api/monitors", bearer(ek.Secret), newMon); code != 403 {
		t.Errorf("rolü düşen sahibin editör anahtarı: %d, 403 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(ek.Secret), nil); code != 200 {
		t.Errorf("rolü düşen sahibin anahtarı okuyabilmeli: %d", code)
	}

	// Başka kullanıcının anahtarı: editör göremez/iptal edemez (404); yönetici hepsini görür.
	if code := editor.do("DELETE", fmt.Sprintf("/api/api-keys/%d", ak.Key.ID), nil, nil); code != 404 {
		t.Errorf("başkasının anahtarını iptal: %d, 404 bekleniyordu", code)
	}
	if code := editor.do("GET", "/api/api-keys?all=1", nil, nil); code != 403 {
		t.Errorf("yönetici olmayan ?all=1: %d, 403 bekleniyordu", code)
	}
	var all []apiKeyView
	admin.mustDo("GET", "/api/api-keys?all=1", nil, &all, 200)
	if len(all) != 3 {
		t.Errorf("yönetici tüm anahtarları görmeli: %d", len(all))
	}

	// Yönetici şifreyi sıfırlayınca (zorunlu değişim) anahtar çalışmaya devam eder,
	// şifre değişimi kapısına takılmaz.
	admin.mustDo("POST", fmt.Sprintf("/api/users/%d/password", eu.ID), map[string]any{"password": "yeni-gecici-1"}, nil, 200)
	if code, _, body := admin.rawReq("GET", "/api/monitors", bearer(vk.Secret), nil); code != 200 {
		t.Errorf("zorunlu şifre değişimindeki sahibin anahtarı: %d %s", code, body)
	}

	// Sahibi devre dışı → geçersiz; tekrar açılınca çalışır; silinince geçersiz.
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", eu.ID), map[string]any{"role": "viewer", "disabled": true}, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(vk.Secret), nil); code != 401 {
		t.Errorf("devre dışı sahibin anahtarı: %d, 401 bekleniyordu", code)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", eu.ID), map[string]any{"role": "viewer"}, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(vk.Secret), nil); code != 200 {
		t.Errorf("tekrar açılan sahibin anahtarı: %d", code)
	}
	admin.mustDo("DELETE", fmt.Sprintf("/api/users/%d", eu.ID), nil, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/monitors", bearer(vk.Secret), nil); code != 401 {
		t.Errorf("silinen sahibin anahtarı: %d, 401 bekleniyordu", code)
	}

	// Kısıtlı izleyicinin anahtarı yalnızca izinli monitörleri görür.
	a := admin.push("A")
	b := admin.push("B")
	cust, _ := admin.newUser("musteri", store.RoleViewer, []int64{a.ID})
	ck := cust.newKey("müşteri", store.RoleViewer, 0)
	var list []monitorView
	_, _, body := admin.rawReq("GET", "/api/monitors", bearer(ck.Secret), nil)
	json.Unmarshal(body, &list)
	if len(list) != 1 || list[0].ID != a.ID {
		t.Errorf("kısıtlı anahtar sadece izinli monitörü görmeli: %s", body)
	}
	if code, _, _ := admin.rawReq("GET", fmt.Sprintf("/api/monitors/%d", b.ID), bearer(ck.Secret), nil); code != 404 {
		t.Errorf("kısıtlı anahtarla başka monitör: %d, 404 bekleniyordu", code)
	}

	// İşlem kaydı.
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	seen := map[string]bool{}
	for _, e := range audit {
		seen[e.Action] = true
	}
	if !seen["apikey.create"] {
		t.Error("apikey.create işlem kaydında yok")
	}
}
