package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisConfig Redis kontrol ayarları.
type RedisConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"` // gizli
	DB       int    `json:"db"`       // veritabanı indeksi
	TLS      bool   `json:"tls"`
	Key      string `json:"key"`      // boşsa sadece PING yapılır
	Expected string `json:"expected"` // boş değilse anahtarın değeri bununla eşleşmeli
}

type redisChecker struct{}

func init() {
	Register("redis", redisChecker{})
	// go-redis, bağlantı denemeleri başarısız olunca kendi logger'ıyla
	// stderr'e yazar; bu paket zaten Türkçe bir sonuç mesajı ürettiğinden
	// çift/gürültülü log oluşmasın diye susturuyoruz.
	redis.SetLogger(noopRedisLogger{})
}

type noopRedisLogger struct{}

func (noopRedisLogger) Printf(context.Context, string, ...any) {}

func (redisChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c RedisConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 6379
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	if c.DB < 0 || c.DB > 15 {
		return nil, invalid("Veritabanı indeksi 0-15 arasında olmalı")
	}
	c.Username = strings.TrimSpace(c.Username)
	c.Key = strings.TrimSpace(c.Key)
	if c.Key == "" {
		c.Expected = ""
	}
	return encode(c), nil
}

func (redisChecker) Target(raw json.RawMessage) string {
	var c RedisConfig
	json.Unmarshal(raw, &c)
	target := sqlTarget(c.Username, c.Host, c.Port, "")
	if c.DB != 0 {
		target += fmt.Sprintf("/%d", c.DB)
	}
	return target
}

func (redisChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c RedisConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	dg := newDiag("redis", raw, redisChecker{}.Target(raw)).network(c.Host, c.Port, true)
	defer dg.attach(ctx, &res)

	opts := &redis.Options{
		Addr:         net.JoinHostPort(c.Host, strconv.Itoa(c.Port)),
		Username:     c.Username,
		Password:     c.Password,
		DB:           c.DB,
		DialTimeout:  remainingTimeout(ctx),
		ReadTimeout:  remainingTimeout(ctx),
		WriteTimeout: remainingTimeout(ctx),
	}
	if c.TLS {
		opts.TLSConfig = &tls.Config{ServerName: c.Host}
	}
	client := redis.NewClient(opts)
	defer client.Close()

	start := time.Now()
	if _, err := client.Ping(ctx).Result(); err != nil {
		dg.failConn(ctx, err)
		return down(describeErr(ctx, err))
	}
	ping := msSince(start)

	if c.Key == "" {
		return Result{Up: true, PingMs: ping, Message: "PONG"}
	}

	val, err := client.Get(ctx, c.Key).Result()
	if err == redis.Nil {
		dg.failClass(PhaseQuery, ClassNotFound)
		return down(fmt.Sprintf("Anahtar bulunamadı: %s", c.Key))
	}
	if err != nil {
		dg.fail(ctx, PhaseQuery, err)
		return down(describeErr(ctx, err))
	}
	if c.Expected != "" && val != c.Expected {
		dg.failClass(PhaseResponse, ClassMismatch)
		return down(fmt.Sprintf("Beklenmeyen değer: %q (beklenen: %q)", truncate(val, 80), c.Expected))
	}
	return Result{Up: true, PingMs: ping, Message: fmt.Sprintf("Anahtar bulundu: %s", c.Key)}
}
