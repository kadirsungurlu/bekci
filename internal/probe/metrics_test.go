package probe

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
)

// fakeCollector sahte metrik toplayıcı: ilk çağrı (ve Reset sonrası) hazırlıktır.
type fakeCollector struct {
	mu          sync.Mutex
	calls       int
	resets      int
	primed      bool
	unavailable string
}

func (f *fakeCollector) Collect(context.Context) (metrics.Sample, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	s := metrics.Sample{Time: time.Now().UnixMilli()}
	if f.unavailable != "" {
		s.Unavailable = f.unavailable
		return s, true
	}
	if !f.primed {
		f.primed = true
		return s, false
	}
	s.Host, s.Stats = &metrics.Host{Hostname: "h1"}, &metrics.Stats{CPU: float64(f.calls)}
	return s, true
}

func (f *fakeCollector) Reset() {
	f.mu.Lock()
	f.resets++
	f.primed = false
	f.mu.Unlock()
}

func (f *fakeCollector) counts() (calls, resets int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.resets
}

// metricPosts kabul edilen örnek ve toplam istek sayısı.
func (f *fakeServer) metricPosts() (ok int, attempts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.samples), len(f.metAuth)
}

// stable değerin süre boyunca değişmediğini doğrular.
func stable(t *testing.T, what string, d time.Duration, get func() int) {
	t.Helper()
	time.Sleep(20 * time.Millisecond) // yoldaki istek tamamlansın
	before := get()
	time.Sleep(d)
	if after := get(); after != before {
		t.Fatalf("%s: %d → %d", what, before, after)
	}
}

func TestMetricsLoop(t *testing.T) {
	fs := newFakeServer(t)
	fs.set(func(f *fakeServer) { f.metricsIv = 20 })
	fc := &fakeCollector{}
	start(t, Config{Server: fs.srv.URL, Metrics: fc})

	waitFor(t, "metrik gönderildi", func() bool { n, _ := fs.metricPosts(); return n >= 3 })
	fs.mu.Lock()
	first, auth := fs.samples[0], fs.metAuth[0]
	fs.mu.Unlock()
	if auth != "Bearer upr_test" || first.Host == nil || first.Host.Hostname != "h1" || first.Stats == nil || first.Time <= 0 {
		t.Fatalf("örnek %+v yetki %q", first, auth)
	}
	if first.Stats.CPU != 2 {
		t.Fatalf("hazırlık örneği gönderilmemeli: ilk gönderilen %v. çağrı", first.Stats.CPU)
	}

	// Aralık 0 olunca gönderim durur.
	fs.set(func(f *fakeServer) { f.metricsIv = 0 })
	time.Sleep(30 * time.Millisecond)
	stable(t, "aralık 0 iken gönderim", 100*time.Millisecond, func() int { n, _ := fs.metricPosts(); return n })

	// Yeniden açılınca sayaçlar sıfırdan hazırlanır ve gönderim sürer.
	_, resets := fc.counts()
	n0, _ := fs.metricPosts()
	fs.set(func(f *fakeServer) { f.metricsIv = 20 })
	waitFor(t, "gönderim yeniden başladı", func() bool { n, _ := fs.metricPosts(); return n >= n0+2 })
	if _, r := fc.counts(); r != resets+1 {
		t.Fatalf("yeniden başlarken Reset çağrılmalı: %d → %d", resets, r)
	}

	// Aralık değişikliği hemen uygulanır.
	fs.set(func(f *fakeServer) { f.metricsIv = 5000 })
	time.Sleep(50 * time.Millisecond)
	stable(t, "uzun aralıkta gönderim", 150*time.Millisecond, func() int { n, _ := fs.metricPosts(); return n })
}

