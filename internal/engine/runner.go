package engine

import (
	"context"
	"math"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// unknown: monitörün onaylanmış (UP/DOWN) bir durumu henüz yok.
const unknown = -1

type runner struct {
	e       *Engine
	m       store.Monitor
	checker check.Checker
	cancel  context.CancelFunc
	done    chan struct{}
	pushCh  chan check.Result

	retries   int // art arda başarısız deneme (PENDING'de)
	confirmed int // son onaylanmış durum: StatusUp, StatusDown veya unknown
	downBeats int // DOWN'dayken art arda kontrol sayısı (hatırlatma için)
}

// initialConfirmed yeniden başlatmada sahte bildirim gitmesin diye son
// onaylanmış durumu veritabanından çıkarır. Kayıtlı durum UP/DOWN ise o
// geçerlidir; bekliyorsa (tekrar deneniyordu) açık olay DOWN'u, daha önce
// kaydedilmiş bir değişim UP'ı gösterir; hiçbiri yoksa bilinmiyor.
//
// DOWN kaydedilmiş ama olayı açılamamışsa (ör. yazım yarıda kaldıysa) olay
// burada tamamlanır; böylece kesinti süresi düzelince doğru hesaplanır.
func (r *runner) initialConfirmed(ctx context.Context) int {
	started, err := r.e.store.OpenIncidentStart(ctx, r.m.ID)
	hasIncident := err == nil && started > 0
	switch {
	case r.m.Status == store.StatusDown:
		if err == nil && !hasIncident {
			since := r.m.LastChangeAt
			if since == 0 {
				since = r.m.LastCheckAt
			}
			if since == 0 {
				since = r.e.now().Unix()
			}
			if err := r.e.store.OpenIncident(ctx, r.m.ID, since, r.m.LastMessage); err != nil {
				r.e.log.Error("eksik olay tamamlanamadı", "monitor", r.m.Name, "hata", err)
			}
		}
		return store.StatusDown
	case r.m.Status == store.StatusUp:
		return store.StatusUp
	case hasIncident:
		return store.StatusDown
	case r.m.LastChangeAt > 0:
		return store.StatusUp
	}
	return unknown
}

func (r *runner) unit(n int) time.Duration { return time.Duration(n) * r.e.cfg.Unit }

func (r *runner) nextDelay() time.Duration {
	if r.m.Status == store.StatusPending && r.m.LastCheckAt > 0 {
		return r.unit(r.m.RetryInterval)
	}
	return r.unit(r.m.Interval)
}

func (r *runner) loop(ctx context.Context) {
	defer close(r.done)
	first := r.e.jitter(r.unit(r.m.Interval))
	if r.m.Type == check.TypePush {
		// Push'ta ilk kontrol şimdiden tam bir aralık sonra: uygulama kapalıyken
		// veya monitör durdurulmuşken gelemeyen sinyaller yüzünden sahte DOWN olmasın.
		first = r.unit(r.m.Interval)
	}
	timer := time.NewTimer(first)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case res := <-r.pushCh:
			r.process(res)
			resetTimer(timer, r.nextDelay())
		case <-timer.C:
			var res check.Result
			if r.m.Type == check.TypePush {
				res = r.checker.Check(ctx, r.m.Config) // "sinyal gelmedi"
			} else {
				res = r.runCheck(ctx)
			}
			if ctx.Err() != nil {
				return // durduruldu: yarım kalan kontrol kaydedilmez
			}
			r.process(res)
			timer.Reset(r.nextDelay())
		}
	}
}

func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

func (r *runner) runCheck(ctx context.Context) (res check.Result) {
	select {
	case r.e.sem <- struct{}{}:
	case <-ctx.Done():
		return check.Result{PingMs: -1}
	}
	defer func() { <-r.e.sem }()
	defer func() {
		if p := recover(); p != nil {
			r.e.log.Error("kontrol paniği", "monitor", r.m.Name, "panik", p)
			res = check.Result{PingMs: -1, Message: "İç hata"}
		}
	}()
	cctx, cancel := context.WithTimeout(ctx, r.unit(r.m.Timeout))
	defer cancel()
	return r.checker.Check(cctx, r.m.Config)
}

