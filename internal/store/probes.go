package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// 7: uzak kontrol noktaları (probe). Aynı ikili başka sunucularda "uptime
// probe" olarak çalışır, atanan monitörleri kontrol edip sonuçları ana
// sunucuya bildirir.
//
//   - probes: kayıtlı kontrol noktaları. Token'ın kendisi değil SHA-256 özeti
//     tutulur; token_prefix arayüzde token'ı tanımak için ilk karakterlerdir.
//   - monitor_location_settings: monitörün konum ayarı. Satır yoksa monitör
//     yalnızca ana sunucudan kontrol edilir (varsayılan, eski davranış).
//     include_local: ana sunucu da konumlardan biri mi; down_when: kesinti
//     kuralı (any | majority | all).
//   - monitor_locations: monitörün atandığı kontrol noktaları.
func init() {
	RegisterMigration(7, `
CREATE TABLE probes (
	id           INTEGER PRIMARY KEY,
	name         TEXT    NOT NULL,
	token_hash   TEXT    NOT NULL UNIQUE,
	token_prefix TEXT    NOT NULL DEFAULT '',
	active       INTEGER NOT NULL DEFAULT 1,
	created_at   INTEGER NOT NULL,
	last_seen_at INTEGER,
	last_ip      TEXT    NOT NULL DEFAULT '',
	version      TEXT    NOT NULL DEFAULT ''
);

CREATE TABLE monitor_location_settings (
	monitor_id    INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	include_local INTEGER NOT NULL DEFAULT 1,
	down_when     TEXT    NOT NULL DEFAULT 'any',
	PRIMARY KEY (monitor_id)
);

CREATE TABLE monitor_locations (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	probe_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	PRIMARY KEY (monitor_id, probe_id)
) WITHOUT ROWID;
CREATE INDEX monitor_locations_probe ON monitor_locations(probe_id);
`)
}

// Kesinti kuralları: monitör hangi durumda DOWN sayılır.
const (
	DownWhenAny      = "any"      // herhangi bir konum çalışmıyorsa (varsayılan)
	DownWhenMajority = "majority" // konumların çoğunluğu çalışmıyorsa
	DownWhenAll      = "all"      // tüm konumlar çalışmıyorsa
)

// ValidDownWhen kuralın geçerli olup olmadığını söyler.
func ValidDownWhen(s string) bool {
	return s == DownWhenAny || s == DownWhenMajority || s == DownWhenAll
}

// Probe uzak kontrol noktası. Zaman damgaları unix saniye, 0 "yok".
type Probe struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	Active      bool   `json:"active"`
	CreatedAt   int64  `json:"created_at"`
	LastSeenAt  int64  `json:"last_seen_at"`
	LastIP      string `json:"last_ip"`
	Version     string `json:"version"`
	Hash        string `json:"-"`
}

const probeCols = `id, name, token_prefix, active, created_at, last_seen_at, last_ip, version, token_hash`

func scanProbe(sc scanner) (Probe, error) {
	var p Probe
	var seen sql.NullInt64
	err := sc.Scan(&p.ID, &p.Name, &p.TokenPrefix, &p.Active, &p.CreatedAt, &seen, &p.LastIP, &p.Version, &p.Hash)
	p.LastSeenAt = seen.Int64
	return p, err
}

