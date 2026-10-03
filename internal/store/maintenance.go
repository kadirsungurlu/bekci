package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// 3: bakım pencereleri. Pencerenin zamanlaması strateji alanlarında tutulur;
// hesaplama internal/maintenance paketindedir. Etkilenen monitörler
// maintenance_monitors'ta (all_monitors=1 ise tüm monitörler).
func init() {
	RegisterMigration(3, `
CREATE TABLE maintenance (
	id           INTEGER PRIMARY KEY,
	title        TEXT    NOT NULL,
	description  TEXT    NOT NULL DEFAULT '',
	active       INTEGER NOT NULL DEFAULT 1,
	strategy     TEXT    NOT NULL,
	timezone     TEXT    NOT NULL DEFAULT 'Europe/Istanbul',
	start_local  TEXT    NOT NULL DEFAULT '',
	end_local    TEXT    NOT NULL DEFAULT '',
	weekdays     TEXT    NOT NULL DEFAULT '[]',
	start_time   TEXT    NOT NULL DEFAULT '',
	end_time     TEXT    NOT NULL DEFAULT '',
	date_from    TEXT    NOT NULL DEFAULT '',
	date_to      TEXT    NOT NULL DEFAULT '',
	cron         TEXT    NOT NULL DEFAULT '',
	duration_min INTEGER NOT NULL DEFAULT 0,
	all_monitors INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	updated_at   INTEGER NOT NULL
);

CREATE TABLE maintenance_monitors (
	maintenance_id INTEGER NOT NULL REFERENCES maintenance(id) ON DELETE CASCADE,
	monitor_id     INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	PRIMARY KEY (maintenance_id, monitor_id)
) WITHOUT ROWID;
CREATE INDEX maintenance_monitors_monitor ON maintenance_monitors(monitor_id);
`)
}

// 20: ended_at — süren tekrar "şimdi bitir" ile erken bitirildiğinde o an
// (unix); bu andan sonra, o anda süren tekrar bitmiş sayılır, sonraki
// tekrarlar normal işler. Pencere düzenlenince sıfırlanır.
func init() {
	RegisterMigration(20, `ALTER TABLE maintenance ADD COLUMN ended_at INTEGER NOT NULL DEFAULT 0;`)
}

// Bakım stratejileri.
const (
	MaintManual = "manual"           // "aktif" açık olduğu sürece
	MaintOnce   = "once"             // tek seferlik: başlangıç-bitiş
	MaintWeekly = "recurring_weekly" // seçilen günlerde, günlük saat aralığı
	MaintDaily  = "recurring_daily"  // her gün, günlük saat aralığı
	MaintCron   = "cron"             // cron ifadesi + süre
)

// Maintenance bir bakım penceresi. Tarih/saat alanları pencerenin kendi saat
// diliminde (Timezone) yerel değerlerdir:
//   - Start/End: "2006-01-02T15:04" (tek seferlik)
//   - StartTime/EndTime: "15:04" (haftalık/günlük; bitiş başlangıçtan küçükse ertesi gün)
//   - DateFrom/DateTo: "2006-01-02", isteğe bağlı, dahil (tekrarlayanlar ve cron)
type Maintenance struct {
	ID              int64   `json:"id"`
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Active          bool    `json:"active"`
	Strategy        string  `json:"strategy"`
	Timezone        string  `json:"timezone"`
	Start           string  `json:"start"`
	End             string  `json:"end"`
	Weekdays        []int   `json:"weekdays"` // 0 = Pazar … 6 = Cumartesi
	StartTime       string  `json:"start_time"`
	EndTime         string  `json:"end_time"`
	DateFrom        string  `json:"date_from"`
	DateTo          string  `json:"date_to"`
	Cron            string  `json:"cron"`
	DurationMinutes int     `json:"duration_minutes"`
	AllMonitors     bool    `json:"all_monitors"`
	MonitorIDs      []int64 `json:"monitor_ids"`
	CreatedAt       int64   `json:"created_at"`
	UpdatedAt       int64   `json:"updated_at"`
	// EndedAt süren tekrarın "şimdi bitir" ile bitirildiği an (unix; 0: yok).
	EndedAt int64 `json:"ended_at"`
	// Sunucular (migration 26): pencere içinde sunucu uyarısı ve çevrimdışı
	// bildirimi gitmez. AllServers tüm sunucular; ServerIDs seçili ajanlar.
	AllServers bool    `json:"all_servers"`
	ServerIDs  []int64 `json:"server_ids"`
}

const maintCols = `id, title, description, active, strategy, timezone, start_local, end_local,
	weekdays, start_time, end_time, date_from, date_to, cron, duration_min, all_monitors,
	created_at, updated_at, ended_at, all_servers`

func scanMaintenance(sc scanner) (Maintenance, error) {
	var m Maintenance
	var weekdays string
	err := sc.Scan(&m.ID, &m.Title, &m.Description, &m.Active, &m.Strategy, &m.Timezone,
		&m.Start, &m.End, &weekdays, &m.StartTime, &m.EndTime, &m.DateFrom, &m.DateTo,
		&m.Cron, &m.DurationMinutes, &m.AllMonitors, &m.CreatedAt, &m.UpdatedAt, &m.EndedAt, &m.AllServers)
	if err != nil {
		return m, err
	}
	if json.Unmarshal([]byte(weekdays), &m.Weekdays) != nil || m.Weekdays == nil {
		m.Weekdays = []int{}
	}
	m.MonitorIDs = []int64{}
	m.ServerIDs = []int64{}
	return m, nil
}

