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

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/maintenance"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
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

	mu      sync.Mutex
	ctx     context.Context
	runners map[int64]*runner
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
		sem:     make(chan struct{}, cfg.MaxConcurrent),
		now:     time.Now,
		runners: map[int64]*runner{},
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
	n := 0
	for _, m := range monitors {
		if m.Active {
			if err := e.start(m); err != nil {
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
func (e *Engine) Reload(ctx context.Context, id int64) error {
	e.Remove(id)
	m, err := e.store.GetMonitor(ctx, id)
	if err != nil {
		return err
	}
	if !m.Active {
		return nil
	}
	return e.start(m)
}

// Remove monitörün runner'ını durdurur ve bitmesini bekler.
func (e *Engine) Remove(id int64) {
	e.mu.Lock()
	r := e.runners[id]
	delete(e.runners, id)
	e.mu.Unlock()
	if r != nil {
		r.cancel()
		<-r.done
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

func (e *Engine) start(m store.Monitor) error {
	checker, ok := check.Get(m.Type)
	if !ok {
		return fmt.Errorf("bilinmeyen monitör tipi: %s", m.Type)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ctx == nil {
		return errors.New("motor başlatılmadı")
	}
	if old := e.runners[m.ID]; old != nil {
		return fmt.Errorf("monitör %d zaten çalışıyor", m.ID)
	}
	ctx, cancel := context.WithCancel(e.ctx)
	ctx = check.WithStatusSource(ctx, e.monitorStatuses) // grup monitörleri alt monitörleri okur
	r := &runner{
		e: e, m: m, checker: checker, cancel: cancel,
		done:   make(chan struct{}),
		pushCh: make(chan check.Result, 8),
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
