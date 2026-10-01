package check

import (
	"regexp"
	"sync/atomic"
)

// Kontrol isteklerinin User-Agent'ı. Varsayılan tanınabilir bir değerdir:
// site sahibi güvenlik duvarında (Cloudflare, WAF) "Bekci" içeren isteklere
// izin vererek tüm konumları (ana sunucu + kontrol noktaları) tek kuralla
// geçirebilir; IP adreslerini tek tek eklemek gerekmez. Ayarlar'dan
// değiştirilebilir (SetUserAgent); kontrol noktaları ana sunucunun değerini
// iş listesiyle alır. Monitördeki özel "User-Agent" başlığı her ikisinden önce
// gelir.

var (
	uaVersion  atomic.Pointer[string]
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

// DefaultUserAgent ayarlanmamışsa kullanılan User-Agent.
func DefaultUserAgent() string {
	name := "Bekci"
	if v := uaVersion.Load(); v != nil {
		name += "/" + *v
	}
	return "Mozilla/5.0 (compatible; " + name + "; +https://bekci.app/bot)"
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
