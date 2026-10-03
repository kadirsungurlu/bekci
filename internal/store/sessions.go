package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// 34: oturum / cihaz yönetimi (#13). Oturum kaydı artık giriş yapılan IP'yi,
// tarayıcıyı (User-Agent), son görülme zamanını ve giriş yolunu (password |
// oidc) taşır; kullanıcı Hesabım'da açık oturumlarını görüp tekil ya da
// toplu kapatabilir. Eski kayıtlar boş değerlerle okunur.
func init() {
	RegisterMigration(34, `
ALTER TABLE sessions ADD COLUMN ip TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN user_agent TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN last_seen_at INTEGER;
ALTER TABLE sessions ADD COLUMN via TEXT NOT NULL DEFAULT 'password';
`)
}

// Giriş yolları.
const (
	SessionViaPassword = "password"
	SessionViaOIDC     = "oidc"
)

// Session açık bir oturum (kullanıcıya gösterilen hali; token özeti dışarı
// çıkmaz, ID özetin özetidir).
type Session struct {
	ID         string `json:"id"`
	UserID     int64  `json:"user_id"`
	IP         string `json:"ip"`
	UserAgent  string `json:"user_agent"`
	Via        string `json:"via"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`
	ExpiresAt  int64  `json:"expires_at"`
	Current    bool   `json:"current"`
	hash       string
}

// SessionID token özetinden dışa verilen kimlik (16 onaltılık karakter).
func SessionID(tokenHash string) string {
	sum := sha256.Sum256([]byte("session-id:" + tokenHash))
	return hex.EncodeToString(sum[:8])
}

// SessionInfo yeni oturumun cihaz bilgisi.
type SessionInfo struct {
	IP, UserAgent, Via string
}

// CreateSessionWith oturumu cihaz bilgisiyle açar.
func (s *Store) CreateSessionWith(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time, info SessionInfo) error {
	if info.Via == "" {
		info.Via = SessionViaPassword
	}
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at, ip, user_agent, last_seen_at, via) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		tokenHash, userID, now, expiresAt.Unix(), info.IP, info.UserAgent, now, info.Via)
	return err
}

// TouchSession son görülme zamanını ve adresi yazar.
func (s *Store) TouchSession(ctx context.Context, tokenHash string, now int64, ip string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE sessions SET last_seen_at = ?, ip = ? WHERE token_hash = ?", now, ip, tokenHash)
	return err
}

// ListSessions kullanıcının süresi dolmamış oturumları (en son görülen önce);
// currentHash eşleşen kayıt Current olur.
func (s *Store) ListSessions(ctx context.Context, userID int64, currentHash string, now int64) ([]Session, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT token_hash, user_id, ip, user_agent, via, created_at, last_seen_at, expires_at FROM sessions
		WHERE user_id = ? AND expires_at > ? ORDER BY COALESCE(last_seen_at, created_at) DESC, created_at DESC`, userID, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var se Session
		var last sql.NullInt64
		if err := rows.Scan(&se.hash, &se.UserID, &se.IP, &se.UserAgent, &se.Via, &se.CreatedAt, &last, &se.ExpiresAt); err != nil {
			return nil, err
		}
		se.LastSeenAt = last.Int64
		if se.LastSeenAt == 0 {
			se.LastSeenAt = se.CreatedAt
		}
		se.ID = SessionID(se.hash)
		se.Current = se.hash == currentHash
		out = append(out, se)
	}
	return out, rows.Err()
}

// DeleteSessionByID kullanıcının id'li oturumunu kapatır (yoksa ErrNotFound).
func (s *Store) DeleteSessionByID(ctx context.Context, userID int64, id string) error {
	list, err := s.ListSessions(ctx, userID, "", 0)
	if err != nil {
		return err
	}
	for _, se := range list {
		if se.ID == id {
			_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ? AND user_id = ?", se.hash, userID)
			return err
		}
	}
	return ErrNotFound
}
