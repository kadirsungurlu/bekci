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
	// Kalan güvenlik kuralları (735; docs/GUVENLIK-EKLENTI-730.md §A.7).
	MetricRebootRequired = "reboot_required"  // yeniden başlatma bu kadar gündür bekliyor
	MetricAuthFailed     = "auth_failed"      // başarısız oturum açma sayısı / minutes penceresi (saatlik kova)
	MetricAVSigAge       = "av_signature_age" // Defender imza yaşı (gün); yalnız AV olan sunucuda
	MetricAVOff          = "av_off"           // AV ya da gerçek zamanlı koruma kapalı (eşiksiz)
	MetricFirewallOff    = "firewall_off"     // güvenlik duvarı kapalı (eşiksiz)
	MetricWUFailed       = "wu_failed"        // son 30 günde başarısız Windows güncellemesi (eşiksiz)

	// wuFailedWindow wu_failed kuralının geriye bakış süresi (sn).
	wuFailedWindow = 30 * 86400

	// Zaman çizelgesi olayları (güncellemeler ve yeniden başlatma).
	EventSecUpdatesNew       = "sec_updates_new"
	EventSecUpdatesInstalled = "sec_updates_installed"
	EventRebootRequired      = "reboot_required"
	EventRebootCleared       = "reboot_cleared"
	// EventAccountLocked kilitlenen hesap (Windows 4740); konu kullanıcı adı.
	EventAccountLocked = "account_locked"
	// EventFirewallChanged güvenlik duvarı açıldı/kapandı (data.active).
	EventFirewallChanged = "firewall_changed"
	// EventAVChanged antivirüs koruması açıldı/kapandı (data.ok).
	EventAVChanged = "av_changed"

	maxSecEventPkgs = 20
)

// SecurityMetrics güvenlik raporu ve saatlik denetimle değerlendirilen kurallar
// (metrik örneği bunlara dokunmaz).
var SecurityMetrics = []string{MetricSecUpdatesAge, MetricRebootRequired, MetricAuthFailed, MetricAVSigAge, MetricAVOff, MetricFirewallOff, MetricWUFailed}

// SecurityMetric metrik güvenlik kuralı mı.
func SecurityMetric(m string) bool { return slices.Contains(SecurityMetrics, m) }

// SecurityIntervalFor iş listesine yazılacak aralık (0: rapor toplanmaz).
func SecurityIntervalFor(p store.Probe) int {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return 0
	}
	return SecurityInterval
}

// IngestSecurity ajanın güvenlik raporunu saklar, zaman çizelgesine yazar ve
// güvenlik kurallarını değerlendirir.
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
	if rep.Auth != nil && len(rep.Auth.Locked) > 0 {
		// PK (probe_id,time,kind,subject) yeniden gönderimi yinelemez.
		s.timeline(ctx, lockedEvents(p.ID, rep.Auth.Locked)...)
	}
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
	if f := ch.Firewall; f != nil {
		lvl := store.EventInfo
		if !f.Active {
			lvl = store.EventWarning
		}
		d := map[string]any{"active": f.Active}
		if f.Backend != "" {
			d["backend"] = f.Backend
		}
		out = append(out, timelineEvent(id, t, EventFirewallChanged, "", lvl, d))
	}
	if a := ch.AV; a != nil {
		lvl := store.EventInfo
		if !a.OK() {
			lvl = store.EventWarning
		}
		d := map[string]any{"ok": a.OK(), "service": a.Service, "enabled": a.Enabled, "realtime": a.Realtime}
		out = append(out, timelineEvent(id, t, EventAVChanged, a.Product, lvl, d))
	}
	return out
}

