package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

type meshFieldErr struct {
	Error string `json:"error"`
	Field string `json:"field"`
}

type meshProbeResp struct {
	ID                int64              `json:"id"`
	Kind              string             `json:"kind"`
	MeshPingSec       *int               `json:"mesh_ping_sec"`
	MeshLossThreshold *int               `json:"mesh_loss_threshold"`
	PanelSec          *int               `json:"panel_sec"`
	MeshAddr          *string            `json:"mesh_addr"`
	FallbackNotifier  *bool              `json:"fallback_notifier"`
	Timing            *store.ProbeTiming `json:"timing"`
}

func TestMeshSettingsValidation(t *testing.T) {
	e := setupAdmin(t)
	cases := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"mesh_ping_sec": 61}, "mesh_ping_sec"},
		{map[string]any{"mesh_loss_threshold": 11}, "mesh_loss_threshold"},
		{map[string]any{"probe_panel_sec": 90}, "panel_sec"},
		// eşik x aralık < 3 sn
		{map[string]any{"mesh_loss_threshold": 1, "mesh_ping_sec": 2}, "mesh_loss_threshold"},
	}
	for _, c := range cases {
		var fe meshFieldErr
		if code := e.do("PUT", "/api/settings", c.body, &fe); code != 400 || fe.Field != c.field || fe.Error == "" {
			t.Fatalf("%v: 400 + alan %q bekleniyordu: %d %+v", c.body, c.field, code, fe)
		}
	}
	e.mustDo("PUT", "/api/settings", map[string]any{"mesh_enabled": false, "mesh_ping_sec": 10, "mesh_loss_threshold": 2, "probe_panel_sec": 4}, nil, 200)
	var st store.AppSettings
	e.mustDo("GET", "/api/settings", nil, &st, 200)
	if st.MeshOn() || st.MeshPingSec != 10 || st.MeshLossThreshold != 2 || st.ProbePanelSec != 4 {
		t.Fatalf("mesh ayarları kaydedilmeli: %+v", st)
	}
}

func TestProbeMeshOverrides(t *testing.T) {
	e := setupAdmin(t)
	var cp createdProbe
	e.mustDo("POST", "/api/probes", map[string]any{"name": "Frankfurt"}, &cp, 201)
	path := fmt.Sprintf("/api/probes/%d", cp.Probe.ID)

	var p meshProbeResp
	e.mustDo("PUT", path, map[string]any{"mesh_ping_sec": 10, "panel_sec": 2, "mesh_addr": " 10.0.0.5:7480 ", "fallback_notifier": false}, &p, 200)
	if p.MeshPingSec == nil || *p.MeshPingSec != 10 || p.PanelSec == nil || *p.PanelSec != 2 || p.MeshLossThreshold != nil ||
		*p.MeshAddr != "10.0.0.5:7480" || *p.FallbackNotifier || p.Timing == nil || p.Timing.PingSec != 10 || p.Timing.LossThreshold != 3 {
		t.Fatalf("ezmeler uygulanmalı: %+v %+v", p, p.Timing)
	}

	// Alan gönderilmezse (eski istemci) mesh değerleri korunur.
	e.mustDo("PUT", path, map[string]any{"name": "Frankfurt 2"}, &p, 200)
	if p.MeshPingSec == nil || *p.MeshPingSec != 10 || *p.MeshAddr != "10.0.0.5:7480" {
		t.Fatalf("gönderilmeyen alan korunmalı: %+v", p)
	}

	// null: genel değere döner.
	e.mustDo("PUT", "/api/settings", map[string]any{"mesh_ping_sec": 7}, nil, 200)
	e.mustDo("PUT", path, map[string]any{"mesh_ping_sec": nil}, &p, 200)
	if p.MeshPingSec != nil || p.Timing.PingSec != 7 || p.Timing.PanelSec != 2 {
		t.Fatalf("null ezme genel değere dönmeli: %+v %+v", p, p.Timing)
	}

	// Geçersiz değerler: 400 + alan; hiçbir şey değişmez.
	cases := []struct {
		body  map[string]any
		field string
	}{
		{map[string]any{"mesh_ping_sec": 0}, "mesh_ping_sec"},
		{map[string]any{"panel_sec": 61}, "panel_sec"},
		{map[string]any{"mesh_loss_threshold": "x"}, "mesh_loss_threshold"},
		{map[string]any{"mesh_addr": "adres"}, "mesh_addr"},
		// eşik 1 x panel 2 sn < 3 sn
		{map[string]any{"mesh_loss_threshold": 1, "name": "Degismemeli"}, "mesh_loss_threshold"},
	}
	for _, c := range cases {
		var fe meshFieldErr
		if code := e.do("PUT", path, c.body, &fe); code != 400 || fe.Field != c.field {
			t.Fatalf("%v: 400 + alan %q bekleniyordu: %d %+v", c.body, c.field, code, fe)
		}
	}
	var list []meshProbeResp
	e.mustDo("GET", "/api/probes", nil, &list, 200)
	if len(list) != 1 || list[0].MeshLossThreshold != nil || list[0].Timing == nil || list[0].Timing.LossThreshold != 3 {
		t.Fatalf("liste etkin zamanlamayı göstermeli, geçersiz istek yazmamalı: %+v", list)
	}

	// "-" (dinlemiyor) geçerli; boş görülen IP'ye döner.
	e.mustDo("PUT", path, map[string]any{"mesh_addr": "-"}, &p, 200)
	if *p.MeshAddr != "-" {
		t.Fatalf("- kabul edilmeli: %q", *p.MeshAddr)
	}

	// İşlem kaydı mesh değişikliğini içerir.
	var audit []store.AuditEntry
	e.mustDo("GET", "/api/audit?action=probe.update", nil, &audit, 200)
	found := false
	for _, it := range audit {
		if strings.Contains(it.Detail, "mesh ping aralığı") {
			found = true
		}
	}
	if !found {
		t.Fatalf("işlem kaydında mesh değişikliği bekleniyordu: %+v", audit)
	}
}

func TestServerProbeIgnoresMesh(t *testing.T) {
	e := setupAdmin(t)
	var cp serverSetup
	e.mustDo("POST", "/api/servers", map[string]any{"name": "Sunucu"}, &cp, 201)
	var p meshProbeResp
	e.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"mesh_ping_sec": 99, "mesh_addr": "x"}, &p, 200)
	if p.Timing != nil || p.MeshPingSec != nil || p.MeshAddr != nil {
		t.Fatalf("sunucu ajanında mesh alanları olmamalı: %+v", p)
	}
}
