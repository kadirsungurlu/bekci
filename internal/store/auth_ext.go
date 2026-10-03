package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// 31: kullanıcı e-postası, şifre sıfırlama ve OIDC (E-13).
//
//   - users.email: isteğe bağlı, küçük harfle saklanır, boş değilse tekil
//     (kısmi benzersiz dizin). Şifre sıfırlama bağlantısı buraya gider.
//   - password_resets: tek kullanımlık, süreli sıfırlama bağlantıları; token
//     değil SHA-256 özeti saklanır.
//   - oidc_identities: kullanıcı ↔ sağlayıcı (issuer) + subject bağı.
//   - oidc_states: başlatılan OIDC girişleri (state, nonce, PKCE doğrulayıcı);
//     tarayıcıdaki çerezle eşleşir, 10 dakikada dolar, tek kullanımlıktır.
func init() {
	RegisterMigration(31, `
ALTER TABLE users ADD COLUMN email TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX users_email_key ON users(email) WHERE email <> '';

CREATE TABLE password_resets (
	token_hash TEXT    NOT NULL PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL,
	used_at    INTEGER
) WITHOUT ROWID;
CREATE INDEX password_resets_user ON password_resets(user_id);

CREATE TABLE oidc_identities (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	issuer     TEXT    NOT NULL,
	subject    TEXT    NOT NULL,
	email      TEXT    NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	last_login INTEGER,
	PRIMARY KEY (issuer, subject)
) WITHOUT ROWID;
CREATE INDEX oidc_identities_user ON oidc_identities(user_id);

CREATE TABLE oidc_states (
	id_hash    TEXT    NOT NULL PRIMARY KEY,
	state      TEXT    NOT NULL,
	nonce      TEXT    NOT NULL,
	verifier   TEXT    NOT NULL,
	next       TEXT    NOT NULL DEFAULT '',
	expires_at INTEGER NOT NULL
) WITHOUT ROWID;
`)
}

// 33: kullanıcı başına arayüz teması ("" = sistem, light, dark).
func init() {
	RegisterMigration(33, `ALTER TABLE users ADD COLUMN theme TEXT NOT NULL DEFAULT '';`)
}

// SetUserTheme arayüz teması tercihini yazar.
func (s *Store) SetUserTheme(ctx context.Context, userID int64, theme string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET theme = ? WHERE id = ?", theme, userID)
	return err
}

// ErrEmailTaken aynı e-posta başka bir kullanıcıda.
var ErrEmailTaken = errors.New("Bu e-posta adresi başka bir kullanıcıda kayıtlı")

// NormalizeEmail e-postayı saklanan biçime çevirir (kırpılmış, küçük harf).
func NormalizeEmail(e string) string { return strings.ToLower(strings.TrimSpace(e)) }

// SetUserEmail kullanıcının e-postasını yazar ("" = kaldır). Tekillik
// çakışmasında ErrEmailTaken.
func (s *Store) SetUserEmail(ctx context.Context, userID int64, email string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE users SET email = ? WHERE id = ?", NormalizeEmail(email), userID)
	if IsUniqueViolation(err) {
		return ErrEmailTaken
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UserByEmail e-postası eşleşen kullanıcı (boş e-posta hiçbir zaman eşleşmez).
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	email = NormalizeEmail(email)
	if email == "" {
		return User{}, ErrNotFound
	}
	return s.scanUser(ctx, s.db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE email = ?", email))
}

// UserByLogin kullanıcı adı ya da e-postayla kullanıcı (şifremi unuttum).
func (s *Store) UserByLogin(ctx context.Context, login string) (User, error) {
	login = strings.TrimSpace(login)
	if strings.Contains(login, "@") {
		return s.UserByEmail(ctx, login)
	}
	return s.UserByName(ctx, login)
}

// Şifre sıfırlama ---------------------------------------------------------------------

// CreatePasswordReset kullanıcı için sıfırlama kaydı açar; kullanıcının önceki
// açık kayıtları geçersiz olur (yalnızca son bağlantı çalışır).
func (s *Store) CreatePasswordReset(ctx context.Context, tokenHash string, userID int64, now, expiresAt int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM password_resets WHERE user_id = ? OR expires_at <= ?", userID, now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO password_resets (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
			tokenHash, userID, now, expiresAt)
		return err
	})
}

// PasswordResetUser geçerli (süresi dolmamış, kullanılmamış) kaydın kullanıcısı.
func (s *Store) PasswordResetUser(ctx context.Context, tokenHash string, now int64) (int64, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx,
		"SELECT user_id FROM password_resets WHERE token_hash = ? AND expires_at > ? AND used_at IS NULL", tokenHash, now).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return uid, err
}

// UsePasswordReset kaydı tek kullanımlık harcar, yeni şifreyi yazar
// (zorunlu değişim kalkar) ve kullanıcının tüm oturumlarını kapatır.
// Eşzamanlı ikinci istek false alır.
func (s *Store) UsePasswordReset(ctx context.Context, tokenHash string, userID int64, hash string, now int64) (bool, error) {
	used := false
	err := s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE password_resets SET used_at = ? WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?", now, tokenHash, now)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		used = true
		if _, err := tx.ExecContext(ctx, "UPDATE users SET password_hash = ?, must_change_password = 0 WHERE id = ?", hash, userID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID)
		return err
	})
	return used, err
}

