// Package store tüm kalıcı veriyi yönetir. İki veritabanı desteklenir:
//
//   - SQLite (varsayılan): tek dosya, kurulumu sıfır. Tek bağlantı kullanılır
//     (SetMaxOpenConns(1)); SQLite zaten tek yazıcıya izin verir, tek bağlantı
//     "database is locked" hatalarını tamamen ortadan kaldırır.
//   - PostgreSQL: DATABASE_URL (postgres://...) verilince. Bağlantı havuzu
//     kullanılır.
//
// SQL her iki veritabanında çalışan ortak alt kümede yazılır; sorgulardaki `?`
// yer tutucuları PostgreSQL için `$1, $2…` biçimine çevrilir (bkz. conn), migration
// DDL'i de PostgreSQL'e çevrilir (bkz. migrate.go).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// ErrNotFound istenen kayıt yoksa döner.
var ErrNotFound = errors.New("kayıt bulunamadı")

// ErrBackupUnsupported PostgreSQL'de uygulama içi yedek alınmaz.
var ErrBackupUnsupported = errors.New("PostgreSQL yedeği veritabanı tarafında alınmalı")

// Durum kodları (Uptime Kuma ile aynı).
const (
	StatusDown        = 0
	StatusUp          = 1
	StatusPending     = 2
	StatusMaintenance = 3 // bakım penceresinde: uptime hesabına girmez, bildirim gitmez
)

type Store struct {
	db       *conn
	postgres bool
	path     string         // SQLite dosya yolu
	loc      *time.Location // günlük özetlerin gün sınırı bu saat dilimine göre
	opts     Options
	lockFile *os.File // InstanceLock: süreç boyunca tutulan veri klasörü kilidi
}

// Options Open'ın isteğe bağlı ayarları.
type Options struct {
	// BackupDir migration öncesi SQLite yedeğinin klasörü; boşsa veritabanı
	// dosyasının yanındaki "backups" klasörü (DATA_DIR/backups).
	BackupDir string
	// Log migration kayıtları için; nil ise slog.Default().
	Log *slog.Logger
	// InstanceLock SQLite dosyasına özel kilit alır: aynı veri klasörünü iki
	// örnek aynı anda kullanamaz, ikinci örnek birincinin kapanmasını bekler.
	// Yalnızca uygulama açar; testler aynı dosyayı birden çok kez açabilir.
	InstanceLock bool
	// LockWait kilidin en fazla ne kadar bekleneceği (0 → 10 dk).
	LockWait time.Duration
	// StandbyFile kilit beklenirken oluşturulan dosya; sağlık kontrolü bu
	// dosya varken bekleyen örneği sağlıklı sayar (bkz. Dockerfile).
	StandbyFile string
}

// IsPostgresDSN bağlantı adresinin PostgreSQL olup olmadığını söyler.
func IsPostgresDSN(dsn string) bool {
	return strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://")
}

// Open veritabanını açar ve migration'ları uygular. target bir PostgreSQL
// adresi (postgres://…) ya da SQLite dosya yoludur.
func Open(target string, loc *time.Location) (*Store, error) {
	return OpenWith(target, loc, Options{})
}

