package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestSecurityAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var sv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &sv, 201)
	id := itoa(sv.Probe.ID)
	path := "/api/servers/" + id + "/security"

	// Eski ajan: rapor yok, received_at 0, paketler boş dizi; kurallar ilk
	// metrikle eklendiğinden eşik de yok.
	var empty map[string]any
	admin.mustDo("GET", path, nil, &empty, 200)
	if _, ok := empty["age_threshold"]; ok || empty["received_at"].(float64) != 0 || empty["interval"].(float64) <= 0 {
		t.Fatalf("boş: %v", empty)
	}
	if l, ok := empty["packages"].([]any); !ok || len(l) != 0 {
		t.Fatalf("packages boş dizi olmalı: %v", empty["packages"])
	}

	body := metrics.Security{
		Updates: &metrics.SecUpdates{Manager: "apt", Packages: []metrics.SecPkg{
			{Name: "openssl", Current: "3.0.1", New: "3.0.2", Security: true},
			{Name: "vim", New: "9.1"},
		}},
		Reboot: &metrics.SecReboot{Required: true, Reason: metrics.RebootFile},
	}
	if st, _, _ := admin.rawReq("POST", "/api/probe/security", bearer(sv.Token), body); st != http.StatusNoContent {
		t.Fatalf("gönderim: %d", st)
	}
	if st, _, _ := admin.rawReq("POST", "/api/probe/security", nil, body); st != http.StatusForbidden {
		t.Fatalf("belirteçsiz: %d", st)
	}
	big := map[string]string{"pad": strings.Repeat("x", metrics.MaxSecurityBytes+10)}
	if st, _, _ := admin.rawReq("POST", "/api/probe/security", bearer(sv.Token), big); st != http.StatusRequestEntityTooLarge {
		t.Fatalf("büyük gövde: %d", st)
	}

	var got struct {
		ReceivedAt   int64   `json:"received_at"`
		RebootSince  int64   `json:"reboot_since"`
		AgeThreshold float64 `json:"age_threshold"`
		Now          int64   `json:"now"`
		Report       struct {
			Updates struct {
				Manager  string `json:"manager"`
				Total    int    `json:"total"`
				Security int    `json:"security"`
			} `json:"updates"`
			Reboot struct {
				Required bool `json:"required"`
			} `json:"reboot"`
		} `json:"report"`
		Packages []store.SecPkg `json:"packages"`
	}
	admin.mustDo("GET", path, nil, &got, 200)
	if got.ReceivedAt == 0 || got.RebootSince == 0 || got.Now == 0 || len(got.Packages) != 2 ||
		got.Packages[0].Name != "openssl" || !got.Packages[0].Security || got.Packages[0].FirstSeen == 0 ||
		got.Report.Updates.Manager != "apt" || got.Report.Updates.Total != 2 || got.Report.Updates.Security != 1 || !got.Report.Reboot.Required {
		t.Fatalf("durum: %+v", got)
	}

	// Liste ve ayrıntıda güvenlik özeti.
	var detail struct {
		Security *store.SecSummary `json:"security"`
	}
	admin.mustDo("GET", "/api/servers/"+id, nil, &detail, 200)
	if detail.Security == nil || detail.Security.UpdSecurity != 1 || !detail.Security.RebootRequired || detail.Security.OldestSecurity == 0 {
		t.Fatalf("ayrıntı özeti: %+v", detail.Security)
	}

	// Kural: sec_updates_age kabul edilir, süre 1'e çekilir; kapatılınca eşik 0.
	alerts := "/api/servers/" + id + "/alerts"
	admin.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{{"metric": "sec_updates_age", "threshold": 400, "minutes": 1}}}, nil, 400)
	var saved struct {
		Alerts []store.ServerAlert `json:"alerts"`
	}
	admin.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{
		{"metric": "sec_updates_age", "threshold": 14, "minutes": 30, "warn_threshold": 3, "active": true},
	}}, &saved, 200)
	if len(saved.Alerts) != 1 || saved.Alerts[0].Metric != "sec_updates_age" || saved.Alerts[0].Minutes != 1 || saved.Alerts[0].Threshold != 14 || saved.Alerts[0].WarnThreshold != 0 {
		t.Fatalf("kural: %+v", saved.Alerts)
	}
	var view map[string]any
	admin.mustDo("GET", path, nil, &view, 200)
	if view["age_threshold"].(float64) != 14 {
		t.Fatalf("eşik: %v", view["age_threshold"])
	}
	admin.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{{"metric": "sec_updates_age", "threshold": 14, "minutes": 1, "active": false}}}, nil, 200)
	view = nil
	admin.mustDo("GET", path, nil, &view, 200)
	if _, ok := view["age_threshold"]; ok {
		t.Fatalf("kapalı kuralda eşik: %v", view["age_threshold"])
	}

	// İş listesinde güvenlik aralığı.
	var jobs map[string]any
	st, _, raw := admin.rawReq("GET", "/api/probe/jobs", bearer(sv.Token), nil)
	if st != 200 || json.Unmarshal(raw, &jobs) != nil || jobs["security_interval"].(float64) <= 0 {
		t.Fatalf("jobs: %d %s", st, raw)
	}
}
