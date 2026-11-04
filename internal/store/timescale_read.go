package store

// TimescaleDB kipinde okuma yolları (iş 620, tasarım §3.3, §3.4).
//
//   - Yerel gün çubukları saatlik özetten yerel güne gruplanır: tamsayı zamanlı
//     hiyerarşik özet ofset/saat dilimi kabul etmez. Gün sınırları Go'da
//     hesaplanır (dayStart ile aynı, yaz saati dahil) ve sorguya dizi olarak
//     verilir. Yarım saatlik dilimlerde saat, başladığı güne yazılır.
//   - Uzun pencere toplamları: tam UTC günleri stats_daily_utc'den, kenarlar
//     saatlikten; dönüşüm öncesi günler stats_daily_legacy'den.
//   - p95: saatlik özetin histogram sütunlarından (imajda toolkit yok).

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// tsConvertDaily dönüşüm sınırlarını okur; stats_daily tablosu henüz
// devredilmediyse devreder (idempotent).
func (s *Store) tsConvertDaily(ctx context.Context) error {
	var legacy sql.NullString
	if err := s.db.QueryRowContext(ctx, "SELECT CAST(to_regclass('stats_daily_legacy') AS TEXT)").Scan(&legacy); err != nil {
		return err
	}
	if !legacy.Valid {
		err := s.tx(ctx, func(tx *Tx) error {
			var rows int64
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_daily").Scan(&rows); err != nil {
				return err
			}
			cutoff := int64(0)
			if rows > 0 {
				cutoff = s.dayStart(time.Now().Unix())
			}
			var hourly int64
			if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(bucket) + 3600, 0) FROM stats_hourly_legacy").Scan(&hourly); err != nil {
				return err
			}
			for _, q := range []string{
				`ALTER TABLE stats_daily RENAME TO stats_daily_legacy`,
				fmt.Sprintf(`DELETE FROM stats_daily_legacy WHERE bucket >= %d`, cutoff),
				`CREATE TABLE timescale_meta (key TEXT PRIMARY KEY, value BIGINT NOT NULL)`,
				fmt.Sprintf(`INSERT INTO timescale_meta VALUES ('daily_cutoff', %d), ('hourly_cutoff', %d)`, cutoff, hourly),
			} {
				if _, err := tx.ExecContext(ctx, q); err != nil {
					return fmt.Errorf("%w\n%s", err, q)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	rows, err := s.db.QueryContext(ctx, "SELECT key, value FROM timescale_meta")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var v int64
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		switch k {
		case "daily_cutoff":
			s.tsDailyCutoff = v
		case "hourly_cutoff":
			s.tsHourlyCutoff = v
		}
	}
	return rows.Err()
}

// nextDayStart t gün başıysa t, değilse sonraki yerel gün başı.
func (s *Store) nextDayStart(t int64) int64 {
	d := s.dayStart(t)
	if d == t {
		return t
	}
	y, m, day := time.Unix(d, 0).In(s.loc).Date()
	return time.Date(y, m, day+1, 0, 0, 0, 0, s.loc).Unix()
}

// dayBounds lo gününden en az hi'ye kadar yerel gün başları (hi'yi içeren
// günün ertesi dahil: gelecekteki satır dizinin dışına taşmaz).
func (s *Store) dayBounds(lo, hi int64) string {
	var b strings.Builder
	y, m, d := time.Unix(lo, 0).In(s.loc).Date()
	for i := 0; ; i++ {
		t := time.Date(y, m, d+i, 0, 0, 0, 0, s.loc).Unix()
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(t, 10))
		if t > hi {
			break
		}
	}
	return b.String()
}

// dailySQL "monitor_id cond" süzgeciyle since'den itibaren yerel gün satırları
// (bucketCols sırasıyla, monitör + gün sıralı). args süzgecin parametreleri.
func (s *Store) dailySQL(cond string, args []any, since int64) (string, []any) {
	if !s.tsFull() {
		return "SELECT " + bucketCols + " FROM stats_daily WHERE " + cond + " AND bucket >= ? ORDER BY monitor_id, bucket",
			append(append([]any{}, args...), since)
	}
	lo := s.nextDayStart(since)
	if lo < s.tsDailyCutoff {
		lo = s.tsDailyCutoff
	}
	bounds := s.dayBounds(lo, time.Now().Unix()+2*86400)
	q := `SELECT ` + bucketCols + ` FROM (
		SELECT monitor_id, (ARRAY[` + bounds + `])[width_bucket(bucket, CAST(ARRAY[` + bounds + `] AS BIGINT[]))] AS bucket,
			CAST(SUM(up) AS BIGINT) AS up, CAST(SUM(down) AS BIGINT) AS down,
			CAST(SUM(ping_sum) AS BIGINT) AS ping_sum, CAST(SUM(ping_count) AS BIGINT) AS ping_count,
			MIN(ping_min) AS ping_min, MAX(ping_max) AS ping_max
		FROM stats_hourly WHERE ` + cond + ` AND bucket >= ? GROUP BY 1, 2`
	out := append(append([]any{}, args...), lo)
	if s.tsDailyCutoff > 0 {
		q += ` UNION ALL SELECT ` + bucketCols + ` FROM stats_daily_legacy WHERE ` + cond + ` AND bucket >= ? AND bucket < ?`
		out = append(append(out, args...), since, s.tsDailyCutoff)
	}
	return q + `) x ORDER BY monitor_id, bucket`, out
}

