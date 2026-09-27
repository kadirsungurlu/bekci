package api

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

func TestSanitizeMessage(t *testing.T) {
	in := `Get "https://ali:***@ic.kadir.app/panel?token=gizli": remote error: tls: bad certificate`
	got := sanitizeMessage(in)
	if strings.Contains(got, "ali") || strings.Contains(got, "gizli") || !strings.Contains(got, "https://ic.kadir.app/panel") {
		t.Errorf("temizlenmedi: %s", got)
	}
}

// Güvenlik: grup monitörüne atanmış müşteri, görmediği alt monitörlerin adını
// grup mesajında görmemeli (liste, detay, olaylar).
func TestRestrictedViewerGroupMessage(t *testing.T) {
	admin := setupAdmin(t)
	child := admin.push("GizliVeritabani")
	var group monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Grup", "type": "group", "interval": 20,
		"config": map[string]any{"monitor_ids": []int64{child.ID}}}, &group, 201)
	customer, _ := admin.newUser("grupmusteri", store.RoleViewer, []int64{group.ID})

	resp, _ := http.Get(e2url(admin, "/api/push/"+child.PushToken+"?status=down&msg=baglanti+yok"))
	resp.Body.Close()
	waitFor(t, "grup DOWN", func() bool {
		var d struct{ Monitor monitorView }
		admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", group.ID), nil, &d, 200)
		return d.Monitor.Status == store.StatusDown && strings.Contains(d.Monitor.LastMessage, "GizliVeritabani")
	})

	var list []monitorView
	customer.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || strings.Contains(list[0].LastMessage, "GizliVeritabani") || list[0].LastMessage == "" {
		t.Errorf("müşteri alt monitör adını görmemeli: %+v", list)
	}
	var inc []store.Incident
	customer.mustDo("GET", "/api/incidents", nil, &inc, 200)
	for _, i := range inc {
		if strings.Contains(i.Cause, "GizliVeritabani") {
			t.Errorf("olay nedeninde alt monitör adı: %s", i.Cause)
		}
	}
	var series struct {
		Points []struct{ M string }
	}
	customer.mustDo("GET", fmt.Sprintf("/api/monitors/%d/series", group.ID), nil, &series, 200)
	for _, p := range series.Points {
		if strings.Contains(p.M, "GizliVeritabani") {
			t.Errorf("grafik mesajında alt monitör adı: %s", p.M)
		}
	}
}

// Güvenlik: HTTP/WebSocket başlıkları ve gRPC metadata maskelenir.
func TestHeadersMasked(t *testing.T) {
	var srv *Server
	admin := newEnv(t, func(s *Server) { srv = s })
	admin.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var m monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Api", "type": "http",
		"config": map[string]any{"url": "https://api.kadir.app", "headers": "Authorization: Bearer cokgizli"}}, &m, 201)
	if strings.Contains(string(m.Config), "cokgizli") {
		t.Fatalf("başlık maskelenmedi: %s", m.Config)
	}
	// Maskeli değer geri gönderilince kayıtlı başlık korunur.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Api 2", "type": "http",
		"config": map[string]any{"url": "https://api.kadir.app", "headers": "••••••"}}, nil, 200)
	stored, _ := srv.store.GetMonitor(t.Context(), m.ID)
	if !strings.Contains(string(stored.Config), "cokgizli") {
		t.Errorf("kayıtlı başlık kayboldu: %s", stored.Config)
	}
	for _, typ := range []string{"grpc", "websocket"} {
		if len(monitorSecrets[typ]) == 0 {
			t.Errorf("%s gizli alan listesinde yok", typ)
		}
	}
}
