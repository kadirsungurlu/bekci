package api

// Güvenlik sıkılaştırması için testler (denetim bulguları 1-8).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Bulgu 1: özel alan adı yalnızca yöneticiye açık ve kendini kilitleyemez.
func TestPageCustomDomainAdminGate(t *testing.T) {
	pe := setupPages(t) // yönetici; BaseURL = uptime.test
	editor, _ := pe.newUser("editor1", store.RoleEditor, nil)

	body := func(extra map[string]any) map[string]any {
		m := map[string]any{"slug": "durum", "title": "Durum", "sections": []any{}}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}

	// Editör alan adsız sayfa oluşturabilir.
	var p pageResp
	editor.mustDo("POST", "/api/status-pages", body(nil), &p, 201)
	path := fmt.Sprintf("/api/status-pages/%d", p.ID)

	// Editör alan adı EKLEYEMEZ.
	if code := editor.do("PUT", path, body(map[string]any{"custom_domain": "durum.ornek.com"}), nil); code != 403 {
		t.Fatalf("editör alan adı ekleyince %d, 403 bekleniyordu", code)
	}
	// Yönetici alan adı ekleyebilir.
	var withDom pageResp
	pe.mustDo("PUT", path, body(map[string]any{"custom_domain": "durum.ornek.com"}), &withDom, 200)
	if withDom.CustomDomain != "durum.ornek.com" {
		t.Fatalf("alan adı ayarlanmadı: %q", withDom.CustomDomain)
	}
	// Editör, alan adı DEĞİŞMEDEN sayfayı kaydedebilir (başlık değişir).
	if code := editor.do("PUT", path, body(map[string]any{"custom_domain": "durum.ornek.com", "title": "Yeni Başlık"}), nil); code != 200 {
		t.Fatalf("editör değişmeyen alan adıyla kaydedince %d, 200 bekleniyordu", code)
	}
	// Editör alan adını DEĞİŞTİREMEZ/KALDIRAMAZ.
	if code := editor.do("PUT", path, body(map[string]any{"custom_domain": "", "title": "Yeni Başlık"}), nil); code != 403 {
		t.Fatalf("editör alan adını kaldırınca %d, 403 bekleniyordu", code)
	}
	// Yönetici uygulamanın kendi adresini alan adı yapamaz (kilitlenme koruması).
	if code := pe.do("PUT", path, body(map[string]any{"custom_domain": "uptime.test", "title": "Yeni Başlık"}), nil); code != 400 {
		t.Fatalf("yönetici kendi adresini alan adı yapınca %d, 400 bekleniyordu", code)
	}
}

// hostReq belirli bir Host başlığıyla JSON isteği gönderir (özel alan adı
// testleri için); e.client'ın çerezlerini kullanır.
func (e *env) hostReq(method, path, host string, body any) int {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	// Host elle değiştirilince istemci çerez kavanozunu otomatik eklemez;
	// oturum çerezini açıkça iliştir.
	for _, c := range e.client.Jar.Cookies(req.URL) {
		req.AddCookie(c)
	}
	req.Host = host
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Bulgu 1(b): BASE_URL boşken sayfa kendini isteğin geldiği adrese kilitleyemez.
func TestPageCustomDomainSelfLockNoBaseURL(t *testing.T) {
	e := setupAdmin(t) // s.BaseURL boş
	body := map[string]any{"slug": "durum", "title": "Durum", "sections": []any{}, "custom_domain": "durum.ornek.com"}
	if code := e.hostReq("POST", "/api/status-pages", "durum.ornek.com", body); code != 400 {
		t.Fatalf("isteğin geldiği adres alan adı yapılınca %d, 400 bekleniyordu", code)
	}
	// Başka bir adresten aynı istek geçerli olmalı.
	if code := e.hostReq("POST", "/api/status-pages", "panel.ornek.com", body); code != 201 {
		t.Fatalf("farklı adresten alan adı %d, 201 bekleniyordu", code)
	}
}

// Bulgu 2: eşzamanlı SSE bağlantısı sınırı aşılınca 429.
func TestEventStreamCap(t *testing.T) {
	e := setupAdmin(t)
	var cancels []context.CancelFunc
	var bodies []*http.Response
	t.Cleanup(func() {
		for _, c := range cancels {
			c()
		}
		for _, r := range bodies {
			r.Body.Close()
		}
	})
	open := func() int {
		ctx, cancel := context.WithCancel(context.Background())
		cancels = append(cancels, cancel)
		req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
		resp, err := e.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, resp)
		return resp.StatusCode
	}
	// Kullanıcı başına sınıra kadar açılabilir.
	for i := 0; i < engine.MaxSubsPerUser; i++ {
		if code := open(); code != 200 {
			t.Fatalf("%d. canlı bağlantı: %d, 200 bekleniyordu", i+1, code)
		}
	}
	// Sınırın üstündeki bağlantı reddedilir.
	if code := open(); code != 429 {
		t.Fatalf("sınır üstü canlı bağlantı: %d, 429 bekleniyordu", code)
	}
}

