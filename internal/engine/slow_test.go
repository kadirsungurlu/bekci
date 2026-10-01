package engine

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func upMs(ms int64) check.Result { return check.Result{Up: true, PingMs: ms, Message: "200 OK"} }

// Yavaş yanıt: son N başarılı kontrolün ortalaması eşiği aşınca 🟡 + degraded
// olayı; eşiğin %90 altına inince 🟢 ve olay kapanır. Uptime ve durum değişmez.
func TestSlowResponse(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.SlowMs, m.SlowChecks = 1000, 3 })
	r := f.runnerFor(t, m.ID)
	step := func(res check.Result) {
		f.clock = f.clock.Add(time.Minute)
		r.process(res)
	}
	step(upMs(1500))
	step(upMs(1500)) // pencere dolmadı
	if len(f.n.kinds()) != 0 {
		t.Fatalf("pencere dolmadan bildirim gitmemeli: %v", f.n.kinds())
	}
	step(upMs(1500)) // ortalama 1500 > 1000
	if got := f.n.kinds(); len(got) != 1 || got[0] != notify.KindSlow {
		t.Fatalf("yavaş bildirimi bekleniyordu: %v", got)
	}
	ev := f.n.events[0]
	if ev.Value != 1500 || ev.Threshold != 1000 || ev.Checks != 3 || ev.IncidentID == 0 || ev.Title() != "🟡 site yavaş yanıt veriyor" {
		t.Errorf("yavaş olayı: %+v", ev)
	}
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if !got.Slow || got.Status != store.StatusUp {
		t.Fatalf("monitör yavaş ve çalışıyor olmalı: slow=%v status=%d", got.Slow, got.Status)
	}
	incs, _ := f.st.ListIncidents(ctx, store.IncidentFilter{Kind: store.KindGroupDegraded})
	if len(incs) != 1 || incs[0].Kind != store.IncidentDegraded || incs[0].ResolvedAt != 0 ||
		incs[0].Cause != "Ortalama yanıt 1500 ms (son 3 kontrol, eşik 1000 ms)" {
		t.Fatalf("degraded olayı: %+v", incs)
	}
	if n, _ := f.st.CountIncidentsSince(ctx, 0); n != 0 {
		t.Error("yavaş yanıt kesinti sayılmamalı")
	}
	// Eşiğin biraz altı (950 ≥ %90): hâlâ yavaş, bildirim yok; olay verisi güncellenir.
	step(upMs(950))
	step(upMs(950))
	step(upMs(950))
	if len(f.n.kinds()) != 1 {
		t.Fatalf("histerezis: eşiğin %%90'ı üstünde bildirim gitmemeli: %v", f.n.kinds())
	}
	// Belirgin düzelme: pencere [950, 950, 200] → ortalama 700 < 900: 🟢 ve olay kapanır.
	step(upMs(200))
	kinds := f.n.kinds()
	if len(kinds) != 2 || kinds[1] != notify.KindSlowResolved {
		t.Fatalf("düzelme bildirimi bekleniyordu: %v", kinds)
	}
	if ev := f.n.events[1]; ev.Downtime != 4*time.Minute || ev.Value != 700 || ev.IncidentID != incs[0].ID ||
		ev.Title() != "🟢 site yanıt süresi normale döndü" {
		t.Errorf("düzelme olayı: %+v", ev)
	}
	step(upMs(200))
	step(upMs(200))
	if len(f.n.kinds()) != 2 {
		t.Fatalf("normalde bildirim gitmemeli: %v", f.n.kinds())
	}
	got, _ = f.st.GetMonitor(ctx, m.ID)
	incs, _ = f.st.ListIncidents(ctx, store.IncidentFilter{Kind: store.KindGroupDegraded})
	if got.Slow || len(incs) != 1 || incs[0].ResolvedAt == 0 {
		t.Fatalf("olay kapanmalı: slow=%v %+v", got.Slow, incs)
	}
	d := store.ParseDegradedIncidentData(incs[0].Data)
	if d.PeakMs != 1500 || d.LastMs != 700 {
		t.Errorf("olay verisi: %+v", d)
	}
}

// Kesintiye dönüşen yavaşlık: degraded olayı bildirimsiz kapanır; yeniden
// başlatmada açık olay tanınır; eşik kaldırılınca olay kapanır.
func TestSlowResponseOutageAndRestore(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.SlowMs, m.SlowChecks, m.MaxRetries = 1000, 2, 0 })
	r := f.runnerFor(t, m.ID)
	step := func(r *runner, res check.Result) {
		f.clock = f.clock.Add(time.Minute)
		r.process(res)
	}
	step(r, upMs(2000))
	step(r, upMs(2000))
	if got := f.n.kinds(); len(got) != 1 || got[0] != notify.KindSlow {
		t.Fatalf("yavaş bildirimi: %v", got)
	}
	// Yeniden başlatma: açık degraded olayı ve yavaş durumu korunur.
	r2 := f.runnerFor(t, m.ID)
	if r2.slowID == 0 || !r2.m.Slow {
		t.Fatalf("yeniden başlatmada yavaş durumu korunmalı: id=%d slow=%v", r2.slowID, r2.m.Slow)
	}
	// Kesinti: 🔴 gider, degraded olayı sessizce kapanır.
	step(r2, down("HTTP 503"))
	kinds := f.n.kinds()
	if len(kinds) != 2 || kinds[1] != notify.KindDown {
		t.Fatalf("kesintide yalnızca 🔴 gitmeli: %v", kinds)
	}
	incs, _ := f.st.ListIncidents(ctx, store.IncidentFilter{Kind: store.KindGroupDegraded})
	if len(incs) != 1 || incs[0].ResolvedAt == 0 {
		t.Fatalf("degraded olayı kapanmalı: %+v", incs)
	}
	evs, _ := f.st.IncidentEvents(ctx, incs[0].ID)
	if len(evs) == 0 || evs[0].Kind != store.EventUp || evs[0].Message != "Kesintiye dönüştü; yavaş yanıt olayı kapatıldı" {
		t.Errorf("kapanış kaydı: %+v", evs)
	}
	got, _ := f.st.GetMonitor(ctx, m.ID)
	if got.Slow {
		t.Error("kesintide yavaş bayrağı kalkmalı")
	}
	// Düzelme sonrası yeniden yavaşlar; eşik kaldırılırsa olay kapanır, bildirim yok.
	step(r2, upMs(3000))
	step(r2, upMs(3000))
	if kinds := f.n.kinds(); len(kinds) != 4 || kinds[3] != notify.KindSlow {
		t.Fatalf("yeniden yavaş: %v", kinds)
	}
	r2.m.SlowMs = 0
	step(r2, upMs(3000))
	if kinds := f.n.kinds(); len(kinds) != 4 {
		t.Fatalf("eşik kaldırılınca bildirim gitmemeli: %v", kinds)
	}
	if id, _ := f.st.OpenDegradedIncidentID(ctx, m.ID); id != 0 {
		t.Error("eşik kaldırılınca olay kapanmalı")
	}
}
