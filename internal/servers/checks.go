package servers

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
	"github.com/kadirsungurlu/bekci/internal/eklentiajan"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Denetim sonuçları ve kuralları (iş 741, docs/GUVENLIK-EKLENTI-730.md §B.3-B.5).
//
// Kurallar server_alerts'te metric=check:
//   - mount "<ad>": durum kuralı. Eşik bit maskesi (store.CheckStatus*): hangi
//     durumlar tetikler (varsayılan yalnız critical). Durum minutes boyunca
//     sürerse olay (1 = hemen); seviye critical durumda kritik, diğerinde uyarı.
//   - mount "<ad>/<metrik>": eşik kuralı (Threshold kritik, WarnThreshold
//     isteğe bağlı uyarı; Below ise küçük değer kötü). Süre bellekte izlenir.
const (
	MetricCheck = store.MetricCheck
	// EventCheckUnknown denetim 3 tur üst üste unknown (olay değil, çizelge).
	EventCheckUnknown = "check_unknown"
	checkUnknownTurns = 3

	maxCheckErr    = 64
	maxCheckStderr = 1 << 10
	// checkClockSkew ajan saatine güvenilen fark; dışındaysa panel saati.
	checkClockSkew = 5 * time.Minute
	checkMaxAge    = 24 * time.Hour // kuyrukta bekleyen en eski sonuç
)

// CheckReport ajanın gönderdiği tek sonuç (eklentiajan.Result tel biçimi).
type CheckReport = eklentiajan.Result

// StatusCode durumun sayısal değeri (olay değeri): ok 0 … unknown 3.
func StatusCode(st string) float64 {
	switch eklenti.Status(st) {
	case eklenti.Warning:
		return 1
	case eklenti.Critical:
		return 2
	case eklenti.Unknown:
		return 3
	}
	return 0
}

func statusBit(st string) int {
	switch eklenti.Status(st) {
	case eklenti.Warning:
		return store.CheckStatusWarning
	case eklenti.Critical:
		return store.CheckStatusCritical
	case eklenti.Unknown:
		return store.CheckStatusUnknown
	}
	return 0
}

// ValidCheckName denetim/eklenti adı (manifest kimliği kuralı).
func ValidCheckName(s string) bool { return eklentiajan.IsID(s) }

// sanitizeCheck sonucu doğrular ve kayıt biçimine çevirir; ok=false atılır.
func sanitizeCheck(in CheckReport, now time.Time) (store.CheckResult, bool) {
	if !ValidCheckName(in.Name) || (in.Source != "plugin" && in.Source != "builtin") {
		return store.CheckResult{}, false
	}
	st := in.Status
	if !st.Valid() {
		st = eklenti.Unknown
	}
	t := time.UnixMilli(in.T)
	if in.T <= 0 || t.After(now.Add(checkClockSkew)) || t.Before(now.Add(-checkMaxAge)) {
		t = now
	}
	out := store.CheckResult{
		Name: in.Name, Source: in.Source, Version: max(in.Version, 0), T: t.Unix(),
		DurationMs: min(max(in.DurationMs, 0), 3600_000), Status: string(st),
		Message: cut(in.Message, eklenti.MaxMessage), Error: cut(in.Error, maxCheckErr),
		Stderr: cutBytes(in.Stderr, maxCheckStderr), Dropped: max(in.Dropped, 0), Truncated: in.Truncated,
	}
	ms := make([]eklenti.Metric, 0, len(in.Metrics))
	seen := map[string]bool{}
	for _, m := range in.Metrics {
		if len(ms) >= eklenti.MaxMetrics || !eklenti.ValidName(m.Name) || seen[m.Name] ||
			math.IsNaN(m.Value) || math.IsInf(m.Value, 0) {
			out.Dropped++
			continue
		}
		seen[m.Name] = true
		m.Unit = cut(m.Unit, 16)
		ms = append(ms, m)
		out.Samples = append(out.Samples, store.CheckSample{Metric: m.Name, Value: m.Value})
	}
	out.Metrics, _ = json.Marshal(ms)
	return out, true
}

