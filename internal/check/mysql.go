package check

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

// MySQLConfig MySQL/MariaDB kontrol ayarları.
type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"` // gizli
	Database string `json:"database"` // boşsa varsayılan şema kullanılır
	Query    string `json:"query"`    // boşsa "SELECT 1"
	Expected string `json:"expected"` // boş değilse ilk satırın ilk sütunu bunu eşleşmeli
	TLS      string `json:"tls"`      // false | true | skip-verify
}

var mysqlTLSModes = map[string]bool{"false": true, "true": true, "skip-verify": true}

type mysqlChecker struct{}

func init() { Register("mysql", mysqlChecker{}) }

func (mysqlChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c MySQLConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 3306
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
	c.TLS = strings.ToLower(strings.TrimSpace(c.TLS))
	if c.TLS == "" {
		c.TLS = "false"
	}
	if !mysqlTLSModes[c.TLS] {
		return nil, invalid("Geçersiz TLS modu: %s (false, true, skip-verify)", c.TLS)
	}
	return encode(c), nil
}

func (mysqlChecker) Target(raw json.RawMessage) string {
	var c MySQLConfig
	json.Unmarshal(raw, &c)
	return sqlTarget(c.Username, c.Host, c.Port, c.Database)
}

func (mysqlChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c MySQLConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	dg := newDiag("mysql", raw, sqlTarget(c.Username, c.Host, c.Port, c.Database)).network(c.Host, c.Port, true)
	defer dg.attach(ctx, &res)

	cfg := mysql.NewConfig()
	cfg.Net = "tcp"
	cfg.Addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	cfg.User = c.Username
	cfg.Passwd = c.Password
	cfg.DBName = c.Database
	cfg.TLSConfig = c.TLS
	cfg.Timeout = remainingTimeout(ctx)

	db, err := sql.Open("mysql", cfg.FormatDSN())
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
