//go:build !windows

package uygulama

import "errors"

// Windows hizmeti yalnızca Windows'ta vardır (service_windows.go).
func runProbeService() (bool, error) { return false, nil }

func runServiceCommand([]string) error {
	return errors.New("uptime service yalnızca Windows'ta kullanılır; Linux'ta panelde verilen Docker veya systemd kurulum komutunu kullanın")
}
