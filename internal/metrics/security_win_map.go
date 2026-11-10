package metrics

import (
	"net/netip"
	"strings"
	"time"
)

// Windows güvenlik verisinin (iş 733, docs/GUVENLIK-EKLENTI-730.md §A.2) saf
// eşleme işlevleri: WMI ve WTS ham sonuçlarını tel tiplerine çevirir. Her
// platformda derlenir ve testlenir; sorguların kendisi security_windows.go'da.
// Ham yapıların alan adları WMI özellik adlarıyla aynıdır (wmi paketi adla doldurur).

// wmiMpStatus root\Microsoft\Windows\Defender MSFT_MpComputerStatus (gereken alanlar).
// Eski sürümlerde olmayan alanlar (IsTamperProtected) sıfır kalır.
type wmiMpStatus struct {
	AMServiceEnabled              bool
	AntivirusEnabled              bool
	RealTimeProtectionEnabled     bool
	IsTamperProtected             bool
	AntivirusSignatureLastUpdated time.Time
	AntivirusSignatureVersion     string
	QuickScanAge                  int64 // gün; 4294967295 (ya da -1 olarak gelen uint32) = hiç
	FullScanAge                   int64
}

// wmiMpDetection MSFT_MpThreatDetection (bir algılama kaydı).
type wmiMpDetection struct {
	InitialDetectionTime time.Time
}

// wmiMpThreat MSFT_MpThreat (bilinen tehdit; IsActive hâlâ etkin).
type wmiMpThreat struct {
	IsActive bool
}

// wmiFwProfile root\StandardCimv2 MSFT_NetFirewallProfile. Enabled GpoBoolean
// (0 kapalı, 1 açık, 2 ayarlanmamış = açık); DefaultInboundAction
// (0 ayarlanmamış = engelle, 2 izin, 4 engelle).
type wmiFwProfile struct {
	Name                 string
	Enabled              int64
	DefaultInboundAction int64
}

// wmiConnProfile root\StandardCimv2 MSFT_NetConnectionProfile. NetworkCategory
// 0 ortak, 1 özel, 2 etki alanı.
type wmiConnProfile struct {
	NetworkCategory int64
}

// WMI hata kodları: ad alanı ya da sınıf yok (Defender kaldırılmış / üçüncü taraf AV).
const (
	wbemInvalidNamespace = 0x8004100E
	wbemInvalidClass     = 0x80041010
	wbemNotFound         = 0x80041002
)

// wmiMissingCode WMI hata kodu "ad alanı/sınıf yok" mu.
func wmiMissingCode(code uint32) bool {
	switch code {
	case wbemInvalidNamespace, wbemInvalidClass, wbemNotFound:
		return true
	}
	return false
}

// scanAgeDays WMI gün değerini SecAV yaşına çevirir (-1 hiç / geçersiz).
func scanAgeDays(v int64) int {
	if v < 0 || v >= 0xFFFFFFFF || v > 36500 {
		return -1
	}
	return int(v)
}

// mapDefender Defender durumunu SecAV'a çevirir; tehdit sayısı son 30 gün.
func mapDefender(st wmiMpStatus, det []wmiMpDetection, threats []wmiMpThreat, now time.Time) SecAV {
	a := SecAV{
		Product:    AVDefender,
		Service:    st.AMServiceEnabled,
		Enabled:    st.AntivirusEnabled,
		Realtime:   st.RealTimeProtectionEnabled,
		Tamper:     st.IsTamperProtected,
		SigVersion: strings.TrimSpace(st.AntivirusSignatureVersion),
		QuickAgeD:  scanAgeDays(st.QuickScanAge),
		FullAgeD:   scanAgeDays(st.FullScanAge),
	}
	// 1601/1970 gibi boş tarihler "bilinmiyor"; ileri tarih saat kaymasıdır, yine de kabul.
	if t := st.AntivirusSignatureLastUpdated; !t.IsZero() && t.Year() >= 2000 {
		a.SigUpdated = t.Unix()
	}
	from := now.Add(-30 * 24 * time.Hour)
	for _, d := range det {
		if !d.InitialDetectionTime.IsZero() && !d.InitialDetectionTime.Before(from) {
			a.Threats30d++
		}
	}
	for _, t := range threats {
		if t.IsActive {
			a.ActiveThreats++
		}
	}
	return a
}

// fwProfileName WMI profil adını küçük harfli sabite çevirir ("" tanınmıyor).
func fwProfileName(n string) string {
	switch strings.ToLower(strings.TrimSpace(n)) {
	case "domain", "domainauthenticated":
		return FwProfileDomain
	case "private":
		return FwProfilePrivate
	case "public":
		return FwProfilePublic
	}
	return ""
}

