package check

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	mssql "github.com/microsoft/go-mssqldb"
)

// MSSQLConfig Microsoft SQL Server kontrol ayarları.
type MSSQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"` // gizli
	Database string `json:"database"`
	Query    string `json:"query"`    // boşsa "SELECT 1"
	Expected string `json:"expected"` // boş değilse ilk satırın ilk sütunu bunu eşleşmeli
	Encrypt  bool   `json:"encrypt"`  // true ise bağlantı şifrelenir (sertifika doğrulanmaz)
}

type mssqlChecker struct{}

func init() { Register("mssql", mssqlChecker{}) }

func (mssqlChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c MSSQLConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 1433
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
	return encode(c), nil
}

func (mssqlChecker) Target(raw json.RawMessage) string {
	var c MSSQLConfig
	json.Unmarshal(raw, &c)
	return sqlTarget(c.Username, c.Host, c.Port, c.Database)
}

func (mssqlChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c MSSQLConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}

	db, err := sql.Open("sqlserver", mssqlDSN(ctx, c))
	if err != nil {
		return down("Bağlantı ayarı geçersiz: " + err.Error())
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	start := time.Now()
	if err := db.PingContext(ctx); err != nil {
		return down(mssqlConnErr(ctx, err))
	}
	return runSQLQuery(ctx, db, start, c.Query, c.Expected)
}

// mssqlConnErr sık görülen bağlantı hatalarını Türkçe açıklar; SQL Server'ın
// hata numarası parantez içinde kalır (arama için).
func mssqlConnErr(ctx context.Context, err error) string {
	var me mssql.Error
	if errors.As(err, &me) {
		switch me.Number {
		case 18456:
			return fmt.Sprintf("Giriş başarısız: kullanıcı adı veya şifre yanlış ya da SQL Server kimlik doğrulaması kapalı (%d)", me.Number)
		case 4060, 4063:
			return fmt.Sprintf("Veritabanı açılamadı: veritabanı yok ya da kullanıcının erişim izni yok (%d)", me.Number)
		}
	}
	return describeErr(ctx, err)
}

// mssqlDSN şifreyi güvenli biçimde kodlayan bir bağlantı URL'si üretir.
// encrypt=true seçilirse bağlantı şifrelenir ama sunucu sertifikası
// doğrulanmaz (iç ağda izlenen sunucular çoğunlukla kendinden imzalı
// sertifika kullanır); dışa açık bir sunucu için ayrı bir CA doğrulaması
// gerekiyorsa bu kontrol tipi yeterli değildir.
func mssqlDSN(ctx context.Context, c MSSQLConfig) string {
	u := &url.URL{Scheme: "sqlserver", Host: net.JoinHostPort(c.Host, strconv.Itoa(c.Port))}
	if c.Username != "" {
		if c.Password != "" {
			u.User = url.UserPassword(c.Username, c.Password)
		} else {
			u.User = url.User(c.Username)
		}
	}
	q := url.Values{}
	if c.Database != "" {
		q.Set("database", c.Database)
	}
	if c.Encrypt {
		q.Set("encrypt", "true")
		q.Set("trustservercertificate", "true")
	} else {
		q.Set("encrypt", "disable")
	}
	if secs := int(remainingTimeout(ctx).Seconds()); secs > 0 {
		q.Set("dial timeout", strconv.Itoa(secs))
	}
	u.RawQuery = q.Encode()
	return u.String()
}
