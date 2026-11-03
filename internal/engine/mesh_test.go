package engine

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/mesh"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Üç kontrol noktası; C'nin ajanı durur: tekil probe_offline gitmez, bir panel
// aralığı sonra tek mesh bildirimi ("C genel erişim sorunu") gider; C dönünce
// mesh_resolved. Durmuş C'nin eski "up" raporları ölçülemiyor sayılır.
func TestMeshNodeVerdict(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	var ps []store.Probe
	for _, n := range []string{"A", "B", "C"} {
		p := f.probe(t, n, true)
		if err := f.st.SetProbeNotifyOffline(ctx, p.ID, true); err != nil {
			t.Fatal(err)
		}
		ps = append(ps, p)
	}
	a, b, c := ps[0].ID, ps[1].ID, ps[2].ID
	// seen: eşlerin "up" gördüğü düğümler (ajan durunca eşler 3 ping = 15 sn
	// sonra kopuk raporlar; Main de 15 sn'de çevrimdışı der).
	beatSeen := func(alive, seen []int64) {
		ts := f.clock.Unix()
		var links []store.ProbeLink
		for _, src := range alive {
			f.st.TouchProbe(ctx, src, ts, "10.0.0.1", "v1", "")
			f.e.ProbeHeartbeat(src, f.clock)
			for _, dst := range []int64{a, b, c} {
				if dst == src {
					continue
				}
				st := store.LinkDown
				for _, x := range seen {
					if x == dst {
						st = store.LinkUp
					}
				}
				links = append(links, store.ProbeLink{Src: src, Dst: dst, State: st, UpdatedAt: ts})
			}
		}
		if err := f.st.UpsertProbeLinks(ctx, links); err != nil {
			t.Fatal(err)
		}
	}
	beat := func(alive ...int64) { beatSeen(alive, alive) }
	beat(a, b, c)
	known := f.e.scanProbes(ctx, nil)
	// Isınma (eşik + panel aralığı = 20 sn) boyunca karar verilmez.
	for i := 0; i < 12; i++ {
		f.clock = f.clock.Add(2 * time.Second)
		beat(a, b, c)
		known = f.e.scanProbes(ctx, known)
	}
	if k := f.n.kinds(); len(k) != 0 {
		t.Fatalf("sağlıklı ağda bildirim: %v", k)
	}
	for i := 0; i < 15; i++ { // 30 sn: C sustu
		f.clock = f.clock.Add(2 * time.Second)
		if i < 7 {
			beatSeen([]int64{a, b}, []int64{a, b, c})
			known = f.e.scanProbes(ctx, known)
			continue
		}
		beat(a, b)
		known = f.e.scanProbes(ctx, known)
	}
	k := f.n.kinds()
	if len(k) != 1 || k[0] != notify.KindMesh {
		t.Fatalf("tek mesh bildirimi beklenirdi: %v", k)
	}
	ev := f.n.events[0]
	if ev.Mesh == nil || ev.Mesh.Verdict.Key() != "node:"+strconv.FormatInt(c, 10)+";" || len(ev.MeshProbes) != 1 || ev.MeshProbes[0] != c {
		t.Fatalf("yorum: %+v %v", ev.Mesh, ev.MeshProbes)
	}
	if len(ev.Image) < 8 || string(ev.Image[1:4]) != "PNG" {
		t.Fatalf("mesh bildiriminde şema PNG'si yok (%d bayt)", len(ev.Image))
	}
	if p := ev.Mesh.Verdict.Pair(a, c); p.State != mesh.PairDown {
		t.Fatalf("A-C çifti: %+v", p)
	}
	if id, _, _ := f.st.OpenMeshIncidentData(ctx); id == 0 || id != ev.IncidentID {
		t.Fatalf("açık mesh olayı: %d", id)
	}
	for i := 0; i < 6; i++ {
		f.clock = f.clock.Add(2 * time.Second)
		beat(a, b, c)
		known = f.e.scanProbes(ctx, known)
	}
	if k := f.n.kinds(); len(k) != 2 || k[1] != notify.KindMeshResolved {
		t.Fatalf("düzelme: %v", k)
	}
	if id, _, _ := f.st.OpenMeshIncidentData(ctx); id != 0 {
		t.Fatal("mesh olayı kapanmalı")
	}
}
