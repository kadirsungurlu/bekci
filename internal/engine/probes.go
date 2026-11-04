package engine

import (
	"context"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// ProbeLegacyOfflineAfter kalp atışı (POST /api/probe/mesh) göndermeyen eski
// ajanlar için yedek kural: bu süre boyunca hiç istek göndermeyen kontrol
// noktası çevrimdışı sayılır. Kalp atışı gönderen ajanın eşiği
// store.EffectiveProbeTiming(...).OfflineAfter()'dır (varsayılan 15 sn).
// Uzun yoklama bekletmesi (api.probeHold) bu süreden epey kısa kalmalıdır.
const ProbeLegacyOfflineAfter = 90 * time.Second

// ProbePollAfter uzun yoklama öncesi iş listesi yenileme aralığı (aralık
// birimi cinsinden; üretimde saniye). Sunucu artık poll_after=1 ile uzun
// yoklama yaptırır; bu değer ilk sonuç süresinde (locRulesFor) ajanın işi en
// geç ne zaman öğreneceğinin temkinli üst sınırı olarak kullanılır.
const ProbePollAfter = 30

// ProbeOnline kontrol noktası etkin ve çevrimiçi mi? Kalp atışı gönderen
// ajan için son kalp atışından beri etkin zamanlamanın OfflineAfter süresi
// geçmemiş olmalı; hiç kalp atışı göndermemiş (eski) ajan için son görülme
// + ProbeLegacyOfflineAfter (bkz. probeOnlineAt).
func (e *Engine) ProbeOnline(p store.Probe, now time.Time) bool {
	last, ok := e.lastHeartbeat(p.ID)
	return probeOnlineAt(p, last, ok, e.Settings(), now)
}

// probeOnlineAt çevrimiçi kuralı. Kalp atışı biliniyor ama son görülme
// ondan ProbeLegacyOfflineAfter'dan fazla yeniyse ajan kalp atışını bırakmış
// (eski sürüme dönmüş) sayılır ve yedek kural geçerli olur.
func probeOnlineAt(p store.Probe, lastHB time.Time, haveHB bool, set store.AppSettings, now time.Time) bool {
	if !p.Active {
		return false
	}
	legacySec := int64(ProbeLegacyOfflineAfter / time.Second)
	legacy := p.LastSeenAt > 0 && now.Unix()-p.LastSeenAt <= legacySec
	if !haveHB || p.LastSeenAt-lastHB.Unix() > legacySec {
		return legacy
	}
	return now.Sub(lastHB) <= store.EffectiveProbeTiming(p, set).OfflineAfter()
}

// ProbeLate çevrimiçi sayılan ajanın kalp atışı iki panel aralığından uzun
// süredir gelmiyor: çevrimdışı eşiği (LossThreshold x aralık) dolmadan erken
// uyarı (canlı topoloji "gecikiyor"). Eşik 2 ve altıysa erken uyarı yok.
func (e *Engine) ProbeLate(p store.Probe, now time.Time) bool {
	last, ok := e.lastHeartbeat(p.ID)
	set := e.Settings()
	if !heartbeatBased(p, last, ok) || !probeOnlineAt(p, last, ok, set, now) {
		return false
	}
	t := store.EffectiveProbeTiming(p, set)
	if t.LossThreshold <= 2 {
		return false
	}
	return now.Sub(last) > time.Duration(2*t.PanelSec)*time.Second+500*time.Millisecond
}

// heartbeatBased ajanın çevrimiçi durumu kalp atışından mı hesaplanıyor.
func heartbeatBased(p store.Probe, lastHB time.Time, haveHB bool) bool {
	return haveHB && p.LastSeenAt-lastHB.Unix() <= int64(ProbeLegacyOfflineAfter/time.Second)
}

// ProbeHeartbeat ajanın kalp atışı alındı (API: POST /api/probe/mesh). Main ->
// probe kenarı kopuk işaretliyse kalkar ve konumlar yeniden değerlendirilir.
func (e *Engine) ProbeHeartbeat(probeID int64, at time.Time) {
	e.mu.Lock()
	e.heartbeats[probeID] = at
	_, wasDown := e.mainDown[probeID]
	delete(e.mainDown, probeID)
	e.mu.Unlock()
	if wasDown {
		e.wakeProbe(e.probeRunners(probeID))
	}
}

func (e *Engine) lastHeartbeat(probeID int64) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	at, ok := e.heartbeats[probeID]
	return at, ok
}

