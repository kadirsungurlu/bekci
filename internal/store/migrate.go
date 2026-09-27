package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// migrations sürüm numarasına göre sırayla uygulanır; uygulanan son sürüm
// PRAGMA user_version'da tutulur. Var olan bir migration asla değiştirilmez,
// yenisi eklenir. Özellik dosyaları kendi migration'larını init() içinde
// RegisterMigration ile ekleyebilir (paralel geliştirmede çakışma olmasın diye);
// numaralar çakışamaz ve arada boşluk bırakılamaz.
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

func latestMigration() int {
	n := 0
	for v := range migrations {
		n = max(n, v)
	}
	return n
}

func (s *Store) migrate(ctx context.Context) error {
	version, err := s.schemaVersion(ctx)
	if err != nil {
		return err
	}
	latest := latestMigration()
	if version > latest {
		return fmt.Errorf("veritabanı sürümü (%d) bu uygulamadan (%d) yeni; eski sürüme geri dönülemez", version, latest)
	}
	for v := version + 1; v <= latest; v++ {
		ddl, ok := migrations[v]
		if !ok {
			return fmt.Errorf("migration %d eksik", v)
		}
		err := s.tx(ctx, func(tx *Tx) error {
			if s.postgres {
				// PostgreSQL'in hazırlanmış sorgu kipi çoklu ifade kabul etmez.
				for _, stmt := range splitStatements(pgDDL(ddl)) {
					if _, err := tx.ExecContext(ctx, stmt); err != nil {
						return fmt.Errorf("%w\n%s", err, stmt)
					}
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
			// PRAGMA parametre kabul etmez; v bizim kontrolümüzde bir tamsayı.
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", v))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", v, err)
		}
	}
	return nil
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