// tsWindowParts UptimeWindows'un t'den başlayan penceresi için alt sorgular:
// her biri (mid, w, up, down) döner. Zaman ekseni:
//
//	[t, gün) saatlik | [gün, daily_cutoff) legacy | [.., u1) saatlik |
//	[u1, de) UTC gün özeti | [de, ...) saatlik
//
// Sıfırlanmış monitörde sıfırlama gününün kalanı saatlikten okunur (gün
// özetinde satır silinemez, stats_hourly görünümü sıfırlama öncesini gizler).
func (s *Store) tsWindowParts(t int64, w, filter string, idArgs []any) ([]string, []any) {
	hourly := func(extra string) string {
		return "SELECT h.monitor_id AS mid, " + w + " AS w, h.up AS up, h.down AS down FROM monitors m " +
			"JOIN stats_hourly h ON h.monitor_id = m.id AND h.bucket >= ?" + extra + filter
	}
	const resetDay = "((m.stats_reset_at + 86399) / 86400 * 86400)"
	var (
		parts []string
		args  []any
	)
	add := func(q string, a ...any) {
		parts = append(parts, q)
		args = append(append(args, a...), idArgs...)
	}
	lo := t
	if t < s.tsDailyCutoff && s.tsDailyCutoff%3600 == 0 {
		d0 := s.nextDayStart(t)
		if d0 > s.tsDailyCutoff {
			d0 = s.tsDailyCutoff
		}
		if d0%3600 == 0 {
			if t < d0 {
				add(hourly(" AND h.bucket < ?"), t, d0)
			}
			add("SELECT d.monitor_id AS mid, "+w+" AS w, d.up AS up, d.down AS down FROM monitors m "+
				"JOIN stats_daily_legacy d ON d.monitor_id = m.id AND d.bucket >= ? AND d.bucket < ?"+filter, d0, s.tsDailyCutoff)
			lo = s.tsDailyCutoff
		}
	}
	u1 := max(lo, s.tsHourlyCutoff, s.tsDailyCutoff)
	if r := u1 % 86400; r != 0 {
		u1 += 86400 - r
	}
	now := time.Now().Unix()
	de := now - now%86400 - 86400 // son iki gün saatlikten: gün özeti gecikmeli yenilenir
	if u1 >= de {
		add(hourly(""), lo)
		return parts, args
	}
	add(hourly(" AND h.bucket < ?"), lo, u1)
	add("SELECT d.monitor_id AS mid, "+w+" AS w, d.up AS up, d.down AS down FROM monitors m "+
		"JOIN stats_daily_utc d ON d.monitor_id = m.id AND d.bucket >= ? AND d.bucket < ? AND d.bucket >= "+resetDay+filter, u1, de)
	add(hourly(" AND h.bucket < ?"+" AND h.bucket < "+resetDay+" AND m.stats_reset_at > ?"), u1, de, u1)
	add(hourly(""), de)
	return parts, args
}

// PingPercentile since'den itibaren gecikme yüzdeliği (q: 0-1), saatlik
// özetin histogramından: birikimli sayacın q'yu geçtiği kovada doğrusal ara
// değer. Ölçüm yoksa -1. Hata payı kova genişliği kadardır.
func (s *Store) PingPercentile(ctx context.Context, monitorID, since int64, q float64) (int64, error) {
	n := len(PingBounds) + 1
	cols := make([]string, n)
	for k := range cols {
		cols[k] = fmt.Sprintf("COALESCE(CAST(SUM(h%02d) AS BIGINT), 0)", k)
	}
	counts := make([]int64, n)
	dest := make([]any, n)
	for k := range counts {
		dest[k] = &counts[k]
	}
	err := s.db.QueryRowContext(ctx, "SELECT "+strings.Join(cols, ", ")+
		" FROM stats_hourly WHERE monitor_id = ? AND bucket >= ?", monitorID, hourCeil(since)).Scan(dest...)
	if err != nil {
		return -1, err
	}
	return histPercentile(counts, q), nil
}

