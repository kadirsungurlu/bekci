package probe

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// Kalp atışı (POST /api/probe/mesh): ajan panel aralığında (PanelSec; ilk değer
// 5 sn, yanıttaki zamanlamayla güncellenir) panele yaşam belirtisi, eş
// ölçümleri ve dinleme portunu gönderir. Panel etkin zamanlamayı ve eş listesi
// sürümünü döner; sürüm değişince eş listesi (GET /api/probe/mesh/peers)
// çekilir. Sonuç tamponu da aynı aralıkta toplu gönderilir (flushEvery).
// Kalp atışını tanımayan eski panel (404) uzun aralıkla yeniden denenir.

// MeshSource kalp atışına eklenecek eş ölçümlerini verir (mesh katmanı).
type MeshSource interface {
	Report() []MeshLinkReport
	ListenPort() int
}

// heartbeatState kalp atışı durumu. Atomik alanlar başka goroutine'lerden
// okunur; diğerleri yalnız heartbeatLoop goroutine'inde.
type heartbeatState struct {
	panelSec atomic.Int64 // panelin bildirdiği aralık (sn); 0: henüz bilinmiyor
	mainLoss atomic.Int64 // art arda başarısız kalp atışı
	mainRTT  atomic.Int64 // son başarılı kalp atışının gidiş-dönüş süresi (ms); 0: henüz yok

	peersVersion string // son alınan eş listesi sürümü
	unsupported  bool   // panel 404 döndü (log bir kez)
}

// defaultPanelSec panel zamanlamayı bildirene kadarki kalp atışı aralığı (sn).
const defaultPanelSec = 5

// panelEvery kalp atışı aralığı.
func (c *Client) panelEvery() time.Duration {
	n := c.hb.panelSec.Load()
	if n <= 0 {
		n = defaultPanelSec
	}
	return time.Duration(n) * c.cfg.Unit
}

// flushEvery sonuç gönderme aralığı: panel zamanlamayı bildirdiyse panel
// aralığı, değilse (eski panel) FlushEvery.
func (c *Client) flushEvery() time.Duration {
	if n := c.hb.panelSec.Load(); n > 0 {
		return time.Duration(n) * c.cfg.Unit
	}
	return c.cfg.FlushEvery
}

// MainLoss art arda başarısız kalp atışı sayısı (probe -> Main yönü).
func (c *Client) MainLoss() int { return int(c.hb.mainLoss.Load()) }

func (c *Client) heartbeatLoop(ctx context.Context) {
	wait := time.Duration(0) // ilk kalp atışı hemen
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = c.heartbeatOnce(ctx)
	}
}

// heartbeatOnce bir kalp atışı gönderir; bir sonrakine kadar beklenecek süreyi döner.
func (c *Client) heartbeatOnce(ctx context.Context) time.Duration {
	hb := MeshHeartbeat{MainLoss: c.MainLoss(), MainRTTms: c.hb.mainRTT.Load(), PeersVersion: c.hb.peersVersion}
	if src := c.cfg.MeshSource; src != nil {
		hb.Links = src.Report()
		hb.ListenPort = src.ListenPort()
	}
	every := c.panelEvery()
	// Yanıt bir aralık içinde gelmezse kayıp sayılır (sonraki tur beklemez).
	hctx, cancel := context.WithTimeout(ctx, every)
	defer cancel()
	var resp MeshHeartbeatResponse
	sent := time.Now()
	status, err := c.call(hctx, http.MethodPost, "/api/probe/mesh", hb, &resp)
	rtt := time.Since(sent)
	if ctx.Err() != nil {
		return every
	}
	switch {
	case err != nil || status == http.StatusTooManyRequests || status >= 500:
		c.hb.mainLoss.Add(1)
		if err == nil {
			c.unreachable("mesh", &statusError{status})
		} else {
			c.unreachable("mesh", err)
		}
		return every
	case status == http.StatusNotFound:
		// Eski panel (ya da mesh'e katılmayan sunucu ajanı): sessizce seyrek dene.
		c.hb.mainLoss.Store(0)
		if !c.hb.unsupported {
			c.hb.unsupported = true
			c.log.Debug("ana sunucu kalp atışını desteklemiyor; seyrek denenecek")
		}
		return c.cfg.HeartbeatRetry
	case status != http.StatusOK:
		// 401/403/400: Main'e ulaşıldı; token/devre dışı durumu iş listesi döngüsünde loglanır.
		c.hb.mainLoss.Store(0)
		return max(every, c.cfg.MaxBackoff)
	}
	c.hb.mainLoss.Store(0)
	c.hb.mainRTT.Store(max(rtt.Milliseconds(), 1))
	c.hb.unsupported = false
	c.reachable("mesh")
	c.up.setConfig(resp.OOB, c.oobLog)
	if t := resp.Timing; t.PanelSec > 0 {
		c.hb.panelSec.Store(int64(t.PanelSec))
		c.up.lossThr.Store(int64(t.LossThreshold))
		if c.cfg.OnMeshTiming != nil {
			c.cfg.OnMeshTiming(t)
		}
	}
	if resp.PeersVersion != "" && resp.PeersVersion != c.hb.peersVersion && c.cfg.OnMeshPeers != nil {
		c.fetchPeers(ctx)
	}
	return c.panelEvery()
}

// fetchPeers eş listesini çeker ve OnMeshPeers'a verir; başarısızsa sürüm
// güncellenmez, sonraki kalp atışında yeniden denenir.
func (c *Client) fetchPeers(ctx context.Context) {
	var peers MeshPeersResponse
	status, err := c.call(ctx, http.MethodGet, "/api/probe/mesh/peers", nil, &peers)
	if err != nil || status != http.StatusOK {
		return
	}
	if peers.Peers == nil {
		peers.Peers = []MeshPeer{}
	}
	c.hb.peersVersion = peers.Version
	c.cfg.OnMeshPeers(peers)
}

type statusError struct{ status int }

func (e *statusError) Error() string { return "sunucu yanıtı: " + http.StatusText(e.status) }
