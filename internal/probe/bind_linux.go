//go:build linux

package probe

import (
	"errors"
	"sync/atomic"
	"syscall"
)

// bindDeviceDenied SO_BINDTODEVICE yetki yüzünden reddedildi (CAP_NET_RAW yok,
// eski çekirdek): bağlantı yalnız kaynak adresle (LocalAddr) kurulur; yönlendirme
// için policy routing gerekir (belge: ip rule from <oob_ip> table oob).
var bindDeviceDenied atomic.Bool

// bindControl soketi SO_BINDTODEVICE ile OOB arayüzüne bağlar.
func bindControl(b oobBind) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, b.Iface)
		})
		if err != nil {
			return err
		}
		if errors.Is(serr, syscall.EPERM) || errors.Is(serr, syscall.EACCES) {
			bindDeviceDenied.Store(true)
			return nil
		}
		return serr
	}
}
