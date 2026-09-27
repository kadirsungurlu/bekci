package store

import (
	"context"
	"database/sql"
	"time"
)

// AuditEntry işlem kaydı satırı: kim, ne zaman, neyi değiştirdi.
type AuditEntry struct {
	ID         int64  `json:"id"`
	Time       int64  `json:"time"`
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	Action     string `json:"action"`      // ör. monitor.create, user.delete, login.success
	TargetType string `json:"target_type"` // monitor, notification, user, page, settings…
	TargetID   int64  `json:"target_id"`
	TargetName string `json:"target_name"`
	Detail     string `json:"detail"`
	IP         string `json:"ip"`
}

func (s *Store) AddAudit(ctx context.Context, e AuditEntry) error {
	if e.Time == 0 {
		e.Time = time.Now().Unix()
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO audit_log (time, user_id, username, action, target_type, target_id, target_name, detail, ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Time, nullInt(e.UserID), e.Username, e.Action, e.TargetType, nullInt(e.TargetID), e.TargetName, e.Detail, e.IP)
	return err
}

// ListAudit en yeniden eskiye; before > 0 ise o kimlikten küçükler (sayfalama).
func (s *Store) ListAudit(ctx context.Context, before int64, limit int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := "SELECT id, time, user_id, username, action, target_type, target_id, target_name, detail, ip FROM audit_log"
	var args []any
	if before > 0 {
		q += " WHERE id < ?"
		args = append(args, before)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var uid, tid sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Time, &uid, &e.Username, &e.Action, &e.TargetType, &tid, &e.TargetName, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		e.UserID, e.TargetID = uid.Int64, tid.Int64
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteAuditBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBefore(ctx, "DELETE FROM audit_log WHERE time < ?", t)
}

// Secret uygulama genelinde kullanılan rastgele bir sırrı döner; yoksa
// oluşturup kaydeder (ör. şifreli durum sayfası çerezlerinin imzası).
func (s *Store) Secret(ctx context.Context, key string, gen func() string) (string, error) {
	if v, ok, err := s.GetSetting(ctx, key); err != nil || ok {
		return v, err
	}
	v := gen()
	if _, err := s.db.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO NOTHING", key, v); err != nil {
		return "", err
	}
	v2, _, err := s.GetSetting(ctx, key)
	return v2, err
}
