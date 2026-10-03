package store

import (
	"context"
	"database/sql"
	"errors"
)

// 29: olay onaylama (ack) ve susturma (snooze).
//
//   - incidents.acked_at / acked_by / ack_note: olay bir kullanıcı tarafından
//     onaylandı ("haberim var, üzerinde çalışıyorum"). Onaylı olayda
//     hatırlatma bildirimi ve eskalasyon gitmez; olay kapanınca onay anlamını
//     yitirir (kapanış bildirimi yine gider).
//   - incidents.snoozed_until: bu zamana kadar hatırlatma ve eskalasyon
//     susturulur; süre dolunca kendiliğinden devam eder. Onaydan bağımsızdır.
//
// Gecikme kuralı ya da sessiz saat yüzünden kuyrukta bekleyen ilk sorun
// bildirimi onaydan ETKİLENMEZ: onay "beni tekrar rahatsız etme" demektir,
// ilk uyarıyı susturmak değil (bkz. notify.Dispatcher).
func init() {
	RegisterMigration(29, `
ALTER TABLE incidents ADD COLUMN acked_at INTEGER;
ALTER TABLE incidents ADD COLUMN acked_by TEXT NOT NULL DEFAULT '';
ALTER TABLE incidents ADD COLUMN ack_note TEXT NOT NULL DEFAULT '';
ALTER TABLE incidents ADD COLUMN snoozed_until INTEGER;
`)
}

// ErrIncidentClosed kapanmış olayda onay / susturma girişimi.
var ErrIncidentClosed = errors.New("olay kapanmış")

// İşlem geçmişi kayıtları (incident_events.kind).
const (
	EventAck      = "ack"      // data: {user, note}
	EventUnack    = "unack"    // data: {user}
	EventSnooze   = "snooze"   // data: {user, until}
	EventUnsnooze = "unsnooze" // data: {user}
)

// Muted olay şu an susturulmuş mu: onaylı ya da susturma süresi dolmamış.
func (in Incident) Muted(now int64) bool {
	return in.ResolvedAt == 0 && (in.AckedAt > 0 || in.SnoozedUntil > now)
}

// AckIncident açık olayı onaylar. Kapanmış olayda ErrIncidentClosed, olay
// yoksa ErrNotFound döner. Zaten onaylıysa onaylayan ve not güncellenir.
func (s *Store) AckIncident(ctx context.Context, id int64, by, note string, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if err := requireOpenIncident(ctx, tx, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE incidents SET acked_at = ?, acked_by = ?, ack_note = ? WHERE id = ?", now, by, note, id)
		if err != nil {
			return err
		}
		return addEventTx(ctx, tx, id, IncidentEvent{Time: now, Kind: EventAck, Message: "Olay onaylandı",
			Data: EventData(map[string]string{"user": by, "note": note})})
	})
}

// UnackIncident onayı geri alır (hatırlatma ve eskalasyon yeniden başlar).
func (s *Store) UnackIncident(ctx context.Context, id int64, by string, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if err := requireOpenIncident(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE incidents SET acked_at = NULL, acked_by = '', ack_note = '' WHERE id = ? AND acked_at IS NOT NULL", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil // zaten onaysız
		}
		return addEventTx(ctx, tx, id, IncidentEvent{Time: now, Kind: EventUnack, Message: "Olay onayı geri alındı",
			Data: EventData(map[string]string{"user": by})})
	})
}

// SnoozeIncident açık olayın hatırlatma ve eskalasyonunu until'e kadar susturur.
func (s *Store) SnoozeIncident(ctx context.Context, id int64, until int64, by string, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if err := requireOpenIncident(ctx, tx, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE incidents SET snoozed_until = ? WHERE id = ?", until, id); err != nil {
			return err
		}
		return addEventTx(ctx, tx, id, IncidentEvent{Time: now, Kind: EventSnooze, Message: "Olay susturuldu",
			Data: EventData(map[string]any{"user": by, "until": until})})
	})
}

// UnsnoozeIncident susturmayı kaldırır.
func (s *Store) UnsnoozeIncident(ctx context.Context, id int64, by string, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		if err := requireOpenIncident(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE incidents SET snoozed_until = NULL WHERE id = ? AND snoozed_until IS NOT NULL", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return nil
		}
		return addEventTx(ctx, tx, id, IncidentEvent{Time: now, Kind: EventUnsnooze, Message: "Olay susturması kaldırıldı",
			Data: EventData(map[string]string{"user": by})})
	})
}

// IncidentMuted olayın hatırlatma/eskalasyonu şu an susturulmuş mu (onaylı ya
// da susturma süresi dolmamış). Olay yoksa false.
func (s *Store) IncidentMuted(ctx context.Context, id int64, now int64) (bool, error) {
	var acked, snoozed sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT acked_at, snoozed_until FROM incidents WHERE id = ? AND resolved_at IS NULL", id).Scan(&acked, &snoozed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return acked.Valid || (snoozed.Valid && snoozed.Int64 > now), nil
}

func requireOpenIncident(ctx context.Context, tx *Tx, id int64) error {
	var resolved sql.NullInt64
	err := tx.QueryRowContext(ctx, "SELECT resolved_at FROM incidents WHERE id = ?", id).Scan(&resolved)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if resolved.Valid {
		return ErrIncidentClosed
	}
	return nil
}

// addEventTx işlem içinde tek bir işlem geçmişi kaydı ekler (sınır denetimi
// AddIncidentEvents ile aynı: MaxIncidentEvents dolduysa yazılmaz).
func addEventTx(ctx context.Context, tx *Tx, incidentID int64, ev IncidentEvent) error {
	var n int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_events WHERE incident_id = ?", incidentID).Scan(&n); err != nil {
		return err
	}
	if n >= MaxIncidentEvents {
		return nil
	}
	data := ""
	if len(ev.Data) > 0 && string(ev.Data) != "null" {
		data = string(ev.Data)
	}
	_, err := tx.ExecContext(ctx,
		"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, ?, ?, ?)",
		incidentID, ev.Time, ev.Kind, ev.Location, ev.Message, data)
	return err
}
