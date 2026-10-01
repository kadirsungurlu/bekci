package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// waitingLoc hiç sonuç vermemiş, az önce eklenmiş konum.
func waitingLoc(name string, added time.Time) *location { return &location{name: name, added: added} }

func TestAggregateWaiting(t *testing.T) {
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	rules := locRules{staleAfter: 3 * time.Minute, grace: 2 * time.Minute, maxRetries: 1}
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
		{"any: ana sunucu çalışıyor, uzak bekleniyor → UP, sonuç gelmeyen yok", store.DownWhenAny,
			[]*location{loc("Ana sunucu", now, true, 0, "200 OK"), waitingLoc("CP", now.Add(-time.Second))},
			want{up: true, msg: "200 OK"}},
		{"any: ana sunucu çalışmıyor → DOWN (bekleyen sonucu değiştiremez)", store.DownWhenAny,
			[]*location{loc("Ana sunucu", now, false, 2, "503"), waitingLoc("CP", now)},
			want{msg: "Ana sunucu: 503"}},
		{"all: ana sunucu çalışıyor, uzak bekleniyor → UP", store.DownWhenAll,
			[]*location{loc("Ana sunucu", now, true, 0, "200 OK"), waitingLoc("CP", now)},
			want{up: true, msg: "200 OK"}},
		{"all: ana sunucu çalışmıyor, uzak bekleniyor → DOWN değil, tekrar deneniyor", store.DownWhenAll,
			[]*location{loc("Ana sunucu", now, false, 2, "503"), waitingLoc("CP", now)},
			want{pending: true, msg: "Ana sunucu: 503"}},
		{"majority: 1 çalışmıyor + 1 bekleniyor → tekrar deneniyor", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), waitingLoc("W", now)},
			want{pending: true, msg: "A: 503"}},
		{"majority: 2 çalışmıyor + 1 bekleniyor → DOWN (çoğunluk kesin)", store.DownWhenMajority,
			[]*location{loc("A", now, false, 2, "503"), loc("B", now, false, 2, "Zaman aşımı"), waitingLoc("W", now)},
			want{msg: "A: 503; B: Zaman aşımı"}},
		{"majority: 1 çalışıyor + 1 bekleniyor → UP", store.DownWhenMajority,
			[]*location{loc("A", now, true, 0, "200 OK"), waitingLoc("W", now)},
			want{up: true, msg: "200 OK"}},
		{"süresi dolan konum sonuç gelmeyen olur", store.DownWhenAny,
			[]*location{loc("Ana sunucu", now, true, 0, "200 OK"), waitingLoc("CP", now.Add(-rules.grace-time.Second))},
			want{up: true, msg: "200 OK (ulaşılamayan: CP)"}},
		{"yalnızca bekleyen konumlar → PENDING", store.DownWhenAny,
			[]*location{waitingLoc("A", now), waitingLoc("B", now)},
			want{pending: true, msg: NoLocationData}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := aggregateLocations(c.locs, c.downWhen, now, rules)
			if got.Up != c.want.up || got.Pending != c.want.pending || got.Message != c.want.msg {
				t.Fatalf("sonuç %+v; up=%v pending=%v %q bekleniyordu", got, c.want.up, c.want.pending, c.want.msg)
			}
		})
	}
}

func TestLocRulesGrace(t *testing.T) {
	e := New(nil, nil, NewHub(), nil, Config{Unit: time.Second})
	// Kısa aralık: iş listesi yoklaması + ilk kontrol kaydırması + zaman aşımı + pay.
	r := e.locRulesFor(store.Monitor{Interval: 20, RetryInterval: 20, Timeout: 16})
	if r.staleAfter != time.Minute || r.grace != (30+10+16+15)*time.Second {
		t.Fatalf("20 sn: %+v", r)
	}
	// Uzun aralık: çalışan işin sıradaki planlı kontrolü için staleAfter kadar.
	r = e.locRulesFor(store.Monitor{Interval: 300, RetryInterval: 60, Timeout: 48})
	if r.grace != r.staleAfter || r.staleAfter != 15*time.Minute {
		t.Fatalf("300 sn: %+v", r)
	}
}

func snapStatus(r *runner, probeID int64) string {
	for _, s := range *r.locs.snap.Load() {
		if s.ProbeID == probeID {
			return s.Status
		}
	}
	return ""
}

