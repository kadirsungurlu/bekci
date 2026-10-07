package agentupdate

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// parsePKCS8 "BEGIN PRIVATE KEY" bloğundaki Ed25519 anahtarını çözer
// (openssl genpkey -algorithm ed25519 çıktısı).
func parsePKCS8(data []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("PEM bloğu bulunamadı")
	}
	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("beklenmeyen PEM türü %q (PKCS#8 \"PRIVATE KEY\" olmalı)", block.Type)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("PKCS#8 çözülemedi: %w", err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("anahtar Ed25519 değil (%T)", k)
	}
	return priv, nil
}

// KeyToPEM özel anahtarı PKCS#8 PEM olarak kodlar (anahtar üretimi).
func KeyToPEM(priv ed25519.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}
