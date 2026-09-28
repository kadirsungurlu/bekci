package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/backup"
	"github.com/kadirsa1105/uptime-kadir-app/internal/servers"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Webhook adresi gizli olan kanal (Discord) maskeli adresle yeniden
// adlandırılabilir ve "Test gönder" kayıtlı adrese gider.
func TestMaskedWebhookChannelEditable(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hook.Close()

	admin := setupAdmin(t)
	var ch store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Discord", "type": "discord", "config": map[string]any{"webhook_url": hook.URL + "/api/webhooks/1/gizli"},
	}, &ch, 201)
	editor, _ := admin.newUser("editor3", store.RoleEditor, nil)
	masked := map[string]any{"webhook_url": "••••••"}
	editor.mustDo("PUT", fmt.Sprintf("/api/notifications/%d", ch.ID), map[string]any{"name": "Discord 2", "type": "discord", "config": masked}, nil, 200)
	editor.mustDo("POST", "/api/notifications/test", map[string]any{"id": ch.ID, "type": "discord", "config": masked}, nil, 200)
	mu.Lock()
	if hits != 1 {
		t.Errorf("test bildirimi kayıtlı adrese gitmedi: %d", hits)
	}
	mu.Unlock()
	var list []store.Notification
	admin.mustDo("GET", "/api/notifications", nil, &list, 200)
	if len(list) != 1 || list[0].Name != "Discord 2" {
		t.Fatalf("ad değişmedi: %+v", list)
	}
	stored, _ := json.Marshal(list[0])
	if strings.Contains(string(stored), "gizli") {
		t.Errorf("liste yanıtında adres maskelenmeli: %s", stored)
	}
}

// Veritabanı monitöründe maskeli şifreyle sorgu/kullanıcı değiştirilemez.
func TestMonitorSecretBoundFields(t *testing.T) {
	admin := setupAdmin(t)
	editor, _ := admin.newUser("editor4", store.RoleEditor, nil)
	base := map[string]any{"host": "127.0.0.1", "port": 1, "username": "izleme", "password": "db-sifresi", "database": "app", "query": "SELECT 1"}
	var m monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "PG", "type": "postgres", "interval": 86400, "config": base}, &m, 201)
	put := func(cfg map[string]any) (int, string) {
		var e map[string]any
		code := editor.do("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "PG", "type": "postgres", "interval": 86400, "config": cfg}, &e)
		s, _ := e["error"].(string)
		return code, s
	}
	with := func(k string, v any) map[string]any {
		c := map[string]any{}
		for kk, vv := range base {
			c[kk] = vv
		}
		c["password"] = "••••••"
		if k != "" {
			c[k] = v
		}
		return c
	}
	// Değişiklik yok (ya da ilgisiz alan): maskeli şifre kabul edilir.
	if code, e := put(with("", nil)); code != 200 {
		t.Fatalf("maskeli, değişiklik yok: %d %s", code, e)
	}
	for k, v := range map[string]any{
		"query": "SELECT password FROM users", "username": "postgres", "database": "baska", "expected": "x", "sslmode": "disable",
	} {
		if code, e := put(with(k, v)); code != 400 || !strings.Contains(e, "yeniden girmeniz") {
			t.Errorf("%s değişince maskeli şifre reddedilmeli: %d %s", k, code, e)
		}
	}
	// Şifre yeniden girilirse sorgu değiştirilebilir.
	c := with("query", "SELECT 2")
	c["password"] = "db-sifresi"
	if code, e := put(c); code != 200 {
		t.Fatalf("şifre yeniden girilince: %d %s", code, e)
	}
	// Redis: anahtar değişimi.
	var rm monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "R", "type": "redis", "interval": 86400,
		"config": map[string]any{"host": "127.0.0.1", "port": 1, "password": "r", "key": "saglik"}}, &rm, 201)
	var e map[string]any
	if code := editor.do("PUT", fmt.Sprintf("/api/monitors/%d", rm.ID), map[string]any{"name": "R", "type": "redis", "interval": 86400,
		"config": map[string]any{"host": "127.0.0.1", "port": 1, "password": "••••••", "key": "oturum:admin"}}, &e); code != 400 {
		t.Errorf("redis anahtarı değişince ret bekleniyordu: %d %v", code, e)
	}
	editor.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", rm.ID), map[string]any{"name": "R2", "type": "redis", "interval": 86400,
		"config": map[string]any{"host": "127.0.0.1", "port": 1, "password": "••••••", "key": "saglik"}}, nil, 200)
}