// OpenWith Open'ın ayarlı biçimi (bkz. Options).
func OpenWith(target string, loc *time.Location, opts Options) (*Store, error) {
	if loc == nil {
		loc = time.Local
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	s := &Store{loc: loc, postgres: IsPostgresDSN(target), opts: opts}
	var (
		raw *sql.DB
		err error
	)
	if s.postgres {
		raw, err = sql.Open("pgx", target)
		if err != nil {
			return nil, err
		}
		raw.SetMaxOpenConns(10)
		raw.SetMaxIdleConns(5)
		raw.SetConnMaxLifetime(30 * time.Minute)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := raw.PingContext(ctx); err != nil {
			raw.Close()
			return nil, fmt.Errorf("PostgreSQL'e bağlanılamadı: %w", err)
		}
	} else {
		s.path = target
		if opts.InstanceLock {
			if err := s.acquireInstanceLock(target); err != nil {
				return nil, err
			}
		}
		raw, err = sql.Open("sqlite", sqliteDSN(target))
		if err != nil {
			s.releaseLock()
			return nil, err
		}
		raw.SetMaxOpenConns(1)
		raw.SetConnMaxLifetime(0)
	}
	s.db = &conn{db: raw, pg: s.postgres}
	if err := s.migrate(context.Background()); err != nil {
		raw.Close()
		s.releaseLock()
		return nil, fmt.Errorf("migration: %w", err)
	}
	return s, nil
}

func sqliteDSN(path string) string {
	return "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=synchronous(NORMAL)" +
		"&_pragma=foreign_keys(1)"
}

func (s *Store) Close() error {
	err := s.db.db.Close()
	s.releaseLock()
	return err
}

// releaseLock veri klasörü kilidini bırakır (dosyayı kapatmak flock'u bırakır).
func (s *Store) releaseLock() {
	if s.lockFile != nil {
		s.lockFile.Close()
		s.lockFile = nil
	}
}

// Postgres veritabanının PostgreSQL olup olmadığı.
func (s *Store) Postgres() bool { return s.postgres }

// Ping veritabanının erişilebilir olduğunu doğrular (healthz için).
func (s *Store) Ping(ctx context.Context) error { return s.db.db.PingContext(ctx) }

// Backup veritabanının tutarlı bir kopyasını path'e yazar (yalnız SQLite).
// Ayrı bir bağlantı kullanılır: büyük bir veritabanında yedek sürerken asıl
// bağlantı (kontrol kayıtları, API, sağlık kontrolü) beklemez. WAL modunda
// okuyucu yazıcıyı engellemez.
func (s *Store) Backup(ctx context.Context, path string) error {
	if s.postgres {
		return ErrBackupUnsupported
	}
	db, err := sql.Open("sqlite", sqliteDSN(s.path))
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, "VACUUM INTO ?", path)
	return err
}

func (s *Store) tx(ctx context.Context, fn func(*Tx) error) error {
	raw, err := s.db.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		// fn panik yaparsa işlem geri alınır: aksi halde (SQLite'ta tek olan)
		// bağlantı açık işlemle kalır ve sonraki tüm yazmalar kilitlenir.
		if p := recover(); p != nil {
			raw.Rollback()
			panic(p)
		}
	}()
	if err := fn(&Tx{tx: raw, pg: s.postgres}); err != nil {
		raw.Rollback()
		return err
	}
	return raw.Commit()
}

// conn ve Tx, database/sql'in aynı adlı yöntemlerini sarar; PostgreSQL'de
// sorgudaki `?` yer tutucularını `$n` biçimine çevirir.
type conn struct {
	db *sql.DB
	pg bool
}

func (c *conn) q(query string) string {
	if c.pg {
		return rebind(query)
	}
	return query
}

func (c *conn) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return c.db.ExecContext(ctx, c.q(query), args...)
}

func (c *conn) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return c.db.QueryContext(ctx, c.q(query), args...)
}

func (c *conn) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return c.db.QueryRowContext(ctx, c.q(query), args...)
}

// Tx bir işlem (transaction); yöntemleri conn ile aynı.
type Tx struct {
	tx *sql.Tx
	pg bool
}

func (t *Tx) q(query string) string {
	if t.pg {
		return rebind(query)
	}
	return query
}

func (t *Tx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.tx.ExecContext(ctx, t.q(query), args...)
}

func (t *Tx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.tx.QueryContext(ctx, t.q(query), args...)
}

func (t *Tx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.tx.QueryRowContext(ctx, t.q(query), args...)
}

// rebind `?` yer tutucularını `$1, $2…` biçimine çevirir; tek tırnaklı metin
// ve çift tırnaklı adların içindeki `?` karakterlerine dokunmaz.
func rebind(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	n := 0
	var quote rune
	for _, r := range query {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '?':
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// insertID "INSERT … RETURNING id" sorgusunu çalıştırıp yeni kimliği döner
// (LastInsertId PostgreSQL'de desteklenmez).
func insertID(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, query string, args ...any) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id)
	return id, err
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
