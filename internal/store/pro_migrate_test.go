package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// Pro migration'ı çekirdek sürümü ilerletmez, bir kez uygulanır ve aradaki
// numaralar "atlandı" diye işaretlenmez.
func TestProMigrationAyriIzlenir(t *testing.T) {
	RegisterMigration(ProMigrationStart, "CREATE TABLE pro_deneme (id INTEGER PRIMARY KEY)")
	defer delete(migrations, ProMigrationStart)
	s, err := Open(filepath.Join(t.TempDir(), "uptime.db"), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if v, _ := s.schemaVersion(ctx); v != latestMigration() || v >= ProMigrationStart {
		t.Fatalf("çekirdek sürüm %d", v)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO pro_deneme (id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM migrate_skipped WHERE version > ?", latestMigration()).Scan(&n); err != nil || n != 0 {
		t.Fatalf("atlanan %d %v", n, err)
	}
	if err := s.migrate(ctx); err != nil { // ikinci kez: yeniden uygulamaz
		t.Fatal(err)
	}
}
