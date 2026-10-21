package store

import (
	"context"
	"database/sql"
	"errors"
)

// Yeni sürüm bildirimini kapatan kullanıcının kapattığı sürüm (kullanıcı başına
// tek satır; daha yeni sürüm çıkınca bildirim yeniden görünür).
func init() {
	RegisterMigration(38, `
CREATE TABLE update_dismissals (
	user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	version TEXT NOT NULL
);
`)
}

// UpdateDismissed kullanıcının kapattığı son sürüm ("" = hiç).
func (s *Store) UpdateDismissed(ctx context.Context, userID int64) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT version FROM update_dismissals WHERE user_id = ?`, userID).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

// DismissUpdate kullanıcının bildirimi bu sürüm için kapattığını kaydeder.
func (s *Store) DismissUpdate(ctx context.Context, userID int64, version string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO update_dismissals (user_id, version) VALUES (?, ?)
		ON CONFLICT (user_id) DO UPDATE SET version = excluded.version`, userID, version)
	return err
}
