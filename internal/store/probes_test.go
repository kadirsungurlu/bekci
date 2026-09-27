package store

import (
	"context"
	"errors"
	"testing"
)

func TestProbeStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, b := newMonitor(t, s, "A"), newMonitor(t, s, "B")

	p := Probe{Name: "Frankfurt", Active: true, CreatedAt: 100, Hash: "h1", TokenPrefix: "upr_1234"}
	if err := s.CreateProbe(ctx, &p); err != nil || p.ID == 0 {
		t.Fatal(p.ID, err)
	}
	q := Probe{Name: "istanbul", Active: true, CreatedAt: 100, Hash: "h2"}
	if err := s.CreateProbe(ctx, &q); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateProbe(ctx, &Probe{Name: "x", Hash: "h1"}); !IsUniqueViolation(err) {
		t.Fatalf("aynı token özeti: %v", err)
	}
	if n, _ := s.CountProbes(ctx); n != 2 {
		t.Fatalf("sayı %d", n)
	}
	if taken, _ := s.ProbeNameTaken(ctx, "FRANKFURT", 0); !taken {
		t.Error("ad çakışması büyük/küçük harf duyarsız olmalı")
	}
	if taken, _ := s.ProbeNameTaken(ctx, "frankfurt", p.ID); taken {
		t.Error("kendi adı çakışma sayılmamalı")
	}
	list, _ := s.ListProbes(ctx)
	if len(list) != 2 || list[0].Name != "Frankfurt" || list[1].Name != "istanbul" {
		t.Fatalf("sıralama: %+v", list)
	}

	got, err := s.ProbeByTokenHash(ctx, "h1")
	if err != nil || got.ID != p.ID || !got.Active || got.LastSeenAt != 0 {
		t.Fatalf("özetten bulma: %+v %v", got, err)
	}
	if err := s.TouchProbe(ctx, p.ID, 500, "10.0.0.1", "v1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProbeToken(ctx, p.ID, "h3", "upr_9999"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ProbeByTokenHash(ctx, "h1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("eski token: %v", err)
	}
	if err := s.UpdateProbe(ctx, p.ID, "Frankfurt-1", false); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProbe(ctx, p.ID)
	if got.Name != "Frankfurt-1" || got.Active || got.LastSeenAt != 500 || got.LastIP != "10.0.0.1" || got.Version != "v1" || got.TokenPrefix != "upr_9999" {
		t.Fatalf("güncelleme: %+v", got)
	}
	if err := s.UpdateProbe(ctx, 999, "x", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("olmayan: %v", err)
	}

	// Konumlar.
	if err := s.SetMonitorLocations(ctx, a.ID, LocationSetup{IncludeLocal: false, ProbeIDs: []int64{q.ID, p.ID}, DownWhen: DownWhenAll}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMonitorLocations(ctx, b.ID, LocationSetup{IncludeLocal: true, ProbeIDs: []int64{q.ID}, DownWhen: DownWhenAny}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.MonitorLocations(ctx, a.ID)
	if l.IncludeLocal || len(l.ProbeIDs) != 2 || l.ProbeIDs[0] != p.ID || l.DownWhen != DownWhenAll {
		t.Fatalf("a: %+v", l)
	}
	// Yeniden yazma (ON CONFLICT DO UPDATE).
	s.SetMonitorLocations(ctx, a.ID, LocationSetup{IncludeLocal: true, ProbeIDs: []int64{p.ID}, DownWhen: DownWhenMajority})
	all, _ := s.AllMonitorLocations(ctx)
	if len(all) != 2 || !all[a.ID].IncludeLocal || len(all[a.ID].ProbeIDs) != 1 || all[a.ID].DownWhen != DownWhenMajority {
		t.Fatalf("hepsi: %+v", all)
	}
	counts, _ := s.ProbeMonitorCounts(ctx)
	if counts[p.ID] != 1 || counts[q.ID] != 1 {
		t.Fatalf("sayılar %v", counts)
	}
	byID, _ := s.ProbesByIDs(ctx, []int64{p.ID, 12345})
	if len(byID) != 1 || byID[p.ID].Name != "Frankfurt-1" {
		t.Fatalf("kimlikle: %+v", byID)
	}

	// İş listesi yalnızca aktif, atanmış monitörler.
	jobs, _ := s.ProbeJobs(ctx, q.ID)
	if len(jobs) != 1 || jobs[0].ID != b.ID || string(jobs[0].Config) != `{"url":"https://example.com"}` {
		t.Fatalf("işler: %+v", jobs)
	}
	s.SetMonitorActive(ctx, b.ID, false)
	if jobs, _ := s.ProbeJobs(ctx, q.ID); len(jobs) != 0 {
		t.Fatalf("durdurulan monitör: %+v", jobs)
	}

	// Silme: b yalnızca ana sunucuya döner (satır silinir), a'da p kalır.
	affected, err := s.DeleteProbe(ctx, q.ID)
	if err != nil || len(affected) != 1 || affected[0] != b.ID {
		t.Fatalf("silme: %v %v", affected, err)
	}
	all, _ = s.AllMonitorLocations(ctx)
	if len(all) != 1 || len(all[a.ID].ProbeIDs) != 1 {
		t.Fatalf("silmeden sonra: %+v", all)
	}
	if _, err := s.DeleteProbe(ctx, q.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("iki kez silme: %v", err)
	}
	// Monitör silinince konum satırları da gider.
	s.DeleteMonitor(ctx, a.ID)
	if all, _ := s.AllMonitorLocations(ctx); len(all) != 0 {
		t.Fatalf("monitör silindi: %+v", all)
	}
	if ids, _ := s.ProbeMonitorIDs(ctx, p.ID); len(ids) != 0 {
		t.Fatalf("atamalar: %v", ids)
	}
}
