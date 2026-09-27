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

func TestServerStatsRetention(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	p := store.Probe{Kind: store.ProbeKindServer, Name: "cp", Active: true, CreatedAt: 1, Hash: "h"}
	if err := st.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 4, 0, 0, 0, time.UTC)
	ago := func(d time.Duration) int64 { return now.Add(-d).Unix() }
	for res, times := range map[int][]int64{
		store.ServerRes1:  {ago(25 * time.Hour), ago(23 * time.Hour)},
		store.ServerRes10: {ago(8 * 24 * time.Hour), ago(6 * 24 * time.Hour)},
		store.ServerRes60: {ago(91 * 24 * time.Hour), ago(89 * 24 * time.Hour)},
	} {
		for _, ts := range times {
			if err := st.UpsertServerStats(ctx, p.ID, res, ts, []byte(`{}`)); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Uyarı geçmişi: 100 gün önce başlayıp biten kayıt silinir, süren kalır.
	st.ReplaceServerAlerts(ctx, p.ID, []store.ServerAlert{{Metric: "cpu", Threshold: 90, Minutes: 10, Active: true}}, 0)
	rules, _ := st.ServerAlerts(ctx, p.ID)
	rules[0].ProbeID = p.ID
	st.FireServerAlert(ctx, rules[0], 95, "", ago(100*24*time.Hour))
	st.ResolveServerAlert(ctx, rules[0].ID, ago(99*24*time.Hour))
	st.FireServerAlert(ctx, rules[0], 95, "", ago(24*time.Hour))

	mt := NewMaintenance(st, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), time.UTC)
	mt.now = func() time.Time { return now }
	mt.Tick(ctx)
	for _, res := range []int{store.ServerRes1, store.ServerRes10, store.ServerRes60} {
		if rows, _ := st.ServerStats(ctx, p.ID, res, 0, now.Unix()); len(rows) != 1 {
			t.Errorf("res=%d: %d satır kaldı, 1 bekleniyordu", res, len(rows))
		}
	}
	if evs, _ := st.ServerAlertEvents(ctx, p.ID, 0, 10); len(evs) != 1 || evs[0].EndedAt != 0 {
		t.Errorf("uyarı geçmişi: %+v", evs)
	}
}

// Çözülmüş olaylar 365 gün sonra silinir; süren olay (eski de olsa) kalır.
func TestIncidentRetention(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	now := time.Date(2026, 9, 27, 4, 0, 0, 0, time.UTC)
	day := func(n int) int64 { return now.AddDate(0, 0, -n).Unix() }
	var mons []store.Monitor
	for _, name := range []string{"eski", "yeni", "suren"} {
		m := store.Monitor{Name: name, Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30, Config: json.RawMessage(`{}`)}
		if err := st.CreateMonitor(ctx, &m, nil); err != nil {
			t.Fatal(err)
		}
		mons = append(mons, m)
	}
	st.StartIncident(ctx, mons[0].ID, day(400), "500")
	st.ResolveIncident(ctx, mons[0].ID, day(366))
	st.StartIncident(ctx, mons[1].ID, day(370), "500")
	st.ResolveIncident(ctx, mons[1].ID, day(364))
	st.StartIncident(ctx, mons[2].ID, day(500), "hâlâ kapalı")

	mt := NewMaintenance(st, slog.New(slog.NewTextHandler(io.Discard, nil)), t.TempDir(), time.UTC)
	mt.now = func() time.Time { return now }
	mt.Tick(ctx)
	list, _ := st.ListIncidents(ctx, store.IncidentFilter{})
	if len(list) != 2 || list[0].MonitorID != mons[2].ID || list[1].MonitorID != mons[1].ID {
		t.Fatalf("kalan olaylar: %+v", list)
	}
}
