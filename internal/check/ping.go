package check

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
	"time"

	probing "github.com/prometheus-community/pro-bing"
)

type PingConfig struct {
	Host  string `json:"host"`
	Count int    `json:"count"`
}

type pingChecker struct{}

func init() { Register("ping", pingChecker{}) }

func (pingChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c PingConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Count == 0 {
		c.Count = 3
	}
	if c.Count < 1 || c.Count > 10 {
		return nil, invalid("Paket sayısı 1-10 arasında olmalı")
	}
	return encode(c), nil
}

func (pingChecker) Target(raw json.RawMessage) string {
	var c PingConfig
	json.Unmarshal(raw, &c)
	return c.Host
}

func (pingChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c PingConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	p, err := probing.NewPinger(c.Host)
	if err != nil {
		return down(describeErr(ctx, err))
	}
	// Yetkisiz (UDP tabanlı) ICMP: root gerektirmez. Docker'ın varsayılan
	// net.ipv4.ping_group_range ayarı buna izin verir. Windows'ta yetkisiz
	// ICMP yoktur; ajan hizmeti LocalSystem olarak çalıştığı için ham soket açılır.
	p.SetPrivileged(runtime.GOOS == "windows")
	p.Count = c.Count
	p.Interval = 250 * time.Millisecond
	if dl, ok := ctx.Deadline(); ok {
		p.Timeout = time.Until(dl)
	}
	if err := p.RunWithContext(ctx); err != nil && ctx.Err() == nil {
		return down("Ping gönderilemedi: " + err.Error())
	}
	st := p.Statistics()
	if st.PacketsRecv == 0 {
		return down(fmt.Sprintf("Yanıt yok (%d paketin hiçbiri dönmedi)", st.PacketsSent))
	}
	ms := st.AvgRtt.Milliseconds()
	if ms < 1 {
		ms = 1
	}
	msg := fmt.Sprintf("%d/%d paket", st.PacketsRecv, st.PacketsSent)
	if st.PacketLoss > 0 {
		msg += fmt.Sprintf(", %%%.0f kayıp", st.PacketLoss)
	}
	return Result{Up: true, PingMs: ms, Message: msg}
}
