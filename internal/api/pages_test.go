package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// pageEnv yönetici oturumlu test ortamı ve sunucunun kendisi (veritabanına
// doğrudan veri eklemek ve saati ilerletmek için).
type pageEnv struct {
	*env
	s     *Server
	clock *atomic.Int64 // unix nano
}

func setupPages(t *testing.T) *pageEnv {
	t.Helper()
	pe := &pageEnv{clock: &atomic.Int64{}}
	pe.clock.Store(time.Now().UnixNano())
	pe.env = newEnv(t, func(s *Server) {
		pe.s = s
		s.BaseURL = "https://uptime.test/"
		s.now = func() time.Time { return time.Unix(0, pe.clock.Load()) }
	})
	pe.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return pe
}

func (pe *pageEnv) advance(d time.Duration) { pe.clock.Add(int64(d)) }

// seedMonitor motoru çalıştırmadan doğrudan veritabanına monitör ekler.
func (pe *pageEnv) seedMonitor(name, url string) store.Monitor {
	pe.t.Helper()
	m := store.Monitor{Name: name, Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30,
		Config: json.RawMessage(fmt.Sprintf(`{"url":%q}`, url))}
	if err := pe.s.store.CreateMonitor(context.Background(), &m, nil); err != nil {
		pe.t.Fatal(err)
	}
	return m
}

func (pe *pageEnv) beat(id int64, t int64, status int) {
	pe.t.Helper()
	if err := pe.s.store.RecordBeat(context.Background(), store.BeatUpdate{Beat: store.Beat{MonitorID: id, Time: t, Status: status, PingMs: 5}}); err != nil {
		pe.t.Fatal(err)
	}
}

