package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Roller. Yetki sırası: izleyici < editör < yönetici.
const (
	RoleViewer = "viewer"
	RoleEditor = "editor"
	RoleAdmin  = "admin"
)

// RoleRank rolün yetki düzeyi; bilinmeyen rol 0 (hiçbir yetki).
func RoleRank(role string) int {
	switch role {
	case RoleViewer:
		return 1
	case RoleEditor:
		return 2
	case RoleAdmin:
		return 3
	}
	return 0
}

// ErrLastAdmin son aktif yöneticiyi silme/devre dışı bırakma/rolünü düşürme girişimi.
var ErrLastAdmin = errors.New("son aktif yönetici")

type User struct {
	ID                 int64   `json:"id"`
	Username           string  `json:"username"`
	DisplayName        string  `json:"display_name"`
	Role               string  `json:"role"`
	Disabled           bool    `json:"disabled"`
	MustChangePassword bool    `json:"must_change_password"`
	AllMonitors        bool    `json:"all_monitors"` // false: sadece MonitorIDs (izleyici/müşteri)
	MonitorIDs         []int64 `json:"monitor_ids"`
	ServerIDs          []int64 `json:"server_ids"` // kısıtlıysa görebileceği sunucular (user_servers.go)
	LastLoginAt        int64   `json:"last_login_at"`
	CreatedAt          int64   `json:"created_at"`
	TwoFactorEnabled   bool    `json:"two_factor_enabled"` // TOTP (migration 6, twofactor.go)
	Lang               string  `json:"lang"`               // arayüz dili; "" = tarayıcı dili (migration 16)
	PasswordHash       string  `json:"-"`
}

// Restricted kullanıcı sadece kendisine atanmış monitörleri mi görür?
// Kısıt yalnızca izleyici rolünde anlamlıdır.
func (u User) Restricted() bool { return u.Role == RoleViewer && !u.AllMonitors }

const userCols = `id, username, display_name, role, disabled, must_change_password,
	all_monitors, last_login_at, created_at, password_hash, totp_enabled, lang`

func scanUserRow(sc scanner) (User, error) {
	var u User
	var lastLogin sql.NullInt64
	err := sc.Scan(&u.ID, &u.Username, &u.DisplayName, &u.Role, &u.Disabled,
		&u.MustChangePassword, &u.AllMonitors, &lastLogin, &u.CreatedAt, &u.PasswordHash, &u.TwoFactorEnabled, &u.Lang)
	u.LastLoginAt = lastLogin.Int64
	u.MonitorIDs, u.ServerIDs = []int64{}, []int64{}
	return u, err
}

func (s *Store) scanUser(ctx context.Context, row *sql.Row) (User, error) {
	u, err := scanUserRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	if err != nil {
		return u, err
	}
	if u.Restricted() {
		if u.MonitorIDs, err = s.userMonitorIDs(ctx, u.ID); err != nil {
			return u, err
		}
		u.ServerIDs, err = s.userServerIDs(ctx, u.ID)
	}
	return u, err
}

