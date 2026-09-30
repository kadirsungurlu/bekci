package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/backup"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func layoutIDs(l store.PageLayout) string {
	var ids []string
	for _, b := range l.Blocks {
		id := b.ID
		if !b.Visible {
			id = "-" + id
		}
		ids = append(ids, id)
	}
	return strings.Join(ids, ",")
}

// Dizilim: varsayılanlar, geçersiz değerlerin varsayılana çekilmesi,
// show_incidents ile tek kaynak, herkese açık yanıt ve sayfa listesi.
func TestStatusPageLayout(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://a.example.com")
	now := pe.s.now().Unix()
	pe.beat(a.ID, now, store.StatusUp)

	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "dizi", "title": "Dizi",
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID}}}}}, &p, 201)
	if p.Layout.Style != "list" || p.Layout.Width != "narrow" || layoutIDs(p.Layout) != "overall,announcements,groups,incidents" {
		t.Fatalf("yeni sayfa varsayılan dizilim almalı: %+v", p.Layout)
	}
	url := fmt.Sprintf("/api/status-pages/%d", p.ID)
	base := map[string]any{"slug": "dizi", "title": "Dizi", "sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID}}}}}
	with := func(extra map[string]any) map[string]any {
		out := map[string]any{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}

	// Geçerli dizilim; bilinmeyen bölüm ve tekrar atılır, eksik sona eklenir;
	// olaylar dizilimden kapatılınca show_incidents de kapanır.
	pe.mustDo("PUT", url, with(map[string]any{"layout": map[string]any{"style": "grid", "width": "wide", "fazla": 1, "blocks": []map[string]any{
		{"id": "groups"}, {"id": "overall", "visible": false}, {"id": "bilinmeyen"}, {"id": "groups", "visible": false}, {"id": "incidents", "visible": false},
	}}}), &p, 200)
	if p.Layout.Style != "grid" || p.Layout.Width != "wide" || layoutIDs(p.Layout) != "groups,-overall,-incidents,announcements" || p.ShowIncidents {
		t.Fatalf("dizilim: %+v show_incidents=%v", p.Layout, p.ShowIncidents)
	}
	// Dizilim gönderilmezse değişmez; show_incidents olaylar bölümünü açar.
	pe.mustDo("PUT", url, with(map[string]any{"show_incidents": true}), &p, 200)
	if p.Layout.Style != "grid" || layoutIDs(p.Layout) != "groups,-overall,incidents,announcements" || !p.ShowIncidents {
		t.Fatalf("dizilim korunmalı, olaylar açılmalı: %+v", p.Layout)
	}
	// İkisi birden: show_incidents geçerli.
	pe.mustDo("PUT", url, with(map[string]any{"show_incidents": false, "layout": map[string]any{"style": "compact", "blocks": []map[string]any{{"id": "incidents", "visible": true}}}}), &p, 200)
	if p.ShowIncidents || p.Layout.Visible(store.BlockIncidents) || p.Layout.Style != "compact" || p.Layout.Width != "narrow" ||
		layoutIDs(p.Layout) != "-incidents,overall,announcements,groups" {
		t.Fatalf("show_incidents öncelikli olmalı: %+v %v", p.Layout, p.ShowIncidents)
	}
	// Bilinmeyen değerler varsayılana çekilir (hata değil); nesne olmayan dizilim 400.
	pe.mustDo("PUT", url, with(map[string]any{"layout": map[string]any{"style": "masonry", "width": 5000}}), nil, 400) // tip hatası
	pe.mustDo("PUT", url, with(map[string]any{"layout": map[string]any{"style": "masonry", "width": "dev"}}), &p, 200)
	// Dizilim bütün olarak değişir: bölüm verilmezse varsayılan sıra.
	if p.Layout.Style != "list" || p.Layout.Width != "narrow" || layoutIDs(p.Layout) != "overall,announcements,groups,-incidents" {
		t.Fatalf("bilinmeyen değerler: %+v", p.Layout)
	}
	pe.mustDo("PUT", url, with(map[string]any{"layout": "grid"}), nil, 400)
	pe.mustDo("PUT", url, with(map[string]any{"layout": nil}), &p, 200)
	if layoutIDs(p.Layout) != "overall,announcements,groups,-incidents" {
		t.Fatalf("null dizilim değiştirmemeli: %+v", p.Layout)
	}

	// Herkese açık yanıt: dizilim var; gizli duyuru ve grup verisi gönderilmez,
	// genel durum yine hesaplanır.
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Duyuru"}, nil, 201)
	pe.mustDo("PUT", url, with(map[string]any{"show_incidents": true, "layout": map[string]any{"style": "grid", "width": "wide", "blocks": []map[string]any{
		{"id": "announcements", "visible": false}, {"id": "groups", "visible": false}}}}), &p, 200)
	var pub struct {
		Status        string           `json:"status"`
		Sections      []any            `json:"sections"`
		Announcements []any            `json:"announcements"`
		ShowIncidents bool             `json:"show_incidents"`
		Layout        store.PageLayout `json:"layout"`
	}
	pe.anon().mustDo("GET", "/api/public/pages/dizi", nil, &pub, 200)
	if pub.Status != "up" || len(pub.Sections) != 0 || len(pub.Announcements) != 0 || !pub.ShowIncidents ||
		pub.Layout.Style != "grid" || pub.Layout.Width != "wide" || layoutIDs(pub.Layout) != "-announcements,-groups,overall,incidents" {
		t.Fatalf("herkese açık dizilim: %+v", pub)
	}
	pe.mustDo("PUT", url, with(map[string]any{"layout": map[string]any{"style": "grid"}}), &p, 200)
	pe.anon().mustDo("GET", "/api/public/pages/dizi", nil, &pub, 200)
	if len(pub.Sections) != 1 || len(pub.Announcements) != 1 || layoutIDs(pub.Layout) != "overall,announcements,groups,incidents" {
		t.Fatalf("görünür bölümler: %+v", pub)
	}

	// Sayfa listesi (küçük resim) dizilimi taşır.
	var list []pageListItem
	pe.mustDo("GET", "/api/status-pages", nil, &list, 200)
	if len(list) != 1 || list[0].Layout.Style != "grid" || list[0].Summary.Monitors != 1 {
		t.Fatalf("liste: %+v", list)
	}

	// Hızlı ekleme (monitörü sayfaya ekle) dizilimi korur.
	b := pe.seedMonitor("B", "https://b.example.com")
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/monitors", p.ID), map[string]any{"monitor_id": b.ID, "section": 0}, nil, 200)
	pe.mustDo("GET", url, nil, &p, 200)
	if p.Layout.Style != "grid" || len(p.Sections[0].Monitors) != 2 {
		t.Fatalf("hızlı ekleme dizilimi bozmamalı: %+v", p)
	}
}

// "rows" yerleşimi ve show_uptime: varsayılan gösterilir, gizlenebilir,
// herkese açık yanıtta taşınır; dizilim gönderilmezse değişmez.
func TestStatusPageRowsLayout(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://a.example.com")
	pe.beat(a.ID, pe.s.now().Unix(), store.StatusUp)
	secs := []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID}}}}

	// Dizilimsiz yeni sayfa: show_uptime true.
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "yalin", "title": "Yalın", "sections": secs}, &p, 201)
	if p.Layout.ShowUptime == nil || !*p.Layout.ShowUptime {
		t.Fatalf("varsayılan show_uptime true olmalı: %+v", p.Layout)
	}

	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "satir", "title": "Satır", "sections": secs,
		"layout": map[string]any{"style": "rows", "width": "wide", "show_uptime": false}}, &p, 201)
	if p.Layout.Style != "rows" || p.Layout.Width != "wide" || p.Layout.UptimeShown() {
		t.Fatalf("rows/show_uptime=false: %+v", p.Layout)
	}
	url := fmt.Sprintf("/api/status-pages/%d", p.ID)

	// Herkese açık yanıt yerleşimi ve show_uptime'ı taşır (ham JSON'da alan var).
	var raw map[string]json.RawMessage
	pe.anon().mustDo("GET", "/api/public/pages/satir", nil, &raw, 200)
	var pubLayout map[string]any
	if err := json.Unmarshal(raw["layout"], &pubLayout); err != nil || pubLayout["style"] != "rows" || pubLayout["show_uptime"] != false {
		t.Fatalf("herkese açık dizilim: %s %v", raw["layout"], err)
	}

	// Dizilim gönderilmezse değişmez.
	pe.mustDo("PUT", url, map[string]any{"slug": "satir", "title": "Satır", "sections": secs, "show_targets": true}, &p, 200)
	if p.Layout.Style != "rows" || p.Layout.UptimeShown() {
		t.Fatalf("dizilim korunmalı: %+v", p.Layout)
	}
	// Dizilim bütün olarak değişir: show_uptime verilmezse gösterilir.
	pe.mustDo("PUT", url, map[string]any{"slug": "satir", "title": "Satır", "sections": secs,
		"layout": map[string]any{"style": "rows", "width": "narrow"}}, &p, 200)
	if p.Layout.Style != "rows" || p.Layout.Width != "narrow" || !p.Layout.UptimeShown() {
		t.Fatalf("show_uptime yok → true: %+v", p.Layout)
	}
	pe.anon().mustDo("GET", "/api/public/pages/satir", nil, &raw, 200)
	if err := json.Unmarshal(raw["layout"], &pubLayout); err != nil || pubLayout["show_uptime"] != true || pubLayout["width"] != "narrow" {
		t.Fatalf("herkese açık dizilim (gösterilir): %s", raw["layout"])
	}
	// Tip hatası 400.
	pe.mustDo("PUT", url, map[string]any{"slug": "satir", "title": "Satır", "sections": secs,
		"layout": map[string]any{"style": "rows", "show_uptime": "hayir"}}, nil, 400)
}

