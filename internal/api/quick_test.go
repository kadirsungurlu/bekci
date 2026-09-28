package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// setupAdminSrv setupAdmin gibi; ayrıca veritabanına doğrudan erişim için sunucuyu döner.
func setupAdminSrv(t *testing.T) (*env, *Server) {
	var srv *Server
	e := newEnv(t, func(s *Server) { srv = s })
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return e, srv
}

func findView(t *testing.T, e *env, id int64) monitorView {
	t.Helper()
	var list []monitorView
	e.mustDo("GET", "/api/monitors", nil, &list, 200)
	for _, m := range list {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("monitör %d listede yok", id)
	return monitorView{}
}

func TestQuickActionPermissions(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("A")
	var page store.StatusPage
	admin.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel", "sections": []any{}}, &page, 201)
	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	restricted, _ := admin.newUser("musteri", store.RoleViewer, []int64{m.ID})

	cases := []struct {
		method, path string
		body         any
		editor       int
	}{
		{"PUT", fmt.Sprintf("/api/monitors/%d/notifications", m.ID), map[string]any{"notification_ids": []int64{}}, 200},
		{"POST", fmt.Sprintf("/api/monitors/%d/reset-stats", m.ID), nil, 200},
		{"POST", "/api/monitors/bulk", map[string]any{"ids": []int64{m.ID}, "action": "pause"}, 200},
		{"POST", fmt.Sprintf("/api/status-pages/%d/monitors", page.ID), map[string]any{"monitor_id": m.ID, "section": -1}, 200},
		{"POST", fmt.Sprintf("/api/monitors/%d/clone", m.ID), nil, 201},
	}
	for _, c := range cases {
		for name, v := range map[string]*env{"izleyici": viewer, "kısıtlı izleyici": restricted} {
			if got := v.do(c.method, c.path, c.body, nil); got != http.StatusForbidden {
				t.Errorf("%s %s %s: %d, 403 bekleniyordu", name, c.method, c.path, got)
			}
		}
		if got := editor.do(c.method, c.path, c.body, nil); got != c.editor {
			t.Errorf("editör %s %s: %d, %d bekleniyordu", c.method, c.path, got, c.editor)
		}
	}
	// İzleyicinin isteği hiçbir şeyi değiştirmemeli: kopya yalnızca editörden.
	var list []monitorView
	admin.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 2 {
		t.Errorf("izleyici kopya oluşturmuş olabilir: %d monitör", len(list))
	}
}

func TestCloneMonitor(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	ctx := context.Background()

	var ch store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Kanca", "type": "webhook", "config": map[string]any{"url": "http://127.0.0.1:9/x"},
	}, &ch, 201)
	var tag tagView
	admin.mustDo("POST", "/api/tags", map[string]any{"name": "Ortam"}, &tag, 201)

	var src monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "http", "interval": 60000, "notification_ids": []int64{ch.ID},
		"config": map[string]any{"url": "http://127.0.0.1:9/", "basic_user": "a", "basic_pass": "cok-gizli-parola"},
	}, &src, 201)
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", src.ID), []map[string]any{{"tag_id": tag.ID, "value": "canlı"}}, nil, 200)
	var setup struct{ Probe store.Probe }
	admin.mustDo("POST", "/api/probes", map[string]any{"name": "İstanbul"}, &setup, 201)
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/locations", src.ID), map[string]any{
		"include_local": false, "probe_ids": []int64{setup.Probe.ID}, "down_when": "all",
	}, nil, 200)

	var cp monitorView
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/clone", src.ID), nil, &cp, 201)
	if cp.ID == src.ID || cp.Name != "Site (kopya)" || cp.Active || cp.Type != "http" || cp.Interval != 60000 {
		t.Fatalf("kopya yanlış: %+v", cp)
	}
	if !slices.Equal(cp.NotificationIDs, []int64{ch.ID}) || len(cp.Tags) != 1 || cp.Tags[0].Value != "canlı" {
		t.Errorf("kanallar/etiketler kopyalanmadı: %+v %+v", cp.NotificationIDs, cp.Tags)
	}
	if l := cp.Locations; l.IncludeLocal || l.DownWhen != "all" || !slices.Equal(l.ProbeIDs, []int64{setup.Probe.ID}) {
		t.Errorf("konum ayarı kopyalanmadı: %+v", l)
	}
	if strings.Contains(string(cp.Config), "cok-gizli-parola") {
		t.Error("yanıtta şifre maskelenmeli")
	}
	// Gizli alan veritabanında kopyalanmış olmalı (maskeli değer değil).
	stored, err := srv.store.GetMonitor(ctx, cp.ID)
	if err != nil || !strings.Contains(string(stored.Config), "cok-gizli-parola") {
		t.Errorf("şifre kopyalanmadı: %s %v", stored.Config, err)
	}
	if stored.Status != store.StatusPending || stored.LastCheckAt != 0 {
		t.Errorf("kopya temiz durumla başlamalı: %+v", stored)
	}

	// Push monitörüne yeni adres verilir; uzun ad sınırı aşmaz.
	long := strings.Repeat("ş", 100)
	var p monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": long, "type": "push", "interval": 5000, "config": map[string]any{}}, &p, 201)
	var pc monitorView
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/clone", p.ID), nil, &pc, 201)
	if pc.PushToken == "" || pc.PushToken == p.PushToken {
		t.Errorf("push token'ı yenilenmeli: %q %q", pc.PushToken, p.PushToken)
	}
	if n := len([]rune(pc.Name)); n != 100 || !strings.HasSuffix(pc.Name, " (kopya)") {
		t.Errorf("uzun ad: %d %q", n, pc.Name)
	}
	admin.mustDo("POST", "/api/monitors/99999/clone", nil, nil, 404)

	// İşlem kaydı.
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	if len(audit) == 0 || audit[0].Action != "monitor.clone" {
		t.Errorf("işlem kaydı: %+v", audit[:min(1, len(audit))])
	}
}

