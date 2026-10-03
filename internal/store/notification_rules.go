package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// 23: bildirim kuralları (kanal başına).
//
//   - notifications.events: kanalın alacağı olay türleri (JSON dizi; boş =
//     hepsi, eski kanallar değişmez).
//   - notifications.quiet_hours: sessiz saatler (JSON; boş = yok): başlangıç,
//     bitiş ("HH:MM"), saat dilimi ve kip (critical: yalnızca 🔴 geçer, none:
//     hiçbiri). Sessiz saatlerde "sorun başladı" bildirimleri pencerenin
//     bitimine ertelenir ve olay hâlâ sürüyorsa gönderilir; hatırlatmalar atılır.
//   - notifications.delay_min: gecikme (dk): sorun bu kadar süre sürmezse
//     bildirim gitmez; gitmemişse düzelme bildirimi de gitmez.
//   - notifications.escalate_min: eskalasyon (dk): 0'dan büyükse kanal bir
//     eskalasyon kanalıdır; bu kadar dakikadır açık kalan her olay (monitör,
//     sunucu, kontrol noktası) kanala bağlı olmasa da buraya bildirilir.
//   - notifications.lang: kanalın bildirim dili (boş = ayarlardaki).
//   - notification_deliveries: olay + kanal + tür başına gönderim denemesi
//     (düzelme bildiriminin eşleştirilmesi ve eskalasyonun tekrarlanmaması için).
//   - notification_queue: ertelenmiş bildirimler (gecikme / sessiz saat);
//     payload gönderilecek olayın JSON hali. Yeniden başlatmada kaybolmaz.
func init() {
	RegisterMigration(23, `
ALTER TABLE notifications ADD COLUMN events TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN quiet_hours TEXT NOT NULL DEFAULT '';
ALTER TABLE notifications ADD COLUMN delay_min INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN escalate_min INTEGER NOT NULL DEFAULT 0;
ALTER TABLE notifications ADD COLUMN lang TEXT NOT NULL DEFAULT '';

CREATE TABLE notification_deliveries (
	incident_id     INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
	notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	event           TEXT    NOT NULL,
	time            INTEGER NOT NULL,
	PRIMARY KEY (incident_id, notification_id, event)
) WITHOUT ROWID;

CREATE TABLE notification_queue (
	id              INTEGER PRIMARY KEY,
	incident_id     INTEGER REFERENCES incidents(id) ON DELETE CASCADE,
	notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	event           TEXT    NOT NULL,
	reason          TEXT    NOT NULL DEFAULT 'delay',
	due_at          INTEGER NOT NULL,
	created_at      INTEGER NOT NULL,
	payload         TEXT    NOT NULL
);
CREATE INDEX notification_queue_due ON notification_queue(due_at);
CREATE INDEX notification_queue_incident ON notification_queue(incident_id);
`)
}

// Sessiz saat kipleri.
const (
	QuietCritical = "critical" // yalnızca 🔴 (kesinti, sunucu uyarısı, kontrol noktası) ve onların düzelmesi geçer
	QuietNone     = "none"     // hiçbir bildirim gitmez; sorun başlangıçları pencere bitimine ertelenir
)

// QuietHours bir kanalın sessiz saatleri. Start/End "HH:MM" (kanalın saat
// diliminde; End <= Start ise gece yarısını aşar), TZ IANA adı (boş: sunucunun
// yerel saati), Mode QuietCritical | QuietNone.
type QuietHours struct {
	Start string `json:"start"`
	End   string `json:"end"`
	TZ    string `json:"tz"`
	Mode  string `json:"mode"`
}

// Validate alanları denetler ve varsayılanları doldurur.
func (q *QuietHours) Validate() error {
	if _, err := parseClock(q.Start); err != nil {
		return fmt.Errorf("Sessiz saat başlangıcı SS:DD biçiminde olmalı")
	}
	if _, err := parseClock(q.End); err != nil {
		return fmt.Errorf("Sessiz saat bitişi SS:DD biçiminde olmalı")
	}
	if q.Start == q.End {
		return fmt.Errorf("Sessiz saat başlangıcı ve bitişi aynı olamaz")
	}
	q.TZ = strings.TrimSpace(q.TZ)
	if q.TZ != "" {
		if _, err := time.LoadLocation(q.TZ); err != nil {
			return fmt.Errorf("Sessiz saat dilimi tanınmadı: %s", q.TZ)
		}
	}
	switch q.Mode {
	case "":
		q.Mode = QuietCritical
	case QuietCritical, QuietNone:
	default:
		return fmt.Errorf("Sessiz saat kipi critical veya none olmalı")
	}
	return nil
}

