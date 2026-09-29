package store

import (
	"context"
	"testing"
	"time"
)

// TestLangMigration migration 16 öncesi oluşturulmuş veriler yükseltmeden
// sonra varsayılan dilleri alır: kullanıcı "" (tarayıcı dili), sayfa "tr".
func TestLangMigration(t *testing.T) {
	ctx := context.Background()
	target := testTarget(t)

	// Migration 16 olmadan (eski sürüm) veritabanı kur; sonrakiler de yok.
	restore := hideMigrationsFrom(t, 16)
	old, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := old.schemaVersion(ctx); v != 15 {
		t.Fatalf("eski sürüm %d, 15 bekleniyordu", v)
	}
	if _, err := old.db.ExecContext(ctx, "INSERT INTO users (username, password_hash, created_at) VALUES ('eski', 'x', 1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := old.db.ExecContext(ctx, "INSERT INTO status_pages (slug, title, created_at, updated_at) VALUES ('eski', 'Eski', 1, 1)"); err != nil {
		t.Fatal(err)
	}
	old.Close()

	restore()
	s, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.schemaVersion(ctx); v < 16 {
		t.Fatalf("migration 16 uygulanmadı: %d", v)
	}
	u, err := s.UserByName(ctx, "eski")
	if err != nil || u.Lang != "" {
		t.Fatalf("eski kullanıcının dili boş olmalı: %q %v", u.Lang, err)
	}
	p, err := s.PageBySlug(ctx, "eski")
	if err != nil || p.Lang != "tr" {
		t.Fatalf("eski sayfa tr olmalı: %q %v", p.Lang, err)
	}

	if err := s.SetUserLang(ctx, u.ID, "en"); err != nil {
		t.Fatal(err)
	}
	if u, _ = s.UserByID(ctx, u.ID); u.Lang != "en" {
		t.Errorf("dil kaydedilmedi: %q", u.Lang)
	}
	if err := s.SetUserLang(ctx, 999999, "en"); err != ErrNotFound {
		t.Errorf("olmayan kullanıcı: %v", err)
	}
	p.Lang = "en"
	if err := s.UpdatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if p, _ = s.GetPage(ctx, p.ID); p.Lang != "en" {
		t.Errorf("sayfa dili kaydedilmedi: %q", p.Lang)
	}
	// Geçersiz dil varsayılana düşer.
	n := StatusPage{Slug: "yeni", Title: "Yeni", Lang: "xx", Sections: []PageSection{}}
	if err := s.CreatePage(ctx, &n); err != nil {
		t.Fatal(err)
	}
	if n, _ = s.GetPage(ctx, n.ID); n.Lang != "tr" {
		t.Errorf("geçersiz dil tr olmalı: %q", n.Lang)
	}
}

func TestNotifyLangSettings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, err := s.LoadSettings(ctx)
	if err != nil || a.NotifyLang != "tr" {
		t.Fatalf("varsayılan: %q %v", a.NotifyLang, err)
	}
	a.NotifyLang = "en"
	if err := a.Validate(); err != nil {
		t.Fatal(err)
	}
	s.SaveSettings(ctx, a)
	if a, _ = s.LoadSettings(ctx); a.NotifyLang != "en" {
		t.Errorf("kaydedilmedi: %q", a.NotifyLang)
	}
	a.NotifyLang = "de"
	if a.Validate() == nil {
		t.Error("geçersiz dil kabul edildi")
	}
	// Eski kayıt (alan yok) tr sayılır.
	s.SetSettings(ctx, map[string]string{appSettingsKey: `{"retention_raw_days":14,"retention_hourly_days":365,"cert_days":[7],"backup_keep":7}`})
	if a, _ = s.LoadSettings(ctx); a.NotifyLang != "tr" {
		t.Errorf("eski kayıt: %q", a.NotifyLang)
	}
}
