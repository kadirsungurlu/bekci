package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

type diagCapture struct {
	Time     int64        `json:"time"`
	Location string       `json:"location"`
	Detail   check.Detail `json:"detail"`
}

// HTTP dışı monitörün bağlantı tanısı olay kaydında saklanır ve YALNIZCA
// yöneticiye gösterilir; editör ve izleyici yanıtında tanı izi bile olmaz.
func TestIncidentDiagAdminOnly(t *testing.T) {
	admin := setupAdmin(t)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()

	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Kapalı port", "type": "tcp", "interval": 20, "retry_interval": 20, "max_retries": 1, "timeout": 300,
		"config": map[string]any{"host": "127.0.0.1", "port": port},
	}, &mon, 201)
	var list []store.Incident
	waitFor(t, "olay", func() bool {
		admin.mustDo("GET", "/api/incidents", nil, &list, 200)
		return len(list) == 1
	})
	path := fmt.Sprintf("/api/incidents/%d", list[0].ID)

	var full struct {
		Capture  *diagCapture  `json:"capture"`
		Captures []diagCapture `json:"captures"`
		Details  bool          `json:"details"`
	}
	waitFor(t, "yakalama", func() bool {
		admin.mustDo("GET", path, nil, &full, 200)
		return full.Capture != nil
	})
	c := full.Capture
	g := c.Detail.Diag
	if !full.Details || c.Location != "Ana sunucu" || c.Detail.Kind != "tcp" || c.Detail.Method != "" || g == nil {
		t.Fatalf("yakalama: %+v", c)
	}
	if g.ErrorClass != check.ClassRefused || g.Phase != check.PhaseConnect || g.Port != port ||
		len(g.Attempts) != 1 || g.Attempts[0].Result != check.ClassRefused {
		t.Fatalf("tanı: %+v", g)
	}
	if len(full.Captures) != 1 || full.Captures[0].Location != c.Location || full.Captures[0].Detail.Diag == nil {
		t.Fatalf("konum listesi: %+v", full.Captures)
	}

	editor, _ := admin.newUser("editor-diag", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer-diag", store.RoleViewer, nil)
	for name, cl := range map[string]*env{"editör": editor, "izleyici": viewer} {
		var raw json.RawMessage
		cl.mustDo("GET", path, nil, &raw, 200)
		var v struct {
			Capture  *diagCapture  `json:"capture"`
			Captures []diagCapture `json:"captures"`
			Details  bool          `json:"details"`
		}
		json.Unmarshal(raw, &v)
		if v.Details || v.Capture != nil || v.Captures != nil {
			t.Errorf("%s yakalamayı görmemeli: %s", name, raw)
		}
		for _, s := range []string{`"diag"`, `"attempts"`, `"captures"`, `"raw_error"`, `"error_class"`} {
			if strings.Contains(string(raw), s) {
				t.Errorf("%s yanıtında %s var: %s", name, s, raw)
			}
		}
	}
}

