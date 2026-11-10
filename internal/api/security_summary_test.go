package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

type secSummaryResp struct {
	Servers         int           `json:"servers"`
	Reporting       int           `json:"reporting"`
	SecurityUpdates secFleetGroup `json:"security_updates"`
	Reboot          secFleetGroup `json:"reboot"`
	FirewallOff     secFleetGroup `json:"firewall_off"`
	AvProblem       secFleetGroup `json:"av_problem"`
	StaleIndex      secFleetGroup `json:"stale_index"`
}

func TestSecuritySummary(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var a, b, c serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &a, 201)
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "db-1"}, &b, 201)
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "yeni"}, &c, 201) // rapor yok

	old := time.Now().Add(-10 * 24 * time.Hour).Unix()
	reps := map[string]metrics.Security{
		a.Token: {
			Updates: &metrics.SecUpdates{Manager: "apt", IndexTime: old, Packages: []metrics.SecPkg{
				{Name: "openssl", New: "3.0.2", Security: true}, {Name: "curl", New: "8", Security: true}, {Name: "vim", New: "9.1"}}},
			Reboot:   &metrics.SecReboot{Required: true, Reason: metrics.RebootFile},
			Firewall: &metrics.SecFirewall{Active: false, Backend: "ufw"},
		},
		b.Token: {
			Updates:  &metrics.SecUpdates{Manager: "dnf", IndexTime: time.Now().Unix(), Packages: []metrics.SecPkg{{Name: "vim", New: "9.1"}}},
			Firewall: &metrics.SecFirewall{Active: true, Backend: "firewalld"},
		},
	}
	for tok, body := range reps {
		if st, _, _ := admin.rawReq("POST", "/api/probe/security", bearer(tok), body); st != http.StatusNoContent {
			t.Fatalf("gönderim: %d", st)
		}
	}

	var got secSummaryResp
	admin.mustDo("GET", "/api/security/summary", nil, &got, 200)
	if got.Servers != 3 || got.Reporting != 2 ||
		got.SecurityUpdates.Count != 1 || got.SecurityUpdates.Servers[0].ID != a.Probe.ID || got.SecurityUpdates.Servers[0].Count != 2 ||
		got.SecurityUpdates.Servers[0].Oldest == 0 || got.SecurityUpdates.Servers[0].Name != "web-1" ||
		got.Reboot.Count != 1 || got.Reboot.Servers[0].Since == 0 ||
		got.FirewallOff.Count != 1 || got.FirewallOff.Servers[0].ID != a.Probe.ID ||
		got.AvProblem.Count != 0 || got.AvProblem.Servers == nil ||
		got.StaleIndex.Count != 1 || got.StaleIndex.Servers[0].IndexTime != old {
		t.Fatalf("özet: %+v", got)
	}

	// Liste görünümünde de güvenlik duvarı ve dizin zamanı gelir (rozetler için).
	var list struct {
		Servers []struct {
			ID       int64             `json:"id"`
			Security *store.SecSummary `json:"security"`
		} `json:"servers"`
	}
	admin.mustDo("GET", "/api/servers", nil, &list, 200)
	for _, s := range list.Servers {
		if s.ID == a.Probe.ID && (s.Security == nil || s.Security.FwActive != 0 || s.Security.IndexTime != old) {
			t.Fatalf("liste özeti: %+v", s.Security)
		}
	}

	// Kısıtlı izleyici yalnız kendi sunucusunu sayar.
	var cu store.User
	admin.mustDo("POST", "/api/users", map[string]any{"username": "musteri", "role": store.RoleViewer, "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{}, "server_ids": []int64{b.Probe.ID}}, &cu, 201)
	viewer := admin.loginAs("musteri", "gecici-sifre-1")
	viewer.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	var vg secSummaryResp
	viewer.mustDo("GET", "/api/security/summary", nil, &vg, 200)
	if vg.Servers != 1 || vg.Reporting != 1 || vg.SecurityUpdates.Count != 0 || vg.Reboot.Count != 0 || vg.FirewallOff.Count != 0 || vg.StaleIndex.Count != 0 {
		t.Fatalf("izleyici özeti: %+v", vg)
	}

	// Salt okunur anahtar: özet ve sunucu güvenliği 200, yazma 403.
	var ck createdKey
	admin.mustDo("POST", "/api/api-keys", map[string]any{"name": "cli", "scope": "read"}, &ck, 201)
	key := bearer(ck.Secret)
	for _, p := range []string{"/api/security/summary", "/api/servers/" + itoa(a.Probe.ID) + "/security"} {
		if code, _, body := admin.rawReq("GET", p, key, nil); code != 200 {
			t.Errorf("GET %s: %d %s", p, code, body)
		}
	}
	for _, c := range []struct{ m, p string }{{"PUT", "/api/servers/" + itoa(a.Probe.ID) + "/alerts"}, {"POST", "/api/servers"}} {
		if code, _, body := admin.rawReq(c.m, c.p, key, map[string]any{}); code != 403 {
			t.Errorf("%s %s: %d %s", c.m, c.p, code, body)
		}
	}
}
