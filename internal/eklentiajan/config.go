package eklentiajan

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
)

// Paths yerel dosyaların yeri. Üretimde DefaultPaths (ortam değişkeniyle
// değiştirilemez); başkası yalnız testte.
type Paths struct {
	Config string // /etc/bekci/eklenti.json (root 600): güvenilen anahtarlar + iğneler
	LogDir string // /var/log/bekci-eklenti: kayıt zinciri
	Kimlik string // yerleşik denetim kimlik.d (boş = denetim.KimlikDir)
}

// DefaultPaths üretim yolları.
func DefaultPaths() Paths {
	return Paths{Config: "/etc/bekci/eklenti.json", LogDir: "/var/log/bekci-eklenti"}
}

// Pin yerel içerik iğnesi (680 D12): eklenti yalnız gövdesinin sha256'sı
// burada kayıtlıysa çalışır.
type Pin struct {
	ID     string `json:"id"`
	SHA256 string `json:"sha256"`
}

// Config /etc/bekci/eklenti.json.
type Config struct {
	Trust []string `json:"trust"` // güvenilen yayın anahtarları (base64)
	Pins  []Pin    `json:"pins"`

	keys []ed25519.PublicKey
}

// ErrNoConfig yapılandırma yok: eklentiler kapalı.
var ErrNoConfig = errors.New("eklenti yapılandırması yok")

const maxConfigSize = 256 << 10

// LoadConfig dosyayı okur ve denetler. Dosya yoksa ErrNoConfig; grup/diğer
// yazabiliyorsa ya da sahibi root değilse (Linux) reddedilir.
func LoadConfig(path string) (*Config, error) {
	st, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNoConfig
	}
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o022 != 0 || !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: izinler gevşek (en çok 0644, root sahibi)", path)
	}
	if !ownedByRoot(st) {
		return nil, fmt.Errorf("%s: sahibi root değil", path)
	}
	if st.Size() > maxConfigSize {
		return nil, fmt.Errorf("%s: çok büyük", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	if err := DecodeStrict(raw, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) validate() error {
	c.keys = c.keys[:0]
	for _, s := range c.Trust {
		k, err := ParsePublicKey(s)
		if err != nil {
			return err
		}
		c.keys = append(c.keys, k)
	}
	seen := map[string]bool{}
	for _, p := range c.Pins {
		if !IsID(p.ID) || !IsDigest(p.SHA256) || seen[p.ID] {
			return fmt.Errorf("geçersiz ya da yinelenen iğne %q", p.ID)
		}
		seen[p.ID] = true
	}
	return nil
}

// Keys güvenilen açık anahtarlar.
func (c *Config) Keys() []ed25519.PublicKey { return c.keys }

// Pin kimliğin iğnesi; yoksa "".
func (c *Config) Pin(id string) string {
	for _, p := range c.Pins {
		if p.ID == id {
			return p.SHA256
		}
	}
	return ""
}

// SetPin iğneyi ekler ya da değiştirir; sha boşsa siler.
func (c *Config) SetPin(id, sha string) {
	c.Pins = slices.DeleteFunc(c.Pins, func(p Pin) bool { return p.ID == id })
	if sha != "" {
		c.Pins = append(c.Pins, Pin{ID: id, SHA256: sha})
		slices.SortFunc(c.Pins, func(a, b Pin) int {
			if a.ID < b.ID {
				return -1
			}
			return 1
		})
	}
}

// SaveConfig dosyayı atomik yazar (geçici dosya + fsync + rename), 0600.
func SaveConfig(path string, c *Config) error {
	if c.Trust == nil {
		c.Trust = []string{}
	}
	if c.Pins == nil {
		c.Pins = []Pin{}
	}
	if err := c.validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".eklenti-*.json")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
