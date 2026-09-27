package probe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
)

// fakeServer ana sunucunun kontrol noktası uç noktalarını taklit eder.
type fakeServer struct {
	mu        sync.Mutex
	jobs      []Job
	jobsCode  int
	resCode   int
	batches   [][]Result
	versions  []string
	authFails int
	metricsIv int // -1: alan hiç gönderilmez (eski sunucu)
	metCode   int
	samples   []metrics.Sample
	metAuth   []string
	srv       *httptest.Server
}

func newFakeServer(t *testing.T) *fakeServer {
	f := &fakeServer{jobsCode: 200, resCode: 200, metricsIv: -1, metCode: 200}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer upr_test" {
			f.authFails++
			w.WriteHeader(401)
			return
		}
		f.versions = append(f.versions, r.Header.Get("X-Probe-Version"))
		switch r.URL.Path {
		case "/api/probe/jobs":
			w.WriteHeader(f.jobsCode)
			resp := map[string]any{"poll_after": 5, "jobs": f.jobs}
			if f.metricsIv >= 0 {
				resp["metrics_interval"] = f.metricsIv
			}
			json.NewEncoder(w).Encode(resp)
		case "/api/probe/results":
			var in struct {
				SentAt  int64    `json:"sent_at"`
				Results []Result `json:"results"`
			}
			json.NewDecoder(r.Body).Decode(&in)
			w.WriteHeader(f.resCode)
			if f.resCode == 200 {
				f.batches = append(f.batches, in.Results)
				io.WriteString(w, `{"accepted":1,"rejected":[]}`)
			}
		case "/api/probe/metrics":
			var in metrics.Sample
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil || r.Method != http.MethodPost {
				w.WriteHeader(400)
				return
			}
			f.metAuth = append(f.metAuth, r.Header.Get("Authorization"))
			w.WriteHeader(f.metCode)
			if f.metCode == 200 {
				f.samples = append(f.samples, in)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) set(fn func(f *fakeServer)) { f.mu.Lock(); fn(f); f.mu.Unlock() }

func (f *fakeServer) received() (batches int, results int, maxBatch int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.batches {
		results += len(b)
		maxBatch = max(maxBatch, len(b))
	}
	return len(f.batches), results, maxBatch
}

// site kontrol edilen hedef.
func site(t *testing.T) (*httptest.Server, *atomic.Int64) {
	var hits atomic.Int64
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, "ok")
	}))
	t.Cleanup(s.Close)
	return s, &hits
}

func httpJob(id int64, url string, interval int) Job {
	return Job{ID: id, Name: fmt.Sprintf("m%d", id), Type: "http", Interval: interval, RetryInterval: interval, Timeout: 500,
		Config: json.RawMessage(fmt.Sprintf(`{"url":%q}`, url))}
}

func start(t *testing.T, cfg Config) (*Client, func()) {
	t.Helper()
	if cfg.Token == "" {
		cfg.Token = "upr_test"
	}
	cfg.AllowInsecure = true // testler httptest (http) kullanır
	cfg.Unit = time.Millisecond
	cfg.Version = "v-test"
	if cfg.FlushEvery == 0 {
		cfg.FlushEvery = 10 * time.Millisecond
	}
	if cfg.MaxBackoff == 0 {
		cfg.MaxBackoff = 20 * time.Millisecond
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	stop := func() { cancel(); <-done }
	t.Cleanup(stop)
	return c, sync.OnceFunc(stop)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("beklenen durum oluşmadı: %s", what)
}

func TestNewValidates(t *testing.T) {
	for _, c := range []Config{
		{Server: "", Token: "upr_x"},
		{Server: "ftp://x", Token: "upr_x"},
		{Server: "https://", Token: "upr_x"},
		{Server: "https://uptime.kadir.app", Token: "upk_x"},
	} {
		if _, err := New(c); err == nil {
			t.Errorf("%+v kabul edilmemeliydi", c)
		}
	}
	if _, err := New(Config{Server: "http://uptime.kadir.app", Token: "upr_x"}); err == nil {
		t.Error("http PROBE_ALLOW_INSECURE olmadan reddedilmeliydi")
	}
	if _, err := New(Config{Server: "http://uptime.kadir.app", Token: "upr_x", AllowInsecure: true}); err != nil {
		t.Errorf("http AllowInsecure ile kabul edilmeliydi: %v", err)
	}
	if _, err := New(Config{Server: "https://uptime.kadir.app/", Token: "upr_x"}); err != nil {
		t.Fatal(err)
	}
}

func TestChecksAndReports(t *testing.T) {
	fs := newFakeServer(t)
	target, hits := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(7, target.URL, 20)} })
	c, _ := start(t, Config{Server: fs.srv.URL})

	waitFor(t, "sonuç", func() bool { _, n, _ := fs.received(); return n >= 3 })
	fs.mu.Lock()
	r := fs.batches[0][0]
	v := fs.versions[0]
	fs.mu.Unlock()
	if r.MonitorID != 7 || !r.Up || r.PingMs < 0 || r.Message != "200 OK" || r.Time <= 0 || v != "v-test" {
		t.Fatalf("sonuç %+v sürüm %q", r, v)
	}
	if hits.Load() == 0 || len(c.Jobs()) != 1 {
		t.Fatal("kontrol yapılmadı")
	}

	// İş kaldırılınca kontrol durur.
	fs.set(func(f *fakeServer) { f.jobs = nil })
	waitFor(t, "iş durdu", func() bool { return len(c.Jobs()) == 0 })
	time.Sleep(20 * time.Millisecond)
	before := hits.Load()
	time.Sleep(60 * time.Millisecond)
	if hits.Load() != before {
		t.Fatal("kaldırılan iş çalışmaya devam ediyor")
	}
}

