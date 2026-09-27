// Package store SQLite üzerindeki tüm kalıcı veriyi yönetir.
//
// Tek bağlantı kullanılır (SetMaxOpenConns(1)): SQLite zaten tek yazıcıya izin
// verir, tek bağlantı "database is locked" hatalarını tamamen ortadan kaldırır.
// Kontrol sonuçları küçük ve hızlı yazımlar olduğundan yüzlerce monitörde de
// darboğaz oluşturmaz.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound istenen kayıt yoksa döner.
var ErrNotFound = errors.New("kayıt bulunamadı")

// Durum kodları (Uptime Kuma ile aynı).
const (
	StatusDown        = 0
	StatusUp          = 1
	StatusPending     = 2
	StatusMaintenance = 3 // bakım penceresinde: uptime hesabına girmez, bildirim gitmez
)

type Store struct {
	db   *sql.DB
	path string
	loc  *time.Location // günlük özetlerin gün sınırı bu saat dilimine göre
}

// Open veritabanını açar ve migration'ları uygular.
func Open(path string, loc *time.Location) (*Store, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(0)
	if loc == nil {
		loc = time.Local
	}
	s := &Store{db: db, path: path, loc: loc}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migration: %w", err)
	}
	return s, nil
}

func dsn(path string) string {
	return "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
}

func (s *Store) Close() error { return s.db.Close() }

// Ping veritabanının erişilebilir olduğunu doğrular (healthz için).
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// Backup veritabanının tutarlı bir kopyasını path'e yazar. Ayrı bir bağlantı
// kullanılır: büyük bir veritabanında yedek sürerken asıl bağlantı (kontrol
// kayıtları, API, sağlık kontrolü) beklemez. WAL modunda okuyucu yazıcıyı
// engellemez.
func (s *Store) Backup(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", dsn(s.path))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// nullInt 0'ı NULL olarak yazar (zaman damgaları ve ping için "yok" anlamında).
func nullInt(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
