package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// readDump Store'un tüm özet okuma yollarının çıktısı (karşılaştırma için).
func readDump(t *testing.T, s *Store, ids []int64, now int64, daily bool) map[string]string {
	t.Helper()
	ctx := context.Background()
	out := map[string]string{}
	put := func(k string, v any, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		b, _ := json.Marshal(v)
		out[k] = string(b)
	}
	for _, id := range ids {
		for _, days := range []int64{1, 7, 30, 90, 400} {
			k := fmt.Sprintf("%d/%dg", id, days)
			since := now - days*86400
			v, err := s.Series(ctx, id, since, false)
			put("Series "+k, v, err)
			if daily {
				v, err = s.Series(ctx, id, since, true)
				put("SeriesDaily "+k, v, err)
			}
			u, ok, err := s.Uptime(ctx, id, since)
			put("Uptime "+k, []any{u, ok}, err)
			p, err := s.AvgPing(ctx, id, since)
			put("AvgPing "+k, p, err)
			p, err = s.PingPercentile(ctx, id, since, 0.95)
			put("P95 "+k, p, err)
		}
	}
	for _, days := range []int64{1, 30, 90} {
		h, err := s.HourlyFor(ctx, ids, now-days*86400)
		put(fmt.Sprintf("HourlyFor %d", days), h, err)
		if daily {
			d, err := s.DailyFor(ctx, ids, s.DayStarts(now, int(days))[0])
			put(fmt.Sprintf("DailyFor %d", days), d, err)
		}
		c, err := s.UptimeCounts(ctx, now-days*86400)
		put(fmt.Sprintf("UptimeCounts %d", days), c, err)
	}
	h, err := s.HourlyAll(ctx, now-now%3600-23*3600)
	put("HourlyAll", h, err)
	for _, only := range [][]int64{nil, ids[:1]} {
		w, err := s.UptimeWindows(ctx, only, now-7*86400, now-30*86400, now-90*86400, now-365*86400)
		put(fmt.Sprintf("UptimeWindows %v", only), w, err)
	}
	return out
}

func compareDumps(t *testing.T, name string, want, got map[string]string) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("%s: anahtar sayısı %d != %d", name, len(got), len(want))
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s: %s farklı\n Go  : %.600s\n yeni: %.600s", name, k, w, got[k])
		}
	}
}

// TestTimescaleReadParity sürekli özetten okuma sonuçları eski Go özetleriyle
// (aynı sonuç dizisi, düz PG yolu) birebir aynı olmalı: baştan Timescale
// kurulum, dönüşmüş kurulum, özetler yenilenmeden ve yenilendikten sonra,
// sıfırlama sonrası; yaz saatli ve yarım saatlik dilimlerde.
func TestTimescaleReadParity(t *testing.T) {
	if !IsPostgresDSN(testTarget(t)) {
		t.Skip("PostgreSQL gerekli (UPTIME_TEST_PG)")
	}
	for _, zone := range []string{"Europe/Istanbul", "America/New_York", "Asia/Kolkata"} {
		t.Run(zone, func(t *testing.T) {
			ctx := context.Background()
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Fatal(err)
			}
			_, off := time.Now().In(loc).Zone()
			daily := off%3600 == 0 // yarım saatlik dilimde gün çubukları saate yuvarlanır (belgeli)
			plain, err := OpenWith(testTarget(t), loc, Options{TimescaleOff: true})
			if err != nil {
				t.Fatal(err)
			}
			defer plain.Close()
			if ext, _ := plain.tsExtension(ctx); ext == "" {
				t.Skip("TimescaleDB eklentisi yok")
			}
			fresh, err := OpenWith(testTarget(t), loc, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer fresh.Close()
			if !fresh.tsFull() {
				t.Skipf("kip %s", fresh.DBMode())
			}
			convTarget := testTarget(t)
			conv, err := OpenWith(convTarget, loc, Options{TimescaleOff: true})
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, n := range []string{"a", "b", "c"} {
				var id int64
				for _, s := range []*Store{plain, fresh, conv} {
					id = newMonitor(t, s, n).ID
				}
				ids = append(ids, id)
			}
			now := time.Now().Unix()
			hour := now - now%3600
			write := func(st []*Store, b BeatUpdate) {
				t.Helper()
				for _, s := range st {
					if err := s.RecordBeat(ctx, b); err != nil {
						t.Fatal(err)
					}
				}
			}
			// 100 ve 60 gün önce birer gün + son 40 gün saatte bir, karışık sonuç.
			r := rand.New(rand.NewSource(620))
			var old, cur []BeatUpdate
			add := func(ts int64, id int64) {
				st := StatusUp
				switch r.Intn(12) {
				case 0:
					st = StatusDown
				case 1:
					st = StatusPending
				}
				b := BeatUpdate{Beat: Beat{MonitorID: id, Time: ts, Status: st, PingMs: int64(r.Intn(1 << uint(r.Intn(13))))}}
				if ts < hour {
					old = append(old, b)
				} else {
					cur = append(cur, b)
				}
			}
			for _, id := range ids {
				for _, ago := range []int64{100, 60} {
					for h := range int64(24) {
						add(now-ago*86400+h*3600+r.Int63n(3000), id)
					}
				}
				for i := int64(40 * 24); i >= 1; i-- {
					add(now-i*3600+r.Int63n(3000), id)
				}
				add(now-r.Int63n(now-hour+1), id)
			}
			for _, b := range old {
				write([]*Store{plain, fresh, conv}, b)
			}
			conv.Close()
			if conv, err = OpenWith(convTarget, loc, Options{}); err != nil {
				t.Fatal(err)
			}
			defer conv.Close()
			if !conv.tsFull() || conv.tsDailyCutoff == 0 || conv.tsHourlyCutoff == 0 {
				t.Fatalf("dönüşüm: kip %s, sınırlar %d %d", conv.DBMode(), conv.tsDailyCutoff, conv.tsHourlyCutoff)
			}
			for _, b := range cur {
				write([]*Store{plain, fresh, conv}, b)
			}
			want := readDump(t, plain, ids, now, daily)
			compareDumps(t, "taze, yenilenmemiş", want, readDump(t, fresh, ids, now, daily))
			compareDumps(t, "dönüşmüş, yenilenmemiş", want, readDump(t, conv, ids, now, daily))

			refresh := func(s *Store) {
				t.Helper()
				for _, c := range []string{"stats_hourly_ts", "stats_daily_utc"} {
					if _, err := s.db.ExecContext(ctx, "CALL "+s.tsExt+".refresh_continuous_aggregate('"+c+"', NULL, CAST(? AS BIGINT))", hour-2*86400); err != nil {
						t.Fatal(err)
					}
				}
			}
			refresh(fresh)
			refresh(conv)
			compareDumps(t, "taze, yenilenmiş", want, readDump(t, fresh, ids, now, daily))
			compareDumps(t, "dönüşmüş, yenilenmiş", want, readDump(t, conv, ids, now, daily))

			// Sıfırlama: geçmiş üç yolda da kaybolur, sonraki sonuç sayılır.
			all := []*Store{plain, fresh, conv}
			for _, s := range all {
				if err := s.ResetMonitorStats(ctx, ids[1]); err != nil {
					t.Fatal(err)
				}
			}
			write(all, BeatUpdate{Beat: Beat{MonitorID: ids[1], Time: now, Status: StatusUp, PingMs: 42}})
			want = readDump(t, plain, ids, now, daily)
			compareDumps(t, "taze, sıfırlanmış", want, readDump(t, fresh, ids, now, daily))
			compareDumps(t, "dönüşmüş, sıfırlanmış", want, readDump(t, conv, ids, now, daily))
		})
	}
}