// anon giriş yapmamış, ayrı çerez kavanozlu ziyaretçi.
func (e *env) anon() *env {
	jar, _ := cookiejar.New(nil)
	return &env{t: e.t, srv: e.srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

// raw ham gövdeli istek; host boş değilse Host başlığı olarak gönderilir.
func (e *env) raw(method, path, host, contentType string, body []byte) (*http.Response, []byte) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if host != "" {
		req.Host = host
		// Başka Host'la giden istekte oturum çerezi de açıkça eklenir: özel alan
		// adı korumasının geçerli oturuma rağmen çalıştığı böyle sınanır.
		if e.client.Jar != nil {
			for _, c := range e.client.Jar.Cookies(req.URL) {
				req.AddCookie(c)
			}
		}
	}
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
	return resp, data
}

type pageResp struct {
	store.StatusPage
	PasswordHash *string `json:"password_hash"`
}

func TestStatusPageCRUDAndValidation(t *testing.T) {
	pe := setupPages(t)
	a, b := pe.seedMonitor("A", "https://a.example.com"), pe.seedMonitor("B", "https://b.example.com")

	body := map[string]any{
		"slug": "genel", "title": " Genel durum ", "description": "Açıklama", "footer": "Alt bilgi",
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID, "name": " Ana site "}, {"id": b.ID}}}},
		"password": "gizli",
	}
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", body, &p, 201)
	if p.Slug != "genel" || p.Title != "Genel durum" || !p.Published || p.ShowTargets || !p.HasPassword || p.PasswordHash != nil ||
		len(p.Sections) != 1 || p.Sections[0].Monitors[0].Name != "Ana site" || p.CustomDomain != "" {
		t.Fatalf("sayfa yanlış: %+v", p)
	}
	var raw map[string]any
	pe.mustDo("GET", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, &raw, 200)
	if _, ok := raw["password_hash"]; ok {
		t.Fatal("şifre özeti yanıtta olmamalı")
	}

	// Doğrulama hataları.
	bad := []map[string]any{
		{"slug": "-genel", "title": "x"},
		{"slug": "Geçersiz ad", "title": "x"},
		{"slug": strings.Repeat("a", 51), "title": "x"},
		{"slug": "x2", "title": ""},
		{"slug": "x2", "title": strings.Repeat("ş", 101)},
		{"slug": "x2", "title": "x", "description": strings.Repeat("a", 1001)},
		{"slug": "x2", "title": "x", "footer": strings.Repeat("a", 501)},
		{"slug": "x2", "title": "x", "sections": make([]map[string]any, 21)},
		{"slug": "x2", "title": "x", "sections": []map[string]any{{"title": strings.Repeat("a", 101)}}},
		{"slug": "x2", "title": "x", "sections": []map[string]any{{"monitors": []map[string]any{{"id": a.ID, "name": strings.Repeat("a", 101)}}}}},
		{"slug": "x2", "title": "x", "sections": []map[string]any{{"monitors": []map[string]any{{"id": a.ID}}}, {"monitors": []map[string]any{{"id": a.ID}}}}},
		{"slug": "x2", "title": "x", "sections": []map[string]any{{"monitors": []map[string]any{{"id": 9999}}}}},
		{"slug": "x2", "title": "x", "custom_domain": "https://durum.example.com/"},
		{"slug": "x2", "title": "x", "custom_domain": "localhost"},
		{"slug": "x2", "title": "x", "custom_domain": "10.0.0.1"},
		{"slug": "x2", "title": "x", "custom_domain": "-a.example.com"},
		{"slug": "x2", "title": "x", "custom_domain": "UPTIME.test"}, // uygulamanın kendi adresi
		{"slug": "x2", "title": "x", "password": "abc"},
		{"slug": "x2", "title": "x", "password": strings.Repeat("a", 73)},
		{"slug": "x2", "title": "x", "bilinmeyen": true},
	}
	for i, c := range bad {
		if code := pe.do("POST", "/api/status-pages", c, nil); code != 400 {
			t.Errorf("geçersiz girdi %d kabul edildi: %d", i, code)
		}
	}
	many := make([]map[string]any, 201)
	for i := range many {
		many[i] = map[string]any{"id": i + 1}
	}
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "x2", "title": "x", "sections": []map[string]any{{"monitors": many}}}, nil, 400)

	// Benzersizlik: kısa ad (büyük harfle de) ve alan adı.
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "GENEL", "title": "x"}, nil, 409)
	var p2 pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "iki", "title": "İki", "custom_domain": "Durum.Example.com.", "published": false, "show_targets": true}, &p2, 201)
	if p2.CustomDomain != "durum.example.com" || p2.Published || !p2.ShowTargets || p2.HasPassword {
		t.Fatalf("ikinci sayfa yanlış: %+v", p2)
	}
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "uc", "title": "x", "custom_domain": "durum.example.com"}, nil, 409)
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "iki", "title": "x"}, nil, 409)
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "genel", "title": "x", "custom_domain": "durum.example.com"}, nil, 409)

	// PUT: şifre yoksa korunur; bayraklar verilmezse korunur.
	upd := map[string]any{"slug": "genel-yeni", "title": "Yeni", "sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": b.ID}}}}}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd, &p, 200)
	if !p.HasPassword || p.Slug != "genel-yeni" || !p.Published || len(p.Sections[0].Monitors) != 1 {
		t.Fatalf("güncelleme yanlış: %+v", p)
	}
	upd["password"] = nil
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd, &p, 200)
	if !p.HasPassword {
		t.Fatal("null şifre mevcut şifreyi korumalı")
	}
	upd["password"] = ""
	upd["published"] = false
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd, &p, 200)
	if p.HasPassword || p.Published {
		t.Fatalf("boş şifre kaldırmalı, yayın kapanmalı: %+v", p)
	}

	// Silinen monitör yönetim görünümünden düşer; sayfa yeniden kaydedilebilir.
	pe.mustDo("DELETE", fmt.Sprintf("/api/monitors/%d", b.ID), nil, nil, 200)
	pe.mustDo("GET", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, &p, 200)
	if len(p.Sections) != 1 || len(p.Sections[0].Monitors) != 0 {
		t.Fatalf("silinen monitör görünmemeli: %+v", p.Sections)
	}

	var list []pageResp
	pe.mustDo("GET", "/api/status-pages", nil, &list, 200)
	if len(list) != 2 {
		t.Fatalf("2 sayfa bekleniyordu: %+v", list)
	}
	pe.mustDo("DELETE", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, nil, 200)
	pe.mustDo("GET", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, nil, 404)
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), upd, nil, 404)
	pe.mustDo("DELETE", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, nil, 404)

	var log []store.AuditEntry
	pe.mustDo("GET", "/api/audit", nil, &log, 200)
	var actions []string
	for _, e := range log {
		actions = append(actions, e.Action)
	}
	got := strings.Join(actions, ",")
	for _, want := range []string{"status_page.create", "status_page.update", "status_page.delete"} {
		if !strings.Contains(got, want) {
			t.Errorf("işlem kaydında %s yok: %s", want, got)
		}
	}
}

