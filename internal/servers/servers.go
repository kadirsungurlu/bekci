// Package servers sunucu takibinin sunucu tarafıdır (docs/PLAN.md §12):
// ajanlardan ("uptime probe") gelen örneklerin alınması ve saklanması,
// 10 dk / saatlik özetler (rollup), eşik uyarıları, çevrimdışı takibi ve
// arayüzün gördüğü sunucu görünümü.
//
// Zamanlar sunucunun saatine göredir: örneğin satır zamanı ajanın saati değil,
// alış anının dakikaya yuvarlanmış halidir (iki makinenin saati farklı olabilir).
package servers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kadirsungurlu/bekci/internal/maintenance"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// rebootGrace açılış zamanındaki bu kadarlık fark yeniden başlatma sayılmaz
// (saat kayması / ajanın açılış zamanını yuvarlaması).
const rebootGrace = 60

const (
	// Interval ajanın örnek aralığı (sn); iş listesinde metrics_interval olarak gider.
	Interval = 60
	// historyKeep uyarı değerlendirmesi için bellekte tutulan son örnekler (dk).
	historyKeep = 60
	// checkEvery çevrimdışı kontrolünün aralığı.
	checkEvery = 30 * time.Second
)

// OfflineAfter bu kadar süredir örnek gelmeyen sunucu çevrimdışıdır:
// max(3 dk, 3 × örnek aralığı).
const OfflineAfter = max(3*time.Minute, 3*Interval*time.Second)

// ErrEmptySample örnekte ne ölçüm ne de "toplayamıyorum" nedeni var.
var ErrEmptySample = errors.New("örnekte stats veya unavailable alanı olmalı")

// Notifier bildirimleri gönderir (testlerde sahtesi kullanılır).
type Notifier interface{ Notify(notify.Event) }

// Publisher canlı akışa olay yayınlar (engine.Hub).
type Publisher interface{ Publish(typ string, data any) }

// Service sunucu takibinin durumunu tutar. Ingest ve çevrimdışı taraması
// evalMu ile sıraya girer (özetler ve uyarılar yarışmasın); bellek
// haritaları mu ile korunur.
type Service struct {
	store    *store.Store
	hub      Publisher
	notifier Notifier
	log      *slog.Logger
	now      func() time.Time
	baseURL  string

	evalMu    sync.Mutex
	states    map[int64]string    // çevrimdışı taramasının son gördüğü durum (değişimde yayın)
	armed     map[int64]time.Time // metriği/ajanı yeniden açılan sunucular (çevrimdışı sayacı buradan başlar)
	startedAt time.Time           // Start zamanı: ana sunucu kapalıyken geçen süre ajanın suçu değil

	mu      sync.RWMutex
	latest  map[int64]*sample  // son örnek; anahtar var ama nil: veritabanında da yok
	history map[int64][]point  // son historyKeep dakikanın özet değerleri
	lastRow map[int64]int64    // son 1 dk satırının zamanı (özet sınırı tespiti)
	loaded  map[int64]struct{} // history veritabanından yüklendi mi

	// maint sunucuları kapsayan etkin bakım pencereleri (ReloadMaintenance).
	maint atomic.Pointer[maintenance.Index]

	bg sync.WaitGroup
}

// ReloadMaintenance bakım pencerelerini okuyup sunucu dizinini yeniler
// (açılışta ve pencere değişince API çağırır).
func (s *Service) ReloadMaintenance(ctx context.Context) error {
	windows, err := s.store.ListMaintenance(ctx)
	if err != nil {
		return err
	}
	s.maint.Store(maintenance.NewServerIndex(windows, func(w store.Maintenance, err error) {
		s.log.Error("bakım penceresi derlenemedi", "pencere", w.Title, "hata", err)
	}))
	return nil
}

// InMaintenance sunucu t anında bir bakım penceresinde mi?
func (s *Service) InMaintenance(probeID int64, t time.Time) bool {
	return s.maint.Load().InMaintenance(probeID, t)
}

// sample bellekteki son örnek.
type sample struct {
	t     int64 // satır zamanı (dakika başı, unix sn)
	stats metrics.Stats
}

