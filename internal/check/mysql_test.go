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

func TestMySQLNormalize(t *testing.T) {
	c, _ := Get("mysql")
	bad := []string{
		`{}`,
		`{"host":"x","port":70000}`,
		`{"host":"x","tls":"maybe"}`,
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
	norm, err := c.Normalize(json.RawMessage(`{"host":" db.local ","username":"kadir","password":"gizli"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg MySQLConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Port != 3306 || cfg.Query != "SELECT 1" || cfg.TLS != "false" || cfg.Host != "db.local" {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}
	target := c.Target(norm)
	if strings.Contains(target, "gizli") {
		t.Errorf("şifre hedefe sızdı: %s", target)
	}
	if target != "kadir@db.local:3306" {
		t.Errorf("hedef yanlış: %s", target)
	}
}

func TestMySQLConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	r := run(t, "mysql", map[string]any{"host": "127.0.0.1", "port": port})
	if r.Up || r.Message != "Bağlantı reddedildi" {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

// TestMySQLLive gerçek bir MariaDB/MySQL sunucusuna karşı çalışır.
// UPTIME_IT_MYSQL="user:pass@host:port" biçiminde beklenir.
func TestMySQLLive(t *testing.T) {
	dsn := os.Getenv("UPTIME_IT_MYSQL")
	if dsn == "" {
		t.Skip("UPTIME_IT_MYSQL ayarlı değil, canlı test atlanıyor")
	}
	user, pass, host, port := splitUserPassHostPort(t, dsn)
	c, _ := Get("mysql")
	norm, err := c.Normalize(json.RawMessage(`{"host":"` + host + `","port":` + port + `,"username":"` + user + `","password":"` + pass + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := c.Check(ctx, norm)
	if !r.Up || !strings.Contains(r.Message, "Sorgu başarılı") {
		t.Errorf("canlı MySQL kontrolü başarısız: %+v", r)
	}
}
