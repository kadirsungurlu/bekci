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

	// Kurallar (migration 23; bkz. notification_rules.go). Events boş = tüm
	// türler; Quiet nil = sessiz saat yok; DelayMin / EscalateMin 0 = kapalı;
	// Lang boş = ayarlardaki bildirim dili.
	Events      []string    `json:"events"`
	Quiet       *QuietHours `json:"quiet_hours"`
	DelayMin    int         `json:"delay_min"`
	EscalateMin int         `json:"escalate_min"`
	Lang        string      `json:"lang"`

	// TagRules etiket kuralları (migration 30; bkz. tag_rules.go): bu
	// etiketi (= değeri) taşıyan her monitör için kanal, açık bağlantıya ek
	// olarak gönderir. Boş liste = yok.
	TagRules []TagRule `json:"tag_rules"`

	// BindLevel sunucu bağında kanalın alacağı uyarı seviyesi ("" = hepsi,
	// warning, critical); yalnızca NotificationsForProbe doldurur, saklanmaz.
	BindLevel string `json:"-"`
}

const notificationCols = "id, name, type, config, is_default, active, created_at, updated_at, events, quiet_hours, delay_min, escalate_min, lang"

func scanNotification(sc scanner) (Notification, error) {
	var n Notification
	var cfg, events, quiet string
	err := sc.Scan(&n.ID, &n.Name, &n.Type, &cfg, &n.IsDefault, &n.Active, &n.CreatedAt, &n.UpdatedAt,
		&events, &quiet, &n.DelayMin, &n.EscalateMin, &n.Lang)
	n.Config = json.RawMessage(cfg)
	n.Events = decodeEvents(events)
	n.Quiet = decodeQuiet(quiet)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	return out, s.attachNotificationTags(ctx, out)
}

func (s *Store) ListNotifications(ctx context.Context) ([]Notification, error) {
	return s.queryNotifications(ctx, "SELECT "+notificationCols+" FROM notifications ORDER BY LOWER(name), id")
}

// NotificationsForMonitor monitöre bağlı ve etkin bildirim kanalları: açık
// bağlantılar ile monitörün etiketlerine uyan etiket kurallarının birleşimi.
func (s *Store) NotificationsForMonitor(ctx context.Context, monitorID int64) ([]Notification, error) {
	return s.queryNotifications(ctx, `
		SELECT `+notificationCols+` FROM notifications
		WHERE active = 1 AND (id IN (SELECT notification_id FROM monitor_notifications WHERE monitor_id = ?)
			OR id IN (SELECT r.notification_id FROM notification_tags r JOIN monitor_tags mt ON `+tagRuleCond+` WHERE mt.monitor_id = ?))
		ORDER BY id`, monitorID, monitorID)
}

func (s *Store) GetNotification(ctx context.Context, id int64) (Notification, error) {
	n, err := scanNotification(s.db.QueryRowContext(ctx,
		"SELECT "+notificationCols+" FROM notifications WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return n, ErrNotFound
	}
	if err != nil {
		return n, err
	}
	list := []Notification{n}
	if err := s.attachNotificationTags(ctx, list); err != nil {
		return n, err
	}
	return list[0], nil
}

// CreateNotification kanalı ekler; applyToAll ise mevcut tüm monitörlere bağlar.
func (s *Store) CreateNotification(ctx context.Context, n *Notification, applyToAll bool) error {
	now := time.Now().Unix()
	n.CreatedAt, n.UpdatedAt = now, now
	return s.tx(ctx, func(tx *Tx) error {
		var err error
		n.ID, err = insertID(ctx, tx, `
			INSERT INTO notifications (name, type, config, is_default, active, created_at, updated_at,
				events, quiet_hours, delay_min, escalate_min, lang)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			n.Name, n.Type, string(n.Config), boolInt(n.IsDefault), boolInt(n.Active), n.CreatedAt, n.UpdatedAt,
			encodeEvents(n.Events), encodeQuiet(n.Quiet), n.DelayMin, n.EscalateMin, n.Lang)
		if err != nil {
			return err
		}
		if err := setTagRulesTx(ctx, tx, "notification_tags", "notification_id", n.ID, n.TagRules); err != nil {
			return err
		}
		return applyNotificationToAll(ctx, tx, n.ID, applyToAll)
	})
}

func (s *Store) UpdateNotification(ctx context.Context, n *Notification, applyToAll bool) error {
	n.UpdatedAt = time.Now().Unix()
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE notifications SET name = ?, type = ?, config = ?, is_default = ?, active = ?, updated_at = ?,
				events = ?, quiet_hours = ?, delay_min = ?, escalate_min = ?, lang = ?
			WHERE id = ?`,
			n.Name, n.Type, string(n.Config), boolInt(n.IsDefault), boolInt(n.Active), n.UpdatedAt,
			encodeEvents(n.Events), encodeQuiet(n.Quiet), n.DelayMin, n.EscalateMin, n.Lang, n.ID)
		if err != nil {
			return err
		}
		if c, _ := res.RowsAffected(); c == 0 {
			return ErrNotFound
		}
		if err := setTagRulesTx(ctx, tx, "notification_tags", "notification_id", n.ID, n.TagRules); err != nil {
			return err
		}
		return applyNotificationToAll(ctx, tx, n.ID, applyToAll)
	})
}

func applyNotificationToAll(ctx context.Context, tx *Tx, id int64, apply bool) error {
	if !apply {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO monitor_notifications (monitor_id, notification_id)
		SELECT id, CAST(? AS BIGINT) FROM monitors WHERE 1 = 1
		ON CONFLICT DO NOTHING`, id) // WHERE: SQLite'ın upsert ayrıştırma belirsizliği için şart
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
