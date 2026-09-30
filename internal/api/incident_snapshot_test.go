package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// QA 2. tur O1: olay ayrıntısı olay anındaki tipi ve hedefi gösterir; monitör
// sonradan başka tip/hedefe çevrildiyse "değiştirildi" bilgisi ve güncel
// değerler de döner.
func TestIncidentMonitorSnapshot(t *testing.T) {
	admin := setupAdmin(t)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer site.Close()
	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Değişen", "type": "http", "interval": 20, "retry_interval": 20, "max_retries": 0, "timeout": 5,
		"config": map[string]any{"url": site.URL + "/saglik"},
	}, &mon, 201)
	var list []store.Incident
	waitFor(t, "olay", func() bool {
		admin.mustDo("GET", "/api/incidents", nil, &list, 200)
		return len(list) == 1
	})
	path := fmt.Sprintf("/api/incidents/%d", list[0].ID)
	var before incidentResp
	admin.mustDo("GET", path, nil, &before, 200)
	if before.Monitor.Changed || before.Monitor.Type != "http" {
		t.Fatalf("değişmemiş monitör: %+v", before.Monitor)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", mon.ID), map[string]any{
		"name": "Değişen", "type": "tcp", "interval": 20, "retry_interval": 20, "max_retries": 0, "timeout": 5,
		"config": map[string]any{"host": "127.0.0.1", "port": 1},
	}, nil, 200)
	var after incidentResp
	admin.mustDo("GET", path, nil, &after, 200)
	m := after.Monitor
	if !m.Changed || m.Type != "http" || !strings.Contains(m.Target, "/saglik") || m.CurrentType != "tcp" || m.CurrentTarget != "127.0.0.1:1" {
		t.Fatalf("anlık görüntü: %+v", m)
	}
}
