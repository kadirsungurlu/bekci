package store

import (
	"context"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/i18n"
)

func init() {
	// 16: dil desteği.
	//   users.lang: kullanıcının arayüz dili; '' = tercih yok (tarayıcı dili).
	//   status_pages.lang: herkese açık durum sayfasının dili; mevcut sayfalar
	//   Türkçe kalır.
	// Bildirim dili ayrı sütun değil, uygulama ayarlarındadır (AppSettings.NotifyLang).
	RegisterMigration(16, `
ALTER TABLE users ADD COLUMN lang TEXT NOT NULL DEFAULT '';
ALTER TABLE status_pages ADD COLUMN lang TEXT NOT NULL DEFAULT 'tr';
`)
}

// SetUserLang kullanıcının arayüz dilini kaydeder ("" = tarayıcı dili).
func (s *Store) SetUserLang(ctx context.Context, userID int64, lang string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET lang = ? WHERE id = ?", lang, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// pageLangOr sayfa dilini doğrular; geçersiz veya boşsa varsayılan (tr).
func pageLangOr(lang string) string { return i18n.Or(lang) }
