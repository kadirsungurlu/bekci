package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 10: sunucu takibi (docs/PLAN.md §12). Kontrol noktası ajanı ("uptime probe")
// sunucunun metriklerini de gönderir; yeni bir varlık yoktur, probes tablosu
// "ajan" olur.
//
//   - probes.metrics: metrik toplama açık mı (varsayılan açık); host_info son
//     gelen host bilgisi (JSON); metrics_at son örneğin zamanı; metrics_note
//     ajanın "toplayamıyorum" nedeni (boşsa sorun yok).
//   - server_stats: örnekler ve özetleri. res=1 dakikalık örnek, res=10 ve
//     res=60 dakikalık ortalamalar; data metrics.Stats JSON'udur.
//   - server_alerts: eşik kuralları; firing/fired_at kalıcıdır, yeniden
//     başlatmada aynı uyarı tekrar bildirilmez.
//   - server_alert_events: uyarı geçmişi. alert_id bilerek yabancı anahtar
//     değildir: kural silinse de geçmiş kalır.
//   - probe_notifications: sunucu uyarılarının gideceği kanallar.
func init() {
	RegisterMigration(10, `
ALTER TABLE probes ADD COLUMN kind TEXT NOT NULL DEFAULT 'location';
ALTER TABLE probes ADD COLUMN metrics INTEGER NOT NULL DEFAULT 1;
ALTER TABLE probes ADD COLUMN host_info TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN metrics_at INTEGER;
ALTER TABLE probes ADD COLUMN metrics_note TEXT NOT NULL DEFAULT '';

CREATE TABLE server_stats (
	probe_id INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	res      INTEGER NOT NULL,
	time     INTEGER NOT NULL,
	data     TEXT    NOT NULL,
	PRIMARY KEY (probe_id, res, time)
) WITHOUT ROWID;
CREATE INDEX server_stats_res_time ON server_stats(res, time);

CREATE TABLE server_alerts (
	id        INTEGER PRIMARY KEY,
	probe_id  INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	metric    TEXT    NOT NULL,
	threshold DOUBLE PRECISION NOT NULL DEFAULT 0,
	minutes   INTEGER NOT NULL DEFAULT 1,
	active    INTEGER NOT NULL DEFAULT 1,
	firing    INTEGER NOT NULL DEFAULT 0,
	fired_at  INTEGER,
	mount     TEXT    NOT NULL DEFAULT '',
	UNIQUE (probe_id, metric, mount)
);

CREATE TABLE server_alert_events (
	id         INTEGER PRIMARY KEY,
	probe_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	alert_id   INTEGER,
	metric     TEXT    NOT NULL,
	value      DOUBLE PRECISION NOT NULL DEFAULT 0,
	threshold  DOUBLE PRECISION NOT NULL DEFAULT 0,
	started_at INTEGER NOT NULL,
	ended_at   INTEGER,
	mount      TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX server_alert_events_probe ON server_alert_events(probe_id, started_at);
CREATE INDEX server_alert_events_started ON server_alert_events(started_at);

CREATE TABLE probe_notifications (
	probe_id        INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	PRIMARY KEY (probe_id, notification_id)
) WITHOUT ROWID;
`)
}

// Özet çözünürlükleri (dakika).
const (
	ServerRes1  = 1
	ServerRes10 = 10
	ServerRes60 = 60
)

// SetProbeMetrics ajanın metrik toplamasını açar/kapatır.
func (s *Store) SetProbeMetrics(ctx context.Context, id int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET metrics = ? WHERE id = ?", boolInt(enabled), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ServerSample alınan bir örnek: Time dakikaya yuvarlanmış satır zamanı,
// At gerçek alış zamanı (unix saniye); HostInfo boşsa eski değer korunur.
type ServerSample struct {
	ProbeID  int64
	Time     int64
	At       int64
	Data     []byte
	HostInfo string
}

// SaveServerSample 1 dakikalık örneği yazar (aynı dakikadaki önceki örneğin
// yerine geçer), ajanın son örnek zamanını günceller ve "toplayamıyorum"
// notunu temizler. first: ajanın ilk örneği mi (metrics_at daha önce boştu).
func (s *Store) SaveServerSample(ctx context.Context, in ServerSample) (first bool, err error) {
	err = s.tx(ctx, func(tx *Tx) error {
		var prev sql.NullInt64
		err := tx.QueryRowContext(ctx, "SELECT metrics_at FROM probes WHERE id = ?", in.ProbeID).Scan(&prev)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		first = !prev.Valid
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO server_stats (probe_id, res, time, data) VALUES (?, 1, ?, ?)
			ON CONFLICT (probe_id, res, time) DO UPDATE SET data = excluded.data`,
			in.ProbeID, in.Time, string(in.Data)); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE probes SET metrics_at = ?, metrics_note = '',
				host_info = CASE WHEN ? = '' THEN host_info ELSE ? END
			WHERE id = ?`, in.At, in.HostInfo, in.HostInfo, in.ProbeID)
		return err
	})
	return first, err
}