// parseClock "HH:MM" → gün içindeki dakika.
func parseClock(s string) (int, error) {
	var h, m int
	if _, err := fmt.Sscanf(strings.TrimSpace(s), "%d:%d", &h, &m); err != nil || h < 0 || h > 23 || m < 0 || m > 59 || len(strings.TrimSpace(s)) != 5 {
		return 0, fmt.Errorf("geçersiz saat")
	}
	return h*60 + m, nil
}

// Active now sessiz saatlerin içinde mi; içindeyse pencerenin bitişi de döner.
func (q QuietHours) Active(now time.Time) (bool, time.Time) {
	start, err1 := parseClock(q.Start)
	end, err2 := parseClock(q.End)
	if err1 != nil || err2 != nil {
		return false, time.Time{}
	}
	loc := time.Local
	if q.TZ != "" {
		if l, err := time.LoadLocation(q.TZ); err == nil {
			loc = l
		}
	}
	t := now.In(loc)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	cur := t.Hour()*60 + t.Minute()
	at := func(d time.Time, min int) time.Time { return d.Add(time.Duration(min) * time.Minute) }
	if start < end {
		if cur >= start && cur < end {
			return true, at(day, end)
		}
		return false, time.Time{}
	}
	// Gece yarısını aşan pencere: [start, 24:00) ∪ [00:00, end)
	switch {
	case cur >= start:
		return true, at(day.AddDate(0, 0, 1), end)
	case cur < end:
		return true, at(day, end)
	}
	return false, time.Time{}
}

// HasRules kanalda eşleştirme gerektiren bir kural var mı (gecikme veya
// sessiz saat): böyle kanallarda düzelme bildirimi yalnızca sorun bildirimi
// gönderilmişse gider.
func (n Notification) HasRules() bool { return n.DelayMin > 0 || n.Quiet != nil }

// OptInEvents yalnızca kanal açıkça seçtiyse giden olay türleri: süzgeçsiz
// (events boş) kanallar bunları ALMAZ. Olay onayı (acked) böyledir; aksi
// halde var olan her kanal bir anda onay mesajları almaya başlardı.
var OptInEvents = map[string]bool{"acked": true}

// Accepts kanal bu türü alır mı (events boşsa opt-in olmayan hepsi).
func (n Notification) Accepts(kind string) bool {
	if len(n.Events) == 0 {
		return !OptInEvents[kind]
	}
	for _, k := range n.Events {
		if k == kind {
			return true
		}
	}
	return false
}

// encodeEvents / decodeEvents events sütunu (boş liste "" olarak saklanır).
func encodeEvents(ev []string) string {
	if len(ev) == 0 {
		return ""
	}
	b, _ := json.Marshal(ev)
	return string(b)
}

func decodeEvents(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	if json.Unmarshal([]byte(s), &out) != nil || out == nil {
		return []string{}
	}
	return out
}

func encodeQuiet(q *QuietHours) string {
	if q == nil {
		return ""
	}
	b, _ := json.Marshal(q)
	return string(b)
}

func decodeQuiet(s string) *QuietHours {
	if s == "" {
		return nil
	}
	var q QuietHours
	if json.Unmarshal([]byte(s), &q) != nil || q.Start == "" {
		return nil
	}
	return &q
}

// NotificationsEscalating eskalasyon kanalları (etkin, escalate_min > 0).
func (s *Store) NotificationsEscalating(ctx context.Context) ([]Notification, error) {
	return s.queryNotifications(ctx, "SELECT "+notificationCols+" FROM notifications WHERE active = 1 AND escalate_min > 0 ORDER BY id")
}

// NotificationsByIDs verilen kanallardan etkin olanlar (kimliğe göre sıralı).
func (s *Store) NotificationsByIDs(ctx context.Context, ids []int64) ([]Notification, error) {
	if len(ids) == 0 {
		return []Notification{}, nil
	}
	q, args := inClause(ids)
	return s.queryNotifications(ctx, "SELECT "+notificationCols+" FROM notifications WHERE active = 1 AND id IN ("+q+") ORDER BY id", args...)
}

// Teslimat kayıtları ----------------------------------------------------------------

// RecordDelivery olay + kanal + tür için gönderim denemesini kaydeder (varsa dokunmaz).
func (s *Store) RecordDelivery(ctx context.Context, incidentID, notificationID int64, event string, t int64) error {
	if incidentID <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO notification_deliveries (incident_id, notification_id, event, time) VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, incidentID, notificationID, event, t)
	return err
}

// Delivered olayın bu türdeki bildirimi bu kanala gönderildi (denendi) mi?
func (s *Store) Delivered(ctx context.Context, incidentID, notificationID int64, event string) (bool, error) {
	if incidentID <= 0 {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_deliveries WHERE incident_id = ? AND notification_id = ? AND event = ?",
		incidentID, notificationID, event).Scan(&n)
	return n > 0, err
}

