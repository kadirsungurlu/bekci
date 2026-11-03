package store

// TimescaleDB kipi (iş 610, tasarım docs/TIMESCALE-TASARIM.md §3, §5).
//
// Açılışta (migration'lardan sonra, migration kilidi tutulurken) veritabanı
// türü belirlenir:
//
//   - DBModeTimescale: eklenti kurulu, lisans "timescale" (TSL). heartbeats ve
//     mesh_samples hypertable olur, sıkıştırma politikası eklenir; saatlik özet
//     sürekli özettir (stats_hourly_ts) ve "stats_hourly" adı onu dönüşüm
//     öncesi Go özetiyle (stats_hourly_legacy) birleştiren bir görünümdür.
//     Okuma sorgularının metni değişmez; writeBeat saatlik UPSERT'i atlar.
//   - DBModeTimescaleApache: eklenti var ama lisans "apache" (-oss imaj, bazı
//     yönetilen PG): yalnız hypertable + drop_chunks; özetler Go'da.
//   - DBModePostgres: eklenti yok (ör. yönetilen PG). Düz PG yolu: bugünkü Go
//     özet tabloları. Çalışır ama önerilmez; açılışta uyarı yazılır.
//   - DBModeSQLite: kullanımdan kalkıyor.
//
// Dönüşüm tek yönlü ve idempotenttir: düz PG kurulumuna sonradan eklenti
// gelirse bir sonraki açılışta çevrilir. DB_TIMESCALE=off dönüşümü kapatır
// (Options.TimescaleOff).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// DBMode veritabanı türü (bkz. paket başı).
type DBMode string

const (
	DBModeSQLite          DBMode = "sqlite"
	DBModePostgres        DBMode = "postgres"
	DBModeTimescaleApache DBMode = "timescale-apache"
	DBModeTimescale       DBMode = "timescale"
)

// DBMode açılışta belirlenen veritabanı türü.
func (s *Store) DBMode() DBMode {
	if s.mode == "" {
		if s.postgres {
			return DBModePostgres
		}
		return DBModeSQLite
	}
	return s.mode
}

// tsFull saatlik özet sürekli özet mi (writeBeat UPSERT'i atlar).
func (s *Store) tsFull() bool { return s.mode == DBModeTimescale }

// tsHyper heartbeats hypertable mı (saklama drop_chunks ile).
func (s *Store) tsHyper() bool {
	return s.mode == DBModeTimescale || s.mode == DBModeTimescaleApache
}

// PingBounds gecikme histogramının kova üst sınırları (ms). Kova k
// (h00..h16): [PingBounds[k-1], PingBounds[k]); h00 < 10 ms, h16 >= 30000 ms.
// p95 = birikimli sayacın %95'i geçtiği kovada doğrusal ara değer (iş 620).
var PingBounds = []int64{10, 20, 30, 50, 75, 100, 150, 200, 300, 500, 750, 1000, 2000, 5000, 10000, 30000}

// histCol ping değerinin düştüğü histogram sütunu.
func histCol(ping int64) string {
	k := len(PingBounds)
	for i, b := range PingBounds {
		if ping < b {
			k = i
			break
		}
	}
	return fmt.Sprintf("h%02d", k)
}

// histCols "h00, h01, ..., h16".
func histCols(prefix string) string {
	var b strings.Builder
	for k := 0; k <= len(PingBounds); k++ {
		if k > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%sh%02d", prefix, k)
	}
	return b.String()
}

func init() {
	// 43: histogram sütunları (Go özetinde de), sıfırlama sınırı, mesh örnekleri.
	var cols strings.Builder
	for k := 0; k <= len(PingBounds); k++ {
		fmt.Fprintf(&cols, "ALTER TABLE stats_hourly ADD COLUMN h%02d INTEGER NOT NULL DEFAULT 0;\n", k)
	}
	RegisterMigration(43, cols.String()+`
ALTER TABLE monitors ADD COLUMN stats_reset_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE mesh_samples (
	src_id INTEGER NOT NULL,
	dst_id INTEGER NOT NULL,
	time   INTEGER NOT NULL,
	rtt_ms INTEGER,
	ok     INTEGER NOT NULL
);
CREATE INDEX mesh_samples_pair_time ON mesh_samples(src_id, dst_id, time);
CREATE INDEX mesh_samples_time ON mesh_samples(time);
`)
}

// tsMinVersion dönüşümün gerektirdiği şema sürümü (43 mesh_samples + histogram, 44 sunucu metrikleri).
const tsMinVersion = 44