func New(st *store.Store, hub Publisher, n Notifier, log *slog.Logger) *Service {
	return &Service{
		store: st, hub: hub, notifier: n, log: log, now: time.Now,
		states:  map[int64]string{},
		armed:   map[int64]time.Time{},
		latest:  map[int64]*sample{},
		history: map[int64][]point{},
		lastRow: map[int64]int64{},
		loaded:  map[int64]struct{}{},
	}
}

// SetClock saati değiştirir (testler; Start'tan önce çağrılmalı).
func (s *Service) SetClock(now func() time.Time) { s.now = now }

// SetBaseURL bildirimlerdeki bağlantılar için dış adres (Start'tan önce).
func (s *Service) SetBaseURL(u string) { s.baseURL = u }

// URL arayüzdeki sunucu sayfasının adresi (BaseURL yoksa boş).
func (s *Service) URL(id int64) string {
	if s.baseURL == "" {
		return ""
	}
	return s.baseURL + "/#/servers/" + strconv.FormatInt(id, 10)
}

// Start çevrimdışı taramasını başlatır; ctx iptal edilince durur (bkz. Wait).
func (s *Service) Start(ctx context.Context) {
	s.evalMu.Lock()
	s.startedAt = s.now()
	s.evalMu.Unlock()
	s.bg.Add(1)
	go func() {
		defer s.bg.Done()
		t := time.NewTicker(checkEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.CheckOffline(ctx)
			}
		}
	}()
}

// Wait arka plan işinin bitmesini bekler.
func (s *Service) Wait() { s.bg.Wait() }

// IntervalFor ajanın iş listesine yazılacak örnek aralığı (0: metrik kapalı).
func IntervalFor(p store.Probe) int {
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return 0
	}
	return Interval
}

// Forget silinen ajanın bellekteki verisini atar.
func (s *Service) Forget(id int64) {
	// Önce evalMu: o sırada işlenen bir örnek silinen ajanı haritalara geri eklemesin.
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	s.mu.Lock()
	delete(s.latest, id)
	delete(s.history, id)
	delete(s.lastRow, id)
	delete(s.loaded, id)
	s.mu.Unlock()
	delete(s.states, id)
	delete(s.armed, id)
}

// Arm ajan veya metrik toplama yeniden açıldığında çağrılır: çevrimdışı süresi
// eski son örnekten değil şimdiden sayılır (ajan yeni aralığı öğrenip örnek
// gönderene kadar yanlış uyarı gitmesin).
func (s *Service) Arm(id int64) {
	s.evalMu.Lock()
	s.armed[id] = s.now()
	s.evalMu.Unlock()
}

