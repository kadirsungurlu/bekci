package metrics

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"math/big"
	"strings"
	"testing"
	"time"
)

func winDER(t *testing.T, cn string, isCA bool, eku ...x509.ExtKeyUsage) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		DNSNames: []string{cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(10 * 24 * time.Hour),
		IsCA: isCA, BasicConstraintsValid: true, ExtKeyUsage: eku}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return der
}

func thumbOf(der []byte) string {
	s := sha1.Sum(der)
	return hex.EncodeToString(s[:])
}

func TestWinStoreInv(t *testing.T) {
	web := winDER(t, "web.example", false, x509.ExtKeyUsageServerAuth)
	plain := winDER(t, "plain.example", false)
	client := winDER(t, "mdm-device", false, x509.ExtKeyUsageClientAuth)
	ca := winDER(t, "Root CA", true)
	sql := winDER(t, "sql.example", false, x509.ExtKeyUsageClientAuth) // bağlıysa EKU'ya bakılmaz
	certs := []winStoreCert{
		{"My", web}, {"My", plain}, {"My", client}, {"My", ca}, {"My", sql},
		{"WebHosting", web}, // iki depoda aynı sertifika: bir kez
		{"My", []byte("bozuk")},
	}
	// Kayıt defterindeki parmak izinde büyük harf, boşluk ve görünmez işaret olabilir.
	sqlThumb := "‎" + strings.ToUpper(thumbOf(sql)[:10]) + " " + thumbOf(sql)[10:]
	binds := []winBinding{
		{Thumb: thumbOf(web), Use: "IIS 0.0.0.0:443"},
		{Thumb: thumbOf(web), Use: "IIS web.example:443"},
		{Thumb: thumbOf(web), Use: "IIS 0.0.0.0:443"},
		{Thumb: sqlThumb, Use: "MSSQL SQLEXPRESS"},
		{Thumb: "", Use: "IIS boş"},
	}
	got := winStoreInv(certs, binds)
	loc := map[string]string{}
	for _, c := range got {
		if c.Source != "store" || len(c.Fingerprint) != 64 || c.NotAfter == 0 {
			t.Fatalf("kayıt: %+v", c)
		}
		loc[c.Subject] = c.Location
	}
	if len(got) != 3 {
		t.Fatalf("3 sertifika beklenirdi: %+v", loc)
	}
	want := map[string]string{
		"web.example":   `LocalMachine\My\` + strings.ToUpper(thumbOf(web)) + " (IIS 0.0.0.0:443, IIS web.example:443)",
		"plain.example": `LocalMachine\My\` + strings.ToUpper(thumbOf(plain)),
		"sql.example":   `LocalMachine\My\` + strings.ToUpper(thumbOf(sql)) + " (MSSQL SQLEXPRESS)",
	}
	for k, v := range want {
		if loc[k] != v {
			t.Errorf("%s: %q, beklenen %q", k, loc[k], v)
		}
	}
}

func TestSockaddrString(t *testing.T) {
	v4 := []byte{2, 0, 0x01, 0xbb, 10, 0, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0}
	if s := sockaddrString(v4); s != "10.0.0.5:443" {
		t.Fatal(s)
	}
	v6 := make([]byte, 28)
	v6[0], v6[2], v6[3] = 23, 0x20, 0xfb // 8443
	if s := sockaddrString(v6); s != "[::]:8443" {
		t.Fatal(s)
	}
	if sockaddrString([]byte{9, 0, 0, 0, 0, 0, 0, 0}) != "" || sockaddrString(nil) != "" {
		t.Fatal("bilinmeyen aile")
	}
}
