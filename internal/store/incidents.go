package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// Incident olay satırı. Monitör olaylarında (monitor, partial) MonitorID,
// sunucu olaylarında (server_offline, server_alert) ServerID doludur.
type Incident struct {
	ID          int64           `json:"id"`
	Kind        string          `json:"kind"`
	MonitorID   int64           `json:"monitor_id"`
	MonitorName string          `json:"monitor_name"`
	ServerID    int64           `json:"server_id,omitempty"`
	ServerName  string          `json:"server_name,omitempty"`
	StartedAt   int64           `json:"started_at"`
	ResolvedAt  int64           `json:"resolved_at"` // 0: devam ediyor
	Cause       string          `json:"cause"`
	Data        json.RawMessage `json:"data,omitempty"` // türe özgü (bkz. incident_kinds.go)
}

// incidentCols olay listesi sütunları (incidents i, monitors m, probes p).
const incidentCols = `i.id, i.kind, COALESCE(i.monitor_id, 0), COALESCE(m.name, ''), COALESCE(i.server_id, 0),
	COALESCE(p.name, ''), i.started_at, i.resolved_at, i.cause, i.data`

const incidentFrom = ` FROM incidents i LEFT JOIN monitors m ON m.id = i.monitor_id
	LEFT JOIN probes p ON p.id = i.server_id`

type rowScanner interface{ Scan(...any) error }

func scanIncident(r rowScanner) (Incident, error) {
	var in Incident
	var resolved sql.NullInt64
	var data string
	err := r.Scan(&in.ID, &in.Kind, &in.MonitorID, &in.MonitorName, &in.ServerID, &in.ServerName,
		&in.StartedAt, &resolved, &in.Cause, &data)
	in.ResolvedAt = resolved.Int64
	if data != "" {
		in.Data = json.RawMessage(data)
	}
	return in, err
}

func (s *Store) OpenIncident(ctx context.Context, monitorID, t int64, cause string) error {
	// Açık bir olay zaten varsa (ör. yeniden başlatma sonrası) yenisi açılmaz.
	_, err := s.startKindIncident(ctx, IncidentMonitor, monitorID, t, cause, nil)
	return err
}

// ResolveIncident monitörün açık olayını kapatır ve başlangıç zamanını döner
// (açık olay yoksa 0). Kısmi kesinti olayına dokunmaz.
func (s *Store) ResolveIncident(ctx context.Context, monitorID, t int64) (int64, error) {
	_, started, err := s.resolveKindIncident(ctx, IncidentMonitor, monitorID, t)
	return started, err
}

// OpenIncidentStart monitörün açık olayının başlangıcı (yoksa 0).
func (s *Store) OpenIncidentStart(ctx context.Context, monitorID int64) (int64, error) {
	var started sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT MIN(started_at) FROM incidents WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL",
		monitorID, IncidentMonitor).Scan(&started)
	return started.Int64, err
}

// IncidentFilter: MonitorID 0 ise tüm monitörler; Before 0 ise en yeniden başlar.
//
// Görünürlük (müşteri kısıtı): MonitorIDs ve ServerIDs ikisi de nil ise tüm
// olaylar; biri nil değilse yalnızca listelenen monitörlerin ve sunucuların
// olayları (nil olan boş liste sayılır).
type IncidentFilter struct {
	MonitorID  int64
	ServerID   int64
	MonitorIDs []int64 // müşteri kısıtı: görebileceği monitörler
	ServerIDs  []int64 // müşteri kısıtı: görebileceği sunucular
	Kind       string  // süzgeç grubu: monitor | partial | server ("" = hepsi)
	Before     int64   // sayfalama: bu id'den küçükler
	Since      int64
	Open       bool // yalnızca süren (çözülmemiş) olaylar
	Limit      int
}

func (s *Store) ListIncidents(ctx context.Context, f IncidentFilter) ([]Incident, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := `SELECT ` + incidentCols + incidentFrom + ` WHERE 1 = 1`
	var args []any
	if f.MonitorID > 0 {
		q += " AND i.monitor_id = ?"
		args = append(args, f.MonitorID)
	}
	if f.ServerID > 0 {
		q += " AND i.server_id = ?"
		args = append(args, f.ServerID)
	}
	if f.MonitorIDs != nil || f.ServerIDs != nil {
		var or []string
		if len(f.MonitorIDs) > 0 {
			in, a := inClause(f.MonitorIDs)
			or = append(or, "i.monitor_id IN ("+in+")")
			args = append(args, a...)
		}
		if len(f.ServerIDs) > 0 {
			in, a := inClause(f.ServerIDs)
			or = append(or, "i.server_id IN ("+in+")")
			args = append(args, a...)
		}
		switch len(or) {
		case 0:
			return []Incident{}, nil
		case 1:
			q += " AND " + or[0]
		default:
			q += " AND (" + or[0] + " OR " + or[1] + ")"
		}
	}
	if kinds := kindsOf(f.Kind); kinds != nil {
		q += " AND i.kind IN (" + placeholders(len(kinds)) + ")"
		for _, k := range kinds {
			args = append(args, k)
		}
	}
	if f.Before > 0 {
		q += " AND i.id < ?"
		args = append(args, f.Before)
	}
	if f.Open {
		q += " AND i.resolved_at IS NULL"
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
		in, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func placeholders(n int) string {
	b := make([]byte, 0, 2*n)
	for i := range n {
		if i > 0 {
			b = append(b, ',')
		}
		b = append(b, '?')
	}
	return string(b)
}

// OpenIncidentCounts süren (çözülmemiş) olayların türe göre sayısı. monitorIDs
// ve serverIDs ikisi de nil ise tüm olaylar; biri nil değilse yalnızca listelenen
// monitörlerin ve sunucuların olayları (müşteri kısıtı; nil olan boş sayılır).
func (s *Store) OpenIncidentCounts(ctx context.Context, monitorIDs, serverIDs []int64) (map[string]int, error) {
	q := "SELECT kind, COUNT(*) FROM incidents WHERE resolved_at IS NULL"
	var args []any
	if monitorIDs != nil || serverIDs != nil {
		var or []string
		if len(monitorIDs) > 0 {
			in, a := inClause(monitorIDs)
			or = append(or, "monitor_id IN ("+in+")")
			args = append(args, a...)
		}
		if len(serverIDs) > 0 {
			in, a := inClause(serverIDs)
			or = append(or, "server_id IN ("+in+")")
			args = append(args, a...)
		}
		if len(or) == 0 {
			return map[string]int{}, nil
		}
		q += " AND (" + strings.Join(or, " OR ") + ")"
	}
	rows, err := s.db.QueryContext(ctx, q+" GROUP BY kind", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var kind string
		var n int
		if err := rows.Scan(&kind, &n); err != nil {
			return nil, err
		}
		out[kind] = n
	}
	return out, rows.Err()
}

// CountIncidentsSince since'den sonra başlayan monitör olayı (kesinti) sayısı;
// kısmi kesintiler ve sunucu olayları sayılmaz.
func (s *Store) CountIncidentsSince(ctx context.Context, since int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incidents WHERE started_at >= ? AND kind = ?",
		since, IncidentMonitor).Scan(&n)
	return n, err
}
