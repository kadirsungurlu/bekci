package store

import (
	"context"
	"database/sql"
	"errors"
)

// 5: API anahtarları. Anahtarın kendisi değil SHA-256 özeti tutulur; prefix
// arayüzde anahtarı tanımak için gösterilen ilk karakterlerdir.
func init() {
	RegisterMigration(5, `
CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name         TEXT    NOT NULL,
	prefix       TEXT    NOT NULL,
	key_hash     TEXT    NOT NULL UNIQUE,
	role         TEXT    NOT NULL,
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER,
	expires_at   INTEGER,
	revoked_at   INTEGER
);
CREATE INDEX api_keys_user ON api_keys(user_id);
`)
}

// APIKey kullanıcıya ait bir API anahtarı. Role anahtarın yetki üst sınırıdır;
// kullanımda sahibin o anki rolünü aşamaz. Zaman damgaları unix saniye, 0 "yok".
type APIKey struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"` // sahibin kullanıcı adı
	Name       string `json:"name"`
	Prefix     string `json:"prefix"`
	Role       string `json:"role"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	ExpiresAt  int64  `json:"expires_at"` // 0: süresiz
	RevokedAt  int64  `json:"revoked_at"` // 0: geçerli
	Hash       string `json:"-"`
}

// Usable anahtar iptal edilmemiş ve süresi dolmamışsa true.
func (k APIKey) Usable(now int64) bool {
	return k.RevokedAt == 0 && (k.ExpiresAt == 0 || now < k.ExpiresAt)
}

const apiKeyCols = `k.id, k.user_id, u.username, k.name, k.prefix, k.role, k.created_at,
	k.last_used_at, k.expires_at, k.revoked_at, k.key_hash`

func scanAPIKey(sc scanner) (APIKey, error) {
	var k APIKey
	var used, exp, rev sql.NullInt64
	err := sc.Scan(&k.ID, &k.UserID, &k.Username, &k.Name, &k.Prefix, &k.Role, &k.CreatedAt,
		&used, &exp, &rev, &k.Hash)
	k.LastUsedAt, k.ExpiresAt, k.RevokedAt = used.Int64, exp.Int64, rev.Int64
	return k, err
}

func (s *Store) getAPIKey(ctx context.Context, where string, arg any) (APIKey, error) {
	k, err := scanAPIKey(s.db.QueryRowContext(ctx,
		"SELECT "+apiKeyCols+" FROM api_keys k JOIN users u ON u.id = k.user_id WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return k, ErrNotFound
	}
	return k, err
}

func (s *Store) GetAPIKey(ctx context.Context, id int64) (APIKey, error) {
	return s.getAPIKey(ctx, "k.id = ?", id)
}

// APIKeyByHash anahtarı özetinden bulur (kimlik doğrulama).
func (s *Store) APIKeyByHash(ctx context.Context, hash string) (APIKey, error) {
	return s.getAPIKey(ctx, "k.key_hash = ?", hash)
}

// ListAPIKeys userID'nin anahtarları; userID 0 ise herkesinki. En yeni önce.
func (s *Store) ListAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	q := "SELECT " + apiKeyCols + " FROM api_keys k JOIN users u ON u.id = k.user_id"
	var args []any
	if userID != 0 {
		q += " WHERE k.user_id = ?"
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY k.id DESC", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		k, err := scanAPIKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// CountUsableAPIKeys kullanıcının geçerli (iptal edilmemiş, süresi dolmamış) anahtar sayısı.
func (s *Store) CountUsableAPIKeys(ctx context.Context, userID, now int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM api_keys
		WHERE user_id = ? AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?)`,
		userID, now).Scan(&n)
	return n, err
}

func (s *Store) CreateAPIKey(ctx context.Context, k *APIKey) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO api_keys (user_id, name, prefix, key_hash, role, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		k.UserID, k.Name, k.Prefix, k.Hash, k.Role, k.CreatedAt, nullInt(k.ExpiresAt)).Scan(&k.ID)
}

// RevokeAPIKey anahtarı iptal eder; zaten iptal edilmişse ilk iptal zamanı korunur.
func (s *Store) RevokeAPIKey(ctx context.Context, id, now int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE api_keys SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?", now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchAPIKey son kullanım zamanını yazar (çağıran seyreltir).
func (s *Store) TouchAPIKey(ctx context.Context, id, now int64) error {
	_, err := s.db.ExecContext(ctx, "UPDATE api_keys SET last_used_at = ? WHERE id = ?", now, id)
	return err
}

// UpDown bir zaman aralığındaki başarılı/başarısız kontrol sayıları.
type UpDown struct{ Up, Down int64 }

// UptimeCounts tüm monitörlerin since'den itibaren saatlik özetlerden toplanan
// sayıları (Prometheus metrikleri için tek sorgu). since saat başına yuvarlanır,
// Uptime ile aynı pencere.
func (s *Store) UptimeCounts(ctx context.Context, since int64) (map[int64]UpDown, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT monitor_id, SUM(up), SUM(down) FROM stats_hourly WHERE bucket >= ? GROUP BY monitor_id",
		hourCeil(since))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]UpDown{}
	for rows.Next() {
		var id int64
		var c UpDown
		if err := rows.Scan(&id, &c.Up, &c.Down); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}
