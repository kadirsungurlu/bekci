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
	}
	var out resp
	f.mustDo("GET", "/api/mesh", nil, &out, 200)
	if out.Active || len(out.Nodes) != 0 {
		t.Fatalf("etkin olmamalı: %+v", out)
	}
	a, b := f.newProbe("A"), f.newProbe("B")
	f.heartbeat(a.Token, probe.MeshHeartbeat{Links: []probe.MeshLinkReport{{Peer: b.Probe.ID, State: store.LinkUp}}})
	f.heartbeat(b.Token, probe.MeshHeartbeat{Links: []probe.MeshLinkReport{{Peer: a.Probe.ID, State: store.LinkDown}}})
	f.mustDo("GET", "/api/mesh", nil, &out, 200)
	if !out.Active || len(out.Nodes) != 3 || len(out.Pairs) != 3 || len(out.Findings) != 1 {
		t.Fatalf("matris: %+v", out)
	}
}
