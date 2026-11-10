package store

// Denetim sonuçları (iş 741, docs/GUVENLIK-EKLENTI-730.md §B.3). Eklentiler
// (739) ve yerleşik denetimler (743) aynı tabloları kullanır.
//
//   - check_state: denetimin son sonucu ve panel yapılandırması (aç/kapat,
//     aralık, eklentinin imzalı manifesti + gövdesi; ajan imzayı ve yerel
//     iğneyi kendisi doğrular, panel yalnız taşır). status_since o durumun
//     başladığı an (kural süresi bundan), unknown_streak art arda unknown sayısı.
//   - check_samples: metrik örnekleri (sn). TimescaleDB'de hypertable (1 gün
//     parça, 7 gün sonra sıkıştırma, 30 gün saklama) + check_hourly sürekli
//     özet (365 gün). Düz PG'de (ve Apache lisanslı Timescale'de) tablo +
//     yazımda UPSERT edilen check_hourly tablosu; saklama Go bakımıyla.
//     SQLite'ta örnek yazılmaz (G8): yalnız son durum ve metrikler.
//   - server_alerts.below: küçük değer kötü (eşik altına inince tetiklenir).
//     Denetim kuralları metric='check', mount='<ad>' (durum) ya da
//     '<ad>/<metrik>' (eşik).
//
// Numara: tasarım belgesindeki 53, 735'in varsayılan güvenlik kurallarına
// verildi; bu yüzden 54.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func init() {
	RegisterMigration(54, `
CREATE TABLE check_state (
	server_id      INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	name           TEXT    NOT NULL,
	source         TEXT    NOT NULL DEFAULT 'plugin',
	kind           TEXT    NOT NULL DEFAULT '',
	version        INTEGER NOT NULL DEFAULT 0,
	status         TEXT    NOT NULL DEFAULT '',
	message        TEXT    NOT NULL DEFAULT '',
	error          TEXT    NOT NULL DEFAULT '',
	stderr         TEXT    NOT NULL DEFAULT '',
	metrics        TEXT    NOT NULL DEFAULT '[]',
	dropped        INTEGER NOT NULL DEFAULT 0,
	truncated      INTEGER NOT NULL DEFAULT 0,
	last_run       INTEGER NOT NULL DEFAULT 0,
	last_ok        INTEGER NOT NULL DEFAULT 0,
	duration_ms    INTEGER NOT NULL DEFAULT 0,
	status_since   INTEGER NOT NULL DEFAULT 0,
	unknown_streak INTEGER NOT NULL DEFAULT 0,
	enabled        INTEGER NOT NULL DEFAULT 1,
	interval_sec   INTEGER NOT NULL DEFAULT 0,
	manifest       TEXT    NOT NULL DEFAULT '',
	body           TEXT    NOT NULL DEFAULT '',
	updated_at     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (server_id, name)
);
CREATE TABLE check_samples (
	server_id INTEGER NOT NULL,
	name      TEXT    NOT NULL,
	metric    TEXT    NOT NULL,
	time      INTEGER NOT NULL,
	value     DOUBLE PRECISION NOT NULL,
	PRIMARY KEY (server_id, name, metric, time)
);
CREATE INDEX check_samples_time ON check_samples(time);
CREATE TABLE check_hourly (
	server_id INTEGER NOT NULL,
	name      TEXT    NOT NULL,
	metric    TEXT    NOT NULL,
	bucket    INTEGER NOT NULL,
	n         INTEGER NOT NULL,
	vsum      DOUBLE PRECISION NOT NULL,
	vmin      DOUBLE PRECISION NOT NULL,
	vmax      DOUBLE PRECISION NOT NULL,
	PRIMARY KEY (server_id, name, metric, bucket)
);
ALTER TABLE server_alerts ADD COLUMN below INTEGER NOT NULL DEFAULT 0;
`)
	tsExtras = append(tsExtras, (*Store).tsChecks)
}