func TestStatusPagePermissions(t *testing.T) {
	pe := setupPages(t)
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel"}, &p, 201)
	var ann store.Announcement
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Bakım"}, &ann, 201)
	editor, _ := pe.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := pe.newUser("viewer1", store.RoleViewer, nil)
	anon := pe.anon()

	pid, aid := p.ID, ann.ID
	cases := []struct {
		method, path string
		body         any
		editor       int
	}{
		{"GET", "/api/status-pages", nil, 200},
		{"GET", fmt.Sprintf("/api/status-pages/%d", pid), nil, 200},
		{"GET", fmt.Sprintf("/api/status-pages/%d/preview", pid), nil, 200},
		{"PUT", fmt.Sprintf("/api/status-pages/%d", pid), map[string]any{"slug": "genel", "title": "Editör"}, 200},
		{"GET", fmt.Sprintf("/api/status-pages/%d/announcements", pid), nil, 200},
		{"POST", fmt.Sprintf("/api/status-pages/%d/announcements", pid), map[string]any{"title": "Yeni"}, 201},
		{"PUT", fmt.Sprintf("/api/announcements/%d", aid), map[string]any{"title": "Düzeltme"}, 200},
		{"DELETE", fmt.Sprintf("/api/status-pages/%d/logo", pid), nil, 200},
		{"GET", fmt.Sprintf("/api/status-pages/%d/logo", pid), nil, 404},
		{"DELETE", fmt.Sprintf("/api/announcements/%d", aid), nil, 200},
		{"POST", "/api/status-pages", map[string]any{"slug": "editor", "title": "E"}, 201},
		{"DELETE", fmt.Sprintf("/api/status-pages/%d", pid), nil, 200},
	}
	// Önce izleyici ve ziyaretçi (hiçbir şey değişmemeli), sonra editör.
	for _, c := range cases {
		if got := viewer.do(c.method, c.path, c.body, nil); got != 403 {
			t.Errorf("izleyici %s %s: %d, 403 bekleniyordu", c.method, c.path, got)
		}
		if got := anon.do(c.method, c.path, c.body, nil); got != 401 {
			t.Errorf("ziyaretçi %s %s: %d, 401 bekleniyordu", c.method, c.path, got)
		}
	}
	if resp, _ := viewer.raw("PUT", fmt.Sprintf("/api/status-pages/%d/logo", pid), "", "image/png", pngData(100)); resp.StatusCode != 403 {
		t.Errorf("izleyici logo yükleyememeli: %d", resp.StatusCode)
	}
	for _, c := range cases {
		if got := editor.do(c.method, c.path, c.body, nil); got != c.editor {
			t.Errorf("editör %s %s: %d, %d bekleniyordu", c.method, c.path, got, c.editor)
		}
	}
}

// pngData PNG imzasıyla başlayan n baytlık veri.
func pngData(n int) []byte {
	d := make([]byte, n)
	copy(d, "\x89PNG\x0D\x0A\x1A\x0A\x00\x00\x00\x0DIHDR")
	return d
}

func TestPageLogo(t *testing.T) {
	pe := setupPages(t)
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel"}, &p, 201)
	logoPath := fmt.Sprintf("/api/status-pages/%d/logo", p.ID)
	anon := pe.anon()

	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	jpeg := append([]byte("\xFF\xD8\xFF\xE0"), make([]byte, 100)...)
	rejects := []struct {
		name, ctype string
		body        []byte
		want        int
	}{
		{"svg türü", "image/svg+xml", svg, 415},
		{"png diye svg", "image/png", svg, 415},
		{"jpeg diye png", "image/jpeg", pngData(100), 415},
		{"tür yok", "", pngData(100), 415},
		{"boş", "image/png", nil, 400},
		{"çok büyük", "image/png", pngData(maxLogoBytes + 1), 413},
	}
	for _, c := range rejects {
		if resp, data := pe.raw("PUT", logoPath, "", c.ctype, c.body); resp.StatusCode != c.want {
			t.Errorf("%s: %d, %d bekleniyordu: %s", c.name, resp.StatusCode, c.want, data)
		}
	}
	// CSRF başlığı olmadan yükleme yok.
	req, _ := http.NewRequest("PUT", pe.srv.URL+logoPath, bytes.NewReader(pngData(100)))
	req.Header.Set("Content-Type", "image/png")
	if resp, err := pe.client.Do(req); err != nil || resp.StatusCode != 403 {
		t.Errorf("X-Uptime olmadan 403 bekleniyordu: %v", resp.StatusCode)
	}
	if resp, _ := anon.raw("GET", "/api/public/pages/genel/logo", "", "", nil); resp.StatusCode != 404 {
		t.Errorf("logo yokken 404 bekleniyordu: %d", resp.StatusCode)
	}

	resp, data := pe.raw("PUT", logoPath, "", "image/jpeg", jpeg)
	if resp.StatusCode != 200 {
		t.Fatalf("jpeg yüklenemedi: %d %s", resp.StatusCode, data)
	}
	resp, data = pe.raw("PUT", logoPath, "", "image/png; charset=binary", pngData(maxLogoBytes))
	if resp.StatusCode != 200 || !strings.Contains(string(data), `"has_logo":true`) {
		t.Fatalf("png yüklenemedi: %d %s", resp.StatusCode, data)
	}

	var pub struct {
		HasLogo bool    `json:"has_logo"`
		LogoURL *string `json:"logo_url"`
	}
	anon.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
	if !pub.HasLogo || pub.LogoURL == nil || !strings.HasPrefix(*pub.LogoURL, "/api/public/pages/genel/logo?v=") {
		t.Fatalf("herkese açık sayfada logo yok: %+v", pub)
	}
	resp, data = anon.raw("GET", *pub.LogoURL, "", "", nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" || len(data) != maxLogoBytes ||
		resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Cache-Control") != "public, max-age=3600" {
		t.Fatalf("logo yanıtı yanlış: %d %v %d", resp.StatusCode, resp.Header, len(data))
	}
	if resp, _ := pe.raw("GET", logoPath, "", "", nil); resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Errorf("yönetim logo önizlemesi: %d", resp.StatusCode)
	}

	pe.mustDo("DELETE", logoPath, nil, &p, 200)
	if p.HasLogo {
		t.Fatal("logo kaldırılmalıydı")
	}
	if resp, _ := anon.raw("GET", "/api/public/pages/genel/logo", "", "", nil); resp.StatusCode != 404 {
		t.Errorf("kaldırılan logo 404 olmalı: %d", resp.StatusCode)
	}
	pe.mustDo("PUT", "/api/status-pages/9999/logo", nil, nil, 404)
}

