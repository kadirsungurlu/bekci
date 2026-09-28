package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/check"
	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

var istanbul, _ = time.LoadLocation("Europe/Istanbul")

func localTime(d time.Duration) string {
	return time.Now().In(istanbul).Add(d).Format("2006-01-02T15:04")
}

// onceWindow şu anı kapsayan tek seferlik pencere gövdesi.
func onceWindow(title string, ids ...int64) map[string]any {
	return map[string]any{
		"title": title, "strategy": "once", "start": localTime(-time.Hour), "end": localTime(time.Hour),
		"monitor_ids": ids,
	}
}

func TestMaintenanceAPI(t *testing.T) {
	admin := setupAdmin(t)
	a := admin.push("A")
	b := admin.push("B")

	// Doğrulama.
	bad := []map[string]any{
		{"title": "", "strategy": "manual", "all_monitors": true},
		{"title": "x", "strategy": "yillik", "all_monitors": true},
		{"title": "x", "strategy": "manual"},
		{"title": "x", "strategy": "manual", "monitor_ids": []int64{99999}},
		{"title": "x", "strategy": "manual", "all_monitors": true, "timezone": "Yok/Yer"},
		{"title": "x", "strategy": "once", "start": localTime(time.Hour), "end": localTime(0), "all_monitors": true},
		{"title": "x", "strategy": "recurring_weekly", "start_time": "02:00", "end_time": "03:00", "all_monitors": true},
		{"title": "x", "strategy": "recurring_daily", "start_time": "2:60", "end_time": "03:00", "all_monitors": true},
		{"title": "x", "strategy": "cron", "cron": "bozuk", "duration_minutes": 10, "all_monitors": true},
		{"title": "x", "strategy": "manual", "all_monitors": true, "status": "active"}, // salt okunur alan
	}
	for _, body := range bad {
		if code := admin.do("POST", "/api/maintenance", body, nil); code != 400 {
			t.Errorf("%v: %d, 400 bekleniyordu", body, code)
		}
	}

	// Şu an süren pencere: A bakımda.
	var w maintenanceView
	admin.mustDo("POST", "/api/maintenance", onceWindow("Sunucu taşıma", a.ID), &w, 201)
	if w.Status != "active" || !w.Active || w.Timezone != "Europe/Istanbul" || w.NextEnd <= time.Now().Unix() ||
		len(w.MonitorIDs) != 1 || w.MonitorIDs[0] != a.ID {
		t.Fatalf("pencere yanlış: %+v", w)
	}
	inMaint := func(c *env) map[int64]bool {
		var list []monitorView
		c.mustDo("GET", "/api/monitors", nil, &list, 200)
		out := map[int64]bool{}
		for _, m := range list {
			out[m.ID] = m.InMaintenance
		}
		return out
	}
	if got := inMaint(admin); !got[a.ID] || got[b.ID] {
		t.Fatalf("in_maintenance yanlış: %v", got)
	}
	var detail struct{ Monitor monitorView }
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", a.ID), nil, &detail, 200)
	if !detail.Monitor.InMaintenance {
		t.Error("detayda in_maintenance olmalı")
	}
	var sum map[string]any
	admin.mustDo("GET", "/api/summary", nil, &sum, 200)
	if sum["maintenance"] != float64(1) || sum["total"] != float64(2) {
		t.Errorf("özet bakımı ayrı saymalı: %v", sum)
	}

	// Gelecek haftalık pencere: planlandı; ilgisiz alanlar temizlenir.
	var weekly maintenanceView
	admin.mustDo("POST", "/api/maintenance", map[string]any{
		"title": "Haftalık", "strategy": "recurring_weekly", "weekdays": []int{1, 3}, "start_time": "23:00", "end_time": "01:00",
		"date_from": time.Now().In(istanbul).AddDate(0, 0, 10).Format("2006-01-02"), "start": "yok sayılır",
		"monitor_ids": []int64{b.ID}, "active": true,
	}, &weekly, 201)
	if weekly.Status != "scheduled" || weekly.Start != "" || weekly.NextStart <= time.Now().Unix() {
		t.Fatalf("haftalık pencere yanlış: %+v", weekly)
	}

	// Durdur / başlat.
	var paused maintenanceView
	admin.mustDo("POST", fmt.Sprintf("/api/maintenance/%d/pause", w.ID), nil, &paused, 200)
	if paused.Status != "inactive" || paused.Active {
		t.Fatalf("durdurulmuş olmalı: %+v", paused)
	}
	if inMaint(admin)[a.ID] {
		t.Error("durdurulan pencere bakım saymamalı")
	}
	admin.mustDo("POST", fmt.Sprintf("/api/maintenance/%d/resume", w.ID), nil, nil, 200)
	if !inMaint(admin)[a.ID] {
		t.Error("yeniden başlatılan pencere bakım saymalı")
	}

	// Düzenleme: B'ye taşı.
	body := onceWindow("Sunucu taşıma 2", b.ID)
	var edited maintenanceView
	admin.mustDo("PUT", fmt.Sprintf("/api/maintenance/%d", w.ID), body, &edited, 200)
	if edited.Title != "Sunucu taşıma 2" || edited.MonitorIDs[0] != b.ID {
		t.Fatalf("düzenleme yanlış: %+v", edited)
	}
	if got := inMaint(admin); got[a.ID] || !got[b.ID] {
		t.Fatalf("düzenleme sonrası in_maintenance yanlış: %v", got)
	}
	admin.mustDo("PUT", "/api/maintenance/99999", body, nil, 404)
	admin.mustDo("GET", "/api/maintenance/abc", nil, nil, 404)

	// Yetkiler: izleyici okur ama değiştiremez; editör yönetir.
	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	var list []maintenanceView
	viewer.mustDo("GET", "/api/maintenance", nil, &list, 200)
	if len(list) != 2 {
		t.Errorf("kısıtsız izleyici tüm pencereleri görmeli: %d", len(list))
	}
	viewer.mustDo("POST", "/api/maintenance", onceWindow("x", a.ID), nil, 403)
	viewer.mustDo("PUT", fmt.Sprintf("/api/maintenance/%d", w.ID), body, nil, 403)
	viewer.mustDo("DELETE", fmt.Sprintf("/api/maintenance/%d", w.ID), nil, nil, 403)
	viewer.mustDo("POST", fmt.Sprintf("/api/maintenance/%d/pause", w.ID), nil, nil, 403)
	var byEditor maintenanceView
	editor.mustDo("POST", "/api/maintenance", map[string]any{"title": "Genel", "strategy": "cron", "cron": "0 3 * * *",
		"duration_minutes": 30, "all_monitors": true, "monitor_ids": []int64{a.ID}}, &byEditor, 201)
	if !byEditor.AllMonitors || len(byEditor.MonitorIDs) != 0 || byEditor.NextStart == 0 {
		t.Fatalf("editör penceresi yanlış: %+v", byEditor)
	}

	// Kısıtlı izleyici: yalnızca kendi monitörünü etkileyen ve genel pencereler.
	customer, _ := admin.newUser("musteri", store.RoleViewer, []int64{a.ID})
	customer.mustDo("GET", "/api/maintenance", nil, &list, 200)
	if len(list) != 1 || list[0].ID != byEditor.ID {
		t.Fatalf("müşteri sadece genel pencereyi görmeli: %+v", list)
	}
	customer.mustDo("GET", fmt.Sprintf("/api/maintenance/%d", w.ID), nil, nil, 404)
	admin.mustDo("PUT", fmt.Sprintf("/api/maintenance/%d", w.ID), onceWindow("İkisi", a.ID, b.ID), nil, 200)
	var seen maintenanceView
	customer.mustDo("GET", fmt.Sprintf("/api/maintenance/%d", w.ID), nil, &seen, 200)
	if len(seen.MonitorIDs) != 1 || seen.MonitorIDs[0] != a.ID {
		t.Errorf("müşteri başka monitörün kimliğini görmemeli: %+v", seen.MonitorIDs)
	}

	// Monitör silinince pencereden de çıkar.
	admin.mustDo("DELETE", fmt.Sprintf("/api/monitors/%d", a.ID), nil, nil, 200)
	admin.mustDo("GET", fmt.Sprintf("/api/maintenance/%d", w.ID), nil, &seen, 200)
	if len(seen.MonitorIDs) != 1 || seen.MonitorIDs[0] != b.ID {
		t.Errorf("silinen monitör pencereden çıkmalı: %+v", seen.MonitorIDs)
	}

	// Silme ve işlem kaydı.
	editor.mustDo("DELETE", fmt.Sprintf("/api/maintenance/%d", weekly.ID), nil, nil, 200)
	admin.mustDo("GET", fmt.Sprintf("/api/maintenance/%d", weekly.ID), nil, nil, 404)
	var log []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &log, 200)
	var actions []string
	for _, e := range log {
		if e.TargetType == "maintenance" {
			actions = append(actions, e.Action+"@"+e.Username)
		}
	}
	got := strings.Join(actions, ",")
	for _, want := range []string{"maintenance.create@kadir", "maintenance.pause@kadir", "maintenance.resume@kadir",
		"maintenance.update@kadir", "maintenance.create@editor1", "maintenance.delete@editor1"} {
		if !strings.Contains(got, want) {
			t.Errorf("işlem kaydında %s yok: %s", want, got)
		}
	}
}