func (s *Store) userMonitorIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT monitor_id FROM user_monitors WHERE user_id = ? ORDER BY monitor_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// CreateFirstUser sadece hiç kullanıcı yoksa yönetici oluşturur; aynı anda
// gelen iki kurulum isteğinden yalnızca biri başarılı olur.
func (s *Store) CreateFirstUser(ctx context.Context, username, hash string) (User, error) {
	u := User{Username: username, Role: RoleAdmin, AllMonitors: true, PasswordHash: hash, CreatedAt: time.Now().Unix(), MonitorIDs: []int64{}}
	err := s.tx(ctx, func(tx *Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errors.New("kurulum zaten tamamlanmış")
		}
		var err error
		u.ID, err = insertID(ctx, tx,
			"INSERT INTO users (username, password_hash, role, created_at) VALUES (?, ?, ?, ?)",
			u.Username, u.PasswordHash, u.Role, u.CreatedAt)
		return err
	})
	return u, err
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(ctx, s.db.QueryRowContext(ctx,
		"SELECT "+userCols+" FROM users WHERE LOWER(username) = LOWER(?)", username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(ctx, s.db.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE id = ?", id))
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+userCols+" FROM users ORDER BY LOWER(username)")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUserRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if out[i].Restricted() {
			if out[i].MonitorIDs, err = s.userMonitorIDs(ctx, out[i].ID); err != nil {
				return nil, err
			}
			if out[i].ServerIDs, err = s.userServerIDs(ctx, out[i].ID); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// CreateUser yönetici tarafından eklenen kullanıcı; ilk girişte şifre değişimi zorunlu.
func (s *Store) CreateUser(ctx context.Context, u *User, hash string) error {
	u.CreatedAt = time.Now().Unix()
	u.MustChangePassword = true
	return s.tx(ctx, func(tx *Tx) error {
		var err error
		u.ID, err = insertID(ctx, tx, `
			INSERT INTO users (username, display_name, role, disabled, must_change_password,
				all_monitors, password_hash, created_at)
			VALUES (?, ?, ?, ?, 1, ?, ?, ?)`,
			u.Username, u.DisplayName, u.Role, boolInt(u.Disabled), boolInt(u.AllMonitors), hash, u.CreatedAt)
		if err != nil {
			return err
		}
		if err := setUserMonitors(ctx, tx, u.ID, u.MonitorIDs); err != nil {
			return err
		}
		return setUserServers(ctx, tx, u.ID, u.ServerIDs)
	})
}

// UpdateUser ad, rol, durum ve monitör kısıtını günceller. Değişiklik sonunda
// hiç aktif yönetici kalmayacaksa ErrLastAdmin döner ve hiçbir şey yazılmaz.
// Devre dışı bırakılan kullanıcının oturumları aynı işlemde kapanır.
func (s *Store) UpdateUser(ctx context.Context, u *User) error {
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE users SET display_name = ?, role = ?, disabled = ?, all_monitors = ? WHERE id = ?`,
			u.DisplayName, u.Role, boolInt(u.Disabled), boolInt(u.AllMonitors), u.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if err := ensureAdminLeft(ctx, tx); err != nil {
			return err
		}
		if u.Disabled {
			if _, err := tx.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", u.ID); err != nil {
				return err
			}
		}
		if err := setUserMonitors(ctx, tx, u.ID, u.MonitorIDs); err != nil {
			return err
		}
		return setUserServers(ctx, tx, u.ID, u.ServerIDs)
	})
}

// DeleteUser kullanıcıyı (ve cascade ile oturumlarını) siler; son aktif
// yönetici silinemez.
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM users WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		return ensureAdminLeft(ctx, tx)
	})
}

func ensureAdminLeft(ctx context.Context, tx *Tx) error {
	var n int
	if err := tx.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM users WHERE role = ? AND disabled = 0", RoleAdmin).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

func setUserMonitors(ctx context.Context, tx *Tx, userID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_monitors WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, mid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO user_monitors (user_id, monitor_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, mid); err != nil {
			return err
		}
	}
	return nil
}

// SetPassword şifreyi değiştirir; mustChange yönetici sıfırlamasında true'dur.
func (s *Store) SetPassword(ctx context.Context, userID int64, hash string, mustChange bool) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE users SET password_hash = ?, must_change_password = ? WHERE id = ?",
		hash, boolInt(mustChange), userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdatePassword kullanıcının kendi şifresini değiştirmesi (zorunluluk kalkar).
func (s *Store) UpdatePassword(ctx context.Context, userID int64, hash string) error {
	return s.SetPassword(ctx, userID, hash, false)
}

func (s *Store) TouchLastLogin(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET last_login_at = ? WHERE id = ?", time.Now().Unix(), userID)
	return err
}

// Oturumlar: veritabanında token'ın kendisi değil SHA-256 özeti tutulur.

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		tokenHash, userID, time.Now().Unix(), expiresAt.Unix())
	return err
}

// SessionUser geçerli (süresi dolmamış, kullanıcısı aktif) oturumun kullanıcısını döner.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	return s.scanUser(ctx, s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.display_name, u.role, u.disabled, u.must_change_password,
			u.all_monitors, u.last_login_at, u.created_at, u.password_hash, u.totp_enabled, u.lang
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled = 0`, tokenHash, time.Now().Unix()))
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

// DeleteOtherSessions şifre değişince diğer cihazlardaki oturumları kapatır.
func (s *Store) DeleteOtherSessions(ctx context.Context, userID int64, keepHash string) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM sessions WHERE user_id = ? AND token_hash <> ?", userID, keepHash)
	return err
}

// DeleteUserSessions kullanıcının tüm oturumlarını kapatır.
func (s *Store) DeleteUserSessions(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE user_id = ?", userID)
	return err
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix())
	return err
}
