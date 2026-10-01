package store

import (
	"context"
	"database/sql/driver"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// migrations sürüm numarasına göre sırayla uygulanır; uygulanan son sürüm
// PRAGMA user_version'da tutulur. Var olan bir migration asla değiştirilmez,
// yenisi eklenir. Özellik dosyaları kendi migration'larını init() içinde
// RegisterMigration ile ekleyebilir (paralel geliştirmede çakışma olmasın diye);
// numaralar çakışamaz. Paralel dallarda bir numara geçici olarak eksik
// kalabilir (ör. 17 başka dalda, 18 bu dalda): eksik numara atlanır ve
// migrate_skipped tablosuna yazılır; o migration sonradan (dallar
// birleşince) eklendiğinde sürüm geride kalsa da bir kez uygulanır.
var migrations = map[int]string{
	// 1: ilk şema
	1: `
CREATE TABLE users (
	id            INTEGER PRIMARY KEY,
	username      TEXT    NOT NULL UNIQUE,
	password_hash TEXT    NOT NULL,
	created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
	token_hash TEXT    PRIMARY KEY,
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
) WITHOUT ROWID;

CREATE TABLE monitors (
	id                 INTEGER PRIMARY KEY,
	name               TEXT    NOT NULL,
	type               TEXT    NOT NULL,
	description        TEXT    NOT NULL DEFAULT '',
	active             INTEGER NOT NULL DEFAULT 1,
	interval_sec       INTEGER NOT NULL DEFAULT 60,
	retry_interval_sec INTEGER NOT NULL DEFAULT 60,
	max_retries        INTEGER NOT NULL DEFAULT 0,
	timeout_sec        INTEGER NOT NULL DEFAULT 30,
	resend_every       INTEGER NOT NULL DEFAULT 0,
	upside_down        INTEGER NOT NULL DEFAULT 0,
	config             TEXT    NOT NULL DEFAULT '{}',
	push_token         TEXT    UNIQUE,
	status             INTEGER NOT NULL DEFAULT 2,
	last_check_at      INTEGER,
	last_change_at     INTEGER,
	last_ping_ms       INTEGER,
	last_message       TEXT    NOT NULL DEFAULT '',
	cert_expires_at    INTEGER,
	cert_issuer        TEXT    NOT NULL DEFAULT '',
	created_at         INTEGER NOT NULL,
	updated_at         INTEGER NOT NULL
);

CREATE TABLE heartbeats (
	id         INTEGER PRIMARY KEY,
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	time       INTEGER NOT NULL,
	status     INTEGER NOT NULL,
	ping_ms    INTEGER,
	message    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX heartbeats_monitor_time ON heartbeats(monitor_id, time);
CREATE INDEX heartbeats_time ON heartbeats(time);

CREATE TABLE stats_hourly (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	bucket     INTEGER NOT NULL,
	up         INTEGER NOT NULL DEFAULT 0,
	down       INTEGER NOT NULL DEFAULT 0,
	ping_sum   INTEGER NOT NULL DEFAULT 0,
	ping_count INTEGER NOT NULL DEFAULT 0,
	ping_min   INTEGER,
	ping_max   INTEGER,
	PRIMARY KEY (monitor_id, bucket)
) WITHOUT ROWID;
CREATE INDEX stats_hourly_bucket ON stats_hourly(bucket);

CREATE TABLE stats_daily (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	bucket     INTEGER NOT NULL,
	up         INTEGER NOT NULL DEFAULT 0,
	down       INTEGER NOT NULL DEFAULT 0,
	ping_sum   INTEGER NOT NULL DEFAULT 0,
	ping_count INTEGER NOT NULL DEFAULT 0,
	ping_min   INTEGER,
	ping_max   INTEGER,
	PRIMARY KEY (monitor_id, bucket)
) WITHOUT ROWID;

CREATE TABLE incidents (
	id          INTEGER PRIMARY KEY,
	monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	started_at  INTEGER NOT NULL,
	resolved_at INTEGER,
	cause       TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX incidents_monitor ON incidents(monitor_id, started_at);
CREATE INDEX incidents_started ON incidents(started_at);

CREATE TABLE notifications (
	id         INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	type       TEXT    NOT NULL,
	config     TEXT    NOT NULL DEFAULT '{}',
	is_default INTEGER NOT NULL DEFAULT 0,
	active     INTEGER NOT NULL DEFAULT 1,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE monitor_notifications (
	monitor_id      INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	PRIMARY KEY (monitor_id, notification_id)
) WITHOUT ROWID;

CREATE TABLE cert_notices (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	not_after  INTEGER NOT NULL,
	days       INTEGER NOT NULL,
	sent_at    INTEGER NOT NULL,
	PRIMARY KEY (monitor_id, not_after, days)
) WITHOUT ROWID;

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
) WITHOUT ROWID;
`,
	// 2: kullanıcı rolleri ve müşteri kısıtı, durum sayfaları, duyurular, işlem kaydı
	2: `
ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin';
ALTER TABLE users ADD COLUMN display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN must_change_password INTEGER NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN all_monitors INTEGER NOT NULL DEFAULT 1;
ALTER TABLE users ADD COLUMN last_login_at INTEGER;

CREATE TABLE user_monitors (
	user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	PRIMARY KEY (user_id, monitor_id)
) WITHOUT ROWID;

CREATE TABLE status_pages (
	id            INTEGER PRIMARY KEY,
	slug          TEXT    NOT NULL UNIQUE,
	title         TEXT    NOT NULL,
	description   TEXT    NOT NULL DEFAULT '',
	footer        TEXT    NOT NULL DEFAULT '',
	sections      TEXT    NOT NULL DEFAULT '[]',
	custom_domain TEXT    UNIQUE,
	password_hash TEXT,
	show_targets  INTEGER NOT NULL DEFAULT 0,
	published     INTEGER NOT NULL DEFAULT 1,
	logo          BLOB,
	logo_type     TEXT    NOT NULL DEFAULT '',
	created_at    INTEGER NOT NULL,
	updated_at    INTEGER NOT NULL
);

CREATE TABLE announcements (
	id         INTEGER PRIMARY KEY,
	page_id    INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
	title      TEXT    NOT NULL,
	body       TEXT    NOT NULL DEFAULT '',
	severity   TEXT    NOT NULL DEFAULT 'info',
	starts_at  INTEGER NOT NULL,
	ends_at    INTEGER,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);
CREATE INDEX announcements_page ON announcements(page_id, starts_at);

CREATE TABLE audit_log (
	id          INTEGER PRIMARY KEY,
	time        INTEGER NOT NULL,
	user_id     INTEGER,
	username    TEXT    NOT NULL DEFAULT '',
	action      TEXT    NOT NULL,
	target_type TEXT    NOT NULL DEFAULT '',
	target_id   INTEGER,
	target_name TEXT    NOT NULL DEFAULT '',
	detail      TEXT    NOT NULL DEFAULT '',
	ip          TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_time ON audit_log(time);
`,
}

// RegisterMigration bir özellik dosyasının migration'ını kaydeder (init içinde).
func RegisterMigration(version int, sql string) {
	if _, dup := migrations[version]; dup {
		panic(fmt.Sprintf("migration %d iki kez kaydedildi", version))
	}
	migrations[version] = sql
}

// pgMigrations PostgreSQL'de SQLite metninin yerine uygulanacak migration'lar
// (RegisterMigrationDialect). Yalnızca iki veritabanında farklı yol gereken
// değişiklikler için (ör. SQLite'ta tabloyu yeniden kurmak, PostgreSQL'de
// ALTER COLUMN).
var pgMigrations = map[int]string{}

// RegisterMigrationDialect SQLite ve PostgreSQL için ayrı metni olan bir
// migration kaydeder. PostgreSQL metni de pgDDL'den geçer.
func RegisterMigrationDialect(version int, sqlite, postgres string) {
	RegisterMigration(version, sqlite)
	pgMigrations[version] = postgres
}

func latestMigration() int {
	n := 0
	for v := range migrations {
		n = max(n, v)
	}
	return n
}

// pgMigrateLockKey PostgreSQL'de migration'ları sıraya sokan danışma
// kilidinin (pg_advisory_lock) ilk anahtarı ("upt1"); ikinci anahtar şema
// adının özetidir, böylece yalnızca aynı şemayı taşıyan örnekler birbirini bekler.
const pgMigrateLockKey int32 = 0x75707431

// pgMigrateLockWait kilidi bekleme sınırı (diğer örnek uzun bir migration
// uyguluyor olabilir).
const pgMigrateLockWait = 10 * time.Minute

func (s *Store) migrate(ctx context.Context) error {
	if s.postgres {
		// Aynı veritabanına aynı anda açılan iki örnek (ör. yuvarlanan
		// dağıtım) migration'ları birlikte uygulamaya çalışmasın.
		unlock, err := s.pgMigrateLock(ctx)
		if err != nil {
			return err
		}
		defer unlock()
	}
	version, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	latest := latestMigration()
	if version > latest {
		return fmt.Errorf("veritabanı sürümü (%d) bu uygulamadan (%d) yeni; eski sürüme geri dönülemez", version, latest)
	}
	// Daha önce eksik olduğu için atlanıp şimdi bulunan migration'lar.
	skipped, err := s.skippedMigrations(ctx)
	if err != nil {
		return err
	}
	var late []int
	for _, v := range skipped {
		if _, ok := migrations[v]; ok && v <= version {
			late = append(late, v)
		}
	}
	if version > 0 && (version < latest || len(late) > 0) {
		// Var olan veritabanı güncellenecek: önce yedek. Yedek alınamazsa
		// migration UYGULANMAZ (yedeksiz şema değişikliği geri alınamaz).
		if err := s.preMigrateBackup(ctx, version, latest); err != nil {
			return err
		}
	}
	for _, v := range late {
		if err := s.applyMigration(ctx, v, false); err != nil {
			return err
		}
	}
	for v := version + 1; v <= latest; v++ {
		if _, ok := migrations[v]; !ok {
			// Paralel dalın migration'ı henüz bu sürümde yok: atlanır ve
			// eklendiğinde uygulanmak üzere not edilir.
			if _, err := s.db.ExecContext(ctx,
				"INSERT INTO migrate_skipped (version) VALUES (?) ON CONFLICT DO NOTHING", v); err != nil {
				return err
			}
			log := s.opts.Log.Warn
			if version == 0 {
				log = s.opts.Log.Info // yeni veritabanı: eksik numara yalnızca not edilir
			}
			log("migration bu sürümde yok, atlandı; eklendiğinde uygulanacak", "version", v)
			continue
		}
		if err := s.applyMigration(ctx, v, true); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration v numaralı migration'ı tek işlemde uygular. setVersion:
// şema sürümü v'ye ilerletilir (atlanıp sonradan uygulanan migration'da
// sürüm zaten ileridedir; yalnızca migrate_skipped kaydı silinir).
func (s *Store) applyMigration(ctx context.Context, v int, setVersion bool) error {
	ddl := migrations[v]
	if pg, ok := pgMigrations[v]; ok && s.postgres {
		ddl = pg
	}
	err := s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM migrate_skipped WHERE version = ?", v); err != nil {
			return err
		}
		if s.postgres {
			// PostgreSQL'in hazırlanmış sorgu kipi çoklu ifade kabul etmez.
			for _, stmt := range splitStatements(pgDDL(ddl)) {
				if _, err := tx.ExecContext(ctx, stmt); err != nil {
					return fmt.Errorf("%w\n%s", err, stmt)
				}
			}
			if !setVersion {
				return nil
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM schema_version"); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO schema_version (version) VALUES (?)", v)
			return err
		}
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return err
		}
		if !setVersion {
			return nil
		}
		// PRAGMA parametre kabul etmez; v bizim kontrolümüzde bir tamsayı.
		_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v))
		return err
	})
	if err != nil {
		return fmt.Errorf("migration %d: %w", v, err)
	}
	s.opts.Log.Info("migration uygulandı", "version", v)
	return nil
}

