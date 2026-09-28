package api

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Ajan IP kilidi (adres ailesi başına) --------------------------------------------
//
// Kilit, adres ailesi başına en fazla bir değer tutar: bir IPv4 adresi (tam
// eşleşme) ve bir IPv6 /64 ağı. Çift yığınlı (dual-stack) sunucular IPv4 ile
// IPv6 arasında gidip gelebilir; IPv6 gizlilik adresleri de sık değişir ama
// aynı /64 içinde kalır. Bir ailenin ilk isteği o aileyi sabitler; ailesi
// sabitlenmiş istek eşleşmelidir. Değer mevcut locked_ip TEXT sütununda
// "v4,v6/64" biçiminde (virgülle ayrılmış, boş olan yazılmaz) saklanır; eski
// sürümün tek IP'lik değeri de okunur (IPv6 ise /64'e genişletilir).

// ipLockValue kilitli değerin aile bazında çözülmüş hali.
type ipLockValue struct{ v4, v6 string }

func parseIPLock(s string) ipLockValue {
	var v ipLockValue
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if pfx, err := netip.ParsePrefix(part); err == nil {
			if pfx.Addr().Is4() {
				v.v4 = pfx.Addr().String()
			} else {
				v.v6 = netip.PrefixFrom(pfx.Addr(), 64).Masked().String()
			}
			continue
		}
		fam, key := ipLockKey(part)
		if fam == 6 {
			v.v6 = key
		} else {
			v.v4 = key
		}
	}
	return v
}

func (v ipLockValue) String() string {
	switch {
	case v.v4 != "" && v.v6 != "":
		return v.v4 + "," + v.v6
	case v.v4 != "":
		return v.v4
	}
	return v.v6
}

// ipLockKey adresin ailesi (4 veya 6) ve kilitte karşılaştırılan değeri:
// IPv4 için adresin kendisi, IPv6 için /64 ağı. Çözülemeyen değer (normalde
// olmaz) olduğu gibi IPv4 yuvasında tam eşleşmeyle karşılaştırılır.
func ipLockKey(ip string) (int, string) {
	a, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return 4, ip
	}
	a = a.Unmap()
	if a.Is4() {
		return 4, a.String()
	}
	return 6, netip.PrefixFrom(a.WithZone(""), 64).Masked().String()
}

// checkIPLock isteğin IP'sini kilitli değerle karşılaştırır. Ailesi henüz
// sabitlenmemişse pin=true ve yazılacak yeni değer döner.
func checkIPLock(locked, ip string) (next string, allowed, pin bool) {
	v := parseIPLock(locked)
	fam, key := ipLockKey(ip)
	slot := &v.v4
	if fam == 6 {
		slot = &v.v6
	}
	if *slot == "" {
		*slot = key
		return v.String(), true, true
	}
	return locked, *slot == key, false
}

// enforceIPLock kilidi uygular; gerekirse isteğin ailesini sabitler. İlk
// sabitleme yarışında (iki istek aynı anda) yazma yalnızca değer hâlâ
// okunduğu gibiyse yapılır; kaybeden istek kaydı yeniden okuyup kazananın
// değeriyle karşılaştırılır. p.LockedIP güncel değerle doldurulur.
func (s *Server) enforceIPLock(ctx context.Context, p *store.Probe, reqIP string) (bool, error) {
	for range 3 {
		if !p.IPLock {
			return true, nil
		}
		next, allowed, pin := checkIPLock(p.LockedIP, reqIP)
		if !allowed || !pin {
			return allowed, nil
		}
		ok, err := s.store.SwapProbeLockedIP(ctx, p.ID, p.LockedIP, next)
		if err != nil {
			// Yazılamadıysa istek (önceki davranıştaki gibi) kabul edilir;
			// kilit bir sonraki istekte yeniden denenir.
			s.log.Warn("ajan IP kilidi yazılamadı", "hata", err)
			return true, nil
		}
		if ok {
			s.log.Info("ajan IP'ye kilitlendi", "ajan", p.Name, "ip", reqIP, "kilit", next)
			p.LockedIP = next
			return true, nil
		}
		// Başka bir istek araya girdi (ya da kilit sıfırlandı): güncel kaydı oku.
		fresh, err := s.store.GetProbe(ctx, p.ID)
		if err != nil {
			return false, err
		}
		p.IPLock, p.LockedIP = fresh.IPLock, fresh.LockedIP
	}
	return false, nil
}

// Başarısız ajan kimlik doğrulaması için IP başına ön sınır -----------------------
//
// Geçersiz token veya kilitli IP dışından gelen istekler kontrol noktası
// başına istek sınırından önce reddedilir; her biri bir veritabanı sorgusu ve
// bir uyarı logu demektir. Aynı kaynak IP'den dakikada probeAuthFailPerMin
// başarısızlıktan sonra istekler veritabanına gitmeden 429 ile reddedilir.
// Uyarı logu da IP başına dakikada bir kez yazılır (log taşmasın).

const probeAuthFailPerMin = 30

type probeAuthLimiter struct {
	mu     sync.Mutex
	minute int64
	m      map[string]int
}

func newProbeAuthLimiter() *probeAuthLimiter { return &probeAuthLimiter{m: map[string]int{}} }

// roll dakika değiştiyse sayaçları sıfırlar (sabit pencere; eski kayıtlar
// birikmez).
func (l *probeAuthLimiter) roll(now time.Time) {
	if m := now.Unix() / 60; m != l.minute {
		l.minute = m
		clear(l.m)
	}
}

// blocked IP bu dakikada sınırı aştı mı? Aştıysa kalan saniye de döner.
func (l *probeAuthLimiter) blocked(ip string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll(now)
	if l.m[ip] >= probeAuthFailPerMin {
		return true, int(60 - now.Unix()%60)
	}
	return false, 0
}

// fail başarısızlığı sayar; log=true ise bu IP için bu dakikanın ilk
// başarısızlığıdır (uyarı logu yalnızca o zaman yazılır).
func (l *probeAuthLimiter) fail(ip string, now time.Time) (log bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.roll(now)
	l.m[ip]++
	return l.m[ip] == 1
}

// probeAuthFail başarısız ajan isteğini sayar, gerekirse uyarı yazar ve
// yanıtı gönderir.
func (s *Server) probeAuthFail(w http.ResponseWriter, ip string, status int, msg, logMsg string, attrs ...any) {
	if s.probeAuthRL.fail(ip, s.now()) && logMsg != "" {
		s.log.Warn(logMsg, append([]any{"ip", ip, "not", "bu IP'den dakikadaki diğer başarısız istekler loglanmaz"}, attrs...)...)
	}
	writeError(w, status, msg)
}
