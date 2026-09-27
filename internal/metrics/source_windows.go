//go:build windows

package metrics

import (
	"context"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"golang.org/x/sys/windows"
)

// systemSource Windows'ta değerleri okur (gopsutil + Win32 API):
//
//   - CPU: tüm işlemcilerin kullanıcı/çekirdek/boşta süreleri (kesme süresi
//     çekirdek süresinin içindedir),
//   - yük: Windows'ta yük ortalaması yoktur; gopsutil'in "\System\Processor
//     Queue Length" sayacından 5 sn'de bir hesapladığı üstel ortalama kullanılır
//     (psutil'deki taklit). Yalnızca işlemci bekleyen iş parçacıklarını sayar,
//     çalışanları saymaz; bu yüzden Linux'taki yükten düşük çıkar,
//   - RAM: kullanılan = toplam - kullanılabilir; önbellek = bekleme listesi
//     (standby) sayaçları, okunamazsa 0; swap = sayfa dosyaları,
//   - diskler: yerel sabit sürücüler ve klasöre bağlanmış sabit birimler,
//   - disk G/Ç: sabit sürücü harflerinin sayaçları,
//   - ağ: sanal olmayan bağdaştırıcılar (winfilters.go),
//   - sıcaklık: okunmaz (WMI termal bölgeleri çoğu sunucuda yok veya anlamsız),
//   - Docker: kapalı (Collector).
//
// Host görünürlüğü/konteyner tespiti yalnızca Linux içindir; Windows'ta her
// zaman toplanır.
type systemSource struct {
	standby *pdhQuery // bekleme listesi sayaçları; açılamadıysa nil
	once    sync.Once
}

func newSystemSource(func(string) string) (source, string) {
	return &systemSource{}, ""
}

// ioFilter: G/Ç anahtarları Linux'taki aygıt adları değil; diskler sürücü
// harfi, ağ arayüzleri netIO'da zaten süzülmüş.
func (s *systemSource) keepDisk(name string) bool { return isWinDriveKey(name) }
func (s *systemSource) keepNet(string) bool       { return true }

func (s *systemSource) hostInfo(ctx context.Context) (Host, error) {
	h := Host{OS: runtime.GOOS}
	h.Hostname, _ = os.Hostname()
	if p, _, v, err := host.PlatformInformationWithContext(ctx); err == nil {
		h.Platform = winPlatform(p, v)
	}
	if k, err := host.KernelVersionWithContext(ctx); err == nil {
		h.Kernel = winKernel(k)
	}
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

func (s *systemSource) cpuTimes(ctx context.Context) (total, busy float64, err error) {
	ts, err := cpu.TimesWithContext(ctx, false)
	if err != nil {
		return 0, 0, err
	}
	if len(ts) == 0 {
		return 0, 0, errors.New("cpu süreleri boş")
	}
	t := ts[0]
	// gopsutil System = çekirdek - boşta; Irq çekirdek süresinin içinde olduğu için eklenmez.
	total = t.User + t.System + t.Idle
	return total, t.User + t.System, nil
}

// load gopsutil'in işlemci kuyruğu ortalaması. Arka plan örnekleyicisi ilk
// çağrının bağlamıyla başlar; Collect'in zaman aşımlı bağlamı bitince
// durmasın diye context.Background verilir. İlk dakikalarda değerler 0'dan
// yükselerek oturur.
func (s *systemSource) load(context.Context) (l1, l5, l15 float64, err error) {
	a, err := load.AvgWithContext(context.Background())
	if a == nil {
		return 0, 0, 0, err
	}
	return a.Load1, a.Load5, a.Load15, nil
}

func (s *systemSource) memory(ctx context.Context) (memStat, error) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		return memStat{}, err
	}
	m := memStat{Total: vm.Total, Available: vm.Available, Free: vm.Available}
	// Bekleme listesi kullanılabilir belleğin parçasıdır (Linux'taki sayfa
	// önbelleği gibi): grafikte kullanılan + önbellek + boş = toplam olur.
	if sb, ok := s.standbyBytes(); ok {
		sb = min(sb, vm.Available)
		m.Cached, m.Free = sb, vm.Available-sb
	}
	if devs, err := mem.SwapDevicesWithContext(ctx); err == nil { // sayfa dosyası yoksa boş liste: swap 0
		for _, d := range devs {
			m.SwapTotal += d.UsedBytes + d.FreeBytes
			m.SwapFree += d.FreeBytes
		}
	} else if sw, err := mem.SwapMemoryWithContext(ctx); err == nil {
		m.SwapTotal, m.SwapFree = sw.Total, sw.Free
	}
	return m, nil
}

