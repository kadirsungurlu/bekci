package api

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// loginAs aynı sunucuda ayrı çerez kavanozlu yeni bir istemci açar.
func (e *env) loginAs(username, password string) *env {
	e.t.Helper()
	jar, _ := cookiejar.New(nil)
	other := &env{t: e.t, srv: e.srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
	other.mustDo("POST", "/api/auth/login", map[string]string{"username": username, "password": password}, nil, 200)
	return other
}

// newUser yönetici olarak kullanıcı ekler; kullanıcı zorunlu şifre değişimini yapar.
func (e *env) newUser(username, role string, restrictTo []int64) (*env, store.User) {
	e.t.Helper()
	body := map[string]any{"username": username, "role": role, "password": "gecici-sifre-1"}
	if restrictTo != nil {
		body["all_monitors"] = false
		body["monitor_ids"] = restrictTo
	}
	var u store.User
	e.mustDo("POST", "/api/users", body, &u, 201)
	c := e.loginAs(username, "gecici-sifre-1")
	c.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	return c, u
}

func setupAdmin(t *testing.T) *env {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return e
}

func (e *env) push(name string) monitorView {
	e.t.Helper()
	var m monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": name, "type": "push", "interval": 5000, "config": map[string]any{}}, &m, 201)
	return m
}

func TestRolePermissions(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("A")
	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)

	cases := []struct {
		method, path string
		body         any
		editor       int
		viewer       int
	}{
		{"GET", "/api/monitors", nil, 200, 200},
		{"GET", "/api/summary", nil, 200, 200},
		{"GET", "/api/incidents", nil, 200, 200},
		{"GET", fmt.Sprintf("/api/monitors/%d", m.ID), nil, 200, 200},
		{"POST", "/api/monitors", map[string]any{"name": "x", "type": "push", "interval": 60, "config": map[string]any{}}, 201, 403},
		{"POST", fmt.Sprintf("/api/monitors/%d/pause", m.ID), nil, 200, 403},
		{"GET", "/api/notifications", nil, 200, 403},
		{"GET", "/api/settings", nil, 403, 403},
		{"GET", "/api/users", nil, 403, 403},
		{"GET", "/api/audit", nil, 403, 403},
		{"POST", "/api/users", map[string]any{"username": "hacker", "role": "admin", "password": "12345678"}, 403, 403},
	}
	for _, c := range cases {
		if got := editor.do(c.method, c.path, c.body, nil); got != c.editor {
			t.Errorf("editör %s %s: %d, %d bekleniyordu", c.method, c.path, got, c.editor)
		}
		if got := viewer.do(c.method, c.path, c.body, nil); got != c.viewer {
			t.Errorf("izleyici %s %s: %d, %d bekleniyordu", c.method, c.path, got, c.viewer)
		}
	}

	// İzleyici monitör ayarlarını, push token'ını ve bildirim bağlantılarını görmez.
	var list []monitorView
	viewer.mustDo("GET", "/api/monitors", nil, &list, 200)
	for _, v := range list {
		if v.PushToken != "" || string(v.Config) != "{}" || len(v.NotificationIDs) != 0 {
			t.Errorf("izleyiciye ayar sızdı: %+v", v)
		}
		if v.Target == "" {
			t.Errorf("izleyici hedefi boş görmemeli: %+v", v)
		}
	}
	editor.mustDo("GET", "/api/monitors", nil, &list, 200)
	if list[0].PushToken == "" {
		t.Error("editör push token'ını görmeli")
	}
}

func TestCustomerRestriction(t *testing.T) {
	admin := setupAdmin(t)
	mine := admin.push("Müşterinin sitesi")
	other := admin.push("Başka müşteri")
	customer, _ := admin.newUser("musteri", store.RoleViewer, []int64{mine.ID})

	var list []monitorView
	customer.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || list[0].ID != mine.ID {
		t.Fatalf("müşteri sadece kendi monitörünü görmeli: %+v", list)
	}
	for _, p := range []string{"", "/series", "/incidents"} {
		if got := customer.do("GET", fmt.Sprintf("/api/monitors/%d%s", other.ID, p), nil, nil); got != 404 {
			t.Errorf("başka monitör %s: %d, 404 bekleniyordu", p, got)
		}
	}
	var sum map[string]any
	customer.mustDo("GET", "/api/summary", nil, &sum, 200)
	if sum["total"] != float64(1) {
		t.Errorf("özet sadece izinli monitörleri saymalı: %v", sum)
	}

	// Olaylar: başka monitörün olayı görünmez.
	resp, _ := http.Get(e2url(admin, "/api/push/"+other.PushToken+"?status=down&msg=x"))
	resp.Body.Close()
	waitFor(t, "başka monitörde olay", func() bool {
		var all []store.Incident
		admin.mustDo("GET", "/api/incidents", nil, &all, 200)
		return len(all) == 1
	})
	var inc []store.Incident
	customer.mustDo("GET", "/api/incidents", nil, &inc, 200)
	if len(inc) != 0 {
		t.Errorf("müşteri başka monitörün olayını görmemeli: %+v", inc)
	}

	// Canlı akış: sadece izinli monitörün olayları gelir.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", customer.srv.URL+"/api/events", nil)
	sresp, err := customer.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer sresp.Body.Close()
	for _, m := range []monitorView{other, mine} {
		r, _ := http.Get(e2url(admin, "/api/push/"+m.PushToken))
		r.Body.Close()
	}
	sc := bufio.NewScanner(sresp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Data struct {
				MonitorID int64 `json:"monitor_id"`
			} `json:"data"`
		}
		json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
		if ev.Data.MonitorID == other.ID {
			t.Fatal("canlı akışta başka monitörün olayı geldi")
		}
		if ev.Data.MonitorID == mine.ID {
			return
		}
	}
	t.Fatal("izinli monitörün canlı olayı gelmedi")
}

