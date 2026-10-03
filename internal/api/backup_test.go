package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/backup"
	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
)

// Grup tipi bu dalda henüz yoksa içe aktarmadaki kimlik çevirisini sınamak için sahtesi.
type testGroupChecker struct{}

func (testGroupChecker) Normalize(c json.RawMessage) (json.RawMessage, error) { return c, nil }
func (testGroupChecker) Check(context.Context, json.RawMessage) check.Result {
	return check.Result{Up: true, PingMs: 1}
}
func (testGroupChecker) Target(json.RawMessage) string { return "Grup" }

func init() {
	if _, ok := check.Get("group"); !ok {
		check.Register("group", testGroupChecker{})
	}
}

// send ham gövdeli istek gönderir.
func (e *env) send(method, path, contentType string, body io.Reader) (int, []byte, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("X-Uptime", "1")
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, resp.Header
}

func (e *env) exportDoc() (backup.Doc, []byte) {
	e.t.Helper()
	code, data, h := e.send("GET", "/api/export", "", nil)
	if code != 200 {
		e.t.Fatalf("dışa aktarma: %d %s", code, data)
	}
	if cd := h.Get("Content-Disposition"); !regexp.MustCompile(`^attachment; filename="bekci-yedek-\d{8}\.json"$`).MatchString(cd) {
		e.t.Errorf("Content-Disposition: %q", cd)
	}
	var d backup.Doc
	if err := json.Unmarshal(data, &d); err != nil {
		e.t.Fatal(err)
	}
	if d.Format != backup.Format {
		e.t.Errorf("yedek biçimi %q, %q bekleniyordu", d.Format, backup.Format)
	}
	return d, data
}

func (e *env) importDoc(path string, doc any, want int) importSummary {
	e.t.Helper()
	b, _ := json.Marshal(doc)
	code, data, _ := e.send("POST", path, "application/json", bytes.NewReader(b))
	if code != want {
		e.t.Fatalf("POST %s: %d, %d bekleniyordu: %s", path, code, want, data)
	}
	var sum importSummary
	json.Unmarshal(data, &sum)
	return sum
}

func (e *env) monitors() map[string]monitorView {
	e.t.Helper()
	var list []monitorView
	e.mustDo("GET", "/api/monitors", nil, &list, 200)
	out := map[string]monitorView{}
	for _, m := range list {
		out[m.Name] = m
	}
	return out
}

func itemResults(sum importSummary) map[string]string {
	out := map[string]string{}
	for _, it := range sum.Items {
		out[it.Kind+":"+it.Name] = it.Result
	}
	return out
}

// seedBackupEnv yedeklenecek örnek veriyi kurar.
func seedBackupEnv(t *testing.T, e *env, hookURL string) {
	var n store.Notification
	e.mustDo("POST", "/api/notifications", map[string]any{"name": "Kanca", "type": "webhook", "is_default": true,
		"config": map[string]any{"url": hookURL, "headers": "Authorization: Bearer gizli-token"}}, &n, 201)
	var tag tagView
	e.mustDo("POST", "/api/tags", map[string]any{"name": "Ortam", "color": "#2563eb"}, &tag, 201)
	var site, db monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Site", "type": "http", "interval": 86400, "config": map[string]any{
		"url": "http://127.0.0.1:1/", "basic_user": "u", "basic_pass": "gizli-parola",
		"proxy_url": "http://pu:proxy-sifre@127.0.0.1:3128"}}, &site, 201)
	e.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", site.ID), nil, nil, 200)
	e.mustDo("PUT", fmt.Sprintf("/api/monitors/%d/tags", site.ID), []map[string]any{{"tag_id": tag.ID, "value": "canlı"}}, nil, 200)
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Cron", "type": "push", "interval": 86400, "config": map[string]any{}}, nil, 201)
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Veritabanı", "type": "tcp", "interval": 86400, "notification_ids": []int64{},
		"config": map[string]any{"host": "127.0.0.1", "port": 1}}, &db, 201)
	e.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", db.ID), nil, nil, 200)
}

var pngLogo = []byte("\x89PNG\r\n\x1a\nsahte-logo")

