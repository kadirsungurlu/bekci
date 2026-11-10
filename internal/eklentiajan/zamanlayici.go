package eklentiajan

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

// Result panele giden tek sonuç (POST /api/probe/checks, B.4).
type Result struct {
	Name       string `json:"name"`
	Source     string `json:"source"` // plugin
	Version    int64  `json:"version,omitempty"`
	T          int64  `json:"t"` // unix ms (bitiş)
	DurationMs int64  `json:"duration_ms"`
	eklenti.Result
	Error  string `json:"error,omitempty"`  // red kodu (Red*), "timeout" ya da "exec"
	Stderr string `json:"stderr,omitempty"` // ilk 1 KiB, yalnız ok değilse
}

// ChecksPath sonuç ucu.
const ChecksPath = "/api/probe/checks"

const (
	maxQueue     = 200
	maxBatch     = 20 // B.4: istek başına en çok 20 sonuç
	tick         = 5 * time.Second
	retryAfter   = time.Minute
	oldPanelWait = 10 * time.Minute // uç yok (404, eski panel)
	ozetAralik   = 24 * time.Hour
	firstSpread  = 30 * time.Second // ilk çalıştırmalar aynı ana yığılmasın
)

type sayac struct {
	Calisma int    `json:"calisma"`
	Red     int    `json:"red"`
	Son     string `json:"son"`
}

// Scheduler panelin açtığı eklentileri aralıkla, sırayla (aynı anda tek birim)
// çalıştırır; sonuçları sınırlı kuyrukta tutup panele toplu gönderir.
type Scheduler struct {
	Paths Paths
	Exec  Executor // nil: platform uygun değil, her öğe RedPlatform
	Kayit Kayitci  // nil: kayıt yok
	Log   *slog.Logger
	Now   func() time.Time

	mu        sync.Mutex
	items     map[string]Item
	next      map[string]time.Time
	ver       map[string]int64
	lastErr   map[string]string
	stats     map[string]*sayac
	ozetAt    time.Time
	queue     []Result
	postAfter time.Time
	wake      chan struct{}
}

// NewScheduler üretim zamanlayıcısı (Linux + systemd >= 254 değilse Exec nil).
func NewScheduler(log *slog.Logger) *Scheduler {
	p := DefaultPaths()
	s := &Scheduler{Paths: p, Kayit: &DosyaKayit{Dir: p.LogDir}, Log: log}
	if e := NewSystemd(); e != nil {
		s.Exec = e
	}
	return s
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Scheduler) kaydet(o Olay) {
	if s.Kayit == nil {
		return
	}
	if o.T == 0 {
		o.T = s.now().Unix()
	}
	if err := s.Kayit.Yaz(o); err != nil {
		s.log().Warn("eklenti kaydı yazılamadı", "hata", err)
	}
}

