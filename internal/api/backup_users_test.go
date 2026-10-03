package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/backup"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kullanıcılı ve şifreli yedek: GET /api/export kullanıcı içermez; POST ile
// kullanıcılar (ve istenirse 2FA sırları) gelir; şifreli dosya parolasız
// okunamaz, yanlış parola 400 (backup_password); geri yüklemede var olan
// kullanıcı atlanır, yenisi şifre özetiyle eklenir ve giriş yapabilir.
func TestBackupUsersAndEncryption(t *testing.T) {
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hook.Close()
	a := setupAdmin(t)
	seedBackupEnv(t, a, hook.URL)
	mons := a.monitors()
	var cust store.User
	a.mustDo("POST", "/api/users", map[string]any{"username": "musteri", "role": "viewer", "password": "gecici-sifre-1", "email": "m@ornek.com",
		"all_monitors": false, "monitor_ids": []int64{mons["Site"].ID}}, &cust, 201)
	cl := a.loginAs("musteri", "gecici-sifre-1")
	cl.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)

	// GET: kullanıcı yok (geriye dönük).
	doc, _ := a.exportDoc()
	if len(doc.Users) != 0 {
		t.Fatalf("GET /api/export kullanıcı içermemeli: %d", len(doc.Users))
	}
	// POST: kullanıcılar, şifre özetleriyle; 2FA sırları istenmedi.
	code, data, h := a.send("POST", "/api/export", "application/json", strings.NewReader(`{"users":true}`))
	if code != 200 || !strings.Contains(h.Get("Content-Disposition"), "bekci-yedek-") {
		t.Fatalf("POST /api/export: %d %s", code, data)
	}
	var full backup.Doc
	json.Unmarshal(data, &full)
	if len(full.Users) != 2 {
		t.Fatalf("2 kullanıcı bekleniyordu: %+v", full.Users)
	}
	for _, u := range full.Users {
		if !strings.HasPrefix(u.PasswordHash, "$2") || u.TOTPSecret != "" {
			t.Fatalf("kullanıcı yedeği: %+v", u)
		}
		if u.Username == "musteri" && (u.AllMonitors || len(u.MonitorIDs) != 1 || u.MonitorIDs[0] != mons["Site"].ID || u.Email != "m@ornek.com") {
			t.Fatalf("müşteri kısıtı yedekte: %+v", u)
		}
	}
	// Şifreli: kısa parola reddedilir; dosya düz metinde gizli bilgi taşımaz.
	a.send("POST", "/api/export", "application/json", strings.NewReader(`{"users":true,"password":"kisa"}`))
	code, enc, h := a.send("POST", "/api/export", "application/json", strings.NewReader(`{"users":true,"password":"cok-gizli-yedek-parolasi"}`))
	if code != 200 || !strings.Contains(h.Get("Content-Disposition"), "sifreli") || !backup.IsEncrypted(enc) {
		t.Fatalf("şifreli yedek: %d %s", code, h.Get("Content-Disposition"))
	}
	for _, secret := range []string{"gizli-parola", "Bearer gizli-token", "musteri", "$2"} {
		if bytes.Contains(enc, []byte(secret)) {
			t.Fatalf("şifreli dosyada açık bilgi var: %s", secret)
		}
	}
	if _, err := backup.Decrypt(enc, "yanlis-parola-123"); err == nil {
		t.Fatal("yanlış parola açmamalı")
	}

	// Geri yükleme: şifresiz istek 400 (code), yanlış parola 400, doğru parola birleştirir.
	code, body, _ := a.send("POST", "/api/import", "application/json", bytes.NewReader(enc))
	if code != 400 || !strings.Contains(string(body), "backup_password") {
		t.Fatalf("parolasız içe aktarma: %d %s", code, body)
	}
	req, _ := http.NewRequest("POST", a.srv.URL+"/api/import?mode=merge", bytes.NewReader(enc))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	req.Header.Set("X-Backup-Password", "yanlis-parola-123")
	resp, err := a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 400 {
		t.Fatalf("yanlış parola: %d", resp.StatusCode)
	}
	// Doğru parola; aynı kuruluma birleştirme: kullanıcılar zaten var.
	req, _ = http.NewRequest("POST", a.srv.URL+"/api/import?mode=merge", bytes.NewReader(enc))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Uptime", "1")
	req.Header.Set("X-Backup-Password", "cok-gizli-yedek-parolasi")
	resp, err = a.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var sum importSummary
	json.NewDecoder(resp.Body).Decode(&sum)
	resp.Body.Close()
	if resp.StatusCode != 200 || sum.Existing.Users != 2 || sum.Created.Users != 0 {
		t.Fatalf("aynı kuruluma kullanıcı birleştirme: %d %+v", resp.StatusCode, sum)
	}

	// Taze kuruluma geri yükleme: yeni kullanıcı eklenir ve eski şifresiyle girer.
	fresh := setupAdmin(t) // yönetici "kadir" zaten var → existing; "musteri" eklenir
	sum = fresh.importDoc("/api/import?mode=merge", full, 200)
	if sum.Created.Users != 1 || sum.Existing.Users != 1 {
		t.Fatalf("taze kuruluma kullanıcı aktarımı: %+v", sum)
	}
	m2 := fresh.monitors()
	c := fresh.loginAs("musteri", "kalici-sifre-1")
	var list []monitorView
	c.mustDo("GET", "/api/monitors", nil, &list, 200)
	// Kısıtlı kullanıcı yalnızca "Site"yi görür (dosya kimliği → yeni kimlik).
	if len(list) != 1 || list[0].ID != m2["Site"].ID {
		t.Fatalf("aktarılan kullanıcının kısıtı: %+v", list)
	}
	var st map[string]any
	c.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if u := st["user"].(map[string]any); u["email"] != "m@ornek.com" || u["must_change_password"] != false {
		t.Fatalf("aktarılan kullanıcı: %v", u)
	}
	// Bozuk şifre özeti ve geçersiz ad atlanır.
	bad := full
	bad.Users = []backup.User{{Username: "x", Role: "viewer", PasswordHash: "$2a$..", AllMonitors: true}, {Username: "duz", Role: "viewer", PasswordHash: "plain", AllMonitors: true}}
	sum = fresh.importDoc("/api/import?mode=merge&dry_run=1", bad, 200)
	if sum.Skipped.Users != 2 {
		t.Fatalf("geçersiz kullanıcılar atlanmalı: %+v", sum)
	}
}
