package api

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Süren (tek seferlik) bakım "şimdi bitir" ile hemen biter; elle pencere için
// uç 400 döner (devre dışı bırakılarak biter). İzleyici bitiremez.
func TestMaintenanceEndNow(t *testing.T) {
	admin := setupAdmin(t)
	a := admin.push("A")
	var w maintenanceView
	admin.mustDo("POST", "/api/maintenance", onceWindow("Taşıma", a.ID), &w, 201)
	if w.Status != "active" {
		t.Fatalf("pencere sürüyor olmalı: %+v", w)
	}
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	if code := viewer.do("POST", fmt.Sprintf("/api/maintenance/%d/end", w.ID), nil, nil); code != 403 {
		t.Fatalf("izleyici bitiremez: %d", code)
	}
	admin.mustDo("POST", fmt.Sprintf("/api/maintenance/%d/end", w.ID), nil, &w, 200)
	if w.Status != "ended" || !w.Active || w.EndedAt == 0 {
		t.Fatalf("bitirilen pencere: %+v", w)
	}
	var detail struct{ Monitor monitorView }
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", a.ID), nil, &detail, 200)
	if detail.Monitor.InMaintenance {
		t.Fatal("monitör bakımdan çıkmalı")
	}
	// Düzenleme erken bitişi sıfırlar: pencere yeniden sürer.
	admin.mustDo("PUT", fmt.Sprintf("/api/maintenance/%d", w.ID), onceWindow("Taşıma 2", a.ID), &w, 200)
	if w.Status != "active" || w.EndedAt != 0 {
		t.Fatalf("düzenlenen pencere yeniden sürmeli: %+v", w)
	}
	var manual maintenanceView
	admin.mustDo("POST", "/api/maintenance", map[string]any{"title": "Elle", "strategy": "manual", "all_monitors": true}, &manual, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/maintenance/%d/end", manual.ID), nil, nil); code != 400 {
		t.Fatalf("elle pencere için 400 bekleniyordu: %d", code)
	}
}

// Push adresi yenilenince eski adres hemen 404 verir; yalnızca push tipinde çalışır.
func TestPushTokenRegenerate(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("Yedek")
	oldToken := m.PushToken
	if oldToken == "" {
		t.Fatal("push monitörünün token'ı olmalı")
	}
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	if code := viewer.do("POST", fmt.Sprintf("/api/monitors/%d/push-token", m.ID), nil, nil); code != 403 {
		t.Fatalf("izleyici yenileyemez: %d", code)
	}
	var fresh monitorView
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/push-token", m.ID), nil, &fresh, 200)
	if fresh.PushToken == "" || fresh.PushToken == oldToken {
		t.Fatalf("yeni token üretilmeli: %q → %q", oldToken, fresh.PushToken)
	}
	anon := admin.anon()
	if code := anon.do("GET", "/api/push/"+oldToken, nil, nil); code != 404 {
		t.Fatalf("eski adres 404 vermeli: %d", code)
	}
	if code := anon.do("GET", "/api/push/"+fresh.PushToken, nil, nil); code != 200 {
		t.Fatalf("yeni adres çalışmalı: %d", code)
	}
	var http monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Site", "type": "http", "interval": 5000, "config": map[string]any{"url": "https://example.com"}}, &http, 201)
	if code := admin.do("POST", fmt.Sprintf("/api/monitors/%d/push-token", http.ID), nil, nil); code != 400 {
		t.Fatalf("push dışı tipte 400 bekleniyordu: %d", code)
	}
}

// Ayrılmış adresler durum sayfası adresi olamaz.
func TestReservedStatusPageSlugs(t *testing.T) {
	admin := setupAdmin(t)
	for _, slug := range []string{"new", "api", "admin", "healthz", "metrics"} {
		if code := admin.do("POST", "/api/status-pages", map[string]any{"slug": slug, "title": "X", "sections": []any{}}, nil); code != 400 {
			t.Errorf("%q adresi reddedilmeli: %d", slug, code)
		}
	}
	admin.mustDo("POST", "/api/status-pages", map[string]any{"slug": "durum", "title": "X", "sections": []any{}}, nil, 201) // doğal ad serbest
}

// İşlem kaydı ayrıntısı: monitör ve ayar güncellemelerinde değişen alanlar;
// API anahtarıyla yapılan işlemde anahtarın adı.
func TestAuditDetails(t *testing.T) {
	f := newFeatureEnv(t)
	var m monitorView
	f.mustDo("POST", "/api/monitors", map[string]any{"name": "Site", "type": "http", "interval": 5000, "timeout": 30,
		"config": map[string]any{"url": "https://example.com"}}, &m, 201)
	f.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Site 2", "type": "http", "interval": 5000, "timeout": 20,
		"config": map[string]any{"url": "https://example.com"}}, nil, 200)
	var st map[string]any
	f.mustDo("GET", "/api/settings", nil, &st, 200)
	st["backup_keep"] = 9
	f.mustDo("PUT", "/api/settings", st, nil, 200)

	key := f.newKey("otomasyon", store.RoleAdmin, 0)
	if code, _, body := f.rawReq("PUT", "/api/settings", bearer(key.Secret), st); code != 200 {
		t.Fatalf("anahtarla ayar güncelleme: %d %s", code, body)
	}

	var log []store.AuditEntry
	f.mustDo("GET", "/api/audit", nil, &log, 200)
	byAction := map[string][]string{}
	for _, e := range log {
		byAction[e.Action] = append(byAction[e.Action], e.Detail)
	}
	if d := byAction["monitor.update"]; len(d) != 1 || d[0] != "değişen: ad, zaman aşımı" {
		t.Errorf("monitör güncelleme ayrıntısı: %v", d)
	}
	// En yeni önce: anahtarla yapılan (değişiklik yok) ve oturumla yapılan.
	if d := byAction["settings.update"]; len(d) != 2 || d[0] != "değişiklik yok; API anahtarı: otomasyon" || d[1] != "değişen: yedek sayısı" {
		t.Errorf("ayar güncelleme ayrıntısı: %v", d)
	}
	// İngilizce okuma çevirir.
	var en []store.AuditEntry
	f.langGet("en", "/api/audit", &en)
	var got []string
	for _, e := range en {
		if e.Action == "monitor.update" || e.Action == "settings.update" {
			got = append(got, e.Detail)
		}
	}
	joined := strings.Join(got, " | ")
	for _, want := range []string{"changed: name, timeout", "no changes; API key: otomasyon", "changed: backups kept"} {
		if !strings.Contains(joined, want) {
			t.Errorf("İngilizce ayrıntı %q yok: %s", want, joined)
		}
	}
}
