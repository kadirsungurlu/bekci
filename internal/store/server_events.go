package store

// Sunucu olay zaman çizelgesi (iş 660, tasarım §2.2, §3.2): servis ve konteyner
// değişimleri, OOM, yeniden başlatma, uyarı açılış/kapanışı. Yalnız ekleme; metin
// saklanmaz, arayüz kind + data'dan seçili dilde üretir. TimescaleDB kipinde
// hypertable (7 gün parça, 30 gün sonra sıkıştırma, 365 gün saklama); diğer
// kiplerde saklama Go bakımıyla (PruneTimelineEvents). Birincil anahtar
// (zaman, tür, konu) ajanın tekrar gönderdiği olayı eler.

import (
	"context"
	"encoding/json"
	"strings"
)

const serverEventsKeepSec = 365 * 86400

func init() {
	RegisterMigration(46, `
CREATE TABLE server_events (
	probe_id INTEGER NOT NULL,
	time     INTEGER NOT NULL,
	kind     TEXT    NOT NULL,
	subject  TEXT    NOT NULL DEFAULT '',
	level    TEXT    NOT NULL DEFAULT 'info',
	data     TEXT    NOT NULL DEFAULT '{}',
	PRIMARY KEY (probe_id, time, kind, subject)
);
CREATE INDEX server_events_time ON server_events(time);
INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
	SELECT DISTINCT probe_id, 'disk_forecast', 7, 1, 1, 0 FROM server_alerts;
INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
	SELECT DISTINCT probe_id, 'disk_fast', 5, 60, 1, 0 FROM server_alerts WHERE metric = 'disk_forecast';
`)
}

// Olay seviyeleri (arayüzde renk).
const (
	EventInfo    = "info"
	EventWarning = "warning"
	EventError   = "error"
)

// TimelineEvent zaman çizelgesi kaydı.
type TimelineEvent struct {
	ProbeID int64           `json:"-"`
	Time    int64           `json:"time"`
	Kind    string          `json:"kind"`
	Subject string          `json:"subject"`
	Level   string          `json:"level"`
	Data    json.RawMessage `json:"data"`
}

// AddTimelineEvents olayları tek işlemde yazar; aynı anahtarlı kayıt atlanır.
func (s *Store) AddTimelineEvents(ctx context.Context, list []TimelineEvent) error {
	if len(list) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *Tx) error {
		for _, e := range list {
			data := e.Data
			if len(data) == 0 {
				data = json.RawMessage("{}")
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO server_events (probe_id, time, kind, subject, level, data) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (probe_id, time, kind, subject) DO NOTHING`,
				e.ProbeID, e.Time, e.Kind, e.Subject, e.Level, string(data)); err != nil {
				return err
			}
		}
		return nil
	})
}

// TimelineFilter olay sorgusu. ProbeIDs boşsa sonuç da boştur (çağıran görünür
// sunucuları verir). Before > 0: o andan önceki sayfa; Since > 0: o andan
// sonrakiler; Kinds ve Levels boş değilse yalnız onlar.
type TimelineFilter struct {
	ProbeIDs      []int64
	Before, Since int64
	Kinds, Levels []string
	Limit         int
}

func inList(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

// TimelineEvents en yeni olaylar (yeniden eskiye).
func (s *Store) TimelineEvents(ctx context.Context, f TimelineFilter) ([]TimelineEvent, error) {
	out := []TimelineEvent{}
	if len(f.ProbeIDs) == 0 {
		return out, nil
	}
	q := "SELECT probe_id, time, kind, subject, level, data FROM server_events WHERE probe_id IN (" + inList(len(f.ProbeIDs)) + ")"
	args := make([]any, 0, len(f.ProbeIDs)+len(f.Kinds)+len(f.Levels)+3)
	for _, id := range f.ProbeIDs {
		args = append(args, id)
	}
	if f.Before > 0 {
		q += " AND time < ?"
		args = append(args, f.Before)
	}
	if f.Since > 0 {
		q += " AND time >= ?"
		args = append(args, f.Since)
	}
	if len(f.Kinds) > 0 {
		q += " AND kind IN (" + inList(len(f.Kinds)) + ")"
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if len(f.Levels) > 0 {
		q += " AND level IN (" + inList(len(f.Levels)) + ")"
		for _, l := range f.Levels {
			args = append(args, l)
		}
	}
	rows, err := s.db.QueryContext(ctx, q+" ORDER BY time DESC, kind, subject LIMIT ?", append(args, f.Limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e TimelineEvent
		var data string
		if err := rows.Scan(&e.ProbeID, &e.Time, &e.Kind, &e.Subject, &e.Level, &data); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(data)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneTimelineEvents saklama süresini aşan olayları siler (TimescaleDB tam
// kipinde saklama politikası yapar).
func (s *Store) PruneTimelineEvents(ctx context.Context, now int64) error {
	if s.tsFull() {
		return nil
	}
	_, err := s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM server_events WHERE (probe_id, time, kind, subject) IN
		(SELECT probe_id, time, kind, subject FROM server_events WHERE time < ? LIMIT ?)`, now-serverEventsKeepSec)
	return err
}
