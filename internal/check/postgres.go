package check

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	// pgx sürücüsünü database/sql'e "pgx" adıyla kaydeder.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresConfig PostgreSQL kontrol ayarları.
type PostgresConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"` // gizli
	Database string `json:"database"` // boşsa kullanıcı adı ile aynı varsayılır
	Query    string `json:"query"`    // boşsa "SELECT 1"
	Expected string `json:"expected"` // boş değilse ilk satırın ilk sütunu bunu eşleşmeli
	SSLMode  string `json:"sslmode"`  // prefer | disable | require | verify-full
}

// prefer: mümkünse TLS kullanılır, sunucu desteklemiyorsa düz bağlantıya
// düşer (libpq'nun varsayılan davranışı). Sıkı bir doğrulama gerekiyorsa
// verify-full seçilmeli.
var pgSSLModes = map[string]bool{"prefer": true, "disable": true, "require": true, "verify-full": true}

type postgresChecker struct{}

func init() { Register("postgres", postgresChecker{}) }

func (postgresChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c PostgresConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 5432
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	c.Username = strings.TrimSpace(c.Username)
	c.Database = strings.TrimSpace(c.Database)
	c.Query = strings.TrimSpace(c.Query)
	if c.Query == "" {
		c.Query = "SELECT 1"
	}
	c.SSLMode = strings.ToLower(strings.TrimSpace(c.SSLMode))
	if c.SSLMode == "" {
		c.SSLMode = "prefer"
	}
	if !pgSSLModes[c.SSLMode] {
		return nil, invalid("Geçersiz sslmode: %s (prefer, disable, require, verify-full)", c.SSLMode)
	}
	return encode(c), nil
}

func (postgresChecker) Target(raw json.RawMessage) string {
	var c PostgresConfig
	json.Unmarshal(raw, &c)
	return sqlTarget(c.Username, c.Host, c.Port, c.Database)
}

func (postgresChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c PostgresConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	dg := newDiag("postgres", raw, sqlTarget(c.Username, c.Host, c.Port, c.Database)).network(c.Host, c.Port, true)
	defer dg.attach(ctx, &res)

	db, err := sql.Open("pgx", postgresDSN(ctx, c))
	if err != nil {
		dg.fail(ctx, PhaseConfig, err)
		return down("Bağlantı ayarı geçersiz: " + err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	start := time.Now()
	if err := db.PingContext(ctx); err != nil {
		dg.failConn(ctx, err)
		return down(describeErr(ctx, err))
	}
	return runSQLQuery(ctx, dg, db, start, c.Query, c.Expected)
}

// postgresDSN şifreyi güvenli biçimde kodlayan bir bağlantı URL'si üretir.
func postgresDSN(ctx context.Context, c PostgresConfig) string {
	u := &url.URL{Scheme: "postgres", Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port)), Path: "/" + c.Database}
	if c.Username != "" {
		if c.Password != "" {
			u.User = url.UserPassword(c.Username, c.Password)
		} else {
			u.User = url.User(c.Username)
		}
	}
	q := url.Values{}
	q.Set("sslmode", c.SSLMode)
	if secs := int(remainingTimeout(ctx).Seconds()); secs > 0 {
		q.Set("connect_timeout", strconv.Itoa(secs))
	}
	u.RawQuery = q.Encode()
	return u.String()
}
