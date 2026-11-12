package denetim

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// Loopback ana bilgisayar yerel mi? DNS çözülmez: yalnız "localhost" adı ve
// loopback IP'leri (127.0.0.0/8, ::1) kabul edilir.
func Loopback(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if h == "localhost" {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.Unmap().IsLoopback()
}

// hedefHost target ("host:port") ya da url değerinden ana bilgisayarı çıkarır.
func hedefHost(typ, s string) (string, error) {
	if typ == PURL {
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
			return "", hata(HataParams, "URL http(s)://ana-bilgisayar[:port]/yol biçiminde olmalı")
		}
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
				return "", hata(HataParams, "URL bağlantı noktası geçersiz")
			}
		}
		return u.Hostname(), nil
	}
	host, port, err := net.SplitHostPort(s)
	if err != nil || host == "" {
		return "", hata(HataParams, "hedef ana-bilgisayar:port biçiminde olmalı")
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		return "", hata(HataParams, "hedef bağlantı noktası geçersiz")
	}
	return host, nil
}

// soketDogrula yerel soket yolu: mutlak Unix yolu ya da Windows adlı boru.
func soketDogrula(s string) error {
	switch {
	case strings.Contains(s, ".."):
		return fmt.Errorf("yol '..' içeremez")
	case strings.HasPrefix(s, "/"):
		if len(s) > 107 {
			return fmt.Errorf("soket yolu çok uzun")
		}
		return nil
	case strings.HasPrefix(strings.ToLower(s), `\\.\pipe\`) && len(s) > len(`\\.\pipe\`):
		return nil
	}
	return fmt.Errorf("yalnız yerel soket (mutlak yol ya da \\\\.\\pipe\\ad)")
}