// SetProbeMetricsNote ajanın "metrik toplayamıyorum" nedenini yazar
// (metrics_at değişmez: veri gelmiyor sayılır). hostInfo boşsa eski değer korunur.
func (s *Store) SetProbeMetricsNote(ctx context.Context, id int64, note, hostInfo string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE probes SET metrics_note = ?, host_info = CASE WHEN ? = '' THEN host_info ELSE ? END
		WHERE id = ?`, note, hostInfo, hostInfo, id)
	return err
}

// UpsertServerStats bir özet satırını yazar (tekrar yazmak zararsız).
func (s *Store) UpsertServerStats(ctx context.Context, probeID int64, res int, t int64, data []byte) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_stats (probe_id, res, time, data) VALUES (?, ?, ?, ?)
		ON CONFLICT (probe_id, res, time) DO UPDATE SET data = excluded.data`,
		probeID, res, t, string(data))
	return err
}

// StatRow server_stats satırı.
type StatRow struct {
	Time int64
	Data []byte
}

// ServerStats [from, to) aralığındaki satırlar, zamana göre sıralı.
func (s *Store) ServerStats(ctx context.Context, probeID int64, res int, from, to int64) ([]StatRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT time, data FROM server_stats
		WHERE probe_id = ? AND res = ? AND time >= ? AND time < ?
		ORDER BY time`, probeID, res, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatRow{}
	for rows.Next() {
		var r StatRow
		var data string
		if err := rows.Scan(&r.Time, &data); err != nil {
			return nil, err
		}
		r.Data = []byte(data)
		out = append(out, r)
	}
	return out, rows.Err()
}

// LastServerStat before'dan önceki son 1 dakikalık örnek; yoksa ErrNotFound.
func (s *Store) LastServerStat(ctx context.Context, probeID, before int64) (StatRow, error) {
	var r StatRow
	var data string
	err := s.db.QueryRowContext(ctx, `
		SELECT time, data FROM server_stats WHERE probe_id = ? AND res = 1 AND time < ?
		ORDER BY time DESC LIMIT 1`, probeID, before).Scan(&r.Time, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	r.Data = []byte(data)
	return r, err
}

// Uyarı kuralları --------------------------------------------------------------------

// ServerAlert bir eşik kuralı. Threshold offline için kullanılmaz; Minutes
// ortalama penceresi (offline: çevrimdışı kalma süresi). FiredAt 0: yok.
type ServerAlert struct {
	ID        int64   `json:"id"`
	ProbeID   int64   `json:"-"`
	Metric    string  `json:"metric"`
	Threshold float64 `json:"threshold"`
	Minutes   int     `json:"minutes"`
	Mount     string  `json:"mount"` // disk: bölüm ("" = herhangi bir bölüm, en dolusu)
	Active    bool    `json:"active"`
	Firing    bool    `json:"firing"`
	FiredAt   int64   `json:"fired_at"`
}

const serverAlertCols = "id, probe_id, metric, threshold, minutes, active, firing, fired_at, mount"

func scanServerAlert(sc scanner) (ServerAlert, error) {
	var a ServerAlert
	var fired sql.NullInt64
	err := sc.Scan(&a.ID, &a.ProbeID, &a.Metric, &a.Threshold, &a.Minutes, &a.Active, &a.Firing, &fired, &a.Mount)
	a.FiredAt = fired.Int64
	return a, err
}

func queryServerAlerts(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, query string, args ...any) ([]ServerAlert, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerAlert{}
	for rows.Next() {
		a, err := scanServerAlert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ServerAlerts ajanın kuralları (kimliğe göre sıralı).
func (s *Store) ServerAlerts(ctx context.Context, probeID int64) ([]ServerAlert, error) {
	return queryServerAlerts(ctx, s.db, "SELECT "+serverAlertCols+" FROM server_alerts WHERE probe_id = ? ORDER BY id", probeID)
}

// AllServerAlerts tüm ajanların kuralları, ajana göre gruplu.
func (s *Store) AllServerAlerts(ctx context.Context) (map[int64][]ServerAlert, error) {
	list, err := queryServerAlerts(ctx, s.db, "SELECT "+serverAlertCols+" FROM server_alerts ORDER BY probe_id, id")
	if err != nil {
		return nil, err
	}
	out := map[int64][]ServerAlert{}
	for _, a := range list {
		out[a.ProbeID] = append(out[a.ProbeID], a)
	}
	return out, nil
}

// ReplaceServerAlerts ajanın kurallarını verilen listeyle değiştirir. Aynı
// metriğin kuralı yerinde güncellenir (kimliği ve tetiklenmiş durumu korunur,
// eşik değişince uyarı yeniden bildirilmez); listede olmayan kurallar silinir.
// Silinen veya pasifleşen tetiklenmiş kuralın açık geçmiş kaydı now ile kapanır.
// Metrikler tekil olmalı (çağıran doğrular).
func (s *Store) ReplaceServerAlerts(ctx context.Context, probeID int64, rules []ServerAlert, now int64) ([]ServerAlert, error) {
	var out []ServerAlert
	err := s.tx(ctx, func(tx *Tx) error {
		old, err := queryServerAlerts(ctx, tx, "SELECT "+serverAlertCols+" FROM server_alerts WHERE probe_id = ?", probeID)
		if err != nil {
			return err
		}
		// Kural metrik + bölümle tanınır: aynı kural güncellenince tetiklenme
		// durumu korunur.
		key := func(a ServerAlert) string { return a.Metric + "\x00" + a.Mount }
		byKey := map[string]ServerAlert{}
		for _, a := range old {
			byKey[key(a)] = a
		}
		keep := map[int64]bool{}
		for _, r := range rules {
			if a, ok := byKey[key(r)]; ok {
				keep[a.ID] = true
				if a.Firing && !r.Active {
					if err := resolveAlertTx(ctx, tx, a.ID, now); err != nil {
						return err
					}
				}
				if _, err := tx.ExecContext(ctx,
					"UPDATE server_alerts SET threshold = ?, minutes = ?, active = ? WHERE id = ?",
					r.Threshold, r.Minutes, boolInt(r.Active), a.ID); err != nil {
					return err
				}
				continue
			}
			if _, err := insertID(ctx, tx, `
				INSERT INTO server_alerts (probe_id, metric, mount, threshold, minutes, active, firing)
				VALUES (?, ?, ?, ?, ?, ?, 0)`, probeID, r.Metric, r.Mount, r.Threshold, r.Minutes, boolInt(r.Active)); err != nil {
				return err
			}
		}
		for _, a := range old {
			if keep[a.ID] {
				continue
			}
			if a.Firing {
				if err := resolveAlertTx(ctx, tx, a.ID, now); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM server_alerts WHERE id = ?", a.ID); err != nil {
				return err
			}
		}
		out, err = queryServerAlerts(ctx, tx, "SELECT "+serverAlertCols+" FROM server_alerts WHERE probe_id = ? ORDER BY id", probeID)
		return err
	})
	return out, err
}

// FireServerAlert kuralı tetiklenmiş olarak işaretler ve geçmişe yeni kayıt
// açar. Kural zaten tetiklenmişse (veya yoksa) hiçbir şey yapmaz, false döner.
func (s *Store) FireServerAlert(ctx context.Context, a ServerAlert, value float64, mount string, now int64) (bool, error) {
	fired := false
	err := s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE server_alerts SET firing = 1, fired_at = ? WHERE id = ? AND firing = 0", now, a.ID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		fired = true
		_, err = insertID(ctx, tx, `
			INSERT INTO server_alert_events (probe_id, alert_id, metric, mount, value, threshold, started_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`, a.ProbeID, a.ID, a.Metric, mount, value, a.Threshold, now)
		return err
	})
	return fired, err
}

// ResolveServerAlert tetiklenmiş kuralı bitirir ve açık geçmiş kaydını
// kapatır. Kural tetiklenmiş değilse false döner.
func (s *Store) ResolveServerAlert(ctx context.Context, alertID, now int64) (bool, error) {
	resolved := false
	err := s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx,
			"UPDATE server_alerts SET firing = 0, fired_at = NULL WHERE id = ? AND firing = 1", alertID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		resolved = true
		return resolveAlertTx(ctx, tx, alertID, now)
	})
	return resolved, err
}

// OpenServerAlertMount kuralın süren uyarı kaydındaki bölüm (disk uyarısı
// başladığında dolan bölüm); süren kayıt yoksa "".
func (s *Store) OpenServerAlertMount(ctx context.Context, alertID int64) (string, error) {
	var mount string
	err := s.db.QueryRowContext(ctx, `
		SELECT mount FROM server_alert_events WHERE alert_id = ? AND ended_at IS NULL
		ORDER BY id DESC LIMIT 1`, alertID).Scan(&mount)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return mount, err
}

func resolveAlertTx(ctx context.Context, tx *Tx, alertID, now int64) error {
	if _, err := tx.ExecContext(ctx,
		"UPDATE server_alerts SET firing = 0, fired_at = NULL WHERE id = ?", alertID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		"UPDATE server_alert_events SET ended_at = ? WHERE alert_id = ? AND ended_at IS NULL", now, alertID)
	return err
}

// ServerAlertEvent uyarı geçmişindeki bir kayıt. EndedAt 0: sürüyor.
type ServerAlertEvent struct {
	ID        int64   `json:"id"`
	ProbeID   int64   `json:"-"`
	AlertID   int64   `json:"-"`
	Metric    string  `json:"metric"`
	Mount     string  `json:"mount"` // disk uyarısında dolan bölüm
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	StartedAt int64   `json:"started_at"`
	EndedAt   int64   `json:"ended_at"`
}

// ServerAlertEvents ajanın since sonrası başlayan uyarıları, yeniden eskiye.
func (s *Store) ServerAlertEvents(ctx context.Context, probeID, since int64, limit int) ([]ServerAlertEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, probe_id, alert_id, metric, mount, value, threshold, started_at, ended_at
		FROM server_alert_events WHERE probe_id = ? AND started_at >= ?
		ORDER BY started_at DESC, id DESC LIMIT ?`, probeID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ServerAlertEvent{}
	for rows.Next() {
		var e ServerAlertEvent
		var alertID, ended sql.NullInt64
		if err := rows.Scan(&e.ID, &e.ProbeID, &alertID, &e.Metric, &e.Mount, &e.Value, &e.Threshold, &e.StartedAt, &ended); err != nil {
			return nil, err
		}
		e.AlertID, e.EndedAt = alertID.Int64, ended.Int64
		out = append(out, e)
	}
	return out, rows.Err()
}

// InitServerDefaults ajanın hiç kuralı yoksa verilen varsayılan kuralları,
// hiç bildirim kanalı yoksa varsayılan (is_default) kanalları ekler.
func (s *Store) InitServerDefaults(ctx context.Context, probeID int64, rules []ServerAlert) error {
	return s.tx(ctx, func(tx *Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM server_alerts WHERE probe_id = ?", probeID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			for _, r := range rules {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
					VALUES (?, ?, ?, ?, ?, 0)`, probeID, r.Metric, r.Threshold, r.Minutes, boolInt(r.Active)); err != nil {
					return err
				}
			}
		}
		return attachDefaultProbeNotifications(ctx, tx, probeID)
	})
}

// Bildirim kanalları -----------------------------------------------------------------

// AttachDefaultProbeNotifications ajanın hiç kanalı yoksa varsayılan kanalları bağlar.
func (s *Store) AttachDefaultProbeNotifications(ctx context.Context, probeID int64) error {
	return s.tx(ctx, func(tx *Tx) error { return attachDefaultProbeNotifications(ctx, tx, probeID) })
}

func attachDefaultProbeNotifications(ctx context.Context, tx *Tx, probeID int64) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM probe_notifications WHERE probe_id = ?", probeID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO probe_notifications (probe_id, notification_id)
		SELECT CAST(? AS BIGINT), id FROM notifications WHERE is_default = 1
		ON CONFLICT DO NOTHING`, probeID)
	return err
}

// ProbeNotificationIDs ajana bağlı kanalların kimlikleri.
func (s *Store) ProbeNotificationIDs(ctx context.Context, probeID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT notification_id FROM probe_notifications WHERE probe_id = ? ORDER BY notification_id", probeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetProbeNotifications ajanın kanallarını verilen listeyle değiştirir.
func (s *Store) SetProbeNotifications(ctx context.Context, probeID int64, ids []int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM probe_notifications WHERE probe_id = ?", probeID); err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO probe_notifications (probe_id, notification_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
				probeID, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// NotificationsForProbe ajana bağlı ve etkin bildirim kanalları.
func (s *Store) NotificationsForProbe(ctx context.Context, probeID int64) ([]Notification, error) {
	return s.queryNotifications(ctx, `
		SELECT `+notificationCols+` FROM notifications
		WHERE active = 1 AND id IN (SELECT notification_id FROM probe_notifications WHERE probe_id = ?)
		ORDER BY id`, probeID)
}

// NotificationNames kimlik → ad (işlem kaydı metinleri için).
func (s *Store) NotificationNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, "SELECT id, name FROM notifications WHERE id IN ("+ph+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}
