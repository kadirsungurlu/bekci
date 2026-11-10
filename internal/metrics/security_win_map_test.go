package metrics

import (
	"reflect"
	"testing"
	"time"
)

func TestMapDefender(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	st := wmiMpStatus{
		AMServiceEnabled: true, AntivirusEnabled: true, RealTimeProtectionEnabled: true, IsTamperProtected: true,
		AntivirusSignatureLastUpdated: now.Add(-5 * time.Hour), AntivirusSignatureVersion: " 1.421.12.0 ",
		QuickScanAge: 2, FullScanAge: 4294967295,
	}
	det := []wmiMpDetection{
		{InitialDetectionTime: now.Add(-24 * time.Hour)},
		{InitialDetectionTime: now.Add(-29 * 24 * time.Hour)},
		{InitialDetectionTime: now.Add(-31 * 24 * time.Hour)},
		{},
	}
	thr := []wmiMpThreat{{IsActive: true}, {IsActive: false}}
	got := mapDefender(st, det, thr, now)
	want := SecAV{Product: AVDefender, Service: true, Enabled: true, Realtime: true, Tamper: true,
		SigUpdated: now.Add(-5 * time.Hour).Unix(), SigVersion: "1.421.12.0", QuickAgeD: 2, FullAgeD: -1,
		Threats30d: 2, ActiveThreats: 1}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	if !got.OK() {
		t.Fatal("OK false")
	}
	// uint32 hiç değeri int32 olarak -1 gelebilir; boş tarih bilinmiyor.
	got = mapDefender(wmiMpStatus{AMServiceEnabled: true, AntivirusEnabled: true, QuickScanAge: -1, FullScanAge: 50000,
		AntivirusSignatureLastUpdated: time.Date(1601, 1, 1, 0, 0, 0, 0, time.UTC)}, nil, nil, now)
	if got.QuickAgeD != -1 || got.FullAgeD != -1 || got.SigUpdated != 0 || got.OK() {
		t.Fatalf("hiç/boş: %+v", got)
	}
}

func TestMapFirewall(t *testing.T) {
	prof := []wmiFwProfile{
		{Name: "Public", Enabled: 1, DefaultInboundAction: 4},
		{Name: "Domain", Enabled: 2, DefaultInboundAction: 0}, // ayarlanmamış = açık + engelle
		{Name: "Private", Enabled: 0, DefaultInboundAction: 4},
		{Name: "Bilinmeyen", Enabled: 1},
	}
	// Bağlantı bilinmiyor: tüm profiller açık olmalı, özel kapalı -> etkin değil.
	got := mapFirewall(prof, nil)
	want := &SecFirewall{Backend: FirewallWindows, Profiles: []SecFwProfile{
		{Name: FwProfilePublic, Enabled: true, Inbound: "block"},
		{Name: FwProfileDomain, Enabled: true, Inbound: "block"},
		{Name: FwProfilePrivate, Enabled: false, Inbound: "block"},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("bilinmiyor: %+v", got)
	}
	// Yalnız ortak ağa bağlı: ortak açık -> etkin.
	got = mapFirewall(prof, []wmiConnProfile{{NetworkCategory: 0}})
	if !got.Active || !got.Profiles[0].Connected || got.Profiles[2].Connected {
		t.Fatalf("ortak bağlı: %+v", got)
	}
	// Özel ağa da bağlı: özel kapalı -> etkin değil.
	if got = mapFirewall(prof, []wmiConnProfile{{NetworkCategory: 0}, {NetworkCategory: 1}}); got.Active {
		t.Fatalf("özel bağlı: %+v", got)
	}
	// Açık ama gelen varsayılanı izin: süzmüyor.
	if got = mapFirewall([]wmiFwProfile{{Name: "Domain", Enabled: 1, DefaultInboundAction: 2}}, []wmiConnProfile{{NetworkCategory: 2}}); got.Active || got.Profiles[0].Inbound != "allow" {
		t.Fatalf("izin: %+v", got)
	}
	if got = mapFirewall(nil, nil); got.Active || len(got.Profiles) != 0 {
		t.Fatalf("boş: %+v", got)
	}
	// Sanitize profilleri etki alanı, özel, ortak sırasına koyar.
	s := Security{Firewall: mapFirewall(prof, nil)}
	s.Sanitize()
	if p := s.Firewall.Profiles; p[0].Name != FwProfileDomain || p[1].Name != FwProfilePrivate || p[2].Name != FwProfilePublic {
		t.Fatalf("sıra: %+v", p)
	}
}

func TestMapWTSSession(t *testing.T) {
	logon := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC).Unix()
	ft := logon*10000000 + 116444736000000000
	cases := []struct {
		in   wtsRaw
		ok   bool
		want SecSession
	}{
		{wtsRaw{ID: 0, Station: "Services", User: "SYSTEM"}, false, SecSession{}},
		{wtsRaw{ID: 65536, Station: "RDP-Tcp", State: 6}, false, SecSession{}}, // dinleyici
		{wtsRaw{ID: 2, Station: "RDP-Tcp#3", State: 0, User: "admin", Domain: "CORP", ClientAddr: "203.0.113.7", ClientName: "LAPTOP", LogonTime: ft}, true,
			SecSession{User: `CORP\admin`, TTY: "RDP-Tcp#3", From: "203.0.113.7", Since: logon, Kind: SessionRDP, State: "Active"}},
		{wtsRaw{ID: 3, Station: "", State: 4, User: "ops", ClientName: "EVPC"}, true,
			SecSession{User: "ops", From: "EVPC", Kind: SessionRDP, State: "Disconnected"}},
		{wtsRaw{ID: 1, Station: "Console", State: 0, User: "Administrator", ClientAddr: "10.0.0.1"}, true,
			SecSession{User: "Administrator", TTY: "Console", Kind: SessionTTY, State: "Active"}},
		{wtsRaw{ID: 4, Station: "RDP-Tcp#9", State: 8, User: "x"}, false, SecSession{}}, // kapanmış
	}
	for i, c := range cases {
		got, ok := mapWTSSession(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("%d: got %+v %v, want %+v %v", i, got, ok, c.want, c.ok)
		}
	}
	if filetimeUnix(0) != 0 || filetimeUnix(116444736000000000) != 0 {
		t.Fatal("filetime sıfır")
	}
}

func TestWTSClientAddrAndWMIMissing(t *testing.T) {
	var b [20]byte
	copy(b[2:], []byte{192, 0, 2, 10})
	if got := wtsClientAddr(2, b); got != "192.0.2.10" {
		t.Fatalf("v4: %q", got)
	}
	var b6 [20]byte
	copy(b6[2:], []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1})
	if got := wtsClientAddr(23, b6); got != "2001:db8::1" {
		t.Fatalf("v6: %q", got)
	}
	if wtsClientAddr(2, [20]byte{}) != "" || wtsClientAddr(0, b) != "" {
		t.Fatal("boş adres")
	}
	if !wmiMissingCode(0x8004100E) || !wmiMissingCode(0x80041010) || wmiMissingCode(0x80070005) {
		t.Fatal("wmi kodları")
	}
}