// mapFirewall profilleri SecFirewall'a çevirir. conns nil: bağlı profil
// bilinmiyor, Active = tüm profiller açık. Bağlı profil varsa Active = bağlı
// profillerin hepsi açık. Kural sayısı Windows'ta sayılmaz (0).
func mapFirewall(profiles []wmiFwProfile, conns []wmiConnProfile) *SecFirewall {
	connected := map[string]bool{}
	for _, c := range conns {
		switch c.NetworkCategory {
		case 0:
			connected[FwProfilePublic] = true
		case 1:
			connected[FwProfilePrivate] = true
		case 2:
			connected[FwProfileDomain] = true
		}
	}
	fw := &SecFirewall{Backend: FirewallWindows, Profiles: []SecFwProfile{}}
	allOn, connOn, anyConn := len(profiles) > 0, true, false
	for _, p := range profiles {
		name := fwProfileName(p.Name)
		if name == "" {
			continue
		}
		x := SecFwProfile{Name: name, Enabled: p.Enabled != 0, Connected: connected[name]}
		switch p.DefaultInboundAction {
		case 2:
			x.Inbound = "allow"
		default: // 0 ayarlanmamış (Windows varsayılanı engelle), 4 engelle
			x.Inbound = "block"
		}
		// Açık ama gelen varsayılanı "izin" olan profil süzmüyor sayılır.
		on := x.Enabled && x.Inbound == "block"
		allOn = allOn && on
		if x.Connected {
			anyConn = true
			connOn = connOn && on
		}
		fw.Profiles = append(fw.Profiles, x)
	}
	if anyConn {
		fw.Active = connOn
	} else {
		fw.Active = allOn && len(fw.Profiles) > 0
	}
	return fw
}

// wtsRaw WTS oturum bilgisi (WTSEnumerateSessionsW + WTSQuerySessionInformationW).
type wtsRaw struct {
	ID         uint32
	Station    string // WinStationName: Console, RDP-Tcp#3, ""
	State      uint32 // WTS_CONNECTSTATE_CLASS
	User       string
	Domain     string
	ClientName string
	ClientAddr string // ayrıştırılmış IP ("" yok)
	LogonTime  int64  // FILETIME (100 ns, 1601'den); 0 bilinmiyor
}

// WTS_CONNECTSTATE_CLASS adları (yalnız gösterilenler).
var wtsStateNames = map[uint32]string{0: "Active", 1: "Connected", 4: "Disconnected", 5: "Idle"}

// filetimeUnix FILETIME'ı unix saniyeye çevirir (0 ya da 1970 öncesi -> 0).
func filetimeUnix(ft int64) int64 {
	const epochDiff = 116444736000000000 // 1601-01-01 ile 1970-01-01 arası (100 ns)
	if ft <= epochDiff {
		return 0
	}
	return (ft - epochDiff) / 10000000
}

// mapWTSSession kullanıcısı olan oturumu SecSession'a çevirir; hizmet (0),
// dinleyici ve kullanıcısız oturumlar atlanır.
func mapWTSSession(r wtsRaw) (SecSession, bool) {
	user := strings.TrimSpace(r.User)
	if user == "" || r.ID == 0 {
		return SecSession{}, false
	}
	state, ok := wtsStateNames[r.State]
	if !ok {
		return SecSession{}, false
	}
	if d := strings.TrimSpace(r.Domain); d != "" {
		user = d + `\` + user
	}
	s := SecSession{User: user, TTY: strings.TrimSpace(r.Station), State: state, Since: filetimeUnix(r.LogonTime), Kind: SessionRDP}
	if strings.EqualFold(s.TTY, "console") {
		s.Kind = SessionTTY
	}
	if s.Kind == SessionRDP {
		s.From = r.ClientAddr
		if s.From == "" {
			s.From = strings.TrimSpace(r.ClientName)
		}
	}
	return s, true
}

// wtsClientAddr WTS_CLIENT_ADDRESS'i çözer: family 2 (AF_INET) adres
// baytları Address[2:6], 23 (AF_INET6) Address[2:18]. Geçersizse "".
func wtsClientAddr(family uint32, b [20]byte) string {
	var a netip.Addr
	switch family {
	case 2:
		a = netip.AddrFrom4([4]byte(b[2:6]))
	case 23:
		a = netip.AddrFrom16([16]byte(b[2:18]))
	default:
		return ""
	}
	if a.IsUnspecified() {
		return ""
	}
	return a.Unmap().String()
}
