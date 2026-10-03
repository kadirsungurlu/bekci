package api

import (
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// TestProbeNotifySettings: kontrol noktasının çevrimdışı bildirimi ve
// kanalları PUT /api/probes/{id} ile ayarlanır, GET listesinde geri gelir;
// olmayan kanal reddedilir; alanlar gönderilmezse değişmez.
func TestProbeNotifySettings(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	var ch map[string]any
	admin.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Kanca", "type": "webhook", "config": map[string]any{"url": "http://127.0.0.1:1/x"},
	}, &ch, 201)
	chID := int64(ch["id"].(float64))
	cp := admin.newProbe("Frankfurt")
	if cp.Probe.NotifyOffline || len(cp.Probe.NotificationIDs) != 0 {
		t.Fatalf("varsayılan: bildirim kapalı, kanal yok: %+v", cp.Probe)
	}

	var out probeAdminView
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{
		"name": "Frankfurt", "active": true, "notify_offline": true, "notification_ids": []int64{chID, chID},
	}, &out, 200)
	if !out.NotifyOffline || len(out.NotificationIDs) != 1 || out.NotificationIDs[0] != chID {
		t.Fatalf("ayar kaydedilmedi: %+v", out)
	}
	var list []probeAdminView
	admin.mustDo("GET", "/api/probes", nil, &list, 200)
	if len(list) != 1 || !list[0].NotifyOffline || len(list[0].NotificationIDs) != 1 {
		t.Fatalf("listede ayar yok: %+v", list)
	}
	p, _ := f.st.GetProbe(t.Context(), cp.Probe.ID)
	if !p.NotifyOffline {
		t.Error("veritabanında notify_offline açık olmalı")
	}
	// Kısmi güncelleme: ad gönderilmezse değişmez (API istemcisi yalnızca
	// bildirim ayarını değiştirebilmeli).
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"notify_offline": false}, &out, 200)
	if out.Name != "Frankfurt" || out.NotifyOffline || len(out.NotificationIDs) != 1 {
		t.Fatalf("adsız kısmi güncelleme: %+v", out)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"notify_offline": true}, &out, 200)
	if !out.NotifyOffline {
		t.Error("kısmi güncelleme notify_offline açmalı")
	}
	// Alanlar gönderilmezse değişmez (eski arayüz).
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "Frankfurt", "active": true}, &out, 200)
	if !out.NotifyOffline || len(out.NotificationIDs) != 1 {
		t.Errorf("gönderilmeyen alan değişmemeli: %+v", out)
	}
	// Olmayan kanal reddedilir.
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{
		"name": "Frankfurt", "active": true, "notification_ids": []int64{999},
	}, nil, 400)
	// Kapatma ve kanalları boşaltma.
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{
		"name": "Frankfurt", "active": true, "notify_offline": false, "notification_ids": []int64{},
	}, &out, 200)
	if out.NotifyOffline || len(out.NotificationIDs) != 0 {
		t.Errorf("kapatılmalı: %+v", out)
	}
	// Olay süzgeci "server" grubu kontrol noktası olaylarını da kapsar.
	if _, err := f.st.OpenProbeIncident(t.Context(), cp.Probe.ID, 1000, 900); err != nil {
		t.Fatal(err)
	}
	var incs []store.Incident
	admin.mustDo("GET", "/api/incidents?kind=server", nil, &incs, 200)
	if len(incs) != 1 || incs[0].Kind != store.IncidentProbeOffline || incs[0].ServerName != "Frankfurt" || incs[0].Cause != "Kontrol noktasına ulaşılamıyor" {
		t.Fatalf("kontrol noktası olayı listede: %+v", incs)
	}
	var det incidentDetailView
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", incs[0].ID), nil, &det, 200)
	if det.Server == nil || det.Server.Name != "Frankfurt" || det.Incident.Kind != store.IncidentProbeOffline {
		t.Errorf("olay ayrıntısı: %+v", det)
	}
}
