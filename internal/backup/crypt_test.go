package backup

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	plain := []byte(`{"format":"bekci","version":1,"monitors":[]}`)
	enc, err := Encrypt(plain, "cok-gizli-parola")
	if err != nil {
		t.Fatal(err)
	}
	var env Envelope
	if err := json.Unmarshal(enc, &env); err != nil || env.Format != EncryptedFormat {
		t.Fatalf("zarf biçimi: %q %v", env.Format, err)
	}
	if !IsEncrypted(enc) {
		t.Fatal("IsEncrypted yeni zarfı tanımalı")
	}
	if bytes.Contains(enc, []byte("monitors")) {
		t.Fatal("içerik açık metinde")
	}
	got, err := Decrypt(enc, "cok-gizli-parola")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("açma: %v %s", err, got)
	}
	if _, err := Decrypt(enc, "yanlis-parola-1"); err != ErrWrongPassword {
		t.Fatalf("yanlış parola: %v", err)
	}
	if _, err := Encrypt(plain, "kisa"); err == nil {
		t.Fatal("kısa parola kabul edilmemeli")
	}
}

// Eski adla (uptime-kadir-encrypted) yazılmış şifreli yedekler açılmaya devam eder.
func TestDecryptLegacyEnvelope(t *testing.T) {
	plain := []byte(`{"format":"uptime-kadir","version":1}`)
	enc, err := encryptAs(plain, "eski-yedek-parolasi", LegacyEncryptedFormat)
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncrypted(enc) {
		t.Fatal("IsEncrypted eski zarfı tanımalı")
	}
	got, err := Decrypt(enc, "eski-yedek-parolasi")
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatalf("eski zarf açılamadı: %v %s", err, got)
	}
	// Biçim adı AEAD ek verisidir: zarfın adı değiştirilirse açılmaz.
	tampered := bytes.Replace(enc, []byte(LegacyEncryptedFormat), []byte(EncryptedFormat), 1)
	if _, err := Decrypt(tampered, "eski-yedek-parolasi"); err != ErrWrongPassword {
		t.Fatalf("değiştirilmiş zarf: %v", err)
	}
	for _, f := range []string{Format, LegacyFormat} {
		if !IsFormat(f) {
			t.Errorf("IsFormat(%q) false", f)
		}
	}
	if IsFormat("uptime-kuma") || IsFormat("") {
		t.Error("yabancı biçim kabul edildi")
	}
}
