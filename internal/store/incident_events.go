package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// 11: olay ayrıntıları (docs/PLAN.md §13).
//
//   - incident_events: olayın işlem geçmişi (tekrar denemeler, başlangıç,
//     bildirim gönderimleri, hatırlatmalar, bakım, çözülme…). data isteğe bağlı
//     JSON'dur (” = yok). Olay başına en fazla MaxIncidentEvents kayıt.
//   - incident_captures: olayı açan başarısız kontrolün isteği ve yanıtı
//     (check.Detail JSON'u, maskeli). Olay başına tek kayıt; çözülmesinden
//     CaptureKeepDays gün sonra silinir.
func init() {
	RegisterMigration(11, `
CREATE TABLE incident_events (
	id          INTEGER PRIMARY KEY,
	incident_id INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
	time        INTEGER NOT NULL,
	kind        TEXT    NOT NULL,
	location    TEXT    NOT NULL DEFAULT '',
	message     TEXT    NOT NULL DEFAULT '',
	data        TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX incident_events_incident ON incident_events(incident_id, time);

CREATE TABLE incident_captures (
	incident_id INTEGER NOT NULL PRIMARY KEY REFERENCES incidents(id) ON DELETE CASCADE,
	time        INTEGER NOT NULL,
	location    TEXT    NOT NULL DEFAULT '',
	data        TEXT    NOT NULL
);
`)
}

// MaxIncidentEvents bir olayın en fazla işlem geçmişi kaydı: sık kesilip
// düzelen (veya hatası sürekli değişen) monitör tabloyu şişiremez.
const MaxIncidentEvents = 500

// CaptureKeepDays çözülmüş olayın istek/yanıt kaydının saklama süresi.
const CaptureKeepDays = 90

// İşlem geçmişi türleri (bkz. docs/PLAN.md §13.4).
const (
	EventRetry      = "retry"
	EventDown       = "down"
	EventChange     = "change"
	EventLocation   = "location"
	EventReminder   = "reminder"
	EventMaintStart = "maint_start"
	EventMaintEnd   = "maint_end"
	EventNotify     = "notify"
	EventEdited     = "edited"
	EventPaused     = "paused"
	EventUp         = "up"
	EventLimit      = "limit"
)

// IncidentEvent olayın işlem geçmişindeki tek kayıt.
type IncidentEvent struct {
	ID       int64           `json:"id"`
	Time     int64           `json:"time"`
	Kind     string          `json:"kind"`
	Location string          `json:"location"`
	Message  string          `json:"message"`
	Data     json.RawMessage `json:"data"` // null: yok
}

// IncidentCapture olayı açan kontrolün istek/yanıt kaydı.
type IncidentCapture struct {
	Time     int64           `json:"time"`
	Location string          `json:"location"`
	Detail   json.RawMessage `json:"detail"` // check.Detail
}

// EventData data alanını JSON'a çevirir (nil: yok).
func EventData(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}

// StartIncident monitör için olay açar ve açık olayın kimliğini döner. Açık bir
// olay zaten varsa (ör. yeniden başlatma sonrası) yenisi açılmaz, onun kimliği döner.
func (s *Store) StartIncident(ctx context.Context, monitorID, t int64, cause string) (int64, error) {
	if err := s.OpenIncident(ctx, monitorID, t, cause); err != nil {
		return 0, err
	}
	return s.OpenIncidentID(ctx, monitorID)
}

// OpenIncidentID monitörün açık olayının kimliği (yoksa 0).
func (s *Store) OpenIncidentID(ctx context.Context, monitorID int64) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT MAX(id) FROM incidents WHERE monitor_id = ? AND resolved_at IS NULL", monitorID).Scan(&id)
	return id.Int64, err
}