// sseStream canlı akışı açar; satırlar lines kanalına, akış kapanınca done kapanır.
func sseStream(t *testing.T, e *env, hdr map[string]string) (lines <-chan string, done <-chan struct{}, cancel func()) {
	t.Helper()
	ctx, cancelFn := context.WithTimeout(context.Background(), 10*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events", nil)
	client := e.client
	for k, v := range hdr {
		req.Header.Set(k, v)
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		cancelFn()
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		cancelFn()
		t.Fatalf("akış: %d", resp.StatusCode)
	}
	ch := make(chan string, 100)
	d := make(chan struct{})
	go func() {
		defer close(d)
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			select {
			case ch <- sc.Text():
			default:
			}
		}
	}()
	return ch, d, cancelFn
}

func waitClosed(t *testing.T, what string, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: canlı akış kapanmadı", what)
	}
}

func sseEnv(t *testing.T) *env {
	e := newEnv(t, func(s *Server) { s.sseRecheck = 30 * time.Millisecond })
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return e
}

// Açık canlı akış, erişim kaldırılınca (oturum kapatma, hesap devre dışı,
// API anahtarı iptali) kapanır.
func TestEventsStreamRevalidates(t *testing.T) {
	admin := sseEnv(t)

	// Oturum kapatılınca.
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	_, done, cancel := sseStream(t, viewer, nil)
	defer cancel()
	viewer.mustDo("POST", "/api/auth/logout", nil, nil, 200)
	waitClosed(t, "oturum kapatma", done)

	// Hesap devre dışı bırakılınca.
	v2, u2 := admin.newUser("izleyici2", store.RoleViewer, nil)
	_, done2, cancel2 := sseStream(t, v2, nil)
	defer cancel2()
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", u2.ID), map[string]any{"role": store.RoleViewer, "disabled": true}, nil, 200)
	waitClosed(t, "devre dışı", done2)

	// API anahtarı iptal edilince.
	ck := admin.newKey("akis", store.RoleViewer, 0)
	_, done3, cancel3 := sseStream(t, admin, bearer(ck.Secret))
	defer cancel3()
	admin.mustDo("DELETE", fmt.Sprintf("/api/api-keys/%d", ck.Key.ID), nil, nil, 200)
	waitClosed(t, "anahtar iptali", done3)
}

// Kısıtlı izleyicinin kapsamı değişince açık akış yeni kapsama göre süzer.
func TestEventsStreamScopeChange(t *testing.T) {
	admin := sseEnv(t)
	a, b := admin.push("A"), admin.push("B")
	customer, cu := admin.newUser("musteri", store.RoleViewer, []int64{a.ID})
	lines, _, cancel := sseStream(t, customer, nil)
	defer cancel()
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", cu.ID), map[string]any{"role": store.RoleViewer, "all_monitors": false, "monitor_ids": []int64{b.ID}}, nil, 200)
	time.Sleep(200 * time.Millisecond) // en az bir yeniden doğrulama
	for _, m := range []monitorView{a, b} {
		r, _ := http.Get(admin.srv.URL + "/api/push/" + m.PushToken)
		r.Body.Close()
	}
	deadline := time.After(5 * time.Second)
	for {
		select {
		case line := <-lines:
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev struct {
				Data struct {
					MonitorID int64 `json:"monitor_id"`
				} `json:"data"`
			}
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
			if ev.Data.MonitorID == a.ID {
				t.Fatal("kapsamdan çıkarılan monitörün olayı hâlâ geliyor")
			}
			if ev.Data.MonitorID == b.ID {
				return
			}
		case <-deadline:
			t.Fatal("yeni kapsamdaki monitörün olayı gelmedi")
		}
	}
}

