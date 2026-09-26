package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *Store) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// CreateFirstUser sadece hiç kullanıcı yoksa kullanıcı oluşturur; aynı anda
// gelen iki kurulum isteğinden yalnızca biri başarılı olur.
func (s *Store) CreateFirstUser(ctx context.Context, username, hash string) (User, error) {
	u := User{Username: username, PasswordHash: hash, CreatedAt: time.Now().Unix()}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return errors.New("kurulum zaten tamamlanmış")
		}
		res, err := tx.ExecContext(ctx,
			"INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)",
			u.Username, u.PasswordHash, u.CreatedAt)
		if err != nil {
			return err
		}
		u.ID, err = res.LastInsertId()
		return err
	})
	return u, err
}

func (s *Store) scanUser(row *sql.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByName(ctx context.Context, username string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, created_at FROM users WHERE username = ?", username))
}

func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx,
		"SELECT id, username, password_hash, created_at FROM users WHERE id = ?", id))
}

func (s *Store) UpdatePassword(ctx context.Context, userID int64, hash string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE users SET password_hash = ? WHERE id = ?", hash, userID)
	return err
}

// Oturumlar: veritabanında token'ın kendisi değil SHA-256 özeti tutulur.

func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, expiresAt time.Time) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions (token_hash, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)",
		tokenHash, userID, time.Now().Unix(), expiresAt.Unix())
	return err
}

// SessionUser geçerli (süresi dolmamış) oturumun kullanıcısını döner.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT u.id, u.username, u.password_hash, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, time.Now().Unix()))
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

func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", time.Now().Unix())
	return err
}
