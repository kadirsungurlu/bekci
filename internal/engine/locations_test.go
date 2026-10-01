package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func loc(name string, at time.Time, up bool, fails int, msg string) *location {
	return &location{name: name, have: true, at: at, fails: fails,
		res: check.Result{Up: up, PingMs: map[bool]int64{true: 100, false: -1}[up], Message: msg}}
}

func TestAggregateLocations(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	stale := 3 * time.Minute
	old := now.Add(-time.Hour)
	const retries = 1

	type want struct {
		up, pending bool
		msg         string
	}
	cases := []struct {
		name     string
		downWhen string
		locs     []*location
		want     want
	}{
		{"any: hepsi çalışıyor", store.DownWhenAny,
			[]*location{loc("A", now, true, 0, "200 OK"), loc("B", now, true, 0, "200 OK")},
			want{up: true, msg: "200 OK"}},
		{"any: biri çalışmıyor", store.DownWhenAny,
			[]*location{loc("İstanbul-2", now, false, 2, "Zaman aşımı"), loc("Frankfurt", now, false, 2, "503"), loc("C", now, true, 0, "OK")},
			want{msg: "İstanbul-2: Zaman aşımı; Frankfurt: 503"}},
		{"any: biri tekrar deneniyor", store.DownWhenAny,
			[]*location{loc("A", now, false, 1, "503"), loc("B", now, true, 0, "OK")},
			want{pending: true, msg: "A: 503"}},
		{"majority: 1/3 çalışmıyor → UP", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, true, 0, "OK"), loc("C", now, true, 0, "OK")},
			want{up: true, msg: "2/3 konum çalışıyor — A: 503"}},
		{"majority: 2/3 çalışmıyor → DOWN", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, false, 5, "Zaman aşımı"), loc("C", now, true, 0, "OK")},
			want{msg: "A: 503; B: Zaman aşımı"}},
		{"majority: 2 konumdan 1 → UP", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, true, 0, "OK")},
			want{up: true, msg: "1/2 konum çalışıyor — A: 503"}},
		{"majority: biri çalışmıyor biri tekrar deneniyor → PENDING", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, false, 1, "503"), loc("C", now, true, 0, "OK")},
			want{pending: true, msg: "A: 503; B: 503"}},
		{"all: biri çalışıyor → UP", store.DownWhenAll,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, true, 0, "OK")},
			want{up: true, msg: "1/2 konum çalışıyor — A: 503"}},
		{"all: hepsi çalışmıyor → DOWN", store.DownWhenAll,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, false, 2, "Bağlantı reddedildi")},
			want{msg: "A: 503; B: Bağlantı reddedildi"}},
		{"eski sonuç kurala katılmaz", store.DownWhenAll,
			[]*location{loc("A", now, false, 2, "503"), loc("B", old, true, 0, "OK")},
			want{msg: "A: 503 (ulaşılamayan: B)"}},
		{"eski DOWN sonucu DOWN sayılmaz", store.DownWhenAny,
			[]*location{loc("A", old, false, 9, "503"), loc("B", now, true, 0, "200 OK")},
			want{up: true, msg: "200 OK (ulaşılamayan: A)"}},
		{"hiç güncel sonuç yok → PENDING", store.DownWhenAny,
			[]*location{loc("A", old, true, 0, "OK"), {name: "B"}},
			want{pending: true, msg: NoLocationData}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := aggregateLocations(c.locs, c.downWhen, now, locRules{staleAfter: stale, grace: stale, maxRetries: retries})
			if got.Up != c.want.up || got.Pending != c.want.pending || got.Message != c.want.msg {
				t.Fatalf("sonuç %+v; up=%v pending=%v %q bekleniyordu", got, c.want.up, c.want.pending, c.want.msg)
			}
		})
	}
}