// DeliveredChannels olayın bu türdeki bildirimini almış kanallar.
func (s *Store) DeliveredChannels(ctx context.Context, incidentID int64, event string) ([]int64, error) {
	if incidentID <= 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT notification_id FROM notification_deliveries WHERE incident_id = ? AND event = ? ORDER BY notification_id", incidentID, event)
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

// Ertelenmiş bildirim kuyruğu ---------------------------------------------------------

// Erteleme nedenleri.
const (
	QueueDelay = "delay" // gecikme kuralı: N dk sonra olay hâlâ açıksa
	QueueQuiet = "quiet" // sessiz saatler: pencere bitince
)

// QueuedNotification ertelenmiş bir bildirim. Payload gönderilecek olayın
// (notify.Event) JSON hali; IncidentID 0 olabilir (ör. SSL uyarısı).
type QueuedNotification struct {
	ID             int64
	IncidentID     int64
	NotificationID int64
	Event          string
	Reason         string
	DueAt          int64
	CreatedAt      int64
	Payload        string
}

// EnqueueNotification bildirimi kuyruğa ekler. Aynı olay + kanal + tür için
// bekleyen kayıt varsa eklenmez (false).
func (s *Store) EnqueueNotification(ctx context.Context, q QueuedNotification) (bool, error) {
	added := false
	err := s.tx(ctx, func(tx *Tx) error {
		if q.IncidentID > 0 {
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_queue WHERE incident_id = ? AND notification_id = ? AND event = ?",
				q.IncidentID, q.NotificationID, q.Event).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				return nil
			}
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO notification_queue (incident_id, notification_id, event, reason, due_at, created_at, payload)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			nullInt(q.IncidentID), q.NotificationID, q.Event, q.Reason, q.DueAt, q.CreatedAt, q.Payload)
		added = err == nil
		return err
	})
	return added, err
}

// QueuedExists olay + kanal + tür için bekleyen kayıt var mı?
func (s *Store) QueuedExists(ctx context.Context, incidentID, notificationID int64, event string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM notification_queue WHERE incident_id = ? AND notification_id = ? AND event = ?",
		incidentID, notificationID, event).Scan(&n)
	return n > 0, err
}

func scanQueued(rows *sql.Rows) ([]QueuedNotification, error) {
	defer rows.Close()
	out := []QueuedNotification{}
	for rows.Next() {
		var q QueuedNotification
		var inc sql.NullInt64
		if err := rows.Scan(&q.ID, &inc, &q.NotificationID, &q.Event, &q.Reason, &q.DueAt, &q.CreatedAt, &q.Payload); err != nil {
			return nil, err
		}
		q.IncidentID = inc.Int64
		out = append(out, q)
	}
	return out, rows.Err()
}

const queueCols = "id, incident_id, notification_id, event, reason, due_at, created_at, payload"

// DueNotifications vadesi gelmiş kayıtlar (en eski önce).
func (s *Store) DueNotifications(ctx context.Context, now int64, limit int) ([]QueuedNotification, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+queueCols+" FROM notification_queue WHERE due_at <= ? ORDER BY due_at, id LIMIT ?", now, limit)
	if err != nil {
		return nil, err
	}
	return scanQueued(rows)
}

// QueuedForIncident olayın bekleyen kayıtları.
func (s *Store) QueuedForIncident(ctx context.Context, incidentID int64) ([]QueuedNotification, error) {
	if incidentID <= 0 {
		return []QueuedNotification{}, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+queueCols+" FROM notification_queue WHERE incident_id = ? ORDER BY id", incidentID)
	if err != nil {
		return nil, err
	}
	return scanQueued(rows)
}

// DeleteQueued kuyruk kaydını siler.
func (s *Store) DeleteQueued(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM notification_queue WHERE id = ?", id)
	return err
}

// RescheduleQueued kaydın vadesini ve nedenini değiştirir.
func (s *Store) RescheduleQueued(ctx context.Context, id, dueAt int64, reason string) error {
	_, err := s.db.ExecContext(ctx, "UPDATE notification_queue SET due_at = ?, reason = ? WHERE id = ?", dueAt, reason, id)
	return err
}

// OpenIncidentsBefore before'dan önce başlamış, hâlâ açık ve verilen
// türlerdeki olaylar (eskalasyon taraması).
func (s *Store) OpenIncidentsBefore(ctx context.Context, kinds []string, before int64) ([]Incident, error) {
	if len(kinds) == 0 {
		return []Incident{}, nil
	}
	q := `SELECT ` + incidentCols + incidentFrom + ` WHERE i.resolved_at IS NULL AND i.started_at <= ? AND i.kind IN (` + placeholders(len(kinds)) + `) ORDER BY i.id`
	args := []any{before}
	for _, k := range kinds {
		args = append(args, k)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		in, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}
