// Package rdap alan adının bitiş tarihini RDAP (RFC 9083) ile sorgular.
//
// IANA'nın önyükleme dosyası (dns.json) TLD → RDAP sunucusu eşlemesini verir;
// dosya bellekte bir gün tutulur. Kayıt adı publicsuffix listesiyle bulunur
// (ör. "www.ornek.com.tr" → "ornek.com.tr"). RDAP sunucusu olmayan TLD'ler
// (çoğu ccTLD) hata değil "desteklenmiyor" sonucu verir; ağ hataları ise hata
// olarak döner ki çağıran yanlış uyarı üretmesin.
package rdap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// Durum kodları (store.Monitor.DomainStatus ile aynı).
const (
	StatusOK          = "ok"          // bitiş tarihi okundu
	StatusUnsupported = "unsupported" // TLD için RDAP sunucusu yok ya da ad sorgulanamaz (IP, yerel ad)
	StatusNotFound    = "not_found"   // RDAP 404: kayıt yok
	StatusNoExpiry    = "no_expiry"   // kayıt var ama bitiş tarihi verilmiyor
)

// DefaultBootstrapURL IANA önyükleme dosyası.
const DefaultBootstrapURL = "https://data.iana.org/rdap/dns.json"

// Result tek sorgunun sonucu.
type Result struct {
	Domain    string    // sorgulanan kayıt adı (publicsuffix+1)
	Status    string    // StatusOK | StatusUnsupported | StatusNotFound | StatusNoExpiry
	Expires   time.Time // StatusOK'ta bitiş
	Registrar string    // kayıt operatörü (varsa)
}

// ErrUnavailable sunucuya ulaşılamadı ya da beklenmeyen yanıt (geçici hata).
var ErrUnavailable = errors.New("rdap sunucusuna ulaşılamıyor")

// Client önyükleme dosyasını önbellekleyen RDAP istemcisi.
type Client struct {
	HTTP         *http.Client
	BootstrapURL string
	// BootstrapTTL önyükleme dosyasının yenilenme aralığı (varsayılan 24 saat).
	BootstrapTTL time.Duration
	now          func() time.Time

	mu        sync.Mutex
	services  map[string][]string // tld → sunucu adresleri
	fetchedAt time.Time
}

// New varsayılan ayarlarla istemci oluşturur.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 15 * time.Second}, BootstrapURL: DefaultBootstrapURL, BootstrapTTL: 24 * time.Hour, now: time.Now}
}

// Domain ana makine adının kayıt adını döner (publicsuffix+1). IP adresleri,
// tek etiketli ve yerel adlar için boş döner.
func Domain(host string) string {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
		return ""
	}
	if strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".lan") || strings.HasSuffix(host, ".home.arpa") {
		return ""
	}
	// En uzun ICANN soneki + bir etiket. Özel (ICANN dışı) sonekler
	// (github.io, s3.amazonaws.com) kayıt adı sayılmaz: RDAP kaydı
	// "github.io" içindir, "kadir.github.io" için değil. Listede olmayan
	// TLD'de son iki etiket alınır (önyükleme dosyasında yoksa "desteklenmiyor").
	labels := strings.Split(host, ".")
	start := len(labels) - 1 // en uzun ICANN sonekinin başladığı etiket
	for i := len(labels) - 1; i > 0; i-- {
		s := strings.Join(labels[i:], ".")
		if ps, icann := publicsuffix.PublicSuffix(s); icann && ps == s {
			start = i
		}
	}
	if start == 0 {
		return ""
	}
	return strings.Join(labels[start-1:], ".")
}

// HostOf hedef metninden ana makine adını çıkarır: URL ise host'u, "host:port"
// ise host'u, aksi halde metnin kendisini döner.
func HostOf(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}
	if strings.Contains(target, "://") {
		if u, err := url.Parse(target); err == nil {
			return u.Hostname()
		}
	}
	if h, _, err := net.SplitHostPort(target); err == nil {
		return h
	}
	return strings.Trim(target, "[]")
}

// Lookup ana makine adının alan adını sorgular.
func (c *Client) Lookup(ctx context.Context, host string) (Result, error) {
	domain := Domain(host)
	if domain == "" {
		return Result{Domain: strings.ToLower(host), Status: StatusUnsupported}, nil
	}
	res := Result{Domain: domain}
	servers, err := c.serversFor(ctx, domain)
	if err != nil {
		return res, err
	}
	if len(servers) == 0 {
		res.Status = StatusUnsupported
		return res, nil
	}
	var lastErr error
	for _, base := range servers {
		r, err := c.query(ctx, base, domain)
		if err == nil {
			return r, nil
		}
		lastErr = err
		if errors.Is(err, errNotFound) {
			res.Status = StatusNotFound
			return res, nil
		}
	}
	return res, fmt.Errorf("%w: %v", ErrUnavailable, lastErr)
}

