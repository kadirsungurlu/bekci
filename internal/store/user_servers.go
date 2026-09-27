package store

import "context"

// 12: müşteri kısıtlı izleyicinin görebileceği sunucular (sunucu takibi).
// Monitör atamasının (user_monitors) sunucu karşılığıdır; yalnızca
// kısıtlı izleyicide anlamlıdır, diğer roller tüm sunucuları görür.
func init() {
	RegisterMigration(12, `
CREATE TABLE user_servers (
	user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	probe_id INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, probe_id)
) WITHOUT ROWID;
CREATE INDEX user_servers_probe ON user_servers(probe_id);
`)
}

func (s *Store) userServerIDs(ctx context.Context, userID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT probe_id FROM user_servers WHERE user_id = ? ORDER BY probe_id", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func setUserServers(ctx context.Context, tx *Tx, userID int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM user_servers WHERE user_id = ?", userID); err != nil {
		return err
	}
	for _, pid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO user_servers (user_id, probe_id) VALUES (?, ?) ON CONFLICT DO NOTHING", userID, pid); err != nil {
			return err
		}
	}
	return nil
}
