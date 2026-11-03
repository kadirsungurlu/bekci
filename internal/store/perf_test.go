package store

// Ölçüm aracı (iş 620, docs/PERFORMANS-620.md). Yalnız elle çalışır:
//
//	BEKCI_PERF_DSN=... BEKCI_PERF_MODE=yeni|go|ham BEKCI_PERF_CGROUP=<cpu.stat yolu> \
//	  go test -count=1 -run 'TestPerf(Queries|Write)' -v ./internal/store/
//
// Sorgu başına: EXPLAIN ANALYZE'dan süre (medyan), blok (hit+read), okunan
// satır; art arda çalıştırmadan veritabanı konteynerinin CPU süresi.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func perfOpen(t *testing.T) *Store {
	dsn := os.Getenv("BEKCI_PERF_DSN")
	if dsn == "" {
		t.Skip("BEKCI_PERF_DSN yok")
	}
	s, err := OpenWith(dsn, time.UTC, Options{TimescaleOff: os.Getenv("BEKCI_PERF_MODE") == "go", BeatBatch: time.Duration(perfEnvInt("BEKCI_PERF_BATCH", 0)) * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func perfEnvInt(k string, d int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return d
}

// cgroupCPU konteynerin toplam CPU süresi (mikrosaniye).
func cgroupCPU(t *testing.T) int64 {
	b, err := os.ReadFile(os.Getenv("BEKCI_PERF_CGROUP"))
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(l, "usage_usec "); ok {
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
	}
	return 0
}

// planWalk planın okuduğu satırları toplar (yaprak düğümler, süzgeçle atılan dahil).
func planWalk(n map[string]any) (rows float64) {
	subs, _ := n["Plans"].([]any)
	loops, _ := n["Actual Loops"].(float64)
	if len(subs) == 0 {
		r, _ := n["Actual Rows"].(float64)
		f, _ := n["Rows Removed by Filter"].(float64)
		return (r + f) * loops
	}
	for _, p := range subs {
		rows += planWalk(p.(map[string]any))
	}
	return rows
}

type perfQ struct {
	name string
	sql  string
	args []any
}

func perfQueries(s *Store, n int64, mode string) []perfQ {
	now := time.Now().Unix()
	day := func(d int64) int64 { return now - d*86400 }
	mid := n/2 + 1
	var page []int64
	for i := int64(0); i < 50 && i < n; i++ {
		page = append(page, 1+i*n/50)
	}
	in, inArgs := inClause(page)
	all := append([]any{}, inArgs...)
	if mode == "ham" {
		hb := "FROM heartbeats WHERE monitor_id = ? AND time >= ?"
		grp := "SELECT monitor_id, time - time % B AS b, count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status = 0), sum(ping_ms), count(ping_ms), min(ping_ms), max(ping_ms) FROM heartbeats WHERE "
		return []perfQ{
			{"grafik 7g saatlik", strings.ReplaceAll(grp, "B", "3600") + "monitor_id = ? AND time >= ? GROUP BY 1, 2 ORDER BY 2", []any{mid, day(7)}},
			{"grafik 30g saatlik", strings.ReplaceAll(grp, "B", "3600") + "monitor_id = ? AND time >= ? GROUP BY 1, 2 ORDER BY 2", []any{mid, day(30)}},
			{"grafik 90g gunluk", strings.ReplaceAll(grp, "B", "86400") + "monitor_id = ? AND time >= ? GROUP BY 1, 2 ORDER BY 2", []any{mid, day(90)}},
			{"uptime 30g tek", "SELECT count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status = 0) " + hb, []any{mid, day(30)}},
			{"uptime 90g tek", "SELECT count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status = 0) " + hb, []any{mid, day(90)}},
			{"durum sayfasi 50 monitor 90g", strings.ReplaceAll(grp, "B", "86400") + "monitor_id IN (" + in + ") AND time >= ? GROUP BY 1, 2", append(all, day(90))},
			{"liste uptime tum 30g", "SELECT monitor_id, count(*) FILTER (WHERE status = 1), count(*) FILTER (WHERE status = 0) FROM heartbeats WHERE time >= ? GROUP BY 1", []any{day(30)}},
		}
	}
	series := "SELECT " + bucketCols + " FROM stats_hourly WHERE monitor_id = ? AND bucket >= ? ORDER BY bucket"
	upQ := "SELECT CAST(SUM(up) AS BIGINT), CAST(SUM(down) AS BIGINT) FROM stats_hourly WHERE monitor_id = ? AND bucket >= ?"
	dq, da := s.dailySQL("monitor_id = ?", []any{mid}, day(90))
	pq, pa := s.dailySQL("monitor_id IN ("+in+")", inArgs, s.dayStart(day(90)))
	wq1, wa1 := s.uptimeWindowsSQL([]int64{mid}, []int64{day(30)})
	wq2, wa2 := s.uptimeWindowsSQL([]int64{mid}, []int64{day(90)})
	wq3, wa3 := s.uptimeWindowsSQL(nil, []int64{day(7), day(30)})
	hq := "SELECT " + bucketCols + " FROM stats_hourly WHERE monitor_id IN (" + in + ") AND bucket >= ? ORDER BY monitor_id, bucket"
	return []perfQ{
		{"grafik 7g saatlik", series, []any{mid, day(7)}},
		{"grafik 30g saatlik", series, []any{mid, day(30)}},
		{"grafik 90g gunluk", dq, da},
		{"uptime 30g tek", wq1, wa1},
		{"uptime 90g tek", wq2, wa2},
		{"uptime 30g Uptime()", upQ, []any{mid, hourCeil(day(30))}},
		{"durum sayfasi 50 monitor 90g", pq, pa},
		{"durum sayfasi 50 monitor 24s", hq, append(all, now-now%3600-23*3600)},
		{"liste uptime tum 30g", wq3, wa3},
		{"metrikler tum 30g (UptimeCounts)", "SELECT monitor_id, SUM(up), SUM(down) FROM stats_hourly WHERE bucket >= ? GROUP BY monitor_id", []any{hourCeil(day(30))}},
		{"liste 24s tum (HourlyAll)", "SELECT " + bucketCols + " FROM stats_hourly WHERE bucket >= ? ORDER BY monitor_id, bucket", []any{now - now%3600 - 23*3600}},
	}
}

func TestPerfQueries(t *testing.T) {
	s := perfOpen(t)
	ctx := context.Background()
	mode := os.Getenv("BEKCI_PERF_MODE")
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors").Scan(&n); err != nil {
		t.Fatal(err)
	}
	reps := perfEnvInt("BEKCI_PERF_REPS", 7)
	enc := json.NewEncoder(os.Stdout)
	for _, q := range perfQueries(s, n, mode) {
		var times []float64
		var buf, rows float64
		for range reps {
			var js string
			if err := s.db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+q.sql, q.args...).Scan(&js); err != nil {
				t.Fatalf("%s: %v", q.name, err)
			}
			var pl []map[string]any
			if err := json.Unmarshal([]byte(js), &pl); err != nil {
				t.Fatal(err)
			}
			times = append(times, pl[0]["Execution Time"].(float64)+pl[0]["Planning Time"].(float64))
			p := pl[0]["Plan"].(map[string]any)
			h, _ := p["Shared Hit Blocks"].(float64)
			r, _ := p["Shared Read Blocks"].(float64)
			buf, rows = h+r, planWalk(p)
		}
		sort.Float64s(times)
		c0, t0 := cgroupCPU(t), time.Now()
		for range reps {
			r, err := s.db.QueryContext(ctx, q.sql, q.args...)
			if err != nil {
				t.Fatal(err)
			}
			cnt := 0
			for r.Next() {
				cnt++
			}
			r.Close()
		}
		cpu := float64(cgroupCPU(t)-c0) / 1000 / float64(reps)
		wall := float64(time.Since(t0).Microseconds()) / 1000 / float64(reps)
		enc.Encode(map[string]any{"mod": mode, "sorgu": q.name, "ms": times[len(times)/2], "blok": buf, "satir": rows, "cpu_ms": cpu, "gercek_ms": wall})
	}
}