// Canlı önizleme verisi: kaydedilmemiş monitör listesi için kimliğe göre veri.
func TestPreviewData(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://kullanici:parola@a.example.com/yol?gizli=1")
	b := pe.seedMonitor("B", "https://b.example.com")
	now := pe.s.now().Unix()
	pe.beat(a.ID, now-60, store.StatusUp)
	pe.beat(a.ID, now, store.StatusDown)
	pe.s.store.OpenIncident(t.Context(), a.ID, now-30, "neden")
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "on", "title": "Ön"}, &p, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Duyuru"}, nil, 201)

	var out struct {
		Range         string                    `json:"range"`
		UptimeWindow  string                    `json:"uptime_window"`
		Monitors      map[string]map[string]any `json:"monitors"`
		Incidents     []map[string]any          `json:"incidents"`
		Announcements []map[string]any          `json:"announcements"`
	}
	pe.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"page_id": p.ID, "monitor_ids": []int64{a.ID, b.ID, a.ID, 99999},
		"bar_range": "bilinmeyen", "show_targets": true}, &out, 200)
	ma := out.Monitors[fmt.Sprint(a.ID)]
	if out.Range != "recent" || out.UptimeWindow != "24h" || len(out.Monitors) != 2 || ma["name"] != "A" ||
		len(ma["bars"].([]any)) != 2 || ma["target"] != "https://a.example.com/yol" {
		t.Fatalf("önizleme verisi: %+v", out)
	}
	if len(out.Incidents) != 1 || out.Incidents[0]["monitor_id"].(float64) != float64(a.ID) || keysOf(out.Incidents[0]) != "monitor_id,resolved_at,started_at" {
		t.Fatalf("olaylar: %+v", out.Incidents)
	}
	if len(out.Announcements) != 1 {
		t.Fatalf("duyurular: %+v", out.Announcements)
	}
	pe.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"monitor_ids": []int64{b.ID}, "bar_range": "90d"}, &out, 200)
	if out.Range != "90d" || len(out.Monitors[fmt.Sprint(b.ID)]["bars"].([]any)) != publicDays || out.Monitors[fmt.Sprint(b.ID)]["target"] != nil ||
		len(out.Announcements) != 0 {
		t.Fatalf("90 gün: %+v", out)
	}
	many := make([]int64, maxPageMonitors+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	pe.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"monitor_ids": many}, nil, 400)
	pe.mustDo("POST", "/api/status-pages/preview-data", map[string]any{"page_id": 99999}, nil, 404)
	if code := pe.anon().do("POST", "/api/status-pages/preview-data", map[string]any{}, nil); code != http.StatusUnauthorized {
		t.Fatalf("girişsiz önizleme verisi: %d", code)
	}
}

