package probe

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// InventoryCollector sunucu envanterini toplar (metrics.Collector sağlar;
// sahte toplayıcı sağlamıyorsa envanter gönderilmez).
type InventoryCollector interface {
	Inventory(ctx context.Context) metrics.Inventory
}

// inventoryResendTicks değişmese de envanter bu kadar turda bir gönderilir
// (panel son görülme zamanını tazeler, olay üstünde kaybolan gönderim düzelir).
const inventoryResendTicks = 12

const inventoryTimeout = 90 * time.Second

// setInventoryInterval döngüye yeni aralığı bildirir (setMetricsInterval gibi).
// Eski panel alanı göndermez: aralık 0 kalır, ajan hiç toplamaz.
func (c *Client) setInventoryInterval(n int) {
	if c.cfg.NoMetrics || c.cfg.NoInventory {
		return
	}
	select {
	case <-c.inventoryIv:
	default:
	}
	c.inventoryIv <- max(n, 0)
}

// inventoryLoop envanteri aralıkla toplar, değiştiyse ya da inventoryResendTicks
// turdur gönderilmediyse yollar. Panel uç noktayı tanımıyorsa (404: geri alınmış
// panel) aynı aralık yeniden gelene kadar sessizce bekler; her turda denemez.
func (c *Client) inventoryLoop(ctx context.Context, ic InventoryCollector) {
	var interval time.Duration
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	sent, idle, lastBad := "", 0, 0
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-c.inventoryIv:
			if n > 0 {
				n = max(n, minMetricsInterval)
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
				c.log.Info("sunucu envanteri gönderimi kapatıldı")
			case was == 0:
				sent, idle, lastBad = "", 0, 0
				timer.Reset(min(iv, 10*c.cfg.Unit)) // kurulumdan sonra çabuk görünsün
			default:
				timer.Reset(iv)
			}
		case <-timer.C:
			cctx, cancel := context.WithTimeout(ctx, inventoryTimeout)
			inv := ic.Inventory(cctx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			timer.Reset(interval)
			h := inv.Hash()
			idle++
			if h == sent && idle < inventoryResendTicks {
				continue
			}
			status, err := c.call(ctx, http.MethodPost, "/api/probe/inventory", inv, nil)
			switch {
			case err != nil:
				if ctx.Err() == nil {
					c.unreachable("inventory", err)
				}
			case status >= 200 && status < 300:
				c.reachable("inventory")
				sent, idle, lastBad = h, 0, 0
			case status == http.StatusTooManyRequests || status >= 500:
				c.unreachable("inventory", httpStatusErr(status))
			default:
				// 404 (uç yok), 413 vb.: aynı gövdeyi tekrar denemek düzeltmez.
				c.reachable("inventory")
				sent, idle = h, 0
				if status != lastBad {
					lastBad = status
					c.log.Warn("envanter kabul edilmedi", "durum", status)
				}
			}
		}
	}
}

func httpStatusErr(status int) error { return fmt.Errorf("sunucu yanıtı: %d", status) }