func TestBackupRoundTrip(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hook.Close()
	a := setupAdmin(t)
	seedBackupEnv(t, a, hook.URL)

	doc, raw := a.exportDoc()
	for _, secret := range []string{"gizli-parola", "proxy-sifre", "Bearer gizli-token"} {
		if !bytes.Contains(raw, []byte(secret)) {
			t.Errorf("yedekte gizli bilgi yok: %s", secret)
		}
	}
	if doc.Format != backup.Format || doc.Version != 1 || doc.Warning == "" || doc.Settings == nil ||
		len(doc.Monitors) != 3 || len(doc.Notifications) != 1 || len(doc.Tags) != 1 {
		t.Fatalf("yedek içeriği: %s", raw)
	}
	src := a.monitors()
	for _, m := range doc.Monitors {
		if m.Name == "Cron" && (m.PushToken == "" || m.PushToken != src["Cron"].PushToken) {
			t.Errorf("push token yedekte yok")
		}
		if m.Name == "Veritabanı" && len(m.Notifications) != 0 || m.Name == "Site" && (len(m.Notifications) != 1 || m.Tags[0].Value != "canlı" || m.Active) {
			t.Errorf("monitör yedeği: %+v", m)
		}
	}

	// Aynı kuruluma birleştirme: her şey zaten var, hiçbir şey eklenmez.
	sum := a.importDoc("/api/import", doc, 200)
	if sum.Created != (importCounts{}) || sum.Existing.Monitors != 3 || sum.Existing.Notifications != 1 || sum.Existing.Tags != 1 {
		t.Errorf("aynı kuruluma birleştirme: %+v", sum)
	}

	// Durum sayfası (bu dalda sayfa API'si yok; dosyaya eklenir).
	hash, _ := bcrypt.GenerateFromPassword([]byte("sayfa-sifre"), bcryptCost)
	var siteID, cronID int64
	for _, m := range doc.Monitors {
		switch m.Name {
		case "Site":
			siteID = m.ID
		case "Cron":
			cronID = m.ID
		}
	}
	doc.StatusPages = []backup.Page{{
		Slug: "durum", Title: "Kadir Durum", Description: "Hizmetler", Footer: "alt", Published: true, ShowTargets: true,
		PasswordHash: string(hash), CustomDomain: "Durum.Kadir.App", Logo: pngLogo, LogoType: "image/png",
		Sections: []backup.PageSection{{Title: "Web", Monitors: []backup.PageMonitor{{MonitorID: siteID, Name: "Web sitesi"}, {MonitorID: 999}}},
			{Title: "İşler", Monitors: []backup.PageMonitor{{MonitorID: cronID}}}},
		Announcements: []backup.Announcement{{Title: "Bakım", Body: "Pazar", Severity: "warning", StartsAt: 1700000000}},
	}}
	doc.Settings.RetentionRawDays = 30

	// Yeni kuruluma geri yükleme (birleştir).
	b := setupAdmin(t)
	sum = b.importDoc("/api/import", doc, 200)
	if sum.Created != (importCounts{3, 1, 1, 1, 0}) || sum.Skipped != (importCounts{}) || sum.SettingsApplied || sum.Mode != "merge" {
		t.Fatalf("geri yükleme özeti: %+v", sum)
	}
	if itemResults(sum)["settings:Uygulama ayarları"] != "skipped" {
		t.Errorf("birleştirmede ayarlar uygulanmamalı: %+v", sum.Items)
	}
	var pageMsgs []string
	for _, it := range sum.Items {
		if it.Kind == "status_page" {
			pageMsgs = it.Messages
		}
		if it.Kind == "monitor" && it.ID == 0 {
			t.Errorf("oluşan monitörün kimliği yanıtta yok: %+v", it)
		}
	}
	if len(pageMsgs) != 1 || !strings.Contains(pageMsgs[0], "1 monitör") {
		t.Errorf("sayfa uyarıları: %v", pageMsgs)
	}

	doc2, _ := b.exportDoc()
	byName := func(d backup.Doc) map[string]backup.Monitor {
		out := map[string]backup.Monitor{}
		for _, m := range d.Monitors {
			out[m.Name] = m
		}
		return out
	}
	m1, m2 := byName(doc), byName(doc2)
	for name, m := range m1 {
		n := m2[name]
		if string(n.Config) != string(m.Config) || n.PushToken != m.PushToken || n.Active != m.Active ||
			n.Interval != m.Interval || fmt.Sprint(n.Notifications) != fmt.Sprint(m.Notifications) || fmt.Sprint(n.Tags) != fmt.Sprint(m.Tags) {
			t.Errorf("geri yüklenen monitör farklı:\n%+v\n%+v", m, n)
		}
	}
	if string(doc2.Notifications[0].Config) != string(doc.Notifications[0].Config) || !doc2.Notifications[0].IsDefault {
		t.Errorf("bildirim gizli bilgisi korunmadı: %s", doc2.Notifications[0].Config)
	}
	if len(doc2.StatusPages) != 1 {
		t.Fatalf("durum sayfası geri yüklenmedi")
	}
	p := doc2.StatusPages[0]
	newSite := m2["Site"].ID
	if p.Slug != "durum" || p.CustomDomain != "durum.kadir.app" || p.PasswordHash != string(hash) || !bytes.Equal(p.Logo, pngLogo) ||
		len(p.Sections) != 2 || len(p.Sections[0].Monitors) != 1 || p.Sections[0].Monitors[0].MonitorID != newSite ||
		p.Sections[0].Monitors[0].Name != "Web sitesi" || len(p.Announcements) != 1 || p.Announcements[0].Severity != "warning" {
		t.Errorf("durum sayfası: %+v", p)
	}
	// Push adresi korunduğu için eski cron adresi çalışmaya devam eder.
	var pushRes map[string]any
	b.mustDo("GET", "/api/push/"+m1["Cron"].PushToken+"?status=up", nil, &pushRes, 200)

	// Değiştir: onay ister; deneme modunda hiçbir şey değişmez.
	b.mustDo("POST", "/api/monitors", map[string]any{"name": "Fazla", "type": "push", "interval": 86400, "config": map[string]any{}}, nil, 201)
	b.importDoc("/api/import?mode=replace", doc, 400)
	sum = b.importDoc("/api/import?mode=replace&dry_run=1", doc, 200)
	if !sum.DryRun || sum.Deleted == nil || *sum.Deleted != (importCounts{4, 1, 1, 1, 0}) || len(b.monitors()) != 4 {
		t.Fatalf("deneme modu: %+v", sum)
	}
	sum = b.importDoc("/api/import?mode=replace&confirm=yes", doc, 200)
	if sum.Created != (importCounts{3, 1, 1, 1, 0}) || !sum.SettingsApplied {
		t.Errorf("değiştir özeti: %+v", sum)
	}
	after := b.monitors()
	if len(after) != 3 || after["Fazla"].ID != 0 {
		t.Errorf("değiştir sonrası monitörler: %v", after)
	}
	var st store.AppSettings
	b.mustDo("GET", "/api/settings", nil, &st, 200)
	if st.RetentionRawDays != 30 {
		t.Errorf("ayarlar geri yüklenmedi: %+v", st)
	}
	b.mustDo("GET", "/api/push/"+m1["Cron"].PushToken+"?status=up", nil, nil, 200)

	var audit []store.AuditEntry
	b.mustDo("GET", "/api/audit", nil, &audit, 200)
	var imports, exports int
	for _, e := range audit {
		switch e.Action {
		case "backup.import":
			imports++
			if !strings.Contains(e.Detail, "kaynak=bekci") {
				t.Errorf("işlem kaydı ayrıntısı: %q", e.Detail)
			}
		case "backup.export":
			exports++
		}
	}
	if imports != 2 || exports != 1 { // deneme modu kayda geçmez
		t.Errorf("işlem kaydı: %d içe, %d dışa aktarma", imports, exports)
	}
}

