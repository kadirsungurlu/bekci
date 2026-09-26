package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Monitor hem ayarları hem de son durumu taşır. Zaman damgaları unix saniye;
// 0 "henüz yok" demektir.
type Monitor struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Description   string          `json:"description"`
	Active        bool            `json:"active"`
	Interval      int             `json:"interval"`
	RetryInterval int             `json:"retry_interval"`
	MaxRetries    int             `json:"max_retries"`
	Timeout       int             `json:"timeout"`
	ResendEvery   int             `json:"resend_every"`
	UpsideDown    bool            `json:"upside_down"`
	Config        json.RawMessage `json:"config"`
	PushToken     string          `json:"push_token,omitempty"`

	Status        int    `json:"status"`
	LastCheckAt   int64  `json:"last_check_at"`
	LastChangeAt  int64  `json:"last_change_at"`
	LastPingMs    int64  `json:"last_ping_ms"`
	LastMessage   string `json:"last_message"`
	CertExpiresAt int64  `json:"cert_expires_at"`
	CertIssuer    string `json:"cert_issuer"`

	CreatedAt int64 `json:"created_at"`
	UpdatedAt int64 `json:"updated_at"`
}

const monitorCols = `id, name, type, description, active, interval_sec, retry_interval_sec,
	max_retries, timeout_sec, resend_every, upside_down, config, push_token, status,
	last_check_at, last_change_at, last_ping_ms, last_message, cert_expires_at, cert_issuer,
	created_at, updated_at`

type scanner interface{ Scan(...any) error }

func scanMonitor(sc scanner) (Monitor, error) {
	var (
		m                                            Monitor
		cfg                                          string
		pushToken                                    sql.NullString
		lastCheck, lastChange, lastPing, certExpires sql.NullInt64
	)
	err := sc.Scan(&m.ID, &m.Name, &m.Type, &m.Description, &m.Active, &m.Interval,
		&m.RetryInterval, &m.MaxRetries, &m.Timeout, &m.ResendEvery, &m.UpsideDown, &cfg,
		&pushToken, &m.Status, &lastCheck, &lastChange, &lastPing, &m.LastMessage,
		&certExpires, &m.CertIssuer, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return m, err
	}
	m.Config = json.RawMessage(cfg)
	m.PushToken = pushToken.String
	m.LastCheckAt = lastCheck.Int64
	m.LastChangeAt = lastChange.Int64
	m.LastPingMs = lastPing.Int64
	m.CertExpiresAt = certExpires.Int64
	return m, nil
}

func (s *Store) ListMonitors(ctx context.Context) ([]Monitor, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+monitorCols+" FROM monitors ORDER BY name COLLATE NOCASE, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetMonitor(ctx context.Context, id int64) (Monitor, error) {
	m, err := scanMonitor(s.db.QueryRowContext(ctx, "SELECT "+monitorCols+" FROM monitors WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) MonitorByPushToken(ctx context.Context, token string) (Monitor, error) {
	m, err := scanMonitor(s.db.QueryRowContext(ctx, "SELECT "+monitorCols+" FROM monitors WHERE push_token = ?", token))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// CreateMonitor yeni monitörü ve bildirim bağlantılarını kaydeder.
func (s *Store) CreateMonitor(ctx context.Context, m *Monitor, notificationIDs []int64) error {
	now := time.Now().Unix()
	m.CreatedAt, m.UpdatedAt = now, now
	m.Status = StatusPending
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			INSERT INTO monitors (name, type, description, active, interval_sec, retry_interval_sec,
				max_retries, timeout_sec, resend_every, upside_down, config, push_token, status,
				created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Name, m.Type, m.Description, boolInt(m.Active), m.Interval, m.RetryInterval,
			m.MaxRetries, m.Timeout, m.ResendEvery, boolInt(m.UpsideDown), string(m.Config),
			nullStr(m.PushToken), m.Status, m.CreatedAt, m.UpdatedAt)
		if err != nil {
			return err
		}
		if m.ID, err = res.LastInsertId(); err != nil {
			return err
		}
		return setMonitorNotifications(ctx, tx, m.ID, notificationIDs)
	})
}

// UpdateMonitor sadece kullanıcının değiştirebileceği alanları günceller.
// Tip veya hedef değiştiyse eski durum/SSL bilgisi anlamsız olacağından
// resetState ile sıfırlanır.
func (s *Store) UpdateMonitor(ctx context.Context, m *Monitor, notificationIDs []int64, resetState bool) error {
	m.UpdatedAt = time.Now().Unix()
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE monitors SET name = ?, type = ?, description = ?, interval_sec = ?,
				retry_interval_sec = ?, max_retries = ?, timeout_sec = ?, resend_every = ?,
				upside_down = ?, config = ?, push_token = ?, updated_at = ?
			WHERE id = ?`,
			m.Name, m.Type, m.Description, m.Interval, m.RetryInterval, m.MaxRetries,
			m.Timeout, m.ResendEvery, boolInt(m.UpsideDown), string(m.Config),
			nullStr(m.PushToken), m.UpdatedAt, m.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if resetState {
			if _, err := tx.ExecContext(ctx, `
				UPDATE monitors SET status = ?, last_message = '', last_ping_ms = NULL,
					cert_expires_at = NULL, cert_issuer = ''
				WHERE id = ?`, StatusPending, m.ID); err != nil {
				return err
			}
		}
		return setMonitorNotifications(ctx, tx, m.ID, notificationIDs)
	})
}

func (s *Store) SetMonitorActive(ctx context.Context, id int64, active bool) error {
	// Durdurulan monitör tekrar başlatıldığında temiz bir "bekliyor" durumundan başlar.
	res, err := s.db.ExecContext(ctx,
		"UPDATE monitors SET active = ?, status = ?, updated_at = ? WHERE id = ?",
		boolInt(active), StatusPending, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteMonitor(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM monitors WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func setMonitorNotifications(ctx context.Context, tx *sql.Tx, monitorID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_notifications WHERE monitor_id = ?", monitorID); err != nil {
		return err
	}
	for _, nid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT OR IGNORE INTO monitor_notifications (monitor_id, notification_id) VALUES (?, ?)",
			monitorID, nid); err != nil {
			return err
		}
	}
	return nil
}

// MonitorNotificationIDs tüm monitörlerin bağlı bildirim kimliklerini döner.
func (s *Store) MonitorNotificationIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT monitor_id, notification_id FROM monitor_notifications ORDER BY notification_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var mid, nid int64
		if err := rows.Scan(&mid, &nid); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], nid)
	}
	return out, rows.Err()
}

// UpdateCert monitörün son görülen SSL sertifika bilgisini yazar.
func (s *Store) UpdateCert(ctx context.Context, id int64, notAfter int64, issuer string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE monitors SET cert_expires_at = ?, cert_issuer = ? WHERE id = ?",
		nullInt(notAfter), issuer, id)
	return err
}

// MarkCertNotice bu sertifika + eşik için daha önce bildirim gönderilmediyse
// kaydeder ve true döner; gönderildiyse false döner.
func (s *Store) MarkCertNotice(ctx context.Context, monitorID, notAfter int64, days int) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT OR IGNORE INTO cert_notices (monitor_id, not_after, days, sent_at) VALUES (?, ?, ?, ?)",
		monitorID, notAfter, days, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// IsUniqueViolation SQLite UNIQUE kısıtı hatasını tanır.
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
