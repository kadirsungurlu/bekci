package engine

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/schedule"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kontrol aralıkları ızgaraya oturur ve başarısız kontrolden sonra tekrar
// deneme aralığı kullanılır (aralık 60, tekrar deneme 20 birim).
func TestNextDelayRetryInterval(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 3 })
	r := f.runnerFor(t, m.ID)
	r.plan = schedule.Planner{PhaseMs: schedule.PhaseMs(m.ID)}
	fake.set(up())
	defer fake.set(up())

	r.process(up())
	if d := r.nextDelay(true); d < 30*time.Millisecond || d > 90*time.Millisecond {
		t.Fatalf("çalışırken bekleme %v: aralık (60 birim) ızgarası bekleniyordu", d)
	}
	r.process(down("503"))
	if r.m.Status != store.StatusPending {
		t.Fatalf("PENDING bekleniyordu: %d", r.m.Status)
	}
	if d := r.nextDelay(true); d > 20*time.Millisecond {
		t.Fatalf("tekrar denemede bekleme %v: en fazla 20 birim olmalı", d)
	}
}

// "Tüm konumlar çalışmıyorsa" kuralında ana sunucu başarısızken genel durum
// UP kalır; ana sunucu yine de tekrar deneme aralığında kontrol eder.
func TestNextDelayLocalRetryUnderAllRule(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 3 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAll})
	r.plan = schedule.Planner{PhaseMs: schedule.PhaseMs(m.ID)}
	defer fake.set(up())

	fake.set(up())
	r.locationTick(ctx)
	r.nextDelay(true) // döngüdeki gibi: her kontrolden sonra
	r.remote(fra, f.clock, up())
	fake.set(down("Zaman aşımı"))
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusUp {
		t.Fatalf("kural 'tümü': genel durum UP kalmalı: %d", r.m.Status)
	}
	if d := r.nextDelay(true); d > 20*time.Millisecond {
		t.Fatalf("ana sunucu tekrar denemesi %v sonra: en fazla 20 birim olmalı", d)
	}
	// Deneme hakkı bitince (konum çalışmıyor) normal aralığa dönülür.
	for range 3 {
		f.clock = f.clock.Add(10 * time.Millisecond)
		r.locationTick(ctx)
	}
	if r.retrying() {
		t.Fatal("deneme hakkı bitince normal aralık bekleniyordu")
	}
}

// Kontrol noktasının bağlantısı kopunca konumu eskime süresini beklemeden
// "sonuç yok" sayılır: kural "tümü" iken yalnızca çalışmayan ana sunucu
// kalır ve monitör hemen DOWN olur; yeniden bağlanınca eski haline döner.
func TestProbeDisconnectMarksLocationUnknown(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAll})
	defer fake.set(up())

	fake.set(down("403 Forbidden"))
	r.remote(fra, f.clock, up())
	r.locationTick(ctx)
	if r.m.Status != store.StatusUp {
		t.Fatalf("Frankfurt çalışırken genel durum UP olmalı: %d", r.m.Status)
	}
	f.e.SetProbeConnected(fra.ID, false)
	if !f.e.ProbeDisconnected(fra.ID) {
		t.Fatal("kopukluk kaydedilmedi")
	}
	if !r.locationWake() || r.m.Status != store.StatusDown {
		t.Fatalf("bağlantı kopunca hemen DOWN bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}
	if snap := *r.locs.snap.Load(); snap[1].Status != locUnknown {
		t.Fatalf("Frankfurt 'sonuç yok' olmalı: %+v", snap)
	}
	f.e.SetProbeConnected(fra.ID, true)
	if !r.locationWake() || r.m.Status != store.StatusUp {
		t.Fatalf("yeniden bağlanınca UP bekleniyordu: %d", r.m.Status)
	}
}