// standbyBytes "\Memory\Standby Cache *" sayaçlarının toplamı (bayt). Sayaçlar
// İngilizce adlarıyla eklenir (dil bağımsız). Anlık değerlerdir, tek okuma yeter.
func (s *systemSource) standbyBytes() (uint64, bool) {
	s.once.Do(func() {
		q, err := newPDHQuery(`\Memory\Standby Cache Reserve Bytes`, `\Memory\Standby Cache Normal Priority Bytes`,
			`\Memory\Standby Cache Core Bytes`)
		if err == nil {
			s.standby = q
		}
	})
	if s.standby == nil {
		return 0, false
	}
	vals, err := s.standby.read()
	if err != nil {
		return 0, false
	}
	var sum float64
	for _, v := range vals {
		sum += max(v, 0)
	}
	return uint64(sum), true
}

// mounts yerel sabit birimler: sürücü harfleri ve klasöre bağlanmış birimler.
func (s *systemSource) mounts(ctx context.Context) ([]mount, error) {
	parts, err := disk.PartitionsWithContext(ctx, false) // okunamayan sürücüler uyarı olarak döner
	if len(parts) == 0 && err != nil {
		return nil, err
	}
	var out []mount
	for _, p := range parts {
		if p.Mountpoint == "" {
			continue // hazır olmayan çıkarılabilir sürücü
		}
		path := strings.TrimRight(p.Mountpoint, `\`) + `\`
		v := winVolume{Path: path, FS: p.Fstype, Type: driveType(path)}
		if !keepWinVolume(v) {
			continue
		}
		v.Volume, v.Label = volumeInfo(path)
		out = append(out, winMount(v))
	}
	return out, nil
}

func driveType(root string) uint32 {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return winDriveUnknown
	}
	return windows.GetDriveType(p)
}

// volumeInfo birimin GUID yolu ve etiketi (okunamazsa boş).
func volumeInfo(root string) (guid, label string) {
	p, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return "", ""
	}
	buf := make([]uint16, windows.MAX_PATH+1)
	if windows.GetVolumeNameForVolumeMountPoint(p, &buf[0], uint32(len(buf))) == nil {
		guid = windows.UTF16ToString(buf)
	}
	lb := make([]uint16, windows.MAX_PATH+1)
	if windows.GetVolumeInformation(p, &lb[0], uint32(len(lb)), nil, nil, nil, nil, 0) == nil {
		label = windows.UTF16ToString(lb)
	}
	return guid, label
}

// usage GetDiskFreeSpaceEx: Total toplam boyut, Used toplam - boş.
func (s *systemSource) usage(m mount) (total, used uint64, ok bool) {
	u, err := disk.Usage(m.Point + `\`)
	if err != nil || u.Total == 0 {
		return 0, 0, false
	}
	return u.Total, u.Used, true
}

// diskIO sabit sürücülerin okunan/yazılan baytları (IOCTL_DISK_PERFORMANCE).
func (s *systemSource) diskIO(ctx context.Context) (map[string]ioCounter, error) {
	st, err := disk.IOCountersWithContext(ctx)
	if len(st) == 0 && err != nil {
		return nil, err
	}
	out := make(map[string]ioCounter, len(st))
	for name, c := range st {
		out[strings.ToUpper(strings.TrimRight(name, `\`))] = ioCounter{A: c.ReadBytes, B: c.WriteBytes}
	}
	return out, nil
}

// netIO sanal olmayan bağdaştırıcıların sayaçları. gopsutil arayüzleri kolay
// adlarıyla ("Ethernet") verir; tür ve açıklama GetAdaptersAddresses'ten alınır.
func (s *systemSource) netIO(ctx context.Context) (map[string]ioCounter, error) {
	st, err := net.IOCountersWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	info := adapterInfo()
	out := make(map[string]ioCounter, len(st))
	for _, n := range st {
		a := info[n.Name] // bulunamazsa yalnızca ada bakılır
		if isVirtualWinNet(n.Name, a.desc, a.ifType) {
			continue
		}
		out[n.Name] = ioCounter{A: n.BytesRecv, B: n.BytesSent}
	}
	return out, nil
}

type winAdapter struct {
	desc   string
	ifType uint32
}

// adapterInfo kolay ad → açıklama ve arayüz türü. Okunamazsa boş döner; o
// zaman yalnızca adlara bakılır.
func adapterInfo() map[string]winAdapter {
	size := uint32(16 << 10)
	for range 3 {
		buf := make([]byte, size)
		first := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST|
			windows.GAA_FLAG_SKIP_DNS_SERVER|windows.GAA_FLAG_INCLUDE_ALL_INTERFACES, 0, first, &size)
		if errors.Is(err, windows.ERROR_BUFFER_OVERFLOW) {
			continue
		}
		out := map[string]winAdapter{}
		if err != nil {
			return out
		}
		for a := first; a != nil; a = a.Next {
			out[windows.UTF16PtrToString(a.FriendlyName)] = winAdapter{desc: windows.UTF16PtrToString(a.Description), ifType: a.IfType}
		}
		return out
	}
	return map[string]winAdapter{}
}

func (s *systemSource) temps(context.Context) ([]Temp, error) { return nil, nil }

// PDH (Performance Data Helper) --------------------------------------------------------------

var (
	modPdh                          = windows.NewLazySystemDLL("pdh.dll")
	procPdhOpenQuery                = modPdh.NewProc("PdhOpenQueryW")
	procPdhAddEnglishCounter        = modPdh.NewProc("PdhAddEnglishCounterW")
	procPdhCollectQueryData         = modPdh.NewProc("PdhCollectQueryData")
	procPdhGetFormattedCounterValue = modPdh.NewProc("PdhGetFormattedCounterValue")
)

const pdhFmtDouble = 0x00000200

type pdhFmtCounterValueDouble struct {
	CStatus     uint32
	_           uint32 // hizalama
	DoubleValue float64
}

// pdhQuery açık kalan bir PDH sorgusu; okumalar mu ile sıralanır.
type pdhQuery struct {
	mu       sync.Mutex
	h        windows.Handle
	counters []windows.Handle
}

func newPDHQuery(paths ...string) (*pdhQuery, error) {
	if err := modPdh.Load(); err != nil {
		return nil, err
	}
	q := &pdhQuery{}
	if r, _, _ := procPdhOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&q.h))); r != 0 {
		return nil, windows.Errno(r)
	}
	for _, p := range paths {
		ptr, err := windows.UTF16PtrFromString(p)
		if err != nil {
			return nil, err
		}
		var c windows.Handle
		if r, _, _ := procPdhAddEnglishCounter.Call(uintptr(q.h), uintptr(unsafe.Pointer(ptr)), 0, uintptr(unsafe.Pointer(&c))); r != 0 {
			return nil, windows.Errno(r)
		}
		q.counters = append(q.counters, c)
	}
	return q, nil
}

func (q *pdhQuery) read() ([]float64, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if r, _, _ := procPdhCollectQueryData.Call(uintptr(q.h)); r != 0 {
		return nil, windows.Errno(r)
	}
	out := make([]float64, 0, len(q.counters))
	for _, c := range q.counters {
		var v pdhFmtCounterValueDouble
		if r, _, _ := procPdhGetFormattedCounterValue.Call(uintptr(c), pdhFmtDouble, 0, uintptr(unsafe.Pointer(&v))); r != 0 {
			return nil, windows.Errno(r)
		}
		out = append(out, v.DoubleValue)
	}
	return out, nil
}
