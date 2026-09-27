package check

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
)

// fakeSMTPServer basit bir SMTP sunucusu taklit eder: banner, EHLO/HELO ve
// isteğe bağlı STARTTLS destekler.
func fakeSMTPServer(t *testing.T, starttls bool, cert tls.Certificate) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSMTP(conn, starttls, cert)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func handleFakeSMTP(conn net.Conn, starttls bool, cert tls.Certificate) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	tp.PrintfLine("220 fake.smtp.test hazır")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			tp.PrintfLine("250-fake.smtp.test")
			tp.PrintfLine("250 STARTTLS")
		case strings.HasPrefix(upper, "STARTTLS"):
			if !starttls {
				tp.PrintfLine("502 desteklenmiyor")
				continue
			}
			tp.PrintfLine("220 devam edin")
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			if err := tlsConn.Handshake(); err != nil {
				return
			}
			conn = tlsConn
			tp = textproto.NewConn(conn)
		case strings.HasPrefix(upper, "QUIT"):
			tp.PrintfLine("221 hoşça kal")
			return
		default:
			tp.PrintfLine("500 anlaşılamadı")
		}
	}
}

// fakeSMTPServerTLS doğrudan TLS ile (banner TLS handshake sonrası gelir)
// bağlanan bir SMTP sunucusunu taklit eder ("security":"tls" modu için).
func fakeSMTPServerTLS(t *testing.T, cert tls.Certificate) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			tlsConn := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}})
			go handleFakeSMTP(tlsConn, false, tls.Certificate{})
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func TestSMTP(t *testing.T) {
	addr, closeFn := fakeSMTPServer(t, false, tls.Certificate{})
	defer closeFn()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	if r := run(t, "smtp", map[string]any{"host": host, "port": port}); !r.Up {
		t.Errorf("smtp temel: %+v", r)
	}
	if r := run(t, "smtp", map[string]any{"host": host, "port": port, "expected_banner": "fake.smtp.test"}); !r.Up {
		t.Errorf("banner eşleşmeli: %+v", r)
	}
	if r := run(t, "smtp", map[string]any{"host": host, "port": port, "expected_banner": "hic-boyle-bir-sey"}); r.Up {
		t.Errorf("banner eşleşmemeli: %+v", r)
	}

	freeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freeAddr := freeLis.Addr().String()
	freeLis.Close()
	fh, fp, _ := net.SplitHostPort(freeAddr)
	fpi, _ := strconv.Atoi(fp)
	if r := run(t, "smtp", map[string]any{"host": fh, "port": fpi}); r.Up {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

func TestSMTPStartTLS(t *testing.T) {
	cert := generateTestCert(t)
	addr, closeFn := fakeSMTPServer(t, true, cert)
	defer closeFn()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	r := run(t, "smtp", map[string]any{"host": host, "port": port, "security": "starttls", "ignore_tls": true})
	if !r.Up || r.Cert == nil || r.Cert.NotAfter.IsZero() {
		t.Errorf("starttls sertifika bekleniyordu: %+v", r)
	}
}

func TestSMTPDirectTLS(t *testing.T) {
	cert := generateTestCert(t)
	addr, closeFn := fakeSMTPServerTLS(t, cert)
	defer closeFn()
	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	r := run(t, "smtp", map[string]any{"host": host, "port": port, "security": "tls", "ignore_tls": true})
	if !r.Up || r.Cert == nil {
		t.Errorf("doğrudan tls sertifika bekleniyordu: %+v", r)
	}
}

func TestSMTPNormalize(t *testing.T) {
	c, _ := Get("smtp")
	bad := []string{`{}`, `{"host":""}`, `{"host":"x","port":-1}`, `{"host":"x","security":"yanlis"}`, `{"host":"x","bilinmeyen":1}`}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"host":" mail.kadir.app "}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg SMTPConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Host != "mail.kadir.app" || cfg.Port != 25 || cfg.Security != "none" {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}
}
