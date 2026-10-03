package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"strconv"
	"strings"
	"time"
)

// TLSCertConfig salt sertifika kontrolü ayarı: HTTP olmayan TLS servisleri
// (posta sunucuları, veritabanları vb.) için sertifikanın geçerliliğini ve
// bitiş tarihini izler.
type TLSCertConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	IgnoreTLS  bool   `json:"ignore_tls"`  // sertifika doğrulaması yapılmadan da bağlanılsın mı
	ServerName string `json:"server_name"` // SNI; boşsa Host kullanılır
}

type tlsCertChecker struct{}

func init() { Register("tlscert", tlsCertChecker{}) }

func (tlsCertChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c TLSCertConfig
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
	c.ServerName = strings.TrimSpace(c.ServerName)
	return encode(c), nil
}

func (tlsCertChecker) Target(raw json.RawMessage) string {
	var c TLSCertConfig
	json.Unmarshal(raw, &c)
	return net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// CertExpiryEnabled bu tip için sertifika kontrolünün tüm amacı budur; her
// zaman açık kabul edilir (arayüzü uygulamak zorunlu değil ama açıkça belirtir).
func (tlsCertChecker) CertExpiryEnabled(json.RawMessage) bool { return true }

func (tlsCertChecker) Check(ctx context.Context, raw json.RawMessage) (res Result) {
	var c TLSCertConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	dg := newDiag("tlscert", raw, addr).network(c.Host, c.Port, true)
	defer dg.attach(ctx, &res)
	serverName := c.ServerName
	if serverName == "" {
		serverName = c.Host
	}
	tlsCfg := &tls.Config{InsecureSkipVerify: c.IgnoreTLS, ServerName: serverName}

	start := time.Now()
	cert, err := dialTLSCert(ctx, "tcp", addr, tlsCfg)
	if err != nil {
		dg.failTLSDial(ctx, err)
		return down(describeErr(ctx, err))
	}
	ping := msSince(start)
	if cert == nil {
		dg.failClass(PhaseTLS, ClassTLS)
		return down("Sunucu sertifika sunmadı")
	}
	return Result{Up: true, PingMs: ping, Message: "Sertifika geçerli, bitiş: " + cert.NotAfter.Format("02.01.2006"), Cert: cert}
}

// dialTLSCert host:port'a TLS ile bağlanır ve sunucunun sertifika bilgisini
// döner. Bağlantı hemen kapatılır; sadece handshake ve sertifika bilgisi için
// kullanılır. grpc, smtp, websocket gibi tipler de bunu paylaşır.
func dialTLSCert(ctx context.Context, network, addr string, tlsCfg *tls.Config) (*CertInfo, error) {
	d := &tls.Dialer{Config: tlsCfg}
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	tlsConn, ok := conn.(*tls.Conn)
	if !ok {
		return nil, nil
	}
	state := tlsConn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, nil
	}
	leaf := state.PeerCertificates[0]
	return &CertInfo{NotAfter: leaf.NotAfter, Issuer: issuerName(leaf.Issuer.Organization, leaf.Issuer.CommonName), Subject: leaf.Subject.CommonName}, nil
}

// certInfoFromState var olan bir TLS bağlantısından (ör. SMTP STARTTLS
// sonrası) sertifika bilgisini çıkarır.
func certInfoFromState(state tls.ConnectionState) *CertInfo {
	if len(state.PeerCertificates) == 0 {
		return nil
	}
	leaf := state.PeerCertificates[0]
	return &CertInfo{NotAfter: leaf.NotAfter, Issuer: issuerName(leaf.Issuer.Organization, leaf.Issuer.CommonName), Subject: leaf.Subject.CommonName}
}

// TLSCertConfigOf kayıtlı ayarı çözer (alan adı bitiş sorgusu için).
func TLSCertConfigOf(raw json.RawMessage) TLSCertConfig {
	var c TLSCertConfig
	json.Unmarshal(raw, &c)
	return c
}
