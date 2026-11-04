// Package pgtestdb testler için geçici PostgreSQL hedefi üretir (store ve
// storetest ortak kullanır; bağımlılığı yok).
//
// Sunucuda TimescaleDB varsa her test kendi VERİTABANINI alır: Timescale
// kataloğu veritabanı başınadır; ortak veritabanında paralel paketlerin
// DROP SCHEMA ... CASCADE (hypertable silme) işlemleri birbirini ve arka
// plan işlerini kilitliyordu (deadlock). Yoksa (düz PG) geçici şema yeterli.
package pgtestdb

import (
	"database/sql"
	"fmt"
	"math/rand/v2"
	"net/url"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Target dsn'deki sunucuda geçici bir veritabanı/şema açar, test bitince siler.
func Target(t testing.TB, dsn string) string {
	t.Helper()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("t_%d_%d", time.Now().UnixNano(), rand.IntN(1_000_000))
	var ts int
	if err := admin.QueryRow("SELECT COUNT(*) FROM pg_available_extensions WHERE name = 'timescaledb'").Scan(&ts); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	if ts > 0 {
		u, err := url.Parse(dsn)
		if err != nil {
			admin.Close()
			t.Fatal(err)
		}
		if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
			admin.Close()
			t.Fatal(err)
		}
		t.Cleanup(func() {
			admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)")
			admin.Close()
		})
		u.Path = "/" + name
		return u.String()
	}
	if _, err := admin.Exec("CREATE SCHEMA " + name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		admin.Exec("DROP SCHEMA " + name + " CASCADE")
		admin.Close()
	})
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return dsn + sep + "search_path=" + name
}
