package engine

import (
	"context"
	"strconv"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// OOB ikinci hat (460). Ajan ana hattan panele ulaşamayınca OOB hattına geçer
// ve bunu her istekte X-Probe-Uplink başlığıyla bildirir (api/probes_oob.go).
// OOB'deyken ajanın denetimleri yine OS yönlendirmesiyle (bozuk) ana hattan
// çıkar: o konumun sonuçları karara katılmaz (location.noVote), konum kartı
// "OOB üzerinden, karara katılmıyor" notuyla "bilinmiyor" görünür.

// SetProbeUplink ajanın etkin hattını bildirir. Durum değiştiyse ilgili
// monitörler yeniden değerlendirilir; notify=true ise geçiş/dönüş bildirimi
// gider (kontrol noktasında çevrimdışı bildirimi açıksa; sunucu ajanında
// bağlı kanallar varsa). Çağıran geçişin gerçekten olduğunu garanti eder
// (store.SetProbeUplink karşılaştır-ve-yaz); bellekteki durum syncUplinks ile
// önceden güncellenmiş olabilir, bildirim yine gider.
func (e *Engine) SetProbeUplink(ctx context.Context, p store.Probe, oob bool, now time.Time, notifyChange bool) {
	if e.setUplink(p.ID, oob) {
		e.wakeProbe(e.probeRunners(p.ID))
	}
	if oob {
		e.log.Warn("ajan ana hatta erişemiyor, OOB üzerinden bağlı", "ajan", p.Name, "hat", p.OOBLabel())
	} else {
		e.log.Info("ajan ana hatta döndü", "ajan", p.Name)
	}
	e.hub.Publish("probe", map[string]any{"probe_id": p.ID, "name": p.Name, "uplink": uplinkOf(oob), "uplink_since": now.Unix()})
	if !notifyChange || (p.Kind == store.ProbeKindLocation && !p.NotifyOffline) || !p.Active {
		return
	}
	kind := notify.KindUplinkRestored
	if oob {
		kind = notify.KindUplinkFailover
	}
	ev := e.probeEvent(kind, p, now, 0)
	ev.Metric, ev.UplinkName = "", p.OOBLabel()
	if since := p.UplinkSince; !oob && since > 0 && since <= now.Unix() {
		ev.Downtime = now.Sub(time.Unix(since, 0))
	}
	if p.Kind == store.ProbeKindServer && e.cfg.BaseURL != "" {
		ev.URL = e.cfg.BaseURL + "/#/servers/" + strconv.FormatInt(p.ID, 10)
	}
	e.notifier.Notify(ev)
}

func uplinkOf(oob bool) string {
	if oob {
		return store.UplinkOOB
	}
	return store.UplinkPrimary
}

// setUplink hat durumunu yazar; değiştiyse true.
func (e *Engine) setUplink(probeID int64, oob bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.oob == nil {
		e.oob = map[int64]bool{}
	}
	if e.oob[probeID] == oob {
		return false
	}
	if oob {
		e.oob[probeID] = true
	} else {
		delete(e.oob, probeID)
	}
	return true
}

// ProbeOnOOB ajan OOB hattında mı (sonuçları karara katılmaz)?
func (e *Engine) ProbeOnOOB(probeID int64) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.oob[probeID]
}

// syncUplinks açılışta ve her taramada kayıttaki hat durumunu belleğe alır
// (bildirim göndermez; panel yeniden başlasa da OOB'deki konum oy vermez).
func (e *Engine) syncUplinks(probes []store.Probe) {
	for _, p := range probes {
		oob := p.Active && p.OOBIP != "" && p.OnOOB()
		if e.setUplink(p.ID, oob) {
			e.wakeProbe(e.probeRunners(p.ID))
		}
	}
}