func TestLocationWaitingThenUp(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cp := f.probe(t, "CP Server İstanbul", true)
	m := f.monitor(t, nil)
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{cp.ID}, DownWhen: store.DownWhenAll})
	fake.set(up())
	defer fake.set(up())
	events, unsub, _ := f.e.hub.Subscribe(0)
	defer unsub()

	// Yeni konum hemen "ilk sonuç bekleniyor"; mesajda sonuç gelmeyen yok.
	if st := snapStatus(r, cp.ID); st != locWaiting {
		t.Fatalf("başlangıç durumu %q", st)
	}
	if !r.locationTick(ctx) || r.m.Status != store.StatusUp || r.m.LastMessage != "200 OK" {
		t.Fatalf("UP ve temiz mesaj bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}
	if strings.Contains(r.m.LastMessage, "ulaşılamayan") {
		t.Fatalf("bekleyen konum ulaşılamayan sayılmamalı: %q", r.m.LastMessage)
	}
	if st := snapStatus(r, cp.ID); st != locWaiting {
		t.Fatalf("ilk kontrolden sonra uzak konum %q", st)
	}
	drain(events)

	// İlk sonuç gelince "çalışıyor"; genel durum değişmese de canlı akışa haber gider.
	f.clock = f.clock.Add(5 * time.Millisecond)
	if r.remote(cp, f.clock, up()) {
		t.Fatal("genel durum değişmedi, kayıt yazılmamalı")
	}
	if st := snapStatus(r, cp.ID); st != locUp {
		t.Fatalf("ilk sonuçtan sonra %q", st)
	}
	if !gotEvent(events, "locations") {
		t.Fatal("konum durumu değişince locations olayı yayınlanmalı")
	}
}

func TestLocationWaitingExpires(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cp := f.probe(t, "CP Server İstanbul", true)
	m := f.monitor(t, nil)
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{cp.ID}, DownWhen: store.DownWhenAny})
	fake.set(up())
	defer fake.set(up())
	grace := r.rules().grace

	f.clock = f.clock.Add(grace - time.Millisecond)
	r.locationTick(ctx)
	if st := snapStatus(r, cp.ID); st != locWaiting || r.m.LastMessage != "200 OK" {
		t.Fatalf("süre dolmadan: %q %q", st, r.m.LastMessage)
	}
	f.clock = f.clock.Add(2 * time.Millisecond)
	r.locationTick(ctx)
	if st := snapStatus(r, cp.ID); st != locUnknown {
		t.Fatalf("süre dolunca bilinmiyor olmalı: %q", st)
	}
	if r.m.LastMessage != "200 OK (ulaşılamayan: CP Server İstanbul)" {
		t.Fatalf("mesaj %q", r.m.LastMessage)
	}
}

// Ana sunucu çalışmıyor, "all" kuralında uzak konum henüz oy vermedi: DOWN
// denmez; uzak konum çalışıyor derse UP, çalışmıyor derse DOWN.
func TestLocationWaitingAllRule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	cp := f.probe(t, "CP", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{cp.ID}, DownWhen: store.DownWhenAll})
	fake.set(down("Zaman aşımı"))
	defer fake.set(up())

	r.locationTick(ctx)
	if r.m.Status != store.StatusPending {
		t.Fatalf("bekleyen konum varken DOWN denmemeli: %d %q", r.m.Status, r.m.LastMessage)
	}
	if len(f.n.kinds()) != 0 {
		t.Fatalf("bildirim gitmemeli: %v", f.n.kinds())
	}
	f.clock = f.clock.Add(time.Millisecond)
	if !r.remote(cp, f.clock, down("HTTP 503")) || r.m.Status != store.StatusDown {
		t.Fatalf("iki konum da çalışmıyor: DOWN bekleniyordu: %d", r.m.Status)
	}
	if r.m.LastMessage != "Ana sunucu: Zaman aşımı; CP: HTTP 503" {
		t.Errorf("mesaj %q", r.m.LastMessage)
	}
}