// Konum mesajları izleyiciye temizlenmiş gider; kısıtlı izleyici yalnızca
// kendi monitörlerinde kullanılan kontrol noktalarını görür.
func TestLocationMessagesAndProbeListForViewers(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	ctx := context.Background()
	pa, pb := admin.newProbe("Konum A"), admin.newProbe("Konum B")
	m1 := admin.httpMonitor("m1", "https://a.example")
	m2 := admin.httpMonitor("m2", "https://b.example")
	admin.setLocations(m1.ID, map[string]any{"include_local": false, "probe_ids": []int64{pa.Probe.ID}, "down_when": "any"}, 200)
	admin.setLocations(m2.ID, map[string]any{"include_local": false, "probe_ids": []int64{pb.Probe.ID}, "down_when": "any"}, 200)
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", m1.ID), nil, nil, 200)
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/resume", m1.ID), nil, nil, 200)

	now := time.Now().UnixMilli()
	code, _, body := admin.rawReq("POST", "/api/probe/results", bearer(pa.Token), map[string]any{"sent_at": now, "results": []map[string]any{
		{"monitor_id": m1.ID, "time": now - 100, "up": false, "ping_ms": -1, "message": "Get https://kullanici:parola@ic.example/yol?token=cokgizli: bağlantı reddedildi"},
	}})
	if code != 200 {
		t.Fatalf("sonuç: %d %s", code, body)
	}
	waitFor(t, "m1 DOWN", func() bool {
		m, _ := f.st.GetMonitor(ctx, m1.ID)
		return m.Status == store.StatusDown
	})
	var full locationsView
	waitFor(t, "konum görüntüsü", func() bool {
		admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m1.ID), nil, &full, 200)
		return len(full.Locations) == 1 && full.Locations[0].LastCheckAt != 0
	})
	if !strings.Contains(full.Locations[0].Message, "cokgizli") {
		t.Fatalf("yönetici tam mesajı görmeli: %+v", full.Locations)
	}
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	var lv locationsView
	viewer.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m1.ID), nil, &lv, 200)
	if len(lv.Locations) != 1 || strings.Contains(lv.Locations[0].Message, "cokgizli") || strings.Contains(lv.Locations[0].Message, "parola") ||
		!strings.Contains(lv.Locations[0].Message, "https://ic.example/yol") {
		t.Fatalf("izleyici temizlenmiş mesaj görmeli: %+v", lv.Locations)
	}
	// Motorun ortak kopyası bozulmamalı.
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", m1.ID), nil, &full, 200)
	if !strings.Contains(full.Locations[0].Message, "cokgizli") {
		t.Fatalf("temizleme motorun kopyasını değiştirdi: %+v", full.Locations)
	}

	// Kontrol noktası listesi.
	names := func(e *env) []string {
		var list []probeSummary
		e.mustDo("GET", "/api/probes", nil, &list, 200)
		var out []string
		for _, p := range list {
			out = append(out, p.Name)
		}
		return out
	}
	if got := names(viewer); len(got) != 2 {
		t.Errorf("kısıtsız izleyici tüm konumları görmeli: %v", got)
	}
	customer, _ := admin.newUser("musteri", store.RoleViewer, []int64{m1.ID})
	if got := names(customer); len(got) != 1 || got[0] != "Konum A" {
		t.Errorf("kısıtlı izleyici yalnızca kendi monitörünün konumunu görmeli: %v", got)
	}
	nobody, _ := admin.newUser("musteri2", store.RoleViewer, []int64{})
	if got := names(nobody); len(got) != 0 {
		t.Errorf("monitörü olmayan müşteri konum görmemeli: %v", got)
	}
}