// publicPayload herkese açık sayfa yanıtının tamamı (alan denetimi için).
type publicPayload struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updated_at"`
	Sections  []struct {
		Title    string           `json:"title"`
		Monitors []map[string]any `json:"monitors"`
	} `json:"sections"`
	Announcements []map[string]any `json:"announcements"`
	Incidents     []map[string]any `json:"incidents"`
}

func keysOf(m map[string]any) string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func TestPublicStatusPage(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	a := pe.seedMonitor("Dahili API", "https://kullanici:parola@10.0.0.5:8443/saglik?token=gizli#x")
	b := pe.seedMonitor("B", "https://b.example.com")
	other := pe.seedMonitor("Sayfada değil", "https://gizli.example.com")
	now := pe.s.now().Unix()
	yesterday := st.DayStarts(now, 2)[0] + 12*3600
	pe.beat(a.ID, yesterday, store.StatusUp)
	pe.beat(a.ID, yesterday+60, store.StatusUp)
	pe.beat(a.ID, yesterday+120, store.StatusUp)
	pe.beat(a.ID, now, store.StatusDown)
	pe.beat(b.ID, now, store.StatusUp)
	pe.beat(other.ID, now, store.StatusDown)
	st.OpenIncident(ctx, a.ID, now-3600, "10.0.0.5:8443 bağlantı reddedildi")
	st.OpenIncident(ctx, b.ID, now-20*86400, "eski")
	st.ResolveIncident(ctx, b.ID, now-20*86400+60)
	st.OpenIncident(ctx, other.ID, now-60, "başka monitörün nedeni")

	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "genel", "title": "Genel", "bar_range": "90d",
		"sections": []map[string]any{
			{"title": "Servisler", "monitors": []map[string]any{{"id": a.ID, "name": "API"}, {"id": b.ID}}},
			{"title": "Boş"},
		},
	}, &p, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Bakım", "body": "Gece bakım", "severity": "warning"}, nil, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Gelecek", "starts_at": now + 3600}, nil, 201)
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Bitmiş", "starts_at": now - 7200, "ends_at": now - 3600}, nil, 201)

	anon := pe.anon()
	resp, rawBody := anon.raw("GET", "/api/public/pages/genel", "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("sayfa açılmadı: %d %s", resp.StatusCode, rawBody)
	}
	text := string(rawBody)
	for _, secret := range []string{"10.0.0.5", "parola", "gizli", "bağlantı reddedildi", "Sayfada değil", "başka monitörün", "Dahili API", "cause", "push_token", "config"} {
		if strings.Contains(text, secret) {
			t.Errorf("herkese açık yanıtta %q olmamalı: %s", secret, text)
		}
	}
	var pub publicPayload
	json.Unmarshal(rawBody, &pub)
	if pub.Slug != "genel" || pub.Title != "Genel" || pub.Status != "partial" || pub.UpdatedAt != now || len(pub.Sections) != 2 {
		t.Fatalf("sayfa özeti yanlış: %+v", pub)
	}
	mons := pub.Sections[0].Monitors
	if len(mons) != 2 || len(pub.Sections[1].Monitors) != 0 {
		t.Fatalf("monitörler yanlış: %+v", pub.Sections)
	}
	for _, m := range mons {
		if k := keysOf(m); k != "bars,name,status,uptime,uptime_90d" {
			t.Errorf("monitörde beklenmeyen alanlar: %s", k)
		}
	}
	if mons[0]["name"] != "API" || mons[0]["status"] != "down" || mons[1]["name"] != "B" || mons[1]["status"] != "up" {
		t.Errorf("ad/durum yanlış: %+v", mons)
	}
	if up := mons[0]["uptime_90d"].(float64); up != 75 || mons[0]["uptime"].(float64) != 75 {
		t.Errorf("uptime %v, 75 bekleniyordu", up)
	}
	bars := mons[0]["bars"].([]any)
	if len(bars) != 90 {
		t.Fatalf("90 gün bekleniyordu: %d", len(bars))
	}
	last, prev := bars[89].(map[string]any), bars[88].(map[string]any)
	if last["down"].(float64) != 1 || prev["up"].(float64) != 3 || keysOf(last) != "down,t,up" ||
		bars[0].(map[string]any)["up"].(float64) != 0 || last["t"].(float64) <= prev["t"].(float64) {
		t.Errorf("günlük çubuklar yanlış: %v %v", prev, last)
	}
	if len(pub.Announcements) != 1 || pub.Announcements[0]["title"] != "Bakım" || pub.Announcements[0]["severity"] != "warning" ||
		keysOf(pub.Announcements[0]) != "body,ends_at,id,severity,starts_at,title" {
		t.Errorf("duyurular yanlış: %+v", pub.Announcements)
	}
	// Olaylar: sadece sayfadaki monitörler, son 14 gün; görünen adla, nedensiz.
	if len(pub.Incidents) != 1 || pub.Incidents[0]["monitor"] != "API" || pub.Incidents[0]["resolved_at"].(float64) != 0 ||
		keysOf(pub.Incidents[0]) != "monitor,resolved_at,started_at" {
		t.Errorf("olaylar yanlış: %+v", pub.Incidents)
	}

	// Hedefleri göster: kullanıcı adı/şifre ve sorgu atılır.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{
		"slug": "genel", "title": "Genel", "show_targets": true,
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": a.ID, "name": "API"}}}},
	}, nil, 200)
	anon.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
	if tg := pub.Sections[0].Monitors[0]["target"]; tg != "https://10.0.0.5:8443/saglik" {
		t.Errorf("hedef temizlenmeli: %v", tg)
	}
	if pub.Status != "down" {
		t.Errorf("tek monitör kapalıyken genel durum down olmalı: %s", pub.Status)
	}

	// Yayında olmayan ve olmayan sayfa 404; önizleme editöre açık.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "genel", "title": "Genel", "published": false}, nil, 200)
	anon.mustDo("GET", "/api/public/pages/genel", nil, nil, 404)
	anon.mustDo("GET", "/api/public/pages/yok", nil, nil, 404)
	anon.mustDo("GET", "/api/public/pages/..%2Fstatus-pages", nil, nil, 404)
	anon.mustDo("POST", "/api/public/pages/genel/unlock", map[string]string{"password": "x"}, nil, 404)
	var prev2 publicPayload
	pe.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/preview", p.ID), nil, &prev2, 200)
	if prev2.Title != "Genel" || prev2.Status != "unknown" {
		t.Errorf("önizleme yanlış: %+v", prev2)
	}
}

