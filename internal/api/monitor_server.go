package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Monitörün çalıştığı sunucu: kullanıcı seçer (monitors.server_id). Öneri ucu
// hedefi sunucuların bilinen adresleri ve adlarıyla karşılaştırır; kaydetmez.

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/monitors/server-suggestion", s.editor(s.monitorServerSuggestion))
	})
}

// validateMonitorServer: 0 bağı kaldırır; başka değer var olan bir sunucu olmalı.
func (s *Server) validateMonitorServer(w http.ResponseWriter, r *http.Request, id int64) bool {
	if id == 0 {
		return true
	}
	p, err := s.store.GetProbe(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && p.Kind != store.ProbeKindServer) {
		writeError(w, http.StatusBadRequest, "Seçilen sunucu bulunamadı")
		return false
	}
	if err != nil {
		s.dbError(w, err)
		return false
	}
	return true
}

// serverMatch öneri. Match: ip | hostname | name | dns (alan adı sunucunun IP'sine çözüldü).
type serverMatch struct {
	ServerID int64  `json:"server_id"`
	Name     string `json:"name"`
	Match    string `json:"match"`
	Value    string `json:"value"` // eşleşen adres ya da ad
}

// serverKeys bir sunucunun eşleştirmede kullanılan bilinen adları.
type serverKeys struct {
	ID       int64
	Name     string
	Hostname string
	IPs      []netip.Addr
}

func serverKeysOf(p store.Probe) serverKeys {
	k := serverKeys{ID: p.ID, Name: p.Name}
	if h := p.HostInfo; h != "" {
		var host metrics.Host
		if json.Unmarshal([]byte(h), &host) == nil {
			k.Hostname = host.Hostname
		}
	}
	// LockedIP virgüllü liste olabilir; OOB ikinci hattın adresi.
	for _, raw := range append(strings.Split(p.LockedIP, ","), p.LastIP, p.OOBIP) {
		if a, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil && usableIP(a) {
			k.IPs = append(k.IPs, a.Unmap())
		}
	}
	return k
}

// usableIP: yerel adres eşleşmesi bilgi taşımaz (her makinede aynı).
func usableIP(a netip.Addr) bool { return !a.IsLoopback() && !a.IsUnspecified() }

// targetHost hedef metninden ana makine ya da IP çıkarır: adres, host:port,
// [v6]:port ya da yalın ad. Bulunamazsa boş döner.
func targetHost(target string) string {
	t := strings.TrimSpace(target)
	if t == "" || strings.ContainsAny(t, " \t") {
		return ""
	}
	if strings.Contains(t, "://") {
		u, err := url.Parse(t)
		if err != nil {
			return ""
		}
		return normHost(u.Hostname())
	}
	if h, _, err := net.SplitHostPort(t); err == nil {
		return normHost(h)
	}
	t, _, _ = strings.Cut(t, "/")
	return normHost(strings.Trim(t, "[]"))
}

func normHost(h string) string { return strings.ToLower(strings.TrimSuffix(h, ".")) }

// matchServers hedef ana makinesini sunucularla eşleştirir, güçlüden zayıfa:
// IP, ana makine adı, sunucu adı, alan adının çözüldüğü IP. Eşitlik aranır:
// "www.a.com" adı "www" makinesiyle eşleşmez. resolved yalnız alan adı için dolu.
func matchServers(host string, resolved []netip.Addr, servers []serverKeys) []serverMatch {
	if host == "" {
		return nil
	}
	ip, ipErr := netip.ParseAddr(host)
	var ipOut, hostOut, nameOut, dnsOut []serverMatch
	for _, k := range servers {
		switch {
		case ipErr == nil && containsAddr(k.IPs, ip.Unmap()) && usableIP(ip):
			ipOut = append(ipOut, serverMatch{k.ID, k.Name, "ip", host})
		case k.Hostname != "" && normHost(k.Hostname) == host:
			hostOut = append(hostOut, serverMatch{k.ID, k.Name, "hostname", k.Hostname})
		case normHost(k.Name) == host:
			nameOut = append(nameOut, serverMatch{k.ID, k.Name, "name", k.Name})
		default:
			for _, a := range resolved {
				if containsAddr(k.IPs, a.Unmap()) && usableIP(a) {
					dnsOut = append(dnsOut, serverMatch{k.ID, k.Name, "dns", a.String()})
					break
				}
			}
		}
	}
	out := append(append(append(ipOut, hostOut...), nameOut...), dnsOut...)
	return out
}

func containsAddr(list []netip.Addr, a netip.Addr) bool {
	for _, x := range list {
		if x == a {
			return true
		}
	}
	return false
}

// POST /api/monitors/server-suggestion {type, config}: henüz kaydedilmemiş hedef
// için aday sunucular (ilki en güçlü eşleşme). Alan adı en çok 2 sn çözülür.
func (s *Server) monitorServerSuggestion(w http.ResponseWriter, r *http.Request) {
	var in monitorInput
	if !readJSON(w, r, &in) {
		return
	}
	host := targetHost(engine.Target(store.Monitor{Type: in.Type, Config: in.Config}))
	out := []serverMatch{}
	if host != "" {
		servers, err := s.store.ListProbesOfKind(r.Context(), store.ProbeKindServer)
		if err != nil {
			s.dbError(w, err)
			return
		}
		keys := make([]serverKeys, 0, len(servers))
		for _, p := range servers {
			keys = append(keys, serverKeysOf(p))
		}
		var resolved []netip.Addr
		if _, err := netip.ParseAddr(host); err != nil && strings.Contains(host, ".") {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			resolved, _ = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		}
		if m := matchServers(host, resolved, keys); m != nil {
			out = m
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"host": host, "matches": out})
}

// probeLabelMax üst bilgi alanlarının en çok karakter sayısı; kod kısa kalır.
const (
	probeLabelMax = 64
	probeCodeMax  = 12
)

// mergeProbeLabels verilen alanları eskinin üstüne yazar; nil alan değişmez.
func mergeProbeLabels(old store.ProbeLabels, role, region, provider, code *string) (store.ProbeLabels, error) {
	set := func(dst *string, in *string, limit int) error {
		if in == nil {
			return nil
		}
		v := strings.TrimSpace(*in)
		if len([]rune(v)) > limit {
			return errors.New("Üst bilgi alanı çok uzun")
		}
		*dst = v
		return nil
	}
	return old, errors.Join(set(&old.Role, role, probeLabelMax), set(&old.Region, region, probeLabelMax),
		set(&old.Provider, provider, probeLabelMax), set(&old.Code, code, probeCodeMax))
}
