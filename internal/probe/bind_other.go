//go:build !linux && !windows

package probe

import "syscall"

// Diğer sistemlerde arayüze bağlama yok: yalnız kaynak adres (LocalAddr).
func bindControl(oobBind) func(network, address string, c syscall.RawConn) error { return nil }
