// Package probe uzak kontrol noktası (ajan) istemcisidir: aynı ikili başka bir
// sunucuda "uptime probe" olarak çalışır, ana sunucudan kendisine atanan
// monitörleri (iş listesi) alır, kontrolleri internal/check ile kendisi yapar
// ve sonuçları toplu halde ana sunucuya gönderir. Ana sunucu istediğinde
// (metrics_interval > 0) bulunduğu sunucunun ölçümlerini de (CPU, RAM, disk,
// ağ, Docker; internal/metrics) bu aralıkla gönderir.
//
// Protokol (HTTPS + JSON, Authorization: Bearer upr_…):
//
//	GET  /api/probe/jobs     → {"probe": {...}, "poll_after": 30, "metrics_interval": 60, "jobs": [...]}
//	POST /api/probe/results  ← {"sent_at": ms, "results": [{monitor_id, time, up, ping_ms, message, cert_not_after, cert_issuer, detail}]}
//	POST /api/probe/metrics  ← metrics.Sample: {"time": ms, "host": {...}, "stats": {...}} veya {"time": ms, "unavailable": "neden"}
//
// Ana sunucuya ulaşılamazsa istekler artan beklemeyle tekrarlanır; bu sırada
// kontroller sürer ve sonuçlar sınırlı bir tamponda bekletilir (dolunca en
// eskisi düşer). Metrik örnekleri biriktirilmez: gönderilemeyen örneğin
// yerine bir sonraki aralıkta yenisi gönderilir.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
)

// Config istemci ayarları. Sıfır değerli alanlar varsayılanla doldurulur.
type Config struct {
	Server        string           // ana sunucu adresi, ör. https://uptime.kadir.app
	Token         string           // kontrol noktası token'ı (upr_…)
	Version       string           // sunucuya bildirilen sürüm
	Unit          time.Duration    // aralık birimi; üretimde saniye, testlerde daha kısa
	FlushEvery    time.Duration    // sonuç gönderme sıklığı (varsayılan 5 birim)
	MaxConcurrent int              // aynı anda en fazla kontrol (varsayılan 20)
	MaxBuffer     int              // gönderilemeyen en fazla sonuç (varsayılan 5000)
	BatchSize     int              // tek istekteki en fazla sonuç (varsayılan 200)
	MaxBackoff    time.Duration    // tekrar denemeler arası en uzun bekleme (varsayılan 60 birim)
	NoMetrics     bool             // sunucu metrikleri hiç toplanmaz (METRICS=0), ana sunucu istese de
	Metrics       MetricsCollector // varsayılan metrics.NewCollector
	HTTPClient    *http.Client
	Log           *slog.Logger
}

// MetricsCollector sunucu ölçümlerini toplar (metrics.Collector; testlerde sahtesi).
// Collect ilk çağrıda (ve Reset sonrası) yalnızca sayaçları hazırlar, false döner.
type MetricsCollector interface {
	Collect(ctx context.Context) (metrics.Sample, bool)
	Reset()
}

// minMetricsInterval ana sunucu daha kısa bir aralık istese de örnekler en az
// bu kadar birim arayla alınır (Docker istatistikleri her örnekte sorgulanır).
const minMetricsInterval = 5

// Job ana sunucudan gelen iş: bir monitörün kontrol ayarı. Süreler birim
// (saniye) cinsinden.
type Job struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Interval      int             `json:"interval"`
	RetryInterval int             `json:"retry_interval"`
	Timeout       int             `json:"timeout"`
	Config        json.RawMessage `json:"config"`
}

// Result ana sunucuya gönderilen tek sonuç. Time unix milisaniye.
type Result struct {
	MonitorID    int64  `json:"monitor_id"`
	Time         int64  `json:"time"`
	Up           bool   `json:"up"`
	PingMs       int64  `json:"ping_ms"`
	Message      string `json:"message"`
	CertNotAfter int64  `json:"cert_not_after,omitempty"`
	CertIssuer   string `json:"cert_issuer,omitempty"`
	// Detail başarısız HTTP kontrolünün isteği ve yanıtı (olay sayfası; maskeli).
	// Eski sunucular bilinmeyen alanı yok sayar.
	Detail *check.Detail `json:"detail,omitempty"`
}

