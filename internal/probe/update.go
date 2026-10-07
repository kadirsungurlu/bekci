package probe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
)

// Kendini güncelleme — ajan tarafı (ayrıntı: paket açıklaması ve
// internal/agentupdate). Tümü jobsLoop goroutine'inde çalışır.

// updateRefuseFor reddedilen/başarısız bir sürüm bu kadar süre yeniden
// denenmez (panel teklifi her yoklamada yineler; döngüye girmesin).
const updateRefuseFor = time.Hour

// updateBusyRetry panel indirmeyi geçici olarak reddettiyse (429: aynı IP'den
// çok indirme, 503) bu kadar sonra yeniden denenir; başarısızlık sayılmaz.
const updateBusyRetry = time.Minute

// errUpdateBusy indirme geçici olarak reddedildi.
var errUpdateBusy = errors.New("panel indirmeyi geçici olarak reddetti")

// afterPoll başarılı iş listesi yanıtından sonra: ilk yoklamada bekleyen
// güncelleme onaylanır (yeni sürüm panele ulaştı) ve açılıştan kalan durum
// raporu gönderilir; teklif varsa uygulanır.
func (c *Client) afterPoll(ctx context.Context, offer *agentupdate.Offer) {
	if !c.updateFirst {
		c.updateFirst = true
		c.cfg.Updater.Confirm()
	}
	if c.updateReport != nil && c.reportUpdate(ctx, *c.updateReport) {
		c.updateReport = nil
	}
	if offer != nil && c.cfg.Updater != nil {
		c.tryUpdate(ctx, *offer)
	}
}

// tryUpdate teklifi uygular: kontrol (sürüm, platform, imza), panele
// "başladı", indir + doğrula + değiştir, sonra yeniden başlatma isteği.
// Başarısızlık panele bildirilir ve aynı sürüm bir süre yeniden denenmez.
func (c *Client) tryUpdate(ctx context.Context, o agentupdate.Offer) {
	if until, ok := c.updateRefused[o.Version]; ok && time.Now().Before(until) {
		return
	}
	u := c.cfg.Updater
	if err := u.Check(o); err != nil {
		// Teklif bu ajana uymuyor (eski/aynı sürüm, imza, platform): bir kez
		// logla, bir saat boyunca yeniden bakma.
		c.updateRefused[o.Version] = time.Now().Add(updateRefuseFor)
		c.log.Warn("güncelleme teklifi reddedildi", "sürüm", o.Version, "neden", err)
		return
	}
	c.log.Info("güncelleme başlıyor", "eski", c.cfg.Version, "yeni", o.Version)
	c.reportUpdate(ctx, agentupdate.Report{Status: agentupdate.StatusStarted, Version: o.Version})
	if err := u.Install(ctx, o, c.download); err != nil {
		if ctx.Err() != nil {
			return // kapanış
		}
		if errors.Is(err, errUpdateBusy) {
			c.updateRefused[o.Version] = time.Now().Add(updateBusyRetry)
			c.log.Info("güncelleme indirmesi ertelendi", "sürüm", o.Version, "neden", err)
			return
		}
		c.updateRefused[o.Version] = time.Now().Add(updateRefuseFor)
		c.log.Error("güncelleme başarısız", "sürüm", o.Version, "hata", err)
		rep := agentupdate.Report{Status: agentupdate.StatusFailed, Version: o.Version, Error: err.Error()}
		if !c.reportUpdate(ctx, rep) {
			c.updateReport = &rep
		}
		return
	}
	c.restartOnce.Do(func() { close(c.restart) })
}

// download programı panelden indirir (GET, kimlikli); gövde w'ye akar.
// Yalnızca panelin kendi adresi altındaki göreli yol kabul edilir (Check
// doğruladı). 30 sn'lik istemci zaman aşımı büyük dosyaya yetmez: indirme
// için zaman aşımı yalnızca bağlamla sınırlanır.
func (c *Client) download(ctx context.Context, path string, w io.Writer) error {
	dctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, c.base.String()+path, nil)
	if err != nil {
		return err
	}
	c.setHeaders(req, false)
	hc := *c.cfg.HTTPClient // aynı Transport ve yönlendirme kuralı; zaman aşımı yok
	hc.Timeout = 0
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		return fmt.Errorf("%w (%d)", errUpdateBusy, resp.StatusCode)
	default:
		return fmt.Errorf("sunucu yanıtı: %d", resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// reportUpdate durumu panele bildirir. Kabul edildiyse ya da panel bu uç
// noktayı bilmiyorsa (eski panel, 404/405) true; geçici hatada false (çağıran
// sonraki yoklamada yeniden dener).
func (c *Client) reportUpdate(ctx context.Context, rep agentupdate.Report) bool {
	status, err := c.call(ctx, http.MethodPost, "/api/probe/update", rep, nil)
	switch {
	case err != nil:
		return errors.Is(ctx.Err(), context.Canceled)
	case status >= 200 && status < 300, status == http.StatusNotFound, status == http.StatusMethodNotAllowed, status == http.StatusBadRequest:
		return true
	}
	return false
}
