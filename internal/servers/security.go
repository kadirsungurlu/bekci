package servers

import (
	"context"
	"slices"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	// SecurityInterval ajanın güvenlik raporu turu (sn); iş listesinde
	// security_interval olarak gider. Ajan yalnız değişince (en geç 12 turda
	// bir) gönderir; paket sorgusu ajanda ayrıca önbelleklidir (saatte bir).
	SecurityInterval = 900
	// MetricSecUpdatesAge en eski bekleyen güvenlik güncellemesinin yaşı (gün).
	MetricSecUpdatesAge = "sec_updates_age"

	// Zaman çizelgesi olayları (güncellemeler ve yeniden başlatma).
	EventSecUpdatesNew       = "sec_updates_new"
	EventSecUpdatesInstalled = "sec_updates_installed"
	EventRebootRequired      = "reboot_required"
	EventRebootCleared       = "reboot_cleared"

	maxSecEventPkgs = 20
)

// SecurityIntervalFor iş listesine yazılacak aralık (0: rapor toplanmaz).
func SecurityIntervalFor(p store.Probe) int {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return 0
	}
	return SecurityInterval
}

// IngestSecurity ajanın güvenlik raporunu saklar, zaman çizelgesine yazar ve
// sec_updates_age kuralını değerlendirir.
func (s *Service) IngestSecurity(ctx context.Context, p store.Probe, rep metrics.Security) error {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return nil
	}
	rep.Sanitize()
	now := s.now()
	ch, err := s.store.SaveSecurity(ctx, p.ID, now.Unix(), rep, rep.Hash())
	if err != nil {
		return err
	}
	s.timeline(ctx, securityChanges(p.ID, ch, now.Unix())...)
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	return s.evalSecurity(ctx, p, now)
}

// securityChanges kayıt farkını zaman çizelgesi olaylarına çevirir.
func securityChanges(id int64, ch store.SecurityChange, t int64) []store.TimelineEvent {
	var out []store.TimelineEvent
	pkgs := func(l []string) map[string]any {
		slices.Sort(l)
		d := map[string]any{"count": len(l)}
		if len(l) > maxSecEventPkgs {
			l = l[:maxSecEventPkgs]
		}
		d["packages"] = l
		return d
	}
	if len(ch.NewSecurity) > 0 {
		out = append(out, timelineEvent(id, t, EventSecUpdatesNew, "", store.EventWarning, pkgs(ch.NewSecurity)))
	}
	if len(ch.Installed) > 0 {
		out = append(out, timelineEvent(id, t, EventSecUpdatesInstalled, "", store.EventInfo, pkgs(ch.Installed)))
	}
	if ch.RebootRequired {
		out = append(out, timelineEvent(id, t, EventRebootRequired, "", store.EventWarning, nil))
	}
	if ch.RebootCleared {
		out = append(out, timelineEvent(id, t, EventRebootCleared, "", store.EventInfo, nil))
	}
	return out
}

// CheckSecurity tüm sunucuların güvenlik kurallarını gözden geçirir: rapor
// gelmese de bekleme günü artar (saatlik, sertifika denetimiyle birlikte).
func (s *Service) CheckSecurity(ctx context.Context) {
	probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindServer)
	if err != nil {
		s.log.Error("güvenlik denetimi: sunucular okunamadı", "hata", err)
		return
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	now := s.now()
	for _, p := range probes {
		if err := ctx.Err(); err != nil {
			return
		}
		if err := s.evalSecurity(ctx, p, now); err != nil {
			s.log.Error("güvenlik denetimi", "sunucu", p.Name, "hata", err)
		}
	}
}

// evalSecurity sec_updates_age kuralı: en eski bekleyen güvenlik paketinin
// panelde ilk görülmesinden bu yana geçen gün eşiği aşarsa olay açılır (konu
// en eski paket), bekleyen güvenlik paketi kalmayınca kapanır. Devre dışı ya
// da metriği kapalı sunucuda açık olay kapanır.
func (s *Service) evalSecurity(ctx context.Context, p store.Probe, now time.Time) error {
	rules, err := s.store.ServerAlerts(ctx, p.ID)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(rules, func(a store.ServerAlert) bool { return a.Metric == MetricSecUpdatesAge })
	if i < 0 {
		return nil
	}
	a := &rules[i]
	a.ProbeID = p.ID
	name, first, count, err := s.store.OldestSecurityPkg(ctx, p.ID)
	if err != nil {
		return err
	}
	var days float64
	if count > 0 {
		days = max(float64(now.Unix()-first)/86400, 0)
	}
	live := p.Active && p.Metrics && a.Active
	switch {
	case !a.Firing && live && count > 0 && days >= a.Threshold:
		b := trendCopy(a)
		s.fire(ctx, p, nil, b, days, now, nil, name)
	case a.Firing && (!live || count == 0):
		b := trendCopy(a)
		s.resolve(ctx, p, nil, b, days, now, nil)
	case a.Firing:
		if err := s.store.UpdateServerIncidentValue(ctx, a.ID, days); err != nil {
			s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
		}
	}
	return nil
}
