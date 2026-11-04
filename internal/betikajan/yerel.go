package betikajan

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// Yerel komutların (uptime betik ...) işleri. Yalnız sunucuda root ile çalışır;
// panel, kurulum komutu, ortam değişkeni ya da güncelleme bu yolu kullanamaz.

// EnableOptions "uptime betik ac" girdileri; boş alan var olan değeri korur.
type EnableOptions struct {
	Yayin, Onay, Groups []string
	Allow               []Allow
	MaxRunsPerHour      int
}

// Enable özelliği açar: dizinler, K_ajan, durum, yapılandırma. Bozuk durum
// üzerine yazılmaz (onar gerekir). host_key döner.
func Enable(p Paths, o EnableOptions) (string, error) {
	cfg, err := LoadConfig(p.Config)
	switch {
	case errors.Is(err, ErrNoConfig):
		cfg = &Config{}
	case err != nil:
		return "", err
	}
	cfg.Enabled = true
	cfg.Trust.Yayin = union(cfg.Trust.Yayin, o.Yayin)
	cfg.Trust.Onay = union(cfg.Trust.Onay, o.Onay)
	cfg.Groups = union(cfg.Groups, o.Groups)
	for _, a := range o.Allow {
		cfg.setAllow(a)
	}
	if o.MaxRunsPerHour > 0 {
		cfg.MaxRunsPerHour = o.MaxRunsPerHour
	}
	if cfg.MaxRunsPerHour == 0 {
		cfg.MaxRunsPerHour = DefaultMaxRunsPerHour
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(p.LogDir, 0o750); err != nil {
		return "", err
	}
	s := NewStore(p.StateDir)
	key, err := s.EnsureKey()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(p.StateDir, stateFile)); os.IsNotExist(err) {
		if err := s.save(newState()); err != nil {
			return "", err
		}
	} else if _, err := s.Read(); err != nil {
		return "", fmt.Errorf("durum bozuk, önce uptime betik onar: %w", err)
	}
	s.EnsureReserve()
	if err := SaveConfig(p.Config, cfg); err != nil {
		return "", err
	}
	return HostKey(key), nil
}

func union(a, b []string) []string {
	for _, v := range b {
		if !slices.Contains(a, v) {
			a = append(a, v)
		}
	}
	return a
}

func (c *Config) setAllow(a Allow) {
	if e := c.allow(a.ID); e != nil {
		*e = a
		return
	}
	c.Allow = append(c.Allow, a)
}

// ParseAllow "id", "id=sha256" ya da "id=sha256:min" biçimini çözer. İğnesiz
// kayıt daima signed'dır. Joker yoktur: id kalıbı "*" kabul etmez.
func ParseAllow(s string) (Allow, error) {
	id, rest, hasPin := strings.Cut(s, "=")
	a := Allow{ID: id, MinApproval: betik.ApprovalSigned}
	if hasPin {
		pin, minA, hasMin := strings.Cut(rest, ":")
		a.SHA256, a.MinApproval = pin, betik.ApprovalNone
		if hasMin {
			a.MinApproval = minA
		}
		if !betik.IsDigest(a.SHA256) {
			return a, fmt.Errorf("izin %q: sha256 64 küçük hex olmalı", id)
		}
	}
	if !betik.IsScriptID(a.ID) || !betik.ValidApproval(a.MinApproval) {
		return a, fmt.Errorf("izin %q geçersiz", s)
	}
	return a, nil
}

func editConfig(p Paths, fn func(*Config) error) error {
	cfg, err := LoadConfig(p.Config)
	if err != nil {
		return err
	}
	if err := fn(cfg); err != nil {
		return err
	}
	return SaveConfig(p.Config, cfg)
}

// AllowSet izin kaydı ekler ya da değiştirir.
func AllowSet(p Paths, in []Allow) error {
	return editConfig(p, func(c *Config) error {
		for _, a := range in {
			c.setAllow(a)
		}
		return nil
	})
}

