package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// aggregateDecision: bağlantısı kopan kontrol noktasının oyu, meshLost işareti
// ve açık kısmi olayda (holdGone) uzatılan tolerans.
func TestAggregateDecisionMesh(t *testing.T) {
	now := time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC)
	rules := locRules{staleAfter: 3 * time.Minute, grace: 3 * time.Minute, maxRetries: 0, goneGrace: 5 * time.Second}
	main := func(up bool) *location {
		l := loc("Ana sunucu", now, up, map[bool]int{true: 0, false: 1}[up], map[bool]string{true: "OK", false: "503"}[up])
		l.probeID = LocalProbeID
		return l
	}
	probe := func(id int64, name string, up bool, goneFor time.Duration) *location {
		l := loc(name, now.Add(-time.Second), up, map[bool]int{true: 0, false: 1}[up], map[bool]string{true: "OK", false: "503"}[up])
		l.probeID = id
		if goneFor > 0 {
			l.gone, l.goneAt = true, now.Add(-goneFor)
		}
		return l
	}
	cases := []struct {
		name        string
		locs        []*location
		hold        bool
		up, pending bool
		meshLost    bool
	}{
		{"probe sağlam, Main düşük: UP, mesh sağlam", []*location{main(false), probe(1, "B", true, 0)}, false, true, false, false},
		{"probe az önce koptu (tolerans içinde): UP", []*location{main(false), probe(1, "B", true, 2*time.Second)}, false, true, false, false},
		{"probe tolerans dışında kopuk: yalnız Main, DOWN + meshLost", []*location{main(false), probe(1, "B", true, 10*time.Second)}, false, false, false, true},
		{"açık kısmi + kopma: staleAfter boyunca UP", []*location{main(false), probe(1, "B", true, time.Minute)}, true, true, false, false},
		{"açık kısmi + kopma staleAfter dışında: DOWN + meshLost", []*location{main(false), probe(1, "B", true, 4*time.Minute)}, true, false, false, true},
		{"tüm probe kopuk, Main çalışıyor: UP + meshLost", []*location{main(true), probe(1, "B", true, time.Minute), probe(2, "C", true, time.Minute)}, false, true, false, true},
		{"Main konum değil, probe kopuk: PENDING", []*location{probe(1, "B", true, time.Minute)}, false, false, true, false},
		{"oyu yok sayılan (OOB) konum: yalnız Main", []*location{main(false), func() *location { l := probe(1, "B", true, 0); l.noVote = true; return l }()}, false, false, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := rules
			r.holdGone = c.hold
			got, ml := aggregateDecision(c.locs, store.DownWhenAll, now, r)
			if got.Up != c.up || got.Pending != c.pending || ml != c.meshLost {
				t.Fatalf("sonuç %+v meshLost=%v; up=%v pending=%v meshLost=%v bekleniyordu", got, ml, c.up, c.pending, c.meshLost)
			}
		})
	}
}

// downMeshLost normal olayın başlangıç kaydındaki mesh_lost işareti.
func downMeshLost(t *testing.T, f *fixture, id int64) bool {
	t.Helper()
	evs, err := f.st.IncidentEvents(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if ev.Kind == store.EventDown {
			var d downData
			_ = json.Unmarshal(ev.Data, &d)
			return d.MeshLost
		}
	}
	t.Fatal("başlangıç kaydı yok")
	return false
}

// Mesh koptuğunda kısmi karar: az önce kopan konumun son "çalışıyor" oyu kısmi
// olay açmaz; açık kısmi olay kopmada staleAfter kadar bekler, sonra notlu
// (mesh_lost) tam kesintiye dönüşür.
func TestPartialMeshLost(t *testing.T) {
	f := newFixture(t)
	a := f.probe(t, "Ankara", true)
	setup := store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{a.ID}, DownWhen: store.DownWhenAll}
	defer fake.set(up())

	t.Run("tolerans içindeki oy kısmi açmaz", func(t *testing.T) {
		m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
		r := f.locRunner(t, m, setup)
		fake.set(up())
		r.locationTick(context.Background())
		f.clock = f.clock.Add(time.Millisecond)
		r.remote(a, f.clock, up())
		f.e.SetProbeConnected(a.ID, false)
		defer f.e.SetProbeConnected(a.ID, true)
		f.clock = f.clock.Add(time.Millisecond) // goneGrace (5 ms) içinde
		fake.set(down("HTTP 503"))
		r.locationTick(context.Background())
		if r.m.Status != store.StatusUp {
			t.Fatalf("tolerans içinde UP kalmalı: %d", r.m.Status)
		}
		if n := len(kindIncidents(t, f, m.ID, store.KindGroupPartial)); n != 0 {
			t.Fatalf("kopan konumun oyu kısmi açmamalı: %d", n)
		}
		f.clock = f.clock.Add(10 * time.Millisecond)
		r.locationTick(context.Background())
		mon := kindIncidents(t, f, m.ID, store.KindGroupMonitor)
		if r.m.Status != store.StatusDown || len(mon) != 1 || !downMeshLost(t, f, mon[0].ID) {
			t.Fatalf("notlu DOWN bekleniyordu: %d %+v", r.m.Status, mon)
		}
	})

	t.Run("açık kısmi kopmada bekler", func(t *testing.T) {
		f.e.SetProbeConnected(a.ID, true)
		m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
		r := f.locRunner(t, m, setup)
		fake.set(up())
		r.locationTick(context.Background())
		f.clock = f.clock.Add(time.Millisecond)
		r.remote(a, f.clock, up())
		f.clock = f.clock.Add(time.Millisecond)
		fake.set(down("HTTP 503"))
		r.locationTick(context.Background())
		if r.m.Status != store.StatusUp || len(kindIncidents(t, f, m.ID, store.KindGroupPartial)) != 1 {
			t.Fatalf("UP + kısmi olay bekleniyordu: %d", r.m.Status)
		}
		f.e.SetProbeConnected(a.ID, false)
		defer f.e.SetProbeConnected(a.ID, true)
		f.clock = f.clock.Add(20 * time.Millisecond) // goneGrace geçti, staleAfter (180 ms) dolmadı
		r.locationTick(context.Background())
		if r.m.Status != store.StatusUp || len(kindIncidents(t, f, m.ID, store.KindGroupMonitor)) != 0 {
			t.Fatalf("kopma staleAfter dolmadan tam kesintiye çevirmemeli: %d", r.m.Status)
		}
		f.clock = f.clock.Add(200 * time.Millisecond)
		r.locationTick(context.Background())
		mon := kindIncidents(t, f, m.ID, store.KindGroupMonitor)
		if r.m.Status != store.StatusDown || len(mon) != 1 || !downMeshLost(t, f, mon[0].ID) {
			t.Fatalf("notlu DOWN bekleniyordu: %d %+v", r.m.Status, mon)
		}
		if parts := kindIncidents(t, f, m.ID, store.KindGroupPartial); len(parts) != 1 || parts[0].ResolvedAt == 0 {
			t.Fatalf("kısmi olay tam kesintiye dönüşüp kapanmalı: %+v", parts)
		}
	})
}
