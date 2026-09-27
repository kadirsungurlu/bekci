package servers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store/storetest"
)

type fakeNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (f *fakeNotifier) Notify(ev notify.Event) {
	f.mu.Lock()
	f.events = append(f.events, ev)
	f.mu.Unlock()
}

func (f *fakeNotifier) take() []notify.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.events
	f.events = nil
	return out
}

type fakeHub struct {
	mu    sync.Mutex
	views []View
}

func (h *fakeHub) Publish(typ string, data any) {
	if typ != "server" {
		return
	}
	h.mu.Lock()
	h.views = append(h.views, data.(View))
	h.mu.Unlock()
}

func (h *fakeHub) last() View {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.views[len(h.views)-1]
}

// testEnv sahte saatli servis. restart aynı veritabanıyla yeni servis açar
// (uygulamanın yeniden başlatılması).
type testEnv struct {
	t     *testing.T
	st    *store.Store
	clock time.Time
	svc   *Service
	notif *fakeNotifier
	hub   *fakeHub
	probe store.Probe
}

func newTestEnv(t *testing.T) *testEnv {
	e := &testEnv{t: t, st: storetest.Open(t, time.UTC), clock: time.Date(2026, 9, 27, 10, 0, 5, 0, time.UTC)}
	e.probe = store.Probe{Kind: store.ProbeKindServer, Name: "CP Server İstanbul", Active: true, CreatedAt: 1, Hash: "h"}
	if err := e.st.CreateProbe(context.Background(), &e.probe); err != nil {
		t.Fatal(err)
	}
	e.restart()
	return e
}

func (e *testEnv) restart() {
	e.notif, e.hub = &fakeNotifier{}, &fakeHub{}
	e.svc = New(e.st, e.hub, e.notif, slog.New(slog.NewTextHandler(io.Discard, nil)))
	e.svc.SetClock(func() time.Time { return e.clock })
	e.svc.SetBaseURL("https://uptime.test")
}

