//go:build linux

package metrics

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/sensors"
	"golang.org/x/sys/unix"
)

// systemSource Linux'ta değerleri okur. gopsutil HOST_PROC, HOST_SYS ve
// HOST_ETC ortam değişkenlerini kendisi uygular; bağlama listesi, disk
// doluluğu, disk G/Ç ve ağ sayaçları burada host yollarıyla okunur:
//
//   - bölümler HOST_PROC/1/mountinfo'dan (host'un init sürecinin bağlama ad
//     alanı; --pid host olmadan da host procfs'i olduğu için doğrudur), okunamazsa
//     ajanın kendi mountinfo'sundaki HOST_ROOT altı bağlamalardan,
//   - doluluk statfs(HOST_ROOT + bağlama noktası) ile,
//   - ağ HOST_PROC/1/net/dev'den (host'un ağ ad alanı; --network host olmasa da);
//     köprü, bond üyesi ve VLAN arayüzleri HOST_SYS/class/net ve
//     HOST_PROC/1/net/vlan'a bakılarak ayıklanır (netKeeper).
type systemSource struct {
	proc, root, etc, sys string

	// Bloklayabilen syscall'lar alan olarak tutulur: testte sahtesi verilir.
	// diskTimeout, usage'daki Stat/Statfs için üst süre sınırıdır.
	stat        func(string, *syscall.Stat_t) error
	statfs      func(string, *syscall.Statfs_t) error
	diskTimeout time.Duration

	// inflight zaman aşımına uğramış ve hâlâ dönmemiş disk sorgularının
	// bağlama noktaları: askıdaki bir bağlama için her toplamada yeni bir
	// goroutine (ve çekirdekte takılı bir iş parçacığı) açılmaz.
	mu       sync.Mutex
	inflight map[string]bool
}

func newSystemSource(getenv func(string) string) (source, string) {
	s := &systemSource{
		proc:        strings.TrimRight(getenv("HOST_PROC"), "/"),
		root:        strings.TrimRight(getenv("HOST_ROOT"), "/"),
		etc:         strings.TrimRight(getenv("HOST_ETC"), "/"),
		sys:         strings.TrimRight(getenv("HOST_SYS"), "/"),
		stat:        syscall.Stat,
		statfs:      syscall.Statfs,
		diskTimeout: 2 * time.Second,
	}
	if s.proc == "" {
		s.proc = "/proc"
	}
	if s.sys == "" {
		s.sys = "/sys"
	}
	return s, ""
}

// keepDisk ve keepNet collector'ın ioFilter arayüzü: diskler adlarına göre,
// ağ arayüzleri adlarına ve host'un sysfs/procfs bilgisine göre süzülür.
func (s *systemSource) keepDisk(name string) bool { return isPhysicalDisk(name) }

func (s *systemSource) keepNet(name string) bool {
	return netKeeper(s.sys, filepath.Join(s.proc, "1"))(name)
}

func (s *systemSource) hostInfo(ctx context.Context) (Host, error) {
	h := Host{OS: runtime.GOOS, Hostname: s.hostname()}
	if p, _, v, err := host.PlatformInformationWithContext(ctx); err == nil {
		h.Platform = strings.TrimSpace(p + " " + v)
	}
	h.Kernel, _ = host.KernelVersionWithContext(ctx)
	h.Arch, _ = host.KernelArch()
	if infos, err := cpu.InfoWithContext(ctx); err == nil && len(infos) > 0 {
		h.CPUModel = strings.Join(strings.Fields(infos[0].ModelName), " ")
		if h.CPUModel == "" {
			h.CPUModel = strings.TrimSpace(infos[0].VendorID + " " + infos[0].Model)
		}
	}
	h.Cores, _ = cpu.CountsWithContext(ctx, false)
	h.Threads, _ = cpu.CountsWithContext(ctx, true)
	if h.Threads <= 0 {
		h.Threads = runtime.NumCPU()
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err == nil {
		h.MemTotal = vm.Total
	}
	bt, err := host.BootTimeWithContext(ctx)
	h.BootTime = int64(bt)
	return h, err
}

// hostname konteynerde (HOST_ETC verilmişse) host'un /etc/hostname dosyasından,
// değilse çekirdekten okunur; --network host olmayan konteynerin adı
// konteyner kimliği olurdu.
func (s *systemSource) hostname() string {
	if s.etc != "" {
		if b, err := os.ReadFile(filepath.Join(s.etc, "hostname")); err == nil {
			if n := strings.TrimSpace(string(b)); n != "" {
				return n
			}
		}
	}
	n, _ := os.Hostname()
	return n
}

func (s *systemSource) cpuTimes(ctx context.Context) (total, busy float64, err error) {
	ts, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return 0, 0, err
	}
	if len(ts) == 0 {
		return 0, 0, errors.New("cpu süreleri boş")
	}
	t := ts[0]
	// guest süreleri user/nice'ın içinde olduğu için ayrıca eklenmez.
	total = t.User + t.Nice + t.System + t.Idle + t.Iowait + t.Irq + t.Softirq + t.Steal
	return total, total - t.Idle - t.Iowait, nil
}

func (s *systemSource) load(ctx context.Context) (l1, l5, l15 float64, err error) {
	a, err := load.AvgWithContext(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	return a.Load1, a.Load5, a.Load15, nil
}

func (s *systemSource) memory(ctx context.Context) (memStat, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return memStat{}, err
	}
	return memStat{Total: vm.Total, Available: vm.Available, Free: vm.Free, Buffers: vm.Buffers, Cached: vm.Cached,
		SReclaimable: vm.Sreclaimable, SwapTotal: vm.SwapTotal, SwapFree: vm.SwapFree}, nil
}