var errNotFound = errors.New("not found")

// query tek RDAP sunucusuna domain sorgusu.
func (c *Client) query(ctx context.Context, base, domain string) (Result, error) {
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"domain/"+url.PathEscape(domain), nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Accept", "application/rdap+json, application/json")
	req.Header.Set("User-Agent", "Bekci-rdap/1.0")
	resp, err := c.http().Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Result{}, err
	}
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Result{}, errNotFound
	case resp.StatusCode != http.StatusOK:
		return Result{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var doc struct {
		LDHName string `json:"ldhName"`
		Events  []struct {
			Action string `json:"eventAction"`
			Date   string `json:"eventDate"`
		} `json:"events"`
		Entities []entity `json:"entities"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return Result{}, fmt.Errorf("geçersiz RDAP yanıtı: %w", err)
	}
	res := Result{Domain: domain, Status: StatusNoExpiry}
	for _, ev := range doc.Events {
		if strings.EqualFold(ev.Action, "expiration") {
			if t, err := time.Parse(time.RFC3339, ev.Date); err == nil {
				res.Expires, res.Status = t, StatusOK
			}
		}
	}
	res.Registrar = registrarOf(doc.Entities)
	return res, nil
}

// entity RDAP varlığı (kayıt operatörünün adı vCard'da).
type entity struct {
	Roles      []string        `json:"roles"`
	VCardArray json.RawMessage `json:"vcardArray"`
	Entities   []entity        `json:"entities"`
}

// registrarOf "registrar" rolündeki varlığın vCard "fn" alanını döner.
func registrarOf(ents []entity) string {
	for _, e := range ents {
		for _, r := range e.Roles {
			if strings.EqualFold(r, "registrar") {
				if fn := vcardFN(e.VCardArray); fn != "" {
					return fn
				}
			}
		}
		if fn := registrarOf(e.Entities); fn != "" {
			return fn
		}
	}
	return ""
}

// vcardFN jCard içindeki "fn" özelliğinin değeri: ["vcard", [["fn", {}, "text", "Ad"], …]].
func vcardFN(raw json.RawMessage) string {
	var arr []json.RawMessage
	if json.Unmarshal(raw, &arr) != nil || len(arr) < 2 {
		return ""
	}
	var props [][]json.RawMessage
	if json.Unmarshal(arr[1], &props) != nil {
		return ""
	}
	for _, p := range props {
		if len(p) < 4 {
			continue
		}
		var name, val string
		if json.Unmarshal(p[0], &name) == nil && strings.EqualFold(name, "fn") && json.Unmarshal(p[3], &val) == nil {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// serversFor TLD'nin RDAP sunucuları (önyükleme dosyasından; boş = yok).
func (c *Client) serversFor(ctx context.Context, domain string) ([]string, error) {
	services, err := c.bootstrap(ctx)
	if err != nil {
		return nil, err
	}
	// En uzun eşleşen sonek (ör. "co.uk" gibi çok etiketli girdiler için).
	labels := strings.Split(domain, ".")
	for i := 1; i < len(labels); i++ {
		if s, ok := services[strings.Join(labels[i:], ".")]; ok {
			return s, nil
		}
	}
	return nil, nil
}

// bootstrap önyükleme dosyasını (önbellekten ya da ağdan) döner.
func (c *Client) bootstrap(ctx context.Context) (map[string][]string, error) {
	ttl := c.BootstrapTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.services != nil && c.clock().Sub(c.fetchedAt) < ttl {
		return c.services, nil
	}
	u := c.BootstrapURL
	if u == "" {
		u = DefaultBootstrapURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Bekci-rdap/1.0")
	resp, err := c.http().Do(req)
	if err != nil {
		if c.services != nil {
			return c.services, nil // eski kopya yenisinden iyidir
		}
		return nil, fmt.Errorf("%w: önyükleme dosyası: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if c.services != nil {
			return c.services, nil
		}
		return nil, fmt.Errorf("%w: önyükleme dosyası HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	var doc struct {
		Services [][]json.RawMessage `json:"services"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("%w: önyükleme dosyası okunamadı: %v", ErrUnavailable, err)
	}
	services := map[string][]string{}
	for _, svc := range doc.Services {
		if len(svc) < 2 {
			continue
		}
		var tlds, urls []string
		json.Unmarshal(svc[0], &tlds)
		json.Unmarshal(svc[1], &urls)
		// HTTPS adresleri önce.
		var https, other []string
		for _, s := range urls {
			if strings.HasPrefix(s, "https://") {
				https = append(https, s)
			} else {
				other = append(other, s)
			}
		}
		for _, t := range tlds {
			services[strings.ToLower(t)] = append(https, other...)
		}
	}
	c.services, c.fetchedAt = services, c.clock()
	return services, nil
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}
