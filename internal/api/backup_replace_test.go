package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Değiştir modunda geri yükleme monitör/bildirim/etiketleri silip yeniden
// oluşturur; silinmeyen kayıtların bunlara bağları (kısıtlı kullanıcının
// monitör ve etiket seçimi, konum ayarı, sunucu bildirim bağı, bakım
// penceresinin monitörleri, sistem e-posta kanalı) ada göre korunur. Yedek
// dosyası konumları kontrol noktası adıyla, sistem e-posta kanalını adıyla taşır.
func TestReplaceRestoreKeepsLinks(t *testing.T) {
	f := newFeatureEnv(t)
	a := f.env
	var mail, hook, tag map[string]any
	a.mustDo("POST", "/api/notifications", map[string]any{"name": "Posta", "type": "email",
		"config": map[string]any{"host": "127.0.0.1", "port": 1025, "security": "none", "from": "a@b.c", "to": "x@y.z"}}, &mail, 201)
	a.mustDo("POST", "/api/notifications", map[string]any{"name": "Kanca", "type": "webhook",
		"config": map[string]any{"url": "http://127.0.0.1:1/x"}}, &hook, 201)
	a.mustDo("POST", "/api/tags", map[string]any{"name": "Müşteri", "color": "#2563eb"}, &tag, 201)
	mailID, hookID, tagID := int64(mail["id"].(float64)), int64(hook["id"].(float64)), int64(tag["id"].(float64))

	site := a.httpMonitor("Site", "http://127.0.0.1:1/a")
	api := a.httpMonitor("API", "http://127.0.0.1:1/b")
	cp := a.newProbe("Frankfurt")
	a.setLocations(site.ID, map[string]any{"include_local": true, "probe_ids": []int64{cp.Probe.ID}, "down_when": "all", "notify_partial": true}, 200)

	var srv createdProbe
	a.mustDo("POST", "/api/servers", map[string]any{"name": "Sunucu"}, &srv, 201)
	a.mustDo("PUT", fmt.Sprintf("/api/servers/%d/notifications", srv.Probe.ID), map[string]any{
		"notification_bindings": []map[string]any{{"notification_id": mailID, "level": "critical"}, {"notification_id": hookID, "level": ""}},
	}, nil, 200)

	var maint maintenanceView
	a.mustDo("POST", "/api/maintenance", map[string]any{"title": "Bakım", "strategy": "manual", "timezone": "Europe/Istanbul",
		"monitor_ids": []int64{api.ID}}, &maint, 201)

	a.mustDo("POST", "/api/users", map[string]any{"username": "musteri", "password": "Gizli-Parola-123!", "role": "viewer",
		"all_monitors": false, "monitor_ids": []int64{api.ID}, "tag_rules": []map[string]any{{"tag_id": tagID, "value": "Ayder"}}}, nil, 201)

	var st store.AppSettings
	a.mustDo("GET", "/api/settings", nil, &st, 200)
	st.SystemMailChannelID = mailID
	a.mustDo("PUT", "/api/settings", st, nil, 200)
	// Durum sayfasına bağlı elle açılan olay.
	var page, inc map[string]any
	a.mustDo("POST", "/api/status-pages", map[string]any{"slug": "durum", "title": "Durum"}, &page, 201)
	a.mustDo("POST", "/api/incidents", map[string]any{"title": "Sağlayıcı kesintisi", "severity": "major", "page_id": int64(page["id"].(float64)), "body": "İnceliyoruz"}, &inc, 201)
	incID := int64(inc["id"].(float64))

	doc, raw := a.exportDoc()
	if doc.SystemMailChannel != "Posta" {
		t.Errorf("sistem e-posta kanalı adı yedekte yok: %+v", doc.SystemMailChannel)
	}
	var siteDoc, apiDoc int
	for i, m := range doc.Monitors {
		if m.Name == "Site" {
			siteDoc = i
		}
		if m.Name == "API" {
			apiDoc = i
		}
	}
	if l := doc.Monitors[siteDoc].Locations; l == nil || len(l.Probes) != 1 || l.Probes[0] != "Frankfurt" || l.DownWhen != "all" || !l.NotifyPartial || !l.IncludeLocal {
		t.Fatalf("konum ayarı yedekte: %+v", l)
	}
	if doc.Monitors[apiDoc].Locations != nil {
		t.Errorf("yalnızca ana sunucu olan monitörde konum alanı olmamalı")
	}

	// Değiştir: her şey silinip yeniden oluşturulur.
	a.mustDo("POST", "/api/monitors", map[string]any{"name": "Fazla", "type": "push", "interval": 86400, "config": map[string]any{}}, nil, 201)
	sum := a.importDoc("/api/import?mode=replace&confirm=yes", json.RawMessage(raw), 200)
	if sum.Created.Monitors != 2 || sum.Deleted == nil || sum.Deleted.Monitors != 3 {
		t.Fatalf("özet: %+v", sum)
	}
	after := a.monitors()
	newSite, newAPI := after["Site"].ID, after["API"].ID
	if newSite == 0 || newAPI == 0 {
		t.Fatal("monitörler yeniden oluşturulmadı")
	}
	var notifs []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	a.mustDo("GET", "/api/notifications", nil, &notifs, 200)
	newMail, newHook := int64(0), int64(0)
	for _, n := range notifs {
		switch n.Name {
		case "Posta":
			newMail = n.ID
		case "Kanca":
			newHook = n.ID
		}
	}
	if newMail == 0 || newHook == 0 {
		t.Fatalf("kanallar: %+v", notifs)
	}

	// Konum ayarı (dosyadan, kontrol noktası adıyla).
	var lv struct {
		IncludeLocal  bool    `json:"include_local"`
		ProbeIDs      []int64 `json:"probe_ids"`
		DownWhen      string  `json:"down_when"`
		NotifyPartial bool    `json:"notify_partial"`
	}
	a.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", newSite), nil, &lv, 200)
	if len(lv.ProbeIDs) != 1 || lv.ProbeIDs[0] != cp.Probe.ID || lv.DownWhen != "all" || !lv.NotifyPartial || !lv.IncludeLocal {
		t.Errorf("konum ayarı geri gelmedi: %+v", lv)
	}
	// Kısıtlı kullanıcı: monitör ve etiket seçimi.
	var users []struct {
		Username   string          `json:"username"`
		MonitorIDs []int64         `json:"monitor_ids"`
		TagRules   []store.TagRule `json:"tag_rules"`
	}
	a.mustDo("GET", "/api/users", nil, &users, 200)
	found := false
	for _, u := range users {
		if u.Username != "musteri" {
			continue
		}
		found = true
		if len(u.MonitorIDs) != 1 || u.MonitorIDs[0] != newAPI || len(u.TagRules) != 1 || u.TagRules[0].Name != "Müşteri" || u.TagRules[0].Value != "Ayder" {
			t.Errorf("kısıtlı kullanıcı bağları: %+v", u)
		}
	}
	if !found {
		t.Fatal("kullanıcı yok")
	}
	// Sunucu bildirim bağı (seviyeyle).
	var sd struct {
		Bindings []store.ProbeNotificationBinding `json:"notification_bindings"`
	}
	a.mustDo("GET", fmt.Sprintf("/api/servers/%d", srv.Probe.ID), nil, &sd, 200)
	if len(sd.Bindings) != 2 {
		t.Fatalf("sunucu bağları: %+v", sd.Bindings)
	}
	for _, b := range sd.Bindings {
		if (b.NotificationID == newMail && b.Level != "critical") || (b.NotificationID == newHook && b.Level != "") ||
			(b.NotificationID != newMail && b.NotificationID != newHook) {
			t.Errorf("sunucu bağı: %+v", b)
		}
	}
	// Bakım penceresi monitörleri.
	var mv maintenanceView
	a.mustDo("GET", fmt.Sprintf("/api/maintenance/%d", maint.ID), nil, &mv, 200)
	if len(mv.MonitorIDs) != 1 || mv.MonitorIDs[0] != newAPI {
		t.Errorf("bakım monitörleri: %+v", mv.MonitorIDs)
	}
	// Sistem e-posta kanalı adla yeniden çözüldü.
	a.mustDo("GET", "/api/settings", nil, &st, 200)
	if st.SystemMailChannelID != newMail {
		t.Errorf("sistem e-posta kanalı: %d, %d bekleniyordu", st.SystemMailChannelID, newMail)
	}
	// Elle açılan olay yeniden oluşturulan sayfaya (aynı adres) bağlı kaldı.
	var pages []struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	a.mustDo("GET", "/api/status-pages", nil, &pages, 200)
	var newPage int64
	for _, p := range pages {
		if p.Slug == "durum" {
			newPage = p.ID
		}
	}
	var incAfter struct {
		Incident struct {
			PageID int64 `json:"page_id"`
		} `json:"incident"`
	}
	a.mustDo("GET", fmt.Sprintf("/api/incidents/%d", incID), nil, &incAfter, 200)
	if newPage == 0 || incAfter.Incident.PageID != newPage {
		t.Errorf("elle açılan olayın sayfası: %d, %d bekleniyordu", incAfter.Incident.PageID, newPage)
	}
}