func cut(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func cutBytes(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// IngestChecks ajanın denetim sonuçlarını saklar, zaman çizelgesine yazar ve
// denetim kurallarını değerlendirir. Yalnız sunucu ajanı.
func (s *Service) IngestChecks(ctx context.Context, p store.Probe, list []CheckReport) error {
	if p.Kind != store.ProbeKindServer {
		return nil
	}
	now := s.now()
	for _, in := range list {
		r, ok := sanitizeCheck(in, now)
		if !ok {
			continue
		}
		ch, err := s.store.SaveCheckResult(ctx, p.ID, r, now.Unix())
		if errors.Is(err, store.ErrCheckLimit) {
			s.log.Debug("denetim sınırı dolu; sonuç atıldı", "sunucu", p.Name, "denetim", r.Name)
			continue
		}
		if err != nil {
			return err
		}
		if ch.New {
			if err := s.store.EnsureCheckStatusRule(ctx, p.ID, r.Name); err != nil {
				return err
			}
		}
		if ch.Stale {
			continue
		}
		if ch.UnknownStreak == checkUnknownTurns {
			d := map[string]any{"turns": checkUnknownTurns}
			if r.Message != "" {
				d["message"] = r.Message
			}
			if r.Error != "" {
				d["error"] = r.Error
			}
			s.timeline(ctx, timelineEvent(p.ID, r.T, EventCheckUnknown, r.Name, store.EventWarning, d))
		}
		if err := s.evalCheck(ctx, p, r, ch.Enabled, ch.StatusSince, now); err != nil {
			return err
		}
	}
	return nil
}

// checkBreach metrik kuralının bellekteki ihlal başlangıcı (sunucu/konu).
type checkKey struct {
	probe int64
	mount string
}

// evalCheck denetimin kurallarını son sonuçla değerlendirir.
func (s *Service) evalCheck(ctx context.Context, p store.Probe, r store.CheckResult, enabled bool, since int64, now time.Time) error {
	rules, err := s.store.CheckAlerts(ctx, p.ID, r.Name)
	if err != nil {
		return err
	}
	values := map[string]float64{}
	for _, m := range r.Samples {
		values[m.Metric] = m.Value
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	if s.checkSince == nil {
		s.checkSince = map[checkKey]int64{}
	}
	for i := range rules {
		a := &rules[i]
		a.ProbeID = p.ID
		live := p.Active && a.Active && enabled
		held := func(from int64) bool { return a.Minutes <= 1 || r.T-from >= int64(a.Minutes)*60 }
		var v float64
		var lvl string // "" = koşul yok
		if a.Mount == r.Name {
			v = StatusCode(r.Status)
			if statusBit(r.Status)&int(a.Threshold) != 0 {
				lvl = store.LevelWarning
				if r.Status == string(eklenti.Critical) {
					lvl = store.LevelCritical
				}
				if !a.Firing && !held(since) {
					lvl = "" // süre dolmadı
				}
			}
		} else {
			_, metric, _ := strings.Cut(a.Mount, "/")
			val, ok := values[metric]
			if !ok {
				continue // metrik bu turda yok (zaman aşımı vb.): durum korunur
			}
			v = val
			lvl = metricLevel(a, v)
			k := checkKey{p.ID, a.Mount}
			if lvl == "" {
				delete(s.checkSince, k)
			} else {
				from, ok := s.checkSince[k]
				if !ok {
					from = r.T
					s.checkSince[k] = from
				}
				if !a.Firing && !held(from) {
					lvl = ""
				}
			}
		}
		detail := func(ev *notify.Event) { ev.Message = r.Message }
		switch {
		case !a.Firing && live && lvl != "":
			a.Level = lvl
			s.fire(ctx, p, nil, a, v, now, detail, a.Mount)
		case a.Firing && (!live || lvl == ""):
			s.resolve(ctx, p, nil, a, v, now, detail)
		case a.Firing && lvl != a.Level:
			s.changeLevel(ctx, p, nil, a, lvl, v, now)
		case a.Firing:
			if err := s.store.UpdateServerIncidentValue(ctx, a.ID, v); err != nil {
				s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
			}
		}
	}
	return nil
}

// metricLevel eşik kuralının seviyesi ("" = eşik aşılmadı). Below: küçük değer kötü.
func metricLevel(a *store.ServerAlert, v float64) string {
	bad := func(t float64) bool {
		if a.Below {
			return v <= t
		}
		return v >= t
	}
	switch {
	case bad(a.Threshold):
		return store.LevelCritical
	case a.WarnThreshold != 0 && bad(a.WarnThreshold):
		return store.LevelWarning
	}
	return ""
}

// ResolveCheck denetim kapatıldığında açık olaylarını kapatır.
func (s *Service) ResolveCheck(ctx context.Context, p store.Probe, name string) error {
	rules, err := s.store.CheckAlerts(ctx, p.ID, name)
	if err != nil {
		return err
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	now := s.now()
	for i := range rules {
		a := &rules[i]
		delete(s.checkSince, checkKey{p.ID, a.Mount})
		if a.Firing {
			a.ProbeID = p.ID
			s.resolve(ctx, p, nil, a, 0, now, nil)
		}
	}
	return nil
}