func TestResetStats(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	ctx := context.Background()
	m := admin.push("A")
	other := admin.push("B")
	now := time.Now().Unix()

	for _, id := range []int64{m.ID, other.ID} {
		// Eskiden yeniye; son kontrol DOWN (monitör şu an çalışmıyor).
		for i := range 3 {
			st := store.StatusUp
			if i == 2 {
				st = store.StatusDown
			}
			if err := srv.store.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: id, Time: now - int64(60*(3-i)), Status: st, PingMs: 10}}); err != nil {
				t.Fatal(err)
			}
		}
		// Bitmiş bir olay ve süren bir olay.
		srv.store.OpenIncident(ctx, id, now-7200, "eski")
		srv.store.ResolveIncident(ctx, id, now-7000)
		srv.store.OpenIncident(ctx, id, now-60, "süren")
	}
	if v := findView(t, admin, m.ID); v.Uptime24h == nil || v.OpenIncidentID == nil {
		t.Fatalf("hazırlık: %+v", v)
	}

	var res monitorView
	admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/reset-stats", m.ID), nil, &res, 200)
	if res.Uptime24h != nil {
		t.Errorf("uptime sıfırlanmalı: %v", *res.Uptime24h)
	}
	for _, b := range res.Bars {
		if b.Up+b.Down != 0 {
			t.Fatalf("çubuklar boş olmalı: %+v", b)
		}
	}
	beats, _ := srv.store.Beats(ctx, m.ID, 0)
	daily, _ := srv.store.Series(ctx, m.ID, 0, true)
	if len(beats) != 0 || len(daily) != 0 {
		t.Errorf("ham kayıt/günlük özet silinmeli: %d %d", len(beats), len(daily))
	}
	var incs []store.Incident
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/incidents", m.ID), nil, &incs, 200)
	if len(incs) != 1 || incs[0].ResolvedAt != 0 || res.OpenIncidentID == nil || *res.OpenIncidentID != incs[0].ID {
		t.Errorf("yalnızca süren olay kalmalı: %+v (açık olay %v)", incs, res.OpenIncidentID)
	}
	// Diğer monitör etkilenmez.
	if v := findView(t, admin, other.ID); v.Uptime24h == nil {
		t.Error("diğer monitörün istatistikleri silinmemeli")
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/incidents", other.ID), nil, &incs, 200)
	if len(incs) != 2 {
		t.Errorf("diğer monitörün olayları: %d", len(incs))
	}
	admin.mustDo("POST", "/api/monitors/99999/reset-stats", nil, nil, 404)

	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	if len(audit) == 0 || audit[0].Action != "monitor.reset_stats" || audit[0].TargetID != m.ID {
		t.Errorf("işlem kaydı: %+v", audit[:min(1, len(audit))])
	}
}

