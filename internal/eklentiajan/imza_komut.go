package eklentiajan

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// imzaKomut ücretsiz derlemedeki eklenti imzalama araçları (iş 730 §B.6):
// anahtar-uret ve imzala. Özel anahtar yalnız yayıncının makinesinde durur;
// ajana yalnız açık anahtar (`anahtar`) ve gövde iğnesi (`izin`) girer.
func imzaKomut(args []string, out io.Writer) error {
	switch {
	case args[0] == "anahtar-uret" && len(args) == 2:
		pub, err := AnahtarUret(args[1])
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "özel anahtar: %s (gizli tutun, ajana koymayın)\n", args[1])
		fmt.Fprintf(out, "açık anahtar: %s\n", pub)
		fmt.Fprintf(out, "ajanda: uptime betik eklenti anahtar %s\n", pub)
	case args[0] == "imzala" && len(args) == 4:
		zarf, sha, err := ImzalaDosya(args[1], args[2], args[3])
		if err != nil {
			return err
		}
		out.Write(append(zarf, '\n'))
		fmt.Fprintf(os.Stderr, "gövde sha256: %s\najanda: uptime betik eklenti izin %s %s\n", sha, manifestID(args[2]), sha)
	default:
		return errors.New(usage)
	}
	return nil
}

// manifestID izin komutu için manifestteki id (okunamazsa "<ad>").
func manifestID(path string) string {
	var m struct {
		ID string `json:"id"`
	}
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &m) == nil && IsID(m.ID) {
		return m.ID
	}
	return "<ad>"
}

// AnahtarUret yeni Ed25519 anahtarı üretir, tohumu base64 olarak dosyaya
// (0600, var olanın üzerine yazmaz) koyar ve açık anahtarı base64 döner.
func AnahtarUret(path string) (string, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(base64.StdEncoding.EncodeToString(priv.Seed()) + "\n"); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(pub), nil
}

func okuOzelAnahtar(path string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("özel anahtar dosyası geçersiz (anahtar-uret çıktısı bekleniyor)")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

// ImzalaDosya manifesti okur, sha256 alanını gövdeden (not_before/not_after
// ikisi de 0 ise 90 günlük süreyi) doldurur, katı
// denetimden geçirir ve imzalı zarfı döner (gövde özetiyle birlikte).
func ImzalaDosya(keyPath, manifestPath, bodyPath string) ([]byte, string, error) {
	priv, err := okuOzelAnahtar(keyPath)
	if err != nil {
		return nil, "", err
	}
	body, err := os.ReadFile(bodyPath)
	if err != nil {
		return nil, "", err
	}
	if len(body) == 0 || len(body) > MaxBody {
		return nil, "", errors.New("gövde boş ya da sınırın üstünde")
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, "", err
	}
	var m Manifest
	if err := DecodeStrict(raw, &m); err != nil {
		return nil, "", fmt.Errorf("manifest: %w", err)
	}
	if m.SHA256 != "" && m.SHA256 != sha {
		return nil, "", errors.New("manifestteki sha256 gövdeyle eşleşmiyor (alanı boş bırakın, araç doldurur)")
	}
	m.SHA256 = sha
	// Süre verilmemişse şimdiden başlayıp 90 gün geçerli (üst sınır 180).
	if m.NotBefore == 0 && m.NotAfter == 0 {
		now := time.Now().Unix()
		m.NotBefore, m.NotAfter = now-60, now+90*86400
	}
	payload, err := json.Marshal(&m)
	if err != nil {
		return nil, "", err
	}
	if _, err := ParseManifest(payload); err != nil {
		return nil, "", err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, SignManifest(priv, payload), "", "  "); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), sha, nil
}