// Sınırlar ve saklama.
const (
	MaxChecksPerServer = 20 // B.3: sunucu başına eklenti/denetim
	checkSamplesKeep   = 30 * 86400
	checkHourlyKeep    = 365 * 86400
	MetricCheck        = "check" // server_alerts.metric
)

// ErrCheckLimit sunucunun denetim sınırı dolu (yeni ad kabul edilmez).
var ErrCheckLimit = errors.New("sunucu başına en fazla 20 denetim olabilir")

// CheckState denetimin son durumu + yapılandırması (manifest/gövde JSON'a girmez).
type CheckState struct {
	Name          string          `json:"name"`
	Source        string          `json:"source"` // plugin | builtin
	Kind          string          `json:"kind,omitempty"`
	Version       int64           `json:"version"`
	Status        string          `json:"status"` // ok|warning|critical|unknown; "" henüz çalışmadı
	Message       string          `json:"message"`
	Error         string          `json:"error,omitempty"`
	Stderr        string          `json:"stderr,omitempty"`
	Metrics       json.RawMessage `json:"metrics"`
	Dropped       int             `json:"dropped,omitempty"`
	Truncated     bool            `json:"truncated,omitempty"`
	LastRun       int64           `json:"last_run"`
	LastOK        int64           `json:"last_ok"`
	DurationMs    int64           `json:"duration_ms"`
	StatusSince   int64           `json:"status_since"`
	UnknownStreak int             `json:"unknown_streak"`
	Enabled       bool            `json:"enabled"`
	Interval      int             `json:"interval"`
	HasManifest   bool            `json:"has_manifest"`
	UpdatedAt     int64           `json:"updated_at"`
}

const checkCols = `name, source, kind, version, status, message, error, stderr, metrics, dropped, truncated,
	last_run, last_ok, duration_ms, status_since, unknown_streak, enabled, interval_sec, manifest <> '', updated_at`

func scanCheck(sc scanner) (CheckState, error) {
	var c CheckState
	var metrics string
	var trunc, enabled int
	err := sc.Scan(&c.Name, &c.Source, &c.Kind, &c.Version, &c.Status, &c.Message, &c.Error, &c.Stderr, &metrics,
		&c.Dropped, &trunc, &c.LastRun, &c.LastOK, &c.DurationMs, &c.StatusSince, &c.UnknownStreak, &enabled,
		&c.Interval, &c.HasManifest, &c.UpdatedAt)
	c.Metrics = json.RawMessage(metrics)
	if !json.Valid(c.Metrics) {
		c.Metrics = json.RawMessage("[]")
	}
	c.Truncated, c.Enabled = trunc != 0, enabled != 0
	return c, err
}

