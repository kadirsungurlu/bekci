package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// langGet oturumlu GET isteği; lang boş değilse X-Uptime-Lang gönderilir.
func (e *env) langGet(lang, path string, out any) {
	e.t.Helper()
	req, _ := http.NewRequest("GET", e.srv.URL+path, nil)
	req.Header.Set("X-Uptime", "1")
	if lang != "" {
		req.Header.Set("X-Uptime-Lang", lang)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		e.t.Fatalf("GET %s: %d %s", path, resp.StatusCode, data)
	}
	if err := json.Unmarshal(data, out); err != nil {
		e.t.Fatalf("GET %s: %v: %s", path, err, data)
	}
}

// TestCheckMessageLocalization kontrol mesajları Türkçe saklanır ve okuma
// anında isteğin diline çevrilir: monitör listesi/detayı, kontrol geçmişi,
// olay listesi/detayı, konum durumu ve canlı akış.
func TestCheckMessageLocalization(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	// Kapalı port: "Bağlantı reddedildi".
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	var mon monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Kapalı", "type": "tcp", "interval": 30, "retry_interval": 30, "max_retries": 0, "timeout": 200,
		"config": map[string]any{"host": "127.0.0.1", "port": port},
	}, &mon, 201)
	const tr, en = "Bağlantı reddedildi", "Connection refused"

	get := func(lang string) monitorView {
		var d struct{ Monitor monitorView }
		e.langGet(lang, fmt.Sprintf("/api/monitors/%d", mon.ID), &d)
		return d.Monitor
	}
	waitFor(t, "DOWN", func() bool { m := get(""); return m.Status == store.StatusDown && m.LastMessage == tr })

	// Monitör detayı ve listesi.
	if m := get("en"); m.LastMessage != en {
		t.Errorf("detay en: %q", m.LastMessage)
	}
	if m := get("tr"); m.LastMessage != tr {
		t.Errorf("detay tr: %q", m.LastMessage)
	}
	var list []monitorView
	e.langGet("en", "/api/monitors", &list)
	if len(list) != 1 || list[0].LastMessage != en {
		t.Errorf("liste en: %+v", list)
	}

	// Kontrol geçmişi (24 saat, ham).
	var series struct {
		Points []struct {
			S int
			M string
		}
	}
	e.langGet("en", fmt.Sprintf("/api/monitors/%d/series", mon.ID), &series)
	found := false
	for _, p := range series.Points {
		if p.S == store.StatusDown {
			found = true
			if p.M != en {
				t.Errorf("geçmiş en: %q", p.M)
			}
		}
	}
	if !found {
		t.Error("geçmişte DOWN kaydı yok")
	}

	// Olaylar: liste, monitörün olayları, ayrıntı + işlem geçmişi.
	var incs []store.Incident
	e.langGet("en", "/api/incidents", &incs)
	if len(incs) == 0 || incs[0].Cause != en {
		t.Fatalf("olay listesi en: %+v", incs)
	}
	var monIncs []store.Incident
	e.langGet("tr", fmt.Sprintf("/api/monitors/%d/incidents", mon.ID), &monIncs)
	if len(monIncs) == 0 || monIncs[0].Cause != tr {
		t.Errorf("monitör olayları tr: %+v", monIncs)
	}
	var det incidentDetailView
	e.langGet("en", fmt.Sprintf("/api/incidents/%d", incs[0].ID), &det)
	if det.Incident.Cause != en {
		t.Errorf("olay ayrıntısı en: %q", det.Incident.Cause)
	}
	for _, ev := range det.Events {
		if ev.Kind == store.EventDown && ev.Message != en {
			t.Errorf("işlem geçmişi en: %+v", ev)
		}
		if strings.ContainsAny(ev.Message, "çğıöşüÇĞİÖŞÜ") {
			t.Errorf("işlem geçmişinde Türkçe metin: %+v", ev)
		}
	}

	// Canlı akış: EventSource başlık gönderemez, ?lang= ile.
	for _, c := range []struct{ lang, want string }{{"en", en}, {"tr", tr}} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/events?lang="+c.lang, nil)
		resp, err := e.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		got := ""
		sc := bufio.NewScanner(resp.Body)
		for got == "" && sc.Scan() {
			line, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev struct {
				Type string
				Data map[string]any
			}
			json.Unmarshal([]byte(line), &ev)
			if ev.Type == "beat" && ev.Data["monitor_id"] == float64(mon.ID) {
				got, _ = ev.Data["message"].(string)
			}
		}
		resp.Body.Close()
		cancel()
		if got != c.want {
			t.Errorf("canlı akış %s: %q, %q bekleniyordu", c.lang, got, c.want)
		}
	}

	// Çok konumlu: ana sunucu + sonuç göndermeyen bir kontrol noktası. Birleşik
	// mesaj parça parça çevrilir; ana sunucunun konum adı da çevrilir.
	p := e.newProbe("Paris")
	e.setLocations(mon.ID, map[string]any{"include_local": true, "probe_ids": []int64{p.Probe.ID}, "down_when": "any"}, 200)
	const trMulti = "Ana sunucu: Bağlantı reddedildi (sonuç gelmeyen: Paris)"
	waitFor(t, "çok konumlu mesaj", func() bool { return get("").LastMessage == trMulti })
	if m := get("en"); m.LastMessage != "Main server: Connection refused (no results from: Paris)" {
		t.Errorf("çok konumlu en: %q", m.LastMessage)
	}
	var locs locationsView
	waitFor(t, "konum görüntüsü", func() bool {
		e.langGet("en", fmt.Sprintf("/api/monitors/%d/locations", mon.ID), &locs)
		return len(locs.Locations) == 2 && locs.Locations[0].LastCheckAt != 0
	})
	if l := locs.Locations[0]; l.Name != "Main server" || l.Message != en {
		t.Errorf("konumlar en: %+v", locs.Locations)
	}
	e.langGet("tr", fmt.Sprintf("/api/monitors/%d/locations", mon.ID), &locs)
	if l := locs.Locations[0]; l.Name != engine.LocalName || l.Message != tr {
		t.Errorf("konumlar tr: %+v", locs.Locations)
	}

	// Kullanıcı tercihi başlıktan önce gelir.
	e.mustDo("PUT", "/api/auth/preferences", map[string]any{"lang": "en"}, nil, 200)
	if m := get("tr"); !strings.HasPrefix(m.LastMessage, "Main server: Connection refused") {
		t.Errorf("kullanıcı tercihi en iken: %q", m.LastMessage)
	}
}

