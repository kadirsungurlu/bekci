package engine

import (
	"context"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Yavaş yanıt uyarısı ("degraded") -------------------------------------------------------
//
// Monitörde eşik (SlowMs) tanımlıysa son SlowChecks başarılı kontrolün
// ortalama yanıt süresi eşiği aşınca bir "yavaş yanıt" olayı açılır ve 🟡
// bildirim gider; ortalama eşiğin %90'ının altına inince olay kapanır ve 🟢
// gider (histerezis: eşiğin hemen çevresinde gidip gelen süre her kontrolde
// bildirim üretmez). Uptime ve monitörün durumu etkilenmez. Çok konumlu
// monitörde birleşik sonucun ortalaması (çalışan konumların ortalaması) kullanılır.
//
// Kesintide (DOWN) pencere sıfırlanır ve açık yavaş yanıt olayı bildirimsiz
// kapanır (🔴 zaten gider); bakımda değerlendirme yapılmaz.

// slowRecoverRatio eşiğin bu oranının altına inince "normale döndü".
const slowRecoverRatio = 0.9

// restoreSlow yeniden başlatmada açık yavaş yanıt olayını bulur; eşik
// kaldırıldıysa olay ilk sonuçta kapatılır (slowStep).
func (r *runner) restoreSlow(ctx context.Context) {
	if id, err := r.e.store.OpenDegradedIncidentID(ctx, r.m.ID); err == nil {
		r.slowID = id
	}
}

// slowStep bir sonuçtan sonra yavaş yanıt durumunu günceller.
func (r *runner) slowStep(ctx context.Context, now time.Time, status int, inMaint bool, res check.Result) {
	if r.m.SlowMs <= 0 {
		// Eşik yok (ya da kaldırıldı): açık olay varsa kapatılır.
		if r.m.Slow || r.slowID != 0 {
			r.endSlow(ctx, now, i18n.T(i18n.TR, "incident.degraded.disabled"), 0, false)
		}
		return
	}
	if inMaint {
		return
	}
	if status != store.StatusUp || res.PingMs < 0 {
		if status == store.StatusDown {
			r.slowPings = r.slowPings[:0]
			if r.m.Slow || r.slowID != 0 {
				r.endSlow(ctx, now, i18n.T(i18n.TR, "incident.degraded.outage"), 0, false)
			}
		}
		return
	}
	n := r.m.SlowChecks
	if n <= 0 {
		n = store.DefaultSlowChecks
	}
	r.slowPings = append(r.slowPings, res.PingMs)
	if len(r.slowPings) > n {
		r.slowPings = r.slowPings[len(r.slowPings)-n:]
	}
	if len(r.slowPings) < n {
		return
	}
	var sum int64
	for _, p := range r.slowPings {
		sum += p
	}
	avg := sum / int64(len(r.slowPings))
	threshold := int64(r.m.SlowMs)
	switch {
	case !r.m.Slow && avg > threshold:
		r.beginSlow(ctx, now, avg, n)
	case r.m.Slow && float64(avg) < float64(threshold)*slowRecoverRatio:
		r.endSlow(ctx, now, "", avg, true)
	case r.m.Slow:
		if err := r.e.store.UpdateDegradedIncident(ctx, r.m.ID, avg); err != nil {
			r.e.log.Error("yavaş yanıt olayı güncellenemedi", "monitor", r.m.Name, "hata", err)
		}
	}
}

func (r *runner) beginSlow(ctx context.Context, now time.Time, avg int64, n int) {
	d := store.DegradedIncidentData{ThresholdMs: r.m.SlowMs, Checks: n, AvgMs: avg, PeakMs: avg, LastMs: avg}
	id, err := r.e.store.StartDegradedIncident(ctx, r.m.ID, now.Unix(), d)
	if err != nil {
		r.e.log.Error("yavaş yanıt olayı açılamadı", "monitor", r.m.Name, "hata", err)
	}
	if err := r.e.store.SetMonitorSlow(ctx, r.m.ID, true); err != nil {
		r.e.log.Error("yavaş durumu yazılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	r.m.Slow, r.slowID = true, id
	r.e.log.Warn("monitör yavaş yanıt veriyor", "monitor", r.m.Name, "ortalama_ms", avg, "esik_ms", r.m.SlowMs)
	r.notifySlow(notify.KindSlow, now, id, avg, n, 0)
}

// endSlow yavaş yanıt olayını kapatır; alert=true ise 🟢 bildirim gider
// (kesintiye dönüşme ve eşik kaldırma sessizdir).
func (r *runner) endSlow(ctx context.Context, now time.Time, msg string, avg int64, alert bool) {
	if avg > 0 {
		if err := r.e.store.UpdateDegradedIncident(ctx, r.m.ID, avg); err != nil {
			r.e.log.Error("yavaş yanıt olayı güncellenemedi", "monitor", r.m.Name, "hata", err)
		}
	}
	id, started, err := r.e.store.ResolveDegradedIncident(ctx, r.m.ID, now.Unix(), msg)
	if err != nil {
		r.e.log.Error("yavaş yanıt olayı kapatılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	if err := r.e.store.SetMonitorSlow(ctx, r.m.ID, false); err != nil {
		r.e.log.Error("yavaş durumu yazılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	r.m.Slow, r.slowID = false, 0
	if id == 0 || !alert {
		return
	}
	n := r.m.SlowChecks
	if n <= 0 {
		n = store.DefaultSlowChecks
	}
	r.e.log.Info("monitörün yanıt süresi normale döndü", "monitor", r.m.Name, "ortalama_ms", avg)
	r.notifySlow(notify.KindSlowResolved, now, id, avg, n, now.Sub(time.Unix(started, 0)))
}

func (r *runner) notifySlow(kind string, now time.Time, incidentID int64, avg int64, n int, downtime time.Duration) {
	ev := notify.Event{
		Kind: kind, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: r.checker.Target(r.m.Config), Time: now, Downtime: downtime,
		Value: float64(avg), Threshold: float64(r.m.SlowMs), Checks: n,
		URL: r.e.MonitorURL(r.m.ID), IncidentID: incidentID,
	}
	if incidentID != 0 {
		ev.IncidentURL = r.e.IncidentURL(incidentID)
	}
	r.e.notifier.Notify(ev)
}
