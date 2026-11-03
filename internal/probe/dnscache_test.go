package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDNS sahte çözücü: yanıt ya da hata değiştirilebilir, çağrılar sayılır.
type fakeDNS struct {
	mu    sync.Mutex
	ips   map[string][]string
	err   error
	calls int
}

func (f *fakeDNS) lookup(_ context.Context, host string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	ips, ok := f.ips[host]
	if !ok {
		return nil, &net.DNSError{Err: "no such host", Name: host, IsNotFound: true}
	}
	return ips, nil
}

func (f *fakeDNS) set(host string, ips []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ips[host], f.err = ips, err
}

func (f *fakeDNS) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func testDNSCache(t *testing.T, dir string) (*dnsCache, *fakeDNS, *time.Time) {
	t.Helper()
	d := newDNSCache(dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f := &fakeDNS{ips: map[string][]string{}}
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	d.lookup, d.now = f.lookup, func() time.Time { return now }
	return d, f, &now
}

func mustResolve(t *testing.T, d *dnsCache, host string, want []string, wantStale bool) {
	t.Helper()
	got, stale, err := d.resolve(context.Background(), host)
	if err != nil || !slices.Equal(got, want) || stale != wantStale {
		t.Fatalf("resolve(%s) = %v stale=%v err=%v; beklenen %v stale=%v", host, got, stale, err, want, wantStale)
	}
}

func TestDNSCacheStaleIfError(t *testing.T) {
	dir := t.TempDir()
	d, f, now := testDNSCache(t, dir)
	host := "panel.example.com"

	// Kayıt yokken hata olduğu gibi döner.
	if _, _, err := d.resolve(context.Background(), host); err == nil {
		t.Fatal("kayıt yokken hata beklenirdi")
	}

	f.set(host, []string{"192.0.2.1"}, nil)
	mustResolve(t, d, host, []string{"192.0.2.1"}, false)
	n := f.count()

	// Taze kayıtta DNS sorulmaz.
	*now = now.Add(59 * time.Minute)
	mustResolve(t, d, host, []string{"192.0.2.1"}, false)
	if f.count() != n {
		t.Fatalf("taze kayıtta DNS çağrıldı (%d -> %d)", n, f.count())
	}

	// 1 sa sonra yeniden çözülür.
	f.set(host, []string{"192.0.2.2"}, nil)
	*now = now.Add(2 * time.Minute)
	mustResolve(t, d, host, []string{"192.0.2.2"}, false)
	if f.count() != n+1 {
		t.Fatalf("süresi dolan kayıt yeniden çözülmedi")
	}

	// Hata (NXDOMAIN dahil) -> eski IP, süre sınırı olmadan.
	*now = now.Add(2 * time.Hour)
	f.set(host, nil, errors.New("sunucu yanıt vermiyor"))
	mustResolve(t, d, host, []string{"192.0.2.2"}, true)
	f.mu.Lock() // NXDOMAIN
	delete(f.ips, host)
	f.err = nil
	f.mu.Unlock()
	*now = now.Add(30 * 24 * time.Hour)
	mustResolve(t, d, host, []string{"192.0.2.2"}, true)

	// Hata sürerken her çağrıda yeniden denenir (TTL beklenmez).
	n = f.count()
	mustResolve(t, d, host, []string{"192.0.2.2"}, true)
	if f.count() != n+1 {
		t.Fatal("hata varken yeniden çözülmedi")
	}

	// Düzelince yeni IP hemen alınır.
	f.set(host, []string{"192.0.2.3", "2001:db8::3"}, nil)
	mustResolve(t, d, host, []string{"192.0.2.3", "2001:db8::3"}, false)

	// Disk: 0600, yeniden açılışta yüklenir (DNS kapalıyken bile eski IP).
	path := filepath.Join(dir, dnsFileName)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("dosya izni %v, beklenen 0600", st.Mode().Perm())
	}
	d2, f2, now2 := testDNSCache(t, dir)
	*now2 = now.Add(time.Minute)
	mustResolve(t, d2, host, []string{"192.0.2.3", "2001:db8::3"}, false)
	if f2.count() != 0 {
		t.Fatal("diskteki taze kayıtta DNS çağrıldı")
	}
	*now2 = now.Add(3 * time.Hour)
	f2.err = errors.New("kapalı")
	mustResolve(t, d2, host, []string{"192.0.2.3", "2001:db8::3"}, true)
	if ents, _ := os.ReadDir(dir); len(ents) != 1 {
		t.Fatalf("dizinde geçici dosya kaldı: %v", ents)
	}
}

func TestDNSCacheCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, dnsFileName)
	for _, data := range []string{"{bozuk", `{"a.example":{"ips":["değil-ip"],"resolved":"2026-11-03T12:00:00Z"}}`} {
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		d, f, _ := testDNSCache(t, dir)
		f.err = errors.New("kapalı")
		if _, _, err := d.resolve(context.Background(), "a.example"); err == nil {
			t.Fatalf("bozuk dosya (%q) yok sayılmadı", data)
		}
	}
	// Yazılamayan dizin: önbellek bellekte çalışır.
	d, f, _ := testDNSCache(t, filepath.Join(dir, "yok", "dizin"))
	f.set("b.example", []string{"192.0.2.9"}, nil)
	mustResolve(t, d, "b.example", []string{"192.0.2.9"}, false)
	f.set("b.example", nil, errors.New("kapalı"))
	d.entries["b.example"].Resolved = time.Time{}
	mustResolve(t, d, "b.example", []string{"192.0.2.9"}, true)
}

// TestDNSCacheTransport: çözücü hata verirken istek eski IP ile başarılı olur;
// sertifika URL'deki adla doğrulanır (yanlış ada verilmiş sertifika reddedilir).
func TestDNSCacheTransport(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok "+r.Host+" "+r.TLS.ServerName)
	}))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())

	d, f, now := testDNSCache(t, t.TempDir())
	f.set("example.com", []string{"127.0.0.1"}, nil)
	f.set("yanlis.test", []string{"127.0.0.1"}, nil) // httptest sertifikası bu adı içermez
	hc := newHTTPClient(d)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	hc.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}

	get := func(host string) (string, error) {
		hc.CloseIdleConnections() // her istek yeni bağlantı (DialContext) açsın
		resp, err := hc.Get("https://" + host + ":" + port + "/")
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), nil
	}

	body, err := get("example.com")
	if err != nil || !strings.HasPrefix(body, "ok example.com:"+port+" example.com") {
		t.Fatalf("ilk istek: %q %v", body, err)
	}
	if _, err := get("yanlis.test"); err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("yanlış sertifika kabul edildi: %v", err)
	}
	*now = now.Add(2 * time.Hour)
	f.set("example.com", nil, &net.DNSError{Err: "server misbehaving", Name: "example.com", IsTemporary: true})
	body, err = get("example.com")
	if err != nil || !strings.HasPrefix(body, "ok example.com") {
		t.Fatalf("DNS hatasında eski IP ile istek başarısız: %q %v", body, err)
	}

	if tc := hc.Transport.(*http.Transport).TLSClientConfig; tc.InsecureSkipVerify {
		t.Fatal("InsecureSkipVerify açık")
	}

	// Kayıt yokken çözüm hatası istek hatası olarak döner.
	if _, err := get("kayitsiz.test"); err == nil {
		t.Fatal("kaydı olmayan ad için hata beklenirdi")
	}
}
