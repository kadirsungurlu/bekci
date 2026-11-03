package api

import (
	"encoding/json"
	"fmt"
	"testing"
)

// TestProbeOOB OOB ikinci hat: IP kilidi OOB IP'sini kabul eder (üçüncü IP 403),
// OOB'den gelen istek kilidi sabitlemez ve son görülen IP'yi değiştirmez;
// X-Probe-Uplink geçişi kaydeder; ayar iş listesi yanıtında ajana gider.
func TestProbeOOB(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	cp := admin.newProbe("Hat")
	path := fmt.Sprintf("/api/probes/%d", cp.Probe.ID)
	get := func() probeAdminView {
		var list []probeAdminView
		admin.mustDo("GET", "/api/probes", nil, &list, 200)
		for _, p := range list {
			if p.ID == cp.Probe.ID {
				return p
			}
		}
		t.Fatalf("ajan listede yok")
		return probeAdminView{}
	}
	job := func(xff, uplink string) (int, map[string]any) {
		h := bearer(cp.Token)
		if xff != "" {
			h["X-Forwarded-For"] = xff
		}
		if uplink != "" {
			h["X-Probe-Uplink"] = uplink
			h["X-Probe-OOB-Iface"] = "missing"
		}
		var out map[string]any
		code, _, body := admin.rawReq("GET", "/api/probe/jobs", h, nil)
		_ = json.Unmarshal(body, &out)
		return code, out
	}

	// Geçersiz değerler reddedilir.
	admin.mustDo("PUT", path, map[string]any{"oob_ip": "bozuk"}, nil, 400)
	admin.mustDo("PUT", path, map[string]any{"oob_ip": "127.0.0.1"}, nil, 400)
	admin.mustDo("PUT", path, map[string]any{"oob_ip": "198.51.100.7", "oob_name": "çok uzun bir hat adı çok uzun bir hat adı çok"}, nil, 400)

	var v probeAdminView
	admin.mustDo("PUT", path, map[string]any{"oob_ip": " 198.51.100.7 ", "oob_name": "MARS OOB"}, &v, 200)
	if v.OOBIP != "198.51.100.7" || v.OOBName != "MARS OOB" || v.Uplink != "primary" {
		t.Fatalf("OOB kaydı: %+v", v.ProbeOOB)
	}

	// OOB'den gelen ilk istek: kabul, kilit sabitlenmez.
	code, out := job("198.51.100.7", "oob")
	if code != 200 {
		t.Fatalf("OOB IP'si: %d", code)
	}
	if oob, _ := out["oob"].(map[string]any); oob["ip"] != "198.51.100.7" || oob["name"] != "MARS OOB" {
		t.Fatalf("iş listesinde OOB ayarı yok: %v", out["oob"])
	}
	p := get()
	if p.LockedIP != "" {
		t.Fatalf("OOB isteği kilidi sabitlememeli: %+v", p)
	}
	if p.Uplink != "oob" || p.UplinkSince == 0 || !p.OOBIfaceMissing {
		t.Fatalf("hat geçişi kaydedilmeli: %+v", p.ProbeOOB)
	}
	// Ana hat sabitlenir; OOB kabul edilmeye devam eder; üçüncü IP 403.
	if code, _ := job("", "primary"); code != 200 {
		t.Fatalf("ana hat: %d", code)
	}
	if p = get(); p.LockedIP != "127.0.0.1" || p.Uplink != "primary" || p.LastIP != "127.0.0.1" {
		t.Fatalf("ana hat kilidi/dönüş: %+v", p)
	}
	if code, _ := job("198.51.100.7", "oob"); code != 200 {
		t.Fatalf("kilitliyken OOB IP'si: %d", code)
	}
	if p = get(); p.LastIP != "127.0.0.1" {
		t.Fatalf("OOB isteği son görülen IP'yi değiştirmemeli: %q", p.LastIP)
	}
	if code, _ := job("203.0.113.9", ""); code != 403 {
		t.Fatalf("üçüncü IP: %d, 403 bekleniyordu", code)
	}

	// OOB kaldırılınca hat ana hatta döner, OOB IP'si artık reddedilir.
	admin.mustDo("PUT", path, map[string]any{"oob_ip": ""}, &v, 200)
	if v.OOBIP != "" || v.OOBName != "" || v.Uplink != "primary" || v.OOBIfaceMissing {
		t.Fatalf("OOB kaldırma: %+v", v.ProbeOOB)
	}
	if code, _ := job("198.51.100.7", "oob"); code != 403 {
		t.Fatalf("OOB kaldırıldıktan sonra eski OOB IP'si: %d", code)
	}
}
