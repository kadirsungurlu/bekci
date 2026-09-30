package api

import (
	"context"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Liste ekranındaki sayfa özeti: durum, monitör sayısı, uptime ortalamaları,
// grup başına durumlar ve son olay (görünen adla, nedensiz).
func TestPageListSummary(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	a, b := pe.seedMonitor("A", "https://a.example.com"), pe.seedMonitor("B", "https://b.example.com")
	other := pe.seedMonitor("Sayfada değil", "https://c.example.com")
	now := pe.s.now().Unix()
	pe.beat(a.ID, now-120, store.StatusUp)
	pe.beat(a.ID, now-60, store.StatusUp)
	pe.beat(a.ID, now-30, store.StatusUp)
	pe.beat(a.ID, now, store.StatusDown)
	pe.beat(b.ID, now-10*86400, store.StatusDown) // yalnız 30 günlük ortalamada
	pe.beat(b.ID, now, store.StatusUp)
	st.OpenIncident(ctx, b.ID, now-5*86400, "gizli neden")
	st.ResolveIncident(ctx, b.ID, now-5*86400+600)
	st.OpenIncident(ctx, a.ID, now-60, "iç ayrıntı")
	st.OpenIncident(ctx, other.ID, now-10, "başka")
	// Süren olaydan daha yeni ama çözülmüş olay: süren kesintiyi gizlememeli (QA bulgusu).
	st.OpenIncident(ctx, b.ID, now-30, "kısa")
	st.ResolveIncident(ctx, b.ID, now-20)

	pe.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "genel", "title": "Genel",
		"sections": []map[string]any{
			{"title": "Web", "monitors": []map[string]any{{"id": a.ID, "name": "API"}, {"id": b.ID}}},
		},
	}, nil, 201)
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "bos", "title": "Boş"}, nil, 201)

	var list []pageListItem
	pe.mustDo("GET", "/api/status-pages", nil, &list, 200)
	if len(list) != 2 {
		t.Fatalf("2 sayfa bekleniyordu: %d", len(list))
	}
	var g, empty pageSummary
	for _, p := range list {
		if p.Slug == "genel" {
			g = p.Summary
		} else {
			empty = p.Summary
		}
	}
	if g.Status != "partial" || g.Monitors != 2 || g.Down != 1 || len(g.Sections) != 1 ||
		g.Sections[0].Title != "Web" || len(g.Sections[0].Statuses) != 2 || g.Sections[0].Statuses[0] != "down" {
		t.Fatalf("özet yanlış: %+v", g)
	}
	// 24 saat: A %75, B %100 → 87,5; 30 gün: A %75, B %50 → 62,5.
	if g.Uptime24h == nil || *g.Uptime24h != 87.5 || g.Uptime30d == nil || *g.Uptime30d != 62.5 {
		t.Errorf("uptime ortalamaları yanlış: %v %v", g.Uptime24h, g.Uptime30d)
	}
	if g.LastIncident == nil || g.LastIncident.Monitor != "API" || g.LastIncident.ResolvedAt != 0 || g.Ongoing != 1 {
		t.Errorf("son olay yanlış: %+v ongoing=%d", g.LastIncident, g.Ongoing)
	}
	if empty.Status != "unknown" || empty.Monitors != 0 || empty.Uptime24h != nil || empty.LastIncident != nil || empty.Sections == nil {
		t.Errorf("boş sayfa özeti yanlış: %+v", empty)
	}
}