// histPercentile kova sayaçlarından yüzdelik (bkz. PingPercentile).
func histPercentile(counts []int64, q float64) int64 {
	var total int64
	for _, c := range counts {
		total += c
	}
	if total == 0 {
		return -1
	}
	rank := q * float64(total)
	var cum int64
	for k, c := range counts {
		if c == 0 || float64(cum+c) < rank {
			cum += c
			continue
		}
		lo, hi := int64(0), PingBounds[len(PingBounds)-1]
		if k > 0 {
			lo = PingBounds[k-1]
		}
		if k < len(PingBounds) {
			hi = PingBounds[k]
		}
		return lo + int64(float64(hi-lo)*(rank-float64(cum))/float64(c))
	}
	return PingBounds[len(PingBounds)-1]
}

// Parça aralığı ve sıkıştırma eşiği kuralı (ölçüm: docs/PERFORMANS-620.md).
// Tek parça sıkıştırılana kadar bellekte çalışır: yaklaşık tsChunkRows satırı
// geçmesin. Büyük kurulumda sıkıştırma da erken başlar (ham disk sınırı).
const tsChunkRows = 50_000_000

// tsChunkPlan günlük satır tahmininden parça aralığı ve sıkıştırma eşiği (sn).
func tsChunkPlan(rowsPerDay float64) (chunk, compress int64) {
	chunk = 86400
	for _, h := range []int64{12, 8, 6, 4, 2, 1} {
		if rowsPerDay*float64(chunk)/86400 <= tsChunkRows {
			break
		}
		chunk = h * 3600
	}
	switch {
	case rowsPerDay >= 50_000_000:
		compress = 86400
	case rowsPerDay >= 5_000_000:
		compress = 2 * 86400
	default:
		compress = tsCompressBeats
	}
	return chunk, compress
}

// tsTuneChunks açılışta monitör sayısına göre parça aralığını (yalnız yeni
// parçalar) ve sıkıştırma eşiğini ayarlar.
func (s *Store) tsTuneChunks(ctx context.Context) error {
	var rows sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `SELECT SUM(86400.0 / GREATEST(m.interval_sec, 1) * GREATEST(COALESCE(l.n, 0), 1))
		FROM monitors m LEFT JOIN (SELECT monitor_id, COUNT(*) AS n FROM monitor_locations GROUP BY monitor_id) l
			ON l.monitor_id = m.id WHERE m.active = 1`).Scan(&rows); err != nil {
		return err
	}
	chunk, compress := tsChunkPlan(rows.Float64)
	if _, err := s.db.ExecContext(ctx, fmt.Sprintf("SELECT %s.set_chunk_time_interval('heartbeats', CAST(%d AS BIGINT))", s.tsExt, chunk)); err != nil {
		return err
	}
	var cur sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT CAST(config->>'compress_after' AS BIGINT) FROM timescaledb_information.jobs
		WHERE proc_name = 'policy_compression' AND hypertable_name = 'heartbeats'`).Scan(&cur); err != nil && err != sql.ErrNoRows {
		return err
	}
	if cur.Int64 == compress {
		return nil
	}
	for _, q := range []string{
		fmt.Sprintf("SELECT %s.remove_compression_policy('heartbeats', if_exists => true)", s.tsExt),
		fmt.Sprintf("SELECT %s.add_compression_policy('heartbeats', compress_after => CAST(%d AS BIGINT))", s.tsExt, compress),
	} {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	s.opts.Log.Info("TimescaleDB: parça planı", "tahmini_satir_gun", int64(rows.Float64), "parca_sn", chunk, "sikistirma_sn", compress)
	return nil
}

// tsCached tüm monitörleri tarayan okumaları (liste, özet, metrikler) kısa süre
// paylaştırır: bu sorgular kapanmamış saati ham veriden toplar ve maliyeti
// monitör sayısıyla büyür (ölçüm: docs/PERFORMANS-620.md). Süre dolunca tek
// istek yeniler, ötekiler onu bekler. Süre Options.ReadCache; 0 ya da
// Timescale dışı kipte önbellek yoktur. Dönen değer paylaşılır, değiştirilmez.
func tsCached[T any](s *Store, key string, f func() (T, error)) (T, error) {
	if s.opts.ReadCache <= 0 || !s.tsFull() {
		return f()
	}
	s.tsCacheMu.Lock()
	defer s.tsCacheMu.Unlock()
	now := time.Now()
	if e, ok := s.tsCache[key]; ok && now.Before(e.until) {
		return e.val.(T), nil
	}
	v, err := f()
	if err != nil {
		return v, err
	}
	if s.tsCache == nil {
		s.tsCache = map[string]tsCacheEntry{}
	}
	for k, e := range s.tsCache { // süresi geçen anahtar birikmesin (since saatle değişir)
		if now.After(e.until) {
			delete(s.tsCache, k)
		}
	}
	s.tsCache[key] = tsCacheEntry{val: v, until: time.Now().Add(s.opts.ReadCache)}
	return v, nil
}

type tsCacheEntry struct {
	val   any
	until time.Time
}
