package store

import (
	"context"
	"database/sql"
	"errors"
)

// 26: sunucu ajanı genişletmeleri.
//
//   - maintenance.all_servers / maintenance_servers: bakım penceresi
//     sunucuları da kapsar; pencere içinde sunucu uyarısı ve çevrimdışı
//     bildirimi gitmez, yeni uyarı açılmaz.
//   - server_alerts.warn_threshold: isteğe bağlı uyarı eşiği (0 = yok);
//     threshold kritik eşiktir. level: tetiklenmiş kuralın seviyesi
//     ("" | warning | critical). Eski kurallar yalnızca kritik seviyede çalışır.
//   - probe_notifications.level: kanalın bu sunucuda hangi seviyeyi alacağı
//     ("" = hepsi, warning, critical).
//
// Yeni metrikler sütun gerektirmez: "container" (mount = konteyner adı; boş =
// herhangi biri) ve "reboot" (yeniden başlatma bildirimi; eşik yok).
func init() {
	RegisterMigration(26, `
ALTER TABLE maintenance ADD COLUMN all_servers INTEGER NOT NULL DEFAULT 0;
CREATE TABLE maintenance_servers (
	maintenance_id INTEGER NOT NULL REFERENCES maintenance(id) ON DELETE CASCADE,
	probe_id       INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	PRIMARY KEY (maintenance_id, probe_id)
) WITHOUT ROWID;
CREATE INDEX maintenance_servers_probe ON maintenance_servers(probe_id);
ALTER TABLE server_alerts ADD COLUMN warn_threshold DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE server_alerts ADD COLUMN level TEXT NOT NULL DEFAULT '';
ALTER TABLE probe_notifications ADD COLUMN level TEXT NOT NULL DEFAULT '';
`)
}

// Uyarı seviyeleri.
const (
	LevelWarning  = "warning"
	LevelCritical = "critical"
)

// ValidLevel seviye (kanal bağı için "" = hepsi) geçerli mi?
func ValidLevel(l string) bool { return l == "" || l == LevelWarning || l == LevelCritical }

// SetServerAlertLevel tetiklenmiş kuralın seviyesini değiştirir (uyarı ↔ kritik).
func (s *Store) SetServerAlertLevel(ctx context.Context, alertID int64, level string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE server_alerts SET level = ? WHERE id = ?", level, alertID)
	return err
}

// ProbeNotificationBinding sunucuya bağlı kanal ve seviyesi.
type ProbeNotificationBinding struct {
	NotificationID int64  `json:"notification_id"`
	Level          string `json:"level"` // "" = hepsi
}

// ProbeNotificationBindings sunucunun kanal bağları (kimliğe göre sıralı).
func (s *Store) ProbeNotificationBindings(ctx context.Context, probeID int64) ([]ProbeNotificationBinding, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT notification_id, level FROM probe_notifications WHERE probe_id = ? ORDER BY notification_id", probeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProbeNotificationBinding{}
	for rows.Next() {
		var b ProbeNotificationBinding
		if err := rows.Scan(&b.NotificationID, &b.Level); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetProbeNotificationBindings sunucunun kanallarını seviyeleriyle değiştirir.
func (s *Store) SetProbeNotificationBindings(ctx context.Context, probeID int64, bindings []ProbeNotificationBinding) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM probe_notifications WHERE probe_id = ?", probeID); err != nil {
			return err
		}
		for _, b := range bindings {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO probe_notifications (probe_id, notification_id, level) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
				probeID, b.NotificationID, b.Level); err != nil {
				return err
			}
		}
		return nil
	})
}

// ServerMaintenanceLinks bakım pencerelerinin sunucu bağları (pencere → sunucular).
func (s *Store) serverMaintenanceLinks(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT maintenance_id, probe_id FROM maintenance_servers ORDER BY probe_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var mid, pid int64
		if err := rows.Scan(&mid, &pid); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], pid)
	}
	return out, rows.Err()
}

func setMaintenanceServers(ctx context.Context, tx *Tx, id int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM maintenance_servers WHERE maintenance_id = ?", id); err != nil {
		return err
	}
	for _, pid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO maintenance_servers (maintenance_id, probe_id) VALUES (?, ?) ON CONFLICT (maintenance_id, probe_id) DO NOTHING", id, pid); err != nil {
			return err
		}
	}
	return nil
}

// RecordServerEvent anlık bir sunucu olayı (ör. yeniden başlatma) yazar:
// başlangıç ve bitiş aynı andır, kural kimliği yoktur. value olaya özgü
// (yeniden başlatmada yeni açılış zamanı).
func (s *Store) RecordServerEvent(ctx context.Context, probeID int64, metric string, value float64, now int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO server_alert_events (probe_id, alert_id, metric, mount, value, threshold, started_at, ended_at)
		VALUES (?, NULL, ?, '', ?, 0, ?, ?)`, probeID, metric, value, now, now)
	return err
}

// ServerAlertByMetric sunucunun verilen metrikteki (bölümsüz) kuralı; yoksa ErrNotFound.
func (s *Store) ServerAlertByMetric(ctx context.Context, probeID int64, metric string) (ServerAlert, error) {
	a, err := scanServerAlert(s.db.QueryRowContext(ctx,
		"SELECT "+serverAlertCols+" FROM server_alerts WHERE probe_id = ? AND metric = ? AND mount = ''", probeID, metric))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}