// İçe aktarma, uygulamanın kendi adresini özel alan adı yapamaz.
func TestImportDropsSelfCustomDomain(t *testing.T) {
	e := newEnv(t, func(s *Server) { s.BaseURL = "https://uptime.kadir.app" })
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	doc := map[string]any{
		"format": "uptime-kadir", "version": 1,
		"status_pages": []map[string]any{
			{"slug": "kendi", "title": "Kendi", "published": true, "custom_domain": "Uptime.Kadir.App."},
			{"slug": "baska", "title": "Başka", "published": true, "custom_domain": "durum.ornek.com"},
		},
	}
	sum := e.importDoc("/api/import", doc, 200)
	msgs := map[string]string{}
	for _, it := range sum.Items {
		msgs[it.Name] = strings.Join(it.Messages, " | ")
	}
	if !strings.Contains(msgs["Kendi"], "kendi adresi olamaz") {
		t.Errorf("uyarı yok: %q", msgs["Kendi"])
	}
	var pages []store.StatusPage
	e.mustDo("GET", "/api/status-pages", nil, &pages, 200)
	got := map[string]string{}
	for _, p := range pages {
		got[p.Slug] = p.CustomDomain
	}
	if got["kendi"] != "" || got["baska"] != "durum.ornek.com" {
		t.Errorf("özel alan adları: %v", got)
	}
	// Admin arayüzü kilitlenmedi.
	e.mustDo("GET", "/api/monitors", nil, nil, 200)

	// İsteğin geldiği adres de (BASE_URL boşken) reddedilir.
	_, it := planPage(backup.Page{Slug: "x", Title: "X", CustomDomain: "panel.ornek.com"}, nil, map[string]bool{}, map[string]bool{}, map[string]bool{"panel.ornek.com": true})
	if !strings.Contains(strings.Join(it.Messages, " "), "kendi adresi olamaz") {
		t.Errorf("istek adresi: %+v", it)
	}
}

// Sunucunun kilitli IP'si yalnızca yöneticiye görünür (liste, detay, canlı akış).
func TestServerLockedIPAdminOnly(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var cp serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Sunucu"}, &cp, 201)
	if code := probeCall(t, admin.srv.URL, cp.Token, "POST", "/api/probe/metrics", testSample(10)); code != 204 {
		t.Fatalf("metrik: %d", code)
	}
	type lockView struct {
		IPLock   bool   `json:"ip_lock"`
		LockedIP string `json:"locked_ip"`
	}
	var ad lockView
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", cp.Probe.ID), nil, &ad, 200)
	if !ad.IPLock || ad.LockedIP == "" {
		t.Fatalf("yönetici kilidi görmeli: %+v", ad)
	}
	for _, role := range []string{store.RoleViewer, store.RoleEditor} {
		u, _ := admin.newUser("k-"+role, role, nil)
		var v lockView
		u.mustDo("GET", fmt.Sprintf("/api/servers/%d", cp.Probe.ID), nil, &v, 200)
		var list struct {
			Servers []lockView `json:"servers"`
		}
		u.mustDo("GET", "/api/servers", nil, &list, 200)
		if v.LockedIP != "" || v.IPLock || len(list.Servers) != 1 || list.Servers[0].LockedIP != "" {
			t.Errorf("%s kilitli IP'yi görmemeli: %+v %+v", role, v, list)
		}
	}
	// Canlı akış olayı.
	msg, _ := json.Marshal(map[string]any{"type": "server", "data": servers.View{ID: 1, IPLock: true, LockedIP: "203.0.113.5"}})
	for _, u := range []store.User{{Role: store.RoleViewer}, {Role: store.RoleEditor}} {
		out := viewerEvent(u, msg, nil)
		if bytes.Contains(out, []byte("203.0.113.5")) || !bytes.Contains(out, []byte(`"type":"server"`)) {
			t.Errorf("%s: canlı akışta kilitli IP: %s", u.Role, out)
		}
	}
	if out := viewerEvent(store.User{Role: store.RoleAdmin}, msg, nil); !bytes.Contains(out, []byte("203.0.113.5")) {
		t.Errorf("yönetici canlı akışta kilidi görmeli: %s", out)
	}
}

