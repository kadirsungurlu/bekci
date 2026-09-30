// Package engine monitörleri zamanlar, kontrol sonuçlarını durum makinesinden
// geçirir, olayları (incident) yönetir ve bildirimleri tetikler.
//
// Her aktif monitör kendi goroutine'inde (runner) çalışır. Bir runner'ın tüm
// durumu yalnızca kendi goroutine'inde değişir; push sinyalleri de kanal
// üzerinden aynı goroutine'e iletilir. Bu yüzden runner içinde kilit yoktur.
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/maintenance"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/schedule"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Notifier bildirimleri gönderir (testlerde sahtesi kullanılır).
type Notifier interface{ Notify(notify.Event) }

type Config struct {
	MaxConcurrent int           // aynı anda en fazla kaç kontrol çalışır
	BaseURL       string        // bildirimlerdeki bağlantılar için, ör. https://uptime.kadir.app
	Unit          time.Duration // aralık birimi; üretimde saniye, testlerde daha kısa
}

type Engine struct {
	store    *store.Store
	notifier Notifier
	hub      *Hub
	log      *slog.Logger
	cfg      Config
	sem      chan struct{}
	settings atomic.Pointer[store.AppSettings]
	maint    atomic.Pointer[maintenance.Index] // aktif bakım pencereleri (ReloadMaintenance)
	now      func() time.Time

	mu       sync.Mutex
	ctx      context.Context
	runners  map[int64]*runner
	gone     map[int64]bool        // bağlantısı kopan kontrol noktaları (SetProbeConnected)
	monLocks map[int64]*sync.Mutex // monitör başına: Reload/Remove/start sıraya girer (bkz. lockMonitor)
	retired  map[int64]retiredLocs // durdurulan çok konumlu runner'ların konum sonuçları (bkz. adoptLocations)

	jobs *jobsSignal // kontrol noktası iş listesi sürümü (jobs.go)

	bg              sync.WaitGroup // arka plan işleri (watchProbes)
	probeWatchEvery time.Duration  // kontrol noktası durum taraması; testlerde kısaltılır
}

func New(st *store.Store, n Notifier, hub *Hub, log *slog.Logger, cfg Config) *Engine {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 50
	}
	if cfg.Unit <= 0 {
		cfg.Unit = time.Second
	}
	e := &Engine{
		store: st, notifier: n, hub: hub, log: log, cfg: cfg,
		sem:      make(chan struct{}, cfg.MaxConcurrent),
		now:      time.Now,
		runners:  map[int64]*runner{},
		monLocks: map[int64]*sync.Mutex{},
		gone:     map[int64]bool{},
		retired:  map[int64]retiredLocs{},
		jobs:     newJobsSignal(),

		probeWatchEvery: 10 * time.Second,
	}
	def := store.DefaultSettings()
	e.settings.Store(&def)
	return e
}

// Start aktif monitörleri başlatır. ctx iptal edilince tüm runner'lar durur.
func (e *Engine) Start(ctx context.Context) error {
	if s, err := e.store.LoadSettings(ctx); err == nil {
		e.settings.Store(&s)
	}
	if err := e.ReloadMaintenance(ctx); err != nil {
		e.log.Error("bakım pencereleri yüklenemedi", "hata", err)
	}
	monitors, err := e.store.ListMonitors(ctx)
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.ctx = ctx
	e.mu.Unlock()
	e.bg.Add(1)
	go e.watchProbes(ctx) // kontrol noktalarının çevrimiçi/çevrimdışı değişimleri (probes.go)
	n := 0
	for _, m := range monitors {
		if m.Active {
			unlock := e.lockMonitor(m.ID)
			err := e.start(m)
			unlock()
			if err != nil {
				e.log.Error("monitör başlatılamadı", "monitor", m.Name, "hata", err)
				continue
			}
			n++
		}
	}
	e.log.Info("kontrol motoru başladı", "aktif_monitor", n)
	return nil
}

// SetSettings ayarlar değişince çağrılır (SSL eşikleri anında geçerli olur).
func (e *Engine) SetSettings(s store.AppSettings) { e.settings.Store(&s) }

// Reload monitör eklendi/düzenlendi/durduruldu/başlatıldıktan sonra çağrılır.
//
// Aynı monitör için eşzamanlı Reload/Remove çağrıları sıraya girer: aksi
// halde eski ayarı okuyan çağrı, yeni ayarı okuyanın ardından runner'ı
// eski ayarla başlatabilir (ikincisi "zaten çalışıyor" hatası alırdı).
//
// Bitince kontrol noktalarının iş listesi sürümü artar (JobsChanged): uzun
// yoklamada bekleyen ajanlar yeni/değişen işi hemen alır.
func (e *Engine) Reload(ctx context.Context, id int64) error {
	defer e.JobsChanged() // kilit bırakıldıktan sonra (defer sırası)
	defer e.lockMonitor(id)()
	e.stop(id)
	m, err := e.store.GetMonitor(ctx, id)
	if err != nil {
		return err
	}
	if !m.Active {
		return nil
	}
	return e.start(m)
}