// Saklama ve sıkıştırma (tasarım §3.2). Ham saklama ayardan (Go bakımı
// drop_chunks çağırır); burada yalnız parça aralığı ve sıkıştırma eşiği.
const (
	tsChunkDay        = 86400
	tsCompressBeats   = 7 * 86400
	tsCompressMesh    = 3 * 86400
	tsCaggStart       = 3 * 86400 // sürekli özet yenileme penceresi başı
	tsCaggEnd         = 3600      // ... sonu (süren saat gerçek zamanlı)
	tsDailyStart      = 7 * 86400
	tsDailyEnd        = 86400
	tsMeshRetainDays  = 30
	tsConvertDeadline = 30 * time.Minute
)

// ensureTimescale veritabanı türünü belirler, gerekirse dönüşümü yapar.
// Hata yalnız dönüşüm başlayıp başarısız olursa döner; eklenti yoksa düz PG.
func (s *Store) ensureTimescale(ctx context.Context) error {
	if !s.postgres {
		s.mode = DBModeSQLite
		return nil
	}
	s.mode = DBModePostgres
	if v, err := s.schemaVersion(ctx); err != nil || v < tsMinVersion {
		return err // eski şema (yalnız testler: migration'lar kısıtlı)
	}
	if s.opts.TimescaleOff {
		s.opts.Log.Warn("TimescaleDB kapalı (DB_TIMESCALE=off): düz PostgreSQL yolu")
		return nil
	}
	ext, err := s.tsExtension(ctx)
	if err != nil {
		return err
	}
	if ext == "" {
		s.opts.Log.Warn("Veritabanı: düz PostgreSQL, TimescaleDB yok. Çalışır ama önerilmez, büyük ölçekte yavaşlar (bkz. belgeler: Veritabanı)")
		return nil
	}
	var license string
	if err := s.db.QueryRowContext(ctx, "SHOW timescaledb.license").Scan(&license); err != nil {
		return fmt.Errorf("timescaledb.license okunamadı: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, tsConvertDeadline)
	defer cancel()
	if err := s.tsConvertHyper(cctx, ext); err != nil {
		return fmt.Errorf("TimescaleDB hypertable dönüşümü: %w", err)
	}
	if err := s.tsServerMetrics(cctx, ext, license == "timescale"); err != nil {
		return fmt.Errorf("TimescaleDB sunucu metrikleri: %w", err)
	}
	s.mode = DBModeTimescaleApache
	if license != "timescale" {
		s.opts.Log.Warn("TimescaleDB Apache lisansı: sıkıştırma ve sürekli özet yok, özetler uygulamada", "license", license)
		return nil
	}
	if err := s.tsConvertFull(cctx, ext); err != nil {
		return fmt.Errorf("TimescaleDB sürekli özet dönüşümü: %w", err)
	}
	s.mode = DBModeTimescale
	return nil
}

// tsExtension eklentinin kurulu olduğu şemayı döner ("" = eklenti yok).
// Kurulu değil ama sunucuda varsa kurmayı dener (yetki gerekir).
func (s *Store) tsExtension(ctx context.Context) (string, error) {
	q := `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = 'timescaledb'`
	var schema string
	err := s.db.QueryRowContext(ctx, q).Scan(&schema)
	if err == nil {
		s.tsExt = schema
		return schema, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	var avail int
	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM pg_available_extensions WHERE name = 'timescaledb'").Scan(&avail); err != nil || avail == 0 {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS timescaledb SCHEMA public"); err != nil {
		s.opts.Log.Warn("TimescaleDB sunucuda var ama kurulamadı; düz PostgreSQL yolu. "+
			"shared_preload_libraries = 'timescaledb' ve CREATE EXTENSION yetkisi gerekir", "err", err)
		return "", nil
	}
	err = s.db.QueryRowContext(ctx, q).Scan(&schema)
	if err == nil {
		s.tsExt = schema
	}
	return schema, err
}

// tsHas current_schema'da verilen hypertable / sürekli özet var mı.
func (s *Store) tsHas(ctx context.Context, view, name string) (bool, error) {
	col := "hypertable"
	if view == "continuous_aggregates" {
		col = "view"
	}
	var n int
	err := s.db.QueryRowContext(ctx, fmt.Sprintf(
		"SELECT COUNT(*) FROM timescaledb_information.%s WHERE %s_schema = current_schema() AND %s_name = ?",
		view, col, col), name).Scan(&n)
	return n > 0, err
}

// tsConvertHyper heartbeats ve mesh_samples'ı hypertable'a çevirir (var olan
// veriyle; tablo dönüşüm boyunca kilitli).
func (s *Store) tsConvertHyper(ctx context.Context, ext string) error {
	done, err := s.tsHas(ctx, "hypertables", "heartbeats")
	if err != nil || done {
		return err
	}
	start := time.Now()
	var rows int64
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM heartbeats").Scan(&rows)
	s.opts.Log.Info("TimescaleDB: hypertable dönüşümü başlıyor (tablo kilitli)", "heartbeats", rows)
	err = s.tx(ctx, func(tx *Tx) error {
		stmts := []string{
			`CREATE OR REPLACE FUNCTION bekci_now() RETURNS BIGINT LANGUAGE SQL STABLE AS 'SELECT CAST(extract(epoch FROM now()) AS BIGINT)'`,
			// Hypertable'da benzersiz anahtar zaman sütununu içermeli.
			`ALTER TABLE heartbeats DROP CONSTRAINT heartbeats_pkey`,
			`ALTER TABLE heartbeats ADD PRIMARY KEY (monitor_id, time, id)`,
			`DROP INDEX IF EXISTS heartbeats_monitor_time`,
		}
		for _, t := range []string{"heartbeats", "mesh_samples"} {
			stmts = append(stmts,
				fmt.Sprintf(`SELECT %s.create_hypertable('%s', %s.by_range('time', CAST(%d AS BIGINT)), migrate_data => true)`, ext, t, ext, tsChunkDay),
				fmt.Sprintf(`SELECT %s.set_integer_now_func('%s', 'bekci_now')`, ext, t))
		}
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%w\n%s", err, q)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.opts.Log.Info("TimescaleDB: hypertable dönüşümü bitti", "sure", time.Since(start).Round(time.Millisecond))
	return nil
}

// tsConvertFull sıkıştırma politikaları ve sürekli özetler (TSL).
//
// stats_hourly tablosu stats_hourly_legacy olur; dönüşüm anından (saat başı:
// cutoff) önceki saatler oradan, sonrası sürekli özetten okunur. Böylece
// ham saklamanın ötesindeki geçmiş kaybolmaz ve okuma sorguları değişmez.
// Yeni (boş) veritabanında cutoff 0: her şey sürekli özetten.
func (s *Store) tsConvertFull(ctx context.Context, ext string) error {
	done, err := s.tsHas(ctx, "continuous_aggregates", "stats_hourly_ts")
	if err != nil || done {
		return err
	}
	hist := make([]string, 0, len(PingBounds)+1)
	for k := 0; k <= len(PingBounds); k++ {
		cond := ""
		switch {
		case k == 0:
			cond = fmt.Sprintf("ping_ms < %d", PingBounds[0])
		case k == len(PingBounds):
			cond = fmt.Sprintf("ping_ms >= %d", PingBounds[k-1])
		default:
			cond = fmt.Sprintf("ping_ms >= %d AND ping_ms < %d", PingBounds[k-1], PingBounds[k])
		}
		hist = append(hist, fmt.Sprintf("count(*) FILTER (WHERE %s) AS h%02d", cond, k))
	}
	sumHist := make([]string, 0, len(PingBounds)+1)
	for k := 0; k <= len(PingBounds); k++ {
		sumHist = append(sumHist, fmt.Sprintf("CAST(sum(h%02d) AS BIGINT) AS h%02d", k, k))
	}
	return s.tx(ctx, func(tx *Tx) error {
		var legacyRows int64
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_hourly").Scan(&legacyRows); err != nil {
			return err
		}
		cutoff := int64(0)
		if legacyRows > 0 {
			now := time.Now().Unix()
			cutoff = now - now%3600
		}
		cols := "monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max, " + histCols("")
		stmts := []string{
			`ALTER TABLE stats_hourly RENAME TO stats_hourly_legacy`,
			fmt.Sprintf(`DELETE FROM stats_hourly_legacy WHERE bucket >= %d`, cutoff),
			fmt.Sprintf(`ALTER TABLE heartbeats SET (timescaledb.compress, timescaledb.compress_segmentby = 'monitor_id', timescaledb.compress_orderby = 'time DESC, id DESC')`),
			fmt.Sprintf(`SELECT %s.add_compression_policy('heartbeats', compress_after => CAST(%d AS BIGINT))`, ext, tsCompressBeats),
			`ALTER TABLE mesh_samples SET (timescaledb.compress, timescaledb.compress_segmentby = 'src_id, dst_id', timescaledb.compress_orderby = 'time DESC')`,
			fmt.Sprintf(`SELECT %s.add_compression_policy('mesh_samples', compress_after => CAST(%d AS BIGINT))`, ext, tsCompressMesh),
			fmt.Sprintf(`SELECT %s.add_retention_policy('mesh_samples', drop_after => CAST(%d AS BIGINT))`, ext, tsMeshRetainDays*86400),
			// Saatlik sürekli özet: writeBeat'in Go özetiyle aynı kurallar
			// (up/down yalnız onaylı sonuç, ping yalnız down olmayanda yazılır).
			fmt.Sprintf(`CREATE MATERIALIZED VIEW stats_hourly_ts
				WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
				SELECT monitor_id, %s.time_bucket(CAST(3600 AS BIGINT), time) AS bucket,
					count(*) FILTER (WHERE status = %d) AS up,
					count(*) FILTER (WHERE status = %d) AS down,
					CAST(COALESCE(sum(ping_ms), 0) AS BIGINT) AS ping_sum,
					count(ping_ms) AS ping_count,
					min(ping_ms) AS ping_min, max(ping_ms) AS ping_max,
					%s
				FROM heartbeats GROUP BY 1, 2 WITH NO DATA`, ext, StatusUp, StatusDown, strings.Join(hist, ",\n")),
			fmt.Sprintf(`SELECT %s.add_continuous_aggregate_policy('stats_hourly_ts', start_offset => CAST(%d AS BIGINT), end_offset => CAST(%d AS BIGINT), schedule_interval => INTERVAL '15 minutes')`, ext, tsCaggStart, tsCaggEnd),
			// UTC gün özeti (uzun pencere toplamları; yerel gün saatlikten, iş 620).
			fmt.Sprintf(`CREATE MATERIALIZED VIEW stats_daily_utc
				WITH (timescaledb.continuous, timescaledb.materialized_only = false) AS
				SELECT monitor_id, %s.time_bucket(CAST(86400 AS BIGINT), bucket) AS bucket,
					CAST(sum(up) AS BIGINT) AS up, CAST(sum(down) AS BIGINT) AS down,
					CAST(sum(ping_sum) AS BIGINT) AS ping_sum, CAST(sum(ping_count) AS BIGINT) AS ping_count,
					min(ping_min) AS ping_min, max(ping_max) AS ping_max,
					%s
				FROM stats_hourly_ts GROUP BY 1, 2 WITH NO DATA`, ext, strings.Join(sumHist, ", ")),
			fmt.Sprintf(`SELECT %s.add_continuous_aggregate_policy('stats_daily_utc', start_offset => CAST(%d AS BIGINT), end_offset => CAST(%d AS BIGINT), schedule_interval => INTERVAL '1 hour')`, ext, tsDailyStart, tsDailyEnd),
			// Okuma adı: silinen monitör ve sıfırlanan geçmiş görünmez.
			fmt.Sprintf(`CREATE VIEW stats_hourly AS
				SELECT %s FROM stats_hourly_ts c JOIN monitors m ON m.id = c.monitor_id
					WHERE c.bucket >= %d AND c.bucket >= m.stats_reset_at
				UNION ALL
				SELECT %s FROM stats_hourly_legacy WHERE bucket < %d`,
				histColsPrefixed("c.", cols), cutoff, cols, cutoff),
		}
		for _, q := range stmts {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("%w\n%s", err, q)
			}
		}
		s.opts.Log.Info("TimescaleDB: sıkıştırma ve sürekli özetler kuruldu", "eski_saatlik_satir", legacyRows, "cutoff", cutoff)
		return nil
	})
}

// histColsPrefixed virgüllü sütun listesinin her öğesine önek ekler.
func histColsPrefixed(prefix, cols string) string {
	parts := strings.Split(cols, ", ")
	for i, p := range parts {
		parts[i] = prefix + p
	}
	return strings.Join(parts, ", ")
}

// hourlyTable saatlik özetin yazılabilir tablosu (silme/sıfırlama).
func (s *Store) hourlyTable() string {
	if s.tsFull() {
		return "stats_hourly_legacy"
	}
	return "stats_hourly"
}

// tsDropChunks hypertable/sürekli özetin t'den tamamen eski parçalarını
// atar; atılan parça sayısını döner.
// Arka plan işleri (sıkıştırma, sürekli özet yenileme) aynı parçaları
// kilitleyebilir: kilitlenme (deadlock) ya da kilit beklemesinde kısa
// aralıkla yeniden denenir.
func (s *Store) tsDropChunks(ctx context.Context, rel string, t int64) (int64, error) {
	q := fmt.Sprintf("SELECT %s.drop_chunks('%s', older_than => CAST(? AS BIGINT))", s.tsExt, rel)
	var err error
	for try := range 5 {
		var n int64
		if n, err = s.tsDropOnce(ctx, q, t); err == nil {
			return n, nil
		}
		if !strings.Contains(err.Error(), "40P01") && !strings.Contains(err.Error(), "55P03") {
			return 0, err
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(time.Duration(try+1) * 500 * time.Millisecond):
		}
	}
	return 0, err
}

func (s *Store) tsDropOnce(ctx context.Context, q string, t int64) (int64, error) {
	rows, err := s.db.QueryContext(ctx, q, t)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var n int64
	for rows.Next() {
		n++
	}
	return n, rows.Err()
}
