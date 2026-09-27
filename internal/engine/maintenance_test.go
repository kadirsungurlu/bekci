package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// window fikstürün saatine göre [start, end) tek seferlik bakım penceresi ekler.
func (f *fixture) window(t *testing.T, monitorID int64, start, end time.Time) store.Maintenance {
	t.Helper()
	w := store.Maintenance{Title: "Bakım", Active: true, Strategy: store.MaintOnce, Timezone: "UTC",
		Start: start.UTC().Format("2006-01-02T15:04"), End: end.UTC().Format("2006-01-02T15:04"),
		MonitorIDs: []int64{monitorID}}
	if err := f.st.CreateMaintenance(context.Background(), &w); err != nil {
		t.Fatal(err)
	}
	if err := f.e.ReloadMaintenance(context.Background()); err != nil {
		t.Fatal(err)
	}
	return w
}

func openIncidents(t *testing.T, f *fixture, id int64) int {
	t.Helper()
	list, err := f.st.ListIncidents(context.Background(), store.IncidentFilter{MonitorID: id})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, in := range list {
		if in.ResolvedAt == 0 {
			n++
		}
	}
	return n
}

func TestMaintenanceSuppressesAlerts(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 1; m.ResendEvery = 1 })
	other := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(up()) // bakımdan önce UP (onaylı)

	f.clock = f.clock.Add(time.Minute)
	f.window(t, m.ID, f.clock, f.clock.Add(30*time.Minute))
	if !f.e.InMaintenance(m.ID, f.clock) || f.e.InMaintenance(other.ID, f.clock) {
		t.Fatal("bakım dizini yanlış")
	}
	for i := range 5 {
		f.clock = f.clock.Add(time.Minute)
		r.process(down("502"))
		if r.m.Status != store.StatusMaintenance {
			t.Fatalf("adım %d: durum %d, bakım (3) bekleniyordu", i, r.m.Status)
		}
	}
	if got := f.n.kinds(); len(got) != 0 {
		t.Fatalf("bakımda bildirim gitmemeli: %v", got)
	}
	if n := openIncidents(t, f, m.ID); n != 0 {
		t.Fatalf("bakımda olay açılmamalı: %d", n)
	}
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if got.Status != store.StatusMaintenance || !strings.HasPrefix(got.LastMessage, "Bakımda (502") {
		t.Fatalf("kayıtlı durum yanlış: %d %q", got.Status, got.LastMessage)
	}
	// Bakım kontrolleri uptime'a girmez: sadece bakımdan önceki UP sayılır.
	if pct, ok, _ := f.st.Uptime(ctx, m.ID, f.clock.Add(-2*time.Hour).Unix()); !ok || pct != 100 {
		t.Errorf("uptime bakımdan etkilenmemeli: %v %v", pct, ok)
	}
	beats, _ := f.st.Beats(ctx, m.ID, 0)
	if len(beats) != 6 || beats[5].Status != store.StatusMaintenance {
		t.Errorf("ham kayıtlar bakım durumuyla yazılmalı: %+v", beats)
	}

	// Bakım bitti ama site hâlâ çalışmıyor: normal akış (tekrar deneme → DOWN bildirimi).
	f.clock = f.clock.Add(30 * time.Minute)
	r.process(down("502"))
	if r.m.Status != store.StatusPending {
		t.Fatalf("bakım sonrası ilk hata tekrar denenmeli: %d", r.m.Status)
	}
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	if r.m.Status != store.StatusDown || !equal(f.n.kinds(), []string{notify.KindDown}) {
		t.Fatalf("bakım sonrası DOWN bildirimi bekleniyordu: %d %v", r.m.Status, f.n.kinds())
	}
	if n := openIncidents(t, f, m.ID); n != 1 {
		t.Fatalf("bakım sonrası olay açılmalı: %d", n)
	}
}

// Bakım başladığında açık olan kesinti açık kalır; bakımda UP/DOWN bildirimi
// gitmez, bakım bitince ilk normal kontrol olayı kapatır (UP bildirimi).
func TestMaintenanceWithOpenIncident(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.ResendEvery = 1 })
	r := f.runnerFor(t, m.ID)
	r.process(down("502")) // DOWN bildirimi + olay
	downAt := f.clock

	f.window(t, m.ID, f.clock.Add(time.Minute), f.clock.Add(time.Hour))
	f.clock = f.clock.Add(2 * time.Minute)
	r.process(up())
	r.process(down("502"))
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("bakımda UP/hatırlatma gitmemeli: %v", got)
	}
	if n := openIncidents(t, f, m.ID); n != 1 {
		t.Fatalf("açık olay bakımda açık kalmalı: %d", n)
	}

	f.clock = downAt.Add(2 * time.Hour)
	r.process(up())
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("bakım sonrası UP bildirimi bekleniyordu: %v", got)
	}
	if f.n.events[1].Downtime != 2*time.Hour {
		t.Errorf("kesinti süresi olay başlangıcından: %v", f.n.events[1].Downtime)
	}
	if n := openIncidents(t, f, m.ID); n != 0 {
		t.Fatalf("olay kapanmalı: %d", n)
	}
}

