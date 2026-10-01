package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// TestMetricsAgents: /metrics sunucu ajanının son örneğini, kontrol noktasının
// durumunu ve açık olay sayılarını verir; kısıtlı kullanıcı yalnızca atanmış
// sunucuyu görür, kontrol noktalarını görmez.
func TestMetricsAgents(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var cp serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &cp, 201)
	var other serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "db-1"}, &other, 201)
	loc := admin.newProbe("Frankfurt")
	if code, _, body := admin.rawReq("POST", "/api/probe/metrics", bearer(cp.Token), testSample(12.5)); code != 204 {
		t.Fatalf("örnek: %d %s", code, body)
	}
	// Kontrol noktası bir kez görünür (çevrimiçi), sonra bir açık monitör olayı.
	if code, _, body := admin.rawReq("GET", "/api/probe/jobs", bearer(loc.Token), nil); code != 200 {
		t.Fatalf("iş listesi: %d %s", code, body)
	}
	m := admin.push("Site")
	if err := f.st.OpenIncident(context.Background(), m.ID, time.Now().Unix()-60, "HTTP 500"); err != nil {
		t.Fatal(err)
	}

	k := f.newKey("prom", store.RoleViewer, 0)
	code, _, body := admin.rawReq("GET", "/metrics", bearer(k.Secret), nil)
	if code != 200 {
		t.Fatalf("metrik: %d %s", code, body)
	}
	text := string(body)
	sl := fmt.Sprintf(`server_id="%d",server_name="web-1"`, cp.Probe.ID)
	for _, want := range []string{
		"# TYPE uptime_server_online gauge",
		"uptime_server_online{" + sl + "} 1",
		fmt.Sprintf(`uptime_server_online{server_id="%d",server_name="db-1"} 0`, other.Probe.ID),
		"uptime_server_cpu_percent{" + sl + "} 12.5",
		"uptime_server_memory_percent{" + sl + "} 40",
		"uptime_server_load1_per_core{" + sl + "} 0.125",
		`uptime_server_disk_percent{` + sl + `,mount="/"} 71`,
		"uptime_server_temperature_celsius{" + sl + "} 54.5",
		"uptime_server_uptime_seconds{" + sl + "} 3600",
		"uptime_server_containers{" + sl + "} 1",
		fmt.Sprintf(`uptime_probe_online{probe_id="%d",probe_name="Frankfurt"} 1`, loc.Probe.ID),
		`uptime_incidents_open{kind="monitor"} 1`,
		`uptime_incidents_open{kind="server_offline"} 0`,
	} {
		if !strings.Contains(text, want+"\n") {
			t.Errorf("metrikte yok: %s\n---\n%s", want, text)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.HasPrefix(line, "# ") {
			continue
		}
		if !promLine.MatchString(line) {
			t.Errorf("geçersiz metrik satırı: %q", line)
		}
	}

	// Kısıtlı kullanıcı: yalnızca db-1 atanmış; web-1, kontrol noktası ve
	// başkasının olayı görünmez.
	cust, u := f.newUser("musteri", store.RoleViewer, []int64{})
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", u.ID), map[string]any{
		"username": "musteri", "role": store.RoleViewer, "all_monitors": false, "monitor_ids": []int64{}, "server_ids": []int64{other.Probe.ID},
	}, nil, 200)
	ck := cust.newKey("m", store.RoleViewer, 0)
	_, _, body = cust.rawReq("GET", "/metrics", bearer(ck.Secret), nil)
	text = string(body)
	switch {
	case strings.Contains(text, `server_name="web-1"`):
		t.Errorf("kısıtlı kullanıcı web-1'i görmemeli:\n%s", text)
	case !strings.Contains(text, `server_name="db-1"`):
		t.Errorf("kısıtlı kullanıcı db-1'i görmeli:\n%s", text)
	case strings.Contains(text, "uptime_probe_online{"):
		t.Errorf("kısıtlı kullanıcı kontrol noktalarını görmemeli:\n%s", text)
	case !strings.Contains(text, `uptime_incidents_open{kind="monitor"} 0`):
		t.Errorf("kısıtlı kullanıcının olay sayısı 0 olmalı:\n%s", text)
	}
}
