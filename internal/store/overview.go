package store

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// Monitör listesinin geniş ekran sütunları için toplu sorgular: son kontrollerin
// yanıt süreleri (küçük çizgi grafik) ve 7/30 günlük çalışma oranı. Her ikisi
// de tüm monitörler için TEK sorgudur (monitör başına sorgu yok); monitör
// dizininden (heartbeats_monitor_time, stats_* birincil anahtarı) arama yapar,
// tabloyu taramaz.

// Son kontrol listesindeki özel değerler (RecentPings).
const (
	PingNone = -1 // ölçüm yok (bakım, bekleyen kontrol vb.)
	PingDown = -2 // başarısız kontrol (çalışmıyor)
)

// RecentPings monitörlerin son n kontrolünün yanıt süresi (ms), eskiden
// yeniye. Başarısız kontrol PingDown, ölçümsüz kontrol PingNone olur.
// ids boşsa tüm monitörler.
func (s *Store) RecentPings(ctx context.Context, ids []int64, n int) (map[int64][]int64, error) {
	where, args := "", []any{n}
	if len(ids) > 0 {
		q, a := inClause(ids)
		where = " WHERE m.id IN (" + q + ")"
		args = append(args, a...)
	}
	var query string
	if s.postgres {
		query = `SELECT m.id, h.status, h.ping_ms FROM monitors m
			CROSS JOIN LATERAL (SELECT time, status, ping_ms FROM heartbeats
				WHERE monitor_id = m.id ORDER BY time DESC LIMIT ?) h` + where + `
			ORDER BY m.id, h.time`
	} else {
		// SQLite'ta LATERAL yok: ilişkili alt sorgu her monitör için dizinden
		// son n kaydın kimliğini bulur, satırlar kimlikle (rowid) okunur.
		query = `SELECT h.monitor_id, h.status, h.ping_ms FROM monitors m
			JOIN heartbeats h ON h.id IN (SELECT x.id FROM heartbeats x
				WHERE x.monitor_id = m.id ORDER BY x.time DESC LIMIT ?)` + where + `
			ORDER BY h.monitor_id, h.time`
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var (
			id, status int64
			ping       sql.NullInt64
		)
		if err := rows.Scan(&id, &status, &ping); err != nil {
			return nil, err
		}
		v := int64(PingNone)
		switch {
		case status == StatusDown:
			v = PingDown
		case ping.Valid && ping.Int64 >= 0:
			v = ping.Int64
		}
		out[id] = append(out[id], v)
	}
	return out, rows.Err()
}

// UptimeWindows monitörlerin verilen başlangıçlardan (since) itibaren çalışma
// oranı (0-100); veri olmayan pencere nil. Store.Uptime ile aynı hesaptır
// (since saat başına yukarı yuvarlanır) ama tüm monitörler ve tüm pencereler
// tek sorguda. Okunan satır az olsun diye pencerenin tam günleri günlük
// özetten, ilk (yarım) günün saatleri saatlik özetten toplanır: günlük özet
// o günün saatlik özetlerinin toplamıdır, sonuç aynıdır. ids boşsa tüm monitörler.
func (s *Store) UptimeWindows(ctx context.Context, ids []int64, since ...int64) (map[int64][]*float64, error) {
	out := map[int64][]*float64{}
	if len(since) == 0 {
		return out, nil
	}
	filter, idArgs := "", []any(nil)
	if len(ids) > 0 {
		q, a := inClause(ids)
		filter, idArgs = " AND m.id IN ("+q+")", a
	}
	var (
		parts []string
		args  []any
	)
	for i, t := range since {
		t = hourCeil(t)
		w := strconv.Itoa(i)
		hourly := "SELECT h.monitor_id AS mid, " + w + " AS w, h.up AS up, h.down AS down FROM monitors m " +
			"JOIN stats_hourly h ON h.monitor_id = m.id AND h.bucket >= ?"
		// Pencerenin ilk tam günü (t gün başıysa kendisi).
		d0 := s.dayStart(t)
		if d0 != t {
			y, mo, d := time.Unix(d0, 0).In(s.loc).Date()
			d0 = time.Date(y, mo, d+1, 0, 0, 0, 0, s.loc).Unix()
		}
		if d0%3600 != 0 {
			// Gün başı saat başına denk gelmiyor (yarım saatlik dilim): yalnızca saatlik özet.
			parts = append(parts, hourly+filter)
			args = append(append(args, t), idArgs...)
			continue
		}
		parts = append(parts,
			hourly+" AND h.bucket < ?"+filter,
			"SELECT d.monitor_id, "+w+", d.up, d.down FROM monitors m "+
				"JOIN stats_daily d ON d.monitor_id = m.id AND d.bucket >= ?"+filter)
		args = append(append(args, t, d0), idArgs...)
		args = append(append(args, d0), idArgs...)
	}
	sel := "SELECT mid"
	for i := range since {
		w := strconv.Itoa(i)
		sel += ", CAST(SUM(CASE WHEN w = " + w + " THEN up ELSE 0 END) AS BIGINT)" +
			", CAST(SUM(CASE WHEN w = " + w + " THEN down ELSE 0 END) AS BIGINT)"
	}
	query := sel + " FROM (" + strings.Join(parts, " UNION ALL ") + ") x GROUP BY mid"
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	vals := make([]sql.NullInt64, 2*len(since))
	dest := make([]any, 1+len(vals))
	for rows.Next() {
		var id int64
		dest[0] = &id
		for i := range vals {
			dest[i+1] = &vals[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		res := make([]*float64, len(since))
		for i := range since {
			up, down := vals[2*i].Int64, vals[2*i+1].Int64
			if up+down > 0 {
				pct := 100 * float64(up) / float64(up+down)
				res[i] = &pct
			}
		}
		out[id] = res
	}
	return out, rows.Err()
}