func (e *testEnv) getProbe() store.Probe {
	p, err := e.st.GetProbe(context.Background(), e.probe.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return p
}

// send bir örnek gönderir ve saati bir dakika ileri alır.
func (e *testEnv) send(st metrics.Stats) {
	e.t.Helper()
	smp := metrics.Sample{Time: 1, Host: &metrics.Host{Hostname: "cp1", Threads: 4}, Stats: &st}
	if err := e.svc.Ingest(context.Background(), e.getProbe(), smp); err != nil {
		e.t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Minute)
}

func cpu(v float64) metrics.Stats {
	return metrics.Stats{CPU: v, MemTotal: 100, MemUsed: 10, Disks: []metrics.Disk{{Mount: "/", Total: 100, Used: 10}}}
}

func (e *testEnv) rules() map[string]store.ServerAlert {
	list, err := e.st.ServerAlerts(context.Background(), e.probe.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	out := map[string]store.ServerAlert{}
	for _, a := range list {
		out[a.Metric] = a
	}
	return out
}

func TestIngestAndRollup(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	start := e.clock.Unix() - e.clock.Unix()%60 // 10:00
	if v := e.svc.View(ctx, e.getProbe(), nil, false); v.State != StateWaiting || v.Latest != nil {
		t.Fatalf("örnek yok: %+v", v)
	}
	// 10:00-10:29 (30 örnek): CPU = dakika.
	for i := range 30 {
		e.send(cpu(float64(i)))
	}
	// Uygulama yeniden başlar: 10:30 örneği 10:20 kovasını kapatır; önceki
	// satır bellekte yok, veritabanından bulunur.
	e.restart()
	for i := 30; i < 66; i++ { // 10:30-11:05
		e.send(cpu(float64(i)))
	}
	rows, _ := e.st.ServerStats(ctx, e.probe.ID, store.ServerRes1, 0, start+10_000)
	if len(rows) != 66 || rows[0].Time != start {
		t.Fatalf("1 dk satırları: %d", len(rows))
	}
	tens, _ := e.st.ServerStats(ctx, e.probe.ID, store.ServerRes10, 0, start+10_000)
	if len(tens) != 6 {
		t.Fatalf("10 dk özetleri: %d", len(tens))
	}
	for i, r := range tens {
		var st metrics.Stats
		json.Unmarshal(r.Data, &st)
		want := float64(i*10) + 4.5
		if r.Time != start+int64(i)*600 || st.CPU != want || st.CPUMax != float64(i*10+9) {
			t.Errorf("özet %d: t=%d %+v (cpu %v bekleniyordu)", i, r.Time, st, want)
		}
	}
	hours, _ := e.st.ServerStats(ctx, e.probe.ID, store.ServerRes60, 0, start+10_000)
	if len(hours) != 1 || hours[0].Time != start {
		t.Fatalf("saatlik özet: %+v", hours)
	}
	var h metrics.Stats
	json.Unmarshal(hours[0].Data, &h)
	if h.CPU != 29.5 || h.CPUMax != 59 {
		t.Fatalf("saatlik: %+v", h)
	}

	// Aynı dakikada ikinci örnek öncekinin yerine geçer.
	e.clock = e.clock.Add(-30 * time.Second)
	e.send(cpu(99))
	rows, _ = e.st.ServerStats(ctx, e.probe.ID, store.ServerRes1, 0, start+10_000)
	if len(rows) != 66 {
		t.Fatalf("aynı dakika: %d satır", len(rows))
	}
	v := e.svc.View(ctx, e.getProbe(), nil, false)
	if v.State != StateOnline || v.Latest == nil || v.Latest.CPU != 99 || v.Host == nil || v.Host.Hostname != "cp1" || v.Interval != Interval {
		t.Fatalf("görünüm: %+v", v)
	}
	if last := e.hub.last(); last.ID != e.probe.ID || last.Latest.CPU != 99 {
		t.Fatalf("canlı olay: %+v", last)
	}

	// Seri: 1 saatlik aralık son 60 dakikanın 1 dk satırları (saat 11:06:35;
	// 10:07-11:05 arası 59 satır).
	series, ok, err := e.svc.Series(ctx, e.probe.ID, "1h")
	if err != nil || !ok || series.Res != 1 || series.Interval != 60 || len(series.Points) != 59 || series.To != e.clock.Unix() {
		t.Fatalf("seri: %+v %v %v (%d nokta)", series.Range, ok, err, len(series.Points))
	}
	if p := series.Points[len(series.Points)-1]; p.CPU != 99 || p.CPUMax != 99 || p.DiskPct != 10 || p.Temp != nil || p.MemTotal != 100 {
		t.Fatalf("son nokta: %+v", p)
	}
	if s7, _, _ := e.svc.Series(ctx, e.probe.ID, "7d"); s7.Res != 10 || s7.Interval != 600 || len(s7.Points) != 6 {
		t.Fatalf("7 gün: %+v", s7)
	}
	if _, ok, _ := e.svc.Series(ctx, e.probe.ID, "2d"); ok {
		t.Fatal("geçersiz aralık")
	}

	// Yeniden başlatmadan sonra son örnek veritabanından okunur.
	e.restart()
	if v := e.svc.View(ctx, e.getProbe(), nil, true); v.Latest == nil || v.Latest.CPU != 99 {
		t.Fatalf("açılış sonrası son örnek: %+v", v.Latest)
	}
}

func TestUnavailableAndDisabled(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	if err := e.svc.Ingest(ctx, e.getProbe(), metrics.Sample{}); err != ErrEmptySample {
		t.Fatalf("boş örnek: %v", err)
	}
	st := e.getProbe()
	st.LastSeenAt = e.clock.Unix()
	if err := e.svc.Ingest(ctx, st, metrics.Sample{Unavailable: "host bağlı değil"}); err != nil {
		t.Fatal(err)
	}
	p := e.getProbe()
	p.LastSeenAt = e.clock.Unix()
	if v := e.svc.View(ctx, p, nil, false); v.State != StateUnavailable || v.Note != "host bağlı değil" || v.MetricsAt != 0 {
		t.Fatalf("toplayamıyor: %+v", v)
	}
	if len(e.rules()) != 0 {
		t.Fatal("örnek gelmeden varsayılan kural eklenmemeli")
	}
	e.send(cpu(5))
	if v := e.svc.View(ctx, e.getProbe(), nil, false); v.State != StateOnline || v.Note != "" {
		t.Fatalf("örnek sonrası: %+v", v)
	}
	// Metrik kapalıyken örnek yok sayılır.
	e.st.SetProbeMetrics(ctx, e.probe.ID, false)
	before := e.getProbe().MetricsAt
	e.send(cpu(50))
	if p := e.getProbe(); p.MetricsAt != before || State(p, e.clock) != StateDisabled {
		t.Fatalf("kapalı: %+v", p)
	}
}

func TestState(t *testing.T) {
	now := time.Unix(10_000, 0)
	cases := []struct {
		p    store.Probe
		want string
	}{
		{store.Probe{Active: false, Metrics: true, MetricsAt: 9_990}, StateDisabled},
		{store.Probe{Active: true, Metrics: false, MetricsAt: 9_990}, StateDisabled},
		{store.Probe{Active: true, Metrics: true}, StateWaiting},
		{store.Probe{Active: true, Metrics: true, MetricsAt: 9_990}, StateOnline},
		{store.Probe{Active: true, Metrics: true, MetricsAt: 10_000 - 180}, StateOnline},
		{store.Probe{Active: true, Metrics: true, MetricsAt: 10_000 - 181}, StateOffline},
		{store.Probe{Active: true, Metrics: true, MetricsNote: "x", LastSeenAt: 9_990}, StateUnavailable},
		{store.Probe{Active: true, Metrics: true, MetricsNote: "x", LastSeenAt: 9_990, MetricsAt: 5_000}, StateUnavailable},
		{store.Probe{Active: true, Metrics: true, MetricsNote: "x", LastSeenAt: 5_000, MetricsAt: 5_000}, StateOffline},
	}
	for i, c := range cases {
		if got := State(c.p, now); got != c.want {
			t.Errorf("%d: %s, %s bekleniyordu", i, got, c.want)
		}
	}
}

func TestAlerts(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	webhook := store.Notification{Name: "Kanal", Type: "webhook", Config: json.RawMessage(`{}`), IsDefault: true, Active: true}
	e.st.CreateNotification(ctx, &webhook, false)

	// İlk örnek varsayılan kuralları ve kanalları ekler.
	e.send(cpu(10))
	r := e.rules()
	if len(r) != 4 || r["offline"].Minutes != 3 || r["cpu"].Threshold != 90 || r["cpu"].Minutes != 10 ||
		r["mem"].Threshold != 90 || r["disk"].Threshold != 85 || r["disk"].Minutes != 1 {
		t.Fatalf("varsayılan kurallar: %+v", r)
	}
	if ids, _ := e.st.ProbeNotificationIDs(ctx, e.probe.ID); len(ids) != 1 {
		t.Fatalf("varsayılan kanal: %v", ids)
	}

	// CPU %95: ilk örnek (%10) 10 dk pencereden çıkınca ortalama eşiği geçer
	// (10:10'daki örnekle), bir kez bildirim gider.
	for range 9 {
		e.send(cpu(95))
	}
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("ortalama eşiğin altındayken: %+v", evs)
	}
	e.send(cpu(95))
	evs := e.notif.take()
	if len(evs) != 1 {
		t.Fatalf("bir kez bildirim bekleniyordu: %+v", evs)
	}
	ev := evs[0]
	if ev.Kind != notify.KindServerAlert || ev.ProbeID != e.probe.ID || ev.Metric != "cpu" || ev.Threshold != 90 || ev.Minutes != 10 ||
		ev.Target != "cp1" || ev.URL != fmt.Sprintf("https://uptime.test/#/servers/%d", e.probe.ID) || ev.Value != 95 {
		t.Fatalf("olay: %+v", ev)
	}
	if ev.Title() != "🔴 CP Server İstanbul: CPU %95 (10 dk ortalama, eşik %90)" {
		t.Fatalf("başlık: %s", ev.Title())
	}
	if !e.rules()["cpu"].Firing {
		t.Fatal("kural tetiklenmiş olmalı")
	}
	if v := e.hub.last(); len(v.Firing) != 1 || v.Firing[0] != "cpu" {
		t.Fatalf("canlı olayda tetiklenenler: %+v", v.Firing)
	}
	// Yüksek kaldıkça tekrar bildirim yok; yeniden başlatma da tekrar bildirmez.
	e.send(cpu(96))
	e.restart()
	for range 3 {
		e.send(cpu(97))
	}
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("tekrar bildirim: %+v", evs)
	}
	// Ortalama eşiğin altına inene kadar sürer: tek düşük örnek yetmez.
	e.send(cpu(60))
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("erken bitiş: %+v", evs)
	}
	for range 5 {
		e.send(cpu(10))
	}
	evs = e.notif.take()
	if len(evs) != 1 || evs[0].Kind != notify.KindServerResolved || evs[0].Metric != "cpu" || evs[0].Value > 90 {
		t.Fatalf("bitiş: %+v", evs)
	}
	if evs[0].Title() != "🟢 CP Server İstanbul: CPU normale döndü" {
		t.Fatalf("bitiş başlığı: %s", evs[0].Title())
	}
	events, _ := e.st.ServerAlertEvents(ctx, e.probe.ID, 0, 10)
	if len(events) != 1 || events[0].Metric != "cpu" || events[0].EndedAt == 0 || events[0].Value <= 90 {
		t.Fatalf("geçmiş: %+v", events)
	}

	// Disk %90 (eşik 85, 1 dk): tek örnekle başlar.
	full := cpu(10)
	full.Disks = []metrics.Disk{{Mount: "/", Total: 100, Used: 90}}
	e.send(full)
	evs = e.notif.take()
	if len(evs) != 1 || evs[0].Metric != "disk" || evs[0].Value != 90 {
		t.Fatalf("disk: %+v", evs)
	}
	if evs[0].Title() != "🔴 CP Server İstanbul: Disk (/) %90 (1 dk ortalama, eşik %85)" {
		t.Fatalf("disk başlığı: %s", evs[0].Title())
	}
	e.send(cpu(10))
	if evs := e.notif.take(); len(evs) != 1 || evs[0].Kind != notify.KindServerResolved {
		t.Fatalf("disk bitişi: %+v", evs)
	}

	// Çevrimdışı: son örnekten 3 dk geçmeden uyarı yok, sonra bir kez.
	e.clock = e.clock.Add(2 * time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("erken çevrimdışı: %+v", evs)
	}
	e.clock = e.clock.Add(90 * time.Second)
	e.svc.CheckOffline(ctx)
	e.svc.CheckOffline(ctx)
	evs = e.notif.take()
	if len(evs) != 1 || evs[0].Metric != "offline" || evs[0].Kind != notify.KindServerAlert ||
		evs[0].Title() != "🔴 CP Server İstanbul: sunucudan veri gelmiyor" {
		t.Fatalf("çevrimdışı: %+v", evs)
	}
	if v := e.hub.last(); v.State != StateOffline || len(v.Firing) != 1 || v.Firing[0] != "offline" {
		t.Fatalf("çevrimdışı görünüm: %+v", v)
	}
	// Veri tekrar gelir: uyarı biter.
	e.send(cpu(10))
	evs = e.notif.take()
	if len(evs) != 1 || evs[0].Kind != notify.KindServerResolved || evs[0].Title() != "🟢 CP Server İstanbul: tekrar veri gönderiyor" {
		t.Fatalf("tekrar veri: %+v", evs)
	}

	// Devre dışı bırakılan ajanın uyarısı bildirimsiz kapanır, çevrimdışı uyarısı gelmez.
	e.clock = e.clock.Add(10 * time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 1 {
		t.Fatalf("çevrimdışı: %+v", evs)
	}
	e.st.UpdateProbe(ctx, e.probe.ID, e.probe.Name, false)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("devre dışı: %+v", evs)
	}
	if e.rules()["offline"].Firing {
		t.Fatal("devre dışı ajanın uyarısı kapanmalı")
	}
	if v := e.hub.last(); v.State != StateDisabled || len(v.Firing) != 0 {
		t.Fatalf("devre dışı görünüm: %+v", v)
	}
}