// Bakımdaki push monitörüne DOWN sinyali: durum 3, bildirim/olay yok.
func TestMaintenancePushNoAlert(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("Yedek")
	admin.mustDo("POST", "/api/maintenance", map[string]any{"title": "Elle", "strategy": "manual", "monitor_ids": []int64{m.ID}}, nil, 201)
	resp, err := http.Get(admin.srv.URL + "/api/push/" + m.PushToken + "?status=down&msg=hata")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	waitFor(t, "bakım durumu", func() bool {
		var d struct{ Monitor monitorView }
		admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", m.ID), nil, &d, 200)
		return d.Monitor.Status == store.StatusMaintenance
	})
	var inc []store.Incident
	admin.mustDo("GET", "/api/incidents", nil, &inc, 200)
	if len(inc) != 0 {
		t.Errorf("bakımda olay açılmamalı: %+v", inc)
	}
}

func TestGroupMonitorAPI(t *testing.T) {
	admin := setupAdmin(t)
	a := admin.push("A")
	b := admin.push("B")
	group := func(name string, ids ...int64) map[string]any {
		return map[string]any{"name": name, "type": "group", "interval": 20, "config": map[string]any{"monitor_ids": ids}}
	}
	admin.mustDo("POST", "/api/monitors", group("Boş"), nil, 400)
	admin.mustDo("POST", "/api/monitors", group("Yok", a.ID, 99999), nil, 400)
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Mod", "type": "group", "config": map[string]any{"monitor_ids": []int64{a.ID}, "mode": "x"}}, nil, 400)

	var g1, g2 monitorView
	admin.mustDo("POST", "/api/monitors", group("Grup 1", a.ID, b.ID), &g1, 201)
	if g1.Target != "2 monitör" {
		t.Errorf("hedef yanlış: %q", g1.Target)
	}
	var cfg check.GroupConfig
	json.Unmarshal(g1.Config, &cfg)
	if cfg.Mode != "any_down" {
		t.Errorf("varsayılan mod any_down olmalı: %s", g1.Config)
	}
	admin.mustDo("POST", "/api/monitors", group("Grup 2", g1.ID), &g2, 201) // grup içinde grup serbest

	// Kendini içeremez; döngü oluşturamaz.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", g1.ID), group("Grup 1", a.ID, g1.ID), nil, 400)
	var e struct{ Error string }
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", g1.ID), group("Grup 1", a.ID, g2.ID), &e, 400)
	if !strings.Contains(e.Error, "döngü") {
		t.Errorf("döngü hatası bekleniyordu: %q", e.Error)
	}

	// Alt monitör DOWN → grup DOWN (mesajda adı), düzelince UP.
	r, _ := http.Get(admin.srv.URL + "/api/push/" + b.PushToken + "?status=down&msg=x")
	r.Body.Close()
	status := func(id int64) monitorView {
		var d struct{ Monitor monitorView }
		admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", id), nil, &d, 200)
		return d.Monitor
	}
	waitFor(t, "grup DOWN", func() bool {
		m := status(g1.ID)
		return m.Status == store.StatusDown && strings.Contains(m.LastMessage, "çalışmıyor: B")
	})
	waitFor(t, "üst grup DOWN", func() bool { return status(g2.ID).Status == store.StatusDown })
	r, _ = http.Get(admin.srv.URL + "/api/push/" + b.PushToken)
	r.Body.Close()
	r, _ = http.Get(admin.srv.URL + "/api/push/" + a.PushToken)
	r.Body.Close()
	waitFor(t, "grup UP", func() bool { return status(g1.ID).Status == store.StatusUp })

	// Alt monitör silinince grup ayarından çıkar.
	admin.mustDo("DELETE", fmt.Sprintf("/api/monitors/%d", b.ID), nil, nil, 200)
	m := status(g1.ID)
	json.Unmarshal(m.Config, &cfg)
	if len(cfg.MonitorIDs) != 1 || cfg.MonitorIDs[0] != a.ID || m.Target != "1 monitör" {
		t.Errorf("silinen alt monitör gruptan çıkmalı: %s %q", m.Config, m.Target)
	}
}
