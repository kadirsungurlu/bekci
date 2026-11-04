package store

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store/pgtestdb"
)

var errKesinti = errors.New("kesinti")

// seedSQLite gerçekçi küçük bir kaynak: boolean/blob/NUL'lu metin, üst-alt tablolar.
func seedSQLite(t *testing.T, path string, beats int) {
	t.Helper()
	st, err := OpenWith(path, time.UTC, Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	ctx := context.Background()
	exec := func(q string, a ...any) {
		t.Helper()
		if _, err := st.db.db.ExecContext(ctx, q, a...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, password_hash, role, created_at) VALUES (7, 'yonetici', 'x', 'admin', 1)`)
	exec(`INSERT INTO monitors (id, name, type, config, created_at, updated_at) VALUES (5, 'site', 'http', '{}', 1, 1), (9, 'api', 'http', '{}', 1, 1)`)
	now := time.Now().Unix()
	tx, err := st.db.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < beats; i++ {
		if _, err := tx.Exec(`INSERT INTO heartbeats (monitor_id, time, status, ping_ms, message) VALUES (?, ?, 1, ?, ?)`,
			5+(i%2)*4, now-int64(i)*60, 20+i%50, "tamam\x00bozuk"); err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	h := now - now%3600 - 7*86400
	exec(`INSERT INTO stats_hourly (monitor_id, bucket, up, down, ping_sum, ping_count) VALUES (5, ?, 60, 0, 1200, 60)`, h)
}

func migOpts(from, to string) SQLiteMigrateOptions {
	return SQLiteMigrateOptions{From: from, To: to, Batch: 100, Loc: time.UTC,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestMigrateSQLite(t *testing.T) {
	dsn := os.Getenv("UPTIME_TEST_PG")
	if dsn == "" {
		t.Skip("UPTIME_TEST_PG yok")
	}
	ctx := context.Background()
	from := filepath.Join(t.TempDir(), "uptime.db")
	seedSQLite(t, from, 950)
	before, _ := os.ReadFile(from)
	to := pgtestdb.Target(t, dsn)

	// Üçüncü partide kesinti: yazılan kalır, ikinci çalıştırma tamamlar.
	o := migOpts(from, to)
	o.afterBatch = func(tbl string, n int64) error {
		if tbl == "heartbeats" && n >= 300 {
			return errKesinti
		}
		return nil
	}
	if _, err := MigrateSQLite(ctx, o); !errors.Is(err, errKesinti) {
		t.Fatalf("kesinti beklenirdi: %v", err)
	}
	if err := CheckSQLiteLeftover(ctx, to, from); !errors.Is(err, ErrSQLitePending) {
		t.Fatalf("yarım taşımada panel açılmamalı: %v", err)
	}
	res, err := MigrateSQLite(ctx, migOpts(from, to))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Resumed || res.Sanitized != 650 {
		t.Fatalf("devam/temizlik: %+v", res)
	}
	if err := CheckSQLiteLeftover(ctx, to, from); err != nil {
		t.Fatalf("bitmiş taşımada panel açılmalı: %v", err)
	}
	after, _ := os.ReadFile(from)
	if string(before) != string(after) {
		t.Fatal("kaynak dosya değişti")
	}

	st, err := OpenWith(to, time.UTC, Options{Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	var beats, hourly, nextMon int64
	var msg string
	st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM heartbeats").Scan(&beats)
	st.db.QueryRowContext(ctx, "SELECT message FROM heartbeats LIMIT 1").Scan(&msg)
	st.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM stats_hourly").Scan(&hourly)
	if beats != 950 || strings.Contains(msg, "\x00") || hourly == 0 {
		t.Fatalf("beats=%d msg=%q hourly=%d", beats, msg, hourly)
	}
	// Sayaç taşınan kimliğin üstünden sürmeli.
	if err := st.db.QueryRowContext(ctx, `INSERT INTO monitors (name, type, config, created_at, updated_at) VALUES ('yeni', 'http', '{}', 1, 1) RETURNING id`).Scan(&nextMon); err != nil {
		t.Fatal(err)
	}
	if nextMon <= 9 {
		t.Fatalf("sayaç sıfırlanmadı: %d", nextMon)
	}

	// Bitmiş hedefe ikinci çalıştırma veri kopyalamaz; --restart reddedilir.
	if res, err := MigrateSQLite(ctx, migOpts(from, to)); err != nil || res.Rows != 0 {
		t.Fatalf("ikinci çalıştırma: %+v %v", res, err)
	}
	o = migOpts(from, to)
	o.Restart = true
	if _, err := MigrateSQLite(ctx, o); err == nil {
		t.Fatal("bitmiş hedefte --restart reddedilmeli")
	}
}

func TestMigrateSQLiteRefuses(t *testing.T) {
	dsn := os.Getenv("UPTIME_TEST_PG")
	if dsn == "" {
		t.Skip("UPTIME_TEST_PG yok")
	}
	ctx := context.Background()
	from := filepath.Join(t.TempDir(), "uptime.db")
	seedSQLite(t, from, 10)

	// Dolu hedef.
	to := pgtestdb.Target(t, dsn)
	st, err := OpenWith(to, time.UTC, Options{TimescaleOff: true, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.db.ExecContext(ctx, `INSERT INTO users (username, password_hash, role, created_at) VALUES ('a', 'x', 'admin', 1)`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if _, err := MigrateSQLite(ctx, migOpts(from, to)); err == nil || !strings.Contains(err.Error(), "boş değil") {
		t.Fatalf("dolu hedef reddedilmeli: %v", err)
	}

	// Kaynak değişti: devam edilmez, --restart baştan alır.
	to2 := pgtestdb.Target(t, dsn)
	o := migOpts(from, to2)
	o.afterBatch = func(string, int64) error { return errKesinti }
	if _, err := MigrateSQLite(ctx, o); !errors.Is(err, errKesinti) {
		t.Fatalf("kesinti: %v", err)
	}
	raw, err := sql.Open("sqlite", sqliteDSN(from))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO heartbeats (monitor_id, time, status) VALUES (5, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	if _, err := MigrateSQLite(ctx, migOpts(from, to2)); err == nil || !strings.Contains(err.Error(), "değişmiş") {
		t.Fatalf("değişen kaynak reddedilmeli: %v", err)
	}
	o = migOpts(from, to2)
	o.Restart = true
	if _, err := MigrateSQLite(ctx, o); err != nil {
		t.Fatal(err)
	}
}
