package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

type fakeSecurity struct {
	fakeCollector
	total atomic.Int32
}

func (f *fakeSecurity) Security(context.Context) metrics.Security {
	return metrics.Security{Time: time.Now().UnixMilli(),
		Updates: &metrics.SecUpdates{Manager: metrics.ManagerApt, Total: int(f.total.Load()), Packages: []metrics.SecPkg{}}}
}

// secPanel sahte panel: interval < 0 ise security_interval alanı hiç yok (eski panel).
func secPanel(t *testing.T, interval int, status int) (*httptest.Server, *atomic.Int32) {
	var posts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/probe/jobs":
			out := map[string]any{"probe": map[string]any{"id": 1, "name": "p"}, "poll_after": 1, "version": 1, "jobs": []any{}, "metrics_interval": 1000000}
			if interval >= 0 {
				out["security_interval"] = interval
			}
			json.NewEncoder(w).Encode(out)
		case "/api/probe/security":
			var s metrics.Security
			if json.NewDecoder(r.Body).Decode(&s) != nil || s.Updates == nil {
				t.Error("gövde çözülemedi")
			}
			posts.Add(1)
			w.WriteHeader(status)
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &posts
}

func runSecClient(t *testing.T, url string, f *fakeSecurity, noSec bool) {
	t.Helper()
	c, err := New(Config{Server: url, Token: "upr_test", AllowInsecure: true, Unit: 100 * time.Microsecond, Metrics: f, NoSecurity: noSec})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// Eski panel (alan yok) ya da SECURITY=0: hiç istek yok.
func TestSecurityOldPanelAndDisabled(t *testing.T) {
	srv, posts := secPanel(t, -1, http.StatusNoContent)
	runSecClient(t, srv.URL, &fakeSecurity{}, false)
	srv2, posts2 := secPanel(t, 300, http.StatusNoContent)
	runSecClient(t, srv2.URL, &fakeSecurity{}, true)
	time.Sleep(150 * time.Millisecond)
	if posts.Load()+posts2.Load() != 0 {
		t.Fatalf("istek gitti: %d %d", posts.Load(), posts2.Load())
	}
}

// Yalnız değişince ve 12 turda bir gönderir; en az 300 birim aralık uygulanır.
func TestSecuritySendsOnChange(t *testing.T) {
	srv, posts := secPanel(t, 1, http.StatusNoContent) // 1 -> 300 birim = 30 ms
	f := &fakeSecurity{}
	runSecClient(t, srv.URL, f, false)
	waitUntil(t, func() bool { return posts.Load() >= 1 })
	first := posts.Load()
	time.Sleep(70 * time.Millisecond) // ~2 tur, değişiklik yok
	if n := posts.Load(); n != first {
		t.Fatalf("değişmeden gönderildi: %d -> %d", first, n)
	}
	f.total.Store(3)
	waitUntil(t, func() bool { return posts.Load() == first+1 })
}

// 404: aynı gövde yeniden gönderilmez.
func TestSecurity404Backoff(t *testing.T) {
	srv, posts := secPanel(t, 300, http.StatusNotFound)
	runSecClient(t, srv.URL, &fakeSecurity{}, false)
	waitUntil(t, func() bool { return posts.Load() >= 1 })
	time.Sleep(100 * time.Millisecond)
	if n := posts.Load(); n > 1 {
		t.Fatalf("404 sonrası %d istek", n)
	}
}

// fakeAuthSecurity SSH farkı biriktiren sahte toplayıcı (SecurityAcker).
type fakeAuthSecurity struct {
	fakeCollector
	pending atomic.Int32
	acks    atomic.Int32
}

func (f *fakeAuthSecurity) Security(context.Context) metrics.Security {
	return metrics.Security{Time: time.Now().UnixMilli(),
		Updates: &metrics.SecUpdates{Manager: metrics.ManagerApt, Packages: []metrics.SecPkg{}},
		Auth:    &metrics.SecAuth{Source: "journal", Failed: int(f.pending.Load())}}
}

func (f *fakeAuthSecurity) AckSecurity(rep metrics.Security) {
	f.acks.Add(1)
	f.pending.Add(-int32(rep.Auth.Failed))
}

// SSH farkı varken özet aynı olsa da gönderilir; 2xx ve 4xx sonrası Ack çağrılır,
// sıfır fark yeniden gönderim sebebi olmaz.
func TestSecurityAuthDeltaAck(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusUnprocessableEntity} {
		srv, posts := secPanel(t, 1, status)
		f := &fakeAuthSecurity{}
		c, err := New(Config{Server: srv.URL, Token: "upr_test", AllowInsecure: true, Unit: 100 * time.Microsecond, Metrics: f})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(ctx); close(done) }()
		waitUntil(t, func() bool { return posts.Load() >= 1 })
		f.pending.Store(4)
		waitUntil(t, func() bool { return posts.Load() >= 2 && f.pending.Load() == 0 })
		n := posts.Load()
		time.Sleep(70 * time.Millisecond) // ~2 tur, fark yok
		if m := posts.Load(); m != n {
			t.Fatalf("durum %d: fark yokken gönderildi: %d -> %d", status, n, m)
		}
		if f.acks.Load() < 2 {
			t.Fatalf("durum %d: Ack çağrılmadı (%d)", status, f.acks.Load())
		}
		cancel()
		<-done
	}
}
