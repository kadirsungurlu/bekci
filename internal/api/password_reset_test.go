package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// fakeMail gönderilen sistem e-postalarını biriktirir.
type fakeMail struct {
	mu   sync.Mutex
	sent []struct{ to, subject, text, html string }
	ch   chan struct{}
}

func newFakeMail() *fakeMail { return &fakeMail{ch: make(chan struct{}, 16)} }

func (f *fakeMail) send(_ context.Context, _ json.RawMessage, to, subject, text, html string) error {
	f.mu.Lock()
	f.sent = append(f.sent, struct{ to, subject, text, html string }{to, subject, text, html})
	f.mu.Unlock()
	f.ch <- struct{}{}
	return nil
}

func (f *fakeMail) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

var resetLinkRe = regexp.MustCompile(`/#/reset/([A-Za-z0-9_-]+)`)

// E-posta + şifremi unuttum: sistem e-postası ayarlı değilken bağlantı
// görünmez ve istek sessizce biter; ayarlanınca bağlantı gider, token tek
// kullanımlık, yanıt hesabın varlığını sızdırmaz, oturumlar kapanır.
func TestForgotPassword(t *testing.T) {
	mail := newFakeMail()
	e := newEnv(t, func(s *Server) { s.mailer = mail.send; s.BaseURL = "https://uptime.test" })
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)

	var st map[string]any
	e.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["password_reset"] != false {
		t.Fatalf("sistem e-postası yokken password_reset false olmalı: %v", st)
	}
	// E-posta: geçersiz, geçerli, şifre onayı.
	e.mustDo("POST", "/api/auth/email", map[string]string{"password": "cok-gizli-sifre", "email": "kadir@"}, nil, 400)
	e.mustDo("POST", "/api/auth/email", map[string]string{"password": "yanlis", "email": "kadir@ornek.com"}, nil, 400)
	var resp struct {
		User userView `json:"user"`
	}
	e.mustDo("POST", "/api/auth/email", map[string]string{"password": "cok-gizli-sifre", "email": " Kadir@Ornek.com "}, &resp, 200)
	if resp.User.Email != "kadir@ornek.com" {
		t.Fatalf("e-posta küçük harfe çevrilip kaydedilmeli: %+v", resp.User)
	}
	// Aynı e-posta ikinci kullanıcıya verilemez.
	e.mustDo("POST", "/api/users", map[string]any{"username": "ayse", "role": "viewer", "password": "gecici-sifre-1", "email": "KADIR@ornek.com"}, nil, 409)
	var created store.User
	e.mustDo("POST", "/api/users", map[string]any{"username": "ayse", "role": "viewer", "password": "gecici-sifre-1", "email": "ayse@ornek.com"}, &created, 201)
	if created.Email != "ayse@ornek.com" {
		t.Fatalf("yönetici e-posta verebilmeli: %+v", created)
	}

	// Sistem e-postası yokken istek 200 ama posta yok.
	e.mustDo("POST", "/api/auth/forgot", map[string]string{"login": "kadir"}, nil, 200)
	if mail.count() != 0 {
		t.Fatal("sistem e-postası ayarlı değilken posta gitmemeli")
	}
	// Sistem e-postası: bir SMTP kanalı seç (webhook kanalı reddedilir).
	var hook, smtp store.Notification
	e.mustDo("POST", "/api/notifications", map[string]any{"name": "H", "type": "webhook", "config": map[string]any{"url": "https://h.example"}}, &hook, 201)
	e.mustDo("POST", "/api/notifications", map[string]any{"name": "Posta", "type": "email", "config": map[string]any{"host": "smtp.example.com", "from": "bekci@ornek.com", "to": "ops@ornek.com", "password": "x", "username": "u"}}, &smtp, 201)
	var settings store.AppSettings
	e.mustDo("GET", "/api/settings", nil, &settings, 200)
	settings.SystemMailChannelID = hook.ID
	e.mustDo("PUT", "/api/settings", settings, nil, 400)
	settings.SystemMailChannelID = smtp.ID
	e.mustDo("PUT", "/api/settings", settings, nil, 200)
	e.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st["password_reset"] != true {
		t.Fatalf("sistem e-postası ayarlıyken password_reset true olmalı: %v", st)
	}

	// Oturumsuz istemci: bilinmeyen hesap ve e-postasız hesap da 200 (sızdırmaz), posta yok.
	anon := &env{t: t, srv: e.srv, client: &http.Client{}}
	anon.mustDo("POST", "/api/auth/forgot", map[string]string{"login": "yok-boyle-biri"}, nil, 200)
	anon.mustDo("POST", "/api/auth/forgot", map[string]string{"login": "kimse@ornek.com"}, nil, 200)
	if mail.count() != 0 {
		t.Fatal("eşleşmeyen hesap için posta gitmemeli")
	}
	// E-postayla ya da kullanıcı adıyla: bağlantı gider.
	anon.mustDo("POST", "/api/auth/forgot", map[string]string{"login": "KADIR@ornek.com"}, nil, 200)
	<-mail.ch
	m := mail.sent[0]
	if m.to != "kadir@ornek.com" || !strings.Contains(m.text, "https://uptime.test/#/reset/") || !strings.Contains(m.html, "/#/reset/") {
		t.Fatalf("sıfırlama postası: %+v", m)
	}
	token := resetLinkRe.FindStringSubmatch(m.text)[1]

	// Hatalı token / zayıf şifre.
	anon.mustDo("POST", "/api/auth/reset", map[string]string{"token": "yanlis", "password": "yeni-sifre-123"}, nil, 400)
	anon.mustDo("POST", "/api/auth/reset", map[string]string{"token": token, "password": "kisa"}, nil, 400)
	var done map[string]any
	anon.mustDo("POST", "/api/auth/reset", map[string]string{"token": token, "password": "yeni-sifre-123"}, &done, 200)
	if done["username"] != "kadir" {
		t.Fatalf("sıfırlama yanıtı: %v", done)
	}
	// Tek kullanımlık; eski oturum kapandı; yeni şifre çalışır, eski çalışmaz.
	anon.mustDo("POST", "/api/auth/reset", map[string]string{"token": token, "password": "yeni-sifre-456"}, nil, 400)
	if code := e.do("GET", "/api/monitors", nil, nil); code != 401 {
		t.Fatalf("sıfırlama eski oturumu kapatmalı: %d", code)
	}
	anon.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 401)
	fresh := e.loginAs("kadir", "yeni-sifre-123")
	var audit []store.AuditEntry
	fresh.mustDo("GET", "/api/audit?action=user.password_reset_self", nil, &audit, 200)
	if len(audit) != 1 {
		t.Fatalf("işlem kaydı: %+v", audit)
	}

	// Hız sınırı: aynı hesaba 15 dakikada 3 istek (üçüncüsü de gider, dördüncüsü 429).
	for i := 0; i < 3; i++ {
		anon.mustDo("POST", "/api/auth/forgot", map[string]string{"login": "ayse@ornek.com"}, nil, 200)
	}
	if code := anon.do("POST", "/api/auth/forgot", map[string]string{"login": "ayse@ornek.com"}, nil); code != 429 {
		t.Fatalf("hesap başına sınır: %d", code)
	}
	// Admin e-postayı kaldırabilir; e-postasız hesaba posta gitmez.
	e2 := fresh
	var u store.User
	e2.mustDo("PUT", fmt.Sprintf("/api/users/%d", created.ID), map[string]any{"display_name": "", "role": "viewer", "email": ""}, &u, 200)
	if u.Email != "" {
		t.Fatalf("e-posta kaldırılmalı: %+v", u)
	}
}
