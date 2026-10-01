package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Durum sayfası: olay penceresi ve çoklu uptime pencereleri gidiş-dönüş yapar,
// herkese açık sayfa pencere başına yüzde ve olay penceresini taşır; eski
// istemcinin gövdesi (alanlar yok) varsayılanları korur. Duyuru kopyaları
// diğer sayfalara eklenir.
func TestPageWindowsAndAnnouncementCopies(t *testing.T) {
	e := setupAdmin(t)
	var m store.Monitor
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "site", "type": "http", "interval": 60,
		"config": map[string]any{"url": "https://example.com"}}, &m, 201)

	body := map[string]any{"slug": "acme", "title": "Acme", "sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": m.ID}}}},
		"custom_domain": "", "show_targets": false, "published": true, "bar_range": "recent",
		"incident_days": 30, "uptime_windows": []string{"90d", "24h", "30d", "24h"}}
	var p store.StatusPage
	e.mustDo("POST", "/api/status-pages", body, &p, 201)
	if p.IncidentDays != 30 || len(p.UptimeWindows) != 3 || p.UptimeWindows[0] != "24h" || p.UptimeWindows[2] != "90d" {
		t.Fatalf("pencereler kaydedilmedi/sıralanmadı: %+v", p)
	}
	// Eski istemci: alanlar yok → değişmez.
	delete(body, "incident_days")
	delete(body, "uptime_windows")
	e.mustDo("PUT", "/api/status-pages/"+itoa(p.ID), body, &p, 200)
	if p.IncidentDays != 30 || len(p.UptimeWindows) != 3 {
		t.Fatalf("eski istemci pencereleri bozdu: %+v", p)
	}
	for _, bad := range []map[string]any{{"incident_days": 15}, {"uptime_windows": []string{"1y"}}} {
		req := map[string]any{}
		for k, v := range body {
			req[k] = v
		}
		for k, v := range bad {
			req[k] = v
		}
		if code := e.do("PUT", "/api/status-pages/"+itoa(p.ID), req, nil); code != 400 {
			t.Errorf("%v: %d, 400 bekleniyordu", bad, code)
		}
	}
	// Herkese açık sayfa.
	var pub struct {
		IncidentDays  int      `json:"incident_days"`
		UptimeWindows []string `json:"uptime_windows"`
		Sections      []struct {
			Monitors []struct {
				Uptimes map[string]*float64 `json:"uptimes"`
			} `json:"monitors"`
		} `json:"sections"`
	}
	e.mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	if pub.IncidentDays != 30 || len(pub.UptimeWindows) != 3 || len(pub.Sections) != 1 || len(pub.Sections[0].Monitors) != 1 {
		t.Fatalf("herkese açık sayfa: %+v", pub)
	}
	ups := pub.Sections[0].Monitors[0].Uptimes
	for _, w := range []string{"24h", "30d", "90d"} {
		if _, ok := ups[w]; !ok {
			t.Errorf("%s penceresi yok: %v", w, ups)
		}
	}
	// Düzenleyici önizlemesi de pencereleri alır.
	var prev struct {
		Monitors map[string]struct {
			Uptimes map[string]*float64 `json:"uptimes"`
		} `json:"monitors"`
	}
	e.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"monitor_ids": []int64{m.ID}, "bar_range": "recent",
		"uptime_windows": []string{"7d"}, "incident_days": 7}, &prev, 200)
	if mon, ok := prev.Monitors[itoa(m.ID)]; !ok || len(mon.Uptimes) != 1 {
		t.Fatalf("önizleme pencereleri: %+v", prev)
	}

	// Duyuru kopyaları: ikinci bir sayfa açılır, duyuru her ikisine eklenir.
	var p2 store.StatusPage
	e.mustDo("POST", "/api/status-pages", map[string]any{"slug": "beta", "title": "Beta", "sections": []any{}, "published": true}, &p2, 201)
	var created struct {
		store.Announcement
		Copies int `json:"copies"`
	}
	e.mustDo("POST", "/api/status-pages/"+itoa(p.ID)+"/announcements", map[string]any{"title": "Bakım", "body": "", "severity": "warning",
		"starts_at": time.Now().Unix() - 10, "ends_at": 0, "all_pages": true}, &created, 201)
	if created.Copies != 1 || created.PageID != p.ID {
		t.Fatalf("kopya sayısı: %+v", created)
	}
	var anns []store.Announcement
	e.mustDo("GET", "/api/status-pages/"+itoa(p2.ID)+"/announcements", nil, &anns, 200)
	if len(anns) != 1 || anns[0].Title != "Bakım" || anns[0].PageID != p2.ID {
		t.Fatalf("kopya ikinci sayfada yok: %+v", anns)
	}
	if code := e.do("POST", "/api/status-pages/"+itoa(p.ID)+"/announcements", map[string]any{"title": "x", "severity": "info", "page_ids": []int64{9999}}, nil); code != 400 {
		t.Errorf("olmayan sayfa: %d", code)
	}
	// Yedek pencereleri taşır.
	var raw json.RawMessage
	e.mustDo("GET", "/api/export", nil, &raw, 200)
	var doc struct {
		StatusPages []struct {
			Slug          string   `json:"slug"`
			IncidentDays  int      `json:"incident_days"`
			UptimeWindows []string `json:"uptime_windows"`
		} `json:"status_pages"`
	}
	json.Unmarshal(raw, &doc)
	found := false
	for _, bp := range doc.StatusPages {
		if bp.Slug == "acme" {
			found = bp.IncidentDays == 30 && len(bp.UptimeWindows) == 3
		}
	}
	if !found {
		t.Fatalf("yedek pencereleri taşımıyor: %s", raw)
	}
}
