package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// newServersEnv newFeatureEnv'in PostgreSQL'de de çalışan hali (storetest).
func newServersEnv(t *testing.T) *fenv {
	t.Helper()
	st := storetest.Open(t, time.UTC)
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
	clk := &testClock{}
	s.now = clk.now
	srv := httptest.NewServer(s.Handler())
	jar, _ := cookiejar.New(nil)
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
		cancel()
		eng.Wait()
		disp.Wait(time.Second)
	})
	e := &env{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return &fenv{env: e, s: s, st: st, clk: clk}
}

type serverSetup struct {
	createdProbe
	DockerAgent string `json:"docker_agent"`
	Systemd     string `json:"systemd"`
	Windows     string `json:"windows"`
}

type serverDetailView struct {
	servers.View
	Alerts          []store.ServerAlert `json:"alerts"`
	NotificationIDs []int64             `json:"notification_ids"`
}

func testSample(cpu float64) metrics.Sample {
	return metrics.Sample{
		Time: time.Now().UnixMilli(),
		Host: &metrics.Host{Hostname: "cp1", OS: "linux", Platform: "ubuntu 24.04", Threads: 4, MemTotal: 1000},
		Stats: &metrics.Stats{
			CPU: cpu, Load1: 0.5, MemTotal: 1000, MemUsed: 400, Uptime: 3600,
			Disks:      []metrics.Disk{{Mount: "/", Total: 100, Used: 71}},
			Temps:      []metrics.Temp{{Name: "cpu", C: 54.5}, {Name: "nvme", C: 40}},
			Containers: []metrics.Container{{ID: "abc", Name: "web", CPU: 1.2, Mem: 100}},
		},
	}
}

func TestServersAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	hook := store.Notification{Name: "Varsayılan kanal", Type: "webhook", Config: json.RawMessage(`{"url":"http://127.0.0.1:1/x","method":"POST"}`), IsDefault: true, Active: true}
	if err := f.st.CreateNotification(ctx, &hook, false); err != nil {
		t.Fatal(err)
	}
	other := store.Notification{Name: "Diğer", Type: "webhook", Config: json.RawMessage(`{"url":"http://127.0.0.1:1/y","method":"POST"}`), Active: true}
	f.st.CreateNotification(ctx, &other, false)

	// Sunucu ekleme: host'u gören ajan ve systemd komutları; kontrol noktası
	// komutu verilmez. Windows komutu için sahte Windows programı.
	f.s.AgentDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(f.s.AgentDir, "uptime-windows-amd64.exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	var cp serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "CP Server İstanbul"}, &cp, 201)
	if cp.DockerCommand != "" || cp.Probe.Kind != store.ProbeKindServer {
		t.Fatalf("sunucuya kontrol noktası komutu verilmemeli: %+v", cp)
	}
	// Ayrım: sunucu kontrol noktası listesinde görünmez, konum olarak atanamaz;
	// kontrol noktası da sunucu listesinde/ekranında görünmez.
	loc := admin.newProbe("Frankfurt")
	if loc.DockerCommand == "" {
		t.Fatal("kontrol noktasına docker_command verilmeli")
	}
	var locs []probeAdminView
	admin.mustDo("GET", "/api/probes", nil, &locs, 200)
	if len(locs) != 1 || locs[0].ID != loc.Probe.ID || locs[0].Kind != store.ProbeKindLocation {
		t.Fatalf("kontrol noktası listesi: %+v", locs)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", loc.Probe.ID), nil, nil, 404)
	lm := store.Monitor{Name: "Site", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 10, Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := f.st.CreateMonitor(ctx, &lm, nil); err != nil {
		t.Fatal(err)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/locations", lm.ID),
		map[string]any{"include_local": true, "probe_ids": []int64{cp.Probe.ID}, "down_when": "any"}, nil, 400)
	// Sunucu token'ıyla kontrol sonucu gönderilemez.
	if code := probeCall(t, admin.srv.URL, cp.Token, "POST", "/api/probe/results",
		map[string]any{"sent_at": 1, "results": []map[string]any{{"monitor_id": lm.ID, "time": 1, "up": true}}}); code != 403 {
		t.Fatalf("sunucu token'ıyla sonuç: %d", code)
	}
	for _, c := range []string{cp.DockerAgent, cp.Systemd} {
		if !strings.Contains(c, cp.Token) || !strings.Contains(c, admin.srv.URL) {
			t.Fatalf("kurulum komutu token/adres içermeli: %s", c)
		}
	}
	if !strings.Contains(cp.DockerAgent, "--pid host") || !strings.Contains(cp.DockerAgent, "HOST_PROC=/host/proc") || !strings.Contains(cp.DockerAgent, "ADDR=-") ||
		!strings.Contains(cp.DockerAgent, "--cap-drop ALL") || !strings.Contains(cp.DockerAgent, "-v uptime-agent-bin:/opt/uptime") || !strings.Contains(cp.DockerAgent, "sha256sum -c -") ||
		!strings.Contains(cp.DockerAgent, "--env-file /etc/uptime-agent.env") || strings.Contains(cp.DockerAgent, "-e PROBE_TOKEN") {
		t.Fatalf("docker ajan komutu: %s", cp.DockerAgent)
	}
	if !strings.Contains(cp.Systemd, "/etc/systemd/system/uptime-agent.service") || !strings.Contains(cp.Systemd, "EnvironmentFile=/etc/uptime-agent.env") || !strings.Contains(cp.Systemd, "install -m 600 /dev/null /etc/uptime-agent.env") ||
		!strings.Contains(cp.Systemd, "sha256sum -c -") || !strings.Contains(cp.Systemd, "NoNewPrivileges=yes") ||
		!strings.Contains(cp.Systemd, "systemctl enable uptime-agent") || !strings.Contains(cp.Systemd, "curl") || !strings.Contains(cp.Systemd, "wget") {
		t.Fatalf("systemd komutu: %s", cp.Systemd)
	}
	if !strings.Contains(cp.Windows, "$env:PROBE_TOKEN='"+cp.Token+"'") || !strings.Contains(cp.Windows, "$env:PROBE_SERVER='"+admin.srv.URL+"'") ||
		!strings.Contains(cp.Windows, "/api/probe/binary?os=windows&arch=amd64") || !strings.Contains(cp.Windows, "service install") ||
		!strings.Contains(cp.Windows, "Tls12") || !strings.Contains(cp.Windows, "Remove-Item Env:PROBE_TOKEN") {
		t.Fatalf("windows komutu: %s", cp.Windows)
	}
	id := cp.Probe.ID
	if !cp.Probe.Metrics {
		t.Fatal("yeni ajanda metrik açık olmalı")
	}
	// Varsayılan kanal eklemede seçili gelir.
	if ids, _ := f.st.ProbeNotificationIDs(ctx, id); len(ids) != 1 || ids[0] != hook.ID {
		t.Fatalf("varsayılan kanal: %v", ids)
	}

	hdr := bearer(cp.Token)
	var jobs struct {
		MetricsInterval *int `json:"metrics_interval"`
	}
	code, _, body := admin.rawReq("GET", "/api/probe/jobs", hdr, nil)
	json.Unmarshal(body, &jobs)
	if code != 200 || jobs.MetricsInterval == nil || *jobs.MetricsInterval != 60 {
		t.Fatalf("iş listesi: %d %s", code, body)
	}

	var list struct {
		Servers []servers.View `json:"servers"`
	}
	admin.mustDo("GET", "/api/servers", nil, &list, 200)
	if len(list.Servers) != 1 || list.Servers[0].State != servers.StateWaiting || list.Servers[0].Latest != nil || list.Servers[0].Firing == nil {
		t.Fatalf("örnek öncesi: %+v", list.Servers)
	}

	// Örnek gönderimi: yalnızca geçerli token, CSRF başlığı gerekmez.
	if code, _, _ := admin.rawReq("POST", "/api/probe/metrics", bearer("upr_gecersiz"), testSample(10)); code != 401 {
		t.Fatalf("geçersiz token: %d", code)
	}
	if code, _, body := admin.rawReq("POST", "/api/probe/metrics", hdr, map[string]any{"time": 1}); code != 400 {
		t.Fatalf("boş örnek: %d %s", code, body)
	}
	big := map[string]any{"time": 1, "unavailable": strings.Repeat("x", metrics.MaxBodyBytes)}
	if code, _, _ := admin.rawReq("POST", "/api/probe/metrics", hdr, big); code != 413 {
		t.Fatalf("büyük gövde: %d", code)
	}
	if code, _, body := admin.rawReq("POST", "/api/probe/metrics", hdr, testSample(12.5)); code != 204 {
		t.Fatalf("örnek: %d %s", code, body)
	}

	admin.mustDo("GET", "/api/servers", nil, &list, 200)
	v := list.Servers[0]
	if v.State != servers.StateOnline || v.Latest == nil || v.Latest.CPU != 12.5 || v.Host == nil || v.Host.Hostname != "cp1" ||
		v.ContainerCount != 1 || v.TempMax == nil || *v.TempMax != 54.5 || v.Interval != 60 || v.MetricsAt == 0 || v.LastSeenAt == 0 {
		t.Fatalf("liste: %+v", v)
	}
	if len(v.Latest.Containers) != 0 || len(v.Latest.Temps) != 0 || len(v.Latest.Disks) != 1 {
		t.Fatalf("listede konteyner/sıcaklık olmamalı: %+v", v.Latest)
	}

	var detail serverDetailView
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &detail, 200)
	if len(detail.Latest.Containers) != 1 || len(detail.Latest.Temps) != 2 || len(detail.Alerts) != 4 ||
		len(detail.NotificationIDs) != 1 || detail.NotificationIDs[0] != hook.ID {
		t.Fatalf("detay: %+v", detail)
	}
	admin.mustDo("GET", "/api/servers/999", nil, nil, 404)

	var series servers.Series
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d/stats?range=1h", id), nil, &series, 200)
	if series.Range != "1h" || series.Res != 1 || len(series.Points) != 1 || series.Points[0].CPU != 12.5 ||
		series.Points[0].DiskPct != 71 || series.Points[0].Temp == nil || *series.Points[0].Temp != 54.5 ||
		len(series.Points[0].Containers) != 1 || series.Points[0].Containers[0].Name != "web" {
		t.Fatalf("seri: %+v", series)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d/stats?range=2y", id), nil, nil, 400)
	var events struct {
		Events []map[string]any `json:"events"`
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d/events", id), nil, &events, 200)
	if events.Events == nil || len(events.Events) != 0 {
		t.Fatalf("geçmiş: %+v", events)
	}

	// Kural doğrulama.
	alerts := fmt.Sprintf("/api/servers/%d/alerts", id)
	bad := [][]map[string]any{
		{{"metric": "cpu", "threshold": 90, "minutes": 10}, {"metric": "cpu", "threshold": 80, "minutes": 5}},
		{{"metric": "gpu", "threshold": 90, "minutes": 10}},
		{{"metric": "cpu", "threshold": 150, "minutes": 10}},
		{{"metric": "cpu", "threshold": 0, "minutes": 10}},
		{{"metric": "mem", "threshold": 90, "minutes": 0}},
		{{"metric": "disk", "threshold": 90, "minutes": 61}},
		{{"metric": "temp", "threshold": 500, "minutes": 5}},
	}
	for _, b := range bad {
		admin.mustDo("PUT", alerts, map[string]any{"alerts": b}, nil, 400)
	}
	var saved struct {
		Alerts []store.ServerAlert `json:"alerts"`
	}
	admin.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{
		{"metric": "offline", "threshold": 55, "minutes": 5, "active": true},
		{"metric": "cpu", "threshold": 80, "minutes": 5},
		{"metric": "load", "threshold": 1.5, "minutes": 15, "active": false},
	}}, &saved, 200)
	if len(saved.Alerts) != 3 {
		t.Fatalf("kurallar: %+v", saved)
	}
	byMetric := map[string]store.ServerAlert{}
	for _, a := range saved.Alerts {
		byMetric[a.Metric] = a
	}
	if byMetric["offline"].Threshold != 0 || byMetric["offline"].Minutes != 5 || byMetric["cpu"].Threshold != 80 || !byMetric["cpu"].Active ||
		byMetric["load"].Active || byMetric["load"].Threshold != 1.5 {
		t.Fatalf("kurallar: %+v", byMetric)
	}

	// Kanal seçimi.
	notifs := fmt.Sprintf("/api/servers/%d/notifications", id)
	admin.mustDo("PUT", notifs, map[string]any{"notification_ids": []int64{999}}, nil, 400)
	var nids struct {
		NotificationIDs []int64 `json:"notification_ids"`
	}
	admin.mustDo("PUT", notifs, map[string]any{"notification_ids": []int64{other.ID, hook.ID, other.ID}}, &nids, 200)
	if len(nids.NotificationIDs) != 2 {
		t.Fatalf("kanallar: %+v", nids)
	}
	chans, _ := f.st.NotificationsForProbe(ctx, id)
	if len(chans) != 2 {
		t.Fatalf("ajan kanalları: %+v", chans)
	}
	audit, _ := f.st.ListAudit(ctx, 0, 20)
	var sawAlerts, sawNotifs bool
	for _, a := range audit {
		sawAlerts = sawAlerts || a.Action == "server.alerts" && strings.Contains(a.Detail, "cpu %80/5 dk") && a.TargetName == "CP Server İstanbul"
		sawNotifs = sawNotifs || a.Action == "server.notifications" && strings.Contains(a.Detail, "Diğer")
	}
	if !sawAlerts || !sawNotifs {
		t.Fatalf("işlem kaydı: %+v", audit)
	}

	// Yetkiler: izleyici okur ama değiştiremez (kanal bağlantılarını görmez);
	// müşteri kısıtlı izleyici hiç göremez.
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	viewer.mustDo("GET", "/api/servers", nil, nil, 200)
	var vd serverDetailView
	viewer.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &vd, 200)
	if len(vd.NotificationIDs) != 0 || len(vd.Alerts) != 3 {
		t.Fatalf("izleyici detayı: %+v", vd)
	}
	viewer.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{}}, nil, 403)
	viewer.mustDo("PUT", notifs, map[string]any{"notification_ids": []int64{}}, nil, 403)
	editor, _ := admin.newUser("editor", store.RoleEditor, nil)
	editor.mustDo("PUT", notifs, map[string]any{"notification_ids": []int64{hook.ID}}, nil, 200)
	editor.mustDo("PUT", fmt.Sprintf("/api/probes/%d", id), map[string]any{"name": "x", "metrics": false}, nil, 403)
	mon := admin.push("Müşteri sitesi")
	customer, _ := admin.newUser("musteri", store.RoleViewer, []int64{mon.ID})
	for _, p := range []string{"/api/servers", fmt.Sprintf("/api/servers/%d", id), fmt.Sprintf("/api/servers/%d/stats?range=1h", id),
		fmt.Sprintf("/api/servers/%d/events", id)} {
		customer.mustDo("GET", p, nil, nil, 403)
	}

	// Sunucu atanmış müşteri: yalnızca kendi sunucusunu görür, başkasınınki
	// "bulunamadı"; değiştiremez.
	var own serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Müşteri sunucusu"}, &own, 201)
	var cu store.User
	admin.mustDo("POST", "/api/users", map[string]any{"username": "musteri2", "role": "viewer", "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{mon.ID}, "server_ids": []int64{own.Probe.ID}}, &cu, 201)
	if len(cu.ServerIDs) != 1 || cu.ServerIDs[0] != own.Probe.ID {
		t.Fatalf("atanan sunucular: %+v", cu.ServerIDs)
	}
	admin.mustDo("POST", "/api/users", map[string]any{"username": "musteri3", "role": "viewer", "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{mon.ID}, "server_ids": []int64{loc.Probe.ID}}, nil, 400) // kontrol noktası atanamaz
	c2 := admin.loginAs("musteri2", "gecici-sifre-1")
	c2.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	var me struct {
		User struct {
			Servers bool `json:"servers"`
		} `json:"user"`
	}
	c2.mustDo("GET", "/api/auth/state", nil, &me, 200)
	if !me.User.Servers {
		t.Fatal("sunucu atanmış müşteride Sunucular ekranı açık olmalı")
	}
	var cl struct {
		Servers []servers.View `json:"servers"`
	}
	c2.mustDo("GET", "/api/servers", nil, &cl, 200)
	if len(cl.Servers) != 1 || cl.Servers[0].ID != own.Probe.ID {
		t.Fatalf("müşterinin sunucu listesi: %+v", cl.Servers)
	}
	c2.mustDo("GET", fmt.Sprintf("/api/servers/%d", own.Probe.ID), nil, nil, 200)
	c2.mustDo("GET", fmt.Sprintf("/api/servers/%d/stats?range=1h", own.Probe.ID), nil, nil, 200)
	for _, p := range []string{fmt.Sprintf("/api/servers/%d", id), fmt.Sprintf("/api/servers/%d/stats?range=1h", id),
		fmt.Sprintf("/api/servers/%d/events", id)} {
		c2.mustDo("GET", p, nil, nil, 404)
	}
	c2.mustDo("PUT", fmt.Sprintf("/api/servers/%d/alerts", own.Probe.ID), map[string]any{"alerts": []map[string]any{}}, nil, 403)

	// Metrik kapatma: iş listesinde aralık 0, durum "disabled", örnek yok sayılır.
	var pv probeAdminView
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", id), map[string]any{"name": "CP Server İstanbul", "metrics": false}, &pv, 200)
	if pv.Metrics {
		t.Fatal("metrik kapanmadı")
	}
	_, _, body = admin.rawReq("GET", "/api/probe/jobs", hdr, nil)
	json.Unmarshal(body, &jobs)
	if *jobs.MetricsInterval != 0 {
		t.Fatalf("kapalı ajan aralığı: %s", body)
	}
	if code, _, _ := admin.rawReq("POST", "/api/probe/metrics", hdr, testSample(99)); code != 204 {
		t.Fatalf("kapalıyken örnek: %d", code)
	}
	admin.mustDo("GET", "/api/servers", nil, &list, 200)
	if list.Servers[0].State != servers.StateDisabled || list.Servers[0].Latest.CPU != 12.5 {
		t.Fatalf("kapalı: %+v", list.Servers[0])
	}
	// Ad değişmeden yalnızca metrik değişikliği de işlem kaydına yazılır.
	audit, _ = f.st.ListAudit(ctx, 0, 1)
	if audit[0].Action != "probe.update" || !strings.Contains(audit[0].Detail, "metrik toplama kapatıldı") {
		t.Fatalf("işlem kaydı: %+v", audit[0])
	}

	// Ajan silinince sunucu da gider.
	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", id), nil, nil, 200)
	admin.mustDo("GET", "/api/servers", nil, &list, 200)
	if len(list.Servers) != 1 || list.Servers[0].ID == id {
		t.Fatalf("silinen ajan: %+v", list.Servers)
	}
	// Müşteriye atanmış sunucu silinince atama da gider.
	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", own.Probe.ID), nil, nil, 200)
	if u, _ := f.st.UserByID(ctx, cu.ID); len(u.ServerIDs) != 0 {
		t.Fatalf("silinen sunucu atamada kaldı: %+v", u.ServerIDs)
	}
}

func TestServerEventsHiddenFromRestricted(t *testing.T) {
	msg, _ := json.Marshal(map[string]any{"type": "server", "data": servers.View{ID: 1}})
	if eventVisible(msg, visibility{ids: map[int64]bool{1: true}}) {
		t.Fatal("sunucu olayı, sunucu atanmamış müşteriye gitmemeli (monitör 1 atanmış olsa da)")
	}
	if !eventVisible(msg, visibility{servers: map[int64]bool{1: true}}) {
		t.Fatal("atanmış sunucunun olayı müşteriye gitmeli")
	}
	if eventVisible(msg, visibility{servers: map[int64]bool{2: true}}) {
		t.Fatal("başka sunucunun olayı gitmemeli")
	}
}

// probeCall ajan token'ıyla istek yapar ve durum kodunu döner.
func probeCall(t *testing.T, base, token, method, path string, body any) int {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(method, base+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

// Windows kurulum komutu Windows programının özetini gömmeli (sunucunun
// kendi programının değil).
func TestWindowsCommandUsesWindowsSHA(t *testing.T) {
	dir := t.TempDir()
	exe := []byte("sahte windows programi")
	if err := os.WriteFile(filepath.Join(dir, "uptime-windows-amd64.exe"), exe, 0o644); err != nil {
		t.Fatal(err)
	}
	s := &Server{AgentDir: dir}
	sum := sha256.Sum256(exe)
	want := strings.ToUpper(hex.EncodeToString(sum[:]))
	_, _, win := s.serverSetupCommands("https://uptime.example", "upr_x")
	if !strings.Contains(win, want) {
		t.Fatalf("windows komutunda windows programının özeti yok:\n%s", win)
	}
	if own := strings.ToUpper(s.agentBinarySHA256(runtime.GOOS, runtime.GOARCH)); own != "" && strings.Contains(win, own) {
		t.Fatal("windows komutu sunucunun kendi programının özetini içermemeli")
	}
}
