package api

import (
	"sync"
	"time"
)

// pushPerMinute /api/push/{token} için IP başına dakikalık istek sınırı.
// Cömert tutulur: token tahmin edilemez (192 bit), sınır yalnızca kaynak
// tüketimine karşıdır; aynı makinedeki onlarca cron işi rahatça sığar.
const pushPerMinute = 300

// ipLimiter IP başına sabit pencereli (varsayılan dakikalık) istek sayacı.
type ipLimiter struct {
	mu     sync.Mutex
	perMin int   // pencere başına izin (adı tarihsel: dakikalık pencereden kalma)
	window int64 // pencere uzunluğu (saniye)
	m      map[string]*ipWindow
}

type ipWindow struct {
	start int64 // pencere sırası (unix / window)
	count int
}

func newIPLimiter(perMin int) *ipLimiter {
	return &ipLimiter{perMin: perMin, window: 60, m: map[string]*ipWindow{}}
}

// newIPLimiterWindow pencere başına n istek (pencere saniyenin katı olmalı).
func newIPLimiterWindow(n int, window time.Duration) *ipLimiter {
	return &ipLimiter{perMin: n, window: int64(window / time.Second), m: map[string]*ipWindow{}}
}

// allow isteğe izin verir; vermezse pencerenin bitimine kalan saniye.
func (l *ipLimiter) allow(ip string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	minute := now.Unix() / l.window
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
		return false, int(l.window - now.Unix()%l.window)
	}
	return true, 0
}
