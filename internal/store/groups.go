package store

import (
	"context"
	"encoding/json"
	"time"
)

// SetMonitorConfig yalnızca monitörün ayar JSON'unu değiştirir (ör. silinen
// alt monitör grup ayarından çıkarılırken). Durum alanlarına dokunmaz.
func (s *Store) SetMonitorConfig(ctx context.Context, id int64, cfg json.RawMessage) error {
	res, err := s.db.ExecContext(ctx, "UPDATE monitors SET config = ?, updated_at = ? WHERE id = ?",
		string(cfg), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MonitorsOfType verilen tipteki monitörler (ör. döngü kontrolü için gruplar).
func (s *Store) MonitorsOfType(ctx context.Context, typ string) ([]Monitor, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+monitorCols+" FROM monitors WHERE type = ? ORDER BY id", typ)
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
