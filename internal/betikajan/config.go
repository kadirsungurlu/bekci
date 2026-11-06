// Package betikajan betik çalıştırmanın ajan tarafıdır: yerel yapılandırma,
// yerel durum, karar akışı (docs/BETIK-GUVENLIK-680.md §6), yürütme kafesi ve
// makbuzlar. Güven kökü burada, operatörün sunucuda root ile yazdığı dosyadır;
// panelden gelen hiçbir şey bu dosyayı değiştiremez. Dosya yoksa ya da bozuksa
// özellik kapalıdır. Yollar sabittir: ortam değişkeniyle değiştirilemez.
package betikajan

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// Paths yerel dosyaların yeri. Üretimde DefaultPaths; başkası yalnız testte.
type Paths struct {
	Config   string // /etc/bekci/betik.json
	StateDir string // /var/lib/bekci-betik
	LogDir   string // /var/log/bekci-betik
}

// DefaultPaths üretim yolları.
func DefaultPaths() Paths {
	return Paths{Config: "/etc/bekci/betik.json", StateDir: "/var/lib/bekci-betik", LogDir: "/var/log/bekci-betik"}
}

// DefaultMaxRunsPerHour max_runs_per_hour verilmemişse yerel saatlik kota.
const DefaultMaxRunsPerHour = 6

// Allow yerel izin kaydı. SHA256 boşsa iğnesizdir: daima bilet ister.
type Allow struct {
	ID          string `json:"id"`
	MinApproval string `json:"min_approval"`
	SHA256      string `json:"sha256,omitempty"`
}

// Trust güvenilen açık anahtarlar (base64).
type Trust struct {
	Yayin []string `json:"yayin"`
	Onay  []string `json:"onay"`
}

// Config /etc/bekci/betik.json.
type Config struct {
	Enabled        bool     `json:"enabled"`
	Trust          Trust    `json:"trust"`
	Groups         []string `json:"groups"`
	Allow          []Allow  `json:"allow"`
	MaxRunsPerHour int      `json:"max_runs_per_hour"`

	yayin, onay []ed25519.PublicKey
}

// ErrNoConfig yapılandırma dosyası yok.
var ErrNoConfig = errors.New("betik yapılandırması yok")

// Validate alanları denetler ve anahtarları çözer.
func (c *Config) Validate() error {
	var err error
	if c.yayin, err = parseKeys(c.Trust.Yayin); err != nil {
		return fmt.Errorf("trust.yayin: %w", err)
	}
	if c.onay, err = parseKeys(c.Trust.Onay); err != nil {
		return fmt.Errorf("trust.onay: %w", err)
	}
	if len(c.yayin) == 0 || len(c.onay) == 0 {
		return errors.New("en az bir yayın ve bir onay anahtarı gerekli")
	}
	for _, k := range c.yayin {
		if slices.ContainsFunc(c.onay, func(o ed25519.PublicKey) bool { return o.Equal(k) }) {
			return errors.New("aynı anahtar iki yuvada olamaz (yayın ve onay)")
		}
	}
	for _, g := range c.Groups {
		if !betik.IsGroup(g) {
			return fmt.Errorf("geçersiz grup %q", g)
		}
	}
	seen := map[string]bool{}
	for _, a := range c.Allow {
		switch {
		case !betik.IsScriptID(a.ID) || seen[a.ID]:
			return fmt.Errorf("geçersiz ya da yinelenen izin %q", a.ID)
		case !betik.ValidApproval(a.MinApproval):
			return fmt.Errorf("izin %q: min_approval geçersiz", a.ID)
		case a.SHA256 != "" && !betik.IsDigest(a.SHA256):
			return fmt.Errorf("izin %q: sha256 geçersiz", a.ID)
		}
		seen[a.ID] = true
	}
	if c.MaxRunsPerHour < 0 || c.MaxRunsPerHour > 3600 {
		return errors.New("max_runs_per_hour sınır dışı")
	}
	return nil
}

func parseKeys(in []string) ([]ed25519.PublicKey, error) {
	out := make([]ed25519.PublicKey, 0, len(in))
	for _, s := range in {
		k, err := betik.ParsePublicKey(s)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, nil
}

func (c *Config) hourlyQuota() int {
	if c.MaxRunsPerHour > 0 {
		return c.MaxRunsPerHour
	}
	return DefaultMaxRunsPerHour
}

func (c *Config) allow(id string) *Allow {
	for i := range c.Allow {
		if c.Allow[i].ID == id {
			return &c.Allow[i]
		}
	}
	return nil
}

const maxConfigSize = 1 << 20

// LoadConfig dosyayı okur. Dosya sembolik bağ olamaz, sahibi çalışan kullanıcı
// olmalı, grup ya da başkası yazamamalı: panelin ulaşabildiği bir süreç gevşek
// izinli dosyaya güven anahtarı ekleyemesin. Dosya yoksa ErrNoConfig.
func LoadConfig(path string) (*Config, error) {
	data, err := readTrusted(path, 0o022, maxConfigSize)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, ErrNoConfig
		}
		return nil, err
	}
	var c Config
	if err := betik.DecodeStrict(data, &c); err != nil {
		return nil, fmt.Errorf("betik.json: %w", err)
	}
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("betik.json: %w", err)
	}
	return &c, nil
}

// readTrusted dosyayı sahip ve izin denetimiyle okur; forbid yasak izin bitleri.
func readTrusted(path string, forbid fs.FileMode, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|noFollow, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Size() > limit {
		return nil, fmt.Errorf("%s: düzenli dosya değil ya da büyük", path)
	}
	if fi.Mode().Perm()&forbid != 0 || !ownedBySelf(fi) {
		return nil, fmt.Errorf("%s: sahip ya da izin güvensiz", path)
	}
	data := make([]byte, fi.Size())
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, err
	}
	return data, nil
}

// SaveConfig yapılandırmayı doğrular ve atomik yazar (0640). Yalnız yerel
// komutlar çağırır; ajan süreci bu işlevi kullanmaz.
func SaveConfig(path string, c *Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicWrite(path, append(mustJSON(c), '\n'), 0o640)
}
