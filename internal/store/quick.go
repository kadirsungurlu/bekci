package store

// Monitör listesinin hızlı işlemleri: kopyalama, istatistik sıfırlama, bildirim
// kanalı ve etiket ekleme/çıkarma (tekli ve toplu), açık olay kimlikleri.

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// OpenIncidentIDs verilen monitörlerin açık (sürmekte olan) olaylarının
// kimlikleri: monitör kimliği → olay kimliği. Açık olayı olmayanlar haritada yok.
func (s *Store) OpenIncidentIDs(ctx context.Context, monitorIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(monitorIDs) == 0 {
		return out, nil
	}
	q, args := inClause(monitorIDs)
	rows, err := s.db.QueryContext(ctx,
		"SELECT monitor_id, MAX(id) FROM incidents WHERE monitor_id IN ("+q+") AND kind = 'monitor' AND resolved_at IS NULL GROUP BY monitor_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, iid int64
		if err := rows.Scan(&mid, &iid); err != nil {
			return nil, err
		}
		out[mid] = iid
	}
	return out, rows.Err()
}

// OpenPartialIncidentIDs açık konum kesintisi (kind = partial) olan
// monitörler: monitör kimliği → olay kimliği. Açık konum kesintisi az olur;
// tablo resolved_at IS NULL koşuluyla taranır.
func (s *Store) OpenPartialIncidentIDs(ctx context.Context) (map[int64]int64, error) {
	out := map[int64]int64{}
	rows, err := s.db.QueryContext(ctx,
		"SELECT monitor_id, MAX(id) FROM incidents WHERE kind = ? AND resolved_at IS NULL AND monitor_id IS NOT NULL GROUP BY monitor_id",
		IncidentPartial)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid, iid int64
		if err := rows.Scan(&mid, &iid); err != nil {
			return nil, err
		}
		out[mid] = iid
	}
	return out, rows.Err()
}

// CloneMonitor kaynak monitörün ayarlarını (gizli alanlar dahil, veritabanı
// içinde kopyalanır), bildirim kanallarını, etiketlerini ve konum ayarını yeni
// bir monitöre kopyalar. Durum ve geçmiş kopyalanmaz; yeni monitör "bekliyor"
// durumunda başlar. pushToken push monitörü için yeni token'dır (diğerlerinde boş).
func (s *Store) CloneMonitor(ctx context.Context, srcID int64, name string, active bool, pushToken string) (int64, error) {
	now := time.Now().Unix()
	var id int64
	err := s.tx(ctx, func(tx *Tx) error {
		var err error
		id, err = insertID(ctx, tx, `
			INSERT INTO monitors (name, type, description, active, interval_sec, retry_interval_sec,
				max_retries, timeout_sec, resend_every, upside_down, config, push_token, status,
				created_at, updated_at)
			SELECT CAST(? AS TEXT), type, description, CAST(? AS BIGINT), interval_sec, retry_interval_sec,
				max_retries, timeout_sec, resend_every, upside_down, config, CAST(? AS TEXT),
				CAST(? AS BIGINT), CAST(? AS BIGINT), CAST(? AS BIGINT)
			FROM monitors WHERE id = ?`,
			name, boolInt(active), nullStr(pushToken), StatusPending, now, now, srcID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// PostgreSQL SELECT listesindeki parametrenin tipini çıkaramaz: CAST ile belirtilir.
		for _, q := range []string{
			"INSERT INTO monitor_notifications (monitor_id, notification_id) SELECT CAST(? AS BIGINT), notification_id FROM monitor_notifications WHERE monitor_id = ?",
			"INSERT INTO monitor_tags (monitor_id, tag_id, value) SELECT CAST(? AS BIGINT), tag_id, value FROM monitor_tags WHERE monitor_id = ?",
			"INSERT INTO monitor_location_settings (monitor_id, include_local, down_when, notify_partial) SELECT CAST(? AS BIGINT), include_local, down_when, notify_partial FROM monitor_location_settings WHERE monitor_id = ?",
			"INSERT INTO monitor_locations (monitor_id, probe_id) SELECT CAST(? AS BIGINT), probe_id FROM monitor_locations WHERE monitor_id = ?",
		} {
			if _, err := tx.ExecContext(ctx, q, id, srcID); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

// ResetMonitorStats monitörün ham kontrollerini, saatlik/günlük özetlerini ve
// bitmiş olaylarını siler. Süren (açık) olay korunur: kesinti devam ediyorsa
// motorun durumu ve "düzeldi" bildirimi bozulmasın. Monitör ayarları ve son
// durumu değişmez.
func (s *Store) ResetMonitorStats(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, q := range []string{
			"DELETE FROM heartbeats WHERE monitor_id = ?",
			"DELETE FROM stats_hourly WHERE monitor_id = ?",
			"DELETE FROM stats_daily WHERE monitor_id = ?",
			"DELETE FROM incidents WHERE monitor_id = ? AND resolved_at IS NOT NULL",
		} {
			if _, err := tx.ExecContext(ctx, q, id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetMonitorNotifications monitörün bildirim kanallarını verilen listeyle değiştirir.
func (s *Store) SetMonitorNotifications(ctx context.Context, monitorID int64, ids []int64) error {
	return s.tx(ctx, func(tx *Tx) error { return setMonitorNotifications(ctx, tx, monitorID, ids) })
}

// AddMonitorsNotification kanalı verilen monitörlere ekler (zaten bağlıysa değişmez).
func (s *Store) AddMonitorsNotification(ctx context.Context, monitorIDs []int64, notificationID int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, mid := range monitorIDs {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO monitor_notifications (monitor_id, notification_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
				mid, notificationID); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveMonitorsNotification kanalı verilen monitörlerden çıkarır.
func (s *Store) RemoveMonitorsNotification(ctx context.Context, monitorIDs []int64, notificationID int64) error {
	if len(monitorIDs) == 0 {
		return nil
	}
	q, args := inClause(monitorIDs)
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM monitor_notifications WHERE notification_id = ? AND monitor_id IN ("+q+")",
		append([]any{notificationID}, args...)...)
	return err
}

// AddMonitorsTag etiketi (değeriyle) verilen monitörlere ekler; aynı etiket ve
// değer zaten varsa değişmez.
func (s *Store) AddMonitorsTag(ctx context.Context, monitorIDs []int64, tagID int64, value string) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, mid := range monitorIDs {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO monitor_tags (monitor_id, tag_id, value) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
				mid, tagID, value); err != nil {
				return err
			}
		}
		return nil
	})
}

// RemoveMonitorsTag etiketi (tüm değerleriyle) verilen monitörlerden kaldırır.
func (s *Store) RemoveMonitorsTag(ctx context.Context, monitorIDs []int64, tagID int64) error {
	if len(monitorIDs) == 0 {
		return nil
	}
	q, args := inClause(monitorIDs)
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM monitor_tags WHERE tag_id = ? AND monitor_id IN ("+q+")",
		append([]any{tagID}, args...)...)
	return err
}
