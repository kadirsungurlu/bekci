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

	// Yavaş yanıt uyarısı (migration 22): son SlowChecks kontrolün ortalama
	// yanıt süresi SlowMs'yi aşarsa "yavaş" (degraded olayı + 🟡 bildirim);
	// 0 = kapalı. Slow şu anki durumu (runner yazar); uptime etkilenmez.
	SlowMs     int  `json:"slow_ms"`
	SlowChecks int  `json:"slow_checks"`
	Slow       bool `json:"slow"`

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
	created_at, updated_at, slow_ms, slow_checks, slow`

func init() {
	// 22: yavaş yanıt uyarısı (monitör başına eşik ve pencere; varsayılan kapalı).
	RegisterMigration(22, `
ALTER TABLE monitors ADD COLUMN slow_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE monitors ADD COLUMN slow_checks INTEGER NOT NULL DEFAULT 3;
ALTER TABLE monitors ADD COLUMN slow INTEGER NOT NULL DEFAULT 0;
`)
}

// DefaultSlowChecks eşik verilip pencere verilmezse kullanılan kontrol sayısı.
const DefaultSlowChecks = 3

// SetMonitorSlow monitörün "yavaş" durumunu yazar (runner).
func (s *Store) SetMonitorSlow(ctx context.Context, id int64, slow bool) error {
	_, err := s.db.ExecContext(ctx, "UPDATE monitors SET slow = ? WHERE id = ?", boolInt(slow), id)
	return err
}

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
		&certExpires, &m.CertIssuer, &m.CreatedAt, &m.UpdatedAt, &m.SlowMs, &m.SlowChecks, &m.Slow)
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
	rows, err := s.db.QueryContext(ctx, "SELECT "+monitorCols+" FROM monitors ORDER BY LOWER(name), id")
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

// SetPushToken push monitörünün adresini (token) değiştirir; eskisi hemen geçersiz olur.
func (s *Store) SetPushToken(ctx context.Context, id int64, token string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE monitors SET push_token = ?, updated_at = ? WHERE id = ?", token, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
	return s.tx(ctx, func(tx *Tx) error {
		var err error
		m.ID, err = insertID(ctx, tx, `
			INSERT INTO monitors (name, type, description, active, interval_sec, retry_interval_sec,
				max_retries, timeout_sec, resend_every, upside_down, config, push_token, status,
				created_at, updated_at, slow_ms, slow_checks)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			m.Name, m.Type, m.Description, boolInt(m.Active), m.Interval, m.RetryInterval,
			m.MaxRetries, m.Timeout, m.ResendEvery, boolInt(m.UpsideDown), string(m.Config),
			nullStr(m.PushToken), m.Status, m.CreatedAt, m.UpdatedAt, m.SlowMs, slowChecksOr(m.SlowChecks))
		if err != nil {
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
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE monitors SET name = ?, type = ?, description = ?, interval_sec = ?,
				retry_interval_sec = ?, max_retries = ?, timeout_sec = ?, resend_every = ?,
				upside_down = ?, config = ?, push_token = ?, updated_at = ?, slow_ms = ?, slow_checks = ?
			WHERE id = ?`,
			m.Name, m.Type, m.Description, m.Interval, m.RetryInterval, m.MaxRetries,
			m.Timeout, m.ResendEvery, boolInt(m.UpsideDown), string(m.Config),
			nullStr(m.PushToken), m.UpdatedAt, m.SlowMs, slowChecksOr(m.SlowChecks), m.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if resetState {
			if _, err := tx.ExecContext(ctx, `
				UPDATE monitors SET status = ?, last_message = '', last_ping_ms = NULL,
					last_change_at = NULL, cert_expires_at = NULL, cert_issuer = ''
				WHERE id = ?`, StatusPending, m.ID); err != nil {
				return err
			}
		}
		return setMonitorNotifications(ctx, tx, m.ID, notificationIDs)
	})
}

func (s *Store) SetMonitorActive(ctx context.Context, id int64, active bool) error {
	// Durdurulan monitör tekrar başlatıldığında temiz bir "bekliyor" durumundan
	// başlar; eski "şu andan beri" zamanı yeni duruma taşınmaz. Sertifika
	// bilgisi de silinir: son kontrol zamanı olmayan monitörde (ör. durdurulan)
	// eski kontrolün sertifikası kalmasın; ilk kontrol yeniden yazar.
	res, err := s.db.ExecContext(ctx,
		"UPDATE monitors SET active = ?, status = ?, last_change_at = NULL, last_check_at = NULL, last_message = '', last_ping_ms = NULL, cert_expires_at = NULL, cert_issuer = '', updated_at = ? WHERE id = ?",
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

func setMonitorNotifications(ctx context.Context, tx *Tx, monitorID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_notifications WHERE monitor_id = ?", monitorID); err != nil {
		return err
	}
	for _, nid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO monitor_notifications (monitor_id, notification_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
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
		"INSERT INTO cert_notices (monitor_id, not_after, days, sent_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING",
		monitorID, notAfter, days, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// slowChecksOr yavaş yanıt penceresi (0 → varsayılan).
func slowChecksOr(n int) int {
	if n <= 0 {
		return DefaultSlowChecks
	}
	return n
}

// IsUniqueViolation SQLite ve PostgreSQL'in UNIQUE kısıtı hatasını tanır.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || strings.Contains(msg, "SQLSTATE 23505")
}