// probeFixture çok konumlu monitör için kontrol noktaları ve konum ayarı kurar.
func (f *fixture) probe(t *testing.T, name string, active bool) store.Probe {
	t.Helper()
	p := store.Probe{Name: name, Active: active, CreatedAt: f.clock.Unix(), Hash: "hash-" + name, TokenPrefix: "upr_x"}
	if err := f.st.CreateProbe(context.Background(), &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func (f *fixture) locRunner(t *testing.T, m store.Monitor, setup store.LocationSetup) *runner {
	t.Helper()
	if err := f.st.SetMonitorLocations(context.Background(), m.ID, setup); err != nil {
		t.Fatal(err)
	}
	r := f.runnerFor(t, m.ID)
	r.locs = f.e.loadLocations(r.m)
	if r.locs == nil {
		t.Fatal("konum ayarı yüklenmedi")
	}
	return r
}

// remote uzak sonucu doğrudan runner'ın konumuna uygular (API'den gelen yol
// TestProbeResultsInbox'ta).
func (r *runner) remote(p store.Probe, at time.Time, res check.Result) bool {
	r.locs.mu.Lock()
	r.locs.inbox = append(r.locs.inbox, inboxItem{probeID: p.ID, res: ProbeResult{MonitorID: r.m.ID, Time: at, Result: res}})
	r.locs.mu.Unlock()
	return r.locationWake()
}

func TestLocalPlusRemote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 1 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAny})
	fake.set(up())
	defer fake.set(up())

	// Uzak sonuç gelmeden ana sunucu kontrolü yeterli: UP.
	if !r.locationTick(ctx) || r.m.Status != store.StatusUp {
		t.Fatalf("ilk kontrol UP olmalı: %d %q", r.m.Status, r.m.LastMessage)
	}
	if r.remote(fra, f.clock, up()) {
		t.Fatal("durum değişmeyen uzak sonuç hemen kaydedilmemeli")
	}
	// Uzak konumda ilk hata: tekrar deneniyor → hemen PENDING.
	f.clock = f.clock.Add(10 * time.Millisecond)
	if !r.remote(fra, f.clock, down("HTTP 503")) || r.m.Status != store.StatusPending {
		t.Fatalf("PENDING bekleniyordu: %d", r.m.Status)
	}
	// İkinci hata: tekrar deneme hakkı bitti → DOWN, mesajda konum adı.
	f.clock = f.clock.Add(10 * time.Millisecond)
	if !r.remote(fra, f.clock, down("HTTP 503")) || r.m.Status != store.StatusDown {
		t.Fatalf("DOWN bekleniyordu: %d", r.m.Status)
	}
	if r.m.LastMessage != "Frankfurt: HTTP 503" {
		t.Errorf("mesaj %q", r.m.LastMessage)
	}
	// Eski tarihli (sıra dışı) sonuç yok sayılır.
	if r.remote(fra, f.clock.Add(-time.Second), up()) || r.m.Status != store.StatusDown {
		t.Fatal("eski tarihli sonuç durumu değiştirmemeli")
	}
	// Zamanlayıcıda da ana sunucu çalışıyor ama kural any: DOWN sürer.
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusDown {
		t.Fatalf("DOWN sürmeli: %d", r.m.Status)
	}
	f.clock = f.clock.Add(10 * time.Millisecond)
	if !r.remote(fra, f.clock, up()) || r.m.Status != store.StatusUp {
		t.Fatalf("UP bekleniyordu: %d", r.m.Status)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("bildirimler %v", got)
	}
	if f.n.events[0].Message != "Frankfurt: HTTP 503" {
		t.Errorf("DOWN bildirimi mesajı %q", f.n.events[0].Message)
	}
	// Ana sunucu da çalışmazsa her iki konum mesajda.
	fake.set(down("Zaman aşımı"))
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.remote(fra, f.clock, down("HTTP 503"))
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusDown || r.m.LastMessage != "Ana sunucu: Zaman aşımı; Frankfurt: HTTP 503" {
		t.Fatalf("durum %d mesaj %q", r.m.Status, r.m.LastMessage)
	}
	if _, ok := f.e.LocationStatuses(m.ID); ok {
		t.Error("çalışmayan runner için durum dönmemeli")
	}
	snap := *r.locs.snap.Load()
	if len(snap) != 2 || snap[0].Name != LocalName || snap[0].Status != locDown || snap[1].Status != locRetrying {
		t.Errorf("konum durumları %+v", snap)
	}
}

