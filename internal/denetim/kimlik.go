package denetim

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Kimlik ajandaki yerel kimlik dosyası (G6): /etc/bekci/kimlik.d/<ad>.json
// (root 600) ya da C:\ProgramData\Bekci\kimlik.d\<ad>.json (yalnız SYSTEM
// ve Administrators). Panel yalnız adı söyler; içerik panele hiç gitmez.
type Kimlik struct {
	User     string `json:"user"`
	Password string `json:"password"`
	// Hosts loopback dışında bağlanılabilecek ana bilgisayarlar (G6).
	Hosts []string `json:"hosts"`
	// Options türe özgü ek değerler (ör. veritabanı adı).
	Options map[string]string `json:"options"`
}

// HostIzinli ana bilgisayar hosts listesinde mi (büyük/küçük harf duyarsız;
// listedeki "host:port" girdisinin yalnız ana bilgisayarı karşılaştırılır).
func (k *Kimlik) HostIzinli(host string) bool {
	host = strings.ToLower(strings.Trim(host, "[]"))
	for _, h := range k.Hosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if hh, _, err := splitHostPortOpt(h); err == nil {
			h = hh
		}
		if h != "" && h == host {
			return true
		}
	}
	return false
}

func splitHostPortOpt(s string) (string, string, error) {
	if i := strings.LastIndex(s, ":"); i > 0 && !strings.Contains(s[:i], ":") {
		return s[:i], s[i+1:], nil
	}
	if strings.HasPrefix(s, "[") {
		if i := strings.Index(s, "]"); i > 0 {
			return s[1:i], strings.TrimPrefix(s[i+1:], ":"), nil
		}
	}
	return s, "", errors.New("port yok")
}

var kimlikAdRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// KimlikAdiGecerli dosya adı kuralı (yol ayırıcı ve nokta yok).
func KimlikAdiGecerli(s string) bool { return kimlikAdRe.MatchString(s) }

// KimlikDir üretim yolu.
func KimlikDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("ProgramData")
		if base == "" {
			base = `C:\ProgramData`
		}
		return filepath.Join(base, "Bekci", "kimlik.d")
	}
	return "/etc/bekci/kimlik.d"
}

const maxKimlik = 64 << 10

// KimlikOku dosyayı izin denetiminden geçirip okur. Hata *Hata (kod
// credential) ve kullanıcıya ne yapacağını söyleyen metin taşır.
func KimlikOku(dir, ad string) (*Kimlik, error) {
	if !KimlikAdiGecerli(ad) {
		return nil, hata(HataCredential, "kimlik adı geçersiz: %q", ad)
	}
	path := filepath.Join(dir, ad+".json")
	if err := izinDenetle(dir, path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, hata(HataCredential, "kimlik dosyası yok: %s", path)
		}
		var h *Hata
		if errors.As(err, &h) {
			return nil, err
		}
		return nil, hata(HataCredential, "kimlik dosyası denetlenemedi: %s: %v", path, err)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, hata(HataCredential, "kimlik dosyası açılamadı: %s: %v", path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxKimlik+1))
	if err != nil || len(b) > maxKimlik {
		return nil, hata(HataCredential, "kimlik dosyası okunamadı ya da 64 KiB'den büyük: %s", path)
	}
	var k Kimlik
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&k); err != nil {
		return nil, hata(HataCredential, "kimlik dosyası JSON değil (alanlar: user, password, hosts, options): %s: %v", path, err)
	}
	return &k, nil
}
