package engine

import (
	"context"
	"math"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/rdap"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// DomainLookup alan adı sorgusu (rdap.Client; testlerde sahte).
type DomainLookup interface {
	Lookup(ctx context.Context, host string) (rdap.Result, error)
}

// Alan adı sorgusu zamanlaması: alan adı başına günde bir; tarama 10 dakikada
// bir (yeni monitörler ilk taramada sorgulanır). Yalnızca ana sunucu sorgular.
const (
	domainEvery      = 24 * time.Hour
	domainScanEvery  = 10 * time.Minute
	domainFirstDelay = 90 * time.Second
	domainMaxPerScan = 200 // tek taramada en fazla sorgu (çok monitörlü kurulumda yayılır)
)

// SetDomainLookup alan adı bitiş sorgusunu açar (Start'tan önce çağrılır;
// nil = kapalı, testlerde böyle kalır).
func (e *Engine) SetDomainLookup(l DomainLookup) { e.domain = l }

// domainLoop ctx bitene kadar periyodik tarama yapar.
func (e *Engine) domainLoop(ctx context.Context) {
	defer e.bg.Done()
	timer := time.NewTimer(domainFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			e.DomainScan(ctx)
			timer.Reset(domainScanEvery)
		}
	}
}

// DomainScan günlük sorgusu geçmiş monitörlerin alan adlarını sorgular, sonucu
// yazar ve eşiğe girmiş olanlar için bir kez bildirim gönderir. Aynı alan adı
// bir taramada bir kez sorgulanır; ağ hatasında eski bilgi korunur ve bildirim
// gitmez.
func (e *Engine) DomainScan(ctx context.Context) {
	if e.domain == nil {
		return
	}
	now := e.now()
	mons, err := e.store.MonitorsForDomainCheck(ctx, now.Add(-domainEvery).Unix())
	if err != nil {
		e.log.Error("alan adı sorgusu için monitörler okunamadı", "hata", err)
		return
	}
	type outcome struct {
		res rdap.Result
		err error
	}
	cache := map[string]outcome{}
	n := 0
	for _, m := range mons {
		if ctx.Err() != nil {
			return
		}
		host := DomainHost(m)
		key := rdap.Domain(host)
		if key == "" {
			// Alan adı içermeyen tip/hedef (push, grup, IP, yerel ad):
			// sorgulanmaz ama bir daha denenmesin diye işaretlenir.
			e.store.UpdateDomain(ctx, m.ID, store.DomainInfo{Name: host, Status: store.DomainUnsupported, CheckedAt: now.Unix()})
			continue
		}
		o, ok := cache[key]
		if !ok {
			if n >= domainMaxPerScan {
				continue
			}
			n++
			qctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			o.res, o.err = e.domain.Lookup(qctx, host)
			cancel()
			cache[key] = o
		}
		e.applyDomain(ctx, m, o.res, o.err, now)
	}
}

// applyDomain tek monitörün sorgu sonucunu işler.
func (e *Engine) applyDomain(ctx context.Context, m store.Monitor, r rdap.Result, err error, now time.Time) {
	if err != nil {
		e.log.Warn("alan adı sorgulanamadı", "monitor", m.Name, "alan_adi", r.Domain, "hata", err)
		if err := e.store.UpdateDomain(ctx, m.ID, store.DomainInfo{Name: r.Domain, Status: store.DomainError, CheckedAt: now.Unix()}); err != nil {
			e.log.Error("alan adı durumu yazılamadı", "monitor", m.Name, "hata", err)
		}
		return
	}
	info := store.DomainInfo{Name: r.Domain, Status: r.Status, Registrar: r.Registrar, CheckedAt: now.Unix()}
	if r.Status == rdap.StatusOK {
		info.ExpiresAt = r.Expires.Unix()
	}
	if err := e.store.UpdateDomain(ctx, m.ID, info); err != nil {
		e.log.Error("alan adı bilgisi yazılamadı", "monitor", m.Name, "hata", err)
		return
	}
	if r.Status != rdap.StatusOK {
		return
	}
	daysLeft := int(math.Floor(r.Expires.Sub(now).Hours() / 24))
	threshold, ok := certThreshold(daysLeft, e.settings.Load().DomainDays)
	if !ok {
		return
	}
	sent, err := e.store.MarkDomainNotice(ctx, m.ID, info.ExpiresAt, threshold)
	if err != nil || !sent {
		return
	}
	e.notifier.Notify(notify.Event{
		Kind: notify.KindDomain, MonitorID: m.ID, MonitorName: m.Name, MonitorType: m.Type,
		Target: Target(m), Time: now, Domain: r.Domain, DomainDays: daysLeft, DomainExpires: r.Expires,
		DomainRegistrar: r.Registrar, DomainCritical: store.DomainCriticalDays, URL: e.MonitorURL(m.ID),
	})
}

// DomainHost monitörün alan adı sorgusuna konu ana makine adı; alan adı
// taşımayan tipler (push, grup, ping/IP, veritabanı vb.) için boş. HTTP
// (kelime/JSON dahil), DNS ve TLS sertifikası monitörleri desteklenir.
func DomainHost(m store.Monitor) string {
	switch m.Type {
	case "http":
		return rdap.HostOf(check.HTTPConfigOf(m.Config).URL)
	case "dns":
		return check.DNSConfigOf(m.Config).Host
	case "tlscert":
		return check.TLSCertConfigOf(m.Config).Host
	}
	return ""
}
