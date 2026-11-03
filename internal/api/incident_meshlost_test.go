package api

import (
	"context"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kontrol noktalarına ulaşılamadan yalnız ana sunucunun sonucuyla açılan
// olayda ayrıntı mesh_lost döner; olağan olayda dönmez.
func TestIncidentMeshLost(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Lin4", "type": "tcp", "interval": 60, "retry_interval": 60, "max_retries": 0, "timeout": 5,
		"config": map[string]any{"host": "127.0.0.1", "port": 1},
	}, &mon, 201)
	ctx := context.Background()
	open := func(data string) int64 {
		id, err := srv.store.StartIncident(ctx, mon.ID, 1000, "Ana sunucu: Zaman aşımı")
		if err != nil {
			t.Fatal(err)
		}
		if err := srv.store.AddIncidentEvents(ctx, id, store.IncidentEvent{Time: 1000, Kind: store.EventDown, Location: "Ana sunucu",
			Message: "Zaman aşımı", Data: []byte(data)}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	var got struct {
		MeshLost  bool                   `json:"mesh_lost"`
		Locations []incidentLocationView `json:"locations"`
	}
	id := open(`{"locations":[{"probe_id":0,"name":"Ana sunucu","status":"down"},{"probe_id":3,"name":"Frankfurt","status":"unknown"}],"mesh_lost":true}`)
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", id), nil, &got, 200)
	if !got.MeshLost || len(got.Locations) != 2 {
		t.Fatalf("mesh_lost bekleniyordu: %+v", got)
	}
	if _, err := srv.store.ResolveIncident(ctx, mon.ID, 1100); err != nil {
		t.Fatal(err)
	}
	got.MeshLost = false
	id2 := open(`{"locations":[{"probe_id":0,"name":"Ana sunucu","status":"down"}]}`)
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", id2), nil, &got, 200)
	if got.MeshLost {
		t.Fatal("olağan olayda mesh_lost olmamalı")
	}
}

// 420: yeni varsayılan (all + kısmi erişim bildirimi) yalnız arayüzde yeni
// monitör oluştururken uygulanır; sunucu mevcut/yeni kaydın kuralını değiştirmez.
func TestLocationDefaultsUnchanged(t *testing.T) {
	admin, _ := setupAdminSrv(t)
	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Eski", "type": "tcp", "interval": 60, "retry_interval": 60, "max_retries": 0, "timeout": 5,
		"config": map[string]any{"host": "127.0.0.1", "port": 1},
	}, &mon, 201)
	var loc struct {
		DownWhen      string `json:"down_when"`
		NotifyPartial bool   `json:"notify_partial"`
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", mon.ID), nil, &loc, 200)
	if loc.DownWhen != "any" || loc.NotifyPartial {
		t.Fatalf("mevcut kural değişmemeli: %+v", loc)
	}
}
