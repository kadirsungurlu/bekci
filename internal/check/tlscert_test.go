package check

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// generateTestCert testler için kısa ömürlü, kendinden imzalı bir sertifika
// üretir (smtp, websocket ve tlscert testleri paylaşır).
func generateTestCert(t *testing.T) tls.Certificate {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return cert
}

func TestTLSCert(t *testing.T) {
	cert := generateTestCert(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				tlsConn := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}})
				defer tlsConn.Close()
				if err := tlsConn.Handshake(); err != nil {
					return
				}
				io.Copy(io.Discard, tlsConn)
			}(c)
		}
	}()
	defer ln.Close()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)

	if r := run(t, "tlscert", map[string]any{"host": host, "port": port}); r.Up || !strings.Contains(r.Message, "SSL") {
		t.Errorf("doğrulama hatası bekleniyordu: %+v", r)
	}
	if r := run(t, "tlscert", map[string]any{"host": host, "port": port, "ignore_tls": true}); !r.Up || r.Cert == nil || r.Cert.NotAfter.IsZero() {
		t.Errorf("sertifika bilgisi bekleniyordu: %+v", r)
	}

	freeLis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	freeAddr := freeLis.Addr().String()
	freeLis.Close()
	fh, fp, _ := net.SplitHostPort(freeAddr)
	fpi, _ := strconv.Atoi(fp)
	if r := run(t, "tlscert", map[string]any{"host": fh, "port": fpi}); r.Up {
		t.Errorf("bağlantı reddi bekleniyordu: %+v", r)
	}
}

func TestTLSCertNormalize(t *testing.T) {
	c, _ := Get("tlscert")
	bad := []string{`{}`, `{"host":""}`, `{"host":"x","port":0}`, `{"host":"x","port":1,"bilinmeyen":1}`}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
}
