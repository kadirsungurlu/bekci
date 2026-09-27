package stats

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store/storetest"
)

func TestMaintenance(t *testing.T) {
	dir := t.TempDir()
	loc, _ := time.LoadLocation("Europe/Istanbul")
	st := storetest.Open(t, loc)
	ctx := context.Background()

	m := store.Monitor{Name: "a", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30, Config: json.RawMessage(`{}`)}
	if err := st.CreateMonitor(ctx, &m, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 4, 0, 0, 0, loc)
	old := now.AddDate(0, 0, -20).Unix()
	recent := now.Add(-time.Hour).Unix()
	for _, ts := range []int64{old, recent} {
		if err := st.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: m.ID, Time: ts, Status: 1, PingMs: 5}}); err != nil {
			t.Fatal(err)
		}
	}

	mt := NewMaintenance(st, slog.New(slog.NewTextHandler(io.Discard, nil)), dir, loc)
	mt.now = func() time.Time { return now }

	if st.Postgres() {
		// PostgreSQL'de uygulama yedek almaz; sadece temizlik doğrulanır.
		mt.Tick(ctx)
		if beats, _ := st.Beats(ctx, m.ID, 0); len(beats) != 1 {
			t.Errorf("14 günden eski ham kayıt silinmeliydi: %+v", beats)
		}
		return
	}
	// Eski yedekler: saklama sınırı 7, 8 eski + bugünkü → 7 kalmalı.
	os.MkdirAll(mt.backupDir, 0o750)
	for i := 1; i <= 8; i++ {
		name := "uptime-" + now.AddDate(0, 0, -i).Format("2006-01-02") + ".db"
		os.WriteFile(filepath.Join(mt.backupDir, name), []byte("x"), 0o640)
	}

	mt.Tick(ctx)

	beats, _ := st.Beats(ctx, m.ID, 0)
	if len(beats) != 1 || beats[0].Time != recent {
		t.Errorf("14 günden eski ham kayıt silinmeliydi: %+v", beats)
	}
	// Saatlik özetler 1 yıl tutulur; 20 günlük özet kalmalı.
	if series, _ := st.Series(ctx, m.ID, 0, false); len(series) != 2 {
		t.Errorf("saatlik özetler silinmemeliydi: %d", len(series))
	}

	entries, _ := os.ReadDir(mt.backupDir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	today := "uptime-2026-09-27.db"
	if len(names) != 7 || names[len(names)-1] != today || names[0] != "uptime-2026-09-21.db" {
		t.Fatalf("yedekler yanlış: %v", names)
	}
	// Bugünün yedeği gerçek, açılabilir bir veritabanı olmalı.
	b, err := store.Open(filepath.Join(mt.backupDir, today), loc)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if list, _ := b.ListMonitors(ctx); len(list) != 1 {
		t.Error("yedekte monitör yok")
	}

	// Aynı gün ikinci tur yeni yedek almaz.
	info1, _ := os.Stat(filepath.Join(mt.backupDir, today))
	mt.Tick(ctx)
	info2, _ := os.Stat(filepath.Join(mt.backupDir, today))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("aynı gün yedek tekrar alındı")
	}
}

func TestNoBackupBeforeHour(t *testing.T) {
	dir := t.TempDir()
	st := storetest.Open(t, time.UTC)
	mt := NewMaintenance(st, slog.New(slog.NewTextHandler(io.Discard, nil)), dir, time.UTC)
	mt.now = func() time.Time { return time.Date(2026, 9, 27, 1, 0, 0, 0, time.UTC) }
	mt.Tick(context.Background())
	if _, err := os.Stat(mt.backupDir); !os.IsNotExist(err) {
		t.Error("saat 03:00'ten önce yedek alınmamalı")
	}
}
