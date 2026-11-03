package engine

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kalp atışı gönderen ajan, son kalp atışından beri eşik (vars. 3 x 5 = 15 sn)
// geçince çevrimdışı olur: Main -> probe kenarı "down" yazılır, konum kopuk
// sayılır, probe_offline akışı çalışır. Yeni kalp atışıyla düzelir.
func TestHeartbeatOffline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.probe(t, "Frankfurt", true)
	if err := f.st.SetProbeNotifyOffline(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1", "")
	f.e.ProbeHeartbeat(p.ID, f.clock)
	known := f.e.scanProbes(ctx, nil)
	if !known[p.ID] {
		t.Fatal("kalp atışı yeni: çevrimiçi olmalı")
	}
	// Uzun yoklama sürse de (son görülme tazelenir) kalp atışı belirleyicidir.
	f.clock = f.clock.Add(14 * time.Second)
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1", "")
	known = f.e.scanProbes(ctx, known)
	if !known[p.ID] || len(f.n.kinds()) != 0 {
		t.Fatalf("eşik dolmadı: %v %v", known, f.n.kinds())
	}
	f.clock = f.clock.Add(2 * time.Second) // son kalp atışından 16 sn
	known = f.e.scanProbes(ctx, known)
	if known[p.ID] {
		t.Fatal("15 sn sonra çevrimdışı olmalı")
	}
	if k := f.n.kinds(); len(k) != 1 || k[0] != notify.KindProbeOffline {
		t.Fatalf("çevrimdışı bildirimi: %v", k)
	}
	if !f.e.ProbeDisconnected(p.ID) {
		t.Error("kenar kopunca konum kopuk sayılmalı")
	}
	links, _ := f.st.ListProbeLinks(ctx)
	if len(links) != 1 || links[0].Src != store.MeshMainID || links[0].Dst != p.ID || links[0].State != store.LinkDown ||
		links[0].LossStreak != 3 || links[0].LastOKAt != f.clock.Add(-16*time.Second).Unix() {
		t.Fatalf("Main -> probe kenarı: %+v", links)
	}

	// Probe başına daha uzun panel aralığı eşiği büyütür.
	panel := 10
	if err := f.st.SetProbeMesh(ctx, p.ID, store.ProbeMesh{PanelSec: &panel, FallbackNotifier: true}); err != nil {
		t.Fatal(err)
	}
	p2, _ := f.st.GetProbe(ctx, p.ID)
	if !f.e.ProbeOnline(p2, f.clock) {
		t.Error("30 sn eşikte 16 sn sessizlik çevrimiçi sayılmalı")
	}

	// Yeni kalp atışı: kenar kalkar, düzelme gider.
	f.e.ProbeHeartbeat(p.ID, f.clock)
	if f.e.ProbeDisconnected(p.ID) {
		t.Error("kalp atışıyla kopukluk kalkmalı")
	}
	f.e.scanProbes(ctx, known)
	if k := f.n.kinds(); len(k) != 2 || k[1] != notify.KindProbeOnline {
		t.Fatalf("düzelme bildirimi: %v", k)
	}
}

// Kalp atışı hiç göndermeyen eski ajan: son görülme + 90 sn yedek kuralı.
func TestLegacyProbeOfflineFallback(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.probe(t, "Eski", true)
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1", "")
	known := f.e.scanProbes(ctx, nil)
	f.clock = f.clock.Add(60 * time.Second)
	known = f.e.scanProbes(ctx, known)
	if !known[p.ID] {
		t.Fatal("eski ajan 60 sn sessizlikte çevrimiçi kalmalı")
	}
	f.clock = f.clock.Add(31 * time.Second)
	known = f.e.scanProbes(ctx, known)
	if known[p.ID] {
		t.Fatal("eski ajan 90 sn sonra çevrimdışı olmalı")
	}
	if links, _ := f.st.ListProbeLinks(ctx); len(links) != 0 {
		t.Errorf("eski ajan için kenar yazılmamalı: %+v", links)
	}
}

// Açılışta son kalp atışı probe_links'ten okunur; kalp atışını bırakan
// (eski sürüme dönen) ajan yedek kurala düşer.
func TestHeartbeatLoadAndDowngrade(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.probe(t, "Yeni", true)
	if err := f.st.UpsertProbeLinks(ctx, []store.ProbeLink{{Src: store.MeshMainID, Dst: p.ID, State: store.LinkUp,
		LastOKAt: f.clock.Unix(), UpdatedAt: f.clock.Unix()}}); err != nil {
		t.Fatal(err)
	}
	f.e.loadHeartbeats(ctx)
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1", "")
	p, _ = f.st.GetProbe(ctx, p.ID)
	if !f.e.ProbeOnline(p, f.clock.Add(10*time.Second)) || f.e.ProbeOnline(p, f.clock.Add(20*time.Second)) {
		t.Fatal("yüklenen kalp atışı 15 sn eşikle değerlendirilmeli")
	}
	// Ajan 100 sn boyunca yalnız uzun yoklama yaptı: yedek kural.
	f.st.TouchProbe(ctx, p.ID, f.clock.Add(100*time.Second).Unix(), "10.0.0.1", "v0", "")
	p, _ = f.st.GetProbe(ctx, p.ID)
	if !f.e.ProbeOnline(p, f.clock.Add(105*time.Second)) {
		t.Error("kalp atışını bırakan ajan son görülmeye göre çevrimiçi olmalı")
	}
}