// Bulgu 3: kısıtlı izleyici yalnızca görebildiği monitörlerde kullanılan
// etiketleri görür (kullanılmayan etiketin adı sızmaz).
func TestListTagsRestrictedScope(t *testing.T) {
	admin := setupAdmin(t)
	mine := admin.push("Benim")   // kısıtlı izleyicinin göreceği
	other := admin.push("Diğeri") // görmeyeceği

	var used, hidden tagView
	admin.mustDo("POST", "/api/tags", map[string]any{"name": "Kullanılan"}, &used, 201)
	admin.mustDo("POST", "/api/tags", map[string]any{"name": "Gizli"}, &hidden, 201)

	// "Kullanılan" görünür monitöre, "Gizli" yalnızca görünmeyen monitöre bağlanır.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", mine.ID),
		[]map[string]any{{"tag_id": used.ID}}, nil, 200)
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", other.ID),
		[]map[string]any{{"tag_id": hidden.ID}}, nil, 200)

	// Kısıtsız yönetici iki etiketi de görür.
	var all []tagView
	admin.mustDo("GET", "/api/tags", nil, &all, 200)
	if len(all) != 2 {
		t.Fatalf("yönetici %d etiket gördü, 2 bekleniyordu", len(all))
	}

	// Kısıtlı izleyici yalnızca "mine" monitörünü görür.
	viewer, _ := admin.newUser("musteri", store.RoleViewer, []int64{mine.ID})
	var scoped []tagView
	viewer.mustDo("GET", "/api/tags", nil, &scoped, 200)
	if len(scoped) != 1 || scoped[0].Name != "Kullanılan" {
		t.Fatalf("kısıtlı izleyici %+v gördü, yalnızca \"Kullanılan\" bekleniyordu", scoped)
	}
}

// Bulgu 4: rozet IP hız sınırı (sabit pencere).
func TestBadgeRateLimit(t *testing.T) {
	b := newBadgeState()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < badgeRatePerMin; i++ {
		if !b.rate("1.2.3.4", now) {
			t.Fatalf("%d. istek reddedildi, sınır %d", i+1, badgeRatePerMin)
		}
	}
	if b.rate("1.2.3.4", now) {
		t.Fatal("sınır aşıldıktan sonra istek kabul edildi")
	}
	if !b.rate("5.6.7.8", now) {
		t.Fatal("farklı IP sınırlanmamalı")
	}
	if !b.rate("1.2.3.4", now.Add(time.Minute)) {
		t.Fatal("yeni dakikada sınır sıfırlanmalı")
	}
}

