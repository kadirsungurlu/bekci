package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"
	"unicode/utf8"

	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store/storetest"
)

func init() { bcryptCost = 4 } // bcrypt.MinCost

type env struct {
	t      *testing.T
	srv    *httptest.Server
	client *http.Client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	st := storetest.Open(t, time.UTC)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	// Aralıklar testte milisaniye cinsinden: "interval: 20" = 20 ms.
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Millisecond, BaseURL: "https://uptime.test"})
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>Uptime</title>")}}
	s := New(st, eng, hub, disp, log, static, "test")
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
	return &env{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// do JSON isteği gönderir; out nil değilse yanıtı çözer, durum kodunu döner.
func (e *env) do(method, path string, body any, out any) int {
	e.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, e.srv.URL+path, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			e.t.Fatalf("%s %s: yanıt çözülemedi (%d): %s", method, path, resp.StatusCode, data)
		}
	}
	return resp.StatusCode
}

func (e *env) mustDo(method, path string, body any, out any, want int) {
	e.t.Helper()
	var raw json.RawMessage
	code := e.do(method, path, body, &raw)
	if code != want {
		e.t.Fatalf("%s %s: durum %d, %d bekleniyordu: %s", method, path, code, want, raw)
	}
	if out != nil {
		json.Unmarshal(raw, out)
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("beklenen durum oluşmadı: %s", what)
}

type hookReceiver struct {
	mu     sync.Mutex
	events []notify.WebhookPayload
}

func (h *hookReceiver) kinds() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, e := range h.events {
		out = append(out, e.Event)
	}
	return out
}

