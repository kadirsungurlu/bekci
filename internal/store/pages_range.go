package store

import "context"

// Herkese açık durum sayfasındaki çubukların kapsamı.
const (
	BarRangeRecent = "recent" // son kontroller (her çubuk bir kontrol) — varsayılan
	BarRange24h    = "24h"    // son 24 saat, saatlik
	BarRange90d    = "90d"    // son 90 gün, günlük
)

// ValidBarRange geçerli bir çubuk kapsamı mı?
func ValidBarRange(r string) bool {
	return r == BarRangeRecent || r == BarRange24h || r == BarRange90d
}

func barRangeOr(r string) string {
	if ValidBarRange(r) {
		return r
	}
	return BarRangeRecent
}

func init() {
	// 8: durum sayfası çubuk kapsamı. Ziyaretçi çoğunlukla anlık durumu merak
	// ettiğinden mevcut sayfalar da son kontrollere geçer.
	RegisterMigration(8, `ALTER TABLE status_pages ADD COLUMN bar_range TEXT NOT NULL DEFAULT 'recent';`)
}

// RecentBeats her monitörün en son limit kontrolünü eskiden yeniye döner.
func (s *Store) RecentBeats(ctx context.Context, ids []int64, limit int) (map[int64][]Beat, error) {
	out := make(map[int64][]Beat, len(ids))
	for _, id := range ids {
		rows, err := s.db.QueryContext(ctx,
			"SELECT time, status FROM heartbeats WHERE monitor_id = ? ORDER BY time DESC LIMIT ?", id, limit)
		if err != nil {
			return nil, err
		}
		var beats []Beat
		for rows.Next() {
			b := Beat{MonitorID: id, PingMs: -1}
			if err := rows.Scan(&b.Time, &b.Status); err != nil {
				rows.Close()
				return nil, err
			}
			beats = append(beats, b)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for i, j := 0, len(beats)-1; i < j; i, j = i+1, j-1 {
			beats[i], beats[j] = beats[j], beats[i]
		}
		out[id] = beats
	}
	return out, nil
}

// HourlyFor verilen monitörlerin since'den itibaren saatlik özetleri.
func (s *Store) HourlyFor(ctx context.Context, ids []int64, since int64) (map[int64][]Bucket, error) {
	out := map[int64][]Bucket{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+bucketCols+" FROM stats_hourly WHERE monitor_id IN ("+q+") AND bucket >= ? ORDER BY monitor_id, bucket",
		append(args, since)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		mid, b, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], b)
	}
	return out, rows.Err()
}