// loadHeartbeats açılışta son kalp atışlarını probe_links'ten (src=0) okur;
// böylece yeniden başlatmada kalp atışı gönderen ajanlar eski kurala düşmez.
func (e *Engine) loadHeartbeats(ctx context.Context) {
	links, err := e.store.ListProbeLinks(ctx)
	if err != nil {
		e.log.Error("mesh bağlantıları okunamadı", "hata", err)
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, l := range links {
		if l.Src == store.MeshMainID && l.LastOKAt > 0 {
			if _, ok := e.heartbeats[l.Dst]; !ok {
				e.heartbeats[l.Dst] = time.Unix(l.LastOKAt, 0)
			}
		}
	}
}

// setMainDown Main -> probe kenarının kopukluğunu işaretler/kaldırır. Kopuk
// kenar konumlar için uzun yoklama kopması gibi davranır (probeGoneAt):
// konum "sonuç yok" olur, linkHealthy false döner. Değiştiyse true.
func (e *Engine) setMainDown(probeID int64, down bool, at time.Time) bool {
	e.mu.Lock()
	_, was := e.mainDown[probeID]
	if down && !was {
		e.mainDown[probeID] = at
	} else if !down && was {
		delete(e.mainDown, probeID)
	}
	e.mu.Unlock()
	if was == down {
		return false
	}
	e.wakeProbe(e.probeRunners(probeID))
	return true
}

// markMainLinkDown kalp atışı kesilen probe için Main -> probe kenarını
// "down" yazar (loss_streak: kaçırılan panel aralığı sayısı) ve yayınlar.
func (e *Engine) markMainLinkDown(ctx context.Context, p store.Probe, lastHB time.Time, now time.Time) {
	t := store.EffectiveProbeTiming(p, e.Settings())
	loss := 0
	if t.PanelSec > 0 {
		loss = int(now.Sub(lastHB) / (time.Duration(t.PanelSec) * time.Second))
	}
	l := store.ProbeLink{Src: store.MeshMainID, Dst: p.ID, State: store.LinkDown, LossStreak: loss,
		LastOKAt: lastHB.Unix(), UpdatedAt: now.Unix()}
	if err := e.store.UpsertProbeLinks(ctx, []store.ProbeLink{l}); err != nil {
		e.log.Error("mesh bağlantısı yazılamadı", "kontrol_noktasi", p.Name, "hata", err)
		return
	}
	e.hub.Publish("mesh", map[string]any{"links": []store.ProbeLink{l}})
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
	e.syncUplinks(probes)
	now := e.now()
	set := e.Settings()
	next := make(map[int64]bool, len(probes))
	meshNodes := e.meshNodes(probes)
	for _, p := range probes {
		lastHB, haveHB := e.lastHeartbeat(p.ID)
		online := probeOnlineAt(p, lastHB, haveHB, set, now)
		next[p.ID] = online
		// Kalp atışı kesildi: Main -> probe kenarı kopuk (konumlar hemen
		// "sonuç yok"); kenar satırı geçişte bir kez yazılır.
		hbDown := !online && p.Active && heartbeatBased(p, lastHB, haveHB)
		if e.setMainDown(p.ID, hbDown, now) && hbDown {
			e.markMainLinkDown(ctx, p, lastHB, now)
		}
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
		_, quiet := meshNodes[p.ID]
		e.probeTransition(ctx, p, online, quiet, now)
	}
	e.evalMesh(ctx, probes, next, now)
	return next
}

// probeTransition kontrol noktasının çevrimdışı/çevrimiçi geçişinde olay açar
// veya kapatır ve "çevrimdışında bildir" açıksa bağlı kanallara bildirim
// gönderir. Tolerans kalp atışı eşiğidir (varsayılan 15 sn; eski ajanda
// ProbeLegacyOfflineAfter, 90 sn): ajanın birkaç saniyelik yeniden başlaması
// bildirim üretmez. İlk taramada (known == nil) çağrılmaz:
// ana sunucu uzun kapalı kaldıysa açılışta tüm kontrol noktaları çevrimdışı
// görünür, o an bildirim gitmez. Devre dışı bırakılan kontrol noktası için
// açık olay bildirimsiz kapanır; devre dışıyken yeni olay açılmaz. quiet:
// kontrol noktası mesh kararına katılıyor; olay kaydı açılır/kapanır ama
// bildirimi mesh olayı taşır (tekil bildirim gitmez).
func (e *Engine) probeTransition(ctx context.Context, p store.Probe, online, quiet bool, now time.Time) {
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
		if quiet {
			return // mesh olayı bildirir (tüm bağlantılar + yorum)
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
	if p.NotifyOffline && !quiet {
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
//
// Durum gerçekten değiştiyse true döner (çağıran ör. sunucu görünümünü yayınlar).
func (e *Engine) SetProbeConnected(probeID int64, connected bool) bool {
	e.mu.Lock()
	if e.ctx != nil && e.ctx.Err() != nil {
		// Kapanış: tüm uzun yoklamalar aynı anda kopar; her ajan için uyarı
		// yazmanın ve monitörleri uyandırmanın anlamı yok.
		e.mu.Unlock()
		return false
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
		return false
	}
	e.wakeProbe(wake)
	if !connected {
		e.log.Warn("ajanın bağlantısı koptu", "ajan", probeID)
		// Tolerans dolunca monitörler yeniden değerlendirilir.
		time.AfterFunc(e.goneGrace()+e.goneGrace()/20, func() {
			if _, ok := e.probeGoneAt(probeID); ok {
				e.wakeProbe(e.probeRunners(probeID))
			}
		})
	}
	return true
}

// ProbeGoneAt ajanın uzun yoklama bağlantısı ne zaman koptu (ok=false: bağlı
// ya da bilinmiyor; eski ajanlar izlenmez).
func (e *Engine) ProbeGoneAt(probeID int64) (time.Time, bool) { return e.probeGoneAt(probeID) }

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
// Uzun yoklama kopması ya da kalp atışı kesilmesi (Main -> probe kenarı
// "down"; scanProbes) sayılır; ikisi de varsa erken olanı.
func (e *Engine) probeGoneAt(probeID int64) (time.Time, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	at, ok := e.gone[probeID]
	if md, down := e.mainDown[probeID]; down && (!ok || md.Before(at)) {
		at, ok = md, true
	}
	return at, ok
}

func (e *Engine) probeGone(probeID int64) bool {
	_, ok := e.probeGoneAt(probeID)
	return ok
}

// ProbeDisconnected kontrol noktasının uzun yoklama bağlantısı kopmuş mu (API ve testler için)?
func (e *Engine) ProbeDisconnected(probeID int64) bool { return e.probeGone(probeID) }