// Başka kuruluma taşıma: kontrol noktası adı eşleşirse konum bağlanır,
// eşleşmezse not düşülür ve monitör ana sunucudan kontrol edilir; sistem
// e-posta kanalı adla bulunur.
func TestImportLocationsByProbeName(t *testing.T) {
	f := newFeatureEnv(t)
	a := f.env
	cp := a.newProbe("Frankfurt")
	doc := map[string]any{
		"format": "bekci", "version": 1, "system_mail_channel": "Posta",
		"settings":      map[string]any{"retention_raw_days": 30, "retention_hourly_days": 365, "cert_days": []int{7}, "domain_days": []int{7}, "backup_keep": 7, "notify_lang": "tr"},
		"notifications": []map[string]any{{"name": "Posta", "type": "email", "config": map[string]any{"host": "127.0.0.1", "port": 25, "security": "none", "from": "a@b.c", "to": "x@y.z"}}},
		"monitors": []map[string]any{
			{"id": 1, "name": "Site", "type": "http", "interval": 60, "config": map[string]any{"url": "http://127.0.0.1:1/a"},
				"locations": map[string]any{"include_local": false, "probes": []string{"frankfurt", "Tokyo"}, "down_when": "majority", "notify_partial": true}},
			{"id": 2, "name": "Yalnız", "type": "http", "interval": 60, "config": map[string]any{"url": "http://127.0.0.1:1/b"},
				"locations": map[string]any{"include_local": false, "probes": []string{"Tokyo"}, "down_when": "bozuk"}},
		},
	}
	sum := a.importDoc("/api/import?mode=replace&confirm=yes", doc, 200)
	res := itemResults(sum)
	if res["monitor:Site"] != "created" || res["monitor:Yalnız"] != "created" {
		t.Fatalf("sonuçlar: %v", res)
	}
	mons := a.monitors()
	var lv struct {
		IncludeLocal bool    `json:"include_local"`
		ProbeIDs     []int64 `json:"probe_ids"`
		DownWhen     string  `json:"down_when"`
	}
	a.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", mons["Site"].ID), nil, &lv, 200)
	if lv.IncludeLocal || len(lv.ProbeIDs) != 1 || lv.ProbeIDs[0] != cp.Probe.ID || lv.DownWhen != "majority" {
		t.Errorf("Site konumu: %+v", lv)
	}
	a.mustDo("GET", fmt.Sprintf("/api/monitors/%d/locations", mons["Yalnız"].ID), nil, &lv, 200)
	if !lv.IncludeLocal || len(lv.ProbeIDs) != 0 {
		t.Errorf("eşleşmeyen konum: %+v", lv)
	}
	msgs := ""
	for _, it := range sum.Items {
		if it.Name == "Yalnız" {
			msgs = fmt.Sprint(it.Messages)
		}
	}
	if !strings.Contains(msgs, "Tokyo") {
		t.Errorf("eşleşmeyen kontrol noktası notu yok: %q", msgs)
	}
	var st store.AppSettings
	a.mustDo("GET", "/api/settings", nil, &st, 200)
	var notifs []struct {
		ID int64 `json:"id"`
	}
	a.mustDo("GET", "/api/notifications", nil, &notifs, 200)
	if len(notifs) != 1 || st.SystemMailChannelID != notifs[0].ID {
		t.Errorf("sistem e-posta kanalı: %d, kanallar %+v", st.SystemMailChannelID, notifs)
	}
}
