package probe

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// DNS son bilinen IP önbelleği (stale-if-error), yalnızca ajanın ana sunucu
// bağlantısı için. Ana sunucunun alan adı çözülemediğinde (DNS sunucusu
// kapalı, NXDOMAIN, zaman aşımı) ajan son başarılı çözümün IP'leriyle
// bağlanmaya devam eder; süre sınırı yoktur. Kayıtlar diske de yazılır
// (PROBE_STATE_DIR/uptime.dns.json): ajan DNS kesintisi sırasında yeniden
// başlasa da bağlanabilir.
//
// Go'nun sistem çözücüsü TTL bildirmez; kayıtlar dnsTTL sonra yeniden çözülür.
// Son çözüm başarısızsa her yeni bağlantıda yeniden denenir; DNS düzelince
// yeni IP'ler hemen kullanılır.
//
// Yalnızca TCP bağlantısının hedef IP'si değişir: TLS SNI, Host başlığı ve
// sertifika doğrulaması URL'deki alan adıyla yapılır (bkz. newHTTPClient).

const (
	dnsTTL        = time.Hour
	dnsLookupWait = 5 * time.Second // önbellekte kayıt varken çözüm için en uzun bekleme
	dnsFileName   = "uptime.dns.json"
)

type dnsEntry struct {
	IPs      []string  `json:"ips"`
	Resolved time.Time `json:"resolved"` // son başarılı çözüm
	failing  bool      // son çözüm denemesi başarısız (eski IP kullanılıyor)
}

// dnsCache ana sunucu adlarının son bilinen IP'leri.
type dnsCache struct {
	mu      sync.Mutex
	entries map[string]*dnsEntry
	path    string // boşsa yalnızca bellekte
	log     *slog.Logger
	lookup  func(ctx context.Context, host string) ([]string, error)
	now     func() time.Time
	saveErr bool // yazma hatası bir kez günlüğe yazılır
}

// newDNSCache dir altındaki kayıt dosyasını yükler (yoksa ya da bozuksa boş
// başlar). dir boşsa önbellek yalnızca bellekte tutulur.
func newDNSCache(dir string, log *slog.Logger) *dnsCache {
	d := &dnsCache{
		entries: map[string]*dnsEntry{}, log: log,
		lookup: net.DefaultResolver.LookupHost, now: time.Now,
	}
	if dir != "" {
		d.path = filepath.Join(dir, dnsFileName)
		d.load()
	}
	return d
}

func (d *dnsCache) load() {
	data, err := os.ReadFile(d.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			d.log.Warn("DNS önbellek dosyası okunamadı", "dosya", d.path, "hata", err)
		}
		return
	}
	var m map[string]*dnsEntry
	if err := json.Unmarshal(data, &m); err != nil {
		d.log.Warn("DNS önbellek dosyası bozuk, yok sayıldı", "dosya", d.path, "hata", err)
		return
	}
	for host, e := range m {
		if e == nil || len(e.IPs) == 0 || slices.ContainsFunc(e.IPs, func(s string) bool { return net.ParseIP(s) == nil }) {
			continue
		}
		d.entries[strings.ToLower(host)] = e
	}
}

// saveLocked kayıtları atomik olarak (geçici dosya + yeniden adlandırma, 0600) yazar.
func (d *dnsCache) saveLocked() {
	if d.path == "" {
		return
	}
	data, _ := json.Marshal(d.entries)
	err := writeFileAtomic(d.path, data)
	if err != nil && !d.saveErr {
		d.log.Info("DNS önbellek dosyası yazılamadı; önbellek yalnızca bellekte (PROBE_STATE_DIR ile yazılabilir bir dizin verin)", "dosya", d.path, "hata", err)
	}
	d.saveErr = err != nil
}

func writeFileAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil && !errors.Is(err, errors.ErrUnsupported) {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// resolve host'un IP'lerini verir. Taze kayıtta DNS sorulmaz. Çözüm başarısız
// olur ve eski kayıt varsa eski IP'ler döner (stale=true).
func (d *dnsCache) resolve(ctx context.Context, host string) (ips []string, stale bool, err error) {
	key := strings.ToLower(host)
	d.mu.Lock()
	e := d.entries[key]
	if e != nil && !e.failing && d.now().Sub(e.Resolved) < dnsTTL {
		ips = slices.Clone(e.IPs)
		d.mu.Unlock()
		return ips, false, nil
	}
	d.mu.Unlock()

	lctx := ctx
	if e != nil { // geri dönülecek kayıt var: çözücü takılırsa uzun bekleme
		var cancel context.CancelFunc
		lctx, cancel = context.WithTimeout(ctx, dnsLookupWait)
		defer cancel()
	}
	got, lerr := d.lookup(lctx, host)
	got = slices.DeleteFunc(slices.Clone(got), func(s string) bool { return net.ParseIP(s) == nil })
	if lerr == nil && len(got) == 0 {
		lerr = errors.New("adres bulunamadı")
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	e = d.entries[key] // bu arada başka bağlantı güncellemiş olabilir
	if lerr != nil {
		if e == nil {
			return nil, false, lerr
		}
		if !e.failing {
			d.log.Warn("ana sunucunun adı çözülemedi; son bilinen IP kullanılıyor", "ad", host, "ip", strings.Join(e.IPs, ","), "son_cozum", e.Resolved.Format(time.RFC3339), "hata", lerr)
		}
		e.failing = true
		return slices.Clone(e.IPs), true, nil
	}
	if e != nil && e.failing {
		d.log.Info("ana sunucunun adı yeniden çözülebiliyor", "ad", host, "ip", strings.Join(got, ","))
	}
	d.entries[key] = &dnsEntry{IPs: got, Resolved: d.now()}
	d.saveLocked()
	return slices.Clone(got), false, nil
}

// dialContext alan adlarını önbellekten çözerek bağlanır; IP adresleri
// doğrudan aranır. IP'ler sırayla denenir (ilk başarılı bağlantı döner).
func (d *dnsCache) dialContext(dialer *net.Dialer) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || net.ParseIP(host) != nil {
			return dialer.DialContext(ctx, network, addr)
		}
		ips, _, err := d.resolve(ctx, host)
		if err != nil {
			var de *net.DNSError
			if !errors.As(err, &de) {
				de = &net.DNSError{Err: err.Error(), Name: host}
			}
			return nil, &net.OpError{Op: "dial", Net: network, Err: de}
		}
		var firstErr error
		for _, ip := range ips {
			c, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
			if err == nil {
				return c, nil
			}
			if firstErr == nil {
				firstErr = err
			}
			if ctx.Err() != nil {
				break
			}
		}
		return nil, firstErr
	}
}