func TestFullFlow(t *testing.T) {
	e := newEnv(t)

	// İzlenecek sahte site: failing true olunca 503 döner.
	var failing atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(503)
			return
		}
		io.WriteString(w, "merhaba dünya")
	}))
	defer site.Close()

	// Bildirimleri yakalayan webhook alıcısı.
	hooks := &hookReceiver{}
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p notify.WebhookPayload
		json.NewDecoder(r.Body).Decode(&p)
		hooks.mu.Lock()
		hooks.events = append(hooks.events, p)
		hooks.mu.Unlock()
	}))
	defer hookSrv.Close()

	// Kurulum öncesi durum.
	var state map[string]any
	e.mustDo("GET", "/api/auth/state", nil, &state, 200)
	if state["setup_needed"] != true || state["user"] != nil {
		t.Fatalf("kurulum bekleniyordu: %v", state)
	}
	e.mustDo("GET", "/api/monitors", nil, nil, 401)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "kisa"}, nil, 400)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "baska", "password": "cok-gizli-sifre"}, nil, 409)

	// Varsayılan bildirim kanalı (yeni monitörlere otomatik bağlanır).
	var ch store.Notification
	e.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Test kancası", "type": "webhook", "is_default": true,
		"config": map[string]any{"url": hookSrv.URL, "headers": "Authorization: Bearer gizli"},
	}, &ch, 201)
	if strings.Contains(string(ch.Config), "gizli") {
		t.Fatalf("gizli başlık maskelenmeliydi: %s", ch.Config)
	}
	e.mustDo("POST", "/api/notifications/test", map[string]any{"id": ch.ID, "type": "webhook", "config": json.RawMessage(ch.Config)}, nil, 200)
	waitFor(t, "test bildirimi", func() bool { return len(hooks.kinds()) == 1 })

	// Monitör: HTTP + keyword; 20 ms aralık, 1 tekrar deneme.
	var mon monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "http", "interval": 20, "retry_interval": 20, "max_retries": 1, "timeout": 300,
		"config": map[string]any{"url": site.URL, "keyword": "dünya", "basic_user": "a", "basic_pass": "parola"},
	}, &mon, 201)
	if len(mon.NotificationIDs) != 1 || mon.NotificationIDs[0] != ch.ID || mon.Target != site.URL {
		t.Fatalf("monitör yanlış: %+v", mon)
	}
	if strings.Contains(string(mon.Config), "parola") {
		t.Fatal("basic_pass maskelenmeliydi")
	}
	id := mon.ID
	getStatus := func() int {
		var d struct{ Monitor monitorView }
		e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", id), nil, &d, 200)
		return d.Monitor.Status
	}
	waitFor(t, "UP", func() bool { return getStatus() == store.StatusUp })

	// Geçersiz monitörler reddedilir.
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "x", "type": "http", "config": map[string]any{"url": "bozuk"}}, nil, 400)
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "x", "type": "yok", "config": map[string]any{}}, nil, 400)
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "x", "type": "tcp", "interval": 5, "config": map[string]any{"host": "a", "port": 1}}, nil, 400)

	// Site çöker: tekrar denemeden sonra DOWN ve bildirim.
	failing.Store(true)
	waitFor(t, "DOWN bildirimi", func() bool { return strings.Contains(strings.Join(hooks.kinds(), ","), "down") })
	if getStatus() != store.StatusDown {
		t.Fatal("DOWN bekleniyordu")
	}
	var sum map[string]any
	e.mustDo("GET", "/api/summary", nil, &sum, 200)
	if sum["down"] != float64(1) || sum["incidents_24h"] != float64(1) {
		t.Fatalf("özet yanlış: %v", sum)
	}

	// Site düzelir: UP bildirimi ve kapanmış olay.
	failing.Store(false)
	waitFor(t, "UP bildirimi", func() bool { return strings.Contains(strings.Join(hooks.kinds(), ","), "up") })
	var incidents []store.Incident
	e.mustDo("GET", "/api/incidents", nil, &incidents, 200)
	if len(incidents) != 1 || incidents[0].ResolvedAt == 0 || incidents[0].Cause != "HTTP 503 Service Unavailable" {
		t.Fatalf("olaylar yanlış: %+v", incidents)
	}
	hooks.mu.Lock()
	down := hooks.events[1]
	hooks.mu.Unlock()
	if down.Monitor.Name != "Site" || down.Monitor.URL != fmt.Sprintf("https://uptime.test/#/monitors/%d", id) {
		t.Errorf("bildirim içeriği yanlış: %+v", down)
	}

	// Liste, çubuklar ve grafik verisi.
	var list []monitorView
	e.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || len(list[0].Bars) != 24 || list[0].Uptime24h == nil || *list[0].Uptime24h >= 100 {
		t.Fatalf("liste yanlış: %+v", list)
	}
	var series struct {
		Kind   string
		Points []map[string]any
	}
	e.mustDo("GET", fmt.Sprintf("/api/monitors/%d/series?range=24h", id), nil, &series, 200)
	if series.Kind != "raw" || len(series.Points) < 3 {
		t.Fatalf("grafik verisi yanlış: %+v", series)
	}
	e.mustDo("GET", fmt.Sprintf("/api/monitors/%d/series?range=7d", id), nil, &series, 200)
	if series.Kind != "hourly" || len(series.Points) != 1 {
		t.Fatalf("7 günlük seri yanlış: %+v", series)
	}

	// Düzenleme: maskeli şifre geri gönderilince eski şifre korunur.
	var edited monitorView
	e.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", id), map[string]any{
		"name": "Site 2", "type": "http", "interval": 20, "timeout": 300,
		"config": json.RawMessage(mon.Config),
	}, &edited, 200)
	if edited.Name != "Site 2" || len(edited.NotificationIDs) != 1 {
		t.Fatalf("düzenleme yanlış: %+v", edited)
	}

	// Durdur / başlat.
	var paused monitorView
	e.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", id), nil, &paused, 200)
	if paused.Active {
		t.Fatal("durdurulmuş olmalı")
	}
	e.mustDo("POST", fmt.Sprintf("/api/monitors/%d/resume", id), nil, nil, 200)
	waitFor(t, "yeniden UP", func() bool { return getStatus() == store.StatusUp })

	// Silme.
	e.mustDo("DELETE", fmt.Sprintf("/api/monitors/%d", id), nil, nil, 200)
	e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", id), nil, nil, 404)
}

func TestPushEndpoint(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var mon monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Yedekleme", "type": "push", "interval": 5000, "config": map[string]any{}}, &mon, 201)
	if len(mon.PushToken) < 20 {
		t.Fatalf("push token üretilmedi: %q", mon.PushToken)
	}

	// Push oturum ve CSRF başlığı olmadan çalışır (cron/curl).
	resp, err := http.Get(e.srv.URL + "/api/push/" + mon.PushToken + "?msg=tamam&ping=12")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("push: %v %v", resp.StatusCode, err)
	}
	waitFor(t, "push UP", func() bool {
		var d struct{ Monitor monitorView }
		e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", mon.ID), nil, &d, 200)
		return d.Monitor.Status == store.StatusUp && d.Monitor.LastMessage == "tamam" && d.Monitor.LastPingMs == 12
	})
	resp, _ = http.Post(e.srv.URL+"/api/push/yanlis", "text/plain", nil)
	if resp.StatusCode != 404 {
		t.Errorf("bilinmeyen token 404 olmalı: %d", resp.StatusCode)
	}
}

