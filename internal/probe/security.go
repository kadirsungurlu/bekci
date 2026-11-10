package probe

import (
	"context"
	"net/http"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// SecurityCollector sunucu güvenlik raporunu toplar (metrics.Collector sağlar;
// sahte toplayıcı sağlamıyorsa rapor gönderilmez). İş 731.
type SecurityCollector interface {
	Security(ctx context.Context) metrics.Security
}

// SecurityAcker isteğe bağlı: panel raporu aldıktan (2xx) ya da kalıcı olarak
// reddettikten (4xx) sonra çağrılır; toplayıcı gönderilen SSH giriş farkını
// birikimden düşer (iş 732).
type SecurityAcker interface {
	AckSecurity(rep metrics.Security)
}

// securityHash SSH sayıları hariç özet: fark yalnız varken gönderimi tetikler,
// gönderimden sonraki sıfır fark yeniden gönderim sebebi olmaz.
func securityHash(rep metrics.Security) string {
	if rep.Auth != nil {
		rep.Auth = &metrics.SecAuth{Source: rep.Auth.Source}
	}
	return rep.Hash()
}

const (
	// securityResendTicks değişmese de rapor bu kadar turda bir gönderilir.
	securityResendTicks = 12
	// minSecurityInterval panel daha kısa istese de en az bu kadar birim.
	minSecurityInterval = 300
	// securityTimeout bir toplama turunun üst sınırı (paket sorguları uzun sürebilir).
	securityTimeout = 4 * time.Minute
)

// setSecurityInterval döngüye yeni aralığı bildirir (setInventoryInterval gibi).
// Eski panel alanı göndermez: aralık 0 kalır, ajan hiç toplamaz.
func (c *Client) setSecurityInterval(n int) {
	if c.cfg.NoMetrics || c.cfg.NoSecurity {
		return
	}
	select {
	case <-c.securityIv:
	default:
	}
	c.securityIv <- max(n, 0)
}

// securityLoop raporu aralıkla toplar, değiştiyse ya da securityResendTicks
// turdur gönderilmediyse POST /api/probe/security ile yollar. Panel ucu
// tanımıyorsa (404) aynı gövdeyi yeniden göndermez (inventoryLoop ile aynı).
func (c *Client) securityLoop(ctx context.Context, sc SecurityCollector) {
	var interval time.Duration
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	sent, idle, lastBad := "", 0, 0
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-c.securityIv:
			if n > 0 {
				n = max(n, minSecurityInterval)
			}
			iv := time.Duration(n) * c.cfg.Unit
			if iv == interval {
				continue
			}
			was := interval
			interval = iv
			timer.Stop()
			switch {
			case iv == 0:
				c.log.Info("sunucu güvenlik raporu gönderimi kapatıldı")
			case was == 0:
				sent, idle, lastBad = "", 0, 0
				timer.Reset(min(iv, 30*c.cfg.Unit)) // kurulumdan sonra çabuk görünsün
			default:
				timer.Reset(iv)
			}
		case <-timer.C:
			cctx, cancel := context.WithTimeout(ctx, securityTimeout)
			rep := sc.Security(cctx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			timer.Reset(interval)
			h := securityHash(rep)
			idle++
			if h == sent && idle < securityResendTicks && !rep.HasAuthDelta() {
				continue
			}
			status, err := c.call(ctx, http.MethodPost, "/api/probe/security", rep, nil)
			switch {
			case err != nil:
				if ctx.Err() == nil {
					c.unreachable("security", err)
				}
			case status >= 200 && status < 300:
				c.reachable("security")
				sent, idle, lastBad = h, 0, 0
				ackSecurity(sc, rep)
			case status == http.StatusTooManyRequests || status >= 500:
				c.unreachable("security", httpStatusErr(status))
			default:
				// 404 (uç yok), 413 vb.: aynı gövdeyi tekrar denemek düzeltmez.
				c.reachable("security")
				sent, idle = h, 0
				ackSecurity(sc, rep) // aynı fark yeniden denenmez; yoksa birikim sınırsız büyür
				if status != lastBad {
					lastBad = status
					c.log.Warn("güvenlik raporu kabul edilmedi", "durum", status)
				}
			}
		}
	}
}

func ackSecurity(sc SecurityCollector, rep metrics.Security) {
	if a, ok := sc.(SecurityAcker); ok {
		a.AckSecurity(rep)
	}
}