// Ingest ajandan gelen örneği işler: saklar, özetler, uyarıları değerlendirir
// ve canlı akışa yayınlar. p isteği yapan ajanın güncel kaydıdır.
// Metrik toplama kapalıysa örnek sessizce yok sayılır.
func (s *Service) Ingest(ctx context.Context, p store.Probe, smp metrics.Sample) error {
	smp.Sanitize()
	if smp.Stats == nil && smp.Unavailable == "" {
		return ErrEmptySample
	}
	if p.Kind != store.ProbeKindServer || !p.Metrics {
		return nil // kontrol noktası veya metriği kapalı sunucu: yok sayılır
	}
	prevHost := hostOf(p)
	hostInfo := ""
	if smp.Host != nil {
		b, err := json.Marshal(smp.Host)
		if err != nil {
			return err
		}
		hostInfo = string(b)
		p.HostInfo = hostInfo
	}
	s.evalMu.Lock()
	defer s.evalMu.Unlock()
	now := s.now()
	// Yeniden başlatma: açılış zamanı öncekinden ileriye kaydı (tolerans payıyla).
	if smp.Host != nil && prevHost != nil && prevHost.BootTime > 0 && smp.Host.BootTime > prevHost.BootTime+rebootGrace {
		s.rebooted(ctx, p, smp.Host, now)
	}

	if smp.Stats == nil {
		// Ajan çalışıyor ama metrik toplayamıyor (ör. host bağlanmamış konteyner).
		if err := s.store.SetProbeMetricsNote(ctx, p.ID, smp.Unavailable, hostInfo); err != nil {
			return err
		}
		p.MetricsNote = smp.Unavailable
		s.publish(ctx, p, nil)
		return nil
	}

	t := now.Unix() - now.Unix()%60
	data, err := json.Marshal(smp.Stats)
	if err != nil {
		return err
	}
	prev, err := s.prevRow(ctx, p.ID, t)
	if err != nil {
		return err
	}
	first, err := s.store.SaveServerSample(ctx, store.ServerSample{ProbeID: p.ID, Time: t, At: now.Unix(), Data: data, HostInfo: hostInfo})
	if err != nil {
		return err
	}
	if first {
		// İlk örnek: kuralı ve kanalı olmayan ajana varsayılanlar eklenir.
		if err := s.store.InitServerDefaults(ctx, p.ID, DefaultRules()); err != nil {
			s.log.Error("varsayılan sunucu uyarıları eklenemedi", "sunucu", p.Name, "hata", err)
		}
		s.log.Info("sunucudan ilk metrik geldi", "sunucu", p.Name)
	}
	p.MetricsAt, p.MetricsNote = now.Unix(), ""

	host := hostOf(p)
	if err := s.loadHistory(ctx, p.ID, t); err != nil {
		s.log.Error("sunucu geçmişi okunamadı", "sunucu", p.Name, "hata", err)
	}
	s.remember(p.ID, t, *smp.Stats, host)

	if prev > 0 && prev < t {
		if err := s.rollup(ctx, p.ID, prev, t); err != nil {
			s.log.Error("sunucu özetleri yazılamadı", "sunucu", p.Name, "hata", err)
		}
	}
	rules, err := s.evaluate(ctx, p, host, t, now)
	if err != nil {
		s.log.Error("sunucu uyarıları değerlendirilemedi", "sunucu", p.Name, "hata", err)
	}
	s.publish(ctx, p, rules)
	return nil
}

// rebooted sunucunun yeniden başlatıldığını uyarı geçmişine yazar ve "reboot"
// kuralı açıksa (bakımda değilse) bağlı kanallara bildirir. Olay açılmaz:
// anlık bir durumdur.
func (s *Service) rebooted(ctx context.Context, p store.Probe, host *metrics.Host, now time.Time) {
	s.log.Warn("sunucu yeniden başlatıldı", "sunucu", p.Name, "acilis", time.Unix(host.BootTime, 0).Format(time.RFC3339))
	if err := s.store.RecordServerEvent(ctx, p.ID, MetricReboot, float64(host.BootTime), now.Unix()); err != nil {
		s.log.Error("yeniden başlatma kaydı yazılamadı", "sunucu", p.Name, "hata", err)
	}
	rule, err := s.store.ServerAlertByMetric(ctx, p.ID, MetricReboot)
	if err != nil || !rule.Active || s.notifier == nil || s.InMaintenance(p.ID, now) {
		return
	}
	ev := notify.Event{Kind: notify.KindServerReboot, ProbeID: p.ID, MonitorName: p.Name, MonitorType: "server",
		Metric: MetricReboot, Time: now, URL: s.URL(p.ID), BootTime: time.Unix(host.BootTime, 0), Target: host.Hostname}
	s.notifier.Notify(ev)
}

// prevRow t'den önceki son 1 dk satırının zamanı: bellekte yoksa (açılıştan
// sonraki ilk örnek) veritabanından okunur; özetler yeniden başlatmaya dayanıklı olsun.
func (s *Service) prevRow(ctx context.Context, id, t int64) (int64, error) {
	s.mu.RLock()
	prev, ok := s.lastRow[id]
	s.mu.RUnlock()
	if ok && prev < t {
		return prev, nil
	}
	if ok && prev == t {
		return 0, nil // aynı dakikada ikinci örnek: sınır geçilmedi
	}
	row, err := s.store.LastServerStat(ctx, id, t)
	if errors.Is(err, store.ErrNotFound) {
		return 0, nil
	}
	return row.Time, err
}

