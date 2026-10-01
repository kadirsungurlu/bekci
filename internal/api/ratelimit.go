package api

import (
	"sync"
	"time"
)

// pushPerMinute /api/push/{token} için IP başına dakikalık istek sınırı.
// Cömert tutulur: token tahmin edilemez (192 bit), sınır yalnızca kaynak
// tüketimine karşıdır; aynı makinedeki onlarca cron işi rahatça sığar.
const pushPerMinute = 300

// ipLimiter IP başına sabit pencereli (dakikalık) istek sayacı.
type ipLimiter struct {
	mu     sync.Mutex
	perMin int
	m      map[string]*ipWindow
}

type ipWindow struct {
	start int64 // dakika
	count int
}

func newIPLimiter(perMin int) *ipLimiter {
	return &ipLimiter{perMin: perMin, m: map[string]*ipWindow{}}
}

// allow isteğe izin verir; vermezse pencerenin bitimine kalan saniye.
func (l *ipLimiter) allow(ip string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	minute := now.Unix() / 60
	w := l.m[ip]
	if w == nil || w.start != minute {
		w = &ipWindow{start: minute}
		l.m[ip] = w
		// Bellek şişmesin: eski pencereler ara ara temizlenir.
		if len(l.m) > 10000 {
			for k, v := range l.m {
				if v.start != minute {
					delete(l.m, k)
				}
			}
		}
	}
	w.count++
	if w.count > l.perMin {
		return false, int(60 - now.Unix()%60)
	}
	return true, 0
}