// GetIncident tek olayı monitör adıyla döner.
func (s *Store) GetIncident(ctx context.Context, id int64) (Incident, error) {
	var in Incident
	var resolved sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id WHERE i.id = ?`, id).
		Scan(&in.ID, &in.MonitorID, &in.MonitorName, &in.StartedAt, &resolved, &in.Cause)
	if errors.Is(err, sql.ErrNoRows) {
		return in, ErrNotFound
	}
	in.ResolvedAt = resolved.Int64
	return in, err
}

// AddIncidentEvents olaya işlem geçmişi kayıtları ekler. Olayda
// MaxIncidentEvents kayıt varsa yenileri atılır (çözülme kaydı hariç); sınıra
// gelindiğinde bir kez "sınıra ulaşıldı" kaydı yazılır.
func (s *Store) AddIncidentEvents(ctx context.Context, incidentID int64, evs ...IncidentEvent) error {
	if incidentID <= 0 || len(evs) == 0 {
		return nil
	}
	return s.tx(ctx, func(tx *Tx) error {
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_events WHERE incident_id = ?", incidentID).Scan(&n); err != nil {
			return err
		}
		for _, ev := range evs {
			switch {
			case ev.Kind == EventUp:
			case n > MaxIncidentEvents-1:
				continue
			case n == MaxIncidentEvents-1:
				ev = IncidentEvent{Time: ev.Time, Kind: EventLimit,
					Message: "Kayıt sınırına ulaşıldı; bu olayın sonraki ayrıntıları kaydedilmiyor"}
			}
			data := ""
			if len(ev.Data) > 0 && string(ev.Data) != "null" {
				data = string(ev.Data)
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, ?, ?, ?)",
				incidentID, ev.Time, ev.Kind, ev.Location, ev.Message, data); err != nil {
				return err
			}
			n++
		}
		return nil
	})
}

// AddOpenIncidentEvent monitörün açık olayı varsa ona kayıt ekler.
func (s *Store) AddOpenIncidentEvent(ctx context.Context, monitorID int64, ev IncidentEvent) error {
	id, err := s.OpenIncidentID(ctx, monitorID)
	if err != nil || id == 0 {
		return err
	}
	return s.AddIncidentEvents(ctx, id, ev)
}

// IncidentEvents olayın işlem geçmişi, yeniden eskiye.
func (s *Store) IncidentEvents(ctx context.Context, incidentID int64) ([]IncidentEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, time, kind, location, message, data FROM incident_events
		WHERE incident_id = ? ORDER BY time DESC, id DESC`, incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IncidentEvent{}
	for rows.Next() {
		var ev IncidentEvent
		var data string
		if err := rows.Scan(&ev.ID, &ev.Time, &ev.Kind, &ev.Location, &ev.Message, &data); err != nil {
			return nil, err
		}
		if data != "" {
			ev.Data = json.RawMessage(data)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// SaveIncidentCapture olayın istek/yanıt kaydını yazar; olayda zaten varsa
// dokunmaz (yalnızca ilk yakalama saklanır).
func (s *Store) SaveIncidentCapture(ctx context.Context, incidentID, t int64, location string, detail []byte) error {
	if incidentID <= 0 || len(detail) == 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incident_captures (incident_id, time, location, data)
		SELECT CAST(? AS BIGINT), CAST(? AS BIGINT), CAST(? AS TEXT), CAST(? AS TEXT)
		WHERE NOT EXISTS (SELECT 1 FROM incident_captures WHERE incident_id = ?)`,
		incidentID, t, location, string(detail), incidentID)
	return err
}

// GetIncidentCapture olayın istek/yanıt kaydı; yoksa ok=false.
func (s *Store) GetIncidentCapture(ctx context.Context, incidentID int64) (IncidentCapture, bool, error) {
	var c IncidentCapture
	var data string
	err := s.db.QueryRowContext(ctx, "SELECT time, location, data FROM incident_captures WHERE incident_id = ?", incidentID).
		Scan(&c.Time, &c.Location, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	if err != nil {
		return c, false, err
	}
	c.Detail = json.RawMessage(data)
	return c, true, nil
}
