package store

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// syncBuf eşzamanlı yazılabilen log tamponu.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (w *syncBuf) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.Write(p)
}

func (w *syncBuf) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.b.String()
}

// withTestMigration testin süresince en sona bir migration ekler.
func withTestMigration(t *testing.T) int {
	t.Helper()
	v := latestMigration() + 1
	migrations[v] = `CREATE TABLE migrate_test (id INTEGER PRIMARY KEY);`
	t.Cleanup(func() { delete(migrations, v) })
	return v
}

// Var olan SQLite veritabanı güncellenirken önce yedek alınır; her
// uygulanan migration loglanır.
func TestPreMigrateBackupSQLite(t *testing.T) {
	if os.Getenv("UPTIME_TEST_PG") != "" {
		t.Skip("SQLite'a özgü")
	}
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "uptime.db")
	s, err := Open(dbPath, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	newMonitor(t, s, "yedekte-olmali")
	old, _ := s.schemaVersion(ctx)
	s.Close()
	if _, err := os.Stat(filepath.Join(filepath.Dir(dbPath), "backups")); !os.IsNotExist(err) {
		t.Fatal("yeni veritabanında migration öncesi yedek alınmamalı")
	}

	v := withTestMigration(t)
	var logs syncBuf
	s, err = OpenWith(dbPath, time.UTC, Options{Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := s.schemaVersion(ctx); got != v {
		t.Fatalf("sürüm %d, %d bekleniyordu", got, v)
	}
	if !strings.Contains(logs.String(), `msg="migration uygulandı" version=`+strconv.Itoa(v)) {
		t.Fatalf("migration logu yok: %s", logs.String())
	}
	// Varsayılan klasör: veritabanının yanındaki backups (DATA_DIR/backups).
	matches, _ := filepath.Glob(filepath.Join(filepath.Dir(dbPath), "backups",
		"pre-migrate-v"+strconv.Itoa(old)+"-to-v"+strconv.Itoa(v)+"-*.db"))
	if len(matches) != 1 {
		t.Fatalf("yedek dosyası: %v", matches)
	}
	// Yedek eski sürümde ve verisi tam (migration uygulanmadan, ham okunur).
	db, err := sql.Open("sqlite", matches[0])
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ver int
	var name string
	if err := db.QueryRow("PRAGMA user_version").Scan(&ver); err != nil || ver != old {
		t.Fatalf("yedeğin sürümü %d (%v), %d bekleniyordu", ver, err, old)
	}
	if err := db.QueryRow("SELECT name FROM monitors").Scan(&name); err != nil || name != "yedekte-olmali" {
		t.Fatalf("yedekte monitör: %q %v", name, err)
	}
}

// Yedek alınamazsa migration uygulanmaz, açılış hata verir.
func TestPreMigrateBackupFailureAborts(t *testing.T) {
	if os.Getenv("UPTIME_TEST_PG") != "" {
		t.Skip("SQLite'a özgü")
	}
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "uptime.db")
	s, err := Open(dbPath, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	old, _ := s.schemaVersion(ctx)
	s.Close()

	withTestMigration(t)
	notDir := filepath.Join(dir, "dosya")
	os.WriteFile(notDir, []byte("x"), 0o600)
	_, err = OpenWith(dbPath, time.UTC, Options{BackupDir: filepath.Join(notDir, "backups"), Log: slog.New(slog.NewTextHandler(&syncBuf{}, nil))})
	if err == nil || !strings.Contains(err.Error(), "migration UYGULANMADI") {
		t.Fatalf("yedek hatasıyla açılış durmalı: %v", err)
	}
	delete(migrations, latestMigration())
	s, err = Open(dbPath, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := s.schemaVersion(ctx); got != old {
		t.Fatalf("veritabanı değişmemeli: sürüm %d, %d bekleniyordu", got, old)
	}
}

// PostgreSQL: aynı şemaya aynı anda açılan örnekler migration'ları danışma
// kilidiyle sırayla uygular (hata yok, her migration bir kez); var olan
// veritabanı güncellenirken yedek yerine uyarı yazılır.
func TestPostgresMigrateLockAndWarning(t *testing.T) {
	if os.Getenv("UPTIME_TEST_PG") == "" {
		t.Skip("UPTIME_TEST_PG ayarlı değil")
	}
	ctx := context.Background()
	target := testTarget(t)
	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for range 4 {
		wg.Go(func() {
			s, err := OpenWith(target, time.UTC, Options{Log: slog.New(slog.NewTextHandler(&syncBuf{}, nil))})
			if err != nil {
				errs <- err
				return
			}
			s.Close()
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("eşzamanlı açılış: %v", err)
	}

	v := withTestMigration(t)
	var logs syncBuf
	s, err := OpenWith(target, time.UTC, Options{Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got, _ := s.schemaVersion(ctx); got != v {
		t.Fatalf("sürüm %d, %d bekleniyordu", got, v)
	}
	var n int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM schema_version").Scan(&n)
	if n != 1 {
		t.Fatalf("schema_version satırı %d, 1 bekleniyordu", n)
	}
	if out := logs.String(); !strings.Contains(out, "otomatik yedek ALINMIYOR") || !strings.Contains(out, "version="+strconv.Itoa(v)) {
		t.Fatalf("uyarı/migration logu yok: %s", out)
	}
}