// ListChecks sunucunun denetimleri (ada göre).
func (s *Store) ListChecks(ctx context.Context, serverID int64) ([]CheckState, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+checkCols+" FROM check_state WHERE server_id = ? ORDER BY name", serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckState{}
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetCheck tek denetim (yoksa sql.ErrNoRows).
func (s *Store) GetCheck(ctx context.Context, serverID int64, name string) (CheckState, error) {
	return scanCheck(s.db.QueryRowContext(ctx, "SELECT "+checkCols+" FROM check_state WHERE server_id = ? AND name = ?", serverID, name))
}

// CheckSample tek metrik değeri (taban birimde).
type CheckSample struct {
	Metric string
	Value  float64
}

// CheckResult kaydedilecek sonuç (doğrulanmış; t panel saatine çekilmiş, sn).
type CheckResult struct {
	Name, Source, Kind string
	Version            int64
	T                  int64
	DurationMs         int64
	Status             string
	Message            string
	Error, Stderr      string
	Metrics            json.RawMessage // arayüz için son metrik listesi
	Samples            []CheckSample
	Dropped            int
	Truncated          bool
}

// CheckChange kaydın önceki duruma göre farkı (servers kuralları için).
type CheckChange struct {
	New           bool   // ilk kez görüldü
	Prev          string // önceki durum ("" ilk sonuç)
	StatusSince   int64
	UnknownStreak int
	Enabled       bool
	Stale         bool // son çalıştırmadan eski sonuç: durum yazılmadı
}

// SaveCheckResult sonucu yazar. Yeni ad sınır doluysa ErrCheckLimit; eski
// (son çalıştırmadan önceki) sonuç yalnız örnek olarak yazılır, durumu ezmez.
func (s *Store) SaveCheckResult(ctx context.Context, serverID int64, r CheckResult, now int64) (CheckChange, error) {
	var ch CheckChange
	err := s.tx(ctx, func(tx *Tx) error {
		var lastRun int64
		var enabled int
		err := tx.QueryRowContext(ctx, "SELECT status, status_since, unknown_streak, last_run, enabled FROM check_state WHERE server_id = ? AND name = ?",
			serverID, r.Name).Scan(&ch.Prev, &ch.StatusSince, &ch.UnknownStreak, &lastRun, &enabled)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM check_state WHERE server_id = ?", serverID).Scan(&n); err != nil {
				return err
			}
			if n >= MaxChecksPerServer {
				return ErrCheckLimit
			}
			ch.New, enabled = true, 1
		case err != nil:
			return err
		}
		ch.Enabled = enabled != 0
		if !ch.New && r.T < lastRun {
			ch.Stale = true
			return nil // sıra dışı (kuyruktan geç gelen) sonuç: durum değişmez
		}
		if ch.Prev != r.Status || ch.StatusSince == 0 {
			ch.StatusSince = r.T
		}
		if r.Status == "unknown" {
			ch.UnknownStreak++
		} else {
			ch.UnknownStreak = 0
		}
		lastOK := int64(0)
		if r.Status == "ok" {
			lastOK = r.T
		}
		metrics := r.Metrics
		if !json.Valid(metrics) {
			metrics = json.RawMessage("[]")
		}
		_, err = tx.ExecContext(ctx, `
INSERT INTO check_state (server_id, name, source, kind, version, status, message, error, stderr, metrics, dropped, truncated,
	last_run, last_ok, duration_ms, status_since, unknown_streak, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (server_id, name) DO UPDATE SET source = excluded.source, kind = excluded.kind, version = excluded.version,
	status = excluded.status, message = excluded.message, error = excluded.error, stderr = excluded.stderr,
	metrics = excluded.metrics, dropped = excluded.dropped, truncated = excluded.truncated, last_run = excluded.last_run,
	last_ok = CASE WHEN excluded.last_ok > 0 THEN excluded.last_ok ELSE check_state.last_ok END,
	duration_ms = excluded.duration_ms, status_since = excluded.status_since, unknown_streak = excluded.unknown_streak`,
			serverID, r.Name, r.Source, r.Kind, r.Version, r.Status, r.Message, r.Error, r.Stderr, string(metrics), r.Dropped,
			boolInt(r.Truncated), r.T, lastOK, r.DurationMs, ch.StatusSince, ch.UnknownStreak, now)
		return err
	})
	if err != nil || !s.postgres || len(r.Samples) == 0 {
		return ch, err
	}
	return ch, s.writeCheckSamples(ctx, serverID, r.Name, r.T, r.Samples)
}

