package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func (e *env) monitorOn(name string, serverID int64) monitorView {
	e.t.Helper()
	var m monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": name, "type": "http", "interval": 60,
		"timeout": 5, "server_id": serverID, "config": map[string]any{"url": "http://127.0.0.1:1/"}}, &m, 201)
	return m
}

func findTopoMonitor(t *testing.T, r topologyResp, id int64) topoMonitor {
	t.Helper()
	for _, m := range r.Monitors {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("monitör %d yanıtta yok", id)
	return topoMonitor{}
}

// Monitör-sunucu bağı yalnız seçilince oluşur, sunucu silinince boşalır; var olmayan
// sunucu ya da kontrol noktası bağlanamaz.
func TestMonitorServerLink(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var srv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &srv, 201)
	m := admin.monitorOn("Site", srv.Probe.ID)

	got := findTopoMonitor(t, admin.topology(), m.ID)
	if got.ServerID != srv.Probe.ID {
		t.Fatalf("server_id: %d", got.ServerID)
	}
	// server_id gönderilmeyen güncelleme bağı korur.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Site 2", "type": "http",
		"interval": 60, "timeout": 5, "config": map[string]any{"url": "http://127.0.0.1:1/"}}, nil, 200)
	if got := findTopoMonitor(t, admin.topology(), m.ID); got.ServerID == 0 {
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
	if got := findTopoMonitor(t, admin.topology(), m.ID); got.ServerID != 0 {
		t.Fatalf("sunucu silinince bağ boşalmalı: %d", got.ServerID)
	}
}

// Yanıttaki ilişkiler gerçek kayıtlardan gelir: kanal (doğrudan + etiket), etiket,
// son olay ve sayaçlar; izleyici ham hata metnini, etiketi ve kanalı görmez.
func TestTopologyRelations(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	var srv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "db-1"}, &srv, 201)
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", srv.Probe.ID), map[string]any{"role": "Veritabanı", "region": "Frankfurt"}, nil, 200)
	m := admin.monitorOn("Site", srv.Probe.ID)
	other := admin.httpMonitor("Bağsız", "http://127.0.0.1:1/")

	var ch struct {
		ID int64 `json:"id"`
	}
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "Nöbet", "type": "webhook",
		"config": map[string]any{"url": "https://example.test/h"}}, &ch, 201)
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/notifications", m.ID), map[string]any{"notification_ids": []int64{ch.ID}}, nil, 200)
	if err := f.st.SetProbeNotifications(ctx, srv.Probe.ID, []int64{ch.ID}); err != nil {
		t.Fatal(err)
	}
	tag := store.Tag{Name: "Kritik", Color: "#123456"}
	if err := f.st.CreateTag(ctx, &tag); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetMonitorTags(ctx, m.ID, []store.MonitorTagInput{{TagID: tag.ID}}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	if err := f.st.SetChannelSend(ctx, ch.ID, now-10, ""); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetChannelSend(ctx, ch.ID, now-5, "503 Service Unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetChannelSend(ctx, ch.ID, now-3*86400, ""); err != nil { // pencere dışı
		t.Fatal(err)
	}
	const raw = "yanıt yok: https://u:gizli@ic.example.test/yol?anahtar=1"
	if err := f.st.OpenIncident(ctx, m.ID, now-100, raw); err != nil {
		t.Fatal(err)
	}

	got := admin.topology()
	tm := findTopoMonitor(t, got, m.ID)
	if len(tm.Tags) != 1 || tm.Tags[0] != tag.ID {
		t.Fatalf("etiket: %+v", tm.Tags)
	}
	if len(tm.Channels) != 1 || tm.Channels[0] != ch.ID {
		t.Fatalf("kanal: %+v", tm.Channels)
	}
	if tm.LastIncident == nil || tm.LastIncident.Cause != raw || tm.LastIncident.ResolvedAt != 0 {
		t.Fatalf("son olay: %+v", tm.LastIncident)
	}
	if o := findTopoMonitor(t, got, other.ID); o.ServerID != 0 || o.LastIncident != nil || len(o.Channels) != 0 {
		t.Fatalf("bağsız monitöre ilişki yazılmamalı: %+v", o)
	}
	if len(got.Servers) != 1 || got.Servers[0].Role != "Veritabanı" || got.Servers[0].Region != "Frankfurt" ||
		len(got.Servers[0].Channels) != 1 {
		t.Fatalf("sunucu: %+v", got.Servers)
	}
	if len(got.Channels) != 1 || got.Channels[0].Sent24h != 2 || got.Channels[0].Failed24h != 1 ||
		got.Channels[0].LastSend == nil || got.Channels[0].LastSend.Error == "" {
		t.Fatalf("kanal sayacı: %+v", got.Channels)
	}

	viewer, _ := admin.newUser("izleyici", "viewer", nil)
	v := viewer.topology()
	vm := findTopoMonitor(t, v, m.ID)
	if len(vm.Tags) != 0 || len(vm.Channels) != 0 {
		t.Fatalf("izleyiciden gizli: %+v", vm)
	}
	if vm.LastIncident == nil || strings.Contains(vm.LastIncident.Cause, "gizli") {
		t.Fatalf("izleyici ham nedeni görmemeli: %+v", vm.LastIncident)
	}
	for _, in := range v.Incidents {
		if strings.Contains(in.Cause, "gizli") {
			t.Fatalf("açık olay ham neden: %+v", in)
		}
	}
	if len(v.Channels) != 0 || len(v.Customers) != 0 {
		t.Fatalf("izleyici kanal/müşteri: %+v %+v", v.Channels, v.Customers)
	}

	var sum topologyResp
	admin.mustDo("GET", "/api/topology?view=summary", nil, &sum, 200)
	if sm := findTopoMonitor(t, sum, m.ID); len(sm.Locations) != 0 || sm.ServerID == 0 || sum.Mesh != nil {
		t.Fatalf("özet görünüm: %+v", sm)
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
	got := b.topology()
	if len(got.Monitors) != 1 || got.Monitors[0].ServerID != other.Probe.ID {
		t.Fatalf("içe aktarım bağı: %+v", got.Monitors)
	}

	// Hedefte sunucu yoksa monitör bağsız gelir, içe aktarım bozulmaz.
	c := newServersEnv(t).env
	c.importDoc("/api/import", doc, 200)
	if got := c.topology(); len(got.Monitors) != 1 || got.Monitors[0].ServerID != 0 {
		t.Fatalf("sunucusuz içe aktarım: %+v", got.Monitors)
	}
}
