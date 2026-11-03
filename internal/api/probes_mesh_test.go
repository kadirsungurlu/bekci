package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func (f *fenv) heartbeat(token string, hb probe.MeshHeartbeat) probe.MeshHeartbeatResponse {
	f.t.Helper()
	code, _, body := f.rawReq("POST", "/api/probe/mesh", bearer(token), hb)
	if code != 200 {
		f.t.Fatalf("kalp atışı: %d %s", code, body)
	}
	var resp probe.MeshHeartbeatResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		f.t.Fatal(err)
	}
	return resp
}

func (f *fenv) peers(token string) probe.MeshPeersResponse {
	f.t.Helper()
	code, _, body := f.rawReq("GET", "/api/probe/mesh/peers", bearer(token), nil)
	if code != 200 {
		f.t.Fatalf("eşler: %d %s", code, body)
	}
	var resp probe.MeshPeersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		f.t.Fatal(err)
	}
	return resp
}

func linkOf(links []store.ProbeLink, src, dst int64) (store.ProbeLink, bool) {
	for _, l := range links {
		if l.Src == src && l.Dst == dst {
			return l, true
		}
	}
	return store.ProbeLink{}, false
}

// Kalp atışı Main <-> probe kenarlarını ve yalnız gerçek eşlere giden
// ölçümleri yazar; etkin zamanlamayı ve eş listesi sürümünü döner.
func TestProbeMeshHeartbeat(t *testing.T) {
	f := newFeatureEnv(t)
	ctx := context.Background()
	a, b := f.newProbe("A"), f.newProbe("B")
	resp := f.heartbeat(a.Token, probe.MeshHeartbeat{MainLoss: 2, ListenPort: 7480, Links: []probe.MeshLinkReport{
		{Peer: b.Probe.ID, State: store.LinkUp, RTTms: 12, LastOKAt: 100},
		{Peer: 9999, State: store.LinkUp},         // bilinmeyen eş
		{Peer: b.Probe.ID + 1000, State: "garip"}, // geçersiz
		{Peer: a.Probe.ID, State: store.LinkUp},   // kendisi
	}})
	if resp.Timing.PanelSec != store.DefaultProbePanelSec || resp.Timing.LossThreshold != store.DefaultMeshLoss ||
		!resp.Timing.MeshEnabled || resp.ServerTime == 0 || resp.PeersVersion == "" {
		t.Fatalf("yanıt: %+v", resp)
	}
	links, _ := f.st.ListProbeLinks(ctx)
	if len(links) != 3 {
		t.Fatalf("3 kenar bekleniyordu: %+v", links)
	}
	if l, ok := linkOf(links, store.MeshMainID, a.Probe.ID); !ok || l.State != store.LinkUp || l.LastOKAt == 0 || l.LossStreak != 0 {
		t.Errorf("Main -> A: %+v", l)
	}
	if l, ok := linkOf(links, a.Probe.ID, store.MeshMainID); !ok || l.State != store.LinkUp || l.LossStreak != 2 {
		t.Errorf("A -> Main: %+v", l)
	}
	if l, ok := linkOf(links, a.Probe.ID, b.Probe.ID); !ok || l.RTTms != 12 || l.LastOKAt != 100 {
		t.Errorf("A -> B: %+v", l)
	}
	if !f.s.engine.ProbeOnline(mustProbe(t, f, a.Probe.ID), f.s.now()) {
		t.Error("kalp atışı sonrası çevrimiçi olmalı")
	}

	// Probe başına panel aralığı yanıta yansır.
	panel := 10
	if err := f.st.SetProbeMesh(ctx, a.Probe.ID, store.ProbeMesh{PanelSec: &panel, FallbackNotifier: true}); err != nil {
		t.Fatal(err)
	}
	if resp := f.heartbeat(a.Token, probe.MeshHeartbeat{}); resp.Timing.PanelSec != 10 {
		t.Errorf("probe ezmesi: %+v", resp.Timing)
	}
	// Bozuk gövde.
	if code, _, _ := f.rawReq("POST", "/api/probe/mesh", bearer(a.Token), "x"); code != 400 {
		t.Errorf("bozuk gövde: %d", code)
	}
}

