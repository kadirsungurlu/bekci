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

// Ajan türleri: aynı program ("uptime probe") iki ayrı amaçla kurulur ve
// kayıtlar birbirine karışmaz.
const (
	ProbeKindLocation = "location" // kontrol noktası: atanan monitörleri kontrol eder, metrik göndermez
	ProbeKindServer   = "server"   // takip edilen sunucu: metrik gönderir, monitör kontrolü almaz
)

// ValidProbeKind türün geçerli olup olmadığını söyler.
func ValidProbeKind(k string) bool { return k == ProbeKindLocation || k == ProbeKindServer }

// Probe uzak ajan: kontrol noktası veya takip edilen sunucu (Kind). Zaman
// damgaları unix saniye, 0 "yok".
type Probe struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	TokenPrefix string `json:"token_prefix"`
	Active      bool   `json:"active"`
	CreatedAt   int64  `json:"created_at"`
	LastSeenAt  int64  `json:"last_seen_at"`
	LastIP      string `json:"last_ip"`
	Version     string `json:"version"`
	Hash        string `json:"-"`

	// Sunucu takibi (servers.go): metrik toplama açık mı, son host bilgisi
	// (metrics.Host JSON'u, boş olabilir), son örnek zamanı ve ajanın
	// "toplayamıyorum" nedeni.
	Metrics     bool   `json:"metrics"`
	HostInfo    string `json:"-"`
	MetricsAt   int64  `json:"metrics_at"`
	MetricsNote string `json:"metrics_note"`

	// IPLock açıksa ajan yalnızca LockedIP'den bağlanabilir (probes_iplock.go).
	IPLock   bool   `json:"ip_lock"`
	LockedIP string `json:"locked_ip"`

	// NotifyOffline (kontrol noktası) çevrimdışı kalınca ve tekrar çevrimiçi
	// olunca bağlı kanallara (probe_notifications) bildirim gönderilir ve
	// probe_offline olayı açılır (migration 21).
	NotifyOffline bool `json:"notify_offline"`

	// Kendini güncelleme alanları (migration 35; probes_update.go).
	ProbeUpdate
}

const probeCols = `id, name, token_prefix, active, created_at, last_seen_at, last_ip, version, token_hash,
	metrics, host_info, metrics_at, metrics_note, kind, ip_lock, locked_ip, notify_offline,
	platform, auto_update, update_requested_at, update_status, update_target, update_note, update_at`