func (s *Store) queryProbes(ctx context.Context, q string, args ...any) ([]Probe, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Probe{}
	for rows.Next() {
		p, err := scanProbe(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListProbes tüm kontrol noktaları, ada göre sıralı.
func (s *Store) ListProbes(ctx context.Context) ([]Probe, error) {
	return s.queryProbes(ctx, "SELECT "+probeCols+" FROM probes ORDER BY LOWER(name), id")
}

func (s *Store) getProbe(ctx context.Context, where string, arg any) (Probe, error) {
	p, err := scanProbe(s.db.QueryRowContext(ctx, "SELECT "+probeCols+" FROM probes WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) GetProbe(ctx context.Context, id int64) (Probe, error) {
	return s.getProbe(ctx, "id = ?", id)
}

// ProbeByTokenHash kontrol noktasını token özetinden bulur (kimlik doğrulama).
func (s *Store) ProbeByTokenHash(ctx context.Context, hash string) (Probe, error) {
	return s.getProbe(ctx, "token_hash = ?", hash)
}

// ProbeNameTaken aynı adda (büyük/küçük harf farkı gözetmeden) başka bir
// kontrol noktası var mı? exceptID düzenlenen kaydın kendisidir.
func (s *Store) ProbeNameTaken(ctx context.Context, name string, exceptID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM probes WHERE LOWER(name) = LOWER(?) AND id <> ?", name, exceptID).Scan(&n)
	return n > 0, err
}

func (s *Store) CountProbes(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM probes").Scan(&n)
	return n, err
}

func (s *Store) CreateProbe(ctx context.Context, p *Probe) error {
	return s.db.QueryRowContext(ctx, `
		INSERT INTO probes (name, token_hash, token_prefix, active, created_at)
		VALUES (?, ?, ?, ?, ?) RETURNING id`,
		p.Name, p.Hash, p.TokenPrefix, boolInt(p.Active), p.CreatedAt).Scan(&p.ID)
}

// UpdateProbe adı ve etkinliği değiştirir.
func (s *Store) UpdateProbe(ctx context.Context, id int64, name string, active bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET name = ?, active = ? WHERE id = ?", name, boolInt(active), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProbeToken token'ı yeniler; eski token hemen geçersiz olur.
func (s *Store) SetProbeToken(ctx context.Context, id int64, hash, prefix string) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET token_hash = ?, token_prefix = ? WHERE id = ?", hash, prefix, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// TouchProbe kontrol noktasının son görülme bilgisini yazar (çağıran seyreltir).
func (s *Store) TouchProbe(ctx context.Context, id, now int64, ip, version string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE probes SET last_seen_at = ?, last_ip = ?, version = ? WHERE id = ?", now, ip, version, id)
	return err
}

// DeleteProbe kontrol noktasını siler ve etkilenen monitörlerin kimliklerini
// döner. Atamalar kendiliğinden silinir; başka konumu kalmayan monitör ana
// sunucuya döner (izlemesiz kalmasın), yalnızca ana sunucusu kalan monitörün
// ayar satırı silinir (varsayılan davranış).
func (s *Store) DeleteProbe(ctx context.Context, id int64) ([]int64, error) {
	var affected []int64
	err := s.tx(ctx, func(tx *Tx) error {
		var err error
		if affected, err = probeMonitorIDs(ctx, tx, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM probes WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		// SQLite'ta foreign_keys açık; PostgreSQL'de de ON DELETE CASCADE var.
		// Yine de iki veritabanında aynı sonuç için atamalar açıkça silinir.
		if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_locations WHERE probe_id = ?", id); err != nil {
			return err
		}
		const noProbes = "NOT EXISTS (SELECT 1 FROM monitor_locations l WHERE l.monitor_id = monitor_location_settings.monitor_id)"
		if _, err := tx.ExecContext(ctx,
			"UPDATE monitor_location_settings SET include_local = 1 WHERE include_local = 0 AND "+noProbes); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM monitor_location_settings WHERE include_local = 1 AND "+noProbes)
		return err
	})
	return affected, err
}

func probeMonitorIDs(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, probeID int64) ([]int64, error) {
	rows, err := q.QueryContext(ctx, "SELECT monitor_id FROM monitor_locations WHERE probe_id = ? ORDER BY monitor_id", probeID)
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

// ProbeMonitorIDs kontrol noktasına atanmış monitörler.
func (s *Store) ProbeMonitorIDs(ctx context.Context, probeID int64) ([]int64, error) {
	return probeMonitorIDs(ctx, s.db, probeID)
}

// ProbeMonitorCounts kontrol noktası başına atanmış monitör sayısı.
func (s *Store) ProbeMonitorCounts(ctx context.Context) (map[int64]int, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT probe_id, COUNT(*) FROM monitor_locations GROUP BY probe_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

// ProbeJobs kontrol noktasına atanmış aktif monitörler (tam ayarlarıyla).
// Tip süzmesi (push/grup) çağıranda yapılır.
func (s *Store) ProbeJobs(ctx context.Context, probeID int64) ([]Monitor, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+monitorCols+` FROM monitors
		WHERE active = 1 AND id IN (SELECT monitor_id FROM monitor_locations WHERE probe_id = ?)
		ORDER BY id`, probeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// LocationSetup monitörün konum ayarı. Configured=false ise monitör yalnızca
// ana sunucudan kontrol edilir (IncludeLocal=true, ProbeIDs boş, DownWhen any).
type LocationSetup struct {
	IncludeLocal bool    `json:"include_local"`
	ProbeIDs     []int64 `json:"probe_ids"`
	DownWhen     string  `json:"down_when"`
}

// Configured varsayılandan (yalnızca ana sunucu) farklı bir ayar mı?
func (l LocationSetup) Configured() bool { return !l.IncludeLocal || len(l.ProbeIDs) > 0 }

// DefaultLocations yalnızca ana sunucu.
func DefaultLocations() LocationSetup {
	return LocationSetup{IncludeLocal: true, ProbeIDs: []int64{}, DownWhen: DownWhenAny}
}

// AllMonitorLocations yalnızca konum ayarı yapılmış monitörlerin ayarları.
func (s *Store) AllMonitorLocations(ctx context.Context) (map[int64]LocationSetup, error) {
	return s.monitorLocations(ctx, 0)
}

// MonitorLocations monitörün konum ayarı; ayar yoksa DefaultLocations.
func (s *Store) MonitorLocations(ctx context.Context, monitorID int64) (LocationSetup, error) {
	all, err := s.monitorLocations(ctx, monitorID)
	if err != nil {
		return LocationSetup{}, err
	}
	if l, ok := all[monitorID]; ok {
		return l, nil
	}
	return DefaultLocations(), nil
}

// monitorLocations monitorID 0 ise hepsini okur (tek sorgu).
func (s *Store) monitorLocations(ctx context.Context, monitorID int64) (map[int64]LocationSetup, error) {
	where, args := "", []any{}
	if monitorID != 0 {
		where, args = " WHERE s.monitor_id = ?", []any{monitorID}
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.monitor_id, s.include_local, s.down_when, l.probe_id
		FROM monitor_location_settings s
		LEFT JOIN monitor_locations l ON l.monitor_id = s.monitor_id`+where+`
		ORDER BY s.monitor_id, l.probe_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]LocationSetup{}
	for rows.Next() {
		var id int64
		var local bool
		var downWhen string
		var pid sql.NullInt64
		if err := rows.Scan(&id, &local, &downWhen, &pid); err != nil {
			return nil, err
		}
		l, ok := out[id]
		if !ok {
			l = LocationSetup{IncludeLocal: local, ProbeIDs: []int64{}, DownWhen: downWhen}
		}
		if pid.Valid {
			l.ProbeIDs = append(l.ProbeIDs, pid.Int64)
		}
		out[id] = l
	}
	return out, rows.Err()
}

// SetMonitorLocations monitörün konum ayarını yazar. Varsayılan ayar (yalnızca
// ana sunucu) satır bırakmaz.
func (s *Store) SetMonitorLocations(ctx context.Context, monitorID int64, l LocationSetup) error {
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_locations WHERE monitor_id = ?", monitorID); err != nil {
			return err
		}
		if !l.Configured() {
			_, err := tx.ExecContext(ctx, "DELETE FROM monitor_location_settings WHERE monitor_id = ?", monitorID)
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO monitor_location_settings (monitor_id, include_local, down_when) VALUES (?, ?, ?)
			ON CONFLICT (monitor_id) DO UPDATE SET include_local = excluded.include_local, down_when = excluded.down_when`,
			monitorID, boolInt(l.IncludeLocal), l.DownWhen); err != nil {
			return err
		}
		for _, pid := range l.ProbeIDs {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO monitor_locations (monitor_id, probe_id) VALUES (?, ?) ON CONFLICT DO NOTHING",
				monitorID, pid); err != nil {
				return err
			}
		}
		return nil
	})
}

// ProbesByIDs verilen kimliklerdeki kontrol noktaları (olmayanlar atlanır).
func (s *Store) ProbesByIDs(ctx context.Context, ids []int64) (map[int64]Probe, error) {
	out := map[int64]Probe{}
	if len(ids) == 0 {
		return out, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	list, err := s.queryProbes(ctx, "SELECT "+probeCols+" FROM probes WHERE id IN ("+ph+")", args...)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		out[p.ID] = p
	}
	return out, nil
}