// ListMaintenance tüm bakım pencerelerini monitör listeleriyle döner.
func (s *Store) ListMaintenance(ctx context.Context) ([]Maintenance, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+maintCols+" FROM maintenance ORDER BY id")
	if err != nil {
		return nil, err
	}
	out := []Maintenance{}
	idx := map[int64]int{}
	for rows.Next() {
		m, err := scanMaintenance(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		idx[m.ID] = len(out)
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	links, err := s.db.QueryContext(ctx, "SELECT maintenance_id, monitor_id FROM maintenance_monitors ORDER BY monitor_id")
	if err != nil {
		return nil, err
	}
	defer links.Close()
	for links.Next() {
		var wid, mid int64
		if err := links.Scan(&wid, &mid); err != nil {
			return nil, err
		}
		if i, ok := idx[wid]; ok {
			out[i].MonitorIDs = append(out[i].MonitorIDs, mid)
		}
	}
	if err := links.Err(); err != nil {
		return nil, err
	}
	srv, err := s.serverMaintenanceLinks(ctx)
	if err != nil {
		return nil, err
	}
	for wid, ids := range srv {
		if i, ok := idx[wid]; ok {
			out[i].ServerIDs = ids
		}
	}
	return out, nil
}

func (s *Store) GetMaintenance(ctx context.Context, id int64) (Maintenance, error) {
	m, err := scanMaintenance(s.db.QueryRowContext(ctx, "SELECT "+maintCols+" FROM maintenance WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT monitor_id FROM maintenance_monitors WHERE maintenance_id = ? ORDER BY monitor_id", id)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		if err := rows.Scan(&mid); err != nil {
			return m, err
		}
		m.MonitorIDs = append(m.MonitorIDs, mid)
	}
	if err := rows.Err(); err != nil {
		return m, err
	}
	srows, err := s.db.QueryContext(ctx, "SELECT probe_id FROM maintenance_servers WHERE maintenance_id = ? ORDER BY probe_id", id)
	if err != nil {
		return m, err
	}
	defer srows.Close()
	for srows.Next() {
		var pid int64
		if err := srows.Scan(&pid); err != nil {
			return m, err
		}
		m.ServerIDs = append(m.ServerIDs, pid)
	}
	return m, srows.Err()
}

func maintArgs(m *Maintenance) []any {
	weekdays, _ := json.Marshal(m.Weekdays)
	if m.Weekdays == nil {
		weekdays = []byte("[]")
	}
	return []any{m.Title, m.Description, boolInt(m.Active), m.Strategy, m.Timezone, m.Start, m.End,
		string(weekdays), m.StartTime, m.EndTime, m.DateFrom, m.DateTo, m.Cron, m.DurationMinutes,
		boolInt(m.AllMonitors), boolInt(m.AllServers)}
}

func (s *Store) CreateMaintenance(ctx context.Context, m *Maintenance) error {
	now := time.Now().Unix()
	m.CreatedAt, m.UpdatedAt = now, now
	return s.tx(ctx, func(tx *Tx) error {
		err := tx.QueryRowContext(ctx, `
			INSERT INTO maintenance (title, description, active, strategy, timezone, start_local, end_local,
				weekdays, start_time, end_time, date_from, date_to, cron, duration_min, all_monitors, all_servers,
				created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id`,
			append(maintArgs(m), m.CreatedAt, m.UpdatedAt)...).Scan(&m.ID)
		if err != nil {
			return err
		}
		if err := setMaintenanceMonitors(ctx, tx, m.ID, m.MonitorIDs); err != nil {
			return err
		}
		return setMaintenanceServers(ctx, tx, m.ID, m.ServerIDs)
	})
}

func (s *Store) UpdateMaintenance(ctx context.Context, m *Maintenance) error {
	m.UpdatedAt = time.Now().Unix()
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE maintenance SET title = ?, description = ?, active = ?, strategy = ?, timezone = ?,
				start_local = ?, end_local = ?, weekdays = ?, start_time = ?, end_time = ?,
				date_from = ?, date_to = ?, cron = ?, duration_min = ?, all_monitors = ?, all_servers = ?, updated_at = ?,
				ended_at = 0
			WHERE id = ?`,
			append(maintArgs(m), m.UpdatedAt, m.ID)...)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if err := setMaintenanceMonitors(ctx, tx, m.ID, m.MonitorIDs); err != nil {
			return err
		}
		return setMaintenanceServers(ctx, tx, m.ID, m.ServerIDs)
	})
}

// SetMaintenanceActive pencereyi elle açar/kapatır (diğer alanlar değişmez).
func (s *Store) SetMaintenanceActive(ctx context.Context, id int64, active bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE maintenance SET active = ?, updated_at = ? WHERE id = ?",
		boolInt(active), time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// EndMaintenanceNow süren tekrarı t anında bitirir (ended_at = t); zamanlama
// açık kalır, sonraki tekrarlar normal işler.
func (s *Store) EndMaintenanceNow(ctx context.Context, id, t int64) error {
	res, err := s.db.ExecContext(ctx, "UPDATE maintenance SET ended_at = ?, updated_at = ? WHERE id = ?", t, t, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteMaintenance(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM maintenance WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func setMaintenanceMonitors(ctx context.Context, tx *Tx, id int64, ids []int64) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM maintenance_monitors WHERE maintenance_id = ?", id); err != nil {
		return err
	}
	for _, mid := range ids {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO maintenance_monitors (maintenance_id, monitor_id) VALUES (?, ?) ON CONFLICT (maintenance_id, monitor_id) DO NOTHING", id, mid); err != nil {
			return err
		}
	}
	return nil
}