// Çok konumlu olay: çalışmayan her konumun tanısı saklanır (öncelikli olan
// capture, hepsi captures). Kontrol noktasının tanısı sunucuda maskelenip
// sınırlanır; kontrol noktası "others" alanı gönderemez.
func TestIncidentDiagLocations(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	ctx := context.Background()
	a, b := admin.newProbe("İstanbul"), admin.newProbe("Frankfurt")
	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Veritabanı", "type": "postgres", "interval": 5000, "timeout": 300,
		"config": map[string]any{"host": "db.internal", "port": 5432, "username": "app", "password": "uzak-gizli-sifre"},
	}, &mon, 201)
	admin.setLocations(mon.ID, map[string]any{"include_local": false, "probe_ids": []int64{a.Probe.ID, b.Probe.ID}, "down_when": "all"}, 200)
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", mon.ID), nil, nil, 200)
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/resume", mon.ID), nil, nil, 200)

	send := func(p createdProbe, detail map[string]any) {
		t.Helper()
		now := time.Now().UnixMilli()
		res := map[string]any{"monitor_id": mon.ID, "time": now, "up": detail == nil, "ping_ms": -1, "message": "Zaman aşımı", "detail": detail}
		code, _, body := admin.rawReq("POST", "/api/probe/results", bearer(p.Token), map[string]any{"sent_at": now, "results": []map[string]any{res}})
		if code != 200 || !strings.Contains(string(body), `"accepted":1`) {
			t.Fatalf("sonuç: %d %s", code, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Frankfurt önce çalışıyor: olay ikisi de çalışmayınca açılır.
	send(b, nil)
	send(a, map[string]any{
		"kind": "postgres",
		"diag": map[string]any{
			"target": "app@db.internal:5432", "phase": "connect", "error_class": "refused",
			"raw_error": "failed to connect: password=uzak-gizli-sifre postgres://app:uzak-gizli-sifre@db.internal/x",
			"attempts":  []map[string]any{{"ip": "10.0.0.5", "result": "refused", "elapsed_ms": 3}},
			"ping":      map[string]any{"target": "10.0.0.5", "sent": 1, "received": 1, "loss_pct": 0, "avg_ms": 0.4},
		},
		"others": []map[string]any{{"location": "Sahte", "detail": map[string]any{"kind": "tcp"}}},
	})
	send(b, map[string]any{
		"kind": "postgres",
		"diag": map[string]any{"target": "app@db.internal:5432", "phase": "drop table", "error_class": "timeout", "raw_error": "Zaman aşımı",
			"attempts": []map[string]any{{"ip": "10.0.0.5", "result": "timeout", "elapsed_ms": 300}}},
	})
	waitFor(t, "olay", func() bool {
		id, _ := f.st.OpenIncidentID(ctx, mon.ID)
		return id > 0
	})
	id, _ := f.st.OpenIncidentID(ctx, mon.ID)

	var raw json.RawMessage
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", id), nil, &raw, 200)
	var r struct {
		Capture  *diagCapture  `json:"capture"`
		Captures []diagCapture `json:"captures"`
	}
	json.Unmarshal(raw, &r)
	if strings.Contains(string(raw), "uzak-gizli-sifre") || strings.Contains(string(raw), "Sahte") {
		t.Fatalf("maskelenmedi / sahte konum kabul edildi: %s", raw)
	}
	if r.Capture == nil || r.Capture.Location != "İstanbul" || r.Capture.Detail.Diag == nil || r.Capture.Detail.Diag.ErrorClass != "refused" {
		t.Fatalf("öncelikli yakalama: %s", raw)
	}
	var capture map[string]json.RawMessage
	json.Unmarshal(raw, &capture)
	if strings.Contains(string(capture["capture"]), `"others"`) {
		t.Errorf("öncelikli yakalamada others kalmamalı: %s", capture["capture"])
	}
	if len(r.Captures) != 2 || r.Captures[0].Location != "İstanbul" || r.Captures[1].Location != "Frankfurt" {
		t.Fatalf("konum listesi: %s", raw)
	}
	fr := r.Captures[1].Detail.Diag
	if fr == nil || fr.ErrorClass != "timeout" || fr.Phase != check.ClassOther || len(fr.Attempts) != 1 {
		t.Errorf("Frankfurt tanısı sınırlanmadı: %+v", fr)
	}
	if p := r.Captures[0].Detail.Diag.Ping; p == nil || p.Received != 1 || p.AvgMs != 0.4 {
		t.Errorf("ping: %+v", p)
	}

	// İngilizce: Türkçe saklanan ham mesaj her konum için çevrilir.
	if fr.RawError != "Zaman aşımı" {
		t.Fatalf("ham mesaj: %q", fr.RawError)
	}
	admin.mustDo("PUT", "/api/auth/preferences", map[string]any{"lang": "en"}, nil, 200)
	var en struct {
		Captures []diagCapture `json:"captures"`
	}
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", id), nil, &en, 200)
	if len(en.Captures) != 2 || en.Captures[1].Detail.Diag.RawError != "Timeout" {
		t.Fatalf("en: %+v", en)
	}
}
