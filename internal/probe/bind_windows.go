//go:build windows

package probe

import (
	"encoding/binary"
	"strings"
	"syscall"
)

// Windows: IP_UNICAST_IF / IPV6_UNICAST_IF (ws2ipdef.h, değer 31). IPv4'te
// arayüz sırası ağ bayt sırasıyla, IPv6'da makine sırasıyla verilir.
const (
	ipUnicastIf   = 31
	ipv6UnicastIf = 31
)

func bindControl(b oobBind) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			h := syscall.Handle(fd)
			if strings.HasSuffix(network, "6") || !b.Local.Is4() {
				serr = syscall.SetsockoptInt(h, syscall.IPPROTO_IPV6, ipv6UnicastIf, b.Index)
				return
			}
			var be [4]byte
			binary.BigEndian.PutUint32(be[:], uint32(b.Index))
			serr = syscall.SetsockoptInt(h, syscall.IPPROTO_IP, ipUnicastIf, int(binary.NativeEndian.Uint32(be[:])))
		})
		if err != nil {
			return err
		}
		return serr
	}
}
