package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

func TestInventoryAPI(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	var sv serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "web-1"}, &sv, 201)
	path := "/api/servers/" + itoa(sv.Probe.ID) + "/inventory"

	// Eski ajan yeni panelde: envanter gelmedi, collected_at 0, listeler boş dizi.
	var empty map[string]any
	admin.mustDo("GET", path, nil, &empty, 200)
	if empty["collected_at"].(float64) != 0 {
		t.Fatalf("boş yanıt: %v", empty)
	}
	for _, k := range []string{"services", "containers", "ports", "certs", "disks"} {
		if l, ok := empty[k].([]any); !ok || len(l) != 0 {
			t.Fatalf("%s boş dizi olmalı: %v", k, empty[k])
		}
	}

	// Ajan yeni panele gönderir.
	until := time.Now().Add(5 * 24 * time.Hour).Unix()
	body := metrics.Inventory{
		Time:       time.Now().UnixMilli(),
		Containers: []metrics.InvContainer{{Name: "web", Image: "x:1", State: "running", Command: "x --password=gizli9"}},
		Ports:      []metrics.InvPort{{Proto: "tcp", Addr: "0.0.0.0", Port: 443, Process: "nginx"}},
		Certs:      []metrics.InvCert{{Source: "port", Location: "127.0.0.1:443", Subject: "a.example", NotAfter: until, Fingerprint: "ab12"}},
		Disks:      []metrics.InvDisk{{Mount: "/", Device: "/dev/sda1", FS: "ext4", Total: 100}},
	}
	if st, _, _ := admin.rawReq("POST", "/api/probe/inventory", bearer(sv.Token), body); st != http.StatusNoContent {
		t.Fatalf("gönderim: %d", st)
	}
	if st, _, _ := admin.rawReq("POST", "/api/probe/inventory", nil, body); st != http.StatusForbidden {
		t.Fatalf("belirteçsiz: %d", st)
	}
	big := map[string]string{"pad": strings.Repeat("x", metrics.MaxInventoryBytes+10)}
	if st, _, _ := admin.rawReq("POST", "/api/probe/inventory", bearer(sv.Token), big); st != http.StatusRequestEntityTooLarge {
		t.Fatalf("büyük gövde: %d", st)
	}
	var inv struct {
		CollectedAt int64 `json:"collected_at"`
		Containers  []struct {
			Command string `json:"command"`
		} `json:"containers"`
		Certs []struct {
			Fingerprint string `json:"fingerprint"`
			Ignored     bool   `json:"ignored"`
		} `json:"certs"`
	}
	admin.mustDo("GET", path, nil, &inv, 200)
	if inv.CollectedAt == 0 || len(inv.Certs) != 1 || len(inv.Containers) != 1 || strings.Contains(inv.Containers[0].Command, "gizli9") {
		t.Fatalf("envanter: %+v", inv)
	}

	// Sertifikayı yoksay, sonra geri al; bilinmeyen parmak izi 404.
	admin.mustDo("PUT", path+"/certs/ab12", map[string]any{"ignored": true}, nil, 204)
	admin.mustDo("GET", path, nil, &inv, 200)
	if !inv.Certs[0].Ignored {
		t.Fatal("yoksayılmadı")
	}
	admin.mustDo("PUT", path+"/certs/yok", map[string]any{"ignored": true}, nil, 404)

	// Sunucu ajanı için panel aralık bildirir, ajan alanı alır.
	var jobs map[string]any
	st, _, raw := admin.rawReq("GET", "/api/probe/jobs", bearer(sv.Token), nil)
	if st != 200 || json.Unmarshal(raw, &jobs) != nil || jobs["inventory_interval"].(float64) <= 0 {
		t.Fatalf("jobs: %d %s", st, raw)
	}
}
