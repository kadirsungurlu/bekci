package check

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresNormalize(t *testing.T) {
	c, _ := Get("postgres")
	bad := []string{
		`{}`,
		`{"host":"x","port":70000}`,
		`{"host":"x","sslmode":"yolo"}`,
		`{"host":"x","bilinmeyen":1}`,
	}
	for _, b := range bad {
		if _, err := c.Normalize(json.RawMessage(b)); err == nil {
			t.Errorf("%s: doğrulama hatası bekleniyordu", b)
		} else {
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: ValidationError bekleniyordu, %v geldi", b, err)
			}
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"host":"db.local","username":"kadir","password":"gizli","database":"app"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg PostgresConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Port != 5432 || cfg.Query != "SELECT 1" || cfg.SSLMode != "prefer" {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}
	target := c.Target(norm)
	if strings.Contains(target, "gizli") {
		t.Errorf("şifre hedefe sızdı: %s", target)
	}
	if target != "kadir@db.local:5432/app" {
		t.Errorf("hedef yanlış: %s", target)
	}
}

func TestPostgresConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	r := run(t, "postgres", map[string]any{"host": "127.0.0.1", "port": port, "sslmode": "disable"})
	if r.Up || r.Message != "Bağlantı reddedildi" {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

// TestPostgresLive gerçek bir PostgreSQL sunucusuna karşı çalışır.
// UPTIME_IT_POSTGRES="user:pass@host:port" biçiminde beklenir.
func TestPostgresLive(t *testing.T) {
	dsn := os.Getenv("UPTIME_IT_POSTGRES")
	if dsn == "" {
		t.Skip("UPTIME_IT_POSTGRES ayarlı değil, canlı test atlanıyor")
	}
	user, pass, host, port := splitUserPassHostPort(t, dsn)
	c, _ := Get("postgres")
	norm, err := c.Normalize(json.RawMessage(`{"host":"` + host + `","port":` + port + `,"username":"` + user + `","password":"` + pass + `","sslmode":"disable"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := c.Check(ctx, norm)
	if !r.Up || !strings.Contains(r.Message, "Sorgu başarılı") {
		t.Errorf("canlı PostgreSQL kontrolü başarısız: %+v", r)
	}
}
