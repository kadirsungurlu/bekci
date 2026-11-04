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

type fakeInventory struct {
	fakeCollector
	version atomic.Int32
}

func (f *fakeInventory) Inventory(context.Context) metrics.Inventory {
	return metrics.Inventory{Time: time.Now().UnixMilli(), Disks: []metrics.InvDisk{{Mount: "/", Total: uint64(f.version.Load())}}}
}

// invPanel sahte panel: interval < 0 ise inventory_interval alanı hiç yok (eski panel).
type invPanel struct {
	srv      *httptest.Server
	interval int
	status   atomic.Int32
	posts    atomic.Int32
	metrics  atomic.Int32
}

func newInvPanel(t *testing.T, interval int) *invPanel {
	p := &invPanel{interval: interval}
	p.status.Store(http.StatusNoContent)
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/probe/jobs":
			out := map[string]any{"probe": map[string]any{"id": 1, "name": "p"}, "poll_after": 1, "version": 1, "jobs": []any{}, "metrics_interval": 1000000}
			if p.interval >= 0 {
				out["inventory_interval"] = p.interval
			}
			json.NewEncoder(w).Encode(out)
		case "/api/probe/inventory":
			p.posts.Add(1)
			w.WriteHeader(int(p.status.Load()))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func runInvClient(t *testing.T, p *invPanel, f *fakeInventory) {
	t.Helper()
	c, err := New(Config{Server: p.srv.URL, Token: "upr_test", AllowInsecure: true, Unit: time.Millisecond, Metrics: f})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

// Yeni ajan + eski panel: alan yoksa hiç toplamaz, hiç istek atmaz.
func TestInventoryOldPanelSilent(t *testing.T) {
	p := newInvPanel(t, -1)
	runInvClient(t, p, &fakeInventory{})
	time.Sleep(150 * time.Millisecond)
	if n := p.posts.Load(); n != 0 {
		t.Fatalf("eski panele %d istek gitti", n)
	}
}

// Yalnız değişince (ve 12 turda bir) gönderir.
func TestInventorySendsOnChange(t *testing.T) {
	p := newInvPanel(t, 20)
	f := &fakeInventory{}
	runInvClient(t, p, f)
	waitUntil(t, func() bool { return p.posts.Load() >= 1 })
	first := p.posts.Load()
	time.Sleep(60 * time.Millisecond) // 3 tur: değişiklik yok
	if n := p.posts.Load(); n != first {
		t.Fatalf("değişmeden gönderildi: %d -> %d", first, n)
	}
	f.version.Store(2)
	waitUntil(t, func() bool { return p.posts.Load() == first+1 })
	waitUntil(t, func() bool { return p.posts.Load() >= first+2 }) // zorunlu yeniden gönderim
}

// Panel uç noktayı tanımıyorsa (404) aynı gövde tekrar tekrar gönderilmez.
func TestInventory404Backoff(t *testing.T) {
	p := newInvPanel(t, 10)
	p.status.Store(http.StatusNotFound)
	runInvClient(t, p, &fakeInventory{})
	waitUntil(t, func() bool { return p.posts.Load() >= 1 })
	time.Sleep(60 * time.Millisecond)
	if n := p.posts.Load(); n > 1 {
		t.Fatalf("404 sonrası %d istek", n)
	}
}

// Sunucu hatasında sonraki turda yeniden dener.
func TestInventoryRetriesOn500(t *testing.T) {
	p := newInvPanel(t, 10)
	p.status.Store(http.StatusInternalServerError)
	runInvClient(t, p, &fakeInventory{})
	waitUntil(t, func() bool { return p.posts.Load() >= 3 })
}
