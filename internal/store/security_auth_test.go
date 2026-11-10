package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// Giriş farkları saat ve IP kovalarına toplanır; oturum ve güvenlik duvarı
// bölümü yoksa önceki korunur; gün başına 20 IP kalır; saklama siler.
func TestSaveSecurityAuth(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	id := secServer(t, s, "ssh")
	const day = int64(20000 * 86400)
	now := day + 10*3600 + 120
	h := now - now%3600

	rep := metrics.Security{
		Auth: &metrics.SecAuth{Source: "journal", Failed: 7, Accepted: 1,
			Hours:  []metrics.SecAuthHour{{Hour: h - 3600, Failed: 4}, {Hour: h, Failed: 3, Accepted: 1}},
			TopIPs: []metrics.SecCount{{Key: "203.0.113.5", Count: 5}, {Key: "198.51.100.7", Count: 2}}},
		Sessions: []metrics.SecSession{{User: "root", TTY: "pts/0", From: "192.0.2.1", Kind: metrics.SessionSSH}},
		Firewall: &metrics.SecFirewall{Active: true, Backend: metrics.FirewallNft, Rules: 3, Policy: "drop"},
	}
	if _, err := s.SaveSecurity(ctx, id, now, rep, "a"); err != nil {
		t.Fatal(err)
	}
	// İkinci fark aynı saate eklenir; bölümsüz rapor oturum/güvenlik duvarını korur.
	rep2 := metrics.Security{Auth: &metrics.SecAuth{Source: "journal", Failed: 2,
		Hours: []metrics.SecAuthHour{{Hour: h, Failed: 2}}, TopIPs: []metrics.SecCount{{Key: "198.51.100.7", Count: 2}}}}
	if _, err := s.SaveSecurity(ctx, id, now+60, rep2, "b"); err != nil {
		t.Fatal(err)
	}
	v, err := s.LoadSecAuth(ctx, id, now+60)
	if err != nil {
		t.Fatal(err)
	}
	if v.Failed24h != 9 || v.Accepted24h != 1 || v.Failed7d != 9 || len(v.Hours) != 2 || v.Hours[1].Failed != 5 {
		t.Fatalf("saatler: %+v", v)
	}
	if len(v.TopIPs24h) != 2 || v.TopIPs24h[0].IP != "203.0.113.5" || v.TopIPs24h[0].Count != 5 || v.TopIPs24h[1].Count != 4 {
		t.Fatalf("IP: %+v", v.TopIPs24h)
	}
	got, err := s.LoadSecurity(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	r := got.Report
	if len(r.Sessions) != 1 || r.Firewall == nil || !r.Firewall.Active || r.Auth == nil || r.Auth.Source != "journal" || r.Auth.Failed != 0 {
		t.Fatalf("rapor: %+v", r)
	}
	var fw int
	if err := s.db.QueryRowContext(ctx, `SELECT fw_active FROM sec_state WHERE server_id = ?`, id).Scan(&fw); err != nil || fw != 1 {
		t.Fatalf("fw_active %d %v", fw, err)
	}
	// Boş oturum listesi (nil değil) öncekinin yerine geçer.
	if _, err := s.SaveSecurity(ctx, id, now+120, metrics.Security{Sessions: []metrics.SecSession{}}, "c"); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.LoadSecurity(ctx, id); got.Report.Sessions == nil || len(got.Report.Sessions) != 0 {
		t.Fatalf("boş oturum: %+v", got.Report.Sessions)
	}

	// Gün başına en çok 20 IP.
	var many []metrics.SecCount
	for i := range 25 {
		many = append(many, metrics.SecCount{Key: fmt.Sprintf("192.0.2.%d", i+10), Count: 100 + i})
	}
	if _, err := s.SaveSecurity(ctx, id, now+180, metrics.Security{Auth: &metrics.SecAuth{TopIPs: many}}, "d"); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_auth_ip_daily WHERE server_id = ?`, id).Scan(&n)
	if n != 20 {
		t.Fatalf("IP satırı %d", n)
	}

	// Saklama: 90 gün sonra saatlik, 30 gün sonra IP silinir.
	if err := s.PruneSecAuth(ctx, now+31*86400); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_auth_ip_daily`).Scan(&n)
	if n != 0 {
		t.Fatalf("IP silinmedi %d", n)
	}
	if v, _ = s.LoadSecAuth(ctx, id, now+60); v.Failed7d != 9 {
		t.Fatalf("saatlik erken silindi %+v", v)
	}
	if err := s.PruneSecAuth(ctx, now+91*86400); err != nil {
		t.Fatal(err)
	}
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sec_auth_hourly`).Scan(&n)
	if n != 0 {
		t.Fatalf("saatlik silinmedi %d", n)
	}
}
