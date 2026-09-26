package check

import (
	"context"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"
)

type TCPConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type tcpChecker struct{}

func init() { Register("tcp", tcpChecker{}) }

func (tcpChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c TCPConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	return encode(c), nil
}

func (tcpChecker) Target(raw json.RawMessage) string {
	var c TCPConfig
	json.Unmarshal(raw, &c)
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (tcpChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c TCPConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(c.Host, strconv.Itoa(c.Port)))
	if err != nil {
		return down(describeErr(ctx, err))
	}
	ping := msSince(start)
	conn.Close()
	return Result{Up: true, PingMs: ping, Message: "Port açık"}
}
