package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestReadScopeAllows(t *testing.T) {
	yes := []string{"/api/servers", "/api/servers/7", "/api/servers/7/inventory", "/api/servers/7/timeline",
		"/api/servers/7/stats", "/api/servers/7/events", "/api/servers/7/incidents", "/api/monitors", "/api/monitors/3",
		"/api/incidents", "/api/incidents/9", "/api/certs", "/api/server-events", "/api/summary", "/api/security/summary", "/api/servers/7/security"}
	no := []string{"/api/api-keys", "/api/users", "/api/settings", "/api/export", "/api/audit", "/api/notifications",
		"/api/events", "/metrics", "/api/badge/1/status", "/api/servers/7/", "/api/servers/x", "/api/monitors/3/beats",
		"/api/servers/7/inventory/certs/ab", "/api/topology", "/api/mesh", "/api/probes"}
	for _, p := range yes {
		if !readScopeAllows("GET", p) {
			t.Errorf("GET %s izinli olmalı", p)
		}
	}
	for _, p := range no {
		if readScopeAllows("GET", p) {
			t.Errorf("GET %s kapalı olmalı", p)
		}
	}
	for _, m := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if readScopeAllows(m, "/api/servers") {
			t.Errorf("%s kapalı olmalı", m)
		}
	}
}

func TestReadOnlyKey(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var sv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &sv, 201)
	until := time.Now().Add(5*24*time.Hour + time.Hour).Unix()
	body := metrics.Inventory{
		Time:  time.Now().UnixMilli(),
		Certs: []metrics.InvCert{{Source: "port", Location: "127.0.0.1:443", Subject: "a.example", NotAfter: until, Fingerprint: "ab12"}},
	}
	if st, _, _ := admin.rawReq("POST", "/api/probe/inventory", bearer(sv.Token), body); st != http.StatusNoContent {
		t.Fatalf("envanter: %d", st)
	}

	// Oluşturma: rol istenen ne olursa olsun izleyici, kayıt defterine düşer.
	var ck createdKey
	admin.mustDo("POST", "/api/api-keys", map[string]any{"name": "cli", "role": "admin", "scope": "read"}, &ck, 201)
	if ck.Key.Role != store.RoleViewer || ck.Key.Scope != store.ScopeRead {
		t.Fatalf("anahtar: %+v", ck.Key)
	}
	if code := admin.do("POST", "/api/api-keys", map[string]any{"name": "x", "role": "viewer", "scope": "yaz"}, nil); code != 400 {
		t.Errorf("geçersiz kapsam: %d", code)
	}
	full := admin.newKey("eski", store.RoleViewer, 0)
	if full.Key.Scope != store.ScopeFull {
		t.Errorf("varsayılan kapsam full olmalı: %q", full.Key.Scope)
	}
	var log []store.AuditEntry
	if err := func() (err error) { log, err = f.st.ListAudit(t.Context(), 0, 10); return }(); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range log {
		found = found || (e.Action == "apikey.create" && e.TargetName == "cli" && strings.Contains(e.Detail, "salt okunur"))
	}
	if !found {
		t.Errorf("kayıt defterinde yok: %+v", log)
	}

	key := bearer(ck.Secret)
	for _, p := range []string{"/api/servers", "/api/servers/" + itoa(sv.Probe.ID), "/api/servers/" + itoa(sv.Probe.ID) + "/inventory",
		"/api/monitors", "/api/incidents?open=1", "/api/certs", "/api/server-events"} {
		if code, _, b := admin.rawReq("GET", p, key, nil); code != 200 {
			t.Errorf("GET %s: %d %s", p, code, b)
		}
	}
	// İzin listesi dışı: geçerli anahtar 403, kod ile; yazma da 403.
	for _, c := range []struct{ m, p string }{{"GET", "/api/settings"}, {"GET", "/api/notifications"}, {"GET", "/api/users"},
		{"GET", "/api/audit"}, {"GET", "/api/api-keys"}, {"GET", "/api/export"},
		{"POST", "/api/monitors"}, {"DELETE", "/api/monitors/1"}, {"PUT", "/api/servers/1/alerts"}} {
		code, _, b := admin.rawReq(c.m, c.p, key, map[string]any{})
		if code != 403 || !strings.Contains(string(b), "read_only_key") {
			t.Errorf("%s %s: %d %s", c.m, c.p, code, b)
		}
	}
	if code, _, _ := admin.rawReq("GET", "/metrics", key, nil); code != 401 {
		t.Errorf("/metrics: %d, 401 bekleniyordu", code)
	}
	if code, _, _ := admin.rawReq("GET", "/api/settings", bearer("upk_yanlis"), nil); code != 401 {
		t.Errorf("yanlış anahtar 401 kalmalı: %d", code)
	}

	// Sertifikalar: kalan gün, süzgeç, yoksayılan.
	var res struct {
		Certs []struct {
			Server      string `json:"server"`
			ServerID    int64  `json:"server_id"`
			DaysLeft    int64  `json:"days_left"`
			Fingerprint string `json:"fingerprint"`
		} `json:"certs"`
	}
	get := func(p string) {
		t.Helper()
		_, _, b := admin.rawReq("GET", p, key, nil)
		res.Certs = nil
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatal(err)
		}
	}
	if get("/api/certs"); len(res.Certs) != 1 || res.Certs[0].Server != "web-1" || res.Certs[0].DaysLeft != 5 || res.Certs[0].ServerID != sv.Probe.ID {
		t.Fatalf("sertifikalar: %+v", res.Certs)
	}
	if get("/api/certs?days=3"); len(res.Certs) != 0 {
		t.Fatalf("days=3: %+v", res.Certs)
	}
	if code := admin.do("PUT", "/api/servers/"+itoa(sv.Probe.ID)+"/inventory/certs/ab12", map[string]any{"ignored": true}, nil); code != 204 {
		t.Fatalf("yoksay: %d", code)
	}
	if get("/api/certs"); len(res.Certs) != 0 {
		t.Fatalf("yoksayılan görünmemeli: %+v", res.Certs)
	}
	if get("/api/certs?ignored=1"); len(res.Certs) != 1 {
		t.Fatalf("ignored=1: %+v", res.Certs)
	}

	// Sunucu olayları: süzgeç ve sunucu adı.
	now := time.Now().Unix()
	if err := f.st.AddTimelineEvents(t.Context(), []store.TimelineEvent{
		{ProbeID: sv.Probe.ID, Time: now - 10, Kind: "oom", Subject: "java", Level: store.EventError},
		{ProbeID: sv.Probe.ID, Time: now - 5, Kind: "service_started", Subject: "nginx", Level: store.EventInfo},
	}); err != nil {
		t.Fatal(err)
	}
	var ev struct {
		Events []struct {
			Server string `json:"server"`
			Kind   string `json:"kind"`
		} `json:"events"`
	}
	_, _, b := admin.rawReq("GET", "/api/server-events?level=error,warning&limit=5", key, nil)
	if err := json.Unmarshal(b, &ev); err != nil || len(ev.Events) != 1 || ev.Events[0].Kind != "oom" || ev.Events[0].Server != "web-1" {
		t.Fatalf("olaylar: %s", b)
	}

	// İptal sonrası anahtar geçersiz (403 değil 401).
	admin.mustDo("DELETE", "/api/api-keys/"+itoa(ck.Key.ID), nil, nil, 200)
	if code, _, _ := admin.rawReq("GET", "/api/servers", key, nil); code != 401 {
		t.Errorf("iptal: %d", code)
	}
	if code, _, _ := admin.rawReq("GET", "/api/settings", key, nil); code != 401 {
		t.Errorf("iptal + izin dışı: %d", code)
	}
}

