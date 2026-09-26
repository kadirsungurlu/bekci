package store

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations sırayla uygulanır; uygulanan son sürüm PRAGMA user_version'da
// tutulur. Var olan bir migration asla değiştirilmez, yenisi eklenir.
var migrations = []string{
	// 1: ilk şema
	`
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
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("veritabanı sürümü (%d) bu uygulamadan (%d) yeni; eski sürüme geri dönülemez", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		err := s.tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
				return err
			}
			// PRAGMA parametre kabul etmez; i bizim kontrolümüzde bir tamsayı.
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}
