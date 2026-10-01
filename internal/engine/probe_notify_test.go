package engine

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kontrol noktası çevrimdışı bildirimi: "çevrimdışında bildir" açık kontrol
// noktası 90 sn istek göndermeyince 🔴 bildirim + probe_offline olayı; tekrar
// görülünce 🟢 ve olay kapanır. Kapalı kontrol noktasında hiçbiri olmaz; ilk
// tarama (açılış) bildirim üretmez.
func TestProbeOfflineNotification(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	p := f.probe(t, "Frankfurt", true)
	quiet := f.probe(t, "Sessiz", true)
	if err := f.st.SetProbeNotifyOffline(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1")
	f.st.TouchProbe(ctx, quiet.ID, f.clock.Unix(), "10.0.0.2", "v1")
	known := f.e.scanProbes(ctx, nil) // ilk tarama: yalnızca öğrenir
	f.clock = f.clock.Add(ProbeOfflineAfter / 2)
	known = f.e.scanProbes(ctx, known) // tolerans dolmadı
	if kinds := f.n.kinds(); len(kinds) != 0 {
		t.Fatalf("tolerans dolmadan bildirim gitmemeli: %v", kinds)
	}
	f.clock = f.clock.Add(ProbeOfflineAfter)
	known = f.e.scanProbes(ctx, known)
	evs := f.n.events
	if len(evs) != 1 || evs[0].Kind != notify.KindProbeOffline || evs[0].ProbeID != p.ID || evs[0].MonitorName != "Frankfurt" || evs[0].IncidentID == 0 {
		t.Fatalf("çevrimdışı bildirimi: %+v", evs)
	}
	id, _ := f.st.OpenProbeIncidentID(ctx, p.ID)
	if id == 0 || id != evs[0].IncidentID {
		t.Fatalf("probe_offline olayı açılmalı: %d", id)
	}
	if qid, _ := f.st.OpenProbeIncidentID(ctx, quiet.ID); qid != 0 {
		t.Error("bildirimi kapalı kontrol noktasında olay açılmamalı")
	}
	list, _ := f.st.ListIncidents(ctx, store.IncidentFilter{Kind: store.KindGroupServer})
	if len(list) != 1 || list[0].Kind != store.IncidentProbeOffline || list[0].ServerID != p.ID || list[0].ServerName != "Frankfurt" {
		t.Errorf("olay listesi: %+v", list)
	}
	if got := store.ServerIncidentCause("en", list[0].Kind, store.ParseServerIncidentData(list[0].Data)); got != "Check location unreachable" {
		t.Errorf("neden: %q", got)
	}
	// Aynı durumda ikinci tarama yeni bildirim üretmez.
	f.clock = f.clock.Add(10 * time.Second)
	known = f.e.scanProbes(ctx, known)
	if len(f.n.kinds()) != 1 {
		t.Fatalf("tekrar bildirim gitmemeli: %v", f.n.kinds())
	}
	// Tekrar görüldü: olay kapanır, 🟢 gider (kesinti süresiyle).
	f.st.TouchProbe(ctx, p.ID, f.clock.Unix(), "10.0.0.1", "v1")
	f.e.scanProbes(ctx, known)
	evs = f.n.events
	if len(evs) != 2 || evs[1].Kind != notify.KindProbeOnline || evs[1].IncidentID != id || evs[1].Downtime <= 0 {
		t.Fatalf("çevrimiçi bildirimi: %+v", evs)
	}
	if open, _ := f.st.OpenProbeIncidentID(ctx, p.ID); open != 0 {
		t.Error("olay kapanmalı")
	}
	if evs[0].Title() == "" || evs[0].AlertKey() != evs[1].AlertKey() || !evs[0].IsProblem() || !evs[1].IsRecovery() {
		t.Errorf("olay anahtarı/başlık: %q %q", evs[0].AlertKey(), evs[1].AlertKey())
	}
}