func TestOverallStatus(t *testing.T) {
	cases := map[string][]string{
		"unknown": {}, "up": {"up", "paused"}, "down": {"down", "paused"}, "partial": {"up", "down"},
	}
	cases["unknown"] = append(cases["unknown"], "paused", "pending")
	for want, in := range cases {
		if got := overallStatus(in); got != want {
			t.Errorf("%v: %s, %s bekleniyordu", in, got, want)
		}
	}
	if got := overallStatus([]string{"down", "pending"}); got != "partial" {
		t.Errorf("kapalı + bekleyen partial olmalı: %s", got)
	}
}

func TestPublicPagePassword(t *testing.T) {
	pe := setupPages(t)
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "ozel", "title": "Özel sayfa", "password": "ilk-sifre"}, &p, 201)
	visitor := pe.anon()

	var locked map[string]any
	if code := visitor.do("GET", "/api/public/pages/ozel", nil, &locked); code != 401 || locked["password_required"] != true ||
		locked["title"] != "Özel sayfa" || locked["sections"] != nil {
		t.Fatalf("şifre istenmeliydi: %d %v", code, locked)
	}
	visitor.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "yanlis"}, nil, 401)
	// CSRF başlığı olmadan şifre denemesi yapılamaz.
	req, _ := http.NewRequest("POST", pe.srv.URL+"/api/public/pages/ozel/unlock", strings.NewReader(`{"password":"ilk-sifre"}`))
	if resp, _ := visitor.client.Do(req); resp.StatusCode != 403 {
		t.Errorf("X-Uptime olmadan 403 bekleniyordu: %d", resp.StatusCode)
	}

	resp, _ := visitor.raw("POST", "/api/public/pages/ozel/unlock", "", "application/json", []byte(`{"password":"ilk-sifre"}`))
	if resp.StatusCode != 200 {
		t.Fatalf("doğru şifre kabul edilmedi: %d", resp.StatusCode)
	}
	var c *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == pageCookie {
			c = ck
		}
	}
	if c == nil || !c.HttpOnly || c.Path != "/api/public/pages/ozel" || c.SameSite != http.SameSiteLaxMode || c.MaxAge < 29*86400 {
		t.Fatalf("çerez yanlış: %+v", c)
	}
	var pub publicPayload
	visitor.mustDo("GET", "/api/public/pages/ozel", nil, &pub, 200)
	if pub.Title != "Özel sayfa" {
		t.Fatalf("açılan sayfa yanlış: %+v", pub)
	}
	// Başka bir ziyaretçi çerezsiz göremez; uydurma çerez işe yaramaz.
	other := pe.anon()
	other.mustDo("GET", "/api/public/pages/ozel", nil, nil, 401)
	req, _ = http.NewRequest("GET", pe.srv.URL+"/api/public/pages/ozel", nil)
	req.AddCookie(&http.Cookie{Name: pageCookie, Value: "uydurma"})
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != 401 {
		t.Errorf("uydurma çerez kabul edildi: %d", resp.StatusCode)
	}

	// Şifre değişince eski çerez geçersiz olur.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "ozel", "title": "Özel sayfa", "password": "ikinci-sifre"}, nil, 200)
	visitor.mustDo("GET", "/api/public/pages/ozel", nil, nil, 401)
	visitor.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "ilk-sifre"}, nil, 401)
	visitor.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "ikinci-sifre"}, nil, 200)
	visitor.mustDo("GET", "/api/public/pages/ozel", nil, nil, 200)

	// Şifre kaldırılınca herkes görür; açma isteği zararsızca başarılı olur.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "ozel", "title": "Özel sayfa", "password": ""}, nil, 200)
	other.mustDo("GET", "/api/public/pages/ozel", nil, nil, 200)
	other.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "herhangi"}, nil, 200)

	// Deneme sınırı: aynı IP'den 5 hatalı denemeden sonra doğru şifre de beklemeli.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "ozel", "title": "Özel sayfa", "password": "ucuncu-sifre"}, nil, 200)
	for i := 0; i < 5; i++ {
		other.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "yanlis"}, nil, 401)
	}
	other.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "ucuncu-sifre"}, nil, 429)
	// Sınır giriş sınırından ayrıdır: yönetici hâlâ giriş yapabilir.
	pe.loginAs("kadir", "cok-gizli-sifre")
	pe.advance(16 * time.Minute)
	other.mustDo("POST", "/api/public/pages/ozel/unlock", map[string]string{"password": "ucuncu-sifre"}, nil, 200)
}