// lockedEvents kilitlenen hesapları zaman çizelgesi olaylarına çevirir.
func lockedEvents(id int64, locked []metrics.SecLock) []store.TimelineEvent {
	out := make([]store.TimelineEvent, 0, len(locked))
	for _, l := range locked {
		if l.User == "" || l.Time <= 0 {
			continue
		}
		var d map[string]any
		if l.Source != "" {
			d = map[string]any{"source": l.Source}
		}
		out = append(out, timelineEvent(id, l.Time, EventAccountLocked, l.User, store.EventWarning, d))
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

// secCheck bir güvenlik kuralının o anki durumu: v olaya yazılan değer,
// fire eşik aşıldı (olay açılır), clear koşul tamamen bitti (olay kapanır;
// ikisi de false ise açık olay sürer), subject olay konusu.
type secCheck struct {
	v           float64
	fire, clear bool
	subject     string
}

// evalSecurity güvenlik kurallarını değerlendirir (docs/GUVENLIK-EKLENTI-730.md
// §A.7). Seviye hep "uyarı"; devre dışı ya da metriği kapalı sunucuda açık
// olay kapanır.
func (s *Service) evalSecurity(ctx context.Context, p store.Probe, now time.Time) error {
	rules, err := s.store.ServerAlerts(ctx, p.ID)
	if err != nil {
		return err
	}
	var st *store.SecRuleState
	for i := range rules {
		a := &rules[i]
		if !SecurityMetric(a.Metric) {
			continue
		}
		a.ProbeID = p.ID
		if st == nil && a.Metric != MetricSecUpdatesAge && a.Metric != MetricAuthFailed {
			v, err := s.store.LoadSecRuleState(ctx, p.ID)
			if err != nil {
				return err
			}
			st = &v
		}
		c, err := s.secCheck(ctx, p.ID, a, st, now)
		if err != nil {
			return err
		}
		live := p.Active && p.Metrics && a.Active
		switch {
		case !a.Firing && live && c.fire:
			s.fire(ctx, p, nil, trendCopy(a), c.v, now, nil, c.subject)
		case a.Firing && (!live || c.clear):
			s.resolve(ctx, p, nil, trendCopy(a), c.v, now, nil)
		case a.Firing:
			if err := s.store.UpdateServerIncidentValue(ctx, a.ID, c.v); err != nil {
				s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
			}
		}
	}
	return nil
}

// secCheck kuralın durumunu hesaplar (st, sec_updates_age ve auth_failed
// dışında dolu).
func (s *Service) secCheck(ctx context.Context, id int64, a *store.ServerAlert, st *store.SecRuleState, now time.Time) (secCheck, error) {
	var c secCheck
	days := func(since int64) float64 { return max(float64(now.Unix()-since)/86400, 0) }
	switch a.Metric {
	case MetricSecUpdatesAge:
		// En eski bekleyen güvenlik paketinin panelde ilk görülmesinden bu
		// yana geçen gün; konu en eski paket, paket kalmayınca kapanır.
		name, first, count, err := s.store.OldestSecurityPkg(ctx, id)
		if err != nil {
			return c, err
		}
		if count > 0 {
			c.v = days(first)
		}
		c.fire, c.clear, c.subject = count > 0 && c.v >= a.Threshold, count == 0, name
	case MetricRebootRequired:
		if st.RebootRequired && st.RebootSince > 0 {
			c.v = days(st.RebootSince)
		}
		c.fire, c.clear = st.RebootRequired && c.v >= a.Threshold, !st.RebootRequired
	case MetricAuthFailed:
		// Saatlik kovalar: pencereyle çakışan saatler sayılır.
		n, ip, err := s.store.SecAuthFailedSince(ctx, id, now.Unix()-int64(max(a.Minutes, 1))*60)
		if err != nil {
			return c, err
		}
		c.v, c.subject = float64(n), ip
		c.fire, c.clear = c.v >= a.Threshold, c.v < a.Threshold
	case MetricAVSigAge:
		av := st.Report.AV
		if av == nil || av.SigUpdated <= 0 {
			c.clear = true
			break
		}
		c.v, c.subject = days(av.SigUpdated), av.SigVersion
		c.fire, c.clear = c.v >= a.Threshold, c.v < a.Threshold
	case MetricAVOff:
		av := st.Report.AV
		bad := av != nil && !av.OK()
		if av != nil {
			c.subject = av.Product
		}
		c.fire, c.clear = bad, !bad
	case MetricFirewallOff:
		f := st.Report.Firewall
		bad := f != nil && !f.Active
		if f != nil {
			c.subject = f.Backend
		}
		c.fire, c.clear = bad, !bad
	case MetricWUFailed:
		var n int
		var newest metrics.SecWUFail
		if w := st.Report.WU; w != nil {
			for _, f := range w.Failed {
				if f.Time < now.Unix()-wuFailedWindow {
					continue
				}
				n++
				if f.Time > newest.Time {
					newest = f
				}
			}
		}
		c.v, c.subject = float64(n), newest.KB
		if c.subject == "" {
			c.subject = newest.Title
		}
		c.fire, c.clear = n > 0, n == 0
	}
	return c, nil
}