// Eski sunucu (metrics_interval yok) veya METRICS=0: toplayıcı hiç çağrılmaz.
func TestMetricsDisabled(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 10)} })
	fc := &fakeCollector{}
	start(t, Config{Server: fs.srv.URL, Metrics: fc})
	waitFor(t, "sonuç", func() bool { _, n, _ := fs.received(); return n >= 2 })
	if calls, _ := fc.counts(); calls != 0 {
		t.Fatalf("eski sunucuda metrik toplanmamalı: %d", calls)
	}

	fs2 := newFakeServer(t)
	fs2.set(func(f *fakeServer) { f.metricsIv = 10; f.jobs = []Job{httpJob(1, target.URL, 10)} })
	fc2 := &fakeCollector{}
	start(t, Config{Server: fs2.srv.URL, Metrics: fc2, NoMetrics: true})
	waitFor(t, "sonuç", func() bool { _, n, _ := fs2.received(); return n >= 2 })
	time.Sleep(50 * time.Millisecond)
	if calls, _ := fc2.counts(); calls != 0 {
		t.Fatalf("METRICS=0 iken metrik toplanmamalı: %d", calls)
	}
	if n, a := fs2.metricPosts(); n+a != 0 {
		t.Fatal("METRICS=0 iken gönderim yapıldı")
	}
	c, err := New(Config{Server: "https://x.example", Token: "upr_x", NoMetrics: true})
	if err != nil || c.cfg.Metrics != nil {
		t.Fatal("METRICS=0 iken toplayıcı oluşturulmamalı")
	}
}

// Gönderilemeyen örnekler biriktirilmez: her istek tek ve yeni bir örnektir,
// sunucu dönünce eski örnekler topluca gönderilmez.
func TestMetricsNoBacklog(t *testing.T) {
	fs := newFakeServer(t)
	fs.set(func(f *fakeServer) { f.metricsIv = 10; f.metCode = 503 })
	fc := &fakeCollector{}
	start(t, Config{Server: fs.srv.URL, Metrics: fc})
	waitFor(t, "başarısız denemeler", func() bool { _, a := fs.metricPosts(); return a >= 4 })
	fs.set(func(f *fakeServer) { f.metCode = 200 })
	waitFor(t, "gönderildi", func() bool { n, _ := fs.metricPosts(); return n >= 2 })
	calls, _ := fc.counts()
	_, attempts := fs.metricPosts()
	// Hazırlık çağrısı gönderilmez; en fazla bir istek yolda olabilir.
	if attempts > calls-1 || attempts < calls-2 {
		t.Fatalf("istek sayısı toplama sayısını izlemeli: %d istek, %d toplama", attempts, calls)
	}
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.samples[1].Stats.CPU <= fs.samples[0].Stats.CPU {
		t.Fatal("örnekler yeni olmalı")
	}
}

// Toplanamıyorsa (host bağlanmamış) neden gönderilir.
func TestMetricsUnavailable(t *testing.T) {
	fs := newFakeServer(t)
	fs.set(func(f *fakeServer) { f.metricsIv = 10 })
	fc := &fakeCollector{unavailable: metrics.NoHostReason}
	start(t, Config{Server: fs.srv.URL, Metrics: fc})
	waitFor(t, "neden gönderildi", func() bool { n, _ := fs.metricPosts(); return n >= 1 })
	fs.mu.Lock()
	s := fs.samples[0]
	fs.mu.Unlock()
	if s.Unavailable != metrics.NoHostReason || s.Stats != nil || s.Time <= 0 {
		t.Fatalf("%+v", s)
	}
}

// Token reddedilince metrik gönderimi de durur, etkinleşince geri gelir.
func TestMetricsStopOnUnauthorized(t *testing.T) {
	fs := newFakeServer(t)
	fs.set(func(f *fakeServer) { f.metricsIv = 10 })
	fc := &fakeCollector{}
	start(t, Config{Server: fs.srv.URL, Metrics: fc})
	waitFor(t, "gönderim", func() bool { n, _ := fs.metricPosts(); return n >= 2 })
	fs.set(func(f *fakeServer) { f.jobsCode = 403; f.metCode = 403 })
	time.Sleep(30 * time.Millisecond)
	stable(t, "devre dışıyken metrik isteği", 100*time.Millisecond, func() int { _, a := fs.metricPosts(); return a })
	fs.set(func(f *fakeServer) { f.jobsCode = 200; f.metCode = 200 })
	n0, _ := fs.metricPosts()
	waitFor(t, "gönderim geri geldi", func() bool { n, _ := fs.metricPosts(); return n > n0 })
}