// OIDC ---------------------------------------------------------------------------------

// OIDCIdentity sağlayıcı bağı.
type OIDCIdentity struct {
	UserID    int64
	Issuer    string
	Subject   string
	Email     string
	CreatedAt int64
	LastLogin int64
}

// OIDCUser issuer + subject ile bağlı kullanıcı.
func (s *Store) OIDCUser(ctx context.Context, issuer, subject string) (User, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, "SELECT user_id FROM oidc_identities WHERE issuer = ? AND subject = ?", issuer, subject).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, err
	}
	return s.UserByID(ctx, uid)
}

// LinkOIDC kullanıcıyı sağlayıcı kimliğine bağlar (varsa dokunmaz).
func (s *Store) LinkOIDC(ctx context.Context, id OIDCIdentity) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO oidc_identities (user_id, issuer, subject, email, created_at, last_login) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, id.UserID, id.Issuer, id.Subject, NormalizeEmail(id.Email), id.CreatedAt, nullInt(id.LastLogin))
	return err
}

// TouchOIDC son OIDC girişini yazar.
func (s *Store) TouchOIDC(ctx context.Context, issuer, subject string, now int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE oidc_identities SET last_login = ? WHERE issuer = ? AND subject = ?", now, issuer, subject)
	return err
}

// UnlinkOIDC kullanıcının tüm sağlayıcı bağlarını kaldırır (yönetici).
func (s *Store) UnlinkOIDC(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM oidc_identities WHERE user_id = ?", userID)
	return err
}

// OIDCState başlatılmış bir OIDC girişi.
type OIDCState struct {
	State, Nonce, Verifier, Next string
	ExpiresAt                    int64
}

// CreateOIDCState girişi kaydeder (idHash: tarayıcı çerezindeki kimliğin özeti).
func (s *Store) CreateOIDCState(ctx context.Context, idHash string, st OIDCState, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM oidc_states WHERE expires_at <= ?", now); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO oidc_states (id_hash, state, nonce, verifier, next, expires_at) VALUES (?, ?, ?, ?, ?, ?)",
			idHash, st.State, st.Nonce, st.Verifier, st.Next, st.ExpiresAt)
		return err
	})
}

// ConsumeOIDCState kaydı okur ve siler (tek kullanımlık); yoksa/süresi
// dolmuşsa ErrNotFound.
func (s *Store) ConsumeOIDCState(ctx context.Context, idHash string, now int64) (OIDCState, error) {
	var st OIDCState
	err := s.tx(ctx, func(tx *Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT state, nonce, verifier, next, expires_at FROM oidc_states WHERE id_hash = ?", idHash).
			Scan(&st.State, &st.Nonce, &st.Verifier, &st.Next, &st.ExpiresAt)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM oidc_states WHERE id_hash = ?", idHash); err != nil {
			return err
		}
		if st.ExpiresAt <= now {
			return ErrNotFound
		}
		return nil
	})
	return st, err
}

// SetUserRole yalnızca rolü değiştirir (OIDC rol eşlemesi); son yönetici
// korunur (ErrLastAdmin).
func (s *Store) SetUserRole(ctx context.Context, userID int64, role string) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "UPDATE users SET role = ? WHERE id = ?", role, userID); err != nil {
			return err
		}
		return ensureAdminLeft(ctx, tx)
	})
}

// CreateUserExternal dış sağlayıcıyla (OIDC) açılan hesap: şifre
// kullanılamaz (rastgele özet), ilk girişte şifre değişimi istenmez.
func (s *Store) CreateUserExternal(ctx context.Context, u *User, unusableHash string) error {
	u.CreatedAt = time.Now().Unix()
	u.MustChangePassword = false
	return s.tx(ctx, func(tx *Tx) error {
		var err error
		u.ID, err = insertID(ctx, tx, `
			INSERT INTO users (username, display_name, email, role, disabled, must_change_password, all_monitors, password_hash, created_at)
			VALUES (?, ?, ?, ?, 0, 0, 1, ?, ?)`,
			u.Username, u.DisplayName, NormalizeEmail(u.Email), u.Role, unusableHash, u.CreatedAt)
		return err
	})
}

// ClearMustChangePassword zorunlu şifre değişimini kaldırır (SSO ile giren
// kullanıcı geçici şifreyi hiç kullanmaz).
func (s *Store) ClearMustChangePassword(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET must_change_password = 0 WHERE id = ?", userID)
	return err
}

// UsernameTaken kullanıcı adı (büyük/küçük harf duyarsız) kullanımda mı?
func (s *Store) UsernameTaken(ctx context.Context, username string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users WHERE LOWER(username) = LOWER(?)", username).Scan(&n)
	return n > 0, err
}

// DeleteExpiredAuthTokens süresi dolmuş sıfırlama ve OIDC kayıtlarını siler (günlük bakım).
func (s *Store) DeleteExpiredAuthTokens(ctx context.Context, now int64) error {
	if _, err := s.db.ExecContext(ctx, "DELETE FROM password_resets WHERE expires_at <= ?", now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM oidc_states WHERE expires_at <= ?", now)
	return err
}