func TestCheckIPLock(t *testing.T) {
	cases := []struct {
		locked, ip, next string
		allowed, pin     bool
	}{
		{"", "203.0.113.5", "203.0.113.5", true, true},
		{"203.0.113.5", "203.0.113.5", "203.0.113.5", true, false},
		{"203.0.113.5", "203.0.113.6", "203.0.113.5", false, false},
		{"203.0.113.5", "::ffff:203.0.113.5", "203.0.113.5", true, false},
		// İkinci aile ilk isteğinde sabitlenir.
		{"203.0.113.5", "2001:db8:1:2::10", "203.0.113.5,2001:db8:1:2::/64", true, true},
		{"", "2001:db8:1:2:aaaa::1", "2001:db8:1:2::/64", true, true},
		// Aynı /64 içindeki gizlilik adresi kabul, başka /64 ret.
		{"203.0.113.5,2001:db8:1:2::/64", "2001:db8:1:2:ffff:ee:dd:cc", "203.0.113.5,2001:db8:1:2::/64", true, false},
		{"203.0.113.5,2001:db8:1:2::/64", "2001:db8:1:3::1", "203.0.113.5,2001:db8:1:2::/64", false, false},
		{"2001:db8:1:2::/64", "198.51.100.1", "198.51.100.1,2001:db8:1:2::/64", true, true},
		{"203.0.113.5,2001:db8:1:2::/64", "198.51.100.1", "203.0.113.5,2001:db8:1:2::/64", false, false},
		// Eski sürümün tek IPv6 değeri /64 olarak okunur.
		{"2001:db8:1:2::10", "2001:db8:1:2::99", "2001:db8:1:2::10", true, false},
	}
	for _, c := range cases {
		next, allowed, pin := checkIPLock(c.locked, c.ip)
		if next != c.next || allowed != c.allowed || pin != c.pin {
			t.Errorf("%q + %s: (%q,%v,%v), beklenen (%q,%v,%v)", c.locked, c.ip, next, allowed, pin, c.next, c.allowed, c.pin)
		}
	}
}

// Çift yığınlı ajan: IPv4 ve IPv6 ayrı ayrı sabitlenir; IPv6'da /64 yeterli.
func TestProbeIPLockDualStack(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	cp := admin.newProbe("Çift")
	job := func(xff string) int {
		h := bearer(cp.Token)
		h["X-Forwarded-For"] = xff
		code, _, _ := admin.rawReq("GET", "/api/probe/jobs", h, nil)
		return code
	}
	lockedIP := func() string {
		p, err := f.st.GetProbe(context.Background(), cp.Probe.ID)
		if err != nil {
			t.Fatal(err)
		}
		return p.LockedIP
	}
	for _, c := range []struct {
		ip   string
		want int
	}{
		{"203.0.113.5", 200},
		{"2001:db8:1:2::10", 200},      // IPv6 ailesi ilk kez: sabitlenir
		{"2001:db8:1:2:abcd::77", 200}, // gizlilik adresi, aynı /64
		{"203.0.113.5", 200},
		{"2001:db8:9::1", 403}, // başka /64
		{"203.0.113.6", 403},   // başka IPv4
	} {
		if got := job(c.ip); got != c.want {
			t.Errorf("%s: %d, %d bekleniyordu", c.ip, got, c.want)
		}
	}
	if ip := lockedIP(); ip != "203.0.113.5,2001:db8:1:2::/64" {
		t.Fatalf("kilit değeri: %q", ip)
	}
	// Sıfırlama iki aileyi de temizler.
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": cp.Probe.Name, "reset_ip": true}, nil, 200)
	if ip := lockedIP(); ip != "" {
		t.Fatalf("sıfırlama sonrası: %q", ip)
	}
	if got := job("2001:db8:9::1"); got != 200 {
		t.Fatalf("sıfırlama sonrası yeni IPv6: %d", got)
	}
}