// Yeniden yüklemede (konum ekleme, düzenleme) mevcut konumların sonuçları
// korunur; yalnızca yeni konum bekler. Kontrol ayarı değişirse aktarılmaz.
func TestLocationsKeptAcrossReload(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b := f.probe(t, "A", true), f.probe(t, "B", true)
	m := f.monitor(t, nil)
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{a.ID}, DownWhen: store.DownWhenAny})
	r.remote(a, f.clock, up())
	f.e.retire(r)

	setup := store.LocationSetup{IncludeLocal: false, ProbeIDs: []int64{a.ID, b.ID}, DownWhen: store.DownWhenAny}
	if err := f.st.SetMonitorLocations(ctx, m.ID, setup); err != nil {
		t.Fatal(err)
	}
	ls := f.e.loadLocations(r.m)
	f.e.mu.Lock()
	f.e.adoptLocations(r.m, ls)
	f.e.mu.Unlock()
	got := *ls.snap.Load()
	if len(got) != 2 || got[0].Status != locUp || got[0].PingMs != 10 || got[1].Status != locWaiting {
		t.Fatalf("A korunmalı, B beklemeli: %+v", got)
	}

	// Hedef değişti: eski sonuç yeni hedefe ait değil.
	r2 := &runner{e: f.e, m: r.m, locs: ls}
	f.e.retire(r2)
	changed := r.m
	changed.Config = json.RawMessage(`{"url":"https://baska.example"}`)
	ls2 := f.e.loadLocations(changed)
	f.e.mu.Lock()
	f.e.adoptLocations(changed, ls2)
	f.e.mu.Unlock()
	if got := *ls2.snap.Load(); got[0].Status != locWaiting {
		t.Fatalf("ayar değişince sonuç aktarılmamalı: %+v", got)
	}
	// Uzun aradan sonra (durdurup çok sonra başlatma) aktarılmaz.
	f.e.retire(r2)
	f.clock = f.clock.Add(retireKeep + time.Second)
	ls3 := f.e.loadLocations(r.m)
	f.e.mu.Lock()
	f.e.adoptLocations(r.m, ls3)
	f.e.mu.Unlock()
	if got := *ls3.snap.Load(); got[0].Status != locWaiting {
		t.Fatalf("eski sonuç aktarılmamalı: %+v", got)
	}
}

func TestJobsVersion(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); f.e.Wait() }()
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	v0, ch := f.e.JobsVersion()
	select {
	case <-ch:
		t.Fatal("değişiklik yokken kanal kapanmamalı")
	default:
	}
	m := f.monitor(t, func(m *store.Monitor) { m.Interval = 100000 })
	if err := f.e.Reload(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("Reload bekleyenleri uyandırmalı")
	}
	v1, ch1 := f.e.JobsVersion()
	if v1 <= v0 {
		t.Fatalf("sürüm artmalı: %d → %d", v0, v1)
	}
	f.e.Remove(m.ID)
	select {
	case <-ch1:
	case <-time.After(time.Second):
		t.Fatal("Remove bekleyenleri uyandırmalı")
	}
	if v2, _ := f.e.JobsVersion(); v2 <= v1 {
		t.Fatalf("sürüm artmalı: %d → %d", v1, v2)
	}
}

func drain(ch <-chan []byte) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func gotEvent(ch <-chan []byte, typ string) bool {
	for {
		select {
		case msg := <-ch:
			var ev struct {
				Type string `json:"type"`
			}
			if json.Unmarshal(msg, &ev) == nil && ev.Type == typ {
				return true
			}
		default:
			return false
		}
	}
}

// Tek konumlu monitöre ilk kontrol noktası eklenince ana sunucunun son sonucu
// korunur; yalnızca yeni konum "ilk sonuç bekleniyor" olur.
func TestLocalResultKeptWhenFirstProbeAdded(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a := f.probe(t, "A", true)
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(up())
	f.e.retire(r)

	setup := store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{a.ID}, DownWhen: store.DownWhenAny}
	if err := f.st.SetMonitorLocations(ctx, m.ID, setup); err != nil {
		t.Fatal(err)
	}
	ls := f.e.loadLocations(r.m)
	f.e.mu.Lock()
	f.e.adoptLocations(r.m, ls)
	f.e.mu.Unlock()
	got := *ls.snap.Load()
	if len(got) != 2 || got[0].Name != LocalName || got[0].Status != locUp || got[0].PingMs != 10 || got[1].Status != locWaiting {
		t.Fatalf("ana sunucu korunmalı, A beklemeli: %+v", got)
	}

	// Çalışmıyorken eklenirse ana sunucu "çalışmıyor" kalır.
	r2 := f.runnerFor(t, m.ID)
	r2.locs = nil
	r2.m.MaxRetries = 0
	r2.process(down("503"))
	f.e.retire(r2)
	ls2 := f.e.loadLocations(r2.m)
	f.e.mu.Lock()
	f.e.adoptLocations(r2.m, ls2)
	f.e.mu.Unlock()
	if got := *ls2.snap.Load(); got[0].Status != locDown {
		t.Fatalf("ana sunucu çalışmıyor kalmalı: %+v", got)
	}
}