// Ana sunucuya ulaşılamazken sonuçlar sınırlı tamponda bekler; sunucu
// dönünce gönderilir.
func TestBufferWhileServerDown(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 20)}; f.resCode = 503 })
	c, _ := start(t, Config{Server: fs.srv.URL, MaxBuffer: 5})

	waitFor(t, "tampon doldu", func() bool { return c.pending() == 5 })
	time.Sleep(50 * time.Millisecond)
	if n := c.pending(); n > 5 {
		t.Fatalf("tampon sınırı aşıldı: %d", n)
	}
	fs.set(func(f *fakeServer) { f.resCode = 200 })
	waitFor(t, "gönderildi", func() bool { b, _, _ := fs.received(); return b >= 1 })
	if _, _, mx := fs.received(); mx > 5 {
		t.Fatalf("tampondan fazla sonuç gönderildi: %d", mx)
	}
}

// Kalıcı hata (400) partiyi atar, sonsuza kadar tekrar denenmez.
func TestPermanentErrorDropsBatch(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 1000)}; f.resCode = 400 })
	c, _ := start(t, Config{Server: fs.srv.URL})
	waitFor(t, "ilk kontrol", func() bool { return len(c.Jobs()) == 1 })
	time.Sleep(60 * time.Millisecond)
	if n := c.pending(); n != 0 {
		t.Fatalf("400 alan parti atılmalı: %d bekliyor", n)
	}
}

// Token reddedilince (devre dışı / silinmiş) kontroller hemen durur.
func TestUnauthorizedStopsChecks(t *testing.T) {
	fs := newFakeServer(t)
	target, hits := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 10)} })
	c, _ := start(t, Config{Server: fs.srv.URL})
	waitFor(t, "iş başladı", func() bool { return len(c.Jobs()) == 1 && hits.Load() > 0 })
	fs.set(func(f *fakeServer) { f.jobsCode = 403 })
	waitFor(t, "iş durdu", func() bool { return len(c.Jobs()) == 0 })
	time.Sleep(20 * time.Millisecond)
	before := hits.Load()
	time.Sleep(50 * time.Millisecond)
	if hits.Load() != before {
		t.Fatal("devre dışı kontrol noktası kontrol yapmaya devam ediyor")
	}
	// Tekrar etkinleşince işler geri gelir.
	fs.set(func(f *fakeServer) { f.jobsCode = 200 })
	waitFor(t, "iş geri geldi", func() bool { return len(c.Jobs()) == 1 })
}

// Kapanışta tampondaki sonuçlar son kez gönderilir.
func TestFinalFlushOnShutdown(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 10)} })
	c, stop := start(t, Config{Server: fs.srv.URL, FlushEvery: time.Hour})
	waitFor(t, "sonuç birikti", func() bool { return c.pending() >= 2 })
	stop()
	if _, n, _ := fs.received(); n < 2 {
		t.Fatalf("kapanışta gönderilmedi: %d", n)
	}
}

func TestJobChangeRestarts(t *testing.T) {
	fs := newFakeServer(t)
	a, hitsA := site(t)
	b, hitsB := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, a.URL, 10)} })
	c, _ := start(t, Config{Server: fs.srv.URL})
	waitFor(t, "A kontrol edildi", func() bool { return hitsA.Load() > 0 })
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, b.URL, 10)} })
	waitFor(t, "B kontrol edildi", func() bool { return hitsB.Load() > 0 })
	time.Sleep(20 * time.Millisecond)
	before := hitsA.Load()
	time.Sleep(50 * time.Millisecond)
	if hitsA.Load() != before || len(c.Jobs()) != 1 {
		t.Fatal("değişen iş eski ayarla çalışmaya devam ediyor")
	}
	// Bilinmeyen/desteklenmeyen tipler atlanır.
	fs.set(func(f *fakeServer) {
		f.jobs = []Job{{ID: 2, Type: "gelecek-tip", Interval: 10, Timeout: 10}, {ID: 3, Type: "push", Interval: 10, Timeout: 10}}
	})
	waitFor(t, "işler atlandı", func() bool { return len(c.Jobs()) == 0 })
}
