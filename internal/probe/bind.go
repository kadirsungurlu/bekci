package probe

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// OOB arayüz bulma ve bağlama (462). Panel OOB IP'sini iş listesi/kalp atışı
// yanıtıyla verir; ajan bu IP'nin bağlı olduğu yerel arayüzü net.Interfaces()
// içinde arar. Bulursa Main bağlantıları (OOB hattındayken) o arayüze bağlanır:
// LocalAddr = OOB IP'si; Linux'ta ayrıca SO_BINDTODEVICE, Windows'ta
// IP_UNICAST_IF (bind_linux.go, bind_windows.go). IP yerelde yoksa (NAT arkasında
// genel IP) PROBE_OOB_IFACE ile arayüz adı verilebilir; verilmemişse panele
// "arayüz bulunamadı" bildirilir ve OOB'ye geçilmez.

// ErrOOBIfaceMissing OOB IP'si hiçbir yerel arayüzde yok (NAT arkasında olabilir).
var ErrOOBIfaceMissing = errors.New("OOB IP'si yerel arayüzlerde bulunamadı; NAT arkasındaysa PROBE_OOB_IFACE ile arayüz adını verin")

// oobBind çözülmüş OOB bağlama bilgisi.
type oobBind struct {
	Iface string     // arayüz adı (ör. eth1)
	Index int        // arayüz sırası (Windows IP_UNICAST_IF)
	Local netip.Addr // kaynak adres (LocalAddr)
}

// ifaceAddrs bir arayüz ve adresleri (testlerde sahtesi verilir).
type ifaceAddrs struct {
	Name  string
	Index int
	Addrs []netip.Addr
}

// listIfaces yerel arayüzler ve adresleri.
var listIfaces = func() ([]ifaceAddrs, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]ifaceAddrs, 0, len(ifs))
	for _, it := range ifs {
		if it.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := it.Addrs()
		if err != nil {
			continue
		}
		ia := ifaceAddrs{Name: it.Name, Index: it.Index}
		for _, a := range addrs {
			if pfx, err := netip.ParsePrefix(a.String()); err == nil {
				ia.Addrs = append(ia.Addrs, pfx.Addr().Unmap())
			}
		}
		out = append(out, ia)
	}
	return out, nil
}

// resolveOOB OOB IP'sinin arayüzünü bulur. override (PROBE_OOB_IFACE) boş
// değilse o arayüz kullanılır; kaynak adres, IP o arayüzdeyse OOB IP'si,
// değilse arayüzün aynı aileden ilk genel adresidir.
func resolveOOB(oobIP, override string) (oobBind, error) {
	ip, err := netip.ParseAddr(strings.TrimSpace(oobIP))
	if err != nil {
		return oobBind{}, fmt.Errorf("geçersiz OOB IP'si %q", oobIP)
	}
	ip = ip.Unmap()
	ifs, err := listIfaces()
	if err != nil {
		return oobBind{}, fmt.Errorf("arayüzler okunamadı: %w", err)
	}
	override = strings.TrimSpace(override)
	for _, it := range ifs {
		if override != "" && it.Name != override {
			continue
		}
		for _, a := range it.Addrs {
			if a == ip {
				return oobBind{Iface: it.Name, Index: it.Index, Local: ip}, nil
			}
		}
		if override == "" {
			continue
		}
		// Elle verilen arayüz, IP orada değil (NAT): aynı aileden bir adres.
		for _, a := range it.Addrs {
			if a.Is4() == ip.Is4() && !a.IsLinkLocalUnicast() && !a.IsLoopback() {
				return oobBind{Iface: it.Name, Index: it.Index, Local: a}, nil
			}
		}
		return oobBind{}, fmt.Errorf("PROBE_OOB_IFACE=%s arayüzünde %s ailesinden adres yok", override, map[bool]string{true: "IPv4", false: "IPv6"}[ip.Is4()])
	}
	if override != "" {
		return oobBind{}, fmt.Errorf("PROBE_OOB_IFACE=%s arayüzü bulunamadı (ya da kapalı)", override)
	}
	return oobBind{}, ErrOOBIfaceMissing
}

// dialer base'in kopyası: kaynak adres OOB adresi, soket arayüze bağlanır.
func (b oobBind) dialer(base *net.Dialer) *net.Dialer {
	d := *base
	d.LocalAddr = &net.TCPAddr{IP: b.Local.AsSlice()}
	d.Control = bindControl(b)
	return &d
}
