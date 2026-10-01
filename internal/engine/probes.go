package engine

import (
	"context"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// ProbeOfflineAfter bu süre boyunca hiç istek göndermeyen kontrol noktası
// çevrimdışı sayılır. Kontrol noktası iş listesini uzun yoklamayla (en fazla
// ~20 sn bekletilen istekler; api.probeHold) ister, sonuçları birkaç saniyede
// bir gönderir; bekletme bu süreden epey kısa kalmalıdır.
const ProbeOfflineAfter = 90 * time.Second

// ProbePollAfter uzun yoklama öncesi iş listesi yenileme aralığı (aralık
// birimi cinsinden; üretimde saniye). Sunucu artık poll_after=1 ile uzun
// yoklama yaptırır; bu değer ilk sonuç süresinde (locRulesFor) ajanın işi en
// geç ne zaman öğreneceğinin temkinli üst sınırı olarak kullanılır.
const ProbePollAfter = 30

// ProbeOnline kontrol noktası etkin ve yakın zamanda görülmüş mü?
func ProbeOnline(p store.Probe, now time.Time) bool {
	return p.Active && p.LastSeenAt > 0 && now.Unix()-p.LastSeenAt <= int64(ProbeOfflineAfter/time.Second)
}

// watchProbes kontrol noktalarının çevrimiçi/çevrimdışı değişimlerini izler;
// değişimde canlı akışa "probe" olayı yayınlar ve loga yazar. İlk taramada
// yalnızca mevcut durum öğrenilir (açılışta sahte değişim olayı olmasın).
func (e *Engine) watchProbes(ctx context.Context) {
	defer e.bg.Done()
	t := time.NewTicker(e.probeWatchEvery)
	defer t.Stop()
	var known map[int64]bool
	for {
		known = e.scanProbes(ctx, known)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (e *Engine) scanProbes(ctx context.Context, known map[int64]bool) map[int64]bool {
	probes, err := e.store.ListProbesOfKind(ctx, store.ProbeKindLocation)
	if err != nil {
		if ctx.Err() == nil {
			e.log.Error("kontrol noktaları okunamadı", "hata", err)
		}
		return known
	}
	now := e.now()
	next := make(map[int64]bool, len(probes))
	for _, p := range probes {
		online := ProbeOnline(p, now)
		next[p.ID] = online
		if known == nil {
			continue
		}
		if was, ok := known[p.ID]; ok && was == online {
			continue
		} else if !ok && !online {
			continue // yeni eklenmiş, henüz bağlanmamış
		}
		if online {
			e.log.Info("kontrol noktası çevrimiçi", "kontrol_noktasi", p.Name, "adres", p.LastIP, "sürüm", p.Version)
		} else {
			e.log.Warn("kontrol noktası çevrimdışı", "kontrol_noktasi", p.Name, "son_gorulme", time.Unix(p.LastSeenAt, 0).Format(time.RFC3339))
		}
		e.hub.Publish("probe", map[string]any{
			"probe_id": p.ID, "name": p.Name, "online": online, "last_seen_at": p.LastSeenAt,
		})
		e.probeTransition(ctx, p, online, now)
	}
	return next
}

// probeTransition kontrol noktasının çevrimdışı/çevrimiçi geçişinde olay açar
// veya kapatır ve "çevrimdışında bildir" açıksa bağlı kanallara bildirim
// gönderir. Tolerans ProbeOfflineAfter'dır (90 sn): ajanın birkaç saniyelik
// yeniden başlaması bildirim üretmez. İlk taramada (known == nil) çağrılmaz:
// ana sunucu uzun kapalı kaldıysa açılışta tüm kontrol noktaları çevrimdışı
// görünür, o an bildirim gitmez. Devre dışı bırakılan kontrol noktası için
// açık olay bildirimsiz kapanır; devre dışıyken yeni olay açılmaz.
func (e *Engine) probeTransition(ctx context.Context, p store.Probe, online bool, now time.Time) {
	if !online {
		if !p.Active || !p.NotifyOffline {
			if !p.Active {
				if _, _, err := e.store.ResolveProbeIncident(ctx, p.ID, now.Unix()); err != nil {
					e.log.Error("kontrol noktası olayı kapatılamadı", "kontrol_noktasi", p.Name, "hata", err)
				}
			}
			return
		}
		id, err := e.store.OpenProbeIncident(ctx, p.ID, now.Unix(), p.LastSeenAt)
		if err != nil {
			e.log.Error("kontrol noktası olayı açılamadı", "kontrol_noktasi", p.Name, "hata", err)
		}
		ev := e.probeEvent(notify.KindProbeOffline, p, now, id)
		ev.LastSeen = time.Unix(p.LastSeenAt, 0)
		e.notifier.Notify(ev)
		return
	}
	id, started, err := e.store.ResolveProbeIncident(ctx, p.ID, now.Unix())
	if err != nil {
		e.log.Error("kontrol noktası olayı kapatılamadı", "kontrol_noktasi", p.Name, "hata", err)
		return
	}
	if id == 0 {
		return // açık olay yoktu (bildirim kapalıydı ya da hiç çevrimdışı olmadı)
	}
	ev := e.probeEvent(notify.KindProbeOnline, p, now, id)
	ev.Downtime = now.Sub(time.Unix(started, 0))
	if p.NotifyOffline {
		e.notifier.Notify(ev)
	}
}

func (e *Engine) probeEvent(kind string, p store.Probe, now time.Time, incidentID int64) notify.Event {
	ev := notify.Event{Kind: kind, ProbeID: p.ID, MonitorName: p.Name, MonitorType: "probe", Target: p.LastIP,
		Metric: "offline", Time: now, IncidentID: incidentID}
	if e.cfg.BaseURL != "" {
		ev.URL = e.cfg.BaseURL + "/#/settings/probes"
		if incidentID != 0 {
			ev.IncidentURL = e.IncidentURL(incidentID)
		}
	}
	return ev
}

// probeGoneGrace (aralık birimi; üretimde 5 sn) bağlantısı kopan kontrol
// noktasının son "çalışıyor" sonucu,
// monitörün genel kararında bu süre boyunca geçerli sayılır: konum
// kartı hemen "sonuç yok" olur ama ajanın birkaç saniyelik yeniden başlaması
// yanlış kesinti bildirimi üretmez. Süre dolunca konum hesaptan düşer.
const probeGoneGrace = 5

func (e *Engine) goneGrace() time.Duration { return probeGoneGrace * e.cfg.Unit }

// SetProbeConnected uzun yoklama yapan kontrol noktasının bağlantı durumunu
// bildirir (API: bağlantı koptu ve ProbeGoneAfter içinde yeniden bağlanmadı).
// Kopunca konumları hemen "sonuç yok" sayılır; eskime süresi (3 aralık)
// beklenmez. Durum değişince ilgili monitörler uyandırılır.
func (e *Engine) SetProbeConnected(probeID int64, connected bool) {
	e.mu.Lock()
	if e.ctx != nil && e.ctx.Err() != nil {
		// Kapanış: tüm uzun yoklamalar aynı anda kopar; her kontrol noktası
		// için uyarı yazmanın ve monitörleri uyandırmanın anlamı yok.
		e.mu.Unlock()
		return
	}
	_, was := e.gone[probeID]
	if connected {
		delete(e.gone, probeID)
	} else if !was {
		e.gone[probeID] = e.now()
	}
	var wake []*locationSet
	if was == connected { // değişti
		for _, r := range e.runners {
			if r.locs != nil && r.locs.byProbe[probeID] != nil {
				wake = append(wake, r.locs)
			}
		}
	}
	e.mu.Unlock()
	if was != connected {
		return
	}
	e.wakeProbe(wake)
	if !connected {
		e.log.Warn("kontrol noktasının bağlantısı koptu; konumları sonuç yok sayılıyor", "kontrol_noktasi", probeID)
		// Tolerans dolunca monitörler yeniden değerlendirilir.
		time.AfterFunc(e.goneGrace()+e.goneGrace()/20, func() {
			if _, ok := e.probeGoneAt(probeID); ok {
				e.wakeProbe(e.probeRunners(probeID))
			}
		})
	}
}

func (e *Engine) probeRunners(probeID int64) []*locationSet {
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []*locationSet
	for _, r := range e.runners {
		if r.locs != nil && r.locs.byProbe[probeID] != nil {
			out = append(out, r.locs)
		}
	}
	return out
}

func (e *Engine) wakeProbe(sets []*locationSet) {
	for _, ls := range sets {
		select {
		case ls.wake <- struct{}{}:
		default:
		}
	}
}

// probeGoneAt kontrol noktasının bağlantısı ne zaman koptu (ok=false: bağlı).
func (e *Engine) probeGoneAt(probeID int64) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	at, ok := e.gone[probeID]
	return at, ok
}

func (e *Engine) probeGone(probeID int64) bool {
	_, ok := e.probeGoneAt(probeID)
	return ok
}

// ProbeDisconnected kontrol noktasının uzun yoklama bağlantısı kopmuş mu (API ve testler için)?
func (e *Engine) ProbeDisconnected(probeID int64) bool { return e.probeGone(probeID) }
