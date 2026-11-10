package api

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"testing"
)

func (e *env) monitorOn(name string, serverID int64) monitorView {
	e.t.Helper()
	var m monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": name, "type": "push", "interval": 3600,
		"timeout": 5, "server_id": serverID, "config": map[string]any{}}, &m, 201)
	return m
}

// Monitör-sunucu bağı yalnız seçilince oluşur, sunucu silinince boşalır; var olmayan
// sunucu ya da kontrol noktası bağlanamaz.
// monitorServer monitörün bağlı olduğu sunucu (GET /api/monitors/{id} server_id).
func (e *env) monitorServer(id int64) int64 {
	e.t.Helper()
	var m struct {
		Monitor struct {
			ServerID int64 `json:"server_id"`
		} `json:"monitor"`
	}
	e.mustDo("GET", fmt.Sprintf("/api/monitors/%d", id), nil, &m, 200)
	return m.Monitor.ServerID
}

// monitorServers tüm monitörlerin sunucu bağları (GET /api/monitors sırasıyla).
func (e *env) monitorServers() []int64 {
	e.t.Helper()
	var ms []struct {
		ServerID int64 `json:"server_id"`
	}
	e.mustDo("GET", "/api/monitors", nil, &ms, 200)
	out := make([]int64, len(ms))
	for i, m := range ms {
		out[i] = m.ServerID
	}
	return out
}

func TestMonitorServerLink(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var srv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &srv, 201)
	m := admin.monitorOn("Site", srv.Probe.ID)

	got := admin.monitorServer(m.ID)
	if got != srv.Probe.ID {
		t.Fatalf("server_id: %d", got)
	}
	// server_id gönderilmeyen güncelleme bağı korur.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Site 2", "type": "push",
		"interval": 3600, "timeout": 5, "config": map[string]any{}}, nil, 200)
	if got := admin.monitorServer(m.ID); got == 0 {
		t.Fatal("alan yokken bağ silinmemeli")
	}

	loc := admin.newProbe("Nokta")
	for _, bad := range []int64{loc.Probe.ID, 999999} {
		var raw json.RawMessage
		if code := admin.do("POST", "/api/monitors", map[string]any{"name": "x", "type": "http", "interval": 60,
			"timeout": 5, "server_id": bad, "config": map[string]any{"url": "http://a.test/"}}, &raw); code != 400 {
			t.Fatalf("server_id=%d kod %d", bad, code)
		}
	}

	admin.mustDo("DELETE", fmt.Sprintf("/api/probes/%d", srv.Probe.ID), nil, nil, 200)
	if got := admin.monitorServer(m.ID); got != 0 {
		t.Fatalf("sunucu silinince bağ boşalmalı: %d", got)
	}
}

// Öneri yalnız tam eşleşmede gelir ve hiçbir şey kaydetmez.
func TestServerSuggestion(t *testing.T) {
	for in, want := range map[string]string{
		"https://Web.Example.com:8443/x?y=1": "web.example.com",
		"example.com:443":                    "example.com",
		"10.0.0.5":                           "10.0.0.5",
		"[2001:db8::1]:80":                   "2001:db8::1",
		"  Host.Local. ":                     "host.local",
		"":                                   "",
	} {
		if got := targetHost(in); got != want {
			t.Errorf("targetHost(%q) = %q, beklenen %q", in, got, want)
		}
	}
	ip := netip.MustParseAddr("203.0.113.7")
	keys := []serverKeys{{ID: 1, Name: "web-1", Hostname: "web1.example.com", IPs: []netip.Addr{ip}}, {ID: 2, Name: "db-1"}}
	if got := matchServers("203.0.113.7", nil, keys); len(got) != 1 || got[0].ServerID != 1 || got[0].Match != "ip" {
		t.Fatalf("ip: %+v", got)
	}
	if got := matchServers("web1.example.com", nil, keys); len(got) != 1 || got[0].Match != "hostname" {
		t.Fatalf("hostname: %+v", got)
	}
	if got := matchServers("db-1", nil, keys); len(got) != 1 || got[0].ServerID != 2 || got[0].Match != "name" {
		t.Fatalf("ad: %+v", got)
	}
	if got := matchServers("shop.example.com", []netip.Addr{ip}, keys); len(got) != 1 || got[0].Match != "dns" {
		t.Fatalf("dns: %+v", got)
	}
	// Kısmi benzerlik öneri sayılmaz.
	for _, h := range []string{"web1.example.com.tr", "example.com", "db-10", "203.0.113.70"} {
		if got := matchServers(h, nil, keys); len(got) != 0 {
			t.Fatalf("%s eşleşmemeli: %+v", h, got)
		}
	}

	f := newServersEnv(t)
	admin := f.env
	var srv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "api-1"}, &srv, 201)
	var out struct {
		Host    string        `json:"host"`
		Matches []serverMatch `json:"matches"`
	}
	admin.mustDo("POST", "/api/monitors/server-suggestion", map[string]any{"type": "tcp",
		"config": map[string]any{"host": "api-1", "port": 443}}, &out, 200)
	if out.Host != "api-1" || len(out.Matches) != 1 || out.Matches[0].ServerID != srv.Probe.ID {
		t.Fatalf("öneri: %+v", out)
	}
	viewer, _ := admin.newUser("izle", "viewer", nil)
	var raw json.RawMessage
	if code := viewer.do("POST", "/api/monitors/server-suggestion", map[string]any{"type": "tcp",
		"config": map[string]any{"host": "api-1"}}, &raw); code != 403 {
		t.Fatalf("izleyici öneri kodu %d", code)
	}
}

// Yedek sunucuyu adla taşır: geri yüklemede var olan sunucuya bağlanır.
func TestBackupMonitorServer(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var srv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "yedek-sunucu"}, &srv, 201)
	admin.monitorOn("Bağlı", srv.Probe.ID)
	doc, _ := admin.exportDoc()
	if len(doc.Monitors) != 1 || doc.Monitors[0].Server != "yedek-sunucu" {
		t.Fatalf("dışa aktarım: %+v", doc.Monitors)
	}

	b := newServersEnv(t).env
	var other serverSetup
	b.mustDo("POST", "/api/servers", map[string]any{"name": "yedek-sunucu"}, &other, 201)
	b.importDoc("/api/import", doc, 200)
	got := b.monitorServers()
	if len(got) != 1 || got[0] != other.Probe.ID {
		t.Fatalf("içe aktarım bağı: %v", got)
	}

	// Hedefte sunucu yoksa monitör bağsız gelir, içe aktarım bozulmaz.
	c := newServersEnv(t).env
	c.importDoc("/api/import", doc, 200)
	if got := c.monitorServers(); len(got) != 1 || got[0] != 0 {
		t.Fatalf("sunucusuz içe aktarım: %v", got)
	}
}
