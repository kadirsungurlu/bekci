package api

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Bulgu (O-1): yönetici rollü API anahtarı kullanıcı açıp şifre sıfırlayarak
// "yalnızca oturum" korumalarını dolaylı aşabiliyordu. Kullanıcı yönetiminin
// yazma uçları anahtarla kapalı; listeleme serbest.
func TestAPIKeyCannotManageUsers(t *testing.T) {
	f := newFeatureEnv(t)
	_, victim := f.newUser("izleyici1", store.RoleViewer, nil)
	key := f.newKey("yönetici", store.RoleAdmin, 0)
	hdr := bearer(key.Secret)

	if code, _, _ := f.rawReq("GET", "/api/users", hdr, nil); code != 200 {
		t.Fatalf("anahtarla kullanıcı listesi: %d, 200 bekleniyordu", code)
	}
	cases := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/users", map[string]any{"username": "saldirgan", "role": "admin", "password": "cok-gizli-sifre-2"}},
		{"PUT", fmt.Sprintf("/api/users/%d", victim.ID), map[string]any{"role": "admin"}},
		{"POST", fmt.Sprintf("/api/users/%d/password", victim.ID), map[string]any{"password": "yeni-gizli-sifre"}},
		{"POST", fmt.Sprintf("/api/users/%d/2fa/reset", victim.ID), map[string]any{}},
		{"DELETE", fmt.Sprintf("/api/users/%d", victim.ID), nil},
	}
	for _, c := range cases {
		if code, _, body := f.rawReq(c.method, c.path, hdr, c.body); code != 401 {
			t.Errorf("anahtarla %s %s: %d, 401 bekleniyordu: %s", c.method, c.path, code, body)
		}
	}
	// Oturumla aynı işlemler çalışır.
	f.mustDo("PUT", fmt.Sprintf("/api/users/%d", victim.ID), map[string]any{"role": "editor"}, nil, 200)
	f.mustDo("POST", fmt.Sprintf("/api/users/%d/password", victim.ID), map[string]any{"password": "yeni-gizli-sifre"}, nil, 200)
}

func TestSessionOnlyTable(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/api/users", false},
		{"POST", "/api/users", true},
		{"PUT", "/api/users/3", true},
		{"DELETE", "/api/users/3", true},
		{"POST", "/api/users/3/password", true},
		{"POST", "/api/users/3/2fa/reset", true},
		{"GET", "/api/export", true},
		{"POST", "/api/import", true},
		{"POST", "/api/auth/password", true},
		{"GET", "/api/api-keys", true},
		{"POST", "/api/monitors", false},
		{"PUT", "/api/settings", false},
	}
	for _, c := range cases {
		if got := sessionOnly(c.method, c.path); got != c.want {
			t.Errorf("sessionOnly(%s %s) = %v, %v bekleniyordu", c.method, c.path, got, c.want)
		}
	}
}

// Bulgu (D-7): şifreli durum sayfasında hatalı denemeler sayfa başına da
// sayılıyor, 50 hata sayfayı herkese kapatıyordu. Kilit yalnızca IP+sayfa.
func TestPagePasswordLockoutPerIP(t *testing.T) {
	pe := setupPages(t)
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "ozel", "title": "Özel", "password": "dogru-sifre"}, nil, 201)
	try := func(ip, pw string) int {
		code, _, _ := pe.rawReq("POST", "/api/public/pages/ozel/unlock",
			map[string]string{"X-Uptime": "1", "Content-Type": "application/json", "X-Forwarded-For": ip},
			map[string]string{"password": pw})
		return code
	}
	for i := 0; i < loginMaxPerIP; i++ {
		if code := try("203.0.113.5", "yanlis"); code != 401 {
			t.Fatalf("%d. hatalı deneme: %d", i+1, code)
		}
	}
	if code := try("203.0.113.5", "dogru-sifre"); code != 429 {
		t.Fatalf("kilitli IP doğru şifreyle de beklemeli: %d", code)
	}
	// Başka ziyaretçi etkilenmez.
	if code := try("203.0.113.6", "yanlis"); code != 401 {
		t.Fatalf("başka IP'nin hatalı denemesi 401 olmalı: %d", code)
	}
	if code := try("203.0.113.6", "dogru-sifre"); code != 200 {
		t.Fatalf("başka IP doğru şifreyle açabilmeli: %d", code)
	}
	// Aynı IP başka bir sayfada kilitli değil.
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "diger", "title": "Diğer", "password": "dogru-sifre"}, nil, 201)
	code, _, _ := pe.rawReq("POST", "/api/public/pages/diger/unlock",
		map[string]string{"X-Uptime": "1", "Content-Type": "application/json", "X-Forwarded-For": "203.0.113.5"},
		map[string]string{"password": "dogru-sifre"})
	if code != 200 {
		t.Fatalf("kilit sayfa başına olmalı: %d", code)
	}
}

// Bulgu (D-8): /api/push/{token} hız sınırsızdı. IP başına dakikalık sınır.
func TestPushRateLimit(t *testing.T) {
	e := newEnv(t)
	hdr := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	for i := 0; i < pushPerMinute; i++ {
		if code, _, _ := e.rawReq("GET", "/api/push/gecersiz-token", hdr, nil); code != http.StatusNotFound {
			t.Fatalf("%d. istek: %d, 404 bekleniyordu", i+1, code)
		}
	}
	code, h, _ := e.rawReq("GET", "/api/push/gecersiz-token", hdr, nil)
	if code != http.StatusTooManyRequests || h.Get("Retry-After") == "" {
		t.Fatalf("sınır aşımında 429 + Retry-After bekleniyordu: %d %q", code, h.Get("Retry-After"))
	}
	// Başka IP etkilenmez.
	if code, _, _ := e.rawReq("GET", "/api/push/gecersiz-token", map[string]string{"X-Forwarded-For": "203.0.113.10"}, nil); code != 404 {
		t.Fatalf("başka IP: %d", code)
	}
}

// Bulgu (D-9): yaygın şifreler kabul ediliyordu.
func TestCommonPasswordRejected(t *testing.T) {
	e := newEnv(t)
	var resp struct {
		Error string `json:"error"`
	}
	if code := e.do("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "Password123"}, &resp); code != 400 {
		t.Fatalf("yaygın şifre reddedilmeli: %d %q", code, resp.Error)
	}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	if code := e.do("POST", "/api/users", map[string]any{"username": "yeni", "role": "viewer", "password": "qwerty123"}, &resp); code != 400 {
		t.Fatalf("kullanıcı eklerken yaygın şifre reddedilmeli: %d", code)
	}
}