// skippedMigrations eksik olduğu için atlanmış migration numaraları.
func (s *Store) skippedMigrations(ctx context.Context) ([]int, error) {
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS migrate_skipped (version BIGINT NOT NULL PRIMARY KEY)"); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, "SELECT version FROM migrate_skipped ORDER BY version")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// preMigrateBackup var olan bir veritabanında migration'lardan önce yedek
// alır. SQLite: VACUUM INTO ile (veritabanı açıkken de tutarlı) tek dosya
// <BackupDir>/pre-migrate-v<eski>-to-v<yeni>-<zaman>.db; gece yedeklerinin
// döndürmesine girmez, elle silinir. PostgreSQL: dış veritabanının yedeği
// uygulamanın elinde değildir, yalnızca uyarı yazılır.
func (s *Store) preMigrateBackup(ctx context.Context, from, to int) error {
	if s.postgres {
		s.opts.Log.Warn("PostgreSQL: migration öncesi otomatik yedek ALINMIYOR; gerekirse yedeği veritabanı tarafında alın",
			"eski_surum", from, "yeni_surum", to)
		return nil
	}
	dir := s.opts.BackupDir
	if dir == "" {
		dir = filepath.Join(filepath.Dir(s.path), "backups")
	}
	fail := func(err error) error {
		return fmt.Errorf("migration öncesi yedek alınamadı (%s): %w; migration UYGULANMADI, veritabanı v%d'de kaldı (disk alanını ve klasör izinlerini kontrol edin)", dir, err, from)
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, fmt.Sprintf("pre-migrate-v%d-to-v%d-%s.db", from, to, time.Now().Format("20060102-150405")))
	tmp := path + ".tmp"
	os.Remove(tmp)
	if _, err := s.db.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fail(err)
	}
	s.opts.Log.Info("migration öncesi yedek alındı", "dosya", path, "eski_surum", from, "yeni_surum", to)
	s.prunePreMigrateBackups(dir)
	return nil
}

