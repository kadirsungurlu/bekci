package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	loc, _ := time.LoadLocation("Europe/Istanbul")
	s, err := Open(filepath.Join(t.TempDir(), "test.db"), loc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func newMonitor(t *testing.T, s *Store, name string) Monitor {
	t.Helper()
	m := Monitor{Name: name, Type: "http", Active: true, Interval: 60, RetryInterval: 60,
		Timeout: 30, Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := s.CreateMonitor(context.Background(), &m, nil); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMigrateIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		s, err := Open(filepath.Join(dir, "x.db"), time.UTC)
		if err != nil {
			t.Fatalf("açılış %d: %v", i, err)
		}
		s.Close()
	}
}

func TestFirstUserOnlyOnce(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	if _, err := s.CreateFirstUser(ctx, "kadir", "h"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateFirstUser(ctx, "baska", "h"); err == nil {
		t.Fatal("ikinci kurulum reddedilmeliydi")
	}
}

func TestRecordBeatUpdatesStatsAndMonitor(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "site")
	base := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC).Unix()

	beats := []Beat{
		{MonitorID: m.ID, Time: base, Status: StatusUp, PingMs: 100},
		{MonitorID: m.ID, Time: base + 60, Status: StatusUp, PingMs: 300},
		{MonitorID: m.ID, Time: base + 120, Status: StatusPending, PingMs: -1, Message: "zaman aşımı"},
		{MonitorID: m.ID, Time: base + 180, Status: StatusDown, PingMs: 50, Message: "500"},
	}
	for i, b := range beats {
		var change int64
		if i == 3 {
			change = b.Time
		}
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: b, LastChangeAt: change}); err != nil {
			t.Fatal(err)
		}
	}

	series, err := s.Series(ctx, m.ID, base, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("1 saatlik kova bekleniyordu, %d geldi", len(series))
	}
	b := series[0]
	// Bekleyen kontrol "up" sayılır; DOWN'un ping'i özete girmez.
	if b.Up != 3 || b.Down != 1 {
		t.Errorf("up/down = %d/%d, 3/1 bekleniyordu", b.Up, b.Down)
	}
	if b.PingAvg != 200 || b.PingMin != 100 || b.PingMax != 300 {
		t.Errorf("ping avg/min/max = %d/%d/%d, 200/100/300 bekleniyordu", b.PingAvg, b.PingMin, b.PingMax)
	}

	pct, ok, err := s.Uptime(ctx, m.ID, base)
	if err != nil || !ok || pct != 75 {
		t.Errorf("uptime = %v %v %v, 75 bekleniyordu", pct, ok, err)
	}

	got, err := s.GetMonitor(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDown || got.LastCheckAt != base+180 || got.LastChangeAt != base+180 || got.LastMessage != "500" {
		t.Errorf("monitör durumu yanlış: %+v", got)
	}

	daily, err := s.Series(ctx, m.ID, 0, true)
	if err != nil || len(daily) != 1 || daily[0].Up != 3 {
		t.Errorf("günlük özet yanlış: %+v %v", daily, err)
	}
}

func TestDailyBucketUsesLocalMidnight(t *testing.T) {
	s := openTest(t)
	// İstanbul UTC+3: 22:30 UTC yerel saatle ertesi gün 01:30.
	ts := time.Date(2026, 9, 26, 22, 30, 0, 0, time.UTC).Unix()
	want := time.Date(2026, 9, 27, 0, 0, 0, 0, s.loc).Unix()
	if got := s.dayStart(ts); got != want {
		t.Errorf("dayStart = %d, %d bekleniyordu", got, want)
	}
}

func TestIncidentLifecycle(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "site")

	if err := s.OpenIncident(ctx, m.ID, 1000, "500"); err != nil {
		t.Fatal(err)
	}
	// Açık olay varken ikinci açılış yok sayılır.
	if err := s.OpenIncident(ctx, m.ID, 1100, "tekrar"); err != nil {
		t.Fatal(err)
	}
	started, err := s.ResolveIncident(ctx, m.ID, 1600)
	if err != nil || started != 1000 {
		t.Fatalf("resolve = %d %v", started, err)
	}
	if started, _ := s.ResolveIncident(ctx, m.ID, 1700); started != 0 {
		t.Errorf("açık olay yokken 0 bekleniyordu, %d geldi", started)
	}
	list, err := s.ListIncidents(ctx, IncidentFilter{})
	if err != nil || len(list) != 1 || list[0].ResolvedAt != 1600 || list[0].MonitorName != "site" {
		t.Fatalf("olay listesi yanlış: %+v %v", list, err)
	}
}

func TestNotificationsLinkAndCascade(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m1 := newMonitor(t, s, "a")
	m2 := newMonitor(t, s, "b")

	n := Notification{Name: "wp", Type: "webhook", Config: json.RawMessage(`{}`), Active: true, IsDefault: true}
	if err := s.CreateNotification(ctx, &n, true); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{m1.ID, m2.ID} {
		list, err := s.NotificationsForMonitor(ctx, id)
		if err != nil || len(list) != 1 {
			t.Fatalf("monitör %d: %v %v", id, list, err)
		}
	}
	if err := s.DeleteMonitor(ctx, m1.ID); err != nil {
		t.Fatal(err)
	}
	links, _ := s.MonitorNotificationIDs(ctx)
	if _, ok := links[m1.ID]; ok {
		t.Error("silinen monitörün bağlantıları kalmamalı")
	}
	if err := s.DeleteNotification(ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.NotificationsForMonitor(ctx, m2.ID); len(list) != 0 {
		t.Error("silinen kanal bağlı kalmamalı")
	}
}

func TestCertNoticeOnce(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "a")
	first, _ := s.MarkCertNotice(ctx, m.ID, 5000, 7)
	second, _ := s.MarkCertNotice(ctx, m.ID, 5000, 7)
	if !first || second {
		t.Errorf("ilk=%v ikinci=%v; true/false bekleniyordu", first, second)
	}
}

func TestBackup(t *testing.T) {
	s := openTest(t)
	newMonitor(t, s, "a")
	path := filepath.Join(t.TempDir(), "yedek.db")
	if err := s.Backup(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	list, err := b.ListMonitors(context.Background())
	if err != nil || len(list) != 1 {
		t.Fatalf("yedekte monitör yok: %v %v", list, err)
	}
}
