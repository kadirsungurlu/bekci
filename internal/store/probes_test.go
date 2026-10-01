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
	if n, _ := s.CountProbes(ctx, ProbeKindLocation); n != 2 {
		t.Fatalf("sayı %d", n)
	}
	if taken, _ := s.ProbeNameTaken(ctx, "FRANKFURT", ProbeKindLocation, 0); !taken {
		t.Error("ad çakışması büyük/küçük harf duyarsız olmalı")
	}
	if taken, _ := s.ProbeNameTaken(ctx, "frankfurt", ProbeKindLocation, p.ID); taken {
		t.Error("kendi adı çakışma sayılmamalı")
	}
	if taken, _ := s.ProbeNameTaken(ctx, "Frankfurt", ProbeKindServer, 0); taken {
		t.Error("sunucu ile kontrol noktası aynı adı taşıyabilmeli")
	}
	if p.Kind != ProbeKindLocation || p.Metrics {
		t.Fatalf("varsayılan tür kontrol noktası, metrik kapalı olmalı: %+v", p)
	}
	srv := Probe{Kind: ProbeKindServer, Name: "Frankfurt", Active: true, CreatedAt: 100, Hash: "hs"}
	if err := s.CreateProbe(ctx, &srv); err != nil || !srv.Metrics {
		t.Fatalf("sunucu: %+v %v", srv, err)
	}
	list, _ := s.ListProbesOfKind(ctx, ProbeKindLocation)
	if len(list) != 2 || list[0].Name != "Frankfurt" || list[1].Name != "istanbul" || list[0].Kind != ProbeKindLocation {
		t.Fatalf("sıralama: %+v", list)
	}
	if list, _ := s.ListProbesOfKind(ctx, ProbeKindServer); len(list) != 1 || list[0].ID != srv.ID {
		t.Fatalf("sunucular: %+v", list)
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
	if err := s.SetMonitorLocations(ctx, a.ID, LocationSetup{IncludeLocal: false, ProbeIDs: []int64{q.ID, p.ID}, DownWhen: DownWhenAll, NotifyPartial: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMonitorLocations(ctx, b.ID, LocationSetup{IncludeLocal: true, ProbeIDs: []int64{q.ID}, DownWhen: DownWhenAny}); err != nil {
		t.Fatal(err)
	}
	l, _ := s.MonitorLocations(ctx, a.ID)
	if l.IncludeLocal || len(l.ProbeIDs) != 2 || l.ProbeIDs[0] != p.ID || l.DownWhen != DownWhenAll || !l.NotifyPartial {
		t.Fatalf("a: %+v", l)
	}
	// Yeniden yazma (ON CONFLICT DO UPDATE).
	s.SetMonitorLocations(ctx, a.ID, LocationSetup{IncludeLocal: true, ProbeIDs: []int64{p.ID}, DownWhen: DownWhenMajority})
	all, _ := s.AllMonitorLocations(ctx)
	if len(all) != 2 || !all[a.ID].IncludeLocal || len(all[a.ID].ProbeIDs) != 1 || all[a.ID].DownWhen != DownWhenMajority || all[a.ID].NotifyPartial {
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

// TestProbeIPLock ajanı ilk bağlandığı IP'ye kilitleme yardımcıları:
// LockProbeIP (yalnızca boşken sabitler), SetProbeIPLock (aç/kapat, kilitli
// IP'yi sıfırlar) ve ResetProbeIP (kilidi açık bırakır, IP'yi sıfırlar).
func TestProbeIPLock(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()

	p := Probe{Name: "kilit", Active: true, CreatedAt: 100, Hash: "h1", IPLock: true}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProbe(ctx, p.ID)
	if !got.IPLock || got.LockedIP != "" {
		t.Fatalf("yeni kayıt kilitli ama IP boş olmalı: %+v", got)
	}

	// İlk bağlantı IP'yi sabitler; ikinci bağlantı (kilit doluyken) değiştirmez.
	if err := s.LockProbeIP(ctx, p.ID, "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if err := s.LockProbeIP(ctx, p.ID, "5.6.7.8"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); got.LockedIP != "1.2.3.4" {
		t.Fatalf("kilitli IP yalnızca boşken yazılmalı: %+v", got)
	}

	// Sıfırlama: kilit açık kalır, IP boşalır; sonraki bağlantı yeniden sabitler.
	if err := s.ResetProbeIP(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); !got.IPLock || got.LockedIP != "" {
		t.Fatalf("sıfırlama sonrası kilit açık, IP boş olmalı: %+v", got)
	}
	if err := s.LockProbeIP(ctx, p.ID, "9.9.9.9"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); got.LockedIP != "9.9.9.9" {
		t.Fatalf("sıfırlamadan sonra yeni IP sabitlenmeli: %+v", got)
	}

	// Kilidi kapatmak IP'yi sıfırlar; tekrar açmak yeniden silahlar (IP boş).
	if err := s.SetProbeIPLock(ctx, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); got.IPLock || got.LockedIP != "" {
		t.Fatalf("kilit kapatılınca IP sıfırlanmalı: %+v", got)
	}
	// Kilit kapalıyken sabitlensin diye IP yazılıp kilit yeniden açıldığında da sıfırlanır.
	if err := s.LockProbeIP(ctx, p.ID, "8.8.8.8"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProbeIPLock(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); !got.IPLock || got.LockedIP != "" {
		t.Fatalf("kilit açılınca yeniden silahlanmalı (IP boş): %+v", got)
	}

	// Olmayan kayıt.
	if err := s.SetProbeIPLock(ctx, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("olmayan kayıt (kilit): %v", err)
	}
	if err := s.ResetProbeIP(ctx, 999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("olmayan kayıt (sıfırla): %v", err)
	}
}
