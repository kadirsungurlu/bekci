package eklentiajan

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImzaKomut(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "imza.key")
	var out bytes.Buffer
	if err := komut(Paths{}, []string{"anahtar-uret", key}, &out, false); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(key); st.Mode().Perm() != 0o600 {
		t.Fatalf("izin %v", st.Mode().Perm())
	}
	if err := komut(Paths{}, []string{"anahtar-uret", key}, &out, false); err == nil {
		t.Fatal("var olan anahtarın üzerine yazdı")
	}
	var pubB64 string
	for _, l := range strings.Split(out.String(), "\n") {
		if v, ok := strings.CutPrefix(l, "açık anahtar: "); ok {
			pubB64 = v
		}
	}
	pub, err := ParsePublicKey(pubB64)
	if err != nil {
		t.Fatal(err)
	}
	body := filepath.Join(dir, "x.sh")
	os.WriteFile(body, []byte("echo 'OK | a=1'\n"), 0o644)
	man := filepath.Join(dir, "m.json")
	os.WriteFile(man, []byte(`{"schema":1,"id":"ornek","version":1,"sha256":"","interpreter":"sh","mode":"periodic","class":"metric",
"run_as":"nobody","platforms":["linux"],"interval_min":60,"interval_max":3600,"timeout_sec":10,"output":"nagios","network":false,
"not_before":0,"not_after":0}`), 0o644)
	out.Reset()
	if err := komut(Paths{}, []string{"imzala", key, man, body}, &out, false); err != nil {
		t.Fatal(err)
	}
	p, err := VerifyEnvelope(bytes.TrimSpace(out.Bytes()), []ed25519.PublicKey{pub})
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(p)
	if err != nil || m.ID != "ornek" || m.NotAfter <= m.NotBefore {
		t.Fatalf("%v %+v", err, m)
	}
	if _, err := InspectEnvelope(bytes.TrimSpace(out.Bytes())); err != nil {
		t.Fatal(err)
	}
	// Yanlış sha256 reddedilir; bozuk anahtar dosyası reddedilir.
	os.WriteFile(man, bytes.Replace(mustRead(t, man), []byte(`"sha256":""`), []byte(`"sha256":"`+strings.Repeat("a", 64)+`"`), 1), 0o644)
	if _, _, err := ImzalaDosya(key, man, body); err == nil {
		t.Fatal("yanlış sha256 kabul edildi")
	}
	os.WriteFile(key, []byte(base64.StdEncoding.EncodeToString([]byte("kisa"))), 0o600)
	if _, _, err := ImzalaDosya(key, man, body); err == nil {
		t.Fatal("bozuk anahtar kabul edildi")
	}
}

func mustRead(t *testing.T, p string) []byte {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// VM kabulü: imzala çıktısındaki izin komutu doğrudan kopyalanabilmeli.
func TestManifestID(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "m.json")
	os.WriteFile(p, []byte(`{"schema":1,"id":"mdadm"}`), 0o600)
	if got := manifestID(p); got != "mdadm" {
		t.Fatalf("id: %q", got)
	}
	os.WriteFile(p, []byte(`{"id":"../x"}`), 0o600)
	if got := manifestID(p); got != "<ad>" {
		t.Fatalf("geçersiz id: %q", got)
	}
}