// AllowDelete izin kaydını siler.
func AllowDelete(p Paths, ids []string) error {
	return editConfig(p, func(c *Config) error {
		c.Allow = slices.DeleteFunc(c.Allow, func(a Allow) bool { return slices.Contains(ids, a.ID) })
		return nil
	})
}

// SlotEdit anahtar yuvası değişikliği (dönüşüm panelden geçmez).
type SlotEdit struct{ AddYayin, DelYayin, AddOnay, DelOnay []string }

// EditKeys yuvaları günceller.
func EditKeys(p Paths, e SlotEdit) error {
	return editConfig(p, func(c *Config) error {
		c.Trust.Yayin = slices.DeleteFunc(union(c.Trust.Yayin, e.AddYayin), func(k string) bool { return slices.Contains(e.DelYayin, k) })
		c.Trust.Onay = slices.DeleteFunc(union(c.Trust.Onay, e.AddOnay), func(k string) bool { return slices.Contains(e.DelOnay, k) })
		return nil
	})
}

// Disable özelliği kapatır. Dosya bozuk ya da yoksa özellik zaten kapalıdır.
func Disable(p Paths) error {
	err := editConfig(p, func(c *Config) error { c.Enabled = false; return nil })
	if errors.Is(err, ErrNoConfig) {
		return nil
	}
	return err
}

// RevokeFile iptal zarfını yerelde işler (panel iletmiyorsa).
func RevokeFile(p Paths, raw []byte) error {
	cfg, err := LoadConfig(p.Config)
	if err != nil {
		return err
	}
	return NewStore(p.StateDir).Update(func(st *State) error { return ApplyRevoke(cfg, st, raw) })
}

// Repair durumu yeniden açar. Dosya sağlamsa yalnız last_seen şimdiye alınır
// (yanlış ileri saatle kilitlenme); bozuk ya da yoksa sıfırdan kurulur, makbuz
// zinciri kayit.log'dan sürer. Sürüm tabanı ve sayaçlar kaybolmuş olabilir.
func Repair(p Paths, now time.Time) (rebuilt bool, err error) {
	if err := os.MkdirAll(p.StateDir, 0o700); err != nil {
		return false, err
	}
	s := NewStore(p.StateDir)
	unlock, err := lockFile(s.path(lockName))
	if err != nil {
		return false, err
	}
	defer unlock()
	st, lerr := s.load()
	if lerr != nil {
		rebuilt = true
		st = newState()
		j := &Journal{dir: p.LogDir}
		if st.Seq, st.Prev, err = j.Tail(); err != nil {
			return false, err
		}
	}
	st.LastSeen = now.Unix()
	if err := s.save(st); err != nil {
		return false, err
	}
	if _, err := s.EnsureKey(); err != nil {
		return false, err
	}
	s.EnsureReserve()
	return rebuilt, nil
}

// Report "uptime betik durum" çıktısının verisi.
type Report struct {
	Reason  string
	HostKey string
	Config  *Config
	State   *State
}

// Status yerel durumu okur.
func Status(p Paths, plat Platform) *Report {
	r := &Report{}
	cfg, err := LoadConfig(p.Config)
	switch {
	case !plat.OK:
		r.Reason = betik.ReasonPlatform
	case errors.Is(err, ErrNoConfig):
		r.Reason = betik.ReasonMissing
	case err != nil:
		r.Reason = betik.ReasonConfig
	case !cfg.Enabled:
		r.Reason = betik.ReasonDisabled
	}
	r.Config = cfg
	s := NewStore(p.StateDir)
	if k, err := s.LoadKey(); err == nil {
		r.HostKey = HostKey(k)
	} else if r.Reason == "" {
		r.Reason = betik.ReasonState
	}
	if st, err := s.Read(); err == nil {
		r.State = st
	} else if r.Reason == "" {
		r.Reason = betik.ReasonState
	}
	return r
}