// Bakım sırasında uygulama yeniden başlarsa onaylı durum veritabanından doğru
// çıkarılır: bakım bitince çalışmayan monitör bildirim üretir, çalışan üretmez.
func TestRestartDuringMaintenance(t *testing.T) {
	f := newFixture(t)
	upMon := f.monitor(t, nil)
	downMon := f.monitor(t, nil)
	r1, r2 := f.runnerFor(t, upMon.ID), f.runnerFor(t, downMon.ID)
	r1.process(up())
	r2.process(down("502"))
	if !equal(f.n.kinds(), []string{notify.KindDown}) {
		t.Fatalf("başlangıç bildirimleri yanlış: %v", f.n.kinds())
	}
	start := f.clock.Add(time.Minute)
	f.window(t, upMon.ID, start, start.Add(time.Hour))
	f.window(t, downMon.ID, start, start.Add(time.Hour))
	f.clock = start.Add(time.Minute)
	r1.process(down("bakım"))
	r2.process(up())

	// Yeniden başlatma: kayıtlı durum 3 (bakım).
	r1, r2 = f.runnerFor(t, upMon.ID), f.runnerFor(t, downMon.ID)
	if r1.confirmed != store.StatusUp || r2.confirmed != store.StatusDown {
		t.Fatalf("yeniden başlatmada onaylı durum yanlış: %d %d", r1.confirmed, r2.confirmed)
	}
	f.clock = f.clock.Add(time.Minute)
	r1.process(down("bakım"))
	if r1.m.Status != store.StatusMaintenance || len(f.n.kinds()) != 1 {
		t.Fatalf("yeniden başlatma sonrası da bakım sürmeli: %d %v", r1.m.Status, f.n.kinds())
	}

	f.clock = start.Add(2 * time.Hour)
	r1.process(down("502")) // MaxRetries 0: hemen DOWN
	r2.process(up())
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindDown, notify.KindUp}) {
		t.Fatalf("bakım sonrası bildirimler yanlış: %v", got)
	}
	if f.n.events[1].MonitorID != upMon.ID || f.n.events[2].MonitorID != downMon.ID {
		t.Errorf("bildirim monitörleri yanlış: %+v", f.n.events)
	}
}

// Bakımda push monitörüne gelen DOWN sinyali de bakım olarak kaydedilir.
func TestMaintenanceAllMonitors(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, nil)
	w := store.Maintenance{Title: "Hepsi", Active: true, Strategy: store.MaintManual, Timezone: "UTC", AllMonitors: true}
	if err := f.st.CreateMaintenance(ctx, &w); err != nil {
		t.Fatal(err)
	}
	f.e.ReloadMaintenance(ctx)
	r := f.runnerFor(t, m.ID)
	r.process(down("x"))
	if r.m.Status != store.StatusMaintenance || len(f.n.kinds()) != 0 {
		t.Fatalf("tüm monitörler bakımda olmalı: %d %v", r.m.Status, f.n.kinds())
	}
	// Elle kapatılınca bakım biter.
	f.st.SetMaintenanceActive(ctx, w.ID, false)
	f.e.ReloadMaintenance(ctx)
	r.process(down("x"))
	if r.m.Status != store.StatusDown || !equal(f.n.kinds(), []string{notify.KindDown}) {
		t.Fatalf("bakım kapanınca normal değerlendirme: %d %v", r.m.Status, f.n.kinds())
	}
}

// Grup monitörleri ----------------------------------------------------------------

