package api

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Etiket kuralları uçtan uca: kanal etiketli monitöre bağlanır (listede
// tag_notification_ids), kısıtlı kullanıcı etiketli monitörü görür, etikete
// bağlı sayfa grubu herkese açık sayfada dolar; kurallar yedekte gidip gelir.
func TestTagRulesAPI(t *testing.T) {
	pe := setupPages(t)
	var tg store.Tag
	pe.mustDo("POST", "/api/tags", map[string]string{"name": "müşteri", "color": "#2563eb"}, &tg, 201)

	acme := pe.push("Acme sitesi")
	other := pe.push("Başka site")
	pe.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", acme.ID), []map[string]any{{"tag_id": tg.ID, "value": "acme"}}, nil, 200)
	pe.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", other.ID), []map[string]any{{"tag_id": tg.ID, "value": "globex"}}, nil, 200)

	// Kanal: müşteri=acme kuralı; monitör formunda açık seçim yok.
	var ch store.Notification
	pe.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Acme webhook", "type": "webhook", "config": map[string]any{"url": "https://hook.example.com/x"},
		"is_default": false, "tag_rules": []map[string]any{{"tag_id": tg.ID, "value": "acme"}, {"tag_id": tg.ID, "value": " acme "}},
	}, &ch, 201)
	if len(ch.TagRules) != 1 || ch.TagRules[0].Name != "müşteri" || ch.TagRules[0].Value != "acme" {
		t.Fatalf("kural tekilleşip adıyla dönmeli: %+v", ch.TagRules)
	}
	pe.mustDo("POST", "/api/notifications", map[string]any{
		"name": "x", "type": "webhook", "config": map[string]any{"url": "https://hook.example.com/x"}, "tag_rules": []map[string]any{{"tag_id": 99999}},
	}, nil, 400)
	var det struct {
		Monitor monitorView `json:"monitor"`
	}
	pe.mustDo("GET", fmt.Sprintf("/api/monitors/%d", acme.ID), nil, &det, 200)
	if mv := det.Monitor; len(mv.NotificationIDs) != 0 || len(mv.TagNotificationIDs) != 1 || mv.TagNotificationIDs[0] != ch.ID {
		t.Fatalf("etiketle bağlı kanal listede görünmeli: ids=%v tag=%v", mv.NotificationIDs, mv.TagNotificationIDs)
	}
	det.Monitor = monitorView{}
	pe.mustDo("GET", fmt.Sprintf("/api/monitors/%d", other.ID), nil, &det, 200)
	if len(det.Monitor.TagNotificationIDs) != 0 {
		t.Fatalf("değer uymayan monitöre kanal bağlanmamalı: %v", det.Monitor.TagNotificationIDs)
	}
	// GET → PUT gidiş-dönüş kuralı korur; tag_rules göndermeyen eski istemci de bozmaz.
	var list []store.Notification
	pe.mustDo("GET", "/api/notifications", nil, &list, 200)
	body := map[string]any{"name": list[0].Name, "type": list[0].Type, "config": list[0].Config, "tag_rules": list[0].TagRules}
	pe.mustDo("PUT", fmt.Sprintf("/api/notifications/%d", ch.ID), body, &ch, 200)
	if len(ch.TagRules) != 1 {
		t.Fatalf("gidiş-dönüş kuralı kaybetti: %+v", ch.TagRules)
	}
	delete(body, "tag_rules")
	pe.mustDo("PUT", fmt.Sprintf("/api/notifications/%d", ch.ID), body, &ch, 200)
	if len(ch.TagRules) != 1 {
		t.Fatalf("eski istemci kuralı silmemeli: %+v", ch.TagRules)
	}

	// Kısıtlı kullanıcı: yalnızca etiket kuralıyla (açık seçim yok).
	var u store.User
	pe.mustDo("POST", "/api/users", map[string]any{"username": "acme", "role": "viewer", "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{}, "tag_rules": []map[string]any{{"tag_id": tg.ID, "value": "acme"}}}, &u, 201)
	if len(u.TagRules) != 1 || len(u.MonitorIDs) != 1 || u.MonitorIDs[0] != acme.ID || len(u.PickedMonitorIDs) != 0 {
		t.Fatalf("kullanıcı kısıtı: %+v", u)
	}
	cust := pe.loginAs("acme", "gecici-sifre-1")
	cust.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	var seen []monitorView
	cust.mustDo("GET", "/api/monitors", nil, &seen, 200)
	if len(seen) != 1 || seen[0].ID != acme.ID {
		t.Fatalf("müşteri etiketli monitörü görmeli: %+v", seen)
	}
	// Etiketi sonradan alan monitör anında görünür.
	late := pe.push("Acme API")
	pe.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", late.ID), []map[string]any{{"tag_id": tg.ID, "value": "acme"}}, nil, 200)
	cust.mustDo("GET", "/api/monitors", nil, &seen, 200)
	if len(seen) != 2 {
		t.Fatalf("yeni etiketli monitör görünmeli: %d", len(seen))
	}
	if code := cust.do("GET", fmt.Sprintf("/api/monitors/%d", other.ID), nil, nil); code != 404 {
		t.Fatalf("başka müşteri 404 olmalı: %d", code)
	}

	// Durum sayfası: etikete bağlı grup + açık monitör.
	var p store.StatusPage
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "acme", "title": "Acme", "sections": []map[string]any{
		{"title": "Diğer", "monitors": []map[string]any{{"id": other.ID, "name": ""}}},
		{"title": "Acme", "tag_id": tg.ID, "tag_value": "acme", "monitors": []map[string]any{}},
	}}, &p, 201)
	if len(p.Sections) != 2 || p.Sections[1].TagID != tg.ID || len(p.Sections[1].Monitors) != 2 || !p.Sections[1].Monitors[0].Auto {
		t.Fatalf("etiket grubu dolmalı: %+v", p.Sections)
	}
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "bozuk", "title": "x", "sections": []map[string]any{{"title": "a", "tag_id": 424242}}}, nil, 400)
	var pub struct {
		Sections []struct {
			Title    string                   `json:"title"`
			Monitors []map[string]interface{} `json:"monitors"`
		} `json:"sections"`
	}
	pe.mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	if len(pub.Sections) != 2 || len(pub.Sections[1].Monitors) != 2 || pub.Sections[1].Monitors[0]["name"] != "Acme API" {
		t.Fatalf("herkese açık sayfa etiket grubunu ada göre listelemeli: %+v", pub.Sections)
	}
	// Kaydet (auto monitörler geri gönderilse de saklanmaz) ve etiket kalkınca grup boşalır.
	p.Sections[1].Monitors = append(p.Sections[1].Monitors, store.PageMonitor{ID: other.ID, Auto: true})
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": p.Slug, "title": p.Title, "sections": p.Sections}, &p, 200)
	if len(p.Sections[1].Monitors) != 2 {
		t.Fatalf("auto monitörler kaydetmede atlanmalı: %+v", p.Sections[1].Monitors)
	}

	// Yedek: kural ve grup etiketi adıyla gider; değiştir modunda geri gelir.
	var doc json.RawMessage
	pe.mustDo("GET", "/api/export", nil, &doc, 200)
	var exported struct {
		Notifications []struct {
			TagRules []map[string]string `json:"tag_rules"`
		} `json:"notifications"`
		StatusPages []struct {
			Sections []struct {
				Tag      *map[string]string `json:"tag"`
				Monitors []any              `json:"monitors"`
			} `json:"sections"`
		} `json:"status_pages"`
	}
	json.Unmarshal(doc, &exported)
	if len(exported.Notifications) != 1 || len(exported.Notifications[0].TagRules) != 1 || exported.Notifications[0].TagRules[0]["name"] != "müşteri" {
		t.Fatalf("yedekte kanal kuralı: %s", doc)
	}
	sec := exported.StatusPages[0].Sections[1]
	if sec.Tag == nil || (*sec.Tag)["name"] != "müşteri" || len(sec.Monitors) != 0 {
		t.Fatalf("yedekte grup etiketi adıyla, auto monitörsüz olmalı: %+v", sec)
	}
	var sum importSummary
	pe.mustDo("POST", "/api/import?mode=replace&confirm=yes", json.RawMessage(doc), &sum, 200)
	pe.mustDo("GET", "/api/notifications", nil, &list, 200)
	if len(list) != 1 || len(list[0].TagRules) != 1 || list[0].TagRules[0].Name != "müşteri" {
		t.Fatalf("geri yükleme kanal kuralını getirmeli: %+v", list)
	}
	var pages []store.StatusPage
	pe.mustDo("GET", "/api/status-pages", nil, &pages, 200)
	if len(pages) != 1 || pages[0].Sections[1].TagID == 0 || len(pages[0].Sections[1].Monitors) != 2 {
		t.Fatalf("geri yükleme grup etiketini getirmeli: %+v", pages[0].Sections)
	}
}