// writeCheckSamples örnekleri ve (sürekli özet yoksa) saatlik özeti yazar.
func (s *Store) writeCheckSamples(ctx context.Context, serverID int64, name string, t int64, list []CheckSample) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, m := range list {
			res, err := tx.ExecContext(ctx, `INSERT INTO check_samples (server_id, name, metric, time, value) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT (server_id, name, metric, time) DO NOTHING`, serverID, name, m.Metric, t, m.Value)
			if err != nil {
				return err
			}
			if s.tsFull() {
				continue
			}
			if n, _ := res.RowsAffected(); n == 0 {
				continue // yineleme: özet iki kez saymasın
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO check_hourly (server_id, name, metric, bucket, n, vsum, vmin, vmax)
				VALUES (?, ?, ?, ?, 1, ?, ?, ?)
				ON CONFLICT (server_id, name, metric, bucket) DO UPDATE SET n = check_hourly.n + 1, vsum = check_hourly.vsum + excluded.vsum,
					vmin = LEAST(check_hourly.vmin, excluded.vmin), vmax = GREATEST(check_hourly.vmax, excluded.vmax)`,
				serverID, name, m.Metric, t-t%3600, m.Value, m.Value, m.Value); err != nil {
				return err
			}
		}
		return nil
	})
}

// CheckConfig panelin denetim yapılandırması (nil alan değişmez).
type CheckConfig struct {
	Source   string // yeni kayıtta; boş = plugin
	Enabled  *bool
	Interval *int
	Manifest *string // imzalı zarf JSON ("" kaldırır)
	Body     *string // base64 gövde
	Version  *int64
}

// SetCheckConfig denetimin yapılandırmasını yazar (yoksa oluşturur; sınır
// doluysa ErrCheckLimit). created: yeni kayıt.
func (s *Store) SetCheckConfig(ctx context.Context, serverID int64, name string, c CheckConfig, now int64) (created bool, err error) {
	err = s.tx(ctx, func(tx *Tx) error {
		var one int
		err := tx.QueryRowContext(ctx, "SELECT 1 FROM check_state WHERE server_id = ? AND name = ?", serverID, name).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			var n int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM check_state WHERE server_id = ?", serverID).Scan(&n); err != nil {
				return err
			}
			if n >= MaxChecksPerServer {
				return ErrCheckLimit
			}
			src := c.Source
			if src == "" {
				src = "plugin"
			}
			if _, err := tx.ExecContext(ctx, "INSERT INTO check_state (server_id, name, source, updated_at) VALUES (?, ?, ?, ?)",
				serverID, name, src, now); err != nil {
				return err
			}
			created = true
		} else if err != nil {
			return err
		}
		set := []string{"updated_at = ?"}
		args := []any{now}
		if c.Enabled != nil {
			set, args = append(set, "enabled = ?"), append(args, boolInt(*c.Enabled))
		}
		if c.Interval != nil {
			set, args = append(set, "interval_sec = ?"), append(args, *c.Interval)
		}
		if c.Manifest != nil {
			set, args = append(set, "manifest = ?"), append(args, *c.Manifest)
		}
		if c.Body != nil {
			set, args = append(set, "body = ?"), append(args, *c.Body)
		}
		if c.Version != nil {
			set, args = append(set, "version = ?"), append(args, *c.Version)
		}
		_, err = tx.ExecContext(ctx, "UPDATE check_state SET "+strings.Join(set, ", ")+" WHERE server_id = ? AND name = ?",
			append(args, serverID, name)...)
		return err
	})
	return created, err
}

// CheckJob ajanın iş listesindeki denetim (eklentiajan.Item tel biçimi).
type CheckJob struct {
	Name     string          `json:"name"`
	Source   string          `json:"source"`
	Interval int             `json:"interval"`
	Manifest json.RawMessage `json:"manifest,omitempty"`
	Body     string          `json:"body,omitempty"`
}

// CheckJobs ajana gidecek etkin denetimler (eklentide manifest zorunlu).
func (s *Store) CheckJobs(ctx context.Context, serverID int64) ([]CheckJob, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT name, source, interval_sec, manifest, body FROM check_state
		WHERE server_id = ? AND enabled = 1 AND (source <> 'plugin' OR manifest <> '') ORDER BY name`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CheckJob{}
	for rows.Next() {
		var j CheckJob
		var man string
		if err := rows.Scan(&j.Name, &j.Source, &j.Interval, &man, &j.Body); err != nil {
			return nil, err
		}
		if man != "" {
			j.Manifest = json.RawMessage(man)
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// CheckPoint grafik noktası (ham örnekte üçü aynı).
type CheckPoint struct {
	T   int64   `json:"t"`
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// CheckSeries metriğin [from, to) aralığındaki noktaları: hourly ise saatlik
// özetten, değilse ham örneklerden. SQLite'ta örnek yok (boş).
func (s *Store) CheckSeries(ctx context.Context, serverID int64, name, metric string, from, to int64, hourly bool) ([]CheckPoint, error) {
	out := []CheckPoint{}
	if !s.postgres {
		return out, nil
	}
	q := `SELECT time, value, value, value FROM check_samples WHERE server_id = ? AND name = ? AND metric = ? AND time >= ? AND time < ? ORDER BY time`
	if hourly {
		q = `SELECT bucket, vsum / n, vmin, vmax FROM check_hourly WHERE server_id = ? AND name = ? AND metric = ? AND bucket >= ? AND bucket < ? ORDER BY bucket`
	}
	rows, err := s.db.QueryContext(ctx, q, serverID, name, metric, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p CheckPoint
		if err := rows.Scan(&p.T, &p.Avg, &p.Min, &p.Max); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CheckAlerts sunucunun denetim kuralları (name boşsa hepsi).
func (s *Store) CheckAlerts(ctx context.Context, probeID int64, name string) ([]ServerAlert, error) {
	all, err := s.ServerAlerts(ctx, probeID)
	if err != nil {
		return nil, err
	}
	out := []ServerAlert{}
	for _, a := range all {
		if a.Metric == MetricCheck && (name == "" || CheckRuleName(a.Mount) == name) {
			out = append(out, a)
		}
	}
	return out, nil
}

// CheckRuleName kural konusundan denetim adı ("<ad>" ya da "<ad>/<metrik>").
func CheckRuleName(mount string) string {
	name, _, _ := strings.Cut(mount, "/")
	return name
}

// ReplaceCheckAlerts denetimin kurallarını verilen listeyle değiştirir; diğer
// kurallara dokunmaz (ReplaceServerAlerts kalıbı: aynı konu yerinde güncellenir).
func (s *Store) ReplaceCheckAlerts(ctx context.Context, probeID int64, name string, rules []ServerAlert, now int64) ([]ServerAlert, error) {
	all, err := s.ServerAlerts(ctx, probeID)
	if err != nil {
		return nil, err
	}
	list := make([]ServerAlert, 0, len(all)+len(rules))
	for _, a := range all {
		if a.Metric != MetricCheck || CheckRuleName(a.Mount) != name {
			list = append(list, a)
		}
	}
	for _, r := range rules {
		if r.Metric != MetricCheck || CheckRuleName(r.Mount) != name {
			return nil, fmt.Errorf("kural %s denetimine ait değil", name)
		}
	}
	saved, err := s.ReplaceServerAlerts(ctx, probeID, append(list, rules...), now)
	if err != nil {
		return nil, err
	}
	out := []ServerAlert{}
	for _, a := range saved {
		if a.Metric == MetricCheck && CheckRuleName(a.Mount) == name {
			out = append(out, a)
		}
	}
	return out, nil
}

// EnsureCheckStatusRule denetimin durum kuralı yoksa varsayılanı ekler
// (kritik, hemen, etkin). Yalnız denetim ilk görüldüğünde çağrılır: silinen
// kural sonradan geri gelmez.
func (s *Store) EnsureCheckStatusRule(ctx context.Context, probeID int64, name string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO server_alerts (probe_id, metric, mount, threshold, minutes, active, firing)
		VALUES (?, ?, ?, ?, 1, 1, 0) ON CONFLICT (probe_id, metric, mount) DO NOTHING`, probeID, MetricCheck, name, CheckStatusCritical)
	return err
}

// Durum kuralının eşiği bit maskesidir: hangi durumlar tetikler.
const (
	CheckStatusWarning  = 1
	CheckStatusCritical = 2
	CheckStatusUnknown  = 4
)

// PruneChecks saklama süresini aşan örnekleri ve saatlik özetleri siler
// (TimescaleDB tam kipinde saklama politikaları yapar; SQLite'ta örnek yok).
func (s *Store) PruneChecks(ctx context.Context, now int64) error {
	if !s.postgres || s.tsFull() {
		return nil
	}
	if _, err := s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM check_samples WHERE (server_id, name, metric, time) IN
		(SELECT server_id, name, metric, time FROM check_samples WHERE time < ? LIMIT ?)`, now-checkSamplesKeep); err != nil {
		return err
	}
	_, err := s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM check_hourly WHERE (server_id, name, metric, bucket) IN
		(SELECT server_id, name, metric, bucket FROM check_hourly WHERE bucket < ? LIMIT ?)`, now-checkHourlyKeep)
	return err
}

// tsChecks check_samples'ı hypertable'a çevirir; tam lisansta sıkıştırma,
// saklama ve check_hourly sürekli özeti kurulur. Düz PG döneminden kalan
// check_hourly tablosu kaldırılır: özet ham örneklerden (30 gün) yeniden
// hesaplanır, daha eskisi kaybolur (sürekli özete tablo eklenemez).
func (s *Store) tsChecks(ctx context.Context, ext string, full bool) error {
	done, err := s.tsHas(ctx, "hypertables", "check_samples")
	if err != nil {
		return err
	}
	if !done {
		stmts := []string{
			fmt.Sprintf(`SELECT %s.create_hypertable('check_samples', %s.by_range('time', CAST(%d AS BIGINT)), migrate_data => true)`, ext, ext, tsChunkDay),
			fmt.Sprintf(`SELECT %s.set_integer_now_func('check_samples', 'bekci_now')`, ext),
		}
		if full {
			stmts = append(stmts,
				`ALTER TABLE check_samples SET (timescaledb.compress, timescaledb.compress_segmentby = 'server_id, name, metric', timescaledb.compress_orderby = 'time DESC')`,
				fmt.Sprintf(`SELECT %s.add_compression_policy('check_samples', compress_after => CAST(%d AS BIGINT))`, ext, 7*86400),
				fmt.Sprintf(`SELECT %s.add_retention_policy('check_samples', drop_after => CAST(%d AS BIGINT))`, ext, checkSamplesKeep))
		}
		if err := s.tsExec(ctx, stmts); err != nil {
			return err
		}
	}
	if !full {
		return nil
	}
	if done, err := s.tsHas(ctx, "continuous_aggregates", "check_hourly"); err != nil || done {
		return err
	}
	return s.tsExec(ctx, []string{
		`DROP TABLE IF EXISTS check_hourly`,
		fmt.Sprintf(`CREATE MATERIALIZED VIEW check_hourly WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
			SELECT server_id, name, metric, %s.time_bucket(CAST(3600 AS BIGINT), time) AS bucket,
				count(*) AS n, sum(value) AS vsum, min(value) AS vmin, max(value) AS vmax
			FROM check_samples GROUP BY 1, 2, 3, 4 WITH NO DATA`, ext),
		fmt.Sprintf(`SELECT %s.add_continuous_aggregate_policy('check_hourly', start_offset => CAST(%d AS BIGINT), end_offset => CAST(3600 AS BIGINT), schedule_interval => INTERVAL '30 minutes')`, ext, 3*86400),
		fmt.Sprintf(`SELECT %s.add_retention_policy('check_hourly', drop_after => CAST(%d AS BIGINT))`, ext, checkHourlyKeep),
	})
}

// CheckManifest denetimin kayıtlı manifest zarfı ("" yok; kayıt yoksa sql.ErrNoRows).
func (s *Store) CheckManifest(ctx context.Context, serverID int64, name string) (string, error) {
	var m string
	err := s.db.QueryRowContext(ctx, "SELECT manifest FROM check_state WHERE server_id = ? AND name = ?", serverID, name).Scan(&m)
	return m, err
}
