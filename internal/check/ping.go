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

func (pingChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c PingConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	// Tanı: ping istatistikleri zaten elde; ek ağ işi yapılmaz.
	dg := newDiag("ping", raw, c.Host)
	defer dg.attach(ctx, &res)
	p, err := probing.NewPinger(c.Host)
	if err != nil {
		dg.fail(ctx, PhaseDNS, err)
		return down(describeErr(ctx, err))
	}
	dp := &DiagPing{Target: p.IPAddr().String()}
	dg.ping = dp
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
		dg.fail(ctx, PhasePing, err)
		dp.RawError, dp.Error = err.Error(), "failed"
		if pingUnavailable(err) {
			dp.Error = "unavailable"
		}
		return down("Ping gönderilemedi: " + err.Error())
	}
	st := p.Statistics()
	fillPing(dp, st)
	if st.PacketsRecv == 0 {
		dg.failClass(PhasePing, ClassTimeout)
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
