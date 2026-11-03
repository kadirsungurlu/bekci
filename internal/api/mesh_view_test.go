package api

import (
	"testing"

	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// GET /api/mesh: kalp atışı gelmeden etkin değil; iki nokta raporlayınca
// Main dahil 3 düğüm, 3 çift; tek yönlü kopukluk yorumlanır.
func TestMeshMatrix(t *testing.T) {
	f := newFeatureEnv(t)
	type resp struct {
		Active bool                 `json:"active"`
		Nodes  []struct{ ID int64 } `json:"nodes"`
		Pairs  []struct {
			A, B  int64
			State string
		} `json:"pairs"`
		Findings []string `json:"findings"`
		Links    []struct {
			Src, Dst   int64
			State      string
			RTT        *int64 `json:"rtt_ms"`
			LossStreak *int   `json:"loss_streak"`
			LastOKAt   *int64 `json:"last_ok_at"`
		} `json:"links"`
	}
	var out resp
	f.mustDo("GET", "/api/mesh", nil, &out, 200)
	if out.Active || len(out.Nodes) != 0 {
		t.Fatalf("etkin olmamalı: %+v", out)
	}
	a, b := f.newProbe("A"), f.newProbe("B")
	f.heartbeat(a.Token, probe.MeshHeartbeat{Links: []probe.MeshLinkReport{{Peer: b.Probe.ID, State: store.LinkUp, RTTms: 12, LastOKAt: 1700000000}}})
	f.heartbeat(b.Token, probe.MeshHeartbeat{Links: []probe.MeshLinkReport{{Peer: a.Probe.ID, State: store.LinkDown, LossStreak: 4}}})
	f.mustDo("GET", "/api/mesh", nil, &out, 200)
	if !out.Active || len(out.Nodes) != 3 || len(out.Pairs) != 3 || len(out.Findings) != 1 {
		t.Fatalf("matris: %+v", out)
	}
	// Yönlü bağlantılar: çift başına iki; ayrıntı yalnız noktalar arası ölçümde.
	if len(out.Links) != 6 {
		t.Fatalf("bağlantılar: %+v", out.Links)
	}
	var ab, ba, mainA bool
	for _, l := range out.Links {
		switch {
		case l.Src == a.Probe.ID && l.Dst == b.Probe.ID:
			ab = l.State == store.LinkUp && l.RTT != nil && *l.RTT == 12 && *l.LastOKAt == 1700000000
		case l.Src == b.Probe.ID && l.Dst == a.Probe.ID:
			ba = l.State == store.LinkDown && l.LossStreak != nil && *l.LossStreak == 4
		case l.Src == 0 && l.Dst == a.Probe.ID:
			mainA = l.State == store.LinkUp && l.RTT == nil
		}
	}
	if !ab || !ba || !mainA {
		t.Fatalf("yönlü ayrıntı: %+v", out.Links)
	}
}
