package store

import (
	"context"
	"database/sql"
	"errors"
)

// 6: iki adımlı doğrulama (TOTP). totp_secret kurulum başlayınca yazılır,
// totp_enabled doğrulama kodu onaylanınca 1 olur. totp_last_step aynı kodun
// tekrar kullanılmasını engeller. Kurtarma kodları ve giriş sırasındaki ikinci
// adım bekleyen oturum açma denemeleri (challenge) ayrı tablolarda; ikisinde de
// kodun kendisi değil SHA-256 özeti tutulur.
func init() {
	RegisterMigration(6, `
ALTER TABLE users ADD COLUMN totp_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN totp_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN totp_last_step INTEGER NOT NULL DEFAULT 0;

CREATE TABLE recovery_codes (
	id         INTEGER PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	code_hash  TEXT    NOT NULL,
	used_at    INTEGER
);
CREATE INDEX recovery_codes_user ON recovery_codes(user_id);

CREATE TABLE login_challenges (
	token_hash TEXT    NOT NULL,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	attempts   INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (token_hash)
) WITHOUT ROWID;
`)
}

// ErrTwoFactorEnabled iki adımlı doğrulama zaten açıkken kurulum girişimi.
var ErrTwoFactorEnabled = errors.New("iki adımlı doğrulama zaten açık")

// TOTPSecret kullanıcının TOTP sırrını (kurulum bekliyorsa bekleyen sırrı) ve
// açık olup olmadığını döner.
func (s *Store) TOTPSecret(ctx context.Context, userID int64) (secret string, enabled bool, err error) {
	err = s.db.QueryRowContext(ctx, "SELECT totp_secret, totp_enabled FROM users WHERE id = ?", userID).Scan(&secret, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return secret, enabled, err
}

// SetPendingTOTP kurulum için yeni sırrı yazar; iki adımlı doğrulama zaten
// açıksa ErrTwoFactorEnabled döner (önce kapatılmalı).
func (s *Store) SetPendingTOTP(ctx context.Context, userID int64, secret string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET totp_secret = ?, totp_last_step = 0 WHERE id = ? AND totp_enabled = 0", secret, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrTwoFactorEnabled
	}
	return nil
}

// EnableTOTP bekleyen sırrı etkinleştirir (step: onaylanan kodun adımı, tekrar
// kullanılamaz) ve kurtarma kodlarını yazar. Sır değiştiyse (başka sekmede
// yeniden kurulum) veya zaten açıksa ErrTwoFactorEnabled döner.
func (s *Store) EnableTOTP(ctx context.Context, userID int64, secret string, step int64, codeHashes []string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET totp_enabled = 1, totp_last_step = ?
			WHERE id = ? AND totp_enabled = 0 AND totp_secret = ?`, step, userID, secret)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrTwoFactorEnabled
		}
		return replaceRecoveryCodes(ctx, tx, userID, codeHashes)
	})
}

// UseTOTPStep kodun adımını kullanılmış olarak işaretler. Adım daha önce
// kullanılan (veya daha eski) bir adımsa false döner: aynı kod iki kez
// kullanılamaz. Tek UPDATE olduğundan eşzamanlı iki istekten yalnızca biri geçer.
func (s *Store) UseTOTPStep(ctx context.Context, userID, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET totp_last_step = ? WHERE id = ? AND totp_enabled = 1 AND totp_last_step < ?",
		step, userID, step)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// UseRecoveryCode kullanılmamış kurtarma kodunu harcar; bulunamazsa false.
func (s *Store) UseRecoveryCode(ctx context.Context, userID int64, codeHash string, now int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE recovery_codes SET used_at = ?
		WHERE user_id = ? AND code_hash = ? AND used_at IS NULL`, now, userID, codeHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ReplaceRecoveryCodes eski kurtarma kodlarını silip yenilerini yazar.
func (s *Store) ReplaceRecoveryCodes(ctx context.Context, userID int64, codeHashes []string) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return replaceRecoveryCodes(ctx, tx, userID, codeHashes) })
}

func replaceRecoveryCodes(ctx context.Context, tx *sql.Tx, userID int64, codeHashes []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM recovery_codes WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, h := range codeHashes {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO recovery_codes (user_id, code_hash) VALUES (?, ?)", userID, h); err != nil {
			return err
		}
	}
	return nil
}

// RecoveryCodesLeft kullanılmamış kurtarma kodu sayısı.
func (s *Store) RecoveryCodesLeft(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM recovery_codes WHERE user_id = ? AND used_at IS NULL", userID).Scan(&n)
	return n, err
}

// DisableTwoFactor iki adımlı doğrulamayı kapatır: sır, kurtarma kodları ve
// bekleyen giriş denemeleri silinir.
func (s *Store) DisableTwoFactor(ctx context.Context, userID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE users SET totp_secret = '', totp_enabled = 0, totp_last_step = 0 WHERE id = ?", userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		for _, q := range []string{
			"DELETE FROM recovery_codes WHERE user_id = ?",
			"DELETE FROM login_challenges WHERE user_id = ?",
		} {
			if _, err := tx.ExecContext(ctx, q, userID); err != nil {
				return err
			}
		}
		return nil
	})
}

// Giriş ikinci adımı -------------------------------------------------------------------

// CreateLoginChallenge şifresi doğrulanmış, kod bekleyen giriş denemesini
// kaydeder; süresi geçmiş eski denemeler de temizlenir.
func (s *Store) CreateLoginChallenge(ctx context.Context, tokenHash string, userID, expiresAt, now int64) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE expires_at <= ?", now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO login_challenges (token_hash, user_id, expires_at) VALUES (?, ?, ?)", tokenHash, userID, expiresAt)
	return err
}

// LoginChallenge süresi dolmamış denemenin kullanıcısını döner.
func (s *Store) LoginChallenge(ctx context.Context, tokenHash string, now int64) (int64, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx,
		"SELECT user_id FROM login_challenges WHERE token_hash = ? AND expires_at > ?", tokenHash, now).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

// FailLoginChallenge hatalı kod sayacını artırır; max'a ulaşan deneme silinir.
func (s *Store) FailLoginChallenge(ctx context.Context, tokenHash string, max int) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx,
			"UPDATE login_challenges SET attempts = attempts + 1 WHERE token_hash = ?", tokenHash); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			"DELETE FROM login_challenges WHERE token_hash = ? AND attempts >= ?", tokenHash, max)
		return err
	})
}

// ConsumeLoginChallenge denemeyi siler; tek kullanımlıktır, eşzamanlı iki
// istekten yalnızca biri true alır.
func (s *Store) ConsumeLoginChallenge(ctx context.Context, tokenHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM login_challenges WHERE token_hash = ?", tokenHash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