func (f *fixture) setStatus(t *testing.T, id int64, status int) {
	t.Helper()
	f.clock = f.clock.Add(time.Second)
	if err := f.st.RecordBeat(context.Background(), store.BeatUpdate{
		Beat: store.Beat{MonitorID: id, Time: f.clock.Unix(), Status: status, PingMs: -1},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestGroupMonitor(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.monitor(t, func(m *store.Monitor) { m.Name = "A" })
	b := f.monitor(t, func(m *store.Monitor) { m.Name = "B" })
	c := f.monitor(t, func(m *store.Monitor) { m.Name = "C"; m.Active = false })
	cfg, ok := check.Get(check.TypeGroup)
	if !ok {
		t.Fatal("grup tipi kayıtlı değil")
	}
	raw, verr := cfg.Normalize(json.RawMessage(fmt.Sprintf(`{"monitor_ids":[%d,%d,%d,999]}`, a.ID, b.ID, c.ID)))
	if verr != nil {
		t.Fatal(verr)
	}
	g := f.monitor(t, func(m *store.Monitor) { m.Name = "Grup"; m.Type = check.TypeGroup; m.Config = raw })
	r := f.runnerFor(t, g.ID)
	r.checker = cfg
	if tgt := cfg.Target(raw); tgt != "4 monitör" {
		t.Errorf("hedef yanlış: %q", tgt)
	}
	gctx := check.WithStatusSource(ctx, f.e.monitorStatuses)
	step := func() {
		f.clock = f.clock.Add(time.Minute)
		r.process(r.checker.Check(gctx, r.m.Config))
	}

	// Alt monitörler henüz kontrol edilmedi: bekliyor.
	step()
	if r.m.Status != store.StatusPending || !strings.Contains(r.m.LastMessage, "2 alt monitör bekliyor") {
		t.Fatalf("bekleyen alt monitörler: %d %q", r.m.Status, r.m.LastMessage)
	}
	f.setStatus(t, a.ID, store.StatusUp)
	f.setStatus(t, b.ID, store.StatusUp)
	step()
	if r.m.Status != store.StatusUp || !strings.Contains(r.m.LastMessage, "1 durdurulmuş") {
		t.Fatalf("grup UP olmalı: %d %q", r.m.Status, r.m.LastMessage)
	}
	f.setStatus(t, b.ID, store.StatusDown)
	step()
	if r.m.Status != store.StatusDown || r.m.LastMessage != "1/2 alt monitör çalışmıyor: B (1 durdurulmuş)" {
		t.Fatalf("grup DOWN olmalı: %d %q", r.m.Status, r.m.LastMessage)
	}
	f.setStatus(t, a.ID, store.StatusMaintenance)
	f.setStatus(t, b.ID, store.StatusUp)
	step()
	if r.m.Status != store.StatusUp || !strings.Contains(r.m.LastMessage, "1 bakımda") {
		t.Fatalf("bakımdaki alt monitör DOWN sayılmamalı: %d %q", r.m.Status, r.m.LastMessage)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("grup bildirimleri yanlış: %v", got)
	}
	if ev := f.n.events[0]; ev.MonitorID != g.ID || ev.Target != "4 monitör" || !strings.Contains(ev.Message, "B") {
		t.Errorf("grup DOWN olayı yanlış: %+v", ev)
	}
	if n := openIncidents(t, f, g.ID); n != 0 {
		t.Errorf("grup olayı kapanmalı: %d", n)
	}
}

// Bekleyen (belirsiz) grup sonucu tekrar deneme hakkını harcamaz ve DOWN'a dönmez.
func TestGroupPendingDoesNotEscalate(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 1 })
	r := f.runnerFor(t, m.ID)
	for range 5 {
		r.process(check.Result{Pending: true, PingMs: -1, Message: "bekliyor"})
	}
	if r.m.Status != store.StatusPending || r.retries != 0 || len(f.n.kinds()) != 0 {
		t.Fatalf("bekleyen sonuç DOWN'a dönmemeli: %d %d %v", r.m.Status, r.retries, f.n.kinds())
	}
}

// Motor döngüsünde grup kontrolü context'teki kaynaktan alt monitörleri okur.
func TestGroupLoop(t *testing.T) {
	f := newFixture(t)
	f.e.now = time.Now
	fake.set(up())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a := f.monitor(t, func(m *store.Monitor) { m.Name = "A" })
	b := f.monitor(t, func(m *store.Monitor) { m.Name = "B" })
	raw := json.RawMessage(fmt.Sprintf(`{"monitor_ids":[%d,%d],"mode":"all_down"}`, a.ID, b.ID))
	g := f.monitor(t, func(m *store.Monitor) { m.Type = check.TypeGroup; m.Config = raw; m.Interval = 20 })
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "grup UP", func() bool {
		got, _ := f.st.GetMonitor(ctx, g.ID)
		return got.Status == store.StatusUp && got.LastMessage == "Tüm alt monitörler çalışıyor (2)"
	})
	cancel()
	f.e.Wait()
}