// loadHistory ajanın son historyKeep dakikasını (bu açılışta ilk kez)
// veritabanından belleğe alır; yeniden başlatmadan sonra uyarılar beklemesin.
func (s *Service) loadHistory(ctx context.Context, id, t int64) error {
	s.mu.RLock()
	_, done := s.loaded[id]
	s.mu.RUnlock()
	if done {
		return nil
	}
	rows, err := s.store.ServerStats(ctx, id, store.ServerRes1, t-historyKeep*60, t)
	if err != nil {
		return err
	}
	p, _ := s.store.GetProbe(ctx, id)
	host := hostOf(p)
	pts := make([]point, 0, len(rows))
	for _, r := range rows {
		var st metrics.Stats
		if json.Unmarshal(r.Data, &st) != nil {
			continue
		}
		pts = append(pts, pointOf(r.Time, &st, host))
	}
	s.mu.Lock()
	s.history[id] = append(pts, s.history[id]...)
	s.loaded[id] = struct{}{}
	s.mu.Unlock()
	return nil
}

// remember son örneği ve uyarı geçmişini günceller (aynı dakikadaki örnek
// öncekinin yerine geçer).
func (s *Service) remember(id, t int64, st metrics.Stats, host *metrics.Host) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latest[id] = &sample{t: t, stats: st}
	s.lastRow[id] = t
	h := s.history[id]
	if n := len(h); n > 0 && h[n-1].t == t {
		h = h[:n-1]
	}
	h = append(h, pointOf(t, &st, host))
	cut := 0
	for cut < len(h) && h[cut].t <= t-historyKeep*60 {
		cut++
	}
	s.history[id] = slices.Clone(h[cut:])
}

// Latest ajanın son örneği (yoksa nil). Bellekte değilse (açılıştan sonra)
// bir kez veritabanından okunur.
func (s *Service) Latest(ctx context.Context, p store.Probe) *metrics.Stats {
	s.mu.RLock()
	l, ok := s.latest[p.ID]
	s.mu.RUnlock()
	if !ok {
		if p.MetricsAt == 0 {
			return nil
		}
		row, err := s.store.LastServerStat(ctx, p.ID, s.now().Unix()+3600)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Error("son sunucu örneği okunamadı", "sunucu", p.Name, "hata", err)
			return nil
		}
		if err == nil {
			var st metrics.Stats
			if json.Unmarshal(row.Data, &st) == nil {
				l = &sample{t: row.Time, stats: st}
			}
		}
		s.mu.Lock()
		if cur, ok := s.latest[p.ID]; ok {
			l = cur // bu arada yeni örnek geldi
		} else {
			s.latest[p.ID] = l
		}
		s.mu.Unlock()
	}
	if l == nil {
		return nil
	}
	st := l.stats
	return &st
}

// hostOf ajanın kayıtlı host bilgisi (yoksa nil).
func hostOf(p store.Probe) *metrics.Host {
	if p.HostInfo == "" {
		return nil
	}
	var h metrics.Host
	if json.Unmarshal([]byte(p.HostInfo), &h) != nil {
		return nil
	}
	return &h
}

// publish ajanın liste görünümünü canlı akışa yayınlar. rules nil ise
// veritabanından okunur.
func (s *Service) publish(ctx context.Context, p store.Probe, rules []store.ServerAlert) {
	if s.hub == nil {
		return
	}
	if rules == nil {
		var err error
		if rules, err = s.store.ServerAlerts(ctx, p.ID); err != nil {
			s.log.Error("sunucu uyarı kuralları okunamadı", "hata", err)
		}
	}
	s.hub.Publish("server", s.View(ctx, p, rules, false))
}

// Publish ajanın güncel görünümünü yayınlar (ör. ayarı değişince).
func (s *Service) Publish(ctx context.Context, id int64) {
	p, err := s.store.GetProbe(ctx, id)
	if err != nil {
		return
	}
	s.publish(ctx, p, nil)
}