func TestMonitorNotificationsPut(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("A")
	var a, b store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "A", "type": "webhook", "config": map[string]any{"url": "http://127.0.0.1:9/a"}}, &a, 201)
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "B", "type": "webhook", "config": map[string]any{"url": "http://127.0.0.1:9/b"}}, &b, 201)

	var v monitorView
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/notifications", m.ID), map[string]any{"notification_ids": []int64{b.ID, a.ID, b.ID}}, &v, 200)
	if !slices.Equal(v.NotificationIDs, []int64{a.ID, b.ID}) {
		t.Errorf("kanallar: %v", v.NotificationIDs)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/notifications", m.ID), map[string]any{"notification_ids": []int64{a.ID, 9999}}, nil, 400)
	if got := findView(t, admin, m.ID).NotificationIDs; len(got) != 2 {
		t.Errorf("hatalı istek değişiklik yapmamalı: %v", got)
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/notifications", m.ID), map[string]any{"notification_ids": nil}, &v, 200)
	if len(v.NotificationIDs) != 0 {
		t.Errorf("boş liste tüm kanalları kaldırmalı: %v", v.NotificationIDs)
	}
	admin.mustDo("PUT", "/api/monitors/9999/notifications", map[string]any{"notification_ids": []int64{}}, nil, 404)
}

func TestBulkMonitors(t *testing.T) {
	admin := setupAdmin(t)
	a, b, c := admin.push("A"), admin.push("B"), admin.push("C")
	ids := []int64{a.ID, b.ID}
	var ch store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "Kanca", "type": "webhook", "config": map[string]any{"url": "http://127.0.0.1:9/a"}}, &ch, 201)
	var tag tagView
	admin.mustDo("POST", "/api/tags", map[string]any{"name": "Ekip"}, &tag, 201)

	bulk := func(body map[string]any, want int) bulkResult {
		t.Helper()
		var res bulkResult
		admin.mustDo("POST", "/api/monitors/bulk", body, &res, want)
		return res
	}

	// Doğrulama: hiçbir değişiklik yapılmaz.
	bulk(map[string]any{"ids": []int64{a.ID, 99999}, "action": "pause"}, 400)
	bulk(map[string]any{"ids": []int64{}, "action": "pause"}, 400)
	bulk(map[string]any{"ids": ids, "action": "patlat"}, 400)
	bulk(map[string]any{"ids": ids, "action": "add_tag", "tag_id": 9999}, 400)
	bulk(map[string]any{"ids": ids, "action": "add_notification", "notification_id": 9999}, 400)
	many := make([]int64, maxBulkMonitors+1)
	for i := range many {
		many[i] = int64(i + 1)
	}
	bulk(map[string]any{"ids": many, "action": "pause"}, 400)
	if !findView(t, admin, a.ID).Active {
		t.Fatal("geçersiz istek monitörü durdurmamalı")
	}

	res := bulk(map[string]any{"ids": []int64{a.ID, b.ID, a.ID}, "action": "pause"}, 200)
	if res.Changed != 2 || len(res.Monitors) != 2 || res.Monitors[0].Active || res.Monitors[1].Active {
		t.Errorf("durdurma: %+v", res)
	}
	if res := bulk(map[string]any{"ids": ids, "action": "pause"}, 200); res.Changed != 0 {
		t.Errorf("zaten durdurulmuş: %d", res.Changed)
	}
	res = bulk(map[string]any{"ids": []int64{a.ID, b.ID, c.ID}, "action": "resume"}, 200)
	if res.Changed != 2 || !res.Monitors[0].Active || !res.Monitors[1].Active {
		t.Errorf("başlatma: %+v", res)
	}

	res = bulk(map[string]any{"ids": ids, "action": "add_tag", "tag_id": tag.ID, "value": " altyapı "}, 200)
	if res.Changed != 2 || len(res.Monitors[0].Tags) != 1 || res.Monitors[0].Tags[0].Value != "altyapı" {
		t.Errorf("etiket ekleme: %+v", res.Monitors)
	}
	if res := bulk(map[string]any{"ids": ids, "action": "add_tag", "tag_id": tag.ID, "value": "altyapı"}, 200); res.Changed != 0 {
		t.Errorf("aynı etiket tekrar eklenmemeli: %d", res.Changed)
	}
	bulk(map[string]any{"ids": ids, "action": "add_tag", "tag_id": tag.ID, "value": strings.Repeat("d", 101)}, 400)
	res = bulk(map[string]any{"ids": []int64{a.ID, c.ID}, "action": "remove_tag", "tag_id": tag.ID}, 200)
	if res.Changed != 1 || len(findView(t, admin, a.ID).Tags) != 0 || len(findView(t, admin, b.ID).Tags) != 1 {
		t.Errorf("etiket kaldırma: %+v", res)
	}

	res = bulk(map[string]any{"ids": []int64{a.ID, b.ID, c.ID}, "action": "add_notification", "notification_id": ch.ID}, 200)
	if res.Changed != 3 || !slices.Equal(res.Monitors[2].NotificationIDs, []int64{ch.ID}) {
		t.Errorf("kanal ekleme: %+v", res)
	}
	res = bulk(map[string]any{"ids": ids, "action": "remove_notification", "notification_id": ch.ID}, 200)
	if res.Changed != 2 || len(findView(t, admin, a.ID).NotificationIDs) != 0 || len(findView(t, admin, c.ID).NotificationIDs) != 1 {
		t.Errorf("kanal çıkarma: %+v", res)
	}

	res = bulk(map[string]any{"ids": ids, "action": "delete"}, 200)
	if res.Changed != 2 || !slices.Equal(res.Deleted, ids) {
		t.Errorf("silme: %+v", res)
	}
	var list []monitorView
	admin.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || list[0].ID != c.ID {
		t.Errorf("silme sonrası liste: %+v", list)
	}
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit?limit=200", nil, &audit, 200)
	deletes := 0
	for _, e := range audit {
		if e.Action == "monitor.delete" && e.Detail == "toplu işlem" {
			deletes++
		}
	}
	if deletes != 2 {
		t.Errorf("her silinen monitör için işlem kaydı: %d", deletes)
	}
}