// keepPreMigrateBackups saklanan en fazla migration öncesi yedek sayısı
// (en yenileri). Eskiler her sürüm geçişinde birikip diski doldurmasın.
const keepPreMigrateBackups = 3

// prunePreMigrateBackups klasördeki pre-migrate-*.db dosyalarından en yeni
// keepPreMigrateBackups tanesini bırakır (ad zaman damgası taşır: ada göre
// sıralama zamana göredir). Hata yalnızca loglanır.
func (s *Store) prunePreMigrateBackups(dir string) {
	names, err := filepath.Glob(filepath.Join(dir, "pre-migrate-v*-to-v*-*.db"))
	if err != nil || len(names) <= keepPreMigrateBackups {
		return
	}
	sort.Strings(names)
	for _, old := range names[:len(names)-keepPreMigrateBackups] {
		if err := os.Remove(old); err != nil {
			s.opts.Log.Warn("eski migration yedeği silinemedi", "dosya", old, "hata", err)
			continue
		}
		s.opts.Log.Info("eski migration yedeği silindi", "dosya", old)
	}
}

// pgMigrateLock migration danışma kilidini ayrı bir bağlantıda alır; dönen
// fonksiyon kilidi bırakır. Kilit oturuma bağlıdır: bırakılamazsa bağlantı
// havuza dönmez, kapatılır (oturum kapanınca kilit de düşer).
func (s *Store) pgMigrateLock(ctx context.Context) (func(), error) {
	lctx, cancel := context.WithTimeout(ctx, pgMigrateLockWait)
	defer cancel()
	c, err := s.db.db.Conn(lctx)
	if err != nil {
		return nil, fmt.Errorf("migration kilidi için bağlantı alınamadı: %w", err)
	}
	const key2 = "hashtext(COALESCE(current_schema(), ''))"
	if _, err := c.ExecContext(lctx, "SELECT pg_advisory_lock($1, "+key2+")", pgMigrateLockKey); err != nil {
		c.Close()
		return nil, fmt.Errorf("migration kilidi alınamadı (başka bir örnek migration uyguluyor olabilir): %w", err)
	}
	return func() {
		uctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.ExecContext(uctx, "SELECT pg_advisory_unlock($1, "+key2+")", pgMigrateLockKey); err != nil {
			c.Raw(func(any) error { return driver.ErrBadConn })
		}
		c.Close()
	}, nil
}

