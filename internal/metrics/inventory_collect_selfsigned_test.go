package metrics

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func caPEM(t *testing.T) []byte {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "yerel"},
		NotBefore: time.Now(), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestParseLeafSelfSignedCA(t *testing.T) {
	one := caPEM(t)
	if parseLeaf(one) == nil {
		t.Fatal("tek kendinden imzalı sertifika sayılmalı")
	}
	if parseLeaf(append(append([]byte{}, one...), caPEM(t)...)) != nil {
		t.Fatal("iki CA'lı zincir dosyası atlanmalı")
	}
}