func TestRemoteOnlyStaleAndNoData(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.probe(t, "A", true), f.probe(t, "B", true)
	m := f.monitor(t, func(m *store.Monitor) { m.Interval, m.RetryInterval = 60, 60 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{a.ID, b.ID}, DownWhen: store.DownWhenAll})
	fake.set(down("ana sunucu kullanılmamalı"))
	defer fake.set(up())
	stale := r.staleAfter() // 3 × 60 ms

	// Başlangıç süresi içinde sonuç yoksa hiçbir şey yazılmaz.
	r.locationTick(ctx)
	if got, _ := f.st.GetMonitor(ctx, m.ID); got.LastCheckAt != 0 {
		t.Fatal("başlangıçta sonuç yokken kayıt yazılmamalı")
	}
	r.remote(a, f.clock, up())
	r.remote(b, f.clock, up())
	r.locationTick(ctx)
	if r.m.Status != store.StatusUp || r.m.LastMessage != "200 OK" {
		t.Fatalf("UP bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}
	// B susar; A çalışmıyor. "all" kuralında güncel olan tek konum A: DOWN.
	f.clock = f.clock.Add(stale / 2)
	r.remote(a, f.clock, down("HTTP 503"))
	if r.m.Status != store.StatusUp {
		t.Fatal("B hâlâ güncel ve çalışıyor: UP kalmalı")
	}
	f.clock = f.clock.Add(stale/2 + time.Millisecond)
	r.remote(a, f.clock, down("HTTP 503"))
	r.locationTick(ctx)
	if r.m.Status != store.StatusDown || r.m.LastMessage != "A: HTTP 503 (ulaşılamayan: B)" {
		t.Fatalf("durum %d mesaj %q", r.m.Status, r.m.LastMessage)
	}
	// Hiçbir konumdan güncel sonuç yok: PENDING.
	f.clock = f.clock.Add(stale + time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusPending || r.m.LastMessage != NoLocationData {
		t.Fatalf("durum %d mesaj %q", r.m.Status, r.m.LastMessage)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("sonuç gelmemesi bildirim üretmemeli: %v", got)
	}
}

func TestLocationsUpsideDownAndCert(t *testing.T) {
	f := newFixture(t)
	p := f.probe(t, "P", true)
	m := f.monitor(t, func(m *store.Monitor) {
		m.UpsideDown = true
		m.Type, m.Config = "http", json.RawMessage(`{"url":"https://kadir.app"}`)
	})
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{p.ID}, DownWhen: store.DownWhenAny})
	r.remote(p, f.clock, down("Bağlantı reddedildi"))
	if r.m.Status != store.StatusUp {
		t.Fatalf("ters modda erişilemeyen hedef UP: %d", r.m.Status)
	}
	res := up()
	res.Cert = &check.CertInfo{NotAfter: f.clock.Add(100 * 24 * time.Hour), Issuer: "R3"}
	f.clock = f.clock.Add(time.Millisecond)
	r.remote(p, f.clock, res)
	if r.m.Status != store.StatusDown || !strings.HasPrefix(r.m.LastMessage, "P: Ters mod") {
		t.Fatalf("ters modda erişilebilen hedef DOWN: %d %q", r.m.Status, r.m.LastMessage)
	}
	if r.m.CertIssuer != "R3" {
		t.Errorf("uzak konumun SSL bilgisi kaydedilmeli: %q", r.m.CertIssuer)
	}
}

func TestLoadLocations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	on, off := f.probe(t, "Açık", true), f.probe(t, "Kapalı", false)

	plain := f.monitor(t, nil)
	if r := f.runnerFor(t, plain.ID); f.e.loadLocations(r.m) != nil {
		t.Fatal("ayarsız monitör tek konumlu kalmalı")
	}
	m := f.monitor(t, nil)
	f.st.SetMonitorLocations(ctx, m.ID, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{on.ID, off.ID}, DownWhen: "majority"})
	ls := f.e.loadLocations(m)
	if ls == nil || len(ls.locs) != 2 || ls.local == nil || ls.byProbe[on.ID] == nil || ls.byProbe[off.ID] != nil || ls.downWhen != "majority" {
		t.Fatalf("devre dışı kontrol noktası konum sayılmamalı: %+v", ls)
	}
	push := f.monitor(t, func(m *store.Monitor) { m.Type, m.PushToken = check.TypePush, "tok" })
	f.st.SetMonitorLocations(ctx, push.ID, store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{on.ID}, DownWhen: "any"})
	if f.e.loadLocations(push) != nil {
		t.Fatal("push monitörü uzak konum kullanamaz")
	}
	// Varsayılan ayar satır bırakmaz.
	f.st.SetMonitorLocations(ctx, m.ID, store.DefaultLocations())
	if all, _ := f.st.AllMonitorLocations(ctx); len(all) != 1 {
		t.Fatalf("yalnızca push monitörünün ayarı kalmalı: %+v", all)
	}
	// Kontrol noktası silinince başka konumu kalmayan monitör ana sunucuya döner.
	affected, err := f.st.DeleteProbe(ctx, on.ID)
	if err != nil || len(affected) != 1 || affected[0] != push.ID {
		t.Fatalf("etkilenen %v %v", affected, err)
	}
	if l, _ := f.st.MonitorLocations(ctx, push.ID); !l.IncludeLocal || len(l.ProbeIDs) != 0 || l.Configured() {
		t.Fatalf("ana sunucuya dönmeliydi: %+v", l)
	}
}