// Yönetici anahtarı izleyiciye düşse de sahibi kısıtsız olduğundan her şeyi görür.
func TestReadKeyVisibility(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var a, b serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "a"}, &a, 201)
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "b"}, &b, 201)
	owner, _ := admin.newUser("yonetici2", store.RoleAdmin, nil)
	var ck createdKey
	owner.mustDo("POST", "/api/api-keys", map[string]any{"name": "cli", "scope": "read"}, &ck, 201)
	var list struct {
		Servers []struct{ Name string } `json:"servers"`
	}
	_, _, raw := admin.rawReq("GET", "/api/servers", bearer(ck.Secret), nil)
	if err := json.Unmarshal(raw, &list); err != nil || len(list.Servers) != 2 {
		t.Fatalf("yönetici anahtarı: %s", raw)
	}
	// Atanmamış kısıtlı izleyicinin anahtarı sunuculara giremez.
	viewer, _ := admin.newUser("musteri", store.RoleViewer, []int64{})
	var vk createdKey
	viewer.mustDo("POST", "/api/api-keys", map[string]any{"name": "cli", "scope": "read"}, &vk, 201)
	if code, _, _ := admin.rawReq("GET", "/api/certs", bearer(vk.Secret), nil); code != 403 {
		t.Errorf("kısıtlı izleyici sertifika: %d", code)
	}
	// Anahtarsız istek: envanter açık olmamalı.
	if code, _, _ := admin.rawReq("GET", "/api/servers/"+itoa(a.Probe.ID)+"/inventory", nil, nil); code != 401 {
		t.Errorf("anahtarsız envanter: %d, 401 bekleniyordu", code)
	}
}