// TestPerfWrite gerçek yazma yolu (RecordBeat, grup commit): BEKCI_PERF_RATE
// sonuç/sn ile açık döngü (0 = kapalı döngü, azami hız), 30 sn.
func TestPerfWrite(t *testing.T) {
	s := perfOpen(t)
	ctx := context.Background()
	var n int64
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitors").Scan(&n); err != nil {
		t.Fatal(err)
	}
	rate := perfEnvInt("BEKCI_PERF_RATE", 0)
	dur := time.Duration(perfEnvInt("BEKCI_PERF_SEC", 30)) * time.Second
	workers := max(rate, 3000) // her RecordBeat grup commit'i bekler
	if rate > 0 {
		workers = rate
	}
	var done, late atomic.Int64
	var wg sync.WaitGroup
	end := time.Now().Add(dur)
	c0 := cgroupCPU(t)
	now := time.Now().Unix()
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var tick *time.Ticker
			if rate > 0 {
				tick = time.NewTicker(time.Second)
				defer tick.Stop()
			}
			i := int64(w)
			time.Sleep(time.Duration(w) * time.Second / time.Duration(workers))
			for time.Now().Before(end) {
				if tick != nil {
					<-tick.C
				}
				i += int64(workers)
				id := 1 + i%n
				t0 := time.Now()
				err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: id, Time: now + i%600, Status: StatusUp, PingMs: 20 + i%200, Message: "200 - OK", Location: fmt.Sprintf("loc-%d", i%5)}})
				if err != nil {
					t.Error(err)
					return
				}
				if time.Since(t0) > 2*time.Second {
					late.Add(1)
				}
				done.Add(1)
			}
		}()
	}
	wg.Wait()
	cpu := float64(cgroupCPU(t)-c0) / 1e6
	sec := dur.Seconds()
	json.NewEncoder(os.Stdout).Encode(map[string]any{"mod": os.Getenv("BEKCI_PERF_MODE"), "yazma_hedef": rate, "sonuc_sn": float64(done.Load()) / sec, "cpu_cekirdek": cpu / sec, "cpu_ms_1000sonuc": 1e6 * cpu / float64(done.Load()), "gec_2sn": late.Load()})
}