// Gerçek döngüyle: ProbeResults runner'ı uyandırır, durum hemen değişir.
func TestProbeResultsInbox(t *testing.T) {
	f := newFixture(t)
	f.e.now = time.Now
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); f.e.Wait() }()
	p, other := f.probe(t, "P", true), f.probe(t, "Diğer", true)
	m := f.monitor(t, func(m *store.Monitor) { m.Interval = 5000 })
	f.st.SetMonitorLocations(ctx, m.ID, store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{p.ID}, DownWhen: "any"})
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	// Atanmamış kontrol noktasının sonucu kabul edilmez.
	if n := f.e.ProbeResults(other.ID, []ProbeResult{{MonitorID: m.ID, Time: time.Now(), Result: up()}}); n != 0 {
		t.Fatalf("atanmamış kontrol noktası: %d", n)
	}
	if n := f.e.ProbeResults(p.ID, []ProbeResult{{MonitorID: m.ID, Time: time.Now(), Result: down("HTTP 500")}}); n != 1 {
		t.Fatalf("kabul edilen %d", n)
	}
	waitFor(t, "DOWN", func() bool {
		got, _ := f.st.GetMonitor(ctx, m.ID)
		return got.Status == store.StatusDown && got.LastMessage == "P: HTTP 500"
	})
	waitFor(t, "konum durumu", func() bool {
		st, ok := f.e.LocationStatuses(m.ID)
		return ok && len(st) == 1 && st[0].Status == locDown && st[0].Name == "P" && st[0].Message == "HTTP 500"
	})
}

func TestProbeWatcher(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	ch, unsub, _ := f.e.hub.Subscribe(0)
	defer unsub()
	p := f.probe(t, "P", true)
	known := f.e.scanProbes(ctx, nil) // ilk tarama olay üretmez
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1")
	known = f.e.scanProbes(ctx, known)
	f.clock = f.clock.Add(ProbeOfflineAfter + time.Second)
	f.e.scanProbes(ctx, known)
	var got []string
	for len(ch) > 0 {
		got = append(got, string(<-ch))
	}
	if len(got) != 2 || !strings.Contains(got[0], `"online":true`) || !strings.Contains(got[1], `"online":false`) ||
		!strings.Contains(got[0], `"type":"probe"`) {
		t.Fatalf("olaylar %v", got)
	}
}

// Kontrol noktasının bağlantısı kopunca son sonucu "çalışmıyor" olan konumun
// oyu sonuç eskiyene kadar korunur: ajanın yeniden başlaması açık kesinti
// olayını kapatıp yeniden açmaz (sahte 🟢 + 🔴 yok).
func TestGoneProbeKeepsDownVote(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAny})
	fake.set(up())
	defer fake.set(up())

	r.locationTick(ctx)
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.remote(fra, f.clock, down("Alan adı bulunamadı"))
	if r.m.Status != store.StatusDown || r.incidentID == 0 {
		t.Fatalf("DOWN ve açık olay bekleniyordu: %d", r.m.Status)
	}
	inc := r.incidentID

	// Ajan yeniden başlıyor: bağlantı koptu, kart "sonuç yok" ama oy korunur.
	f.e.SetProbeConnected(fra.ID, false)
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationWake()
	if snap := *r.locs.snap.Load(); snap[1].Status != locUnknown {
		t.Fatalf("kart için 'sonuç yok' bekleniyordu: %+v", snap)
	}
	if r.m.Status != store.StatusDown || r.incidentID != inc {
		t.Fatalf("kopan ajanın DOWN oyu korunmalı: durum %d olay %d → %d", r.m.Status, inc, r.incidentID)
	}
	f.clock = f.clock.Add(f.e.goneGrace() + time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusDown || r.incidentID != inc {
		t.Fatalf("tolerans dolsa da DOWN oyu sonuç eskiyene kadar korunmalı: %d", r.m.Status)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("yalnızca ilk kesinti bildirimi gitmeli: %v", got)
	}
	// Ajan geri döner, konum hâlâ çalışmıyor: olay değişmez.
	f.e.SetProbeConnected(fra.ID, true)
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.remote(fra, f.clock, down("Alan adı bulunamadı"))
	if r.m.Status != store.StatusDown || r.incidentID != inc {
		t.Fatalf("olay sürmeli: %d", r.incidentID)
	}
	// Sonuç eskiyince (ajan uzun süre gelmezse) konum hesaptan düşer: UP + "ulaşılamayan".
	f.e.SetProbeConnected(fra.ID, false)
	f.clock = f.clock.Add(r.staleAfter() + time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusUp || !strings.Contains(r.m.LastMessage, "ulaşılamayan: Frankfurt") {
		t.Fatalf("eskiyen sonuç kurala katılmamalı: %d %q", r.m.Status, r.m.LastMessage)
	}
}