func TestCustomDomainRouting(t *testing.T) {
	pe := setupPages(t)
	push := pe.push("Push")
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel", "custom_domain": "durum.example.com"}, &p, 201)
	const host = "Durum.Example.com:443"

	get := func(path, h string) (int, string) {
		resp, data := pe.raw("GET", path, h, "", nil)
		return resp.StatusCode, string(data)
	}
	if code, body := get("/api/public/resolve", host); code != 200 || !strings.Contains(body, `"slug":"genel"`) {
		t.Fatalf("resolve özel alan adında slug döndürmeli: %d %s", code, body)
	}
	if code, body := get("/api/public/resolve", ""); code != 200 || !strings.Contains(body, `"slug":null`) {
		t.Fatalf("resolve ana adreste null döndürmeli: %d %s", code, body)
	}
	// Yönetici oturum çerezi gönderilse bile özel alan adında yönetim API'si yok.
	for _, path := range []string{"/api/monitors", "/api/auth/state", "/api/status-pages", "/api/users", "/api/events"} {
		if code, _ := get(path, host); code != 404 {
			t.Errorf("özel alan adında %s: %d, 404 bekleniyordu", path, code)
		}
	}
	if resp, _ := pe.raw("POST", "/api/auth/login", host, "application/json", []byte(`{"username":"kadir","password":"cok-gizli-sifre"}`)); resp.StatusCode != 404 {
		t.Errorf("özel alan adında giriş yapılamamalı: %d", resp.StatusCode)
	}
	if code, _ := get("/api/monitors", ""); code != 200 {
		t.Errorf("ana adreste yönetim API'si çalışmalı: %d", code)
	}
	// İzin verilenler: arayüz, healthz, herkese açık API, push.
	for _, path := range []string{"/", "/durum/genel", "/healthz", "/api/public/pages/genel", "/api/push/" + push.PushToken} {
		if code, body := get(path, host); code != 200 {
			t.Errorf("özel alan adında %s: %d %s", path, code, body)
		}
	}
	// Yol hileleri temizlenmiş yol üzerinden denetlenir.
	for _, path := range []string{"/api/public/../monitors", "/api/push/../monitors", "//api/monitors", "/metrics"} {
		req := httptest.NewRequest("GET", "http://x"+path, nil)
		req.URL.Path = path
		req.Host = host
		rec := httptest.NewRecorder()
		pe.s.customDomainOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) })).ServeHTTP(rec, req)
		if rec.Code != 404 {
			t.Errorf("%s engellenmeliydi: %d", path, rec.Code)
		}
	}

	// Yayında olmayan sayfa: resolve null, koruma sürer.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "genel", "title": "Genel", "custom_domain": "durum.example.com", "published": false}, nil, 200)
	if _, body := get("/api/public/resolve", host); !strings.Contains(body, `"slug":null`) {
		t.Errorf("yayında olmayan sayfa çözülmemeli: %s", body)
	}
	if code, _ := get("/api/monitors", host); code != 404 {
		t.Errorf("yayında olmasa da koruma sürmeli: %d", code)
	}
	// Alan adı değişince eşleme hemen yenilenir.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "genel", "title": "Genel", "custom_domain": "status.example.org", "published": true}, nil, 200)
	if code, _ := get("/api/monitors", host); code != 200 {
		t.Errorf("eski alan adı serbest kalmalı: %d", code)
	}
	if _, body := get("/api/public/resolve", "status.example.org"); !strings.Contains(body, `"slug":"genel"`) {
		t.Errorf("yeni alan adı çözülmeli: %s", body)
	}
	pe.mustDo("DELETE", fmt.Sprintf("/api/status-pages/%d", p.ID), nil, nil, 200)
	if code, _ := get("/api/monitors", "status.example.org"); code != 200 {
		t.Errorf("silinen sayfanın alan adı serbest kalmalı: %d", code)
	}
}

