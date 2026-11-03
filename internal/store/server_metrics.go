package store

// Sunucu metrikleri sütunlu şema (iş 610b, tasarım §3.2-3.3). Ajan örneği
// (metrics.Stats JSON) bugün server_stats.data'da JSON olarak duruyor; TimescaleDB
// kipinde aynı örnek sütunlara da yazılır (server_metrics, server_disk_metrics,
// server_container_metrics). Okuma yolları 660'ta bu tablolara geçer; o zamana
// dek server_stats (Go özetleri) kaynak kalır. Düz PG/SQLite'ta bu tablolar boş
// kalır (okuyan yok, saklama politikası yok).

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

func init() {
	RegisterMigration(44, `
CREATE TABLE server_metrics (
	probe_id       INTEGER NOT NULL,
	time           INTEGER NOT NULL,
	cpu            DOUBLE PRECISION NOT NULL,
	load1          DOUBLE PRECISION NOT NULL,
	load5          DOUBLE PRECISION NOT NULL,
	load15         DOUBLE PRECISION NOT NULL,
	mem_total      INTEGER NOT NULL,
	mem_used       INTEGER NOT NULL,
	mem_cache      INTEGER NOT NULL,
	swap_total     INTEGER NOT NULL,
	swap_used      INTEGER NOT NULL,
	disk_read_bps  DOUBLE PRECISION NOT NULL,
	disk_write_bps DOUBLE PRECISION NOT NULL,
	net_rx_bps     DOUBLE PRECISION NOT NULL,
	net_tx_bps     DOUBLE PRECISION NOT NULL,
	uptime         INTEGER NOT NULL,
	temp_max       DOUBLE PRECISION,
	PRIMARY KEY (probe_id, time)
);
CREATE INDEX server_metrics_time ON server_metrics(time);
CREATE TABLE server_disk_metrics (
	probe_id INTEGER NOT NULL,
	time     INTEGER NOT NULL,
	mount    TEXT    NOT NULL,
	device   TEXT    NOT NULL DEFAULT '',
	fs       TEXT    NOT NULL DEFAULT '',
	total    INTEGER NOT NULL,
	used     INTEGER NOT NULL,
	PRIMARY KEY (probe_id, mount, time)
);
CREATE INDEX server_disk_metrics_time ON server_disk_metrics(time);
CREATE TABLE server_container_metrics (
	probe_id     INTEGER NOT NULL,
	time         INTEGER NOT NULL,
	name         TEXT    NOT NULL,
	container_id TEXT    NOT NULL DEFAULT '',
	cpu          DOUBLE PRECISION NOT NULL,
	mem          INTEGER NOT NULL,
	mem_limit    INTEGER NOT NULL DEFAULT 0,
	net_rx_bps   DOUBLE PRECISION NOT NULL,
	net_tx_bps   DOUBLE PRECISION NOT NULL,
	state        TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (probe_id, name, time)
);
CREATE INDEX server_container_metrics_time ON server_container_metrics(time);
`)
}

// tsServerTables hypertable ayarları: sıkıştırma segmentby, sıkıştırma ve
// saklama eşiği (sn).
var tsServerTables = []struct {
	name, segment    string
	compress, retain int64
}{
	{"server_metrics", "probe_id", 3 * 86400, 30 * 86400},
	{"server_disk_metrics", "probe_id, mount", 3 * 86400, 30 * 86400},
	{"server_container_metrics", "probe_id, name", 3 * 86400, 14 * 86400},
}

