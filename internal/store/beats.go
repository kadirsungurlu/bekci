package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Beat tek bir kontrol sonucudur. PingMs < 0 "ölçüm yok" demektir.
type Beat struct {
	MonitorID int64
	Time      int64
	Status    int
	PingMs    int64
	Message   string
}

// BeatUpdate kontrol sonucunu ve monitörün yeni durumunu tek işlemde yazar:
// ham kayıt + saatlik/günlük özet + monitör satırı. Özetler burada anında
// güncellendiği için liste ekranı ham tabloya hiç dokunmaz.
type BeatUpdate struct {
	Beat
	LastChangeAt int64 // 0 ise değişmez
}

func (s *Store) RecordBeat(ctx context.Context, u BeatUpdate) error {
	b := u.Beat
	// Uptime yalnızca onaylanmış sonuçlardan hesaplanır: bekleyen (tekrar
	// denenen) ve bakımdaki kontroller ne çalışıyor ne de kesinti sayılır.
	// Böylece anlık bir hata uptime'ı düşürmez, hiç çalışmamış ama tekrar
	// denenen bir monitör de yanlışlıkla %100 görünmez.
	up, down := 0, 0
	switch b.Status {
	case StatusUp:
		up = 1
	case StatusDown:
		down = 1
	}
	var ping any
	pingSum, pingCount := int64(0), 0
	if b.PingMs >= 0 && b.Status != StatusDown {
		ping = b.PingMs
		pingSum, pingCount = b.PingMs, 1
	}
	hour := b.Time - b.Time%3600
	day := s.dayStart(b.Time)

	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO heartbeats (monitor_id, time, status, ping_ms, message) VALUES (?, ?, ?, ?, ?)",
			b.MonitorID, b.Time, b.Status, ping, b.Message); err != nil {
			return err
		}
		for _, t := range []struct {
			table  string
			bucket int64
		}{{"stats_hourly", hour}, {"stats_daily", day}} {
			// ping_min/max: yeni değer NULL ise eskisi korunur. Sütunlar tablo adıyla
			// nitelenir: PostgreSQL'de "up" tek başına excluded.up ile belirsiz olur.
			if _, err := tx.ExecContext(ctx, strings.ReplaceAll(`
				INSERT INTO `+t.table+` (monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (monitor_id, bucket) DO UPDATE SET
					up = T.up + excluded.up,
					down = T.down + excluded.down,
					ping_sum = T.ping_sum + excluded.ping_sum,
					ping_count = T.ping_count + excluded.ping_count,
					ping_min = CASE WHEN excluded.ping_min IS NULL THEN T.ping_min
						WHEN T.ping_min IS NULL OR excluded.ping_min < T.ping_min THEN excluded.ping_min
						ELSE T.ping_min END,
					ping_max = CASE WHEN excluded.ping_max IS NULL THEN T.ping_max
						WHEN T.ping_max IS NULL OR excluded.ping_max > T.ping_max THEN excluded.ping_max
						ELSE T.ping_max END`, "T.", t.table+"."),
				b.MonitorID, t.bucket, up, down, pingSum, pingCount, ping, ping); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE monitors SET status = ?, last_check_at = ?, last_ping_ms = ?, last_message = ?,
				last_change_at = COALESCE(?, last_change_at)
			WHERE id = ?`,
			b.Status, b.Time, ping, b.Message, nullInt(u.LastChangeAt), b.MonitorID)
		return err
	})
}

// dayStart t'nin ait olduğu günün (yerel saat diliminde) başlangıcı.
func (s *Store) dayStart(t int64) int64 {
	y, m, d := time.Unix(t, 0).In(s.loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, s.loc).Unix()
}

// Bucket saatlik veya günlük özet satırıdır.
type Bucket struct {
	Time    int64 `json:"t"`
	Up      int64 `json:"up"`
	Down    int64 `json:"down"`
	PingAvg int64 `json:"ping"` // -1: ölçüm yok
	PingMin int64 `json:"ping_min"`
	PingMax int64 `json:"ping_max"`
}

func scanBucket(sc scanner) (int64, Bucket, error) {
	var (
		mid      int64
		b        Bucket
		sum      int64
		cnt      int64
		min, max sql.NullInt64
	)
	if err := sc.Scan(&mid, &b.Time, &b.Up, &b.Down, &sum, &cnt, &min, &max); err != nil {
		return 0, b, err
	}
	b.PingAvg = -1
	if cnt > 0 {
		b.PingAvg = sum / cnt
	}
	b.PingMin, b.PingMax = -1, -1
	if min.Valid {
		b.PingMin = min.Int64
	}
	if max.Valid {
		b.PingMax = max.Int64
	}
	return mid, b, nil
}

const bucketCols = "monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max"

// HourlyAll tüm monitörlerin since'den itibaren saatlik özetleri (liste çubukları için).
func (s *Store) HourlyAll(ctx context.Context, since int64) (map[int64][]Bucket, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+bucketCols+" FROM stats_hourly WHERE bucket >= ? ORDER BY monitor_id, bucket", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Bucket{}
	for rows.Next() {
		mid, b, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], b)
	}
	return out, rows.Err()
}

// Series bir monitörün since'den itibaren özet serisi; daily=false saatlik.
func (s *Store) Series(ctx context.Context, monitorID int64, since int64, daily bool) ([]Bucket, error) {
	table := "stats_hourly"
	if daily {
		table = "stats_daily"
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+bucketCols+" FROM "+table+" WHERE monitor_id = ? AND bucket >= ? ORDER BY bucket",
		monitorID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		_, b, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// hourCeil since'i bir sonraki saat başına yuvarlar: "son 24 saat" mevcut saat
// dahil 24 saatlik kova olur (liste çubuklarıyla aynı pencere).
func hourCeil(t int64) int64 {
	if r := t % 3600; r != 0 {
		return t - r + 3600
	}
	return t
}

// Uptime since'den itibaren çalışma oranı (0-100). Veri yoksa ok=false.
// Saatlik özetten hesaplanır; since saat başına (yukarı) yuvarlanır.
func (s *Store) Uptime(ctx context.Context, monitorID int64, since int64) (pct float64, ok bool, err error) {
	var up, down sql.NullInt64
	err = s.db.QueryRowContext(ctx,
		"SELECT CAST(SUM(up) AS BIGINT), CAST(SUM(down) AS BIGINT) FROM stats_hourly WHERE monitor_id = ? AND bucket >= ?",
		monitorID, hourCeil(since)).Scan(&up, &down)
	if err != nil || up.Int64+down.Int64 == 0 {
		return 0, false, err
	}
	return 100 * float64(up.Int64) / float64(up.Int64+down.Int64), true, nil
}

// AvgPing since'den itibaren ortalama yanıt süresi (ms), veri yoksa -1.
func (s *Store) AvgPing(ctx context.Context, monitorID int64, since int64) (int64, error) {
	var sum, cnt sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT CAST(SUM(ping_sum) AS BIGINT), CAST(SUM(ping_count) AS BIGINT) FROM stats_hourly WHERE monitor_id = ? AND bucket >= ?",
		monitorID, hourCeil(since)).Scan(&sum, &cnt)
	if err != nil || cnt.Int64 == 0 {
		return -1, err
	}
	return sum.Int64 / cnt.Int64, nil
}

// Beats ham kontrol kayıtları (detay grafiği için), eskiden yeniye.
func (s *Store) Beats(ctx context.Context, monitorID int64, since int64) ([]Beat, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT time, status, ping_ms, message FROM heartbeats WHERE monitor_id = ? AND time >= ? ORDER BY time",
		monitorID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Beat{}
	for rows.Next() {
		b := Beat{MonitorID: monitorID}
		var ping sql.NullInt64
		if err := rows.Scan(&b.Time, &b.Status, &ping, &b.Message); err != nil {
			return nil, err
		}
		b.PingMs = -1
		if ping.Valid {
			b.PingMs = ping.Int64
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CountBeats monitörün since'ten (dahil) itibaren verilen durumdaki kontrol
// kayıtlarının sayısı (yeniden başlatmada hatırlatma sayacını kurmak için).
func (s *Store) CountBeats(ctx context.Context, monitorID int64, status int, since int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM heartbeats WHERE monitor_id = ? AND status = ? AND time >= ?",
		monitorID, status, since).Scan(&n)
	return n, err
}

// Veri temizliği: bkz. cleanup.go (parçalı silme).
