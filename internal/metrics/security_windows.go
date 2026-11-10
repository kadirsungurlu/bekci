//go:build windows

package metrics

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/yusufpapurcu/wmi"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Windows'ta bekleyen güncellemeler Windows Update ile (iş 734) gelecek; şimdilik
// yalnız yeniden başlatma gereksinimi kayıt defterinden okunur.
func osSecUpdates(context.Context, secEnv) (*SecUpdates, string) {
	return nil, NoteUnsupported
}

// rebootKeys varlığı yeniden başlatma bekliyor demek olan anahtarlar.
var rebootKeys = []string{
	`SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`,
	`SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`,
}

func osSecReboot(context.Context, secEnv) (*SecReboot, string) {
	for _, k := range rebootKeys {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, k, registry.QUERY_VALUE)
		if err == nil {
			key.Close()
			return &SecReboot{Required: true, Reason: RebootRegistry}, ""
		}
	}
	return &SecReboot{}, ""
}

// secExtrasSupported SSH girişleri (732) yalnız Linux'ta; Windows'ta oturum,
// güvenlik duvarı ve Defender osSecExtras ile (733), 4625/4740 734'te.
const secExtrasSupported = false

const (
	nsDefender = `root\Microsoft\Windows\Defender`
	nsStdCimv2 = `root\StandardCimv2`
)

// osSecExtras Windows: RDP/konsol oturumları (WTS), güvenlik duvarı profilleri
// ve Defender (WMI). PowerShell çağrılmaz.
func osSecExtras(ctx context.Context, _ secEnv, s *Security) bool {
	s.Notes[SecSectionAuth] = NoteUnsupported // 4625/4740: iş 734
	var code string
	if s.Sessions, code = winSessions(); code != "" {
		s.Notes[SecSectionSessions] = code
	}
	if s.Firewall, code = winFirewall(ctx); code != "" {
		s.Notes[SecSectionFirewall] = code
	}
	if s.AV, code = winDefender(ctx); code != "" {
		s.Notes[SecSectionAV] = code
	}
	return true
}

// wmiBusy askıda kalan WMI sorgusu varken yenisi başlatılmaz (zaman aşımında
// iş parçacığı geri alınamaz; birikmesin).
var wmiBusy atomic.Int32

var errWMIBusy = errors.New("wmi: önceki sorgu sürüyor")