func e2url(e *env, path string) string { return e.srv.URL + path }

func TestMustChangePassword(t *testing.T) {
	admin := setupAdmin(t)
	var u store.User
	admin.mustDo("POST", "/api/users", map[string]any{"username": "yeni", "role": "editor", "password": "gecici-sifre-1"}, &u, 201)
	c := admin.loginAs("yeni", "gecici-sifre-1")

	var state struct{ User userView }
	c.mustDo("GET", "/api/auth/state", nil, &state, 200)
	if !state.User.MustChangePassword || state.User.Role != "editor" {
		t.Fatalf("durum yanlış: %+v", state.User)
	}
	var e map[string]string
	if got := c.do("GET", "/api/monitors", nil, &e); got != 403 || e["code"] != "password_change_required" {
		t.Fatalf("şifre değişmeden erişim olmamalı: %d %v", got, e)
	}
	c.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)
	c.mustDo("GET", "/api/monitors", nil, nil, 200)

	// Yönetici şifreyi sıfırlayınca oturum kapanır, yeni şifreyle tekrar zorunlu değişim.
	admin.mustDo("POST", fmt.Sprintf("/api/users/%d/password", u.ID), map[string]string{"password": "baska-gecici-1"}, nil, 200)
	c.mustDo("GET", "/api/monitors", nil, nil, 401)
	c2 := admin.loginAs("yeni", "baska-gecici-1")
	c2.mustDo("GET", "/api/monitors", nil, nil, 403)
}

func TestUserManagementGuards(t *testing.T) {
	admin := setupAdmin(t)
	var me struct{ User userView }
	admin.mustDo("GET", "/api/auth/state", nil, &me, 200)

	// Kendini devre dışı bırakma / rol düşürme / silme yok.
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", me.User.ID), map[string]any{"role": "admin", "disabled": true}, nil, 400)
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", me.User.ID), map[string]any{"role": "viewer"}, nil, 400)
	admin.mustDo("DELETE", fmt.Sprintf("/api/users/%d", me.User.ID), nil, nil, 400)

	// İkinci yönetici: ilk yönetici onu düşürebilir; ama son aktif yönetici korunur.
	second, u2 := admin.newUser("ikinci", store.RoleAdmin, nil)
	second.mustDo("PUT", fmt.Sprintf("/api/users/%d", me.User.ID), map[string]any{"role": "editor"}, nil, 200)
	admin.mustDo("GET", "/api/users", nil, nil, 403) // yetkisi anında düştü
	second.mustDo("DELETE", fmt.Sprintf("/api/users/%d", me.User.ID), nil, nil, 200)
	_ = u2

	// Aynı ad (büyük/küçük harf farkıyla) iki kez eklenemez.
	second.mustDo("POST", "/api/users", map[string]any{"username": "Musteri", "role": "viewer", "password": "gecici-sifre-1"}, nil, 201)
	second.mustDo("POST", "/api/users", map[string]any{"username": "musteri", "role": "viewer", "password": "gecici-sifre-1"}, nil, 409)
	// Olmayan monitöre kısıt verilemez; geçersiz rol reddedilir.
	second.mustDo("POST", "/api/users", map[string]any{"username": "m2", "role": "viewer", "password": "gecici-sifre-1", "all_monitors": false, "monitor_ids": []int64{999}}, nil, 400)
	second.mustDo("POST", "/api/users", map[string]any{"username": "m3", "role": "root", "password": "gecici-sifre-1"}, nil, 400)
}

func TestDisableKillsSessions(t *testing.T) {
	admin := setupAdmin(t)
	c, u := admin.newUser("gecici", store.RoleEditor, nil)
	c.mustDo("GET", "/api/monitors", nil, nil, 200)
	admin.mustDo("PUT", fmt.Sprintf("/api/users/%d", u.ID), map[string]any{"role": "editor", "disabled": true}, nil, 200)
	c.mustDo("GET", "/api/monitors", nil, nil, 401)
	c.mustDo("POST", "/api/auth/login", map[string]string{"username": "gecici", "password": "kalici-sifre-1"}, nil, 403)
}

