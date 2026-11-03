package store

import (
	"context"
	"testing"
)

func TestMeshTimingAndValidation(t *testing.T) {
	var a AppSettings
	mt := a.MeshTiming()
	if !mt.MeshEnabled || mt.PingSec != 5 || mt.LossThreshold != 3 || mt.PanelSec != 5 || mt.OfflineAfter().Seconds() != 15 {
		t.Fatalf("varsayılan %+v", mt)
	}
	ten := 10
	p := Probe{ProbeMesh: ProbeMesh{PanelSec: &ten}}
	if e := EffectiveProbeTiming(p, a); e.PanelSec != 10 || e.PingSec != 5 || e.OfflineAfter().Seconds() != 30 {
		t.Fatalf("ezme %+v", e)
	}
	bad := []ProbeTiming{{PingSec: 0, LossThreshold: 3, PanelSec: 5}, {PingSec: 61, LossThreshold: 3, PanelSec: 5},
		{PingSec: 5, LossThreshold: 11, PanelSec: 5}, {PingSec: 5, LossThreshold: 3, PanelSec: 61},
		{PingSec: 1, LossThreshold: 1, PanelSec: 5}, {PingSec: 5, LossThreshold: 2, PanelSec: 1}}
	for _, b := range bad {
		if ValidateProbeTiming(b) == nil {
			t.Errorf("kabul edilmemeli: %+v", b)
		}
	}
	if err := ValidateProbeTiming(ProbeTiming{PingSec: 1, LossThreshold: 3, PanelSec: 1}); err != nil {
		t.Error(err)
	}
	for in, ok := range map[string]bool{"": true, "-": true, "10.0.0.1:7480": true, "[::1]:7480": true, "h.example:1": true,
		"h": false, "h:0": false, ":7480": false, "a b:1": false} {
		if _, err := NormalizeMeshAddr(in); (err == nil) != ok {
			t.Errorf("%q: %v", in, err)
		}
	}
}

func TestMeshStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a := Probe{Name: "A", Active: true, CreatedAt: 1, Hash: "ha"}
	b := Probe{Name: "B", Active: true, CreatedAt: 1, Hash: "hb"}
	if err := s.CreateProbe(ctx, &a); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProbe(ctx, &b); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProbe(ctx, a.ID)
	if !got.FallbackNotifier || got.MeshPingSec != nil || got.MeshAddr != "" {
		t.Fatalf("varsayılan %+v", got.ProbeMesh)
	}
	seven := 7
	if err := s.SetProbeMesh(ctx, a.ID, ProbeMesh{MeshPingSec: &seven, MeshAddr: "10.0.0.1:7480"}); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProbe(ctx, a.ID)
	if got.MeshPingSec == nil || *got.MeshPingSec != 7 || got.FallbackNotifier || got.MeshAddr != "10.0.0.1:7480" {
		t.Fatalf("yazılan %+v", got.ProbeMesh)
	}
	k1, err := s.PairKey(ctx, b.ID, a.ID, 5)
	if err != nil || len(k1) != 64 {
		t.Fatal(k1, err)
	}
	if k2, _ := s.PairKey(ctx, a.ID, b.ID, 6); k2 != k1 {
		t.Fatal("çift anahtarı yönden bağımsız ve kalıcı olmalı")
	}
	if err := s.SetProbeToken(ctx, a.ID, "ha2", "upr_x"); err != nil {
		t.Fatal(err)
	}
	if k3, _ := s.PairKey(ctx, a.ID, b.ID, 7); k3 == k1 {
		t.Fatal("belirteç yenilenince anahtar değişmeli")
	}
	links := []ProbeLink{{Src: a.ID, Dst: b.ID, State: LinkUp, RTTms: 12, UpdatedAt: 10},
		{Src: MeshMainID, Dst: a.ID, State: LinkDown, LossStreak: 3, UpdatedAt: 10}}
	if err := s.UpsertProbeLinks(ctx, links); err != nil {
		t.Fatal(err)
	}
	links[0].RTTms = 20
	if err := s.UpsertProbeLinks(ctx, links[:1]); err != nil {
		t.Fatal(err)
	}
	ls, _ := s.ListProbeLinks(ctx)
	if len(ls) != 2 || ls[0].Src != 0 || ls[1].RTTms != 20 {
		t.Fatalf("bağlantılar %+v", ls)
	}
	if _, err := s.DeleteProbe(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if ls, _ := s.ListProbeLinks(ctx); len(ls) != 0 {
		t.Fatalf("silinen probe bağlantıları kalmamalı: %+v", ls)
	}
}
