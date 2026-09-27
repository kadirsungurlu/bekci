package servers

import (
	"context"
	"fmt"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Uyarı metrikleri.
const (
	MetricCPU     = "cpu"     // % (tüm çekirdekler)
	MetricMem     = "mem"     // RAM %
	MetricSwap    = "swap"    // swap %
	MetricDisk    = "disk"    // bölüm doluluğu % (kuralda bölüm yoksa en dolusu)
	MetricLoad    = "load"    // 1 dk yük / mantıksal çekirdek
	MetricTemp    = "temp"    // en sıcak sensör °C
	MetricOffline = "offline" // veri gelmiyor
)

// Metrics geçerli uyarı metrikleri (arayüzdeki sırayla).
var Metrics = []string{MetricOffline, MetricCPU, MetricMem, MetricDisk, MetricSwap, MetricLoad, MetricTemp}

// ValidMetric metriğin geçerli olup olmadığını söyler.
func ValidMetric(m string) bool {
	for _, x := range Metrics {
		if x == m {
			return true
		}
	}
	return false
}

// ThresholdRange metriğin eşik sınırları (offline eşik kullanmaz).
func ThresholdRange(metric string) (lo, hi float64) {
	switch metric {
	case MetricLoad:
		return 0.01, 100
	case MetricTemp:
		return 1, 150
	case MetricOffline:
		return 0, 0
	}
	return 1, 100
}

// DefaultRules ilk örnek geldiğinde kuralı olmayan ajana eklenen kurallar.
func DefaultRules() []store.ServerAlert {
	return []store.ServerAlert{
		{Metric: MetricOffline, Minutes: 3, Active: true},
		{Metric: MetricCPU, Threshold: 90, Minutes: 10, Active: true},
		{Metric: MetricMem, Threshold: 90, Minutes: 10, Active: true},
		{Metric: MetricDisk, Threshold: 85, Minutes: 1, Active: true},
	}
}

// point bir dakikalık örneğin uyarı değerleri.
type point struct {
	t                          int64
	cpu, mem, swap, disk, load float64
	temp                       float64
	hasTemp                    bool
	disks                      map[string]float64 // bölüm → doluluk %
	fullest                    string             // en dolu bölüm
}

func pointOf(t int64, st *metrics.Stats, host *metrics.Host) point {
	p := point{t: t, cpu: st.CPU, mem: st.MemPct(), swap: st.SwapPct(), disk: st.DiskPct(), load: st.Load1}
	if host != nil && host.Threads > 0 {
		p.load = st.Load1 / float64(host.Threads)
	}
	p.temp, p.hasTemp = st.TempMax()
	if len(st.Disks) > 0 {
		p.disks = make(map[string]float64, len(st.Disks))
		best := -1.0
		for _, d := range st.Disks {
			v := 0.0
			if d.Total > 0 {
				v = 100 * float64(d.Used) / float64(d.Total)
			}
			p.disks[d.Mount] = v
			if v > best {
				best, p.fullest = v, d.Mount
			}
		}
	}
	return p
}

// value kuralın bu örnekteki değeri. Disk kuralı bir bölüme bağlıysa o
// bölümün doluluğu (bölüm bu örnekte yoksa değer yok), değilse en dolu bölüm.
func (p point) value(metric, mount string) (float64, bool) {
	if metric == MetricDisk && mount != "" {
		v, ok := p.disks[mount]
		return v, ok
	}
	switch metric {
	case MetricCPU:
		return p.cpu, true
	case MetricMem:
		return p.mem, true
	case MetricSwap:
		return p.swap, true
	case MetricDisk:
		return p.disk, true
	case MetricLoad:
		return p.load, true
	case MetricTemp:
		return p.temp, p.hasTemp
	}
	return 0, false
}

// minCoverage pencerede bulunması gereken en az örnek sayısı: dakikaların
// %80'i (en az 1). Açılıştan hemen sonra veya ajan kesik kesik gönderirken
// birkaç örnekle yanlış uyarı başlamasın/bitmesin; eksikse değerlendirme atlanır.
func minCoverage(minutes int) int { return max(1, minutes*8/10) }

// windowAvg son `minutes` dakikalık penceredeki (now dahil) örneklerin
// ortalaması. Yeterli örnek yoksa ok=false.
func windowAvg(h []point, metric, mount string, minutes int, now int64) (float64, bool) {
	from := now - int64(minutes)*60
	sum, n := 0.0, 0
	for _, p := range h {
		if p.t <= from || p.t > now {
			continue
		}
		if v, ok := p.value(metric, mount); ok {
			sum += v
			n++
		}
	}
	if n == 0 || n < minCoverage(minutes) {
		return 0, false
	}
	return sum / float64(n), true
}

// evaluate yeni örnekten sonra ajanın kurallarını değerlendirir ve güncel
// kuralları döner. Histerezis: ortalama eşiği geçince bir kez "başladı",
// eşiğe veya altına inince bir kez "bitti" bildirimi gider. Tetiklenme
// durumu veritabanındadır; yeniden başlatma aynı uyarıyı tekrar bildirmez.
func (s *Service) evaluate(ctx context.Context, p store.Probe, host *metrics.Host, t int64, now time.Time) ([]store.ServerAlert, error) {
	rules, err := s.store.ServerAlerts(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	h := s.history[p.ID]
	s.mu.RUnlock()
	for i := range rules {
		a := &rules[i]
		a.ProbeID = p.ID
		if a.Metric == MetricOffline {
			// Veri geldi: çevrimdışı uyarısı bitti.
			if a.Firing {
				s.resolve(ctx, p, host, a, 0, now)
			}
			continue
		}
		if !a.Active {
			continue
		}
		v, ok := windowAvg(h, a.Metric, a.Mount, a.Minutes, t)
		switch {
		case !ok:
		case !a.Firing && v > a.Threshold:
			s.fire(ctx, p, host, a, v, now, "", alertMount(h, a))
		case a.Firing && v <= a.Threshold:
			s.resolve(ctx, p, host, a, v, now)
		}
	}
	return rules, nil
}

// CheckOffline örnek göndermeyi bırakan ajanlar için çevrimdışı uyarısını
// başlatır (Start 30 sn'de bir çağırır; testlerden de çağrılır). Durumu
// değişen ajanın görünümü canlı akışa yayınlanır. Devre dışı bırakılan ajanın
// veya metriği kapatılanın açık uyarıları bildirimsiz kapatılır.
func (s *Service) CheckOffline(ctx context.Context) {
	// Kilit okumadan önce alınır: tam bu arada gelen bir örnek eski
	// metrics_at ile yanlış çevrimdışı uyarısı başlatmasın.
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	probes, err := s.store.ListProbes(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("ajanlar okunamadı", "hata", err)
		}
		return
	}
	all, err := s.store.AllServerAlerts(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error("sunucu uyarı kuralları okunamadı", "hata", err)
		}
		return
	}
	now := s.now()
	for _, p := range probes {
		rules := all[p.ID]
		changed := false
		if !p.Active || !p.Metrics {
			for i := range rules {
				if !rules[i].Firing {
					continue
				}
				if ok, err := s.store.ResolveServerAlert(ctx, rules[i].ID, now.Unix()); err != nil {
					s.log.Error("sunucu uyarısı kapatılamadı", "hata", err)
				} else if ok {
					rules[i].Firing, rules[i].FiredAt, changed = false, 0, true
				}
			}
		} else if p.MetricsAt > 0 {
			// Süre son örnekten, ana sunucunun açılışından veya ajanın yeniden
			// açılmasından (hangisi yeniyse) sayılır.
			since := time.Unix(p.MetricsAt, 0)
			if s.startedAt.After(since) {
				since = s.startedAt
			}
			if t := s.armed[p.ID]; t.After(since) {
				since = t
			}
			stale := now.Sub(since)
			for i := range rules {
				a := &rules[i]
				if a.Metric != MetricOffline || !a.Active || a.Firing {
					continue
				}
				limit := max(time.Duration(a.Minutes)*time.Minute, OfflineAfter)
				if stale > limit {
					a.ProbeID = p.ID
					msg := "Son veri: " + time.Unix(p.MetricsAt, 0).Local().Format("02.01.2006 15:04:05")
					s.fire(ctx, p, hostOf(p), a, float64(int(stale/time.Minute)), now, msg, "")
					changed = true
				}
			}
		}
		state := State(p, now)
		if old, ok := s.states[p.ID]; changed || (ok && old != state) {
			s.publish(ctx, p, rules)
		}
		s.states[p.ID] = state
	}
}

