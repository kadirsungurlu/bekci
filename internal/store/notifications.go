package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type Notification struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Config    json.RawMessage `json:"config"`
	IsDefault bool            `json:"is_default"`
	Active    bool            `json:"active"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

const notificationCols = "id, name, type, config, is_default, active, created_at, updated_at"

func scanNotification(sc scanner) (Notification, error) {
	var n Notification
	var cfg string
	err := sc.Scan(&n.ID, &n.Name, &n.Type, &cfg, &n.IsDefault, &n.Active, &n.CreatedAt, &n.UpdatedAt)
	n.Config = json.RawMessage(cfg)
	return n, err
}

func (s *Store) queryNotifications(ctx context.Context, q string, args ...any) ([]Notification, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) ListNotifications(ctx context.Context) ([]Notification, error) {
	return s.queryNotifications(ctx, "SELECT "+notificationCols+" FROM notifications ORDER BY name COLLATE NOCASE, id")
}

// NotificationsForMonitor monitöre bağlı ve etkin bildirim kanalları.
func (s *Store) NotificationsForMonitor(ctx context.Context, monitorID int64) ([]Notification, error) {
	return s.queryNotifications(ctx, `
		SELECT `+notificationCols+` FROM notifications
		WHERE active = 1 AND id IN (SELECT notification_id FROM monitor_notifications WHERE monitor_id = ?)
		ORDER BY id`, monitorID)
}

func (s *Store) GetNotification(ctx context.Context, id int64) (Notification, error) {
	n, err := scanNotification(s.db.QueryRowContext(ctx,
		"SELECT "+notificationCols+" FROM notifications WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	return n, err
}

// CreateNotification kanalı ekler; applyToAll ise mevcut tüm monitörlere bağlar.
func (s *Store) CreateNotification(ctx context.Context, n *Notification, applyToAll bool) error {
	now := time.Now().Unix()
	n.CreatedAt, n.UpdatedAt = now, now
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO notifications (name, type, config, is_default, active, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			n.Name, n.Type, string(n.Config), boolInt(n.IsDefault), boolInt(n.Active), n.CreatedAt, n.UpdatedAt)
		if err != nil {
			return err
		}
		if n.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		return applyNotificationToAll(ctx, tx, n.ID, applyToAll)
	})
}

func (s *Store) UpdateNotification(ctx context.Context, n *Notification, applyToAll bool) error {
	n.UpdatedAt = time.Now().Unix()
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE notifications SET name = ?, type = ?, config = ?, is_default = ?, active = ?, updated_at = ?
			WHERE id = ?`,
			n.Name, n.Type, string(n.Config), boolInt(n.IsDefault), boolInt(n.Active), n.UpdatedAt, n.ID)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrNotFound
		}
		return applyNotificationToAll(ctx, tx, n.ID, applyToAll)
	})
}

func applyNotificationToAll(ctx context.Context, tx *sql.Tx, id int64, apply bool) error {
	if !apply {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO monitor_notifications (monitor_id, notification_id)
		SELECT id, ? FROM monitors`, id)
	return err
}

func (s *Store) DeleteNotification(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM notifications WHERE id = ?", id)
	if err != nil {
		return err
	}
	if c, _ := res.RowsAffected(); c == 0 {
		return ErrNotFound
	}
	return nil
}

// DefaultNotificationIDs yeni monitörlere otomatik bağlanacak kanallar.
func (s *Store) DefaultNotificationIDs(ctx context.Context) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id FROM notifications WHERE is_default = 1 ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ExistingNotificationIDs verilen kimliklerden var olanları döner (doğrulama için).
func (s *Store) ExistingNotificationIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	for _, id := range ids {
		var x int64
		err := s.db.QueryRowContext(ctx, "SELECT id FROM notifications WHERE id = ?", id).Scan(&x)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, nil
}
