package api

import (
	"context"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/probe"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func (e *env) topology() topologyResp {
	e.t.Helper()
	var out topologyResp
	e.mustDo("GET", "/api/topology", nil, &out, 200)
	return out
}

// Canlı topoloji: tek uç tüm sayfa verisini verir; rol ve müşteri kısıtı diğer
// uçlarla aynı sınırda kalır.
func TestTopologyVisibility(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	a := admin.httpMonitor("Site A", "http://127.0.0.1:1/")
	b := admin.push("Site B")
	p1 := admin.newProbe("Nokta 1")
	p2 := admin.newProbe("Nokta 2")
	admin.setLocations(a.ID, map[string]any{"include_local": true, "probe_ids": []int64{p1.Probe.ID}, "down_when": "all"}, 200)
	var srv, own serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Genel sunucu"}, &srv, 201)
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Müşteri sunucusu"}, &own, 201)
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "Kanca", "type": "webhook",
		"config": map[string]any{"url": "https://example.test/hook"}}, nil, 201)
	if err := f.st.OpenIncident(ctx, b.ID, 1000, "zaman aşımı"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.st.OpenMeshIncident(ctx, 1000, "mesh", `{}`); err != nil {
		t.Fatal(err)
	}

	f.heartbeat(p1.Token, probe.MeshHeartbeat{MainRTTms: 42})
	got := admin.topology()
	for _, p := range got.Probes {
		if p.ID == p1.Probe.ID && (p.PanelRTTms == nil || *p.PanelRTTms != 42 || !p.Online) {
			t.Fatalf("panel RTT kalp atışından gelmeli: %+v", p)
		}
	}
	if !got.Admin || len(got.Monitors) != 2 || len(got.Probes) != 2 || got.Mesh == nil || len(got.Channels) != 1 ||
		len(got.Servers) != 2 || got.Stats.ResultsPerSec == nil || got.Stats.ProbesTotal != 2 {
		t.Fatalf("yönetici görünümü eksik: %+v", got)
	}
	if !hasIncident(got.Incidents, store.IncidentMesh, 0) || !hasIncident(got.Incidents, "", b.ID) || got.Stats.OpenIncidents != len(got.Incidents) {
		t.Fatalf("yönetici açık olaylar (monitör + mesh): %+v", got.Incidents)
	}
	for _, p := range got.Probes {
		if p.Peers == nil || p.Uplink == "" {
			t.Fatalf("yönetici nokta ayrıntısı: %+v", p)
		}
	}
	for _, m := range got.Monitors {
		if m.ID == a.ID && len(m.Locations) != 2 {
			t.Fatalf("çok konumlu monitör konumları: %+v", m.Locations)
		}
		if m.ID == b.ID && (len(m.Locations) != 1 || m.Locations[0].ProbeID != 0) {
			t.Fatalf("tek konumlu monitör: %+v", m.Locations)
		}
	}

	editor, _ := admin.newUser("duzenleyici", store.RoleEditor, nil)
	eg := editor.topology()
	if eg.Admin || eg.Mesh != nil || len(eg.Channels) != 1 || len(eg.Monitors) != 2 {
		t.Fatalf("düzenleyici: %+v", eg)
	}
	if hasIncident(eg.Incidents, store.IncidentMesh, 0) || !hasIncident(eg.Incidents, "", b.ID) || eg.Stats.OpenIncidents != got.Stats.OpenIncidents-1 {
		t.Fatalf("düzenleyici mesh olayını görmemeli: %+v", eg.Incidents)
	}
	for _, p := range eg.Probes {
		if p.Peers != nil || p.PanelRTTms != nil || p.Version != "" || p.Uplink != "" {
			t.Fatalf("düzenleyiciye nokta ayrıntısı sızdı: %+v", p)
		}
	}

	// Kısıtlı müşteri: yalnız A, A'nın kullandığı nokta; sunucu, kanal, hız yok.
	customer, _ := admin.newUser("musteri", store.RoleViewer, []int64{a.ID})
	cg := customer.topology()
	if len(cg.Monitors) != 1 || cg.Monitors[0].ID != a.ID {
		t.Fatalf("müşteri monitörleri: %+v", cg.Monitors)
	}
	if len(cg.Probes) != 1 || cg.Probes[0].ID != p1.Probe.ID || cg.Stats.ProbesTotal != 1 {
		t.Fatalf("müşteri yalnız kullandığı noktayı görmeli: %+v", cg.Probes)
	}
	if len(cg.Servers) != 0 || cg.Channels != nil || cg.Mesh != nil || cg.Stats.ResultsPerSec != nil {
		t.Fatalf("müşteriye sızıntı: %+v", cg)
	}
	if hasIncident(cg.Incidents, "", b.ID) || hasIncident(cg.Incidents, store.IncidentMesh, 0) || cg.Stats.OpenIncidents != len(cg.Incidents) {
		t.Fatalf("müşteri başka monitörün olayını görmemeli: %+v", cg.Incidents)
	}
	_ = p2

	// Sunucu atanmış müşteri yalnız kendi sunucusunu görür.
	admin.mustDo("POST", "/api/users", map[string]any{"username": "musteri2", "role": "viewer", "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{a.ID}, "server_ids": []int64{own.Probe.ID}}, nil, 201)
	c2 := admin.loginAs("musteri2", "gecici-sifre-1")
	c2.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	sg := c2.topology()
	if len(sg.Servers) != 1 || sg.Servers[0].ID != own.Probe.ID {
		t.Fatalf("müşteri sunucuları: %+v", sg.Servers)
	}

	admin.anon().mustDo("GET", "/api/topology", nil, nil, 401)
}

func TestMonitorState(t *testing.T) {
	m := store.Monitor{Active: true, Status: store.StatusUp}
	cases := []struct {
		m       store.Monitor
		maint   bool
		partial bool
		want    string
	}{
		{m, false, false, "up"},
		{m, false, true, "partial"},
		{m, true, true, "maintenance"},
		{store.Monitor{Active: true, Status: store.StatusDown}, false, true, "down"},
		{store.Monitor{Active: true, Status: store.StatusPending}, false, false, "pending"},
		{store.Monitor{Active: false, Status: store.StatusDown}, false, false, "paused"},
	}
	for _, c := range cases {
		if got := monitorState(c.m, c.maint, c.partial); got != c.want {
			t.Errorf("%+v maint=%v partial=%v: %s, beklenen %s", c.m, c.maint, c.partial, got, c.want)
		}
	}
}

// hasIncident kind boşsa monitör eşleşmesine, değilse türe bakar.
func hasIncident(list []topoIncident, kind string, monitorID int64) bool {
	for _, in := range list {
		if (kind != "" && in.Kind == kind) || (kind == "" && in.MonitorID == monitorID) {
			return true
		}
	}
	return false
}