func TestAnnouncementsAPI(t *testing.T) {
	pe := setupPages(t)
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel"}, &p, 201)
	path := fmt.Sprintf("/api/status-pages/%d/announcements", p.ID)
	now := pe.s.now().Unix()

	var a store.Announcement
	pe.mustDo("POST", path, map[string]any{"title": " Bakım ", "body": "Gece 02:00"}, &a, 201)
	if a.Title != "Bakım" || a.Severity != "info" || a.StartsAt != now || a.EndsAt != 0 || a.PageID != p.ID {
		t.Fatalf("duyuru yanlış: %+v", a)
	}
	for i, bad := range []map[string]any{
		{"title": ""},
		{"title": strings.Repeat("a", 201)},
		{"title": "x", "body": strings.Repeat("a", 5001)},
		{"title": "x", "severity": "critical"},
		{"title": "x", "starts_at": 1000, "ends_at": 1000},
		{"title": "x", "starts_at": -5},
		{"title": "x", "ends_at": now - 10},
	} {
		if code := pe.do("POST", path, bad, nil); code != 400 {
			t.Errorf("geçersiz duyuru %d kabul edildi: %d", i, code)
		}
	}
	pe.mustDo("POST", "/api/status-pages/9999/announcements", map[string]any{"title": "x"}, nil, 404)
	pe.mustDo("GET", "/api/status-pages/9999/announcements", nil, nil, 404)

	pe.mustDo("PUT", fmt.Sprintf("/api/announcements/%d", a.ID), map[string]any{"title": "Çözüldü", "severity": "success", "starts_at": 100, "ends_at": now + 60}, &a, 200)
	if a.Title != "Çözüldü" || a.Severity != "success" || a.StartsAt != 100 || a.EndsAt != now+60 {
		t.Fatalf("güncelleme yanlış: %+v", a)
	}
	var list []store.Announcement
	pe.mustDo("GET", path, nil, &list, 200)
	if len(list) != 1 || list[0].ID != a.ID {
		t.Fatalf("liste yanlış: %+v", list)
	}
	pe.mustDo("DELETE", fmt.Sprintf("/api/announcements/%d", a.ID), nil, nil, 200)
	pe.mustDo("DELETE", fmt.Sprintf("/api/announcements/%d", a.ID), nil, nil, 404)
	pe.mustDo("PUT", fmt.Sprintf("/api/announcements/%d", a.ID), map[string]any{"title": "x"}, nil, 404)
}

func TestPublicPageCache(t *testing.T) {
	pe := setupPages(t)
	m := pe.seedMonitor("Eski ad", "https://a.example.com")
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel",
		"sections": []map[string]any{{"monitors": []map[string]any{{"id": m.ID}}}}}, &p, 201)
	anon := pe.anon()
	name := func() string {
		var pub publicPayload
		anon.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
		return pub.Sections[0].Monitors[0]["name"].(string)
	}
	if got := name(); got != "Eski ad" {
		t.Fatalf("ad: %s", got)
	}
	// Monitör değişikliği önbellek süresi (30 sn) dolunca görünür.
	m.Name = "Yeni ad"
	if err := pe.s.store.UpdateMonitor(context.Background(), &m, nil, false); err != nil {
		t.Fatal(err)
	}
	if got := name(); got != "Eski ad" {
		t.Errorf("önbellekten eski ad gelmeliydi: %s", got)
	}
	pe.advance(31 * time.Second)
	if got := name(); got != "Yeni ad" {
		t.Errorf("önbellek süresi dolunca yeni ad gelmeliydi: %s", got)
	}

	// Sayfa ve duyuru değişikliği önbelleği hemen boşaltır.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "genel", "title": "Genel",
		"sections": []map[string]any{{"monitors": []map[string]any{{"id": m.ID, "name": "Görünen"}}}}}, nil, 200)
	if got := name(); got != "Görünen" {
		t.Errorf("sayfa değişikliği hemen görünmeli: %s", got)
	}
	var a store.Announcement
	pe.mustDo("POST", fmt.Sprintf("/api/status-pages/%d/announcements", p.ID), map[string]any{"title": "Duyuru"}, &a, 201)
	var pub publicPayload
	anon.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
	if len(pub.Announcements) != 1 {
		t.Errorf("yeni duyuru hemen görünmeli: %+v", pub.Announcements)
	}
	pe.mustDo("DELETE", fmt.Sprintf("/api/announcements/%d", a.ID), nil, nil, 200)
	anon.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
	if len(pub.Announcements) != 0 {
		t.Errorf("silinen duyuru hemen kalkmalı: %+v", pub.Announcements)
	}
	// Kısa ad değişince eski adres hemen 404 olur.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "yeni", "title": "Genel"}, nil, 200)
	anon.mustDo("GET", "/api/public/pages/genel", nil, nil, 404)
	anon.mustDo("GET", "/api/public/pages/yeni", nil, nil, 200)
}