func TestOfflineNeverSent(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	// Hiç örnek göndermemiş ajan çevrimdışı uyarısı almaz (kuralı olsa bile).
	e.st.InitServerDefaults(ctx, e.probe.ID, DefaultRules())
	e.clock = e.clock.Add(time.Hour)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("örnek göndermemiş ajan: %+v", evs)
	}
	// Kural süresi 3 dk'dan uzunsa o kadar beklenir.
	e.send(cpu(1))
	e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{{Metric: "offline", Minutes: 10, Active: true}}, e.clock.Unix())
	e.clock = e.clock.Add(9 * time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("10 dk dolmadan: %+v", evs)
	}
	e.clock = e.clock.Add(2 * time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 1 || evs[0].Minutes != 10 {
		t.Fatalf("10 dk sonra: %+v", evs)
	}
}

// cPanel tipi düzen: /boot hep dolu, /home ayrı bölüm. Bölüme bağlı kural
// yalnızca kendi bölümüne bakar; bölümsüz kural en dolu bölümü bildirir.
func TestDiskRulePerMount(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	disks := func(home uint64) metrics.Stats {
		st := cpu(10)
		st.Disks = []metrics.Disk{
			{Mount: "/", Total: 100, Used: 40},
			{Mount: "/boot", Total: 100, Used: 95},
			{Mount: "/home", Total: 100, Used: home},
		}
		return st
	}
	e.send(disks(50)) // ilk örnek varsayılan kuralları ekler
	e.notif.take()
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricDisk, Mount: "/home", Threshold: 85, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	e.send(disks(50))
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("/boot dolu diye /home kuralı tetiklenmemeli: %+v", evs)
	}
	e.send(disks(91))
	evs := e.notif.take()
	if len(evs) != 1 || evs[0].Mount != "/home" || evs[0].Value != 91 {
		t.Fatalf("/home uyarısı: %+v", evs)
	}
	if got := evs[0].Title(); got != "🔴 CP Server İstanbul: Disk (/home) %91 (1 dk ortalama, eşik %85)" {
		t.Fatalf("başlık: %s", got)
	}
	e.send(disks(60))
	if evs := e.notif.take(); len(evs) != 1 || evs[0].Kind != notify.KindServerResolved || evs[0].Mount != "/home" {
		t.Fatalf("/home bitişi: %+v", evs)
	}

	// Bölümsüz kural: en dolu bölüm (/boot) bildirilir ve geçmişe yazılır.
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricDisk, Threshold: 90, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	e.send(disks(60))
	evs = e.notif.take()
	if len(evs) != 1 || evs[0].Mount != "/boot" {
		t.Fatalf("en dolu bölüm: %+v", evs)
	}
	events, _ := e.st.ServerAlertEvents(ctx, e.probe.ID, 0, 10)
	if len(events) == 0 || events[0].Mount != "/boot" {
		t.Fatalf("geçmişte bölüm: %+v", events)
	}
}

