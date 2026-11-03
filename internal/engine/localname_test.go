package engine

import (
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Ana sunucu konum adı ayardan gelir; boşsa varsayılan (LocalName).
func TestLocalLocationName(t *testing.T) {
	f := newFixture(t)
	if got := f.e.LocalLocationName(); got != LocalName {
		t.Fatalf("varsayılan ad %q, %q bekleniyordu", got, LocalName)
	}
	s := f.e.Settings()
	s.LocalLocationName = "DH-EU"
	f.e.SetSettings(s)
	if got := f.e.LocalLocationName(); got != "DH-EU" {
		t.Fatalf("ad %q", got)
	}
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, nil)
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAll})
	st, ok := f.e.LocationStatuses(m.ID)
	if !ok && r.locs != nil {
		st = r.locs.statuses(f.clock, r.rules())
	}
	if len(st) != 2 || st[0].ProbeID != LocalProbeID || st[0].Name != "DH-EU" {
		t.Fatalf("konum kartı: %+v", st)
	}
	// Tek konumlu monitörde olay kaydının konumu da ayardaki ad.
	r2 := f.runnerFor(t, f.monitor(t, nil).ID)
	r2.rememberRetry(f.clock, down("HTTP 503"))
	if len(r2.preDown) != 1 || r2.preDown[0].Location != "DH-EU" {
		t.Fatalf("deneme kaydı konumu: %+v", r2.preDown)
	}
}