func mustProbe(t *testing.T, f *fenv, id int64) store.Probe {
	t.Helper()
	p, err := f.st.GetProbe(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// Eş listesi: yalnız kendi çiftleri, adres kuralları, sürüm değişimi, silinen
// ve pasif probe'ların listeden çıkması, mesh kapalıyken boş liste.
func TestProbeMeshPeers(t *testing.T) {
	f := newFeatureEnv(t)
	ctx := context.Background()
	a, b, c := f.newProbe("A"), f.newProbe("B"), f.newProbe("C")
	pa, pb := f.peers(a.Token), f.peers(b.Token)
	if len(pa.Peers) != 2 || pa.Peers[0].ID != b.Probe.ID || pa.Peers[1].ID != c.Probe.ID {
		t.Fatalf("A eşleri: %+v", pa)
	}
	// Çift anahtarı iki uçta aynı, farklı çiftlerde farklı.
	if pa.Peers[0].Key == "" || pa.Peers[0].Key != pb.Peers[0].Key || pa.Peers[0].Key == pa.Peers[1].Key {
		t.Errorf("anahtarlar: A=%+v B=%+v", pa.Peers, pb.Peers)
	}
	for _, p := range pa.Peers {
		if p.Addr != "" {
			t.Errorf("port bildirilmeden adres boş olmalı: %+v", p)
		}
	}
	// B port bildirir: görülen IP + port; A'nın sürümü değişir.
	f.heartbeat(b.Token, probe.MeshHeartbeat{ListenPort: 7480})
	pa2 := f.peers(a.Token)
	if pa2.Peers[0].Addr != "127.0.0.1:7480" || pa2.Version == pa.Version {
		t.Fatalf("B adresi/sürüm: %+v (eski %s)", pa2, pa.Version)
	}
	if v := f.heartbeat(a.Token, probe.MeshHeartbeat{}).PeersVersion; v != pa2.Version {
		t.Errorf("kalp atışı sürümü %s, eş listesi %s", v, pa2.Version)
	}
	// Elle adres ve "-" (dinlemiyor).
	f.st.SetProbeMesh(ctx, b.Probe.ID, store.ProbeMesh{MeshAddr: "mesh.ornek:9000", FallbackNotifier: true})
	f.st.SetProbeMesh(ctx, c.Probe.ID, store.ProbeMesh{MeshAddr: "-", FallbackNotifier: true})
	f.heartbeat(c.Token, probe.MeshHeartbeat{ListenPort: 7480})
	pa3 := f.peers(a.Token)
	if pa3.Peers[0].Addr != "mesh.ornek:9000" || pa3.Peers[1].Addr != "" {
		t.Fatalf("adres kuralları: %+v", pa3.Peers)
	}
	// Anahtar değişince sürüm değişir.
	f.st.DropProbePairKeys(ctx, b.Probe.ID)
	pa4 := f.peers(a.Token)
	if pa4.Peers[0].Key == pa3.Peers[0].Key || pa4.Version == pa3.Version {
		t.Error("anahtar yenilenince sürüm değişmeli")
	}
	// Pasif probe listeden çıkar.
	f.mustDo("PUT", fmt.Sprintf("/api/probes/%d", c.Probe.ID), map[string]any{"name": "C", "active": false}, nil, 200)
	if pa5 := f.peers(a.Token); len(pa5.Peers) != 1 || pa5.Peers[0].ID != b.Probe.ID || pa5.Version == pa4.Version {
		t.Fatalf("pasif probe: %+v", pa5)
	}
	// Silinen probe: listeden çıkar, bağlantı ve anahtarları silinir.
	f.heartbeat(a.Token, probe.MeshHeartbeat{Links: []probe.MeshLinkReport{{Peer: b.Probe.ID, State: store.LinkUp}}})
	f.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", b.Probe.ID), nil, nil, 200)
	if pa6 := f.peers(a.Token); len(pa6.Peers) != 0 {
		t.Fatalf("silinen probe: %+v", pa6)
	}
	links, _ := f.st.ListProbeLinks(ctx)
	if _, ok := linkOf(links, a.Probe.ID, b.Probe.ID); ok {
		t.Error("silinen probe'un kenarı kalmamalı")
	}
	if code, _, _ := f.rawReq("GET", "/api/probe/mesh/peers", bearer(b.Token), nil); code != 401 {
		t.Errorf("silinen probe token'ı: %d", code)
	}
	// Mesh kapalı: boş liste.
	set, _ := f.st.LoadSettings(ctx)
	off := false
	set.MeshEnabled = &off
	f.s.engine.SetSettings(set)
	f.newProbe("D")
	if p := f.peers(a.Token); len(p.Peers) != 0 {
		t.Errorf("mesh kapalıyken eş olmamalı: %+v", p)
	}
}