// alertMount disk uyarısının ilgili olduğu bölüm: kuraldaki bölüm, yoksa son
// örnekteki en dolu bölüm.
func alertMount(h []point, a *store.ServerAlert) string {
	if a.Metric != MetricDisk {
		return ""
	}
	if a.Mount != "" || len(h) == 0 {
		return a.Mount
	}
	return h[len(h)-1].fullest
}

func (s *Service) event(kind string, p store.Probe, host *metrics.Host, a *store.ServerAlert, v float64, now time.Time, mount string) notify.Event {
	ev := notify.Event{
		Kind: kind, ProbeID: p.ID, MonitorName: p.Name, MonitorType: "server",
		Metric: a.Metric, Mount: mount, Value: v, Threshold: a.Threshold, Minutes: a.Minutes,
		Time: now, URL: s.URL(p.ID),
	}
	if host != nil {
		ev.Target = host.Hostname
	}
	return ev
}

// fire kuralı tetikler; kural zaten tetiklenmişse (başka yoldan) bildirim gitmez.
func (s *Service) fire(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, v float64, now time.Time, msg, mount string) {
	ok, err := s.store.FireServerAlert(ctx, *a, v, mount, now.Unix())
	if err != nil {
		s.log.Error("sunucu uyarısı yazılamadı", "sunucu", p.Name, "metrik", a.Metric, "hata", err)
		return
	}
	a.Firing, a.FiredAt = true, now.Unix()
	if !ok {
		return
	}
	s.log.Warn("sunucu uyarısı", "sunucu", p.Name, "metrik", a.Metric, "deger", fmt.Sprintf("%.1f", v), "esik", a.Threshold)
	if s.notifier != nil {
		ev := s.event(notify.KindServerAlert, p, host, a, v, now, mount)
		ev.Message = msg
		s.notifier.Notify(ev)
	}
}

// resolve tetiklenmiş kuralı bitirir ve "düzeldi" bildirimi gönderir.
func (s *Service) resolve(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, v float64, now time.Time) {
	ok, err := s.store.ResolveServerAlert(ctx, a.ID, now.Unix())
	if err != nil {
		s.log.Error("sunucu uyarısı kapatılamadı", "sunucu", p.Name, "metrik", a.Metric, "hata", err)
		return
	}
	a.Firing, a.FiredAt = false, 0
	if !ok {
		return
	}
	s.log.Info("sunucu uyarısı bitti", "sunucu", p.Name, "metrik", a.Metric)
	if s.notifier != nil {
		s.notifier.Notify(s.event(notify.KindServerResolved, p, host, a, v, now, a.Mount))
	}
}
