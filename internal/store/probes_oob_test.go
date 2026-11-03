package store

import (
	"context"
	"testing"
)

func TestProbeOOBStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := Probe{Name: "oob", Hash: "h-oob"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if g, _ := s.GetProbe(ctx, p.ID); g.Uplink != UplinkPrimary || g.OOBIP != "" || g.OOBLabel() != "OOB" {
		t.Fatalf("varsayılan: %+v", g.ProbeOOB)
	}
	if err := s.SetProbeOOB(ctx, p.ID, "198.51.100.7", "MARS"); err != nil {
		t.Fatal(err)
	}
	// Karşılaştır-ve-yaz: aynı hat iki kez yazılırsa yalnız ilki değişiklik sayılır.
	if ch, err := s.SetProbeUplink(ctx, p.ID, UplinkOOB, 100); err != nil || !ch {
		t.Fatalf("ilk geçiş: %v %v", ch, err)
	}
	if ch, _ := s.SetProbeUplink(ctx, p.ID, UplinkOOB, 200); ch {
		t.Fatal("aynı hat ikinci kez değişiklik sayılmamalı")
	}
	if err := s.SetProbeOOBIfaceMissing(ctx, p.ID, true); err != nil {
		t.Fatal(err)
	}
	g, _ := s.GetProbe(ctx, p.ID)
	if !g.OnOOB() || g.UplinkSince != 100 || g.OOBLabel() != "MARS" || !g.OOBIfaceMissing {
		t.Fatalf("OOB durumu: %+v", g.ProbeOOB)
	}
	// OOB kaldırılınca hat ve uyarı sıfırlanır.
	if err := s.SetProbeOOB(ctx, p.ID, "", ""); err != nil {
		t.Fatal(err)
	}
	if g, _ = s.GetProbe(ctx, p.ID); g.OnOOB() || g.OOBIfaceMissing || g.OOBIP != "" {
		t.Fatalf("kaldırma: %+v", g.ProbeOOB)
	}
	if err := s.SetProbeOOB(ctx, 9999, "198.51.100.7", ""); err != ErrNotFound {
		t.Fatalf("olmayan kayıt: %v", err)
	}
}

func TestNormalizeOOB(t *testing.T) {
	for _, c := range []struct {
		ip, name, want string
		ok             bool
	}{
		{"", "x", "", true}, {" 10.0.0.5 ", "", "10.0.0.5", true}, {"::ffff:10.0.0.5", "", "10.0.0.5", true},
		{"2001:db8::1", "Yedek", "2001:db8::1", true}, {"0.0.0.0", "", "", false}, {"::1", "", "", false},
		{"fe80::1%eth0", "", "", false}, {"a.b", "", "", false}, {"10.0.0.5", "a\nb", "", false},
	} {
		got, _, err := NormalizeOOB(c.ip, c.name)
		if (err == nil) != c.ok || (c.ok && got != c.want) {
			t.Errorf("%q %q: %q %v", c.ip, c.name, got, err)
		}
	}
	if !SameIP("::ffff:10.0.0.5", "10.0.0.5") || SameIP("10.0.0.5", "10.0.0.6") || SameIP("", "") {
		t.Error("SameIP")
	}
}

func TestUplinkKindsAlias(t *testing.T) {
	n := Notification{Events: []string{"probe_offline", "probe_online"}}
	if !n.Accepts("probe_uplink_failover") || !n.Accepts("probe_uplink_restored") {
		t.Fatal("kontrol noktası türlerini seçmiş kanal hat bildirimini almalı")
	}
	if (Notification{Events: []string{"down"}}).Accepts("probe_uplink_failover") {
		t.Fatal("ilgisiz kanal almamalı")
	}
}