func TestBackupPermissions(t *testing.T) {
	admin := setupAdmin(t)
	editor, _ := admin.newUser("editor", store.RoleEditor, nil)
	viewer, _ := admin.newUser("izleyici", store.RoleViewer, nil)
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/export"}, {"POST", "/api/import"}, {"POST", "/api/import/uptime-kuma"}, {"POST", "/api/import/uptimerobot"},
	} {
		for who, e := range map[string]*env{"editör": editor, "izleyici": viewer} {
			if code, _, _ := e.send(c.method, c.path, "application/json", strings.NewReader(`{}`)); code != 403 {
				t.Errorf("%s %s %s: %d", who, c.method, c.path, code)
			}
		}
	}
	// CSRF başlığı olmadan içe aktarma yapılamaz.
	req, _ := http.NewRequest("POST", admin.srv.URL+"/api/import", strings.NewReader(`{}`))
	resp, err := admin.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 403 {
		t.Errorf("CSRF başlıksız: %d", resp.StatusCode)
	}
}

func TestImportValidation(t *testing.T) {
	e := setupAdmin(t)
	for _, c := range []struct {
		path, body string
		want       int
		msg        string
	}{
		{"/api/import", `çöp`, 400, "JSON"},
		{"/api/import", `{"format":"baska","version":1}`, 400, brand.Name + " yedeği değil"},
		{"/api/import", `{"version":"1.23","monitorList":[]}`, 400, "Uptime Kuma"},
		{"/api/import", `{"format":"uptime-kadir","version":2}`, 400, "sürümü"},
		{"/api/import", `{"format":"bekci","version":2}`, 400, "sürümü"},
		{"/api/import?mode=hepsi", `{}`, 400, "Mod"},
		{"/api/import", ``, 400, "boş"},
	} {
		code, data, _ := e.send("POST", c.path, "application/json", strings.NewReader(c.body))
		if code != c.want || !strings.Contains(string(data), c.msg) {
			t.Errorf("%s %q: %d %s", c.path, c.body, code, data)
		}
	}
	// 20 MB sınırı.
	big := `{"format":"uptime-kadir","version":1,"x":"` + strings.Repeat("a", maxBackupBytes) + `"}`
	if code, data, _ := e.send("POST", "/api/import", "application/json", strings.NewReader(big)); code != 413 {
		t.Errorf("büyük dosya: %d %s", code, data)
	}

	doc := map[string]any{
		"format": "uptime-kadir", "version": 1,
		"tags": []map[string]any{{"name": "", "color": "#fff"}, {"name": "iyi", "color": "kırmızı"}},
		"notifications": []map[string]any{
			{"name": "Bilinmeyen", "type": "guvercin", "config": map[string]any{}},
			{"name": "Eksik", "type": "telegram", "config": map[string]any{"chat_id": "1"}},
		},
		"monitors": []map[string]any{
			{"id": 1, "name": "Sihirli", "type": "sihirli", "interval": 60, "config": map[string]any{}},
			{"id": 2, "name": "Hızlı", "type": "push", "interval": 5, "config": map[string]any{}},
			{"id": 3, "name": "P1", "type": "push", "interval": 86400, "config": map[string]any{}, "push_token": "ayni-token-123",
				"notifications": []string{"Yok"}, "tags": []map[string]any{{"name": "yeni-etiket", "value": "d"}}},
			{"id": 3, "name": "P2", "type": "push", "interval": 86400, "config": map[string]any{}, "push_token": "ayni-token-123"},
			{"id": 5, "name": "P3", "type": "push", "interval": 86400, "config": map[string]any{}, "push_token": "kötü/token"},
			{"id": 6, "name": "Grup", "type": "group", "interval": 86400, "active": true, "config": map[string]any{"monitor_ids": []int64{3, 5, 77}}},
		},
		"status_pages": []map[string]any{{"slug": "Kötü Adres", "title": "x"}, {"slug": "iyi", "title": "İyi", "password_hash": "düz-şifre", "published": true}},
	}
	sum := e.importDoc("/api/import", doc, 200)
	res := itemResults(sum)
	for k, want := range map[string]string{
		"tag:": "skipped", "tag:iyi": "created", "tag:yeni-etiket": "created",
		"notification:Bilinmeyen": "skipped", "notification:Eksik": "skipped",
		"monitor:Sihirli": "skipped", "monitor:Hızlı": "skipped", "monitor:P1": "created", "monitor:P2": "created",
		"monitor:P3": "created", "monitor:Grup": "created", "status_page:x": "skipped", "status_page:İyi": "created",
	} {
		if res[k] != want {
			t.Errorf("%s: %q, %q bekleniyordu", k, res[k], want)
		}
	}
	msgs := map[string]string{}
	for _, it := range sum.Items {
		msgs[it.Kind+":"+it.Name] = strings.Join(it.Messages, " | ")
	}
	for k, want := range map[string]string{
		"monitor:P1": "“Yok” bulunamadı", "monitor:P2": "başka bir monitörde", "monitor:P3": "kullanılamıyor",
		"monitor:Hızlı": "20 saniye", "notification:Bilinmeyen": "bu sürümde yok", "status_page:İyi": "yayından kaldırılmış",
	} {
		if !strings.Contains(msgs[k], want) {
			t.Errorf("%s mesajı %q, %q içermeli", k, msgs[k], want)
		}
	}
	mons := e.monitors()
	if mons["P1"].PushToken != "ayni-token-123" || mons["P2"].PushToken == "ayni-token-123" || mons["P3"].PushToken == "kötü/token" ||
		len(mons["P1"].Tags) != 1 || mons["P1"].Tags[0].Value != "d" {
		t.Errorf("push adresleri / etiketler: %+v %+v %+v", mons["P1"], mons["P2"].PushToken, mons["P3"].PushToken)
	}
	// Grup alt monitörleri yeni kimliklere çevrildi; P2 aynı dosya kimliğini taşıdığı için bağlanamaz, 77 yok.
	var gcfg struct {
		MonitorIDs []int64 `json:"monitor_ids"`
	}
	json.Unmarshal(mons["Grup"].Config, &gcfg)
	if fmt.Sprint(gcfg.MonitorIDs) != fmt.Sprint([]int64{mons["P1"].ID, mons["P3"].ID}) {
		t.Errorf("grup alt monitörleri: %v (P1=%d P3=%d)", gcfg.MonitorIDs, mons["P1"].ID, mons["P3"].ID)
	}

	// multipart ile yükleme de kabul edilir; aynı dosya ikinci kez eklenmez.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "yedek.json")
	json.NewEncoder(fw).Encode(doc)
	mw.Close()
	code, data, _ := e.send("POST", "/api/import", mw.FormDataContentType(), &buf)
	json.Unmarshal(data, &sum)
	if code != 200 || sum.Created.Monitors != 0 || sum.Existing.Monitors != 4 {
		t.Errorf("multipart tekrar: %d %+v", code, sum)
	}
}

