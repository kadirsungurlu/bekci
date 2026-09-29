package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Geniş ekran listesi: monitör görünümünde son yanıt süreleri ve 7/30 günlük
// oran; sunucu görünümünde CPU geçmişi ve (yalnızca yöneticiye) ajanın IP'si.
func TestListWideColumns(t *testing.T) {
	f := newServersEnv(t)
	ctx := context.Background()

	var m struct {
		ID int64 `json:"id"`
	}
	f.mustDo("POST", "/api/monitors", map[string]any{"name": "Yedek", "type": "push", "interval": 3600, "config": map[string]any{}}, &m, 201)
	now := time.Now().Unix()
	for i, b := range []struct {
		status int
		ping   int64
	}{{store.StatusUp, 100}, {store.StatusDown, 5000}, {store.StatusUp, -1}, {store.StatusUp, 120}} {
		if err := f.st.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: m.ID, Time: now - int64(40-i), Status: b.status, PingMs: b.ping}}); err != nil {
			t.Fatal(err)
		}
	}
	type wideView struct {
		ID        int64    `json:"id"`
		Pings     []int64  `json:"pings"`
		Uptime7d  *float64 `json:"uptime_7d"`
		Uptime30d *float64 `json:"uptime_30d"`
	}
	check := func(v wideView) {
		t.Helper()
		if !slices.Equal(v.Pings, []int64{100, store.PingDown, store.PingNone, 120}) {
			t.Errorf("pings: %v", v.Pings)
		}
		if v.Uptime7d == nil || v.Uptime30d == nil || *v.Uptime7d != 75 || *v.Uptime30d != 75 {
			t.Errorf("uptime 7/30: %v %v", v.Uptime7d, v.Uptime30d)
		}
	}
	var list []wideView
	f.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 {
		t.Fatalf("liste: %+v", list)
	}
	check(list[0])
	var one struct {
		Monitor wideView `json:"monitor"`
	}
	f.mustDo("GET", fmt.Sprintf("/api/monitors/%d", m.ID), nil, &one, 200)
	check(one.Monitor)

	// Sunucu: CPU geçmişi listede, IP yalnızca yöneticide.
	var cp serverSetup
	f.mustDo("POST", "/api/servers", map[string]any{"name": "Sunucu"}, &cp, 201)
	if code := probeCall(t, f.srv.URL, cp.Token, "POST", "/api/probe/metrics", testSample(37)); code != 204 {
		t.Fatalf("metrik: %d", code)
	}
	type srvView struct {
		IP      string    `json:"ip"`
		CPUHist []float64 `json:"cpu_hist"`
	}
	var sl struct {
		Servers []srvView `json:"servers"`
	}
	f.mustDo("GET", "/api/servers", nil, &sl, 200)
	if len(sl.Servers) != 1 || sl.Servers[0].IP == "" || !slices.Equal(sl.Servers[0].CPUHist, []float64{37}) {
		t.Fatalf("yönetici listesi: %+v", sl)
	}
	u, _ := f.newUser("izleyici", store.RoleViewer, nil)
	sl.Servers = nil // omitempty: eski değer kalmasın
	u.mustDo("GET", "/api/servers", nil, &sl, 200)
	if len(sl.Servers) != 1 || sl.Servers[0].IP != "" {
		t.Errorf("izleyici IP'yi görmemeli: %+v", sl)
	}
	msg, _ := json.Marshal(map[string]any{"type": "server", "data": servers.View{ID: 1, IP: "203.0.113.9"}})
	if out := viewerEvent(store.User{Role: store.RoleEditor}, msg, nil); bytes.Contains(out, []byte("203.0.113.9")) {
		t.Errorf("canlı akışta IP: %s", out)
	}
	if out := viewerEvent(store.User{Role: store.RoleAdmin}, msg, nil); !bytes.Contains(out, []byte("203.0.113.9")) {
		t.Errorf("yönetici IP'yi görmeli: %s", out)
	}
}
