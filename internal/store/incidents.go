package store

import (
	"context"
	"database/sql"
)

type Incident struct {
	ID          int64  `json:"id"`
	MonitorID   int64  `json:"monitor_id"`
	MonitorName string `json:"monitor_name"`
	StartedAt   int64  `json:"started_at"`
	ResolvedAt  int64  `json:"resolved_at"` // 0: devam ediyor
	Cause       string `json:"cause"`
}

func (s *Store) OpenIncident(ctx context.Context, monitorID, t int64, cause string) error {
	// Açık bir olay zaten varsa (ör. yeniden başlatma sonrası) yenisi açılmaz.
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incidents (monitor_id, started_at, cause)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM incidents WHERE monitor_id = ? AND resolved_at IS NULL)`,
		monitorID, t, cause, monitorID)
	return err
}

// ResolveIncident monitörün açık olayını kapatır ve başlangıç zamanını döner
// (açık olay yoksa 0).
func (s *Store) ResolveIncident(ctx context.Context, monitorID, t int64) (int64, error) {
	var started sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		UPDATE incidents SET resolved_at = ?
		WHERE monitor_id = ? AND resolved_at IS NULL
		RETURNING started_at`, t, monitorID).Scan(&started)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return started.Int64, err
}

// OpenIncidentStart monitörün açık olayının başlangıcı (yoksa 0).
func (s *Store) OpenIncidentStart(ctx context.Context, monitorID int64) (int64, error) {
	var started sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT MIN(started_at) FROM incidents WHERE monitor_id = ? AND resolved_at IS NULL",
		monitorID).Scan(&started)
	return started.Int64, err
}

// IncidentFilter: MonitorID 0 ise tüm monitörler; Before 0 ise en yeniden başlar.
type IncidentFilter struct {
	MonitorID  int64
	MonitorIDs []int64 // boş değilse sadece bu monitörler (müşteri kısıtı); nil: hepsi
	Before     int64   // sayfalama: bu id'den küçükler
	Since      int64
	Limit      int
}

func (s *Store) ListIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id WHERE 1 = 1`
	var args []any
	if f.MonitorID > 0 {
		q += " AND i.monitor_id = ?"
		args = append(args, f.MonitorID)
	}
	if f.MonitorIDs != nil {
		if len(f.MonitorIDs) == 0 {
			return []Incident{}, nil
		}
		in, args2 := inClause(f.MonitorIDs)
		q += " AND i.monitor_id IN (" + in + ")"
		args = append(args, args2...)
	}
	if f.Before > 0 {
		q += " AND i.id < ?"
		args = append(args, f.Before)
	}
	if f.Since > 0 {
		q += " AND (i.started_at >= ? OR i.resolved_at IS NULL OR i.resolved_at >= ?)"
		args = append(args, f.Since, f.Since)
	}
	q += " ORDER BY i.id DESC LIMIT ?"
	args = append(args, f.Limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var in Incident
		var resolved sql.NullInt64
		if err := rows.Scan(&in.ID, &in.MonitorID, &in.MonitorName, &in.StartedAt, &resolved, &in.Cause); err != nil {
			return nil, err
		}
		in.ResolvedAt = resolved.Int64
		out = append(out, in)
	}
	return out, rows.Err()
}

// CountIncidentsSince since'den sonra başlayan olay sayısı.
func (s *Store) CountIncidentsSince(ctx context.Context, since int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incidents WHERE started_at >= ?", since).Scan(&n)
	return n, err
}
