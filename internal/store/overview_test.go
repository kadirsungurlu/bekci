package store

import (
	"context"
	"math"
	"slices"
	"testing"
	"time"
)

func TestRecentPings(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, b, c := newMonitor(t, s, "a"), newMonitor(t, s, "b"), newMonitor(t, s, "c")
	now := time.Now().Unix()
	for i := range 40 {
		st, ping := StatusUp, int64(i)
		if i == 38 {
			st = StatusDown
		}
		if i == 39 {
			ping = -1
		}
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: a.ID, Time: now - 400 + int64(i), Status: st, PingMs: ping}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: b.ID, Time: now, Status: StatusUp, PingMs: 7}}); err != nil {
		t.Fatal(err)
	}
	all, err := s.RecentPings(ctx, nil, 5)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(all[a.ID], []int64{35, 36, 37, PingDown, PingNone}) || !slices.Equal(all[b.ID], []int64{7}) || all[c.ID] != nil {
		t.Errorf("tümü: %v", all)
	}
	one, err := s.RecentPings(ctx, []int64{b.ID}, 5)
	if err != nil || len(one) != 1 || !slices.Equal(one[b.ID], []int64{7}) {
		t.Errorf("tek: %v %v", one, err)
	}
}

// UptimeWindows (günlük + saatlik özet) Store.Uptime ile (yalnızca saatlik) aynı sonucu verir.
func TestUptimeWindowsMatchesUptime(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, b := newMonitor(t, s, "a"), newMonitor(t, s, "b")
	now := time.Now().Unix()
	start := now - now%3600 - 40*86400
	// Her saat bir kontrol; a'da bazı saatler başarısız, b'de yalnızca son 3 günde veri.
	for h := start; h <= now; h += 3600 {
		st := StatusUp
		if (h/3600)%13 == 0 {
			st = StatusDown
		}
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: a.ID, Time: h + 60, Status: st, PingMs: 10}}); err != nil {
			t.Fatal(err)
		}
		if h > now-3*86400 {
			if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: b.ID, Time: h + 60, Status: StatusUp, PingMs: 10}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	day := s.dayStart(now - 5*86400)
	sinces := []int64{now - 7*86400, now - 30*86400, day, day + 1, now - 3600}
	got, err := s.UptimeWindows(ctx, nil, sinces...)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{a.ID, b.ID} {
		for i, since := range sinces {
			want, ok, err := s.Uptime(ctx, id, since)
			if err != nil {
				t.Fatal(err)
			}
			g := got[id][i]
			if !ok {
				if g != nil {
					t.Errorf("%d/%d: veri yokken %v", id, i, *g)
				}
				continue
			}
			if g == nil || math.Abs(*g-want) > 1e-9 {
				t.Errorf("%d/%d: %v != %v", id, i, g, want)
			}
		}
	}
	one, err := s.UptimeWindows(ctx, []int64{b.ID}, now-7*86400)
	if err != nil || len(one) != 1 || one[b.ID][0] == nil || *one[b.ID][0] != 100 {
		t.Errorf("tek monitör: %v %v", one, err)
	}
}
