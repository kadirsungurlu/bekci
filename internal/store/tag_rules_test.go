package store

import (
	"context"
	"encoding/json"
	"testing"
)

// Etiket kuralları: kanal etiketli monitöre kendiliğinden bağlanır, kısıtlı
// kullanıcı etiketli monitörü görür, etikete bağlı sayfa grubu dolar; açık
// seçimler korunur ve etiket silinince kural düşer.
func TestTagRules(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.CreateFirstUser(ctx, "admin", "x"); err != nil { // UpdateUser en az bir yönetici ister
		t.Fatal(err)
	}
	env := Tag{Name: "ortam", Color: "#2563eb"}
	cust := Tag{Name: "müşteri", Color: "#16a34a"}
	for _, tg := range []*Tag{&env, &cust} {
		if err := s.CreateTag(ctx, tg); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(name string, tags ...MonitorTagInput) Monitor {
		m := Monitor{Name: name, Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30, Config: json.RawMessage(`{"url":"https://` + name + `.example.com"}`)}
		if err := s.CreateMonitor(ctx, &m, nil); err != nil {
			t.Fatal(err)
		}
		if err := s.SetMonitorTags(ctx, m.ID, tags); err != nil {
			t.Fatal(err)
		}
		return m
	}
	prod := mk("prod", MonitorTagInput{TagID: env.ID, Value: "canlı"}, MonitorTagInput{TagID: cust.ID, Value: "acme"})
	stage := mk("stage", MonitorTagInput{TagID: env.ID, Value: "test"})
	plain := mk("plain")

	// Kanal: ortam=canlı kuralı + plain'e açık bağlantı.
	ch := Notification{Name: "Ops", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"https://hook.example.com"}`),
		TagRules: []TagRule{{TagID: env.ID, Value: "canlı"}}}
	if err := s.CreateNotification(ctx, &ch, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateMonitor(ctx, &plain, []int64{ch.ID}, false); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetNotification(ctx, ch.ID)
	if err != nil || len(got.TagRules) != 1 || got.TagRules[0].Name != "ortam" || got.TagRules[0].Value != "canlı" {
		t.Fatalf("kural okunamadı: %+v %v", got.TagRules, err)
	}
	for _, c := range []struct {
		m    Monitor
		want int
	}{{prod, 1}, {stage, 0}, {plain, 1}} {
		list, err := s.NotificationsForMonitor(ctx, c.m.ID)
		if err != nil || len(list) != c.want {
			t.Fatalf("%s: %d kanal, %d bekleniyordu (%v)", c.m.Name, len(list), c.want, err)
		}
	}
	// Değersiz kural etiketin her değerine uyar; stage de kazanır.
	ch.TagRules = []TagRule{{TagID: env.ID}}
	if err := s.UpdateNotification(ctx, &ch, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.NotificationsForMonitor(ctx, stage.ID); len(list) != 1 {
		t.Fatalf("değersiz kural stage'e uymalıydı: %d", len(list))
	}
	byTag, err := s.TagNotificationIDs(ctx)
	if err != nil || len(byTag[prod.ID]) != 1 || len(byTag[plain.ID]) != 0 {
		t.Fatalf("TagNotificationIDs: %v %v", byTag, err)
	}

	// Kısıtlı kullanıcı: açık seçim plain + müşteri=acme kuralı → plain ve prod.
	u := User{Username: "acme", Role: RoleViewer, AllMonitors: false, MonitorIDs: []int64{plain.ID},
		TagRules: []TagRule{{TagID: cust.ID, Value: "acme"}}}
	if err := s.CreateUser(ctx, &u, "x"); err != nil {
		t.Fatal(err)
	}
	ru, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ru.PickedMonitorIDs) != 1 || ru.PickedMonitorIDs[0] != plain.ID || len(ru.TagRules) != 1 || len(ru.MonitorIDs) != 2 {
		t.Fatalf("kullanıcı kısıtı: picked=%v rules=%v eff=%v", ru.PickedMonitorIDs, ru.TagRules, ru.MonitorIDs)
	}
	// Etiketi alan yeni monitör anında görünür.
	later := mk("later", MonitorTagInput{TagID: cust.ID, Value: "acme"})
	ru, _ = s.UserByID(ctx, u.ID)
	if len(ru.MonitorIDs) != 3 {
		t.Fatalf("yeni etiketli monitör görünmeli: %v", ru.MonitorIDs)
	}
	// Güncellemede açık seçim yazılır, etiketle gelen yazılmaz.
	ru.MonitorIDs = ru.PickedMonitorIDs
	if err := s.UpdateUser(ctx, &ru); err != nil {
		t.Fatal(err)
	}
	ru, _ = s.UserByID(ctx, u.ID)
	if len(ru.PickedMonitorIDs) != 1 || len(ru.MonitorIDs) != 3 {
		t.Fatalf("güncelleme açık seçimi bozdu: picked=%v eff=%v", ru.PickedMonitorIDs, ru.MonitorIDs)
	}

	// Sayfa: etikete bağlı grup + açık monitör; auto kayıt saklanmaz.
	p := StatusPage{Slug: "acme", Title: "Acme", Sections: []PageSection{
		{Title: "Web", Monitors: []PageMonitor{{ID: plain.ID, Name: "Site"}}},
		{Title: "Acme", TagID: cust.ID, TagValue: "acme", Monitors: []PageMonitor{{ID: later.ID, Name: "Önce"}}},
	}}
	if err := s.CreatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	rp, err := s.GetPage(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	sec := rp.Sections[1]
	if sec.TagID != cust.ID || len(sec.Monitors) != 2 || sec.Monitors[0].ID != later.ID || sec.Monitors[0].Auto || !sec.Monitors[1].Auto || sec.Monitors[1].ID != prod.ID {
		t.Fatalf("etiket grubu: %+v", sec)
	}
	// Başka grupta açıkça listelenen plain etiket grubuna eklenmez (sahip olsa da).
	s.SetMonitorTags(ctx, plain.ID, []MonitorTagInput{{TagID: cust.ID, Value: "acme"}})
	rp, _ = s.GetPage(ctx, p.ID)
	if len(rp.Sections[1].Monitors) != 2 {
		t.Fatalf("açıkça listelenen monitör etiketle ikinci kez eklenmemeli: %+v", rp.Sections[1].Monitors)
	}
	// Kaydet: auto monitörler saklanmaz (ham JSON'da yok).
	if err := s.UpdatePage(ctx, &rp); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := s.db.QueryRowContext(ctx, "SELECT sections FROM status_pages WHERE id = ?", p.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var stored []PageSection
	json.Unmarshal([]byte(raw), &stored)
	if len(stored[1].Monitors) != 1 || stored[1].TagID != cust.ID {
		t.Fatalf("saklanan gruplar: %s", raw)
	}
	// Etiket silinince kurallar düşer, açık seçimler kalır.
	if err := s.DeleteTag(ctx, cust.ID); err != nil {
		t.Fatal(err)
	}
	ru, _ = s.UserByID(ctx, u.ID)
	if len(ru.TagRules) != 0 || len(ru.MonitorIDs) != 1 {
		t.Fatalf("etiket silinince kural düşmeli: %v %v", ru.TagRules, ru.MonitorIDs)
	}
	rp, _ = s.GetPage(ctx, p.ID)
	if len(rp.Sections[1].Monitors) != 1 {
		t.Fatalf("etiket silinince grupta yalnızca açık monitör kalmalı: %+v", rp.Sections[1].Monitors)
	}
	if got, _ := s.GetNotification(ctx, ch.ID); len(got.TagRules) != 1 {
		t.Fatalf("ortam kuralı kalmalıydı: %+v", got.TagRules)
	}
}
