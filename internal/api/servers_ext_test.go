package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// TestServerRulesLevelsAndMaintenance: uyarı/kritik eşikleri, konteyner ve
// yeniden başlatma kuralları, kanal seviyesi bağlantıları ve sunucuları
// kapsayan bakım penceresi (API uçtan uca).
func TestServerRulesLevelsAndMaintenance(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	hook := store.Notification{Name: "Genel", Type: "webhook", Config: json.RawMessage(`{"url":"http://127.0.0.1:1/x","method":"POST"}`), Active: true}
	if err := f.st.CreateNotification(ctx, &hook, false); err != nil {
		t.Fatal(err)
	}
	crit := store.Notification{Name: "Kritik hat", Type: "webhook", Config: json.RawMessage(`{"url":"http://127.0.0.1:1/y","method":"POST"}`), Active: true}
	f.st.CreateNotification(ctx, &crit, false)

	var cp serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Üretim"}, &cp, 201)
	id := cp.Probe.ID
	// İlk örnek varsayılan kuralları oluşturur.
	if code, _, body := admin.rawReq("POST", "/api/probe/metrics", bearer(cp.Token), testSample(5)); code != 204 {
		t.Fatalf("örnek: %d %s", code, body)
	}

	// Varsayılan kurallarda yeniden başlatma da var.
	var det struct {
		Alerts   []store.ServerAlert              `json:"alerts"`
		Bindings []store.ProbeNotificationBinding `json:"notification_bindings"`
		InMaint  bool                             `json:"in_maintenance"`
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	var hasReboot bool
	for _, a := range det.Alerts {
		hasReboot = hasReboot || a.Metric == "reboot"
	}
	if !hasReboot {
		t.Fatalf("varsayılan kurallarda reboot olmalı: %+v", det.Alerts)
	}

	// Doğrulama: uyarı eşiği kritikten küçük olmalı; konteyner adı biçimi;
	// aynı konteyner için iki kural olmaz.
	alerts := fmt.Sprintf("/api/servers/%d/alerts", id)
	bad := [][]map[string]any{
		{{"metric": "cpu", "threshold": 80, "warn_threshold": 80, "minutes": 5}},
		{{"metric": "cpu", "threshold": 80, "warn_threshold": 95, "minutes": 5}},
		{{"metric": "mem", "threshold": 80, "warn_threshold": 0.5, "minutes": 5}},
		{{"metric": "container", "mount": "kötü ad!", "minutes": 2}},
		{{"metric": "container", "mount": "web", "minutes": 2}, {"metric": "container", "mount": "web", "minutes": 3}},
		{{"metric": "reboot", "minutes": 1}, {"metric": "reboot", "minutes": 1}},
	}
	for _, b := range bad {
		admin.mustDo("PUT", alerts, map[string]any{"alerts": b}, nil, 400)
	}
	var saved struct {
		Alerts []store.ServerAlert `json:"alerts"`
	}
	admin.mustDo("PUT", alerts, map[string]any{"alerts": []map[string]any{
		{"metric": "cpu", "threshold": 90, "warn_threshold": 70, "minutes": 5},
		{"metric": "container", "mount": "", "minutes": 2},
		{"metric": "container", "mount": "web", "minutes": 3},
		{"metric": "reboot", "threshold": 42, "minutes": 9},
	}}, &saved, 200)
	if len(saved.Alerts) != 4 {
		t.Fatalf("kurallar: %+v", saved.Alerts)
	}
	byKey := map[string]store.ServerAlert{}
	for _, a := range saved.Alerts {
		byKey[a.Metric+"|"+a.Mount] = a
	}
	if byKey["cpu|"].WarnThreshold != 70 || byKey["cpu|"].Threshold != 90 {
		t.Fatalf("cpu uyarı eşiği: %+v", byKey["cpu|"])
	}
	if byKey["container|web"].Minutes != 3 || byKey["container|"].Minutes != 2 {
		t.Fatalf("konteyner kuralları: %+v", byKey)
	}
	if rb := byKey["reboot|"]; rb.Threshold != 0 || rb.WarnThreshold != 0 || rb.Minutes != 1 {
		t.Fatalf("reboot kuralı eşiksiz ve 1 dk olmalı: %+v", rb)
	}
	// GET→PUT tur: okunan kurallar (arayüzün gönderdiği alanlarla) aynen kaydedilebilir.
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	again := make([]map[string]any, 0, len(det.Alerts))
	for _, a := range det.Alerts {
		again = append(again, map[string]any{"metric": a.Metric, "mount": a.Mount, "threshold": a.Threshold,
			"warn_threshold": a.WarnThreshold, "minutes": a.Minutes, "active": a.Active})
	}
	admin.mustDo("PUT", alerts, map[string]any{"alerts": again}, &saved, 200)
	if len(saved.Alerts) != 4 || saved.Alerts[0].ID == 0 {
		t.Fatalf("tur sonrası kurallar: %+v", saved.Alerts)
	}

	// Kanal bağlantıları: seviye ile.
	notifs := fmt.Sprintf("/api/servers/%d/notifications", id)
	admin.mustDo("PUT", notifs, map[string]any{"notification_bindings": []map[string]any{{"notification_id": hook.ID, "level": "yüksek"}}}, nil, 400)
	admin.mustDo("PUT", notifs, map[string]any{"notification_bindings": []map[string]any{{"notification_id": 9999, "level": ""}}}, nil, 400)
	var out struct {
		NotificationIDs []int64                          `json:"notification_ids"`
		Bindings        []store.ProbeNotificationBinding `json:"notification_bindings"`
	}
	admin.mustDo("PUT", notifs, map[string]any{"notification_bindings": []map[string]any{
		{"notification_id": hook.ID, "level": ""},
		{"notification_id": crit.ID, "level": "critical"},
	}}, &out, 200)
	if len(out.NotificationIDs) != 2 || len(out.Bindings) != 2 {
		t.Fatalf("bağlantılar: %+v", out)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	lvl := map[int64]string{}
	for _, b := range det.Bindings {
		lvl[b.NotificationID] = b.Level
	}
	if lvl[hook.ID] != "" || lvl[crit.ID] != "critical" {
		t.Fatalf("seviye bağlantıları: %+v", det.Bindings)
	}
	chans, _ := f.st.NotificationsForProbe(ctx, id)
	for _, c := range chans {
		if c.ID == crit.ID && c.BindLevel != "critical" {
			t.Fatalf("kanal seviyesi taşınmalı: %+v", c)
		}
	}
	// Eski istemci: yalnızca notification_ids gönderir, seviye sıfırlanır.
	admin.mustDo("PUT", notifs, map[string]any{"notification_ids": []int64{crit.ID}}, &out, 200)
	if len(out.Bindings) != 1 || out.Bindings[0].Level != "" {
		t.Fatalf("eski istemci bağlantısı: %+v", out)
	}

	// Bakım penceresi: yalnızca sunucu seçmek yeter; bilinmeyen sunucu 400.
	admin.mustDo("POST", "/api/maintenance", map[string]any{
		"title": "x", "strategy": "manual", "server_ids": []int64{99999},
	}, nil, 400)
	var mw struct {
		ID         int64   `json:"id"`
		Status     string  `json:"status"`
		AllServers bool    `json:"all_servers"`
		ServerIDs  []int64 `json:"server_ids"`
	}
	admin.mustDo("POST", "/api/maintenance", map[string]any{
		"title": "Çekirdek güncellemesi", "strategy": "once",
		"start": localTime(-time.Hour), "end": localTime(time.Hour), "server_ids": []int64{id},
	}, &mw, 201)
	if mw.Status != "active" || len(mw.ServerIDs) != 1 || mw.ServerIDs[0] != id || mw.AllServers {
		t.Fatalf("sunucu bakımı: %+v", mw)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	if !det.InMaint {
		t.Fatal("sunucu bakımda görünmeli")
	}
	// Tüm sunucular seçeneği.
	admin.mustDo("PUT", fmt.Sprintf("/api/maintenance/%d", mw.ID), map[string]any{
		"title": "Çekirdek güncellemesi", "strategy": "once",
		"start": localTime(-time.Hour), "end": localTime(time.Hour), "all_servers": true,
	}, &mw, 200)
	if !mw.AllServers || len(mw.ServerIDs) != 0 {
		t.Fatalf("tüm sunucular: %+v", mw)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	if !det.InMaint {
		t.Fatal("tüm sunucular bakımında sunucu bakımda görünmeli")
	}
	admin.mustDo("DELETE", fmt.Sprintf("/api/maintenance/%d", mw.ID), nil, nil, 200)
	admin.mustDo("GET", fmt.Sprintf("/api/servers/%d", id), nil, &det, 200)
	if det.InMaint {
		t.Fatal("bakım silinince sunucu bakımdan çıkmalı")
	}
}