// schemaVersion uygulanmış son migration: SQLite'ta PRAGMA user_version,
// PostgreSQL'de schema_version tablosu.
func (s *Store) schemaVersion(ctx context.Context) (int, error) {
	var v int
	if !s.postgres {
		err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
		return v, err
	}
	if _, err := s.db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS schema_version (version BIGINT NOT NULL)"); err != nil {
		return 0, err
	}
	err := s.db.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&v)
	return v, err
}

var (
	reIntPK     = regexp.MustCompile(`(?i)\bINTEGER\s+PRIMARY\s+KEY\b`)
	reInteger   = regexp.MustCompile(`(?i)\bINTEGER\b`)
	reBlob      = regexp.MustCompile(`(?i)\bBLOB\b`)
	reNoRowID   = regexp.MustCompile(`(?i)\)\s*WITHOUT\s+ROWID`)
	reLineCmt   = regexp.MustCompile(`(?m)--[^\n]*$`)
	pkSentinel  = "\x00PK\x00"
	pgIdentityP = "BIGINT GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY"
)

// pgDDL migration'ların ortak DDL alt kümesini PostgreSQL'e çevirir:
// INTEGER PRIMARY KEY → kimlik sütunu, INTEGER → BIGINT (unix zaman 2038
// sonrası da sığsın), BLOB → BYTEA, WITHOUT ROWID kaldırılır.
func pgDDL(ddl string) string {
	ddl = reLineCmt.ReplaceAllString(ddl, "")
	ddl = reIntPK.ReplaceAllString(ddl, pkSentinel)
	ddl = reInteger.ReplaceAllString(ddl, "BIGINT")
	ddl = strings.ReplaceAll(ddl, pkSentinel, pgIdentityP)
	ddl = reBlob.ReplaceAllString(ddl, "BYTEA")
	return reNoRowID.ReplaceAllString(ddl, ")")
}

// splitStatements DDL'i tırnak dışındaki `;` karakterlerinden böler.
func splitStatements(ddl string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	for _, r := range ddl {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ';':
			if st := strings.TrimSpace(cur.String()); st != "" {
				out = append(out, st)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	if st := strings.TrimSpace(cur.String()); st != "" {
		out = append(out, st)
	}
	return out
}