// Remove monitörün runner'ını durdurur ve bitmesini bekler. Veritabanı
// değişikliği (durdurma, silme) Remove'dan sonra yapılıyorsa çağıran ayrıca
// JobsChanged çağırmalıdır.
func (e *Engine) Remove(id int64) {
	defer e.JobsChanged()
	defer e.lockMonitor(id)()
	e.stop(id)
}

// lockMonitor monitörün kilidini alır; dönen fonksiyon bırakır. Kilitler
// monitör başına bir kez oluşturulur ve silinmez (bekleyen varken silinen
// kilit sıralamayı bozardı; monitör sayısı kadar küçük bir harita).
func (e *Engine) lockMonitor(id int64) func() {
	e.mu.Lock()
	l := e.monLocks[id]
	if l == nil {
		l = &sync.Mutex{}
		e.monLocks[id] = l
	}
	e.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// stop runner'ı durdurur ve bitmesini bekler (monitör kilidi tutulurken).
func (e *Engine) stop(id int64) {
	e.mu.Lock()
	r := e.runners[id]
	delete(e.runners, id)
	e.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.done
		e.retire(r)
	}
}

// Wait tüm runner'lar durana kadar bekler (kapanışta, ctx iptalinden sonra).
func (e *Engine) Wait() {
	e.mu.Lock()
	rs := make([]*runner, 0, len(e.runners))
	for _, r := range e.runners {
		rs = append(rs, r)
	}
	e.mu.Unlock()
	for _, r := range rs {
		<-r.done
	}
	e.bg.Wait()
}

var (
	ErrPushNotFound = errors.New("push adresi bulunamadı")
	ErrPushPaused   = errors.New("monitör durdurulmuş")
)

// Push /api/push/{token} çağrısını ilgili runner'a iletir.
func (e *Engine) Push(ctx context.Context, token string, up bool, msg string, pingMs int64) error {
	m, err := e.store.MonitorByPushToken(ctx, token)
	if errors.Is(err, store.ErrNotFound) || (err == nil && m.Type != check.TypePush) {
		return ErrPushNotFound
	}
	if err != nil {
		return err
	}
	e.mu.Lock()
	r := e.runners[m.ID]
	e.mu.Unlock()
	if r == nil || !m.Active {
		return ErrPushPaused
	}
	if msg == "" {
		msg = "Push alındı"
		if !up {
			msg = "Push: hata bildirildi"
		}
	}
	// Runner bu arada durdurulduysa (düzenleme/durdurma) sinyal kaybolmasın
	// ve istek kuyruk dolu diye asılı kalmasın: çağırana bildirilir.
	select {
	case <-r.done:
		return ErrPushPaused
	default:
	}
	select {
	case r.pushCh <- check.Result{Up: up, PingMs: pingMs, Message: msg}:
		return nil
	case <-r.done:
		return ErrPushPaused
	case <-ctx.Done():
		return ctx.Err()
	}
}

// MonitorURL arayüzdeki monitör sayfasının adresi.
func (e *Engine) MonitorURL(id int64) string {
	if e.cfg.BaseURL == "" {
		return ""
	}
	return e.cfg.BaseURL + "/#/monitors/" + strconv.FormatInt(id, 10)
}

// Target monitörün hedef açıklaması (liste ve bildirimler için).
func Target(m store.Monitor) string {
	if c, ok := check.Get(m.Type); ok {
		return c.Target(m.Config)
	}
	return ""
}

// start monitörün runner'ını başlatır; çalışan bir runner varsa önce onu
// durdurur (yeni ayar geçerli olsun). Çağıran monitör kilidini tutar.
func (e *Engine) start(m store.Monitor) error {
	checker, ok := check.Get(m.Type)
	if !ok {
		return fmt.Errorf("bilinmeyen monitör tipi: %s", m.Type)
	}
	e.stop(m.ID)
	locs := e.loadLocations(m) // kilit dışında: veritabanı okur
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil {
		return errors.New("motor başlatılmadı")
	}
	if old := e.runners[m.ID]; old != nil {
		return fmt.Errorf("monitör %d zaten çalışıyor", m.ID) // monitör kilidi tutulduğu sürece olmaz
	}
	if locs != nil {
		e.adoptLocations(m, locs)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	ctx = check.WithStatusSource(ctx, e.monitorStatuses) // grup monitörleri alt monitörleri okur
	r := &runner{
		e: e, m: m, checker: checker, cancel: cancel,
		done:   make(chan struct{}),
		pushCh: make(chan check.Result, 8),
		locs:   locs,
		plan:   schedule.Planner{PhaseMs: schedule.PhaseMs(m.ID)},
	}
	r.confirmed = r.initialConfirmed(ctx)
	e.runners[m.ID] = r
	go r.loop(ctx)
	return nil
}

// jitter kontrollerin aynı ana yığılmasını önlemek için ilk kontrolü kaydırır.
func (e *Engine) jitter(interval time.Duration) time.Duration {
	max := min(interval, 10*e.cfg.Unit)
	if max <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(max)))
}
