package metrics

import (
	"context"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// NoHostReason konteynerde host dizinleri bağlanmadan çalışan ajanın bildirdiği neden.
const NoHostReason = "Ajan konteyner içinde çalışıyor ama sunucunun /proc ve /sys dizinleri bağlanmamış; kurulum komutunu güncelleyin"

// Options toplayıcı ayarları. Sıfır değerli alanlar varsayılanla doldurulur;
// fonksiyon alanları testlerde sahte ortam vermek içindir.
type Options struct {
	Getenv        func(string) string // varsayılan os.Getenv (HOST_PROC, HOST_ROOT, DOCKER_HOST…)
	InContainer   func() bool         // varsayılan DetectContainer
	DockerSocket  string              // "" → DOCKER_HOST veya /var/run/docker.sock; "-" → Docker kapalı
	DockerTimeout time.Duration       // Docker sorgularının toplam süresi (varsayılan 5 sn)
	Now           func() time.Time
	Log           *slog.Logger
}

// Collector sunucunun ölçümlerini toplar. Hızlar (CPU, disk G/Ç, ağ,
// konteyner CPU/ağ) ardışık iki Collect çağrısındaki kümülatif sayaçların
// farkıdır; bu yüzden ilk çağrı yalnızca sayaçları hazırlar.
type Collector struct {
	opts   Options
	src    source
	docker *dockerClient
	reason string // boş değilse metrik toplanamıyor (host görünmüyor vb.)

	mu        sync.Mutex
	host      Host
	hostAt    time.Time
	prev      *counters
	prevCont  map[string]contCounters
	dockerErr string // son Docker hatası (log tekrarını önler)
	dockerOK  bool
}

// source işletim sisteminden ham değerleri okur (Linux'ta gopsutil ve
// /proc, /sys dosyaları); testlerde sahtesi kullanılır.
type source interface {
	hostInfo(ctx context.Context) (Host, error) // Docker alanı hariç
	cpuTimes(ctx context.Context) (total, busy float64, err error)
	load(ctx context.Context) (l1, l5, l15 float64, err error)
	memory(ctx context.Context) (memStat, error)
	mounts(ctx context.Context) ([]mount, error)              // host yollarıyla bağlama listesi
	usage(m mount) (total, used uint64, ok bool)              // ok=false: bölüm görünmüyor
	diskIO(ctx context.Context) (map[string]ioCounter, error) // aygıt → okunan/yazılan bayt
	netIO(ctx context.Context) (map[string]ioCounter, error)  // arayüz → alınan/gönderilen bayt
	temps(ctx context.Context) ([]Temp, error)
}

// ioFilter G/Ç anahtarlarını kendisi süzen kaynak (Windows: diskler sürücü
// harfi, ağ arayüzleri kaynakta süzülür). Yoksa Linux aygıt/arayüz adlarına
// göre süzülür (isPhysicalDisk, isVirtualNet).
type ioFilter interface {
	keepDisk(name string) bool
	keepNet(name string) bool
}

type memStat struct {
	Total, Available, Free, Buffers, Cached, SReclaimable uint64
	SwapTotal, SwapFree                                   uint64
}

// ioCounter iki kümülatif sayaç: disklerde okunan/yazılan, ağda alınan/gönderilen bayt.
type ioCounter struct{ A, B uint64 }

type counters struct {
	at       time.Time
	cpuOK    bool
	cpuTotal float64
	cpuBusy  float64
	disk     map[string]ioCounter
	net      map[string]ioCounter
}

// NewCollector gerçek sistemi okuyan toplayıcı. Host görünmüyorsa (konteynerde
// HOST_PROC yok) veya işletim sistemi desteklenmiyorsa Collect yalnızca
// Unavailable nedeni döner. Windows'ta konteyner tespiti yapılmaz ve Docker
// istatistikleri toplanmaz.
func NewCollector(o Options) *Collector {
	o = o.withDefaults()
	src, reason := newSystemSource(o.Getenv)
	if reason == "" && runtime.GOOS == "linux" {
		reason = visibility(o.Getenv, o.InContainer(), fileExists)
	}
	if runtime.GOOS == "windows" && o.DockerSocket == "" {
		o.DockerSocket = "-"
	}
	return newCollector(o, src, reason)
}

func newCollector(o Options, src source, reason string) *Collector {
	o = o.withDefaults()
	return &Collector{opts: o, src: src, reason: reason, docker: newDockerClient(dockerEndpoint(o))}
}

func (o Options) withDefaults() Options {
	if o.Getenv == nil {
		o.Getenv = os.Getenv
	}
	if o.InContainer == nil {
		o.InContainer = DetectContainer
	}
	if o.DockerTimeout <= 0 {
		o.DockerTimeout = 5 * time.Second
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return o
}

// Unavailable metrik toplanamıyorsa nedeni ("" ise toplanabiliyor).
func (c *Collector) Unavailable() string { return c.reason }

// Reset önceki sayaçları unutur; sonraki Collect yeniden hazırlık örneği olur
// (ör. metrik gönderimi uzun süre kapalı kaldıktan sonra).
func (c *Collector) Reset() {
	c.mu.Lock()
	c.prev, c.prevCont = nil, nil
	c.mu.Unlock()
}

// Collect bir örnek alır. İlk çağrıda (veya Reset sonrası) sayaçları hazırlar
// ve false döner; bu örnek gönderilmez. Toplanamıyorsa Unavailable dolu örnek
// ve true döner. Tek tek okunamayan değerler (ör. sıcaklık) boş kalır.
func (c *Collector) Collect(ctx context.Context) (Sample, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.opts.Now()
	s := Sample{Time: now.UnixMilli()}
	if c.reason != "" {
		s.Unavailable = c.reason
		return s, true
	}

	host := c.hostInfo(ctx, now)
	st := &Stats{MemTotal: host.MemTotal}
	cur := &counters{at: now}
	if total, busy, err := c.src.cpuTimes(ctx); err == nil {
		cur.cpuOK, cur.cpuTotal, cur.cpuBusy = true, total, busy
	}
	if l1, l5, l15, err := c.src.load(ctx); err == nil {
		st.Load1, st.Load5, st.Load15 = round2(l1), round2(l5), round2(l15)
	}
	if m, err := c.src.memory(ctx); err == nil {
		fillMemory(st, m)
		host.MemTotal = m.Total
	}
	if host.BootTime > 0 {
		st.Uptime = max(now.Unix()-host.BootTime, 0)
	}
	if ms, err := c.src.mounts(ctx); err == nil {
		st.Disks = selectDisks(ms, c.src.usage)
	}
	keepDisk, keepNet := isPhysicalDisk, func(n string) bool { return !isVirtualNet(n) }
	if f, ok := c.src.(ioFilter); ok {
		keepDisk, keepNet = f.keepDisk, f.keepNet
	}
	if io, err := c.src.diskIO(ctx); err == nil {
		cur.disk = filterKeys(io, keepDisk)
	}
	if io, err := c.src.netIO(ctx); err == nil {
		cur.net = filterKeys(io, keepNet)
	}
	if ts, err := c.src.temps(ctx); err == nil || len(ts) > 0 {
		st.Temps = filterTemps(ts)
	}
	st.Containers, host.Docker = c.containers(ctx, host.Threads, host.MemTotal)

	prev := c.prev
	c.prev = cur
	dt := 0.0
	if prev != nil {
		dt = cur.at.Sub(prev.at).Seconds()
	}
	if dt <= 0 {
		return s, false // ilk örnek: yalnızca sayaçlar hazırlandı
	}
	if prev.cpuOK && cur.cpuOK {
		st.CPU = cpuPct(prev.cpuTotal, prev.cpuBusy, cur.cpuTotal, cur.cpuBusy)
	}
	st.DiskReadBps, st.DiskWriteBps = rates(prev.disk, cur.disk, dt)
	st.NetRxBps, st.NetTxBps = rates(prev.net, cur.net, dt)
	s.Host, s.Stats = &host, st
	return s, true
}

// hostInfo sabit bilgileri 10 dakikada bir yeniler.
func (c *Collector) hostInfo(ctx context.Context, now time.Time) Host {
	if c.hostAt.IsZero() || now.Sub(c.hostAt) >= 10*time.Minute {
		if h, err := c.src.hostInfo(ctx); err == nil || h.Hostname != "" {
			c.host, c.hostAt = h, now
		}
	}
	return c.host
}

// fillMemory RAM değerlerini doldurur. MemUsed = toplam - kullanılabilir
// (procps 4'teki "free" ile aynı "used"); MemCache = tampon + önbellek +
// geri alınabilir slab ("buff/cache"). free'deki buff/cache, geri
// alınamayan kısmı (paylaşımlı bellek, tmpfs) used ile örtüştüğü için
// used + buff/cache + free toplamı aşabilir; cache, grafikte
// used + cache + boş = toplam olacak şekilde toplam - used - boş ile sınırlanır.
func fillMemory(st *Stats, m memStat) {
	st.MemTotal = m.Total
	st.MemUsed = m.Total - min(m.Available, m.Total)
	st.MemCache = min(m.Buffers+m.Cached+m.SReclaimable, m.Total-st.MemUsed-min(m.Free, m.Total-st.MemUsed))
	st.SwapTotal = m.SwapTotal
	st.SwapUsed = m.SwapTotal - min(m.SwapFree, m.SwapTotal)
}

// cpuPct iki okuma arasındaki meşgul süre oranı (%).
func cpuPct(prevTotal, prevBusy, total, busy float64) float64 {
	dt, db := total-prevTotal, busy-prevBusy
	if dt <= 0 || db < 0 {
		return 0
	}
	return round2(min(100*db/dt, 100))
}

// rates iki sayaç kümesinden saniyelik toplam hızı hesaplar. Yalnızca iki
// okumada da olan anahtarlar sayılır; geriye giden sayaç (aygıt yeniden
// oluşturuldu, taşma) o anahtar için atlanır.
func rates(prev, cur map[string]ioCounter, dt float64) (a, b float64) {
	if dt <= 0 {
		return 0, 0
	}
	for k, c := range cur {
		p, ok := prev[k]
		if !ok {
			continue
		}
		if c.A >= p.A {
			a += float64(c.A - p.A)
		}
		if c.B >= p.B {
			b += float64(c.B - p.B)
		}
	}
	return math.Round(a / dt), math.Round(b / dt)
}

func filterKeys(m map[string]ioCounter, keep func(string) bool) map[string]ioCounter {
	out := make(map[string]ioCounter, len(m))
	for k, v := range m {
		if keep(k) {
			out[k] = v
		}
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// Host görünürlüğü -------------------------------------------------------------------------

// visibility metrik toplanamıyorsa nedenini döner. Konteynerde çalışan ajan
// host'un /proc'unu (HOST_PROC) görmüyorsa ölçtüğü değerler konteynerin
// kendisine ait olur ve yanıltır; bu durumda hiç toplanmaz.
func visibility(getenv func(string) string, inContainer bool, exists func(string) bool) string {
	hp := getenv("HOST_PROC")
	if hp == "" {
		if inContainer {
			return NoHostReason
		}
		return ""
	}
	if !exists(filepath.Join(hp, "stat")) {
		return "HOST_PROC=" + hp + " altında /proc bulunamadı; sunucunun kök dizini bağlanmamış (-v /:/host:ro), kurulum komutunu kontrol edin"
	}
	return ""
}

// DetectContainer ajanın bir konteyner içinde çalışıp çalışmadığını tahmin eder.
func DetectContainer() bool {
	for _, f := range []string{"/.dockerenv", "/run/.containerenv"} {
		if fileExists(f) {
			return true
		}
	}
	for _, f := range []string{"/proc/1/cgroup", "/proc/self/cgroup"} {
		b, _ := os.ReadFile(f)
		if cgroupInContainer(string(b)) {
			return true
		}
	}
	return false
}

func cgroupInContainer(s string) bool {
	for _, k := range []string{"docker", "containerd", "kubepods", "libpod"} {
		if strings.Contains(s, k) {
			return true
		}
	}
	return false
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
