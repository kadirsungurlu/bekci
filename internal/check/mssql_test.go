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

func TestMSSQLNormalize(t *testing.T) {
	c, _ := Get("mssql")
	bad := []string{
		`{}`,
		`{"host":"x","port":70000}`,
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
	norm, err := c.Normalize(json.RawMessage(`{"host":"db.local","username":"sa","password":"gizli"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg MSSQLConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Port != 1433 || cfg.Query != "SELECT 1" || cfg.Encrypt {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}
	target := c.Target(norm)
	if strings.Contains(target, "gizli") {
		t.Errorf("şifre hedefe sızdı: %s", target)
	}
	if target != "sa@db.local:1433" {
		t.Errorf("hedef yanlış: %s", target)
	}
}

func TestMSSQLConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	r := run(t, "mssql", map[string]any{"host": "127.0.0.1", "port": port})
	if r.Up {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

// TestMSSQLLive gerçek bir MSSQL sunucusuna karşı çalışır.
// UPTIME_IT_MSSQL="user:pass@host:port" biçiminde beklenir. MSSQL 2 GB'ın
// üzerinde bellek istediğinden hazır bir sunucu yoksa atlanır.
func TestMSSQLLive(t *testing.T) {
	dsn := os.Getenv("UPTIME_IT_MSSQL")
	if dsn == "" {
		t.Skip("UPTIME_IT_MSSQL ayarlı değil, canlı test atlanıyor")
	}
	user, pass, host, port := splitUserPassHostPort(t, dsn)
	c, _ := Get("mssql")
	norm, err := c.Normalize(json.RawMessage(`{"host":"` + host + `","port":` + port + `,"username":"` + user + `","password":"` + pass + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := c.Check(ctx, norm)
	if !r.Up || !strings.Contains(r.Message, "Sorgu başarılı") {
		t.Errorf("canlı MSSQL kontrolü başarısız: %+v", r)
	}

	// Diğer ayarlar: beklenen değer, veritabanı, şifreli bağlantı ve hata durumları.
	for _, tc := range []struct {
		name string
		cfg  map[string]any
		up   bool
		msg  string // mesajda geçmesi gereken parça
	}{
		{"beklenen değer tutuyor", map[string]any{"query": "SELECT 40 + 2", "expected": "42"}, true, "Sorgu başarılı"},
		{"beklenen değer tutmuyor", map[string]any{"query": "SELECT 40 + 2", "expected": "43"}, false, "42"},
		{"metin sonucu", map[string]any{"query": "SELECT DB_NAME()", "database": "master", "expected": "master"}, true, "Sorgu başarılı"},
		{"şifreli bağlantı", map[string]any{"encrypt": true}, true, "Sorgu başarılı"},
		{"olmayan veritabanı", map[string]any{"database": "yok_boyle_bir_db"}, false, "Veritabanı açılamadı"},
		{"hatalı sorgu", map[string]any{"query": "SELECT * FROM yok_boyle_tablo"}, false, ""},
		{"yanlış şifre", map[string]any{"password": "yanlis-parola"}, false, "Giriş başarısız"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := map[string]any{"host": host, "port": json.Number(port), "username": user, "password": pass}
			for k, v := range tc.cfg {
				cfg[k] = v
			}
			raw, _ := json.Marshal(cfg)
			norm, err := c.Normalize(raw)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			start := time.Now()
			r := c.Check(ctx, norm)
			t.Logf("up=%v ping=%dms süre=%s mesaj=%q", r.Up, r.PingMs, time.Since(start).Round(time.Millisecond), r.Message)
			if r.Up != tc.up || !strings.Contains(r.Message, tc.msg) {
				t.Errorf("beklenmeyen sonuç: %+v", r)
			}
			if strings.Contains(r.Message, pass) || strings.Contains(r.Message, "yanlis-parola") {
				t.Errorf("mesajda şifre görünüyor: %q", r.Message)
			}
		})
	}
}
