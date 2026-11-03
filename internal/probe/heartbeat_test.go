package probe

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hbServer sahte panel: kalp atışı ve eş listesi uçları.
type hbServer struct {
	mu       sync.Mutex
	bodies   []MeshHeartbeat
	mode     string // "" normal, "hang" yanıt vermez, "404" eski panel
	panelSec int
	version  string
	peerGets int
	srv      *httptest.Server
}

func newHBServer(t *testing.T) *hbServer {
	h := &hbServer{panelSec: 2, version: "v1"}
	h.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		mode := h.mode
		h.mu.Unlock()
		switch {
		case mode == "hang":
			select {
			case <-r.Context().Done():
			case <-time.After(time.Second):
			}
			return
		case mode == "404" || r.Header.Get("Authorization") != "Bearer upr_test":
			w.WriteHeader(404)
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		switch r.URL.Path {
		case "/api/probe/mesh":
			var hb MeshHeartbeat
			json.NewDecoder(r.Body).Decode(&hb)
			h.bodies = append(h.bodies, hb)
			json.NewEncoder(w).Encode(MeshHeartbeatResponse{
				Timing:       MeshTiming{MeshEnabled: true, PingSec: 5, LossThreshold: 3, PanelSec: h.panelSec},
				PeersVersion: h.version, ServerTime: time.Now().UnixMilli()})
		case "/api/probe/mesh/peers":
			h.peerGets++
			json.NewEncoder(w).Encode(MeshPeersResponse{Version: h.version, Peers: []MeshPeer{{ID: 2, Name: "B", Addr: "10.0.0.2:7480", Key: "k"}}})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(h.srv.Close)
	return h
}

func (h *hbServer) set(fn func(h *hbServer)) { h.mu.Lock(); fn(h); h.mu.Unlock() }

type fakeMesh struct{}

func (fakeMesh) Report() []MeshLinkReport {
	return []MeshLinkReport{{Peer: 2, State: "up", RTTms: 3}}
}
func (fakeMesh) ListenPort() int { return 7480 }

func newHBClient(t *testing.T, h *hbServer, mut func(*Config)) *Client {
	t.Helper()
	cfg := Config{Server: h.srv.URL, Token: "upr_test", AllowInsecure: true, Unit: time.Millisecond, NoMetrics: true}
	if mut != nil {
		mut(&cfg)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// Döngü: kalp atışı ölçümleri taşır, zamanlama güncellenir (sonuç gönderimi
// de panel aralığına bağlanır), eş listesi yalnız sürüm değişince çekilir.
func TestHeartbeatLoop(t *testing.T) {
	h := newHBServer(t)
	var timings, peerCalls atomic.Int32
	var lastPeers atomic.Value
	c := newHBClient(t, h, func(cfg *Config) {
		cfg.MeshSource = fakeMesh{}
		cfg.OnMeshTiming = func(t MeshTiming) {
			if t.PanelSec == 2 {
				timings.Add(1)
			}
		}
		cfg.OnMeshPeers = func(p MeshPeersResponse) { peerCalls.Add(1); lastPeers.Store(p) }
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.heartbeatLoop(ctx); close(done) }()
	waitUntil(t, func() bool { h.mu.Lock(); defer h.mu.Unlock(); return len(h.bodies) >= 5 })
	if timings.Load() == 0 || c.panelEvery() != 2*time.Millisecond || c.flushEvery() != 2*time.Millisecond {
		t.Fatalf("zamanlama uygulanmadı: %d %v %v", timings.Load(), c.panelEvery(), c.flushEvery())
	}
	if peerCalls.Load() != 1 {
		t.Fatalf("eş listesi bir kez çekilmeli: %d", peerCalls.Load())
	}
	if p := lastPeers.Load().(MeshPeersResponse); len(p.Peers) != 1 || p.Peers[0].Addr != "10.0.0.2:7480" {
		t.Errorf("eşler: %+v", p)
	}
	h.mu.Lock()
	first, last := h.bodies[0], h.bodies[len(h.bodies)-1]
	h.mu.Unlock()
	if first.PeersVersion != "" || last.PeersVersion != "v1" || last.ListenPort != 7480 || len(last.Links) != 1 || last.Links[0].Peer != 2 {
		t.Errorf("gövde: ilk %+v son %+v", first, last)
	}
	// Sürüm değişince yeniden çekilir.
	h.set(func(h *hbServer) { h.version = "v2" })
	waitUntil(t, func() bool { return peerCalls.Load() == 2 })
	cancel()
	<-done
}

// Panel 3 tur yanıt vermeyince MainLoss=3; sonraki başarılı kalp atışı bunu
// bildirir ve sayaç sıfırlanır.
func TestHeartbeatMainLoss(t *testing.T) {
	h := newHBServer(t)
	c := newHBClient(t, h, nil)
	ctx := context.Background()
	h.set(func(h *hbServer) { h.mode = "hang" })
	for range 3 {
		if wait := c.heartbeatOnce(ctx); wait != 5*time.Millisecond {
			t.Fatalf("bekleme %v", wait)
		}
	}
	if c.MainLoss() != 3 {
		t.Fatalf("MainLoss %d, 3 bekleniyordu", c.MainLoss())
	}
	h.set(func(h *hbServer) { h.mode = "" })
	c.heartbeatOnce(ctx)
	h.mu.Lock()
	got := h.bodies[len(h.bodies)-1].MainLoss
	h.mu.Unlock()
	if got != 3 || c.MainLoss() != 0 {
		t.Fatalf("bildirilen %d, sayaç %d", got, c.MainLoss())
	}
}

// Eski panel (404): kalp atışı uzun aralıkla yeniden denenir, kayıp sayılmaz,
// sonuç gönderimi FlushEvery'de kalır.
func TestHeartbeatOldPanel(t *testing.T) {
	h := newHBServer(t)
	h.set(func(h *hbServer) { h.mode = "404" })
	c := newHBClient(t, h, func(cfg *Config) { cfg.HeartbeatRetry = time.Minute })
	if wait := c.heartbeatOnce(context.Background()); wait != time.Minute {
		t.Fatalf("geri çekilme %v", wait)
	}
	if c.MainLoss() != 0 || c.flushEvery() != c.cfg.FlushEvery {
		t.Errorf("kayıp %d, gönderim %v", c.MainLoss(), c.flushEvery())
	}
	if def := newHBClient(t, h, nil); def.cfg.HeartbeatRetry != 300*time.Millisecond {
		t.Errorf("varsayılan yeniden deneme %v", def.cfg.HeartbeatRetry)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("koşul gerçekleşmedi")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