// Durum sayfası çubuk görünümleri: varsayılan son kontroller; 24 saat saatlik.
func TestPublicBarRanges(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://a.example.com")
	now := pe.s.now().Unix()
	for i := 0; i < 70; i++ { // 70 kontrol: son 60'ı gösterilir
		status := store.StatusUp
		if i == 69 {
			status = store.StatusDown
		}
		pe.beat(a.ID, now-int64(69-i)*60, status)
	}
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "anlik", "title": "Anlık",
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, &p, 201)
	if p.BarRange != "recent" {
		t.Fatalf("varsayılan görünüm son kontroller olmalı: %q", p.BarRange)
	}
	var pub struct {
		Range        string `json:"range"`
		UptimeWindow string `json:"uptime_window"`
		Sections     []struct {
			Monitors []struct {
				Uptime *float64 `json:"uptime"`
				Bars   []struct{ T, Up, Down int64 }
			}
		}
	}
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &pub, 200)
	bars := pub.Sections[0].Monitors[0].Bars
	if pub.Range != "recent" || pub.UptimeWindow != "24h" || len(bars) != 60 {
		t.Fatalf("son kontroller: %s %s %d çubuk", pub.Range, pub.UptimeWindow, len(bars))
	}
	if last := bars[59]; last.Down != 1 || last.T != now || bars[0].Up != 1 || bars[0].T != now-59*60 {
		t.Errorf("çubuklar eskiden yeniye son 60 kontrol olmalı: ilk %+v son %+v", bars[0], last)
	}
	if u := pub.Sections[0].Monitors[0].Uptime; u == nil || *u >= 100 {
		t.Errorf("24 saatlik uptime hesaplanmalı: %v", u)
	}

	// 24 saat: 24 saatlik çubuk; geçersiz değer reddedilir.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "anlik", "title": "Anlık", "bar_range": "1y",
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 400)
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "anlik", "title": "Anlık", "bar_range": "24h",
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 200)
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &pub, 200)
	bars = pub.Sections[0].Monitors[0].Bars
	if pub.Range != "24h" || len(bars) != 24 || bars[23].Up+bars[23].Down == 0 {
		t.Errorf("24 saat görünümü: %s %d çubuk, son %+v", pub.Range, len(bars), bars[len(bars)-1])
	}

	// Olay bölümü: varsayılan açık; kapatılınca yanıt bunu bildirir ve olay listesi boş gelir.
	var inc struct {
		ShowIncidents bool              `json:"show_incidents"`
		Incidents     []json.RawMessage `json:"incidents"`
	}
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &inc, 200)
	if !inc.ShowIncidents {
		t.Fatal("olay bölümü varsayılan olarak açık olmalı")
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "anlik", "title": "Anlık", "show_incidents": false,
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 200)
	inc.ShowIncidents, inc.Incidents = true, nil
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &inc, 200)
	if inc.ShowIncidents || len(inc.Incidents) != 0 {
		t.Errorf("olay bölümü gizli olmalı: %+v", inc)
	}
	// Başka bir alan güncellenince (show_incidents gönderilmeden) ayar korunur.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "anlik", "title": "Anlık 2",
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 200)
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &inc, 200)
	if inc.ShowIncidents {
		t.Error("show_incidents gönderilmeyince değişmemeli")
	}

	// Açılıp kapanan gruplar: varsayılan kapalı, açılınca yanıtta bildirilir.
	var col struct {
		Collapsible bool `json:"collapsible"`
	}
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &col, 200)
	if col.Collapsible {
		t.Fatal("gruplar varsayılan olarak sabit olmalı")
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "anlik", "title": "Anlık", "collapsible": true,
		"sections": []map[string]any{{"title": "S", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 200)
	pe.anon().mustDo("GET", "/api/public/pages/anlik", nil, &col, 200)
	if !col.Collapsible {
		t.Error("collapsible açılmadı")
	}
}