// tsServerMetrics sunucu metrik tablolarını hypertable'a çevirir, TSL'de
// sıkıştırma/saklama politikaları ve sürekli özetleri kurar (tablo başına
// idempotent; düz PG'den sonradan geçen kurulumda da çalışır).
func (s *Store) tsServerMetrics(ctx context.Context, ext string, full bool) error {
	for _, t := range tsServerTables {
		done, err := s.tsHas(ctx, "hypertables", t.name)
		if err != nil {
			return err
		}
		if done {
			continue
		}
		stmts := []string{
			fmt.Sprintf(`SELECT %s.create_hypertable('%s', %s.by_range('time', CAST(%d AS BIGINT)), migrate_data => true)`, ext, t.name, ext, tsChunkDay),
			fmt.Sprintf(`SELECT %s.set_integer_now_func('%s', 'bekci_now')`, ext, t.name),
		}
		if full {
			stmts = append(stmts,
				fmt.Sprintf(`ALTER TABLE %s SET (timescaledb.compress, timescaledb.compress_segmentby = '%s', timescaledb.compress_orderby = 'time DESC')`, t.name, t.segment),
				fmt.Sprintf(`SELECT %s.add_compression_policy('%s', compress_after => CAST(%d AS BIGINT))`, ext, t.name, t.compress),
				fmt.Sprintf(`SELECT %s.add_retention_policy('%s', drop_after => CAST(%d AS BIGINT))`, ext, t.name, t.retain))
		}
		if err := s.tsExec(ctx, stmts); err != nil {
			return err
		}
	}
	if !full {
		return nil
	}
	done, err := s.tsHas(ctx, "continuous_aggregates", "server_metrics_10m")
	if err != nil || done {
		return err
	}
	tb := func(w int, col string) string {
		return fmt.Sprintf("%s.time_bucket(CAST(%d AS BIGINT), %s)", ext, w, col)
	}
	pol := func(name string, start, end int64, every string) string {
		return fmt.Sprintf(`SELECT %s.add_continuous_aggregate_policy('%s', start_offset => CAST(%d AS BIGINT), end_offset => CAST(%d AS BIGINT), schedule_interval => INTERVAL '%s')`,
			ext, name, start, end, every)
	}
	ret := func(name string, days int64) string {
		return fmt.Sprintf(`SELECT %s.add_retention_policy('%s', drop_after => CAST(%d AS BIGINT))`, ext, name, days*86400)
	}
	cagg := "WITH (timescaledb.continuous, timescaledb.materialized_only = false)"
	return s.tsExec(ctx, []string{
		`CREATE MATERIALIZED VIEW server_metrics_10m ` + cagg + ` AS
			SELECT probe_id, ` + tb(600, "time") + ` AS bucket, count(*) AS n,
				avg(cpu) AS cpu_avg, max(cpu) AS cpu_max, avg(load1) AS load1_avg,
				avg(mem_used) AS mem_used_avg, max(mem_used) AS mem_used_max, max(mem_total) AS mem_total,
				avg(disk_read_bps) AS disk_read_avg, max(disk_read_bps) AS disk_read_max,
				avg(disk_write_bps) AS disk_write_avg, max(disk_write_bps) AS disk_write_max,
				avg(net_rx_bps) AS net_rx_avg, max(net_rx_bps) AS net_rx_max,
				avg(net_tx_bps) AS net_tx_avg, max(net_tx_bps) AS net_tx_max, max(temp_max) AS temp_max
			FROM server_metrics GROUP BY 1, 2 WITH NO DATA`,
		pol("server_metrics_10m", 86400, 600, "5 minutes"),
		ret("server_metrics_10m", 30),
		// 1 sa: 10 dk üzerine hiyerarşik; ortalamalar örnek sayısıyla ağırlıklı.
		`CREATE MATERIALIZED VIEW server_metrics_1h ` + cagg + ` AS
			SELECT probe_id, ` + tb(3600, "bucket") + ` AS bucket, CAST(sum(n) AS BIGINT) AS n,
				sum(cpu_avg * n) / sum(n) AS cpu_avg, max(cpu_max) AS cpu_max, sum(load1_avg * n) / sum(n) AS load1_avg,
				sum(mem_used_avg * n) / sum(n) AS mem_used_avg, max(mem_used_max) AS mem_used_max, max(mem_total) AS mem_total,
				sum(disk_read_avg * n) / sum(n) AS disk_read_avg, max(disk_read_max) AS disk_read_max,
				sum(disk_write_avg * n) / sum(n) AS disk_write_avg, max(disk_write_max) AS disk_write_max,
				sum(net_rx_avg * n) / sum(n) AS net_rx_avg, max(net_rx_max) AS net_rx_max,
				sum(net_tx_avg * n) / sum(n) AS net_tx_avg, max(net_tx_max) AS net_tx_max, max(temp_max) AS temp_max
			FROM server_metrics_10m GROUP BY 1, 2 WITH NO DATA`,
		pol("server_metrics_1h", 3*86400, 3600, "30 minutes"),
		ret("server_metrics_1h", 730),
		// Disk dolma tahmini (660): regr_slope(used_max, bucket).
		`CREATE MATERIALIZED VIEW server_disk_1h ` + cagg + ` AS
			SELECT probe_id, mount, ` + tb(3600, "time") + ` AS bucket,
				min(used) AS used_min, max(used) AS used_max, max(total) AS total
			FROM server_disk_metrics GROUP BY 1, 2, 3 WITH NO DATA`,
		pol("server_disk_1h", 3*86400, 3600, "30 minutes"),
		ret("server_disk_1h", 730),
		`CREATE MATERIALIZED VIEW server_container_1h ` + cagg + ` AS
			SELECT probe_id, name, ` + tb(3600, "time") + ` AS bucket,
				avg(cpu) AS cpu_avg, max(cpu) AS cpu_max, avg(mem) AS mem_avg, max(mem) AS mem_max,
				` + ext + `.last(state, time) AS state
			FROM server_container_metrics GROUP BY 1, 2, 3 WITH NO DATA`,
		pol("server_container_1h", 3*86400, 3600, "30 minutes"),
		ret("server_container_1h", 90),
	})
}