// Ana sunucu uzun süre kapalı kaldıysa (deploy, yeniden başlatma) veya metrik
// toplama yeniden açıldıysa ajanın ilk örneğini beklemeden çevrimdışı uyarısı gitmez.
func TestOfflineGraceAfterStartAndArm(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1)) // varsayılan kurallar: çevrimdışı 3 dk
	e.notif.take()

	// Ana sunucu 20 dk kapalı kaldı, sonra açıldı.
	e.clock = e.clock.Add(20 * time.Minute)
	e.restart()
	sctx, cancel := context.WithCancel(ctx)
	e.svc.Start(sctx)
	cancel()
	e.svc.Wait()
	e.clock = e.clock.Add(time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("açılıştan hemen sonra uyarı: %+v", evs)
	}
	// Açılıştan sonra da veri gelmezse uyarı gider.
	e.clock = e.clock.Add(OfflineAfter)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 1 || evs[0].Metric != MetricOffline {
		t.Fatalf("açılıştan sonra veri gelmeyince: %+v", evs)
	}
	e.send(cpu(1))
	e.notif.take()

	// Metrik toplama kapatılıp çok sonra açıldı.
	e.st.SetProbeMetrics(ctx, e.probe.ID, false)
	e.clock = e.clock.Add(time.Hour)
	e.st.SetProbeMetrics(ctx, e.probe.ID, true)
	e.svc.Arm(e.probe.ID)
	e.clock = e.clock.Add(time.Minute)
	e.svc.CheckOffline(ctx)
	if evs := e.notif.take(); len(evs) != 0 {
		t.Fatalf("yeniden açıldıktan hemen sonra uyarı: %+v", evs)
	}
}