func scanProbe(sc scanner) (Probe, error) {
	var p Probe
	var seen, metricsAt, autoUpdate sql.NullInt64
	err := sc.Scan(&p.ID, &p.Name, &p.TokenPrefix, &p.Active, &p.CreatedAt, &seen, &p.LastIP, &p.Version, &p.Hash,
		&p.Metrics, &p.HostInfo, &metricsAt, &p.MetricsNote, &p.Kind, &p.IPLock, &p.LockedIP, &p.NotifyOffline,
		&p.Platform, &autoUpdate, &p.UpdateRequestedAt, &p.UpdateStatus, &p.UpdateTarget, &p.UpdateNote, &p.UpdateAt)
	p.LastSeenAt, p.MetricsAt = seen.Int64, metricsAt.Int64
	if autoUpdate.Valid {
		on := autoUpdate.Int64 != 0
		p.AutoUpdate = &on
	}
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

// ListProbes tüm ajanlar (kontrol noktaları ve sunucular), ada göre sıralı.
func (s *Store) ListProbes(ctx context.Context) ([]Probe, error) {
	return s.queryProbes(ctx, "SELECT "+probeCols+" FROM probes ORDER BY LOWER(name), id")
}

// ListProbesOfKind yalnızca verilen türdeki ajanlar, ada göre sıralı.
func (s *Store) ListProbesOfKind(ctx context.Context, kind string) ([]Probe, error) {
	return s.queryProbes(ctx, "SELECT "+probeCols+" FROM probes WHERE kind = ? ORDER BY LOWER(name), id", kind)
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

// ProbeNameTaken aynı türde aynı adda (büyük/küçük harf farkı gözetmeden)
// başka bir ajan var mı? exceptID düzenlenen kaydın kendisidir. Bir kontrol
// noktası ile bir sunucu aynı adı taşıyabilir (ör. aynı makine).
func (s *Store) ProbeNameTaken(ctx context.Context, name, kind string, exceptID int64) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM probes WHERE LOWER(name) = LOWER(?) AND kind = ? AND id <> ?", name, kind, exceptID).Scan(&n)
	return n > 0, err
}

// CountProbes verilen türdeki ajan sayısı.
func (s *Store) CountProbes(ctx context.Context, kind string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM probes WHERE kind = ?", kind).Scan(&n)
	return n, err
}

// CreateProbe ajanı ekler. Tür boşsa kontrol noktasıdır; metrik toplama
// yalnızca sunucularda açıktır.
func (s *Store) CreateProbe(ctx context.Context, p *Probe) error {
	if !ValidProbeKind(p.Kind) {
		p.Kind = ProbeKindLocation
	}
	p.Metrics = p.Kind == ProbeKindServer
	return s.db.QueryRowContext(ctx, `
		INSERT INTO probes (name, kind, token_hash, token_prefix, active, created_at, metrics, ip_lock)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		p.Name, p.Kind, p.Hash, p.TokenPrefix, boolInt(p.Active), p.CreatedAt, boolInt(p.Metrics), boolInt(p.IPLock)).Scan(&p.ID)
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
	// NotifyPartial konum kesintisinde (monitör çalışırken bir konum
	// düştüğünde) de bildirim gönderilir; düzelince de.
	NotifyPartial bool `json:"notify_partial"`
}

func init() {
	// 19: konum kesintisi bildirimi (monitör başına; varsayılan kapalı).
	RegisterMigration(19, `ALTER TABLE monitor_location_settings ADD COLUMN notify_partial INTEGER NOT NULL DEFAULT 0;`)
	// 21: kontrol noktası çevrimdışı bildirimi (varsayılan kapalı).
	RegisterMigration(21, `ALTER TABLE probes ADD COLUMN notify_offline INTEGER NOT NULL DEFAULT 0;`)
}

// SetProbeNotifyOffline kontrol noktasının çevrimdışı bildirimini açar/kapatır.
func (s *Store) SetProbeNotifyOffline(ctx context.Context, id int64, on bool) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET notify_offline = ? WHERE id = ?", boolInt(on), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
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
		SELECT s.monitor_id, s.include_local, s.down_when, s.notify_partial, l.probe_id
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
		var local, notify bool
		var downWhen string
		var pid sql.NullInt64
		if err := rows.Scan(&id, &local, &downWhen, &notify, &pid); err != nil {
			return nil, err
		}
		l, ok := out[id]
		if !ok {
			l = LocationSetup{IncludeLocal: local, ProbeIDs: []int64{}, DownWhen: downWhen, NotifyPartial: notify}
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
	return s.tx(ctx, func(tx *Tx) error { return setMonitorLocationsTx(ctx, tx, monitorID, l) })
}

func setMonitorLocationsTx(ctx context.Context, tx *Tx, monitorID int64, l LocationSetup) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_locations WHERE monitor_id = ?", monitorID); err != nil {
		return err
	}
	if !l.Configured() {
		_, err := tx.ExecContext(ctx, "DELETE FROM monitor_location_settings WHERE monitor_id = ?", monitorID)
		return err
	}
	if l.DownWhen == "" {
		l.DownWhen = DownWhenAny
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO monitor_location_settings (monitor_id, include_local, down_when, notify_partial) VALUES (?, ?, ?, ?)
		ON CONFLICT (monitor_id) DO UPDATE SET include_local = excluded.include_local, down_when = excluded.down_when,
			notify_partial = excluded.notify_partial`,
		monitorID, boolInt(l.IncludeLocal), l.DownWhen, boolInt(l.NotifyPartial)); err != nil {
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