// process bir sonucu durum makinesinden geçirir, kaydeder ve gerekirse bildirir.
//
// Veritabanı işlemleri runner'ın context'ini değil kendi kısa süreli
// context'ini kullanır: monitör düzenlenirken veya uygulama kapanırken
// kontrol sonucu, olay ve durum yarım yazılmış halde kalmasın.
func (r *runner) process(res check.Result) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := r.e.now()
	if r.m.UpsideDown {
		res.Up = !res.Up
		if !res.Up {
			res.Message = "Ters mod: hedef erişilebilir (" + res.Message + ")"
		}
	}

	status := store.StatusDown
	switch {
	case res.Up:
		status = store.StatusUp
	case r.confirmed != store.StatusDown && r.retries < r.m.MaxRetries:
		status = store.StatusPending
		r.retries++
	}
	if status != store.StatusPending {
		r.retries = 0
	}

	var lastChange int64
	prevConfirmed := r.confirmed
	if status != store.StatusPending && status != prevConfirmed {
		lastChange = now.Unix()
		r.confirmed = status
	}

	err := r.e.store.RecordBeat(ctx, store.BeatUpdate{
		Beat:         store.Beat{MonitorID: r.m.ID, Time: now.Unix(), Status: status, PingMs: res.PingMs, Message: res.Message},
		LastChangeAt: lastChange,
	})
	if err != nil {
		// Kaydedilemeyen sonuç için bildirim gönderilmez; durum bellekte de
		// değişmez, bir sonraki kontrol aynı geçişi tekrar dener.
		r.e.log.Error("kontrol sonucu kaydedilemedi", "monitor", r.m.Name, "hata", err)
		r.confirmed = prevConfirmed
		return
	}
	r.m.Status, r.m.LastCheckAt, r.m.LastMessage = status, now.Unix(), res.Message
	r.m.LastPingMs = res.PingMs
	if lastChange > 0 {
		r.m.LastChangeAt = lastChange
	}

	switch {
	case status == store.StatusDown && prevConfirmed != store.StatusDown:
		r.downBeats = 0
		if err := r.e.store.OpenIncident(ctx, r.m.ID, now.Unix(), res.Message); err != nil {
			r.e.log.Error("olay açılamadı", "monitor", r.m.Name, "hata", err)
		}
		r.e.log.Warn("monitör DOWN", "monitor", r.m.Name, "neden", res.Message)
		r.notify(notify.KindDown, now, res.Message, 0)

	case status == store.StatusUp && prevConfirmed == store.StatusDown:
		started, err := r.e.store.ResolveIncident(ctx, r.m.ID, now.Unix())
		if err != nil {
			r.e.log.Error("olay kapatılamadı", "monitor", r.m.Name, "hata", err)
		}
		var downtime time.Duration
		if started > 0 {
			downtime = now.Sub(time.Unix(started, 0))
		}
		r.e.log.Info("monitör tekrar UP", "monitor", r.m.Name, "kesinti", downtime.Round(time.Second))
		r.notify(notify.KindUp, now, res.Message, downtime)

	case status == store.StatusDown:
		r.downBeats++
		if r.m.ResendEvery > 0 && r.downBeats%r.m.ResendEvery == 0 {
			started, _ := r.e.store.OpenIncidentStart(ctx, r.m.ID)
			var downtime time.Duration
			if started > 0 {
				downtime = now.Sub(time.Unix(started, 0))
			}
			r.notify(notify.KindReminder, now, res.Message, downtime)
		}
	}

	if res.Cert != nil && r.m.Type == "http" {
		r.handleCert(ctx, now, res.Cert)
	}

	r.e.hub.Publish("beat", map[string]any{
		"monitor_id": r.m.ID, "status": status, "time": now.Unix(), "ping": res.PingMs,
		"message": res.Message, "last_change_at": r.m.LastChangeAt, "cert_expires_at": r.m.CertExpiresAt,
	})
}

func (r *runner) notify(kind string, now time.Time, msg string, downtime time.Duration) {
	r.e.notifier.Notify(notify.Event{
		Kind: kind, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: r.checker.Target(r.m.Config), Message: msg, Time: now, Downtime: downtime,
		URL: r.e.MonitorURL(r.m.ID),
	})
}

// handleCert sertifika bilgisini günceller ve eşiğe girildiyse bir kez uyarır.
func (r *runner) handleCert(ctx context.Context, now time.Time, cert *check.CertInfo) {
	notAfter := cert.NotAfter.Unix()
	if notAfter != r.m.CertExpiresAt || cert.Issuer != r.m.CertIssuer {
		if err := r.e.store.UpdateCert(ctx, r.m.ID, notAfter, cert.Issuer); err != nil {
			r.e.log.Error("sertifika bilgisi yazılamadı", "monitor", r.m.Name, "hata", err)
			return
		}
		r.m.CertExpiresAt, r.m.CertIssuer = notAfter, cert.Issuer
	}
	cfg := check.HTTPConfigOf(r.m.Config)
	if cfg.CertExpiry != nil && !*cfg.CertExpiry {
		return
	}
	daysLeft := int(math.Floor(cert.NotAfter.Sub(now).Hours() / 24))
	threshold, ok := certThreshold(daysLeft, r.e.settings.Load().CertDays)
	if !ok {
		return
	}
	sent, err := r.e.store.MarkCertNotice(ctx, r.m.ID, notAfter, threshold)
	if err != nil || !sent {
		return
	}
	r.e.notifier.Notify(notify.Event{
		Kind: notify.KindCert, MonitorID: r.m.ID, MonitorName: r.m.Name, MonitorType: r.m.Type,
		Target: cfg.URL, Time: now, CertDays: daysLeft, CertExpires: cert.NotAfter,
		CertIssuer: cert.Issuer, URL: r.e.MonitorURL(r.m.ID),
	})
}

// certThreshold kalan güne uyan en dar eşiği seçer: 10 gün kala eşikler
// [21 14 7 3 1] ise 14 döner. Böylece yeni eklenen, bitmek üzere olan bir
// sertifika için tüm eşiklerin bildirimi aynı anda gitmez.
func certThreshold(daysLeft int, thresholds []int) (int, bool) {
	best, ok := math.MaxInt, false
	for _, t := range thresholds {
		if daysLeft <= t && t < best {
			best, ok = t, true
		}
	}
	return best, ok
}