func TestRemapMonitorRefs(t *testing.T) {
	out, ok := remapMonitorRefs("group", json.RawMessage(`{"mode":"all_down","monitor_ids":[1,2,3]}`), map[int64]int64{1: 10, 3: 30})
	if !ok || string(out) != `{"mode":"all_down","monitor_ids":[10,30]}` {
		t.Errorf("çeviri: %s", out)
	}
	if _, ok := remapMonitorRefs("http", json.RawMessage(`{"monitor_ids":[1]}`), nil); ok {
		t.Error("grup olmayan tip değiştirildi")
	}
}

func TestReadUploadLimit(t *testing.T) {
	r := httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 11)))
	if err := readUpload(r, 10, io.Discard); err != errUploadTooLarge {
		t.Errorf("sınır aşımı: %v", err)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(strings.Repeat("x", 10)))
	if err := readUpload(r, 10, io.Discard); err != nil {
		t.Errorf("sınırda: %v", err)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("baska", "deger")
	mw.Close()
	r = httptest.NewRequest("POST", "/", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	if err := readUpload(r, 10, io.Discard); err == nil || !strings.Contains(err.Error(), "file") {
		t.Errorf("dosyasız multipart: %v", err)
	}
}

const apiKumaJSON = `{
 "version": "1.23.16",
 "notificationList": [
  {"id": 1, "name": "Kanca", "config": "{\"type\":\"webhook\",\"webhookURL\":\"%s\"}", "active": 1, "isDefault": true},
  {"id": 2, "name": "Garip", "config": "{\"type\":\"Feishu\"}", "active": 1}
 ],
 "monitorList": [
  {"id": 11, "name": "Site", "type": "http", "url": "http://127.0.0.1:1/", "interval": 86400, "active": false,
   "notificationIDList": {"1": true}, "tags": [{"tag_id": 4, "name": "canlı", "color": "#059669", "value": ""}]},
  {"id": 12, "name": "Cron", "type": "push", "pushToken": "KumaPushToken42", "interval": 86400, "active": true},
  {"id": 13, "name": "Oyun", "type": "steam", "interval": 60, "active": true}
 ]
}`

func TestImportUptimeKuma(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hook.Close()
	e := setupAdmin(t)
	body := fmt.Sprintf(apiKumaJSON, hook.URL)

	code, data, _ := e.send("POST", "/api/import/uptime-kuma?dry_run=1", "application/json", strings.NewReader(body))
	var sum importSummary
	json.Unmarshal(data, &sum)
	if code != 200 || !sum.DryRun || sum.Created.Monitors != 2 || len(e.monitors()) != 0 {
		t.Fatalf("deneme: %d %s", code, data)
	}
	code, data, _ = e.send("POST", "/api/import/uptime-kuma", "application/json", strings.NewReader(body))
	json.Unmarshal(data, &sum)
	if code != 200 || sum.Source != "uptime-kuma" || sum.Created != (importCounts{2, 1, 1, 0, 0}) || sum.Skipped.Monitors != 1 || sum.Skipped.Notifications != 1 {
		t.Fatalf("Kuma JSON: %d %s", code, data)
	}
	mons := e.monitors()
	if mons["Site"].Active || len(mons["Site"].NotificationIDs) != 1 || len(mons["Site"].Tags) != 1 || mons["Cron"].PushToken != "KumaPushToken42" {
		t.Errorf("Kuma monitörleri: %+v", mons)
	}
	// Kuma'daki push adresi (yalnızca sunucu adı değişerek) çalışmaya devam eder.
	e.mustDo("GET", "/api/push/KumaPushToken42?status=up&msg=OK&ping=", nil, nil, 200)

	// kuma.db (multipart): aynı monitörler zaten var, yenisi eklenir.
	path := filepath.Join(t.TempDir(), "kuma.db")
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`CREATE TABLE monitor (id INTEGER PRIMARY KEY, name TEXT, type TEXT, url TEXT, hostname TEXT, port INTEGER, interval INTEGER, active BOOLEAN, push_token TEXT)`,
		`INSERT INTO monitor VALUES (12, 'Cron', 'push', NULL, NULL, NULL, 86400, 1, 'KumaPushToken42'), (14, 'SSH', 'port', NULL, '127.0.0.1', 1, 86400, 0, NULL)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	file, _ := os.ReadFile(path)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "kuma.db")
	fw.Write(file)
	mw.Close()
	code, data, _ = e.send("POST", "/api/import/uptime-kuma", mw.FormDataContentType(), &buf)
	json.Unmarshal(data, &sum)
	if code != 200 || sum.Created.Monitors != 1 || sum.Existing.Monitors != 1 {
		t.Fatalf("kuma.db: %d %s", code, data)
	}
	if m := e.monitors()["SSH"]; m.Type != "tcp" || m.Active {
		t.Errorf("SSH: %+v", m)
	}

	for _, bad := range []string{`{"a":1}`, "düz metin", `SQLite format 3` + "\x00bozuk"} {
		if code, data, _ := e.send("POST", "/api/import/uptime-kuma", "application/octet-stream", strings.NewReader(bad)); code != 400 {
			t.Errorf("geçersiz Kuma dosyası %q: %d %s", bad, code, data)
		}
	}
}

func TestImportUptimeRobot(t *testing.T) {
	ur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("api_key") != "ur1234-salt-okunur" {
			fmt.Fprint(w, `{"stat":"fail","error":{"type":"invalid_parameter","message":"api_key is invalid."}}`)
			return
		}
		fmt.Fprint(w, `{"stat":"ok","pagination":{"offset":0,"limit":50,"total":3},"monitors":[
			{"id":1,"friendly_name":"Site","url":"http://127.0.0.1:1/","type":1,"interval":300,"status":0},
			{"id":2,"friendly_name":"Kalp atışı","url":"https://heartbeat.uptimerobot.com/x","type":5,"interval":3600,"status":2},
			{"id":3,"friendly_name":"Port","url":"127.0.0.1","type":4,"sub_type":99,"port":"1","interval":300,"status":0}]}`)
	}))
	defer ur.Close()
	old := backup.UptimeRobotAPI
	backup.UptimeRobotAPI = ur.URL + "/v2"
	defer func() { backup.UptimeRobotAPI = old }()

	e := setupAdmin(t)
	sum := e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": "ur1234-salt-okunur"}, 200)
	if sum.Source != "uptimerobot" || sum.Created.Monitors != 3 {
		t.Fatalf("UptimeRobot: %+v", sum)
	}
	mons := e.monitors()
	if mons["Site"].Active || mons["Port"].Active || !mons["Kalp atışı"].Active || mons["Kalp atışı"].PushToken == "" || mons["Kalp atışı"].Interval != 3600 {
		t.Errorf("UptimeRobot monitörleri: %+v", mons)
	}
	// Tekrar: kopya eklenmez.
	sum = e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": "ur1234-salt-okunur"}, 200)
	if sum.Created.Monitors != 0 || sum.Existing.Monitors != 3 {
		t.Errorf("tekrar içe aktarma: %+v", sum)
	}
	e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": "yanlis"}, 400)
	e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": ""}, 400)
	e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": "a b"}, 400)
	backup.UptimeRobotAPI = "http://127.0.0.1:1/v2"
	e.importDoc("/api/import/uptimerobot", map[string]string{"api_key": "ur1234-salt-okunur"}, 422)
}
