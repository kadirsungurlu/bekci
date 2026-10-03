package servers

import (
	"context"
	"fmt"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Uyarı metrikleri.
const (
	MetricCPU       = "cpu"       // % (tüm çekirdekler)
	MetricMem       = "mem"       // RAM %
	MetricSwap      = "swap"      // swap %
	MetricDisk      = "disk"      // bölüm doluluğu % (kuralda bölüm yoksa en dolusu)
	MetricLoad      = "load"      // 1 dk yük / mantıksal çekirdek
	MetricTemp      = "temp"      // en sıcak sensör °C
	MetricNet       = "net"       // gelen+giden ağ hızı (Mbit/s)
	MetricOffline   = "offline"   // veri gelmiyor
	MetricContainer = "container" // Docker konteyneri çalışmıyor / yeniden başlıyor (mount = ad; "" = herhangi biri)
	MetricReboot    = "reboot"    // sunucu yeniden başlatıldı (yalnızca bildirim; eşik ve süre yok)
)

// Metrics geçerli uyarı metrikleri (arayüzdeki sırayla).
var Metrics = []string{MetricOffline, MetricCPU, MetricMem, MetricDisk, MetricSwap, MetricLoad, MetricTemp, MetricNet, MetricContainer, MetricReboot}

// ValidMetric metriğin geçerli olup olmadığını söyler.
func ValidMetric(m string) bool {
	for _, x := range Metrics {
		if x == m {
			return true
		}
	}
	return false
}

// Thresholdless eşik kullanmayan metrikler (offline, container, reboot).
func Thresholdless(m string) bool {
	return m == MetricOffline || m == MetricContainer || m == MetricReboot
}

// ThresholdRange metriğin eşik sınırları (eşiksiz metriklerde 0, 0).
func ThresholdRange(metric string) (lo, hi float64) {
	switch metric {
	case MetricLoad:
		return 0.01, 100
	case MetricTemp:
		return 1, 150
	case MetricNet:
		return 0.1, 1000000 // Mbit/s (0,1 – 1.000.000 = 1 Tbit/s)
	case MetricOffline, MetricContainer, MetricReboot:
		return 0, 0
	}
	return 1, 100
}

// DefaultRules ilk örnek geldiğinde kuralı olmayan ajana eklenen kurallar.
// Yeniden başlatma bildirimi varsayılan açıktır (yalnızca bilgi, olay açmaz).
func DefaultRules() []store.ServerAlert {
	return []store.ServerAlert{
		{Metric: MetricOffline, Minutes: 3, Active: true},
		{Metric: MetricCPU, Threshold: 90, Minutes: 10, Active: true},
		{Metric: MetricMem, Threshold: 90, Minutes: 10, Active: true},
		{Metric: MetricDisk, Threshold: 85, Minutes: 1, Active: true},
		{Metric: MetricReboot, Minutes: 1, Active: true},
	}
}

// point bir dakikalık örneğin uyarı değerleri.
type point struct {
	t                          int64
	cpu, mem, swap, disk, load float64
	net                        float64 // gelen+giden Mbit/s
	temp                       float64
	hasTemp                    bool
	disks                      map[string]float64 // bölüm → doluluk %
	fullest                    string             // en dolu bölüm
	// containers ad → durum ("running", "exited", "restarting"…; eski ajan: running).
	// docker: ajan Docker'a erişiyor (false ise konteyner kuralı değerlendirilmez).
	containers map[string]string
	docker     bool
}

func pointOf(t int64, st *metrics.Stats, host *metrics.Host) point {
	p := point{t: t, cpu: st.CPU, mem: st.MemPct(), swap: st.SwapPct(), disk: st.DiskPct(), load: st.Load1}
	if host != nil && host.Threads > 0 {
		p.load = st.Load1 / float64(host.Threads)
	}
	if host != nil {
		p.docker = host.Docker
	}
	p.net = (st.NetRxBps + st.NetTxBps) * 8 / 1e6 // bayt/sn → Mbit/s
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
	if len(st.Containers) > 0 {
		p.containers = make(map[string]string, len(st.Containers))
		for _, c := range st.Containers {
			state := c.State
			if state == "" {
				state = "running"
			}
			p.containers[c.Name] = state
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
	case MetricNet:
		return p.net, true
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

// resolveBelow tetiklenmiş kuralın bitmesi için ortalamanın inmesi gereken
// sınır: eşik eksi histerezis payı. Pay eşiğin %5'idir, en az: yüzde
// metriklerinde 2 puan, yükte 0,05, sıcaklıkta 2 °C (ağda yalnızca %5). Pay
// eşiğin yarısını geçmez (çok küçük eşikte kural hiç bitmez olmasın).
// Böylece eşiğin hemen çevresinde gidip gelen değer uyarıyı her dakika
// başlatıp bitirmez.
func resolveBelow(metric string, threshold float64) float64 {
	margin := threshold * 0.05
	switch metric {
	case MetricLoad:
		margin = max(margin, 0.05)
	case MetricTemp:
		margin = max(margin, 2)
	case MetricNet: // yalnızca eşiğin %5'i
	default: // cpu, mem, swap, disk (%)
		margin = max(margin, 2)
	}
	return threshold - min(margin, threshold/2)
}

// absentAfter tetiklenmiş kuralın değeri (bölüm veya sıcaklık sensörü artık
// raporlanmıyor) bu kadar dakika hiç gelmezse kural kapatılır: kuralın
// penceresi, en az 10 dk (bellekteki geçmişle sınırlı).
func absentAfter(minutes int) int { return min(max(minutes, 10), historyKeep) }

// valueGone ajan örnek göndermeyi sürdürürken (pencerenin en az %80'i) kuralın
// değeri son absentAfter dakikada hiç gelmediyse true: ör. bölüm ayrıldı veya
// sensör kayboldu. Aksi halde kural sonsuza dek tetiklenmiş kalırdı.
func valueGone(h []point, metric, mount string, minutes int, now int64) bool {
	n := absentAfter(minutes)
	from := now - int64(n)*60
	samples := 0
	for _, p := range h {
		if p.t <= from || p.t > now {
			continue
		}
		if _, ok := p.value(metric, mount); ok {
			return false
		}
		samples++
	}
	return samples >= minCoverage(n)
}

// lastValue geçmişte kuralın görülen son değeri (hiç yoksa 0).
func lastValue(h []point, metric, mount string) float64 {
	for i := len(h) - 1; i >= 0; i-- {
		if v, ok := h[i].value(metric, mount); ok {
			return v
		}
	}
	return 0
}

// levelOf ortalamaya göre kuralın seviyesi: kritik eşik (Threshold) aşıldıysa
// critical, uyarı eşiği (WarnThreshold > 0) aşıldıysa warning, yoksa "".
// cur tetiklenmiş kuralın mevcut seviyesidir: histerezis seviye değişiminde
// uygulanır (eşiğin hemen altına inince seviye düşmez).
func levelOf(a *store.ServerAlert, v float64, cur string) string {
	critDown := resolveBelow(a.Metric, a.Threshold)
	warnDown := 0.0
	if a.WarnThreshold > 0 {
		warnDown = resolveBelow(a.Metric, a.WarnThreshold)
	}
	switch {
	case v >= a.Threshold || (cur == store.LevelCritical && v >= critDown):
		return store.LevelCritical
	case a.WarnThreshold > 0 && (v >= a.WarnThreshold || (cur == store.LevelWarning && v >= warnDown)):
		return store.LevelWarning
	}
	return ""
}

// containerDown kuralın konteyneri son `minutes` dakikadır çalışmıyor mu:
// her örnekte (yeterli kapsama ile) ya listede değil ya da durumu running
// değil. Döndürülen değer son durum kodu (store.ContainerState*). Konteyner
// hiç görülmediyse (adı yanlış ya da ajan eski) uyarı verilmez: bilinen bir
// konteyner yalnızca son 60 dk içinde en az bir kez running görülmüş ya da
// listede çalışmıyor halde yer alan konteynerdir.
func containerDown(h []point, name string, minutes int, now int64) (float64, bool) {
	from := now - int64(minutes)*60
	seenEver, samples, downAll := false, 0, true
	lastState := ""
	for _, p := range h {
		if p.containers == nil || !p.docker {
			continue
		}
		st, listed := p.containers[name]
		if listed {
			seenEver = true
		}
		if p.t <= from || p.t > now {
			continue
		}
		samples++
		if listed && st == "running" {
			downAll = false
		}
		lastState = st
	}
	if !seenEver || samples == 0 || samples < minCoverage(minutes) || !downAll {
		return 0, false
	}
	return store.ContainerStateCode(lastState), true
}

// knownContainers son geçmişte en az bir kez görülen konteyner adları.
func knownContainers(h []point) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range h {
		for name := range p.containers {
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// evaluate yeni örnekten sonra ajanın kurallarını değerlendirir ve güncel
// kuralları döner. Pencere ortalaması eşiğe ulaşınca (>=) bir kez "başladı",
// eşiğin histerezis payı kadar altına inince (resolveBelow) bir kez "bitti"
// bildirimi gider. Uyarı eşiği tanımlıysa seviye uyarı ↔ kritik arasında
// değişebilir (seviye değişimi bildirilir). Tetiklenme durumu
// veritabanındadır; yeniden başlatma aynı uyarıyı tekrar bildirmez.
// Bakım penceresindeki sunucuda yeni uyarı açılmaz ve bildirim gitmez.
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
		switch a.Metric {
		case MetricOffline:
			// Veri geldi: çevrimdışı uyarısı bitti.
			if a.Firing {
				s.resolve(ctx, p, host, a, 0, now, nil)
			}
			continue
		case MetricReboot:
			continue // yeniden başlatma Ingest'te açılış zamanından tespit edilir
		}
		if !a.Active {
			continue
		}
		if a.Metric == MetricContainer {
			s.evaluateContainer(ctx, p, host, a, h, t, now)
			continue
		}
		v, ok := windowAvg(h, a.Metric, a.Mount, a.Minutes, t)
		switch {
		case !ok:
			if a.Firing && valueGone(h, a.Metric, a.Mount, a.Minutes, t) {
				gone := func(ev *notify.Event) { ev.GoneMinutes = absentAfter(a.Minutes) }
				s.resolve(ctx, p, host, a, lastValue(h, a.Metric, a.Mount), now, gone)
			}
		case !a.Firing:
			if lvl := levelOf(a, v, ""); lvl != "" {
				a.Level = lvl
				s.fire(ctx, p, host, a, v, now, nil, alertMount(h, a))
			}
		default:
			lvl := levelOf(a, v, a.Level)
			switch {
			case lvl == "":
				s.resolve(ctx, p, host, a, v, now, nil)
			case lvl != a.Level:
				s.changeLevel(ctx, p, host, a, lvl, v, now)
			default:
				// Uyarı sürüyor: olayın son ve en yüksek değeri güncellenir.
				if err := s.store.UpdateServerIncidentValue(ctx, a.ID, v); err != nil {
					s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
				}
			}
		}
	}
	return rules, nil
}

// evaluateContainer "konteyner çalışmıyor" kuralı. Adlı kural o konteyneri,
// adsız kural bilinen (son bir saatte görülen) tüm konteynerleri izler;
// ilk çalışmayan konteyner olayın adı olur, hepsi çalışınca biter.
func (s *Service) evaluateContainer(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, h []point, t int64, now time.Time) {
	if len(h) == 0 || !h[len(h)-1].docker {
		return // ajan Docker'a erişemiyor: ne açılır ne kapanır
	}
	names := []string{a.Mount}
	if a.Mount == "" {
		names = knownContainers(h)
	}
	var downName string
	var code float64
	for _, name := range names {
		if v, down := containerDown(h, name, a.Minutes, t); down {
			downName, code = name, v
			break
		}
	}
	switch {
	case downName != "" && !a.Firing:
		a.Level = store.LevelCritical
		s.fire(ctx, p, host, a, code, now, func(ev *notify.Event) {
			ev.Message = containerStateText(code)
		}, downName)
	case downName == "" && a.Firing:
		mount := a.Mount
		if mount == "" {
			mount, _ = s.store.OpenServerAlertMount(ctx, a.ID)
		}
		s.resolve(ctx, p, host, a, 0, now, func(ev *notify.Event) { ev.Mount = mount })
	}
}

// containerStateText durum kodunun bildirimdeki metni.
func containerStateText(code float64) string {
	switch int(code) {
	case store.ContainerStateExited:
		return "exited"
	case store.ContainerStateRestarting:
		return "restarting"
	case store.ContainerStateOther:
		return "stopped"
	}
	return "Konteyner listede yok"
}

// CheckOffline örnek göndermeyi bırakan ajanlar için çevrimdışı uyarısını
// başlatır (Start 30 sn'de bir çağırır; testlerden de çağrılır). Durumu
// değişen ajanın görünümü canlı akışa yayınlanır. Devre dışı bırakılan ajanın
// veya metriği kapatılanın açık uyarıları bildirimsiz kapatılır. Bakımdaki
// sunucuda çevrimdışı uyarısı açılmaz.
func (s *Service) CheckOffline(ctx context.Context) {
	// Kilit okumadan önce alınır: tam bu arada gelen bir örnek eski
	// metrics_at ile yanlış çevrimdışı uyarısı başlatmasın.
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindServer)
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
				if ok, err := s.store.ResolveServerAlertNote(ctx, rules[i].ID, now.Unix(), store.ServerIncidentResolveDisabled); err != nil {
					s.log.Error("sunucu uyarısı kapatılamadı", "hata", err)
				} else if ok {
					rules[i].Firing, rules[i].FiredAt, changed = false, 0, true
				}
			}
		} else if p.MetricsAt > 0 && !s.InMaintenance(p.ID, now) {
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
					last := time.Unix(p.MetricsAt, 0)
					s.fire(ctx, p, hostOf(p), a, float64(int(stale/time.Minute)), now, func(ev *notify.Event) { ev.LastSeen = last }, "")
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
		Time: now, URL: s.URL(p.ID), Level: a.Level,
	}
	if a.Level == store.LevelWarning {
		ev.Threshold = a.WarnThreshold
	}
	if host != nil {
		ev.Target = host.Hostname
	}
	return ev
}

// fire kuralı tetikler; kural zaten tetiklenmişse (başka yoldan) bildirim gitmez.
// detail verilirse olaya ayrıntı alanlarını (LastSeen, GoneMinutes) yazar;
// metin bildirim dilinde notify tarafında üretilir. Bakım penceresindeki
// sunucuda kural tetiklenmez (olay da açılmaz).
func (s *Service) fire(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, v float64, now time.Time, detail func(*notify.Event), mount string) {
	if s.InMaintenance(p.ID, now) {
		s.log.Debug("sunucu bakımda; uyarı tetiklenmedi", "sunucu", p.Name, "metrik", a.Metric)
		return
	}
	var lastSeen int64
	if a.Metric == MetricOffline {
		lastSeen = p.MetricsAt
	}
	ok, err := s.store.FireServerAlertAt(ctx, *a, v, mount, now.Unix(), lastSeen)
	if err != nil {
		s.log.Error("sunucu uyarısı yazılamadı", "sunucu", p.Name, "metrik", a.Metric, "hata", err)
		return
	}
	a.Firing, a.FiredAt = true, now.Unix()
	if a.Level == "" {
		a.Level = store.LevelCritical
	}
	if !ok {
		return
	}
	s.log.Warn("sunucu uyarısı", "sunucu", p.Name, "metrik", a.Metric, "deger", fmt.Sprintf("%.1f", v), "esik", a.Threshold, "seviye", a.Level)
	if s.notifier != nil {
		ev := s.event(notify.KindServerAlert, p, host, a, v, now, mount)
		// Gönderim sonucu (veya "bağlı kanal yok") olayın işlem geçmişine yazılsın.
		ev.IncidentID, _ = s.store.OpenServerIncidentID(ctx, a.ID)
		if detail != nil {
			detail(&ev)
		}
		s.notifier.Notify(ev)
	}
}

// changeLevel tetiklenmiş kuralın seviyesi değişti (uyarı ↔ kritik): olay
// güncellenir, işlem geçmişine yazılır ve bildirim gider (kritiğe çıkış 🔴,
// uyarıya iniş 🟡 "kritik seviyeden indi"). Bakımda bildirim gitmez.
func (s *Service) changeLevel(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, level string, v float64, now time.Time) {
	prev := a.Level
	if err := s.store.SetServerAlertLevel(ctx, a.ID, level); err != nil {
		s.log.Error("sunucu uyarısının seviyesi yazılamadı", "sunucu", p.Name, "hata", err)
		return
	}
	if err := s.store.SetServerIncidentLevel(ctx, a.ID, level, v, now.Unix()); err != nil {
		s.log.Error("sunucu olayının seviyesi yazılamadı", "sunucu", p.Name, "hata", err)
	}
	a.Level = level
	s.log.Warn("sunucu uyarısının seviyesi değişti", "sunucu", p.Name, "metrik", a.Metric, "onceki", prev, "yeni", level, "deger", fmt.Sprintf("%.1f", v))
	if s.notifier == nil || s.InMaintenance(p.ID, now) {
		return
	}
	ev := s.event(notify.KindServerAlert, p, host, a, v, now, alertMountFiring(ctx, s, a))
	ev.IncidentID, _ = s.store.OpenServerIncidentID(ctx, a.ID)
	if level == store.LevelWarning {
		ev.Downgraded = true
		ev.Threshold = a.Threshold // kritik eşikten indi
		ev.PeakLevel = store.LevelCritical
	}
	s.notifier.Notify(ev)
}

// alertMountFiring tetiklenmiş kuralın olaydaki bölümü (disk) ya da kuralın bölümü.
func alertMountFiring(ctx context.Context, s *Service, a *store.ServerAlert) string {
	if a.Metric == MetricDisk && a.Mount == "" {
		if m, err := s.store.OpenServerAlertMount(ctx, a.ID); err == nil {
			return m
		}
	}
	return a.Mount
}

// resolve tetiklenmiş kuralı bitirir ve "düzeldi" bildirimi gönderir. Bölümsüz
// disk kuralında bildirimdeki bölüm, uyarı başladığında dolan bölümdür
// (geçmiş kaydından okunur; o an en dolu bölüm başka olabilir). Bakımdaki
// sunucuda olay kapanır ama bildirim gitmez.
func (s *Service) resolve(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, v float64, now time.Time, detail func(*notify.Event)) {
	mount := a.Mount
	if (a.Metric == MetricDisk || a.Metric == MetricContainer) && mount == "" {
		var err error
		if mount, err = s.store.OpenServerAlertMount(ctx, a.ID); err != nil {
			s.log.Error("sunucu uyarısının bölümü okunamadı", "sunucu", p.Name, "hata", err)
		}
	}
	if a.Metric != MetricOffline && a.Metric != MetricContainer {
		// Olayın kapanış değeri (son ortalama) kapatmadan önce yazılır.
		if err := s.store.UpdateServerIncidentValue(ctx, a.ID, v); err != nil {
			s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
		}
	}
	// Kapatılacak olay: "düzeldi" bildiriminin sonucu onun işlem geçmişine yazılır.
	incidentID, _ := s.store.OpenServerIncidentID(ctx, a.ID)
	var peak string
	if incidentID != 0 {
		if inc, err := s.store.GetIncident(ctx, incidentID); err == nil {
			peak = store.ParseServerIncidentData(inc.Data).PeakLevel
		}
	}
	prevLevel := a.Level
	ok, err := s.store.ResolveServerAlert(ctx, a.ID, now.Unix())
	if err != nil {
		s.log.Error("sunucu uyarısı kapatılamadı", "sunucu", p.Name, "metrik", a.Metric, "hata", err)
		return
	}
	a.Firing, a.FiredAt, a.Level = false, 0, ""
	if !ok {
		return
	}
	s.log.Info("sunucu uyarısı bitti", "sunucu", p.Name, "metrik", a.Metric, "bolum", mount)
	if s.notifier != nil && !s.InMaintenance(p.ID, now) {
		a.Level = prevLevel
		ev := s.event(notify.KindServerResolved, p, host, a, v, now, mount)
		a.Level = ""
		ev.IncidentID, ev.PeakLevel = incidentID, peak
		if detail != nil {
			detail(&ev)
		}
		s.notifier.Notify(ev)
	}
}