// Handle jobs yanıtındaki `checks` alanı. Alan yoksa (eski panel) hiçbir
// eklenti çalışmaz (G4); yalnız source=plugin öğeleri alınır.
func (s *Scheduler) Handle(raw json.RawMessage) {
	var list []Item
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &list); err != nil {
			s.log().Warn("checks alanı çözülemedi", "hata", err)
			return // bozuk yanıt: mevcut liste korunur
		}
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	fresh := map[string]Item{}
	for _, it := range list {
		if it.Source != "plugin" || !IsID(it.Name) || len(fresh) >= MaxPlugins {
			continue
		}
		if _, dup := fresh[it.Name]; !dup {
			fresh[it.Name] = it
		}
	}
	var olaylar []Olay
	for name := range s.items {
		if _, ok := fresh[name]; !ok {
			delete(s.next, name)
			delete(s.lastErr, name)
			olaylar = append(olaylar, Olay{Tur: OlayKapat, ID: name})
		}
	}
	for name, it := range fresh {
		old, had := s.items[name]
		iv := time.Duration(clampInterval(it.Interval)) * time.Second
		switch {
		case !had:
			s.next[name] = now.Add(time.Duration(rand.Int64N(int64(min(firstSpread, iv)))))
			olaylar = append(olaylar, Olay{Tur: OlayAc, ID: name, Ayrinti: map[string]any{"aralik": it.Interval}})
		case old.Interval != it.Interval:
			s.next[name] = minTime(s.next[name], now.Add(iv))
		}
	}
	s.items = fresh
	if len(olaylar) > 0 {
		go func() {
			for _, o := range olaylar {
				s.kaydet(o)
			}
		}()
	}
	if s.wake != nil {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

// ensure haritaları kurar; s.mu tutulurken çağrılır.
func (s *Scheduler) ensure() {
	if s.next == nil {
		s.items, s.next, s.ver, s.lastErr, s.stats = map[string]Item{}, map[string]time.Time{}, map[string]int64{}, map[string]string{}, map[string]*sayac{}
	}
}

func clampInterval(n int) int { return min(max(n, MinInterval), MaxInterval) }

func minTime(a, b time.Time) time.Time {
	if a.IsZero() || b.Before(a) {
		return b
	}
	return a
}

// Run döngü: vadesi gelen eklentileri çalıştırır, özet yazar, kuyruğu boşaltır.
func (s *Scheduler) Run(ctx context.Context, post func(ctx context.Context, path string, body any) (int, error)) {
	s.mu.Lock()
	s.wake = make(chan struct{}, 1)
	s.mu.Unlock()
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		s.RunDue(ctx)
		s.ozet()
		s.Flush(ctx, post)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

// RunDue vadesi gelen eklentileri ad sırasıyla tek tek çalıştırır.
func (s *Scheduler) RunDue(ctx context.Context) {
	now := s.now()
	s.mu.Lock()
	var due []Item
	for name, at := range s.next {
		if !at.After(now) {
			due = append(due, s.items[name])
		}
	}
	s.mu.Unlock()
	slices.SortFunc(due, func(a, b Item) int {
		if a.Name < b.Name {
			return -1
		}
		return 1
	})
	for _, it := range due {
		if ctx.Err() != nil {
			return
		}
		r := s.RunOne(ctx, it)
		s.mu.Lock()
		if cur, ok := s.items[it.Name]; ok { // bu arada kapatılmadıysa
			s.next[it.Name] = s.now().Add(time.Duration(clampInterval(cur.Interval)) * time.Second)
			s.push(r)
		}
		s.mu.Unlock()
	}
}

// RunOne öğeyi yerel güven köküne göre doğrular ve kafeste çalıştırır.
func (s *Scheduler) RunOne(ctx context.Context, it Item) Result {
	start := s.now()
	res := Result{Name: it.Name, Source: "plugin"}
	plan, err := s.decide(it, start)
	if err != nil {
		var re *RedError
		errors.As(err, &re)
		res.Result = eklenti.Result{Status: eklenti.Unknown, Message: trim(re.Msg, eklenti.MaxMessage), Metrics: []eklenti.Metric{}}
		res.Error, res.T = re.Code, start.UnixMilli()
		s.note(it.Name, re.Code, err)
		s.count(it.Name, "", true)
		return res
	}
	s.note(it.Name, "", nil)
	m := plan.M
	res.Version = m.Version
	s.mu.Lock()
	s.ensure()
	prev := s.ver[m.ID]
	s.ver[m.ID] = m.Version
	s.mu.Unlock()
	if prev != m.Version {
		s.kaydet(Olay{Tur: OlaySurum, ID: m.ID, Surum: m.Version, SHA256: m.SHA256, Ayrinti: map[string]any{"onceki": prev}})
	}
	o := s.Exec.Run(ctx, Spec{Unit: NewUnit(m.ID), RunAs: m.RunAs, Network: m.Network, Body: plan.Body,
		Timeout: time.Duration(m.TimeoutSec) * time.Second})
	res.DurationMs = o.Elapsed.Milliseconds()
	res.T = s.now().UnixMilli()
	switch {
	case o.Err != nil:
		res.Result = eklenti.Result{Status: eklenti.Unknown, Message: trim(o.Err.Error(), eklenti.MaxMessage), Metrics: []eklenti.Metric{}}
		res.Error = "exec"
	default:
		rc := o.ExitCode
		if o.TimedOut {
			rc = -1
		}
		res.Result = eklenti.Parse(m.Output, rc, o.Stdout)
		if o.TimedOut {
			res.Error = "timeout"
		}
		res.Truncated = res.Truncated || o.Truncated
	}
	if res.Status != eklenti.OK && len(o.Stderr) > 0 {
		res.Stderr = trim(string(o.Stderr), maxStderr)
	}
	s.count(it.Name, string(res.Status), false)
	return res
}

func (s *Scheduler) decide(it Item, now time.Time) (*Plan, error) {
	if s.Exec == nil {
		return nil, red(RedPlatform, "eklenti yalnız Linux + systemd >= 254 yerli ajanda çalışır")
	}
	cfg, err := LoadConfig(s.Paths.Config)
	if err != nil {
		return nil, red(RedKapali, "%v", err)
	}
	return Decide(cfg, it, now)
}

// note reddi yalnız kod değişince loglar (her turda gürültü olmasın).
func (s *Scheduler) note(name, code string, err error) {
	s.mu.Lock()
	s.ensure()
	changed := s.lastErr[name] != code
	s.lastErr[name] = code
	s.mu.Unlock()
	if !changed {
		return
	}
	if err != nil {
		s.log().Warn("eklenti reddedildi", "ad", name, "hata", err)
	} else {
		s.log().Info("eklenti çalışıyor", "ad", name)
	}
}

func (s *Scheduler) count(name, status string, redd bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensure()
	c := s.stats[name]
	if c == nil {
		c = &sayac{}
		s.stats[name] = c
	}
	if redd {
		c.Red++
		c.Son = "red"
		return
	}
	c.Calisma++
	c.Son = status
}

// ozet günde bir sayaçları kayda yazar ve sıfırlar.
func (s *Scheduler) ozet() {
	now := s.now()
	s.mu.Lock()
	if s.ozetAt.IsZero() {
		s.ozetAt = now
	}
	if now.Sub(s.ozetAt) < ozetAralik || len(s.stats) == 0 {
		s.mu.Unlock()
		return
	}
	a := map[string]any{}
	for k, v := range s.stats {
		a[k] = *v
	}
	s.stats, s.ozetAt = map[string]*sayac{}, now
	s.mu.Unlock()
	s.kaydet(Olay{Tur: OlayOzet, Ayrinti: a})
}

func (s *Scheduler) push(r Result) {
	s.queue = append(s.queue, r)
	if n := len(s.queue) - maxQueue; n > 0 {
		s.queue = slices.Delete(s.queue, 0, n) // en eskiler atılır
	}
}

// Queue kuyruktaki sonuç sayısı (test).
func (s *Scheduler) Queue() []Result {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.queue)
}

// Flush kuyruğu en çok 20'lik partilerle gönderir. Uç yoksa (404, eski
// panel) 10 dk, hata/5xx'te 1 dk bekler; diğer 4xx partiyi atar.
func (s *Scheduler) Flush(ctx context.Context, post func(ctx context.Context, path string, body any) (int, error)) {
	for {
		now := s.now()
		s.mu.Lock()
		if len(s.queue) == 0 || now.Before(s.postAfter) {
			s.mu.Unlock()
			return
		}
		batch := slices.Clone(s.queue[:min(maxBatch, len(s.queue))])
		s.mu.Unlock()
		code, err := post(ctx, ChecksPath, map[string]any{"results": batch})
		s.mu.Lock()
		switch {
		case err != nil || code >= 500 || code == http.StatusTooManyRequests:
			s.postAfter = now.Add(retryAfter)
		case code == http.StatusNotFound:
			s.postAfter = now.Add(oldPanelWait)
		}
		if err == nil && code < 500 && code != http.StatusNotFound && code != http.StatusTooManyRequests {
			s.queue = s.queue[min(len(batch), len(s.queue)):] // 2xx ya da kalıcı 4xx
		}
		wait := !s.postAfter.IsZero() && now.Before(s.postAfter)
		s.mu.Unlock()
		if wait || ctx.Err() != nil {
			return
		}
	}
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
