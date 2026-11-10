package store

import (
	"context"
	"slices"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

func secRep(pkgs ...metrics.SecPkg) metrics.Security {
	sec := 0
	for _, p := range pkgs {
		if p.Security {
			sec++
		}
	}
	return metrics.Security{Updates: &metrics.SecUpdates{Manager: "apt", Total: len(pkgs), Security: sec, Packages: pkgs}}
}

func secServer(t *testing.T, s *Store, name string) int64 {
	t.Helper()
	p := &Probe{Name: name, Kind: ProbeKindServer, Active: true, Hash: "h-" + name}
	if err := s.CreateProbe(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	return p.ID
}

func pkgNames(l []SecPkg) []string {
	out := make([]string, len(l))
	for i, p := range l {
		out[i] = p.Name
	}
	return out
}

func TestSaveSecurityPackages(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	id := secServer(t, s, "sec")

	// Boş durum: ReceivedAt 0, boş liste.
	empty, err := s.LoadSecurity(ctx, id)
	if err != nil || empty.ReceivedAt != 0 || empty.Packages == nil || len(empty.Packages) != 0 || empty.Notes == nil {
		t.Fatalf("boş: %+v %v", empty, err)
	}

	ssl := metrics.SecPkg{Name: "openssl", Current: "3.0.1", New: "3.0.2", Security: true, Severity: "Important"}
	vim := metrics.SecPkg{Name: "vim", New: "9.1"}
	ch, err := s.SaveSecurity(ctx, id, 1000, secRep(ssl, vim), "h1")
	if err != nil {
		t.Fatal(err)
	}
	if !ch.UpdatesReported || !slices.Equal(ch.NewSecurity, []string{"openssl"}) || len(ch.Installed) != 0 {
		t.Fatalf("ilk: %+v", ch)
	}

	// İkinci rapor: first_seen korunur, yeni güvenlik paketi eklenir.
	curl := metrics.SecPkg{Name: "curl", New: "8", Security: true}
	ch, err = s.SaveSecurity(ctx, id, 2000, secRep(ssl, vim, curl), "h2")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ch.NewSecurity, []string{"curl"}) {
		t.Fatalf("ikinci: %+v", ch)
	}
	got, err := s.LoadSecurity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	// Sıra: güvenlik önce, sonra first_seen, sonra ad.
	if !slices.Equal(pkgNames(got.Packages), []string{"openssl", "curl", "vim"}) {
		t.Fatalf("sıra: %v", pkgNames(got.Packages))
	}
	if got.ReceivedAt != 2000 || got.Packages[0].FirstSeen != 1000 || got.Packages[1].FirstSeen != 2000 ||
		got.Packages[0].Current != "3.0.1" || got.Packages[0].Severity != "Important" || !got.Packages[0].Security || got.Packages[2].Security {
		t.Fatalf("yükleme: %+v", got)
	}
	if got.Report.Updates == nil || got.Report.Updates.Total != 3 || got.Report.Updates.Packages != nil {
		t.Fatalf("rapor (paket listesi saklanmaz): %+v", got.Report.Updates)
	}
	name, first, count, err := s.OldestSecurityPkg(ctx, id)
	if err != nil || name != "openssl" || first != 1000 || count != 2 {
		t.Fatalf("en eski: %q %d %d %v", name, first, count, err)
	}

	// Kesilmiş liste: görünmeyen paket kurulmuş sayılmaz.
	tr := secRep(curl)
	tr.Updates.Truncated = true
	if ch, err = s.SaveSecurity(ctx, id, 3000, tr, "h3"); err != nil || len(ch.Installed) != 0 {
		t.Fatalf("kesik: %+v %v", ch, err)
	}
	if got, _ = s.LoadSecurity(ctx, id); len(got.Packages) != 3 {
		t.Fatalf("kesikte silindi: %v", pkgNames(got.Packages))
	}

	// Updates nil (bölüm toplanamadı): paketler ve önceki özet korunur.
	if ch, err = s.SaveSecurity(ctx, id, 4000, metrics.Security{Notes: map[string]string{"updates": "locked"}}, "h4"); err != nil || ch.UpdatesReported || len(ch.Installed) != 0 {
		t.Fatalf("bölümsüz: %+v %v", ch, err)
	}
	got, _ = s.LoadSecurity(ctx, id)
	if len(got.Packages) != 3 || got.Report.Updates == nil || got.Report.Updates.Manager != "apt" || got.Report.Updates.Total != 1 || got.Notes["updates"] != "locked" {
		t.Fatalf("bölümsüz yükleme: %+v", got)
	}
	sum, err := s.SecSummaries(ctx)
	if err != nil || sum[id].UpdTotal != 1 || sum[id].OldestSecurity != 1000 {
		t.Fatalf("özet korunmadı: %+v %v", sum[id], err)
	}

	// openssl kuruldu: listeden çıkar, silinir.
	if ch, err = s.SaveSecurity(ctx, id, 5000, secRep(curl, vim), "h5"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ch.Installed, []string{"openssl"}) || len(ch.NewSecurity) != 0 {
		t.Fatalf("kurulum: %+v", ch)
	}
	if got, _ = s.LoadSecurity(ctx, id); !slices.Equal(pkgNames(got.Packages), []string{"curl", "vim"}) {
		t.Fatalf("silinmedi: %v", pkgNames(got.Packages))
	}
	if name, first, count, _ = s.OldestSecurityPkg(ctx, id); name != "curl" || first != 2000 || count != 1 {
		t.Fatalf("en eski: %q %d %d", name, first, count)
	}

	// Boş liste: hepsi kuruldu, güvenlik paketi kalmadı.
	if ch, err = s.SaveSecurity(ctx, id, 6000, secRep(), "h6"); err != nil || len(ch.Installed) != 2 {
		t.Fatalf("hepsi: %+v %v", ch, err)
	}
	if _, _, count, err = s.OldestSecurityPkg(ctx, id); err != nil || count != 0 {
		t.Fatalf("kalan: %d %v", count, err)
	}
}

func TestSaveSecurityReboot(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	id := secServer(t, s, "rb")
	other := secServer(t, s, "rapor-yok")
	_ = other

	req := metrics.Security{Reboot: &metrics.SecReboot{Required: true, Reason: metrics.RebootFile}}
	ch, err := s.SaveSecurity(ctx, id, 100, req, "a")
	if err != nil || !ch.RebootRequired || ch.RebootCleared || ch.UpdatesReported {
		t.Fatalf("ilk: %+v %v", ch, err)
	}
	// Sürerken reboot_since değişmez, yeniden "gerekli oldu" denmez.
	if ch, err = s.SaveSecurity(ctx, id, 200, req, "a"); err != nil || ch.RebootRequired {
		t.Fatalf("sürüyor: %+v %v", ch, err)
	}
	// Bölüm yoksa (toplanamadı) önceki durum korunur.
	if ch, err = s.SaveSecurity(ctx, id, 250, metrics.Security{}, "b"); err != nil || ch.RebootRequired || ch.RebootCleared {
		t.Fatalf("bölümsüz: %+v %v", ch, err)
	}
	got, _ := s.LoadSecurity(ctx, id)
	if got.RebootSince != 100 || got.Report.Reboot == nil || !got.Report.Reboot.Required {
		t.Fatalf("since: %+v", got)
	}
	sum, err := s.SecSummaries(ctx)
	if err != nil || len(sum) != 1 || !sum[id].RebootRequired || sum[id].RebootSince != 100 || sum[id].UpdTotal != 0 {
		t.Fatalf("özet: %+v %v", sum, err)
	}
	// Bitti: 0 ve RebootCleared.
	if ch, err = s.SaveSecurity(ctx, id, 300, metrics.Security{Reboot: &metrics.SecReboot{}}, "c"); err != nil || !ch.RebootCleared || ch.RebootRequired {
		t.Fatalf("bitti: %+v %v", ch, err)
	}
	got, _ = s.LoadSecurity(ctx, id)
	if got.RebootSince != 0 {
		t.Fatalf("since sıfırlanmadı: %d", got.RebootSince)
	}
	if sum, _ = s.SecSummaries(ctx); sum[id].RebootRequired || sum[id].RebootSince != 0 {
		t.Fatalf("özet: %+v", sum[id])
	}
	// Tekrar gerekli: yeni an.
	if ch, _ = s.SaveSecurity(ctx, id, 400, req, "a"); !ch.RebootRequired {
		t.Fatalf("yeniden: %+v", ch)
	}
	if got, _ = s.LoadSecurity(ctx, id); got.RebootSince != 400 {
		t.Fatalf("since: %d", got.RebootSince)
	}
}

// Migration 51 kuralı, kuralı olan var olan sunucuya eklenir. Geç uygulanan
// migration yoluyla denenir (migrate_skipped).
func TestSecurityMigration51(t *testing.T) {
	target := testTarget(t)
	s, err := Open(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id := secServer(t, s, "eski")
	if _, err := s.ReplaceServerAlerts(ctx, id, []ServerAlert{{Metric: "cpu", Threshold: 90, Minutes: 5, Active: true}}, 1); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP TABLE sec_pkgs`, `DROP TABLE sec_state`,
		`DELETE FROM server_alerts WHERE metric = 'sec_updates_age'`,
		`INSERT INTO migrate_skipped (version) VALUES (51)`,
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	s.Close()
	s, err = Open(target, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	rules, err := s.ServerAlerts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(rules, func(a ServerAlert) bool { return a.Metric == "sec_updates_age" })
	if i < 0 || rules[i].Threshold != 7 || !rules[i].Active || rules[i].Firing || rules[i].Minutes != 1 || len(rules) != 2 {
		t.Fatalf("kural: %+v", rules)
	}
	if _, err := s.SaveSecurity(ctx, id, 1, secRep(), "x"); err != nil {
		t.Fatalf("tablolar: %v", err)
	}
}