func (s *systemSource) mounts(context.Context) ([]mount, error) {
	if s.proc != "/proc" && s.root == "" {
		// Host'un /proc'u var ama kök dizini yok: statfs konteynerin kendi
		// dosya sistemini ölçerdi.
		return nil, errors.New("HOST_ROOT verilmemiş, diskler okunamıyor")
	}
	if b, err := os.ReadFile(filepath.Join(s.proc, "1", "mountinfo")); err == nil {
		return parseMountinfo(string(b), ""), nil
	}
	b, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	return parseMountinfo(string(b), s.root), nil
}

type diskUsage struct {
	total, used uint64
	ok          bool
}

// usage bölümün doluluğu. Total, df'teki gibi kullanılan + kullanılabilir
// alandır (root'a ayrılan pay hariç), böylece Used/Total df'teki Use% ile aynı olur.
// Konteynerde HOST_ROOT altındaki yol başka bir aygıta çıkıyorsa (bağlama
// konteynere yansımamış) bölüm atlanır; yoksa üst dizinin değerleri yanlışlıkla
// bu bölüme yazılırdı.
//
// Stat/Statfs bloklayan çağrılardır: ölü veya askıda bir bağlama (yanıt vermeyen
// ağ diski, kopan USB, arızalı NFS) bunları süresiz bekletebilir. Bu yüzden asıl
// syscall'lar süreli bir goroutine'de çalıştırılır; diskTimeout içinde dönmezse
// bölüm "bilinmiyor" sayılıp atlanır (ok=false). Böylece tek bir kötü disk tüm
// metrik toplamayı (ve toplayıcının kilidini) süresiz bloklamaz. Askıdaki
// goroutine yalnızca syscall çekirdekte gerçekten takılı kaldığı sürece yaşar.
// Önceki sorgusu hâlâ dönmemiş bağlama sonraki toplamalarda hiç sorgulanmaz
// (bilinmiyor sayılır): askıdaki bağlama başına en fazla bir goroutine olur,
// toplamalar boyunca birikmez. Sorgu dönünce bağlama yeniden ölçülür.
func (s *systemSource) usage(m mount) (total, used uint64, ok bool) {
	key := s.root + m.Point
	s.mu.Lock()
	if s.inflight[key] {
		s.mu.Unlock()
		return 0, 0, false
	}
	if s.inflight == nil {
		s.inflight = map[string]bool{}
	}
	s.inflight[key] = true
	s.mu.Unlock()
	r, timedOut := callWithTimeout(s.diskTimeout, func() diskUsage {
		defer func() {
			s.mu.Lock()
			delete(s.inflight, key)
			s.mu.Unlock()
		}()
		t, u, k := s.usageBlocking(m)
		return diskUsage{total: t, used: u, ok: k}
	})
	if timedOut {
		return 0, 0, false
	}
	return r.total, r.used, r.ok
}

// usageBlocking usage'ın syscall yapan çekirdeğidir (goroutine içinde çağrılır).
func (s *systemSource) usageBlocking(m mount) (total, used uint64, ok bool) {
	p := s.root + m.Point
	if s.root != "" && m.Point != "/" && m.FS != "btrfs" {
		var st syscall.Stat_t
		if s.stat(p, &st) != nil {
			return 0, 0, false
		}
		dev := strconv.FormatUint(uint64(unix.Major(uint64(st.Dev))), 10) + ":" + strconv.FormatUint(uint64(unix.Minor(uint64(st.Dev))), 10)
		if dev != m.Dev {
			return 0, 0, false
		}
	}
	var fs syscall.Statfs_t
	if s.statfs(p, &fs) != nil {
		return 0, 0, false
	}
	bs := uint64(fs.Bsize)
	used = (fs.Blocks - fs.Bfree) * bs
	return used + fs.Bavail*bs, used, true
}

// diskIO /proc/diskstats'tan okunan/yazılan baytlar (sektör her zaman 512 bayt).
func (s *systemSource) diskIO(context.Context) (map[string]ioCounter, error) {
	f, err := os.Open(filepath.Join(s.proc, "diskstats"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]ioCounter{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fl := strings.Fields(sc.Text())
		if len(fl) < 10 {
			continue
		}
		r, err1 := strconv.ParseUint(fl[5], 10, 64)
		w, err2 := strconv.ParseUint(fl[9], 10, 64)
		if err1 == nil && err2 == nil {
			out[fl[2]] = ioCounter{A: r * 512, B: w * 512}
		}
	}
	return out, sc.Err()
}

func (s *systemSource) netIO(ctx context.Context) (map[string]ioCounter, error) {
	st, err := net.IOCountersByFileWithContext(ctx, true, filepath.Join(s.proc, "1", "net", "dev"))
	if err != nil {
		st, err = net.IOCountersWithContext(ctx, true)
		if err != nil {
			return nil, err
		}
	}
	out := make(map[string]ioCounter, len(st))
	for _, n := range st {
		out[n.Name] = ioCounter{A: n.BytesRecv, B: n.BytesSent}
	}
	return out, nil
}

func (s *systemSource) temps(ctx context.Context) ([]Temp, error) {
	ts, err := sensors.TemperaturesWithContext(ctx) // kısmi okumada uyarıyla birlikte sonuç döner
	out := make([]Temp, 0, len(ts))
	for _, t := range ts {
		out = append(out, Temp{Name: t.SensorKey, C: t.Temperature})
	}
	return out, err
}