// Bulgu 4: herkese açık monitör kümesi kısa süre önbelleğe alınır.
func TestBadgeAllowCache(t *testing.T) {
	f := newFeatureEnv(t)
	m := f.push("Site")
	p := f.page(true, "", m.ID)
	r := httptest.NewRequest("GET", "/", nil)

	allow, err := f.s.publicBadgeMonitors(r)
	if err != nil || !allow[m.ID] {
		t.Fatalf("herkese açık monitör izinli değil: %v %v", allow, err)
	}
	// Sayfa doğrudan silinir (API'yi atlar); önbellek TTL içinde eski sonucu döner.
	if err := f.st.DeletePage(context.Background(), p.ID); err != nil {
		t.Fatal(err)
	}
	if allow, _ := f.s.publicBadgeMonitors(r); !allow[m.ID] {
		t.Fatal("önbellek TTL içinde eski sonucu döndürmeliydi")
	}
	// TTL sonrası yenilenir.
	f.clk.advance(badgeAllowTTL + time.Second)
	if allow, _ := f.s.publicBadgeMonitors(r); allow[m.ID] {
		t.Fatal("TTL sonrası önbellek yenilenmeliydi")
	}
}

// Bulgu 5: push adresindeki gizli token loga yazılmadan maskelenir.
func TestMaskLogPath(t *testing.T) {
	cases := map[string]string{
		"/api/push/gizli-token-123": "/api/push/***",
		"/api/push/":                "/api/push/***",
		"/api/monitors":             "/api/monitors",
		"/api/pushx":                "/api/pushx",
		"/api/push":                 "/api/push",
	}
	for in, want := range cases {
		if got := maskLogPath(in); got != want {
			t.Errorf("maskLogPath(%q) = %q, %q bekleniyordu", in, got, want)
		}
	}
}

// Bulgu 6: /api/export API anahtarıyla kullanılamaz (yalnızca oturum).
func TestExportSessionOnly(t *testing.T) {
	f := newFeatureEnv(t) // f.env oturumla (çerez) yönetici
	key := f.newKey("yedek", store.RoleAdmin, 0)

	// API anahtarıyla (çerezsiz) → 401.
	if code, _, _ := f.rawReq("GET", "/api/export", bearer(key.Secret), nil); code != 401 {
		t.Fatalf("API anahtarıyla export: %d, 401 bekleniyordu", code)
	}
	// Oturum çereziyle → 200.
	if code := f.do("GET", "/api/export", nil, nil); code != 200 {
		t.Fatalf("oturumla export: %d, 200 bekleniyordu", code)
	}
	// İçe aktarma da anahtarla kapalı.
	if code, _, _ := f.rawReq("POST", "/api/import", bearer(key.Secret), map[string]any{}); code != 401 {
		t.Fatalf("API anahtarıyla import: %d, 401 bekleniyordu", code)
	}
}

// Bulgu 7: bildirim test/örnek uçlarında kullanıcı başına hız sınırı.
func TestNotificationLimiterUnit(t *testing.T) {
	l := newNotifyLimiter()
	now := time.Unix(1_700_000_000, 0)
	for i := 0; i < notifTestPerMin; i++ {
		if !l.allowTest(1, now) {
			t.Fatalf("%d. test isteği reddedildi", i+1)
		}
	}
	if l.allowTest(1, now) {
		t.Fatal("test sınırı aşıldıktan sonra kabul edildi")
	}
	if !l.allowTest(2, now) {
		t.Fatal("başka kullanıcının testi etkilenmemeli")
	}
	// Örnek: kullanıcı başına aralıkta bir kez.
	if ok, _ := l.allowSample(1, now); !ok {
		t.Fatal("ilk örnek isteği reddedildi")
	}
	if ok, wait := l.allowSample(1, now.Add(10*time.Second)); ok || wait <= 0 {
		t.Fatalf("aralık içinde örnek kabul edildi (wait=%d)", wait)
	}
	if ok, _ := l.allowSample(1, now.Add(notifSampleInterval*time.Second)); !ok {
		t.Fatal("aralık sonrası örnek reddedildi")
	}
}

// Bulgu 7: örnek uç noktası uçtan uca 429 döner (aralık içinde ikinci istek).
func TestNotificationSampleRateLimit(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	t.Cleanup(hook.Close)
	e := setupAdmin(t)
	var ch struct {
		ID int64 `json:"id"`
	}
	e.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Kanca", "type": "webhook", "config": map[string]any{"url": hook.URL},
	}, &ch, 201)
	path := fmt.Sprintf("/api/notifications/%d/samples", ch.ID)
	if code := e.do("POST", path, map[string]any{}, nil); code != 202 {
		t.Fatalf("ilk örnek isteği: %d, 202 bekleniyordu", code)
	}
	if code := e.do("POST", path, map[string]any{}, nil); code != 429 {
		t.Fatalf("ardışık örnek isteği: %d, 429 bekleniyordu", code)
	}
}

