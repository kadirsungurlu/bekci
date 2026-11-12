package api

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
	"github.com/kadirsungurlu/bekci/internal/eklentiajan"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func testEnvelope(t *testing.T, id string, body []byte, imin, imax int) json.RawMessage {
	t.Helper()
	_, priv, _ := ed25519.GenerateKey(nil)
	now := time.Now().Unix()
	m := eklentiajan.Manifest{Schema: 1, ID: id, Version: 3, SHA256: eklentiajan.Digest(body), Interpreter: "sh",
		Mode: "periodic", Class: "metric", RunAs: "nobody", Platforms: []string{"linux"}, IntervalMin: imin, IntervalMax: imax,
		TimeoutSec: 10, Output: "nagios", NotBefore: now - 60, NotAfter: now + 86400}
	p, _ := json.Marshal(m)
	return eklentiajan.SignManifest(priv, p)
}

func TestChecksAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var sv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "db-1"}, &sv, 201)
	id := itoa(sv.Probe.ID)
	base := "/api/servers/" + id + "/checks"

	// Ajan sonucu: yetkisiz 403, fazla sonuç 400, geçerli 204.
	res := servers.CheckReport{Name: "mysql", Source: "plugin", T: time.Now().UnixMilli(),
		Result: eklenti.Result{Status: eklenti.Critical, Message: "bağlantı yok", Metrics: []eklenti.Metric{{Name: "conn", Value: 4, Unit: "c"}}}}
	if st, _, _ := admin.rawReq("POST", "/api/probe/checks", nil, map[string]any{"results": []any{res}}); st != http.StatusForbidden {
		t.Fatalf("anahtarsız: %d", st)
	}
	many := make([]servers.CheckReport, 21)
	if st, _, _ := admin.rawReq("POST", "/api/probe/checks", bearer(sv.Token), map[string]any{"results": many}); st != http.StatusBadRequest {
		t.Fatalf("21 sonuç: %d", st)
	}
	if st, _, b := admin.rawReq("POST", "/api/probe/checks", bearer(sv.Token), map[string]any{"results": []any{res}}); st != http.StatusNoContent {
		t.Fatalf("gönderim: %d %s", st, b)
	}

	var list struct {
		Checks []store.CheckState  `json:"checks"`
		Rules  []store.ServerAlert `json:"rules"`
		Max    int                 `json:"max"`
	}
	admin.mustDo("GET", base, nil, &list, 200)
	if len(list.Checks) != 1 || list.Checks[0].Status != "critical" || len(list.Rules) != 1 || list.Rules[0].Mount != "mysql" || list.Max != 20 {
		t.Fatalf("liste: %+v", list)
	}
	// Varsayılan durum kuralı olayı açtı; genel uyarı formunda görünmez.
	if inc, _ := f.st.ListIncidents(t.Context(), store.IncidentFilter{ServerID: sv.Probe.ID}); len(inc) != 1 || inc[0].ResolvedAt != 0 {
		t.Fatalf("olay açılmadı: %+v", inc)
	}
	var det serverDetailView
	admin.mustDo("GET", "/api/servers/"+id, nil, &det, 200)
	for _, a := range det.Alerts {
		if a.Metric == store.MetricCheck {
			t.Fatalf("genel kurallarda denetim kuralı: %+v", det.Alerts)
		}
	}
	admin.mustDo("PUT", "/api/servers/"+id+"/alerts", map[string]any{"alerts": []any{}}, nil, 200)
	if got, _ := f.st.CheckAlerts(t.Context(), sv.Probe.ID, ""); len(got) != 1 {
		t.Fatalf("genel kayıt denetim kuralını sildi: %+v", got)
	}

	var ser struct {
		Unit   string             `json:"unit"`
		Points []store.CheckPoint `json:"points"`
	}
	admin.mustDo("GET", base+"/mysql/series?metric=conn&range=6h", nil, &ser, 200)
	if ser.Unit != "c" {
		t.Fatalf("seri birimi: %+v", ser)
	}
	for _, q := range []string{"metric=conn&range=5y", "metric=k%20k"} {
		if st := admin.do("GET", base+"/mysql/series?"+q, nil, nil); st != 400 {
			t.Fatalf("seri %s: %d", q, st)
		}
	}

	// PUT doğrulamaları.
	body := []byte("#!/bin/sh\necho OK\n")
	env := testEnvelope(t, "mysql", body, 120, 600)
	b64 := base64.StdEncoding.EncodeToString(body)
	bad := []map[string]any{
		{"manifest": env}, // gövde yok
		{"manifest": env, "body": base64.StdEncoding.EncodeToString([]byte("x"))}, // özet tutmaz
		{"manifest": testEnvelope(t, "redis", body, 120, 600), "body": b64},       // ad tutmaz
		{"manifest": map[string]any{"key": "x"}, "body": b64},                     // zarf bozuk
		{"body": b64},    // manifestsiz gövde
		{"interval": 30}, // sınır dışı
		{"rules": []any{map[string]any{"threshold": 0, "minutes": 1}}},
		{"rules": []any{map[string]any{"metric": "conn", "threshold": 10, "warn_threshold": 20, "minutes": 1}}},
		{"rules": []any{map[string]any{"metric": "conn", "threshold": 10, "minutes": 0}}},
	}
	for i, in := range bad {
		if st := admin.do("PUT", base+"/mysql", in, nil); st != 400 {
			t.Fatalf("geçersiz istek %d: %d", i, st)
		}
	}
	if st := admin.do("PUT", base+"/Kotu_Ad", map[string]any{"enabled": true}, nil); st != 400 {
		t.Fatalf("geçersiz ad: %d", st)
	}
	editor := f.newKey("ed", store.RoleEditor, 0)
	if st, _, _ := admin.rawReq("PUT", base+"/mysql", bearer(editor.Secret), map[string]any{"enabled": false}); st != 403 && st != 401 {
		t.Fatalf("düzenleyici PUT: %d", st)
	}

	// Geçerli manifest: aralık sınırın en küçüğüne çekilir, iş listesine girer.
	var out struct {
		Check store.CheckState    `json:"check"`
		Rules []store.ServerAlert `json:"rules"`
	}
	admin.mustDo("PUT", base+"/mysql", map[string]any{"manifest": env, "body": b64}, &out, 200)
	if !out.Check.HasManifest || out.Check.Interval != 120 || out.Check.Version != 3 {
		t.Fatalf("manifest kaydı: %+v", out.Check)
	}
	if st := admin.do("PUT", base+"/mysql", map[string]any{"interval": 900}, nil); st != 400 {
		t.Fatalf("manifest üst sınırı: %d", st)
	}
	jobs := func() []store.CheckJob {
		st, _, b := admin.rawReq("GET", "/api/probe/jobs", bearer(sv.Token), nil)
		var r struct {
			Checks *[]store.CheckJob `json:"checks"`
		}
		if st != 200 || json.Unmarshal(b, &r) != nil || r.Checks == nil {
			t.Fatalf("jobs: %d %s", st, b)
		}
		return *r.Checks
	}
	if j := jobs(); len(j) != 1 || j[0].Name != "mysql" || j[0].Interval != 120 || j[0].Body != b64 || !strings.Contains(string(j[0].Manifest), `"payload"`) {
		t.Fatalf("iş listesi: %+v", j)
	}

	// Kurallar + kapatma: açık olay kapanır, iş listesinden çıkar, işlem kaydı yazılır.
	admin.mustDo("PUT", base+"/mysql", map[string]any{"interval": 300, "rules": []any{
		map[string]any{"threshold": 6, "minutes": 1},
		map[string]any{"metric": "conn", "threshold": 2, "warn_threshold": 5, "below": true, "minutes": 5},
	}}, &out, 200)
	if len(out.Rules) != 2 || !out.Rules[1].Below || out.Rules[1].Mount != "mysql/conn" || out.Check.Interval != 300 {
		t.Fatalf("kurallar: %+v %+v", out.Rules, out.Check)
	}
	admin.mustDo("PUT", base+"/mysql", map[string]any{"enabled": false}, &out, 200)
	if inc, _ := f.st.ListIncidents(t.Context(), store.IncidentFilter{ServerID: sv.Probe.ID}); len(inc) != 1 || inc[0].ResolvedAt == 0 {
		t.Fatalf("olay kapanmadı: %+v", inc)
	}
	if j := jobs(); len(j) != 0 {
		t.Fatalf("kapalı denetim iş listesinde: %+v", j)
	}
	audit, _ := f.st.ListAudit(t.Context(), 0, 20)
	n := 0
	for _, a := range audit {
		if a.Action == "server.check" && strings.HasPrefix(a.Detail, "mysql: ") {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("işlem kaydı: %d %+v", n, audit)
	}
}
