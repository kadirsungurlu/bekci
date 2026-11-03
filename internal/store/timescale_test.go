package store

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// hourlyDump saatlik özetin tüm satırları (karşılaştırma için metin).
func hourlyDump(t *testing.T, s *Store) string {
	t.Helper()
	rows, err := s.db.QueryContext(context.Background(), "SELECT monitor_id, bucket, up, down, ping_sum, ping_count, "+
		"COALESCE(ping_min, -1), COALESCE(ping_max, -1), "+histCols("")+" FROM stats_hourly ORDER BY monitor_id, bucket")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var b strings.Builder
	n := 10 + len(PingBounds)
	for rows.Next() {
		v := make([]int64, n-1)
		ptr := make([]any, len(v))
		for i := range v {
			ptr[i] = &v[i]
		}
		if err := rows.Scan(ptr...); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintln(&b, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// TestTimescaleConvertExisting var olan veriyle (düz PG kurulumu) TimescaleDB
// dönüşümü: hypertable, sürekli özet, ham saklamanın ötesindeki geçmişin
// korunması, okuma sonuçlarının aynı kalması, sonraki yazım ve sıfırlama.
func TestTimescaleConvertExisting(t *testing.T) {
	target := testTarget(t)
	if !IsPostgresDSN(target) {
		t.Skip("PostgreSQL gerekli (UPTIME_TEST_PG)")
	}
	ctx := context.Background()
	plain, err := OpenWith(target, time.UTC, Options{TimescaleOff: true})
	if err != nil {
		t.Fatal(err)
	}
	if ext, _ := plain.tsExtension(ctx); ext == "" {
		plain.Close()
		t.Skip("TimescaleDB eklentisi yok")
	}
	if plain.DBMode() != DBModePostgres {
		t.Fatalf("kapalıyken kip %s", plain.DBMode())
	}
	m := newMonitor(t, plain, "a")
	m2 := newMonitor(t, plain, "b")
	now := time.Now().Unix()
	beat := func(s *Store, id, ts int64, st int, ping int64) {
		t.Helper()
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: id, Time: ts, Status: st, PingMs: ping}}); err != nil {
			t.Fatal(err)
		}
	}
	// 40 gün önce (ham saklama dışında kalacak) + son 3 gün + süren saat.
	for i := range 48 {
		beat(plain, m.ID, now-40*86400+int64(i)*600, StatusUp, int64(5+i*37%4000))
	}
	for i := range 3 * 24 * 6 {
		ts := now - 3*86400 + int64(i)*600
		st := StatusUp
		if i%17 == 0 {
			st = StatusDown
		}
		beat(plain, m.ID, ts, st, int64(3+i*53%12000))
		if i%3 == 0 {
			beat(plain, m2.ID, ts+7, StatusUp, -1)
		}
	}
	beat(plain, m.ID, now-30, StatusUp, 42)
	if _, err := plain.DeleteBeatsBefore(ctx, now-30*86400); err != nil {
		t.Fatal(err)
	}
	before := hourlyDump(t, plain)
	beatsBefore := countRows(t, plain, "heartbeats")
	upBefore, _, _ := plain.Uptime(ctx, m.ID, now-45*86400)
	plain.Close()

	s, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.DBMode() != DBModeTimescale {
		t.Fatalf("kip %s, timescale bekleniyordu", s.DBMode())
	}
	for _, h := range []string{"heartbeats", "mesh_samples"} {
		if ok, err := s.tsHas(ctx, "hypertables", h); err != nil || !ok {
			t.Fatalf("%s hypertable değil (%v)", h, err)
		}
	}
	if got := countRows(t, s, "heartbeats"); got != beatsBefore {
		t.Fatalf("ham kayıt %d, %d bekleniyordu", got, beatsBefore)
	}
	if after := hourlyDump(t, s); after != before {
		t.Fatalf("saatlik özet dönüşümde değişti\nönce:\n%s\nsonra:\n%s", before, after)
	}
	// Sürekli özetin tanımı Go özetiyle aynı: ham verinin olduğu saatlerde
	// stats_hourly_ts ile stats_hourly_legacy satır satır eşit.
	cmp := func(rel string) string {
		var out string
		s.db.QueryRowContext(ctx, "SELECT string_agg(concat_ws(',', monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max, "+
			histCols("")+"), ';' ORDER BY monitor_id, bucket) FROM "+rel+" WHERE bucket >= ? AND bucket < ?", now-3*86400+3600, now-now%3600).Scan(&out)
		return out
	}
	if a, b := cmp("stats_hourly_ts"), cmp("stats_hourly_legacy"); a != b || a == "" {
		t.Fatalf("sürekli özet Go özetinden farklı\nts:     %.300s\nlegacy: %.300s", a, b)
	}
	if up, _, _ := s.Uptime(ctx, m.ID, now-45*86400); up != upBefore {
		t.Fatalf("uptime %v, %v bekleniyordu", up, upBefore)
	}

	// Dönüşüm sonrası yazım: süren saat sürekli özetten (gerçek zamanlı).
	var upNow int64
	hour := now - now%3600
	q := "SELECT COALESCE(SUM(up), 0) FROM stats_hourly WHERE monitor_id = ? AND bucket = ?"
	s.db.QueryRowContext(ctx, q, m.ID, hour).Scan(&upNow)
	beat(s, m.ID, now-1, StatusUp, 9)
	var upAfter int64
	s.db.QueryRowContext(ctx, q, m.ID, hour).Scan(&upAfter)
	if upAfter != upNow+1 {
		t.Fatalf("yeni sonuç özete yansımadı: %d -> %d", upNow, upAfter)
	}

	// Sıfırlama: eski (legacy) ve sürekli özet satırları görünmez olur.
	if err := s.ResetMonitorStats(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	var left int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_hourly WHERE monitor_id = ?", m.ID).Scan(&left)
	if left != 0 {
		t.Fatalf("sıfırlama sonrası %d saatlik satır kaldı", left)
	}
	var other int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_hourly WHERE monitor_id = ?", m2.ID).Scan(&other)
	if other == 0 {
		t.Fatal("diğer monitörün özeti kayboldu")
	}
	// Saklama: ham silme önce özete işler, özet kaybolmaz.
	if _, err := s.DeleteBeatsBefore(ctx, now+3600); err != nil {
		t.Fatal(err)
	}
	var other2 int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_hourly WHERE monitor_id = ?", m2.ID).Scan(&other2)
	if other2 != other {
		t.Fatalf("ham silme sonrası özet %d, %d bekleniyordu", other2, other)
	}

	// Mesh örneği: bağlantı durumu yazılınca zaman serisine de düşer.
	if err := s.UpsertProbeLinks(ctx, []ProbeLink{{Src: 1, Dst: 0, State: LinkUp, RTTms: 12, UpdatedAt: now}, {Src: 0, Dst: 1, State: LinkUnknown, UpdatedAt: now}}); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, s, "mesh_samples"); n != 1 {
		t.Fatalf("mesh örneği %d, 1 bekleniyordu", n)
	}

	// İkinci açılış: dönüşüm tekrar yapılmaz, hata yok.
	again, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if again.DBMode() != DBModeTimescale {
		t.Fatalf("ikinci açılışta kip %s", again.DBMode())
	}
	again.Close()
}

func TestHistCol(t *testing.T) {
	for ping, want := range map[int64]string{0: "h00", 9: "h00", 10: "h01", 74: "h04", 29999: "h15", 30000: "h16", 99999: "h16"} {
		if got := histCol(ping); got != want {
			t.Errorf("histCol(%d) = %s, %s bekleniyordu", ping, got, want)
		}
	}
}