// Bulgu 7: test uç noktası dakikalık sınırı aşınca 429.
func TestNotificationTestRateLimit(t *testing.T) {
	e := setupAdmin(t)
	// Geçersiz tip: gönderim yapılmadan 400 döner ama sınır sayacı artar.
	for i := 0; i < notifTestPerMin; i++ {
		if code := e.do("POST", "/api/notifications/test", map[string]any{"type": "yok", "config": map[string]any{}}, nil); code != 400 {
			t.Fatalf("%d. test isteği: %d, 400 bekleniyordu", i+1, code)
		}
	}
	if code := e.do("POST", "/api/notifications/test", map[string]any{"type": "yok", "config": map[string]any{}}, nil); code != 429 {
		t.Fatalf("sınır üstü test isteği: %d, 429 bekleniyordu", code)
	}
}

// Bulgu 8: TRUSTED_PROXY ayarlıyken yalnızca güvenilir eş XFF/CF başlıklarında
// güvenilir; ayarlı değilken eski davranış (özel/loopback güvenilir) korunur.
func TestClientIPTrustedProxy(t *testing.T) {
	trusted := parseCIDRList("10.9.0.0/16")

	newReq := func(remote, xff, cf string) *http.Request {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = remote
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		if cf != "" {
			r.Header.Set("CF-Connecting-IP", cf)
		}
		return r
	}

	// TRUSTED_PROXY ayarlı, doğrudan eş güvenilir ağda → XFF'e güvenilir.
	if got := clientIPUsing(newReq("10.9.0.1:1234", "203.0.113.7", ""), trusted); got != "203.0.113.7" {
		t.Errorf("güvenilir proxy: %q, 203.0.113.7 bekleniyordu", got)
	}
	// TRUSTED_PROXY ayarlı, doğrudan eş güvenilir DEĞİL → XFF yok sayılır.
	if got := clientIPUsing(newReq("192.168.1.5:1234", "203.0.113.7", ""), trusted); got != "192.168.1.5" {
		t.Errorf("güvenilmeyen özel eş: %q, 192.168.1.5 bekleniyordu (XFF yok sayılmalı)", got)
	}
	// Güvenilir proxy arkasında Cloudflare (XFF son değeri CF kenarı) → CF-Connecting-IP.
	cfEdge := "173.245.48.1"
	if got := clientIPUsing(newReq("10.9.0.1:1234", cfEdge, "198.51.100.9"), trusted); got != "198.51.100.9" {
		t.Errorf("güvenilir proxy + Cloudflare: %q, 198.51.100.9 bekleniyordu", got)
	}
	// Güvenilmeyen eş bir CF kenarı olsa bile TRUSTED_PROXY ayarlıyken CF başlığına güvenilmez.
	if got := clientIPUsing(newReq(cfEdge+":1234", "", "198.51.100.9"), trusted); got != cfEdge {
		t.Errorf("güvenilmeyen CF kenarı: %q, %q bekleniyordu", got, cfEdge)
	}

	// TRUSTED_PROXY boş: eski davranış — özel/loopback eş XFF'e güvenilir.
	var none []netip.Prefix
	if got := clientIPUsing(newReq("192.168.1.5:1234", "203.0.113.7", ""), none); got != "203.0.113.7" {
		t.Errorf("varsayılan özel eş: %q, 203.0.113.7 bekleniyordu", got)
	}
	// Boşken CF kenarından doğrudan gelen istekte CF-Connecting-IP'ye güvenilir.
	if got := clientIPUsing(newReq(cfEdge+":1234", "", "198.51.100.9"), none); got != "198.51.100.9" {
		t.Errorf("varsayılan CF kenarı: %q, 198.51.100.9 bekleniyordu", got)
	}
}