// tsExec deyimleri tek işlemde çalıştırır.
func (s *Store) tsExec(ctx context.Context, stmts []string) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%w\n%s", err, q)
			}
		}
		return nil
	})
}

// writeServerMetrics örneği sütunlu tablolara yazar (yalnız TimescaleDB;
// aynı dakikadaki önceki örneğin yerine geçer). Çözülemeyen örnek atlanır:
// JSON kaynak server_stats'ta durur.
func (s *Store) writeServerMetrics(ctx context.Context, tx *Tx, probeID, t int64, data []byte) error {
	if !s.tsFull() {
		return nil
	}
	var st metrics.Stats
	if json.Unmarshal(data, &st) != nil {
		return nil
	}
	var temp any
	for i, tm := range st.Temps {
		if i == 0 || tm.C > temp.(float64) {
			temp = tm.C
		}
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO server_metrics (probe_id, time, cpu, load1, load5, load15, mem_total, mem_used, mem_cache,
			swap_total, swap_used, disk_read_bps, disk_write_bps, net_rx_bps, net_tx_bps, uptime, temp_max)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (probe_id, time) DO UPDATE SET cpu = excluded.cpu, load1 = excluded.load1, load5 = excluded.load5,
			load15 = excluded.load15, mem_total = excluded.mem_total, mem_used = excluded.mem_used, mem_cache = excluded.mem_cache,
			swap_total = excluded.swap_total, swap_used = excluded.swap_used, disk_read_bps = excluded.disk_read_bps,
			disk_write_bps = excluded.disk_write_bps, net_rx_bps = excluded.net_rx_bps, net_tx_bps = excluded.net_tx_bps,
			uptime = excluded.uptime, temp_max = excluded.temp_max`,
		probeID, t, st.CPU, st.Load1, st.Load5, st.Load15, int64(st.MemTotal), int64(st.MemUsed), int64(st.MemCache),
		int64(st.SwapTotal), int64(st.SwapUsed), st.DiskReadBps, st.DiskWriteBps, st.NetRxBps, st.NetTxBps, st.Uptime, temp); err != nil {
		return err
	}
	for _, d := range st.Disks {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO server_disk_metrics (probe_id, time, mount, device, fs, total, used) VALUES (?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (probe_id, mount, time) DO UPDATE SET device = excluded.device, fs = excluded.fs,
				total = excluded.total, used = excluded.used`,
			probeID, t, d.Mount, d.Device, d.FS, int64(d.Total), int64(d.Used)); err != nil {
			return err
		}
	}
	for _, c := range st.Containers {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO server_container_metrics (probe_id, time, name, container_id, cpu, mem, mem_limit, net_rx_bps, net_tx_bps, state)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (probe_id, name, time) DO UPDATE SET container_id = excluded.container_id, cpu = excluded.cpu,
				mem = excluded.mem, mem_limit = excluded.mem_limit, net_rx_bps = excluded.net_rx_bps,
				net_tx_bps = excluded.net_tx_bps, state = excluded.state`,
			probeID, t, c.Name, c.ID, c.CPU, int64(c.Mem), int64(c.MemLimit), c.NetRxBps, c.NetTxBps, c.State); err != nil {
			return err
		}
	}
	return nil
}