func TestHistPercentile(t *testing.T) {
	c := make([]int64, len(PingBounds)+1)
	if histPercentile(c, 0.95) != -1 {
		t.Fatal("boş histogram -1 olmalı")
	}
	c[1], c[3] = 90, 10 // 90 tane 10-20 ms, 10 tane 30-50 ms
	if p := histPercentile(c, 0.95); p != 40 {
		t.Fatalf("p95 = %d, 40 olmalı", p)
	}
	if p := histPercentile(c, 0.5); p != 15 {
		t.Fatalf("p50 = %d", p)
	}
}

func TestTsChunkPlan(t *testing.T) {
	for _, c := range []struct {
		rows            float64
		chunk, compress int64
	}{
		{1_440_000, 86400, 7 * 86400},
		{14_400_000, 86400, 2 * 86400},
		{144_000_000, 8 * 3600, 86400},
		{1_440_000_000, 3600, 86400},
	} {
		ch, co := tsChunkPlan(c.rows)
		if ch != c.chunk || co != c.compress {
			t.Errorf("%v: %d %d", c.rows, ch, co)
		}
	}
}

func TestTsCached(t *testing.T) {
	calls := 0
	f := func() (int, error) { calls++; return calls, nil }
	s := &Store{mode: DBModeTimescale, opts: Options{ReadCache: time.Hour}}
	for range 3 {
		if v, _ := tsCached(s, "a", f); v != 1 {
			t.Fatalf("önbellek kullanılmadı: %d", v)
		}
	}
	if v, _ := tsCached(s, "b", f); v != 2 {
		t.Fatalf("anahtar ayrı olmalı: %d", v)
	}
	s.opts.ReadCache = 0
	if v, _ := tsCached(s, "a", f); v != 3 {
		t.Fatalf("0 süre önbelleği kapatmalı: %d", v)
	}
	s = &Store{mode: DBModePostgres, opts: Options{ReadCache: time.Hour}}
	if a, _ := tsCached(s, "a", f); a != 4 {
		t.Fatalf("Timescale dışında önbellek olmamalı: %d", a)
	}
}

// TestTimescaleWindowFirstPartNamesColumns pencere gün başına denk gelince
// UNION'ın ilk parçası gün özeti olur; dış sorgu mid'e bu parçadaki adla bakar.
func TestTimescaleWindowFirstPartNamesColumns(t *testing.T) {
	day := time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC).Unix()
	s := &Store{loc: time.UTC, tsDailyCutoff: day + 5*86400, tsHourlyCutoff: day + 5*86400}
	parts, _ := s.tsWindowParts(day, "0", "", nil)
	if len(parts) == 0 || !strings.Contains(parts[0], "stats_daily_legacy") {
		t.Fatalf("ilk parça gün özeti olmalı: %q", parts)
	}
	for _, p := range parts {
		if !strings.Contains(p, " AS mid,") {
			t.Fatalf("mid adı yok: %s", p)
		}
	}
}
