package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"time"
)

// SMTPConfig posta sunucusuna bağlanıp banner/EHLO/(varsa) STARTTLS
// aşamalarını kontrol eder. Kimlik doğrulama gerektirmez.
type SMTPConfig struct {
	Host           string `json:"host"`
	Port           int    `json:"port"`
	Security       string `json:"security"` // none, starttls, tls
	IgnoreTLS      bool   `json:"ignore_tls"`
	ExpectedBanner string `json:"expected_banner"` // boş değilse banner bunu içermeli
}

var smtpSecurity = map[string]bool{"none": true, "starttls": true, "tls": true}

type smtpChecker struct{}

func init() { Register("smtp", smtpChecker{}) }

func (smtpChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c SMTPConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		return nil, invalid("Sunucu adresi gerekli")
	}
	if c.Port == 0 {
		c.Port = 25
	}
	if c.Port < 1 || c.Port > 65535 {
		return nil, invalid("Port 1-65535 arasında olmalı")
	}
	c.Security = strings.ToLower(strings.TrimSpace(c.Security))
	if c.Security == "" {
		c.Security = "none"
	}
	if !smtpSecurity[c.Security] {
		return nil, invalid("Geçersiz güvenlik modu: %s", c.Security)
	}
	c.ExpectedBanner = strings.TrimSpace(c.ExpectedBanner)
	return encode(c), nil
}

func (smtpChecker) Target(raw json.RawMessage) string {
	var c SMTPConfig
	json.Unmarshal(raw, &c)
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

func (smtpChecker) CertExpiryEnabled(json.RawMessage) bool { return true }

func (smtpChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c SMTPConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dg := newDiag("smtp", raw, addr).network(c.Host, c.Port, true)
	defer dg.attach(ctx, &res)

	start := time.Now()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		dg.failDial(ctx, err)
		return down(describeErr(ctx, err))
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}

	tlsCfg := &tls.Config{ServerName: c.Host, InsecureSkipVerify: c.IgnoreTLS}
	var cert *CertInfo

	if c.Security == "tls" {
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			dg.fail(ctx, PhaseTLS, err)
			return down(describeErr(ctx, err))
		}
		cert = certInfoFromState(tlsConn.ConnectionState())
		conn = tlsConn
	}

	tp := textproto.NewConn(conn)
	_, banner, err := tp.ReadResponse(220)
	dg.banner = banner
	if err != nil {
		dg.fail(ctx, PhaseGreeting, err)
		return down("Banner okunamadı: " + describeErr(ctx, err))
	}
	if c.ExpectedBanner != "" && !strings.Contains(strings.ToLower(banner), strings.ToLower(c.ExpectedBanner)) {
		dg.failClass(PhaseGreeting, ClassMismatch)
		return down("Banner beklenen metni içermiyor: " + truncate(banner, 120))
	}

	if _, _, err := ehlo(tp); err != nil {
		dg.fail(ctx, PhaseEHLO, err)
		return down("EHLO başarısız: " + describeErr(ctx, err))
	}

	if c.Security == "starttls" {
		id, err := tp.Cmd("STARTTLS")
		if err != nil {
			dg.fail(ctx, PhaseSTARTTLS, err)
			return down("STARTTLS gönderilemedi: " + err.Error())
		}
		tp.StartResponse(id)
		_, _, err = tp.ReadResponse(220)
		tp.EndResponse(id)
		if err != nil {
			dg.fail(ctx, PhaseSTARTTLS, err)
			return down("STARTTLS reddedildi: " + describeErr(ctx, err))
		}
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			dg.fail(ctx, PhaseTLS, err)
			return down("SSL handshake hatası: " + describeErr(ctx, err))
		}
		cert = certInfoFromState(tlsConn.ConnectionState())
		conn = tlsConn
		tp = textproto.NewConn(conn)
		// RFC 3207: STARTTLS sonrası oturum sıfırlanır, tekrar EHLO gerekir.
		if _, _, err := ehlo(tp); err != nil {
			dg.fail(ctx, PhaseEHLO, err)
			return down("STARTTLS sonrası EHLO başarısız: " + describeErr(ctx, err))
		}
	}

	id, err := tp.Cmd("QUIT")
	if err == nil {
		tp.StartResponse(id)
		tp.ReadResponse(221)
		tp.EndResponse(id)
	}

	ping := msSince(start)
	msg := "Sunucu yanıt verdi"
	if banner != "" {
		msg = truncate(banner, 160)
	}
	return Result{Up: true, PingMs: ping, Message: msg, Cert: cert}
}

// ehlo EHLO komutunu gönderir, başarısız olursa HELO'ya düşer (bazı eski
// sunucular EHLO'yu desteklemez).
func ehlo(tp *textproto.Conn) (int, string, error) {
	id, err := tp.Cmd("EHLO bekci")
	if err != nil {
		return 0, "", err
	}
	tp.StartResponse(id)
	code, msg, err := tp.ReadResponse(250)
	tp.EndResponse(id)
	if err == nil {
		return code, msg, nil
	}
	id, err = tp.Cmd("HELO bekci")
	if err != nil {
		return 0, "", err
	}
	tp.StartResponse(id)
	code, msg, err = tp.ReadResponse(250)
	tp.EndResponse(id)
	return code, msg, err
}