func TestSecurity(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	// CSRF başlığı olmadan değiştiren istek reddedilir.
	req, _ := http.NewRequest("POST", e.srv.URL+"/api/monitors", strings.NewReader(`{}`))
	resp, _ := e.client.Do(req)
	if resp.StatusCode != 403 {
		t.Errorf("CSRF: %d", resp.StatusCode)
	}
	// Güvenlik başlıkları.
	resp, _ = e.client.Get(e.srv.URL + "/")
	if resp.Header.Get("X-Frame-Options") != "DENY" || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
		t.Error("güvenlik başlıkları eksik")
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<title>Uptime</title>") {
		t.Error("arayüz sunulmadı")
	}
	// Tek sayfalık uygulama: bilinmeyen yol arayüze düşer; bilinmeyen API 404.
	resp, _ = e.client.Get(e.srv.URL + "/herhangi/bir/yol")
	if resp.StatusCode != 200 {
		t.Errorf("SPA yedeği: %d", resp.StatusCode)
	}
	resp, _ = e.client.Get(e.srv.URL + "/api/yok")
	if resp.StatusCode != 404 {
		t.Errorf("bilinmeyen API: %d", resp.StatusCode)
	}

	// Çıkış sonrası erişim yok.
	e.mustDo("POST", "/api/auth/logout", nil, nil, 200)
	e.mustDo("GET", "/api/summary", nil, nil, 401)

	// Hatalı giriş sınırı: 5 hatadan sonra doğru şifre de 429 alır.
	for i := 0; i < 5; i++ {
		e.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "yanlis-sifre"}, nil, 401)
	}
	e.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 429)
}

func TestLoginAndPasswordChange(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	// İkinci cihaz.
	other := &env{t: t, srv: e.srv, client: &http.Client{}}
	jar, _ := cookiejar.New(nil)
	other.client.Jar = jar
	other.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	other.mustDo("GET", "/api/summary", nil, nil, 200)

	e.mustDo("POST", "/api/auth/password", map[string]string{"current": "yanlis", "new": "yeni-uzun-sifre"}, nil, 400)
	e.mustDo("POST", "/api/auth/password", map[string]string{"current": "cok-gizli-sifre", "new": "yeni-uzun-sifre"}, nil, 200)

	// Şifreyi değiştiren oturum açık kalır, diğer cihaz düşer.
	e.mustDo("GET", "/api/summary", nil, nil, 200)
	other.mustDo("GET", "/api/summary", nil, nil, 401)
	other.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 401)
	other.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "yeni-uzun-sifre"}, nil, 200)
}

func TestSettings(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var s store.AppSettings
	e.mustDo("GET", "/api/settings", nil, &s, 200)
	if s.RetentionRawDays != 14 || len(s.CertDays) != 5 {
		t.Fatalf("varsayılan ayarlar yanlış: %+v", s)
	}
	e.mustDo("PUT", "/api/settings", map[string]any{"retention_raw_days": 30, "retention_hourly_days": 10, "cert_days": []int{7}, "backup_keep": 7}, nil, 400)
	e.mustDo("PUT", "/api/settings", map[string]any{"retention_raw_days": 30, "retention_hourly_days": 365, "cert_days": []int{3, 30, 7, 7}, "backup_keep": 3}, &s, 200)
	if s.RetentionRawDays != 30 || fmt.Sprint(s.CertDays) != "[30 7 3]" {
		t.Fatalf("ayarlar kaydedilmedi: %+v", s)
	}
}

func TestEventsStream(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer site.Close()

	req, _ := http.NewRequest("GET", e.srv.URL+"/api/events", nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := e.client.Do(req.WithContext(ctx))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("SSE başlığı yanlış: %s", resp.Header.Get("Content-Type"))
	}

	e.mustDo("POST", "/api/monitors", map[string]any{"name": "S", "type": "http", "interval": 50, "config": map[string]any{"url": site.URL}}, nil, 201)

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var ev struct {
				Type string
				Data map[string]any
			}
			json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
			if ev.Type == "beat" && ev.Data["status"] == float64(store.StatusUp) {
				return
			}
		}
	}
	t.Fatal("canlı olay gelmedi")
}

// Üretimdeki gibi okuma/yazma süre sınırı olan sunucuda SSE bağlantısı
// sınırdan uzun süre açık kalmalı.
func TestEventsSurviveServerTimeouts(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	defer st.Close()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Millisecond})
	s := New(st, eng, hub, disp, log, fstest.MapFS{}, "test")

	srv := httptest.NewUnstartedServer(s.Handler())
	srv.Config.ReadTimeout = 300 * time.Millisecond
	srv.Config.WriteTimeout = 300 * time.Millisecond
	srv.Start()
	defer srv.Close()

	jar, _ := cookiejar.New(nil)
	e := &env{t: t, srv: srv, client: &http.Client{Jar: jar}}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	resp, err := e.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	time.Sleep(time.Second) // sınırların 3 katı
	hub.Publish("beat", map[string]int{"monitor_id": 42})

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if strings.Contains(sc.Text(), `"monitor_id":42`) {
			return
		}
	}
	t.Fatalf("bağlantı süre sınırında koptu: %v", sc.Err())
}