func TestAuditLog(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("Kayıtlı")
	admin.mustDo("DELETE", fmt.Sprintf("/api/monitors/%d", m.ID), nil, nil, 200)
	admin.do("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "yanlis"}, nil)
	var log []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &log, 200)
	var actions []string
	for _, e := range log {
		actions = append(actions, e.Action)
	}
	got := strings.Join(actions, ",")
	for _, want := range []string{"login.fail", "monitor.delete", "monitor.create", "user.setup"} {
		if !strings.Contains(got, want) {
			t.Errorf("işlem kaydında %s yok: %s", want, got)
		}
	}
	if log[1].Action != "monitor.delete" || log[1].Username != "kadir" || log[1].TargetName != "Kayıtlı" {
		t.Errorf("kayıt içeriği yanlış: %+v", log[1])
	}
	// Süzgeçler: eylem alanı, tam eylem, kullanıcı, arama, tarih.
	admin.mustDo("GET", "/api/audit?action=monitor.", nil, &log, 200)
	if len(log) != 2 || log[0].Action != "monitor.delete" || log[1].Action != "monitor.create" {
		t.Errorf("alan süzgeci: %+v", log)
	}
	admin.mustDo("GET", "/api/audit?action=login.fail", nil, &log, 200)
	if len(log) != 1 {
		t.Errorf("tam eylem süzgeci: %+v", log)
	}
	admin.mustDo("GET", "/api/audit?action=monitor.&user=kadir", nil, &log, 200)
	if len(log) != 2 {
		t.Errorf("eylem+kullanıcı süzgeci: %+v", log)
	}
	admin.mustDo("GET", "/api/audit?user=yok", nil, &log, 200)
	if len(log) != 0 {
		t.Errorf("olmayan kullanıcı: %+v", log)
	}
	admin.mustDo("GET", "/api/audit?q=kay%C4%B1tl", nil, &log, 200)
	if len(log) != 2 {
		t.Errorf("hedef araması: %+v", log)
	}
	admin.mustDo("GET", fmt.Sprintf("/api/audit?from=%d", time.Now().Unix()+3600), nil, &log, 200)
	if len(log) != 0 {
		t.Errorf("gelecek tarih süzgeci boş dönmeli: %+v", log)
	}
	var facets struct {
		Users   []string `json:"users"`
		Actions []string `json:"actions"`
	}
	admin.mustDo("GET", "/api/audit/facets", nil, &facets, 200)
	if len(facets.Users) != 1 || facets.Users[0] != "kadir" || !slices.Contains(facets.Actions, "monitor.delete") {
		t.Errorf("yüzler: %+v", facets)
	}
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	viewer.mustDo("GET", "/api/audit/facets", nil, nil, 403)
}

// Güvenlik: editör, maskeli gizli bilgiyi hedefini değiştirerek dışarı sızdıramaz.
func TestSecretRebindBlocked(t *testing.T) {
	admin := setupAdmin(t)
	var ch store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Kanca", "type": "webhook", "config": map[string]any{"url": "https://kanca.kadir.app/x", "headers": "Authorization: Bearer gizli"},
	}, &ch, 201)
	editor, _ := admin.newUser("editor2", store.RoleEditor, nil)
	masked := map[string]any{"url": "https://saldirgan.example/topla", "headers": "••••••"}
	var e map[string]string
	if code := editor.do("POST", "/api/notifications/test", map[string]any{"id": ch.ID, "type": "webhook", "config": masked}, &e); code != 400 || !strings.Contains(e["error"], "yeniden girmeniz") {
		t.Errorf("test gönder: %d %v", code, e)
	}
	if code := editor.do("PUT", fmt.Sprintf("/api/notifications/%d", ch.ID), map[string]any{"name": "Kanca", "type": "webhook", "config": masked}, nil); code != 400 {
		t.Errorf("kanal düzenleme: %d", code)
	}
	// Monitör: adres değişip basic auth şifresi maskeli bırakılırsa ret.
	var m monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{"name": "Gizli", "type": "http", "config": map[string]any{
		"url": "https://ic.kadir.app", "basic_user": "a", "basic_pass": "cokgizli"}}, &m, 201)
	if code := editor.do("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Gizli", "type": "http", "config": map[string]any{
		"url": "https://saldirgan.example", "basic_user": "a", "basic_pass": "••••••"}}, nil); code != 400 {
		t.Errorf("monitör hedef değişimi: %d", code)
	}
	// Aynı adresle maskeli kaydetme çalışmaya devam eder.
	editor.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{"name": "Gizli 2", "type": "http", "config": map[string]any{
		"url": "https://ic.kadir.app", "basic_user": "a", "basic_pass": "••••••"}}, nil, 200)
}
