package check

import (
	"regexp"
	"sync/atomic"
)

// Kontrol isteklerinin User-Agent'ı. Varsayılan tanınabilir bir değerdir
// ("Bekci" ve panelin adresi; site sahibi günlükte kimin kontrol ettiğini görür):
// site sahibi güvenlik duvarında (Cloudflare, WAF) "Bekci" içeren isteklere
// izin vererek tüm konumları (ana sunucu + kontrol noktaları) tek kuralla
// geçirebilir; IP adreslerini tek tek eklemek gerekmez. Ayarlar'dan
// değiştirilebilir (SetUserAgent); kontrol noktaları ana sunucunun değerini
// iş listesiyle alır. Monitördeki özel "User-Agent" başlığı her ikisinden önce
// gelir.

var (
	uaVersion  atomic.Pointer[string]
	uaInfoURL  atomic.Pointer[string]
	uaOverride atomic.Pointer[string]
	semverRe   = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)`)
)

// SetVersion varsayılan User-Agent'taki sürümü ayarlar (program açılışında).
// Sürüm numarası değilse (geliştirme derlemesi, commit kimliği) yazılmaz.
func SetVersion(v string) {
	if m := semverRe.FindStringSubmatch(v); m != nil {
		s := m[1]
		uaVersion.Store(&s)
	}
}

// SetInfoURL varsayılan User-Agent'taki adresi ayarlar: Bekci'nin kurulu
// olduğu panelin adresi (BASE_URL). Boşsa adres yazılmaz.
func SetInfoURL(u string) {
	if u == "" || len(u) > 200 || !printableASCII(u) {
		uaInfoURL.Store(nil)
		return
	}
	uaInfoURL.Store(&u)
}

// DefaultUserAgent ayarlanmamışsa kullanılan User-Agent, ör.
// "Mozilla/5.0 (compatible; Bekci/1.2.2; +https://uptime.ornek.com)".
func DefaultUserAgent() string {
	name := "Bekci"
	if v := uaVersion.Load(); v != nil {
		name += "/" + *v
	}
	if u := uaInfoURL.Load(); u != nil {
		name += "; +" + *u
	}
	return "Mozilla/5.0 (compatible; " + name + ")"
}

func printableASCII(s string) bool {
	for _, r := range s {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

// SetUserAgent kontrol isteklerinin User-Agent'ını değiştirir; boş: varsayılan.
func SetUserAgent(ua string) {
	if ua == "" {
		uaOverride.Store(nil)
		return
	}
	uaOverride.Store(&ua)
}

// UserAgent kontrol isteklerinde kullanılan (geçerli) User-Agent.
func UserAgent() string {
	if ua := uaOverride.Load(); ua != nil {
		return *ua
	}
	return DefaultUserAgent()
}