func TestClientIP(t *testing.T) {
	cases := []struct {
		name, remote, xff, cf, want string
	}{
		{"Cloudflare → Traefik: CF başlığına güvenilir", "10.0.1.7:5000", "1.2.3.4, 172.70.1.1", "5.6.7.8", "5.6.7.8"},
		{"doğrudan bağlanan uydurma CF başlığı yok sayılır", "10.0.1.7:5000", "9.9.9.9", "5.6.7.8", "9.9.9.9"},
		{"uydurma ilk XFF değeri yok sayılır", "10.0.1.7:5000", "1.1.1.1, 9.9.9.9", "", "9.9.9.9"},
		{"proxy'siz doğrudan bağlantı", "9.9.9.9:5000", "1.1.1.1", "5.6.7.8", "9.9.9.9"},
		{"Cloudflare IPv6 kenar sunucusu", "10.0.1.7:5000", "2606:4700::1", "2a02::1", "2a02::1"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tc.remote
		if tc.xff != "" {
			r.Header.Set("X-Forwarded-For", tc.xff)
		}
		if tc.cf != "" {
			r.Header.Set("CF-Connecting-IP", tc.cf)
		}
		if got := clientIP(r); got != tc.want {
			t.Errorf("%s: %s, %s bekleniyordu", tc.name, got, tc.want)
		}
	}
}

func TestLoginLimiterPerUser(t *testing.T) {
	l := newLoginLimiter()
	now := time.Now()
	// Farklı IP'lerden dağıtık deneme: IP başına 4 hata (sınırın altında).
	for i := 0; i < loginMaxPerUser; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/4, i%4)
		if ok, _ := l.allow(ip, "kadir", now); !ok {
			t.Fatalf("%d. denemede erken kilit", i)
		}
		l.fail(ip, "kadir", now)
	}
	if ok, _ := l.allow("99.99.99.99", "Kadir", now); ok {
		t.Error("kullanıcı adı sınırı aşılınca yeni IP'den de kilitli olmalı")
	}
	if ok, _ := l.allow("99.99.99.99", "baska", now); !ok {
		t.Error("başka kullanıcı adı etkilenmemeli (genel kilit yok)")
	}
	if ok, _ := l.allow("99.99.99.99", "kadir", now.Add(loginLockoutTime+time.Second)); !ok {
		t.Error("kilit süre sonunda açılmalı")
	}
}

func TestReviewAPIEdges(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	// Var olmayan monitörün grafik verisi ve olayları 404.
	e.mustDo("GET", "/api/monitors/999/series", nil, nil, 404)
	e.mustDo("GET", "/api/monitors/999/incidents", nil, nil, 404)

	// Boş SSL eşik listesi null değil [] dönmeli; saatlik özet 90 günden az olamaz.
	var raw map[string]json.RawMessage
	e.mustDo("PUT", "/api/settings", map[string]any{"retention_raw_days": 14, "retention_hourly_days": 365, "cert_days": []int{}, "backup_keep": 7}, &raw, 200)
	if string(raw["cert_days"]) != "[]" {
		t.Errorf("cert_days = %s, [] bekleniyordu", raw["cert_days"])
	}
	e.mustDo("PUT", "/api/settings", map[string]any{"retention_raw_days": 14, "retention_hourly_days": 30, "cert_days": []int{7}, "backup_keep": 7}, nil, 400)

	// Push mesajı çok baytlı harfin ortasından kesilmemeli.
	var mon monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "p", "type": "push", "interval": 5000, "config": map[string]any{}}, &mon, 201)
	long := strings.Repeat("ğ", 300)
	resp, err := http.Get(e.srv.URL + "/api/push/" + mon.PushToken + "?msg=" + long)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("push: %v", err)
	}
	waitFor(t, "push mesajı", func() bool {
		var d struct{ Monitor monitorView }
		e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", mon.ID), nil, &d, 200)
		return d.Monitor.LastMessage != ""
	})
	var d struct{ Monitor monitorView }
	e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", mon.ID), nil, &d, 200)
	if d.Monitor.LastMessage != strings.Repeat("ğ", 250) {
		t.Errorf("mesaj 250 harfe düzgün kısaltılmalı: %d rune, geçerli UTF-8=%v", len([]rune(d.Monitor.LastMessage)), utf8.ValidString(d.Monitor.LastMessage))
	}
}
