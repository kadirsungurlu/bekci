package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Yavaş yanıt (degraded) olayı açık olan monitör herkese açık sayfada yalnızca
// "slow: true" bayrağıyla işaretlenir: durum "up" kalır, genel durum ve uptime
// değişmez, eşik/ortalama gibi iç ayrıntılar ve olay listesine kayıt gitmez.
func TestPublicPageSlow(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	slow := pe.seedMonitor("Yavaş API", "https://yavas.example.com")
	fast := pe.seedMonitor("Hızlı", "https://hizli.example.com")
	downSlow := pe.seedMonitor("Kapalı ve yavaş", "https://kapali.example.com")
	now := pe.s.now().Unix()
	for _, m := range []store.Monitor{slow, fast} {
		pe.beat(m.ID, now-60, store.StatusUp)
		pe.beat(m.ID, now, store.StatusUp)
	}
	pe.beat(downSlow.ID, now, store.StatusDown)
	if _, err := st.StartDegradedIncident(ctx, slow.ID, now-600, store.DegradedIncidentData{ThresholdMs: 500, Checks: 3, AvgMs: 1234, PeakMs: 1500, LastMs: 1234}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetMonitorSlow(ctx, slow.ID, true); err != nil {
		t.Fatal(err)
	}
	// Kesintideki monitörde bayrak kalmış olsa bile (motor normalde kapatır) "slow" gönderilmez.
	if err := st.SetMonitorSlow(ctx, downSlow.ID, true); err != nil {
		t.Fatal(err)
	}
	st.OpenIncident(ctx, downSlow.ID, now-120, "bağlantı reddedildi")

	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "yavas", "title": "Yavaş",
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": slow.ID, "name": "API"}, {"id": fast.ID}}}},
	}, &p, 201)
	anon := pe.anon()
	resp, raw := anon.raw("GET", "/api/public/pages/yavas", "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("sayfa açılmadı: %d %s", resp.StatusCode, raw)
	}
	// (Sayısal değerler zaman damgalarında rastlantıyla geçebilir; alan adlarına bakılır.)
	for _, secret := range []string{"threshold", "avg", "peak", "degraded", "incident_id", "Yavaş API"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("herkese açık yanıtta yavaşlık ayrıntısı olmamalı (%q): %s", secret, raw)
		}
	}
	var pub publicPayload
	json.Unmarshal(raw, &pub)
	if pub.Status != "up" {
		t.Errorf("yavaşlık genel durumu değiştirmemeli: %s", pub.Status)
	}
	mons := pub.Sections[0].Monitors
	if len(mons) != 2 {
		t.Fatalf("monitörler: %+v", mons)
	}
	if mons[0]["slow"] != true || mons[0]["status"] != "up" || keysOf(mons[0]) != "bars,name,slow,status,uptime,uptime_90d" {
		t.Errorf("yavaş monitör: %+v", mons[0])
	}
	if up, _ := mons[0]["uptime"].(float64); up != 100 {
		t.Errorf("yavaşlık uptime'ı düşürmemeli: %v", mons[0]["uptime"])
	}
	if _, has := mons[1]["slow"]; has || keysOf(mons[1]) != "bars,name,status,uptime,uptime_90d" {
		t.Errorf("hızlı monitörde slow alanı olmamalı: %+v", mons[1])
	}
	if len(pub.Incidents) != 0 {
		t.Errorf("yavaş yanıt olayı herkese açık olay listesine girmemeli: %+v", pub.Incidents)
	}

	// Kesintideki monitör: status down, slow yok. Sayfa değişince önbellek boşalır.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{
		"slug": "yavas", "title": "Yavaş",
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": slow.ID}, {"id": downSlow.ID}}}},
	}, nil, 200)
	var pub2 publicPayload
	anon.mustDo("GET", "/api/public/pages/yavas", nil, &pub2, 200)
	mons = pub2.Sections[0].Monitors
	if mons[0]["slow"] != true || mons[1]["status"] != "down" || mons[1]["slow"] != nil || pub2.Status != "partial" {
		t.Errorf("kesintideki monitörde slow olmamalı: %+v (%s)", mons, pub2.Status)
	}

	// Düzenleyicinin canlı önizleme verisi de bayrağı taşır (kimliğe göre).
	var prev struct {
		Monitors map[string]map[string]any `json:"monitors"`
	}
	pe.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"monitor_ids": []int64{slow.ID, fast.ID}}, &prev, 200)
	if prev.Monitors[fmt.Sprint(slow.ID)]["slow"] != true || prev.Monitors[fmt.Sprint(fast.ID)]["slow"] != nil {
		t.Errorf("önizleme verisi: %+v", prev.Monitors)
	}

	// Yavaşlık geçince bayrak kalkar (önbellek süresi dolunca).
	if err := st.SetMonitorSlow(ctx, slow.ID, false); err != nil {
		t.Fatal(err)
	}
	pe.advance(publicCacheTTL + time.Second)
	var after publicPayload // (json.Unmarshal var olan map'e eski anahtarları bırakır; yeni değişken)
	anon.mustDo("GET", "/api/public/pages/yavas", nil, &after, 200)
	if after.Sections[0].Monitors[0]["slow"] != nil {
		t.Errorf("yavaşlık geçince slow kalkmalı: %+v", after.Sections[0].Monitors[0])
	}
}