// maxBatchDetail tek partideki ayrıntıların (Detail) toplam boyut sınırı:
// eski sunucuların 1 MB gövde sınırı aşılmasın diye; fazlası o partide atılır.
const maxBatchDetail = 256 << 10

type jobsResponse struct {
	Probe struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"probe"`
	PollAfter       int   `json:"poll_after"`
	MetricsInterval int   `json:"metrics_interval"` // sn; 0 veya alan yok (eski sunucu) = metrik gönderilmez
	Jobs            []Job `json:"jobs"`
}

// Client bir kontrol noktası.
type Client struct {
	cfg  Config
	base *url.URL
	log  *slog.Logger
	sem  chan struct{}

	mu      sync.Mutex
	buf     []Result
	dropped int // tampon dolduğu için düşen sonuç (son uyarıdan beri)
	tasks   map[int64]*task
	unknown map[string]bool // uyarısı yazılmış bilinmeyen tipler
	reach   map[string]bool // uç nokta başına erişilebilirlik (log tekrarını önler)

	metricsIv  chan int // iş listesindeki son metrics_interval (yalnızca en yenisi bekler)
	metricsBad int      // son reddedilen metrik gönderiminin durumu (log tekrarını önler; yalnızca metricsLoop)
}

type task struct {
	def    []byte // değişiklik tespiti için işin JSON'u
	cancel context.CancelFunc
	done   chan struct{}
}

// ErrUnauthorized token geçersiz veya kontrol noktası devre dışı.
var ErrUnauthorized = errors.New("token geçersiz veya kontrol noktası devre dışı")

// New ayarları doğrular.
func New(cfg Config) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(cfg.Server), "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("PROBE_SERVER geçerli bir http(s) adresi olmalı, ör. https://uptime.kadir.app")
	}
	if !strings.HasPrefix(cfg.Token, "upr_") {
		return nil, errors.New("PROBE_TOKEN geçersiz: upr_ ile başlamalı")
	}
	if cfg.Unit <= 0 {
		cfg.Unit = time.Second
	}
	if cfg.FlushEvery <= 0 {
		cfg.FlushEvery = 5 * cfg.Unit
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 20
	}
	if cfg.MaxBuffer <= 0 {
		cfg.MaxBuffer = 5000
	}
	if cfg.BatchSize <= 0 || cfg.BatchSize > 500 {
		cfg.BatchSize = 200
	}
	if cfg.MaxBackoff <= 0 {
		cfg.MaxBackoff = 60 * cfg.Unit
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if cfg.Version == "" {
		cfg.Version = "dev"
	}
	if cfg.Metrics == nil && !cfg.NoMetrics {
		cfg.Metrics = metrics.NewCollector(metrics.Options{Log: cfg.Log})
	}
	return &Client{
		cfg: cfg, base: u, log: cfg.Log, sem: make(chan struct{}, cfg.MaxConcurrent),
		tasks: map[int64]*task{}, unknown: map[string]bool{}, reach: map[string]bool{},
		metricsIv: make(chan int, 1),
	}, nil
}

// Run ctx iptal edilene kadar çalışır. Kapanışta çalışan kontroller durur,
// tampondaki sonuçlar kısa bir süre içinde son kez gönderilmeye çalışılır.
func (c *Client) Run(ctx context.Context) error {
	if c.base.Scheme == "http" {
		c.log.Warn("ana sunucu bağlantısı şifrelenmemiş (http); token ve monitör ayarları açık metin gider, https kullanın")
	}
	c.log.Info("kontrol noktası başladı", "sunucu", c.base.String(), "sürüm", c.cfg.Version)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); c.jobsLoop(ctx) }()
	go func() { defer wg.Done(); c.flushLoop(ctx) }()
	if c.cfg.NoMetrics {
		c.log.Info("sunucu metrikleri bu ajanda kapalı (METRICS=0)")
	} else {
		wg.Add(1)
		go func() { defer wg.Done(); c.metricsLoop(ctx) }()
	}
	wg.Wait()
	c.stopAll()

	// Son gönderim: kapanış uzamasın diye kısa süreli.
	fctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for c.pending() > 0 {
		if _, err := c.flushOnce(fctx); err != nil {
			c.log.Warn("kapanışta sonuçlar gönderilemedi", "bekleyen", c.pending(), "hata", err)
			break
		}
	}
	c.log.Info("kontrol noktası durdu")
	return nil
}

// Jobs şu an çalışan işlerin kimlikleri (testler için).
func (c *Client) Jobs() []int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int64, 0, len(c.tasks))
	for id := range c.tasks {
		out = append(out, id)
	}
	return out
}

// İş listesi ---------------------------------------------------------------------------

func (c *Client) jobsLoop(ctx context.Context) {
	backoff := time.Duration(0)
	for {
		wait, err := c.pollJobs(ctx)
		if ctx.Err() != nil {
			return
		}
		switch {
		case errors.Is(err, ErrUnauthorized):
			// Devre dışı bırakılan kontrol noktası hemen durur; ara ara yeniden denenir
			// (yönetici tekrar etkinleştirebilir).
			c.stopAll()
			c.clearBuffer()
			c.setMetricsInterval(0)
			wait = c.cfg.MaxBackoff
		case err != nil:
			backoff = c.nextBackoff(backoff)
			wait = backoff
		default:
			backoff = 0
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
	}
}

// pollJobs iş listesini alır ve uygular; bir sonraki yoklamaya kadar beklenecek süreyi döner.
func (c *Client) pollJobs(ctx context.Context) (time.Duration, error) {
	var resp jobsResponse
	status, err := c.call(ctx, http.MethodGet, "/api/probe/jobs", nil, &resp)
	if err != nil {
		c.unreachable("jobs", err)
		return 0, err
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		c.log.Error("ana sunucu isteği reddetti: token geçersiz veya kontrol noktası devre dışı; kontroller durduruldu", "durum", status)
		c.reachable("jobs")
		return 0, ErrUnauthorized
	}
	if status != http.StatusOK {
		err := fmt.Errorf("beklenmeyen yanıt: %d", status)
		c.unreachable("jobs", err)
		return 0, err
	}
	c.reachable("jobs")
	c.apply(ctx, resp.Jobs)
	c.setMetricsInterval(resp.MetricsInterval)
	poll := resp.PollAfter
	if poll <= 0 {
		poll = 30
	}
	return time.Duration(poll) * c.cfg.Unit, nil
}

// apply iş listesini çalışan işlerle eşitler: kaldırılan veya değişen iş
// durdurulur, yeni veya değişen iş başlatılır.
func (c *Client) apply(ctx context.Context, jobs []Job) {
	want := map[int64]Job{}
	for _, j := range jobs {
		if _, ok := check.Get(j.Type); !ok || j.Type == check.TypePush || j.Type == check.TypeGroup {
			c.mu.Lock()
			first := !c.unknown[j.Type]
			c.unknown[j.Type] = true
			c.mu.Unlock()
			if first {
				c.log.Warn("bu monitör tipi kontrol noktasında çalıştırılamıyor (sürümler farklı olabilir)", "tip", j.Type, "monitor", j.Name)
			}
			continue
		}
		want[j.ID] = j
	}
	var stop []*task
	var start []Job
	c.mu.Lock()
	for id, t := range c.tasks {
		j, ok := want[id]
		if ok {
			def, _ := json.Marshal(j)
			if bytes.Equal(def, t.def) {
				delete(want, id)
				continue
			}
		}
		stop = append(stop, t)
		delete(c.tasks, id)
	}
	for _, j := range want {
		start = append(start, j)
	}
	c.mu.Unlock()

	for _, t := range stop {
		t.cancel()
		<-t.done
	}
	for _, j := range start {
		def, _ := json.Marshal(j)
		tctx, cancel := context.WithCancel(ctx)
		t := &task{def: def, cancel: cancel, done: make(chan struct{})}
		c.mu.Lock()
		c.tasks[j.ID] = t
		c.mu.Unlock()
		go c.runJob(tctx, j, t.done)
	}
	if len(stop) > 0 || len(start) > 0 {
		c.log.Info("iş listesi güncellendi", "monitor_sayisi", len(jobs), "baslayan", len(start), "duran", len(stop))
	}
}

func (c *Client) stopAll() {
	c.mu.Lock()
	tasks := c.tasks
	c.tasks = map[int64]*task{}
	c.mu.Unlock()
	for _, t := range tasks {
		t.cancel()
	}
	for _, t := range tasks {
		<-t.done
	}
}

// runJob bir monitörü kendi aralığında kontrol eder. Başarısız kontrolden
// sonra tekrar deneme aralığı kullanılır (hata sayımı ana sunucuda yapılır).
func (c *Client) runJob(ctx context.Context, j Job, done chan struct{}) {
	defer close(done)
	interval := time.Duration(max(j.Interval, 1)) * c.cfg.Unit
	retry := time.Duration(max(j.RetryInterval, 1)) * c.cfg.Unit
	// İlk kontrol kaydırılır: tüm kontroller aynı ana yığılmasın.
	first := time.Duration(rand.Int64N(int64(min(interval, 10*c.cfg.Unit)) + 1))
	timer := time.NewTimer(first)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		res, ok := c.checkOnce(ctx, j)
		if !ok {
			return // durduruldu: yarım kalan kontrol gönderilmez
		}
		c.add(res)
		if res.Up {
			timer.Reset(interval)
		} else {
			timer.Reset(retry)
		}
	}
}

func (c *Client) checkOnce(ctx context.Context, j Job) (out Result, ok bool) {
	checker, _ := check.Get(j.Type)
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return out, false
	}
	defer func() { <-c.sem }()
	cctx, cancel := context.WithTimeout(ctx, time.Duration(max(j.Timeout, 1))*c.cfg.Unit)
	defer cancel()
	var res check.Result
	func() {
		defer func() {
			if p := recover(); p != nil {
				c.log.Error("kontrol paniği", "monitor", j.Name, "panik", p)
				res = check.Result{PingMs: -1, Message: "İç hata"}
			}
		}()
		res = checker.Check(cctx, j.Config)
	}()
	if ctx.Err() != nil {
		return out, false
	}
	out = Result{MonitorID: j.ID, Time: time.Now().UnixMilli(), Up: res.Up && !res.Pending, PingMs: res.PingMs, Message: res.Message}
	if res.Cert != nil {
		out.CertNotAfter, out.CertIssuer = res.Cert.NotAfter.Unix(), res.Cert.Issuer
	}
	if !out.Up {
		out.Detail = res.Detail
	}
	c.log.Debug("kontrol", "monitor", j.Name, "çalışıyor", out.Up, "ping", out.PingMs, "mesaj", out.Message)
	return out, true
}

// Sonuç tamponu ve gönderim ---------------------------------------------------------------

func (c *Client) add(r Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, r)
	c.trimLocked()
}

// trimLocked tampon sınırını aşan en eski sonuçları düşürür.
func (c *Client) trimLocked() {
	if over := len(c.buf) - c.cfg.MaxBuffer; over > 0 {
		c.buf = append([]Result(nil), c.buf[over:]...)
		c.dropped += over
	}
}

func (c *Client) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buf)
}

func (c *Client) clearBuffer() {
	c.mu.Lock()
	c.buf = nil
	c.mu.Unlock()
}

func (c *Client) flushLoop(ctx context.Context) {
	backoff := time.Duration(0)
	wait := c.cfg.FlushEvery
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = c.cfg.FlushEvery
		for c.pending() > 0 && ctx.Err() == nil {
			retryAfter, err := c.flushOnce(ctx)
			if err != nil {
				backoff = max(c.nextBackoff(backoff), retryAfter)
				wait = backoff
				break
			}
			backoff = 0
		}
		c.mu.Lock()
		dropped := c.dropped
		c.dropped = 0
		c.mu.Unlock()
		if dropped > 0 {
			c.log.Warn("sonuç tamponu doldu, en eski sonuçlar atıldı", "atilan", dropped)
		}
	}
}

// flushOnce tamponun başından bir parti gönderir. Geçici hatada (ağ, 5xx,
// 429) parti tampona geri konur ve hata döner; kalıcı hatada (400, 413,
// 401/403) parti atılır.
func (c *Client) flushOnce(ctx context.Context) (retryAfter time.Duration, err error) {
	c.mu.Lock()
	n := min(len(c.buf), c.cfg.BatchSize)
	batch := append([]Result(nil), c.buf[:n]...)
	c.buf = c.buf[n:]
	c.mu.Unlock()
	if n == 0 {
		return 0, nil
	}
	requeue := func() {
		c.mu.Lock()
		c.buf = append(batch, c.buf...)
		c.trimLocked()
		c.mu.Unlock()
	}
	body := map[string]any{"sent_at": time.Now().UnixMilli(), "results": limitDetails(batch)}
	var resp struct {
		Accepted int `json:"accepted"`
		Rejected []struct {
			MonitorID int64  `json:"monitor_id"`
			Error     string `json:"error"`
		} `json:"rejected"`
	}
	status, hdr, err := c.callH(ctx, http.MethodPost, "/api/probe/results", body, &resp)
	switch {
	case err != nil:
		requeue()
		c.unreachable("results", err)
		return 0, err
	case status == http.StatusOK:
		c.reachable("results")
		if len(resp.Rejected) > 0 {
			c.log.Warn("bazı sonuçlar reddedildi", "reddedilen", len(resp.Rejected), "ilk_neden", resp.Rejected[0].Error)
		}
		return 0, nil
	case status == http.StatusTooManyRequests || status >= 500:
		requeue()
		err := fmt.Errorf("sunucu yanıtı: %d", status)
		c.unreachable("results", err)
		secs, _ := strconv.Atoi(hdr.Get("Retry-After"))
		return time.Duration(secs) * time.Second, err
	default:
		c.reachable("results")
		c.log.Error("sonuçlar kabul edilmedi, parti atıldı", "durum", status, "sonuc_sayisi", len(batch))
		return 0, nil
	}
}

// limitDetails partideki ayrıntıların toplam boyutunu maxBatchDetail ile
// sınırlar; sınırı aşan sonuçlar ayrıntısız gider (tampondakiler değişmez).
func limitDetails(batch []Result) []Result {
	budget := maxBatchDetail
	var out []Result
	for i, r := range batch {
		if r.Detail == nil {
			continue
		}
		b, _ := json.Marshal(r.Detail)
		if len(b) <= budget {
			budget -= len(b)
			continue
		}
		if out == nil {
			out = append([]Result(nil), batch...)
		}
		out[i].Detail = nil
	}
	if out == nil {
		return batch
	}
	return out
}

// Sunucu metrikleri --------------------------------------------------------------------------

// setMetricsInterval metrik döngüsüne yeni aralığı bildirir; henüz alınmamış
// eski değer atılır, döngü yalnızca en yenisini görür.
func (c *Client) setMetricsInterval(n int) {
	if c.cfg.NoMetrics {
		return
	}
	select {
	case <-c.metricsIv:
	default:
	}
	c.metricsIv <- max(n, 0)
}

// metricsLoop ana sunucunun istediği aralıkla örnek alıp gönderir; aralık 0
// iken bekler. Başlarken ilk örnek yalnızca sayaçları hazırlar, ilk gönderim
// kurulumdan sonra çabuk görünsün diye en geç 10 birim sonra yapılır.
// Kontrollerden ve sonuç gönderiminden bağımsız çalışır.
func (c *Client) metricsLoop(ctx context.Context) {
	var interval time.Duration
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()
	lastReason := ""
	for {
		select {
		case <-ctx.Done():
			return
		case n := <-c.metricsIv:
			if n > 0 {
				n = max(n, minMetricsInterval)
			}
			iv := time.Duration(n) * c.cfg.Unit
			if iv == interval {
				continue
			}
			was := interval
			interval = iv
			timer.Stop()
			switch {
			case iv == 0:
				c.log.Info("sunucu metrik gönderimi kapatıldı")
			case was == 0:
				// Uzun aradan sonra hızlar eski sayaçlarla hesaplanmasın.
				c.cfg.Metrics.Reset()
				lastReason, c.metricsBad = "", 0
				c.log.Info("sunucu metrikleri gönderiliyor", "aralik", n)
				timer.Reset(0)
			default:
				c.log.Info("sunucu metrik aralığı değişti", "aralik", n)
				timer.Reset(iv)
			}
		case <-timer.C:
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			s, ok := c.cfg.Metrics.Collect(cctx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if !ok {
				timer.Reset(min(interval, 10*c.cfg.Unit)) // hazırlık örneği gönderilmez
				continue
			}
			timer.Reset(interval)
			if s.Unavailable != lastReason {
				if s.Unavailable != "" {
					c.log.Warn("sunucu metrikleri toplanamıyor", "neden", s.Unavailable)
				}
				lastReason = s.Unavailable
			}
			c.sendMetrics(ctx, s)
		}
	}
}

// sendMetrics tek örneği gönderir. Gönderilemeyen örnek saklanmaz, bir sonraki
// aralıkta yerine yenisi gider (eski ölçümleri biriktirmenin anlamı yok).
// 401/403'te iş listesi yoklaması da aynı yanıtı alır ve metrikleri durdurur.
func (c *Client) sendMetrics(ctx context.Context, s metrics.Sample) {
	status, err := c.call(ctx, http.MethodPost, "/api/probe/metrics", s, nil)
	switch {
	case err != nil:
		if ctx.Err() == nil {
			c.unreachable("metrics", err)
		}
	case status >= 200 && status < 300:
		c.reachable("metrics")
		c.metricsBad = 0
	case status == http.StatusTooManyRequests || status >= 500:
		c.unreachable("metrics", fmt.Errorf("sunucu yanıtı: %d", status))
	default:
		// 400/413 veya yetki hatası: aynı örneği tekrar göndermek düzeltmez.
		c.reachable("metrics")
		if status != c.metricsBad {
			c.metricsBad = status
			c.log.Warn("metrik örneği kabul edilmedi", "durum", status)
		}
	}
}

// HTTP ------------------------------------------------------------------------------------

func (c *Client) call(ctx context.Context, method, path string, in, out any) (int, error) {
	status, _, err := c.callH(ctx, method, path, in, out)
	return status, err
}

func (c *Client) callH(ctx context.Context, method, path string, in, out any) (int, http.Header, error) {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("X-Probe-Version", c.cfg.Version)
	req.Header.Set("User-Agent", "uptime-probe/"+c.cfg.Version)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return 0, nil, err
	}
	if resp.StatusCode == http.StatusOK && out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			return 0, nil, fmt.Errorf("yanıt çözülemedi: %w", err)
		}
	}
	return resp.StatusCode, resp.Header, nil
}

func (c *Client) nextBackoff(cur time.Duration) time.Duration {
	if cur <= 0 {
		return min(2*c.cfg.Unit, c.cfg.MaxBackoff)
	}
	return min(2*cur, c.cfg.MaxBackoff)
}

// unreachable/reachable ana sunucuya erişim durumunu yalnızca değiştiğinde loglar.
func (c *Client) unreachable(what string, err error) {
	c.mu.Lock()
	was, known := c.reach[what]
	c.reach[what] = false
	c.mu.Unlock()
	if !known || was {
		c.log.Warn("ana sunucuya ulaşılamıyor, tekrar denenecek", "istek", what, "hata", err)
	}
}

func (c *Client) reachable(what string) {
	c.mu.Lock()
	was, known := c.reach[what]
	c.reach[what] = true
	c.mu.Unlock()
	if known && !was {
		c.log.Info("ana sunucuya tekrar ulaşıldı", "istek", what)
	}
}
