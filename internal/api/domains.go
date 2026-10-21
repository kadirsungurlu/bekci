package api

// Durum sayfası özel alan adlarının otomatik HTTPS'i.
//
// Ters vekil (Caddy) "on-demand TLS" ile çalışır: tanımadığı bir alan adına ilk
// HTTPS isteği geldiğinde sertifika almadan önce GET /api/tls/ask?domain=<ad>
// ucuna sorar; yalnız 200 dönerse sertifika alır. Uç, adı yalnızca YAYINDAKİ bir
// durum sayfasına tanımlıysa (ya da uygulamanın kendi adresiyse) onaylar; böylece
// rastgele adlar için sertifika alınmaz ve yeni alan adı için vekile dokunulmaz.
//
// Panel ayrıca alan adının durumunu denetler: DNS bu sunucuyu gösteriyor mu,
// alan HTTPS ile açılıp bu sayfayı sunuyor mu.

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/idna"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/tls/ask", s.tlsAsk)
		mux.Handle("GET /api/status-pages/{id}/domain", s.admin(s.pageDomainStatus))
	})
}

// tlsAskRL ask ucunun IP başına sınırı. Vekil tek IP'den sorar ve yalnız
// sertifikası olmayan adlar için (önbelleğe alınana kadar) sorar; sınır,
// uç doğrudan çağrılarak alan adı taranmasını yavaşlatmak içindir.
var tlsAskRL = newIPLimiterWindow(600, time.Minute)

// normalizeDomain alan adını karşılaştırılabilir biçime getirir: boşluk ve
// sondaki nokta atılır, küçük harf, Unicode ad punycode'a (xn--) çevrilir.
// Geçerli bir ana makine adı değilse ok=false.
func normalizeDomain(raw string) (string, bool) {
	d := strings.TrimSuffix(strings.TrimSpace(raw), ".")
	if d == "" {
		return "", false
	}
	a, err := idna.Lookup.ToASCII(d)
	if err != nil {
		return "", false
	}
	a = strings.ToLower(a)
	return a, validHostname(a)
}

