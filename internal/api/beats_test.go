package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// TestMonitorBeatsAPI son kontroller tablosu: yeniden eskiye, limit, mesaj
// çevirisi, konum ve görünürlük kısıtı.
func TestMonitorBeatsAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	m := store.Monitor{Name: "Site", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 10, Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := f.st.CreateMonitor(ctx, &m, nil); err != nil {
		t.Fatal(err)
	}
	other := store.Monitor{Name: "Gizli", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 10, Config: json.RawMessage(`{"url":"https://example.org"}`)}
	if err := f.st.CreateMonitor(ctx, &other, nil); err != nil {
		t.Fatal(err)
	}
	for i := int64(1); i <= 70; i++ {
		b := store.Beat{MonitorID: m.ID, Time: 1000 + i, Status: store.StatusUp, PingMs: i, Message: "200 OK"}
		if i == 70 {
			b.Status, b.PingMs, b.Message, b.Location = store.StatusDown, -1, "zaman aşımı", "Yerel, Frankfurt"
		}
		if err := f.st.RecordBeat(ctx, store.BeatUpdate{Beat: b}); err != nil {
			t.Fatal(err)
		}
	}
	type row struct {
		Time     int64  `json:"time"`
		Status   int    `json:"status"`
		PingMs   int64  `json:"ping_ms"`
		Message  string `json:"message"`
		Location string `json:"location"`
	}
	var rows []row
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/beats", m.ID), nil, &rows, 200)
	if len(rows) != 50 || rows[0].Time != 1070 || rows[49].Time != 1021 {
		t.Fatalf("varsayılan 50, yeniden eskiye: %d", len(rows))
	}
	if rows[0].Status != store.StatusDown || rows[0].PingMs != -1 || rows[0].Location != "Yerel, Frankfurt" || rows[0].Message != "zaman aşımı" {
		t.Fatalf("ilk satır: %+v", rows[0])
	}
	if rows[1].PingMs != 69 || rows[1].Location != "" || rows[1].Message != "200 OK" {
		t.Fatalf("ikinci satır: %+v", rows[1])
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/beats?limit=10", m.ID), nil, &rows, 200)
	if len(rows) != 10 {
		t.Fatalf("limit 10: %d", len(rows))
	}
	// İngilizce yanıt: saklanan Türkçe mesaj çevrilir.
	admin.langGet("en", fmt.Sprintf("/api/monitors/%d/beats?limit=1", m.ID), &rows)
	if len(rows) != 1 || rows[0].Message != "timeout" {
		t.Fatalf("mesaj çevrilmeli: %+v", rows)
	}
	// Kısıtlı kullanıcı yalnızca kendi monitörünü görür.
	cust, _ := admin.newUser("musteri", store.RoleViewer, []int64{m.ID})
	cust.mustDo("GET", fmt.Sprintf("/api/monitors/%d/beats", m.ID), nil, &rows, 200)
	cust.mustDo("GET", fmt.Sprintf("/api/monitors/%d/beats", other.ID), nil, nil, 404)
	admin.mustDo("GET", "/api/monitors/99999/beats", nil, nil, 404)
}
