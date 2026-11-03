// Package storetest testler için veritabanı açar. UPTIME_TEST_PG ortam
// değişkeni bir PostgreSQL adresi içeriyorsa her test kendi geçici şemasında
// PostgreSQL'e karşı çalışır; yoksa geçici bir SQLite dosyası kullanılır.
//
//	UPTIME_TEST_PG=postgres://postgres:parola@pg:5432/postgres?sslmode=disable go test ./...
package storetest

import (
	"github.com/kadirsungurlu/bekci/internal/store/pgtestdb"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// PG testlerin PostgreSQL'e karşı çalışıp çalışmadığı.
func PG() bool { return os.Getenv("UPTIME_TEST_PG") != "" }

// Open testi için boş, migration'ları uygulanmış bir veritabanı açar ve test
// bitince kapatıp siler.
func Open(t testing.TB, loc *time.Location) *store.Store {
	t.Helper()
	target := Target(t)
	s, err := store.Open(target, loc)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Target testi için yeni bir veritabanı adresi (SQLite yolu veya kendi
// şemasına bakan PostgreSQL adresi) üretir; aynı adres birden çok kez
// açılabilir (ör. yeniden başlatma testleri).
func Target(t testing.TB) string {
	t.Helper()
	dsn := os.Getenv("UPTIME_TEST_PG")
	if dsn == "" {
		return filepath.Join(t.TempDir(), "test.db")
	}
	return pgtestdb.Target(t, dsn)
}