func TestBulkDeleteCleansGroups(t *testing.T) {
	admin := setupAdmin(t)
	a, b, c := admin.push("A"), admin.push("B"), admin.push("C")
	var g monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Grup", "type": "group", "interval": 60,
		"config": map[string]any{"monitor_ids": []int64{a.ID, b.ID, c.ID}}}, &g, 201)
	admin.mustDo("POST", "/api/monitors/bulk", map[string]any{"ids": []int64{a.ID, b.ID}, "action": "delete"}, nil, 200)
	var d struct{ Monitor monitorView }
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", g.ID), nil, &d, 200)
	if s := string(d.Monitor.Config); !strings.Contains(s, fmt.Sprintf("[%d]", c.ID)) {
		t.Errorf("grup ayarından silinenler çıkarılmalı: %s", s)
	}
}

func TestAddPageMonitor(t *testing.T) {
	admin := setupAdmin(t)
	a, b, c := admin.push("A"), admin.push("B"), admin.push("C")
	var page store.StatusPage
	admin.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "genel", "title": "Genel", "footer": "alt", "bar_range": "90d", "published": false, "password": "sifre1234",
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID}}}},
	}, &page, 201)
	path := fmt.Sprintf("/api/status-pages/%d/monitors", page.ID)

	var p store.StatusPage
	admin.mustDo("POST", path, map[string]any{"monitor_id": b.ID, "section": 0, "name": " B sitesi "}, &p, 200)
	if len(p.Sections) != 1 || len(p.Sections[0].Monitors) != 2 || p.Sections[0].Monitors[1].Name != "B sitesi" {
		t.Errorf("gruba ekleme: %+v", p.Sections)
	}
	// Sayfanın diğer ayarları korunur.
	if p.Footer != "alt" || p.BarRange != "90d" || p.Published || !p.HasPassword || p.Slug != "genel" {
		t.Errorf("sayfa ayarları değişmemeli: %+v", p)
	}
	admin.mustDo("POST", path, map[string]any{"monitor_id": b.ID, "section": -1}, nil, 409)
	admin.mustDo("POST", path, map[string]any{"monitor_id": c.ID, "section": 5}, nil, 400)
	admin.mustDo("POST", path, map[string]any{"monitor_id": 9999, "section": 0}, nil, 400)
	admin.mustDo("POST", path, map[string]any{"monitor_id": c.ID, "section": -1, "section_title": strings.Repeat("x", 101)}, nil, 400)
	admin.mustDo("POST", path, map[string]any{"monitor_id": c.ID, "section": -1, "section_title": "Altyapı"}, &p, 200)
	if len(p.Sections) != 2 || p.Sections[1].Title != "Altyapı" || p.Sections[1].Monitors[0].ID != c.ID {
		t.Errorf("yeni grup: %+v", p.Sections)
	}
	admin.mustDo("POST", "/api/status-pages/9999/monitors", map[string]any{"monitor_id": c.ID, "section": -1}, nil, 404)
}

func TestOpenIncidentID(t *testing.T) {
	admin, srv := setupAdminSrv(t)
	ctx := context.Background()
	m := admin.push("A")
	other := admin.push("B")
	if v := findView(t, admin, m.ID); v.OpenIncidentID != nil {
		t.Fatalf("olay yokken null olmalı: %v", *v.OpenIncidentID)
	}
	srv.store.OpenIncident(ctx, m.ID, time.Now().Unix(), "hata")
	var incs []store.Incident
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d/incidents", m.ID), nil, &incs, 200)
	v := findView(t, admin, m.ID)
	if v.OpenIncidentID == nil || *v.OpenIncidentID != incs[0].ID {
		t.Fatalf("açık olay kimliği: %v", v.OpenIncidentID)
	}
	// Kısıtlı izleyici yalnızca kendi monitörünü (ve olayını) görür.
	restricted, _ := admin.newUser("musteri", store.RoleViewer, []int64{other.ID})
	var list []monitorView
	restricted.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || list[0].ID != other.ID || list[0].OpenIncidentID != nil {
		t.Errorf("kısıtlı izleyici: %+v", list)
	}
	srv.store.ResolveIncident(ctx, m.ID, time.Now().Unix())
	if v := findView(t, admin, m.ID); v.OpenIncidentID != nil {
		t.Errorf("olay kapanınca null olmalı: %v", *v.OpenIncidentID)
	}
}