// TestLocalizeEvent canlı akış çevirisi: yalnızca beat mesajı ve sunucu notu,
// tr bağlantıda bayt bayt aynı olay.
func TestLocalizeEvent(t *testing.T) {
	beat := []byte(`{"data":{"message":"Bakımda (Zaman aşımı)","monitor_id":3,"status":3},"type":"beat"}`)
	if out := localizeEvent("tr", beat); string(out) != string(beat) {
		t.Errorf("tr değişmemeli: %s", out)
	}
	out := localizeEvent("en", beat)
	var ev struct {
		Type string
		Data map[string]any
	}
	if err := json.Unmarshal(out, &ev); err != nil || ev.Type != "beat" || ev.Data["message"] != "In maintenance (Timeout)" || ev.Data["monitor_id"] != float64(3) {
		t.Errorf("beat en: %s", out)
	}
	server := []byte(`{"data":{"id":1,"note":"cpu süreleri boş","state":"unavailable"},"type":"server"}`)
	if out := localizeEvent("en", server); !strings.Contains(string(out), `"note":"cpu times are empty"`) {
		t.Errorf("server en: %s", out)
	}
	other := []byte(`{"data":{"monitor_id":1},"type":"stats_reset"}`)
	if out := localizeEvent("en", other); string(out) != string(other) {
		t.Errorf("diğer olay değişmemeli: %s", out)
	}
	unknown := []byte(`{"data":{"message":"200 OK"},"type":"beat"}`)
	if out := localizeEvent("en", unknown); string(out) != string(unknown) {
		t.Errorf("çevrilmeyen mesaj değişmemeli: %s", out)
	}
}

// BenchmarkListMonitorsLang 500 monitörlük liste uç noktası: tr (çeviri yok)
// ve en (okuma anında çeviri) karşılaştırması.
func BenchmarkListMonitorsLang(b *testing.B) {
	st := storetest.Open(b, time.UTC)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Millisecond, BaseURL: "https://uptime.test"})
	ctx, cancel := context.WithCancel(context.Background())
	if err := eng.Start(ctx); err != nil {
		b.Fatal(err)
	}
	s := New(st, eng, hub, disp, log, fstest.MapFS{"index.html": {Data: []byte("x")}}, "test")
	srv := httptest.NewServer(s.Handler())
	defer func() {
		srv.Close()
		cancel()
		eng.Wait()
		disp.Wait(time.Second)
	}()

	msgs := []string{
		"Bağlantı reddedildi", "Zaman aşımı", `Kelime bulunamadı: "ok"`, "HTTP 503 Service Unavailable",
		"Sorgu çalıştırılamadı: dial tcp 10.0.0.5:5432: connect: connection refused",
		"Ana sunucu: Zaman aşımı; Paris: Bağlantı reddedildi (sonuç gelmeyen: Oslo)",
		"Bakımda (Port açık)", "200 OK", "3/3 paket", "Sertifika geçerli, bitiş: 2027-01-01",
	}
	now := time.Now().Unix()
	for i := range 500 {
		m := store.Monitor{Name: fmt.Sprintf("m%d", i), Type: "tcp", Active: false, Interval: 60, RetryInterval: 60, Timeout: 10,
			Config: json.RawMessage(`{"host":"127.0.0.1","port":1}`)}
		if err := st.CreateMonitor(ctx, &m, nil); err != nil {
			b.Fatal(err)
		}
		if err := st.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: m.ID, Time: now, Status: store.StatusDown, PingMs: -1, Message: msgs[i%len(msgs)]}}); err != nil {
			b.Fatal(err)
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	body := strings.NewReader(`{"username":"kadir","password":"cok-gizli-sifre"}`)
	req, _ := http.NewRequest("POST", srv.URL+"/api/auth/setup", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	if resp, err := client.Do(req); err != nil || resp.StatusCode != 200 {
		b.Fatalf("kurulum: %v", err)
	}
	list := func(lang string) []byte {
		req, _ := http.NewRequest("GET", srv.URL+"/api/monitors", nil)
		req.Header.Set("X-Uptime", "1")
		req.Header.Set("X-Uptime-Lang", lang)
		resp, err := client.Do(req)
		if err != nil {
			b.Fatal(err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			b.Fatalf("liste: %d", resp.StatusCode)
		}
		return data
	}
	if !strings.Contains(string(list("en")), "Connection refused") {
		b.Fatal("liste çevrilmedi")
	}
	for _, lang := range []string{"tr", "en"} {
		b.Run(lang, func(b *testing.B) {
			for b.Loop() {
				list(lang)
			}
		})
	}
}