// Yedek: dizilim dışa ve içe aktarılır; dizilimsiz eski yedek varsayılanı alır.
func TestBackupPageLayout(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hook.Close()
	a := setupAdmin(t)
	seedBackupEnv(t, a, hook.URL)
	var site int64
	for name, m := range a.monitors() {
		if name == "Site" {
			site = m.ID
		}
	}
	a.mustDo("POST", "/api/status-pages", map[string]any{"slug": "yedek", "title": "Yedek", "show_incidents": false,
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": site}}}},
		"layout":   map[string]any{"style": "compact", "width": "wide", "show_uptime": false, "blocks": []map[string]any{{"id": "groups"}, {"id": "overall", "visible": false}}}}, nil, 201)

	doc, _ := a.exportDoc()
	if len(doc.StatusPages) != 1 || doc.StatusPages[0].Layout == nil {
		t.Fatalf("yedekte dizilim yok: %+v", doc.StatusPages)
	}
	want := backup.PageLayout{Style: "compact", Width: "wide", Blocks: []backup.PageBlock{
		{ID: "groups", Visible: true}, {ID: "overall", Visible: false}, {ID: "announcements", Visible: true}, {ID: "incidents", Visible: false}},
		ShowUptime: new(bool)}
	if !reflect.DeepEqual(*doc.StatusPages[0].Layout, want) {
		t.Fatalf("yedekteki dizilim: %+v", *doc.StatusPages[0].Layout)
	}

	b := setupAdmin(t)
	b.importDoc("/api/import", doc, 200)
	doc2, _ := b.exportDoc()
	if len(doc2.StatusPages) != 1 || !reflect.DeepEqual(doc2.StatusPages[0].Layout, doc.StatusPages[0].Layout) ||
		*doc2.StatusPages[0].ShowIncidents {
		t.Fatalf("geri yüklenen dizilim: %+v", doc2.StatusPages)
	}

	// Eski yedek (dizilim yok) varsayılanı; show_incidents'sız elle yazılmış
	// yedekte olaylar dizilimden alınır.
	old := doc
	old.Monitors, old.Notifications, old.Tags, old.Settings = nil, nil, nil, nil
	p1 := doc.StatusPages[0]
	p1.Slug, p1.Layout, p1.Sections, p1.ShowIncidents = "eski", nil, nil, nil
	p2 := doc.StatusPages[0]
	p2.Slug, p2.ShowIncidents, p2.Sections = "elle", nil, nil
	p2.Layout = &backup.PageLayout{Style: "rows", Blocks: []backup.PageBlock{{ID: "incidents", Visible: false}}}
	old.StatusPages = []backup.Page{p1, p2}
	c := setupAdmin(t)
	raw, _ := json.Marshal(old)
	c.importDoc("/api/import", json.RawMessage(raw), 200)
	var list []pageResp
	c.mustDo("GET", "/api/status-pages", nil, &list, 200)
	got := map[string]pageResp{}
	for _, p := range list {
		got[p.Slug] = p
	}
	if e := got["eski"]; e.Layout.Style != "list" || layoutIDs(e.Layout) != "overall,announcements,groups,incidents" || !e.ShowIncidents || !e.Layout.UptimeShown() {
		t.Errorf("eski yedek: %+v", e)
	}
	// show_uptime'sız yedek: uptime gösterilir.
	if e := got["elle"]; e.Layout.Style != "rows" || e.ShowIncidents || layoutIDs(e.Layout) != "-incidents,overall,announcements,groups" || !e.Layout.UptimeShown() {
		t.Errorf("elle yazılmış yedek: %+v", e)
	}
}
