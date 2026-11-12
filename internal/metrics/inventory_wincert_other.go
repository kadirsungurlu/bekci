//go:build !windows

package metrics

import "context"

// windowsCerts yalnız Windows'ta (inventory_wincert_windows.go).
func windowsCerts(context.Context) []InvCert { return nil }