// wmiQuery sorguyu süre sınırıyla çalıştırır; sonuç yalnız tamamlanınca döner.
func wmiQuery[T any](ctx context.Context, query, ns string) ([]T, error) {
	if !wmiBusy.CompareAndSwap(0, 1) {
		return nil, errWMIBusy
	}
	type res struct {
		v   []T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		defer wmiBusy.Store(0)
		var dst []T
		c := &wmi.Client{AllowMissingFields: true}
		err := c.Query(query, &dst, nil, ns)
		ch <- res{dst, err}
	}()
	ctx, cancel := context.WithTimeout(ctx, secCmdTimeout)
	defer cancel()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// wmiMissing hata "ad alanı/sınıf yok" mu (doğrudan HRESULT ya da istisna scode).
func wmiMissing(err error) bool {
	var oe *ole.OleError
	if !errors.As(err, &oe) {
		return false
	}
	if wmiMissingCode(uint32(oe.Code())) {
		return true
	}
	if ex, ok := oe.SubError().(ole.EXCEPINFO); ok {
		return wmiMissingCode(ex.SCODE())
	}
	return false
}

func winDefender(ctx context.Context) (*SecAV, string) {
	st, err := wmiQuery[wmiMpStatus](ctx, "SELECT * FROM MSFT_MpComputerStatus", nsDefender)
	if err != nil {
		if wmiMissing(err) {
			return nil, NoteNoDefender
		}
		return nil, NoteFailed
	}
	if len(st) == 0 {
		return nil, NoteNoDefender
	}
	// Tehdit sorguları isteğe bağlı: başarısızsa sayı 0 kalır.
	det, _ := wmiQuery[wmiMpDetection](ctx, "SELECT InitialDetectionTime FROM MSFT_MpThreatDetection", nsDefender)
	thr, _ := wmiQuery[wmiMpThreat](ctx, "SELECT * FROM MSFT_MpThreat", nsDefender)
	a := mapDefender(st[0], det, thr, time.Now())
	return &a, ""
}

func winFirewall(ctx context.Context) (*SecFirewall, string) {
	prof, err := wmiQuery[wmiFwProfile](ctx, "SELECT Name, Enabled, DefaultInboundAction FROM MSFT_NetFirewallProfile", nsStdCimv2)
	if err != nil || len(prof) == 0 {
		return nil, NoteFailed
	}
	conns, err := wmiQuery[wmiConnProfile](ctx, "SELECT NetworkCategory FROM MSFT_NetConnectionProfile", nsStdCimv2)
	if err != nil {
		conns = nil
	}
	return mapFirewall(prof, conns), ""
}

var (
	modWtsapi32                     = windows.NewLazySystemDLL("wtsapi32.dll")
	procWTSQuerySessionInformationW = modWtsapi32.NewProc("WTSQuerySessionInformationW")
)

// WTS_INFO_CLASS değerleri.
const (
	wtsUserName      = 5
	wtsDomainName    = 7
	wtsClientName    = 10
	wtsClientAddress = 14
	wtsSessionInfo   = 24
)

// wtsInfoW WTSINFOW; LARGE_INTEGER alanları 8 bayta hizalı olduğundan 32 ve
// 64 bitte aynı yerleşim için açık dolgu ve [2]uint32 kullanılır.
type wtsInfoW struct {
	State, SessionID                       uint32
	_                                      [6]uint32 // bayt/çerçeve sayaçları
	WinStationName                         [32]uint16
	Domain                                 [17]uint16
	UserName                               [21]uint16
	_                                      uint32
	ConnectTime, DisconnectTime, LastInput [2]uint32
	LogonTime, CurrentTime                 [2]uint32
}

// wtsClientAddress WTS_CLIENT_ADDRESS.
type wtsClientAddressW struct {
	AddressFamily uint32
	Address       [20]byte
}

// wtsQuery oturum bilgisini okur; buf WTSFreeMemory ile serbest bırakılır.
func wtsQuery(id uint32, class uint32, f func(p unsafe.Pointer, n uint32)) bool {
	if procWTSQuerySessionInformationW.Find() != nil {
		return false
	}
	var buf *byte
	var n uint32
	r, _, _ := procWTSQuerySessionInformationW.Call(0, uintptr(id), uintptr(class), uintptr(unsafe.Pointer(&buf)), uintptr(unsafe.Pointer(&n)))
	if r == 0 || buf == nil {
		return false
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(buf)))
	f(unsafe.Pointer(buf), n)
	return true
}

func wtsString(id, class uint32) string {
	var s string
	wtsQuery(id, class, func(p unsafe.Pointer, _ uint32) { s = windows.UTF16PtrToString((*uint16)(p)) })
	return s
}

// winSessions WTSEnumerateSessionsW ile kullanıcısı olan oturumlar.
func winSessions() ([]SecSession, string) {
	var list *windows.WTS_SESSION_INFO
	var count uint32
	if err := windows.WTSEnumerateSessions(0, 0, 1, &list, &count); err != nil {
		return nil, NoteFailed
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(list)))
	out := []SecSession{}
	for _, si := range unsafe.Slice(list, count) {
		if len(out) >= MaxSecSessions {
			break
		}
		r := wtsRaw{ID: si.SessionID, State: si.State}
		if si.WindowStationName != nil {
			r.Station = windows.UTF16PtrToString(si.WindowStationName)
		}
		r.User = wtsString(r.ID, wtsUserName)
		if r.User == "" {
			continue
		}
		r.Domain = wtsString(r.ID, wtsDomainName)
		r.ClientName = wtsString(r.ID, wtsClientName)
		wtsQuery(r.ID, wtsClientAddress, func(p unsafe.Pointer, n uint32) {
			if n >= uint32(unsafe.Sizeof(wtsClientAddressW{})) {
				a := (*wtsClientAddressW)(p)
				r.ClientAddr = wtsClientAddr(a.AddressFamily, a.Address)
			}
		})
		wtsQuery(r.ID, wtsSessionInfo, func(p unsafe.Pointer, n uint32) {
			if n >= uint32(unsafe.Sizeof(wtsInfoW{})) {
				w := (*wtsInfoW)(p)
				r.LogonTime = int64(uint64(w.LogonTime[1])<<32 | uint64(w.LogonTime[0]))
			}
		})
		if s, ok := mapWTSSession(r); ok {
			out = append(out, s)
		}
	}
	return out, ""
}

func fileInode(os.FileInfo) uint64 { return 0 }