// İlk sabitleme yarışı: aynı anda iki farklı IPv4'ten gelen isteklerden
// yalnızca bir IP kazanır; kaybedenin istekleri reddedilir.
func TestProbeIPLockFirstPinRace(t *testing.T) {
	for round := range 3 {
		f := newFeatureEnv(t)
		cp := f.env.newProbe(fmt.Sprintf("Yarış %d", round))
		ips := []string{"198.51.100.1", "198.51.100.2"}
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok := map[string]int{}
		start := make(chan struct{})
		for i := range 8 {
			ip := ips[i%2]
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				req, _ := http.NewRequest("GET", f.srv.URL+"/api/probe/jobs", nil)
				req.Header.Set("Authorization", "Bearer "+cp.Token)
				req.Header.Set("X-Forwarded-For", ip)
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					return
				}
				resp.Body.Close()
				if resp.StatusCode == 200 {
					mu.Lock()
					ok[ip]++
					mu.Unlock()
				}
			}()
		}
		close(start)
		wg.Wait()
		p, _ := f.st.GetProbe(context.Background(), cp.Probe.ID)
		if len(ok) != 1 || ok[p.LockedIP] != 4 {
			t.Fatalf("tur %d: yalnızca kilitlenen IP kabul edilmeli: kabul=%v kilit=%q", round, ok, p.LockedIP)
		}
	}
}

// Başarısız ajan kimlik doğrulaması IP başına sınırlanır; uyarı logu örneklenir.
func TestProbeAuthFailLimit(t *testing.T) {
	var buf safeBuffer
	clk := &testClock{}
	e := newEnv(t, func(s *Server) {
		s.log = slog.New(slog.NewTextHandler(&buf, nil))
		s.now = clk.now
	})
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	cp := e.newProbe("P")
	bad := map[string]string{"Authorization": "Bearer upr_yanlis", "X-Forwarded-For": "203.0.113.50"}
	for i := range probeAuthFailPerMin {
		if code, _, _ := e.rawReq("GET", "/api/probe/jobs", bad, nil); code != 401 {
			t.Fatalf("%d. hatalı istek: %d", i, code)
		}
	}
	code, hdr, _ := e.rawReq("GET", "/api/probe/jobs", bad, nil)
	if code != 429 || hdr.Get("Retry-After") == "" {
		t.Fatalf("sınır aşılınca 429 bekleniyordu: %d", code)
	}
	// Aynı IP'den geçerli token da bu dakika bekler; başka IP etkilenmez.
	good := bearer(cp.Token)
	good["X-Forwarded-For"] = "203.0.113.51"
	if code, _, _ := e.rawReq("GET", "/api/probe/jobs", good, nil); code != 200 {
		t.Fatalf("başka IP etkilenmemeli: %d", code)
	}
	if n := strings.Count(buf.String(), "geçersiz kontrol noktası token'ı"); n != 1 {
		t.Errorf("uyarı logu IP başına dakikada bir kez yazılmalı: %d\n%s", n, buf.String())
	}
	// Bir sonraki dakikada sınır sıfırlanır.
	clk.advance(time.Minute)
	if code, _, _ := e.rawReq("GET", "/api/probe/jobs", bad, nil); code != 401 {
		t.Fatalf("yeni dakikada: %d", code)
	}
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Panik logu push token'ını içermez.
func TestRecovererMasksPushToken(t *testing.T) {
	var buf safeBuffer
	s := &Server{log: slog.New(slog.NewTextHandler(&buf, nil))}
	h := s.recoverer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("patladı") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/api/push/cok-gizli-token", nil))
	if rec.Code != 500 || strings.Contains(buf.String(), "cok-gizli-token") || !strings.Contains(buf.String(), "/api/push/***") {
		t.Fatalf("panik logu: %d %s", rec.Code, buf.String())
	}
}

// HSTS yalnızca HTTPS isteğinde gönderilir.
func TestHSTSOnlyOnHTTPS(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "ok") }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if v := rec.Header().Get("Strict-Transport-Security"); v != "" {
		t.Errorf("HTTP isteğinde HSTS olmamalı: %q", v)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if v := rec.Header().Get("Strict-Transport-Security"); v != "max-age=31536000" {
		t.Errorf("HTTPS isteğinde HSTS: %q", v)
	}
}