func (s *Server) tlsAsk(w http.ResponseWriter, r *http.Request) {
	if ok, wait := tlsAskRL.allow(clientIP(r), s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeError(w, http.StatusTooManyRequests, "Çok fazla istek")
		return
	}
	d, ok := normalizeDomain(r.URL.Query().Get("domain"))
	if ok && d == s.baseHost() {
		w.WriteHeader(http.StatusOK)
		return
	}
	if ok {
		if p, found := s.domainPage(r.Context(), d); found && p.published {
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	http.Error(w, "", http.StatusNotFound)
}

// domainPage alan adı eşlemesinden sayfayı bulur (gerekirse eşlemeyi yükler).
func (s *Server) domainPage(ctx context.Context, d string) (domainPage, bool) {
	s.pages.mu.RLock()
	loaded := s.pages.domains != nil
	s.pages.mu.RUnlock()
	if !loaded {
		if err := s.reloadDomains(ctx); err != nil {
			s.log.Error("durum sayfası alan adları yüklenemedi", "hata", err)
			return domainPage{}, false
		}
	}
	s.pages.mu.RLock()
	defer s.pages.mu.RUnlock()
	p, ok := s.pages.domains[d]
	return p, ok
}

// Alan adı durumu ----------------------------------------------------------------------

// Testte değiştirilebilsin diye paket değişkenleri.
var (
	lookupHost  = net.DefaultResolver.LookupHost
	domainProbe = probeDomainHTTPS
)

// probeDomainHTTPS https://<alan>/api/public/resolve ister (geçerli sertifika
// zorunlu, yönlendirme izlenmez) ve alanın sunduğu sayfanın kısa adını döner.
func probeDomainHTTPS(ctx context.Context, domain string) (string, error) {
	c := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			Proxy:               nil,
			TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
			TLSHandshakeTimeout: 6 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer c.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/api/public/resolve", nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", httpStatusError(resp.StatusCode)
	}
	var out struct {
		Slug *string `json:"slug"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&out); err != nil {
		return "", errNotBekci
	}
	if out.Slug == nil {
		return "", errUnbound
	}
	return *out.Slug, nil
}

var (
	errNotBekci = errors.New("yanıt Bekci'den gelmiyor")
	errUnbound  = errors.New("alan adı bu kurulumda bir sayfaya bağlı değil")
)

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

// httpsErrCode HTTPS denetimi hatasını arayüzde çevrilecek türe indirger.
func httpsErrCode(err error) string {
	var dnsErr *net.DNSError
	var hs httpStatusError
	var tlsRec tls.RecordHeaderError
	var certErr *tls.CertificateVerificationError
	var opErr *net.OpError
	switch {
	case errors.Is(err, errNotBekci):
		return "not_bekci"
	case errors.Is(err, errUnbound):
		return "unbound"
	case errors.As(err, &hs):
		return "http"
	case errors.As(err, &dnsErr):
		return "no_host"
	case errors.As(err, &certErr), errors.As(err, &tlsRec), strings.Contains(err.Error(), "tls:"), strings.Contains(err.Error(), "x509:"):
		return "tls"
	case errors.Is(err, context.DeadlineExceeded), os.IsTimeout(err):
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.As(err, &opErr) && opErr.Timeout():
		return "timeout"
	}
	return "other"
}

type domainStatus struct {
	Domain     string   `json:"domain"`
	Target     string   `json:"target"`      // CNAME hedefi (uygulamanın adresi)
	TargetIPs  []string `json:"target_ips"`  // A/AAAA hedefleri
	DomainIPs  []string `json:"domain_ips"`  // alan adının çözüldüğü adresler
	DNS        bool     `json:"dns"`         // alan adı bu sunucuyu gösteriyor
	HTTPS      bool     `json:"https"`       // HTTPS ile açılıyor ve bu sayfayı sunuyor
	HTTPSError string   `json:"https_error"` // HTTPS denetiminin ham hatası (ayrıntı)
	// HTTPSCode hatanın türü (arayüzde çevrilir): no_host | refused | timeout |
	// tls | http | not_bekci | unbound | other_page | other.
	HTTPSCode string `json:"https_code"`
	Published bool   `json:"published"`
	// State: none (alan adı yok) | unpublished | dns (DNS bekleniyor) |
	// cert (sertifika bekleniyor) | active (etkin).
	State string `json:"state"`
}

func (s *Server) pageDomainStatus(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	st := domainStatus{Domain: p.CustomDomain, Published: p.Published, State: "none", TargetIPs: []string{}, DomainIPs: []string{}}
	if p.CustomDomain == "" {
		writeJSON(w, http.StatusOK, st)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	st.Target = s.baseHost()
	if st.Target == "" {
		st.Target = requestHost(r)
	}
	if ips, err := lookupHost(ctx, st.Target); err == nil {
		st.TargetIPs = ips
	}
	if ips, err := lookupHost(ctx, p.CustomDomain); err == nil {
		st.DomainIPs = ips
	}
	for _, ip := range st.DomainIPs {
		if slices.Contains(st.TargetIPs, ip) {
			st.DNS = true
			break
		}
	}
	slug, err := domainProbe(ctx, p.CustomDomain)
	switch {
	case err != nil:
		st.HTTPSError, st.HTTPSCode = err.Error(), httpsErrCode(err)
	case slug != p.Slug:
		st.HTTPSError, st.HTTPSCode = "alan adı başka bir sayfayı sunuyor", "other_page"
	default:
		st.HTTPS = true
	}
	switch {
	case !p.Published:
		st.State = "unpublished"
	case st.HTTPS:
		st.State = "active" // vekil (ör. Cloudflare) arkasında DNS başka adres gösterse de
	case st.DNS:
		st.State = "cert"
	default:
		st.State = "dns"
	}
	writeJSON(w, http.StatusOK, st)
}
