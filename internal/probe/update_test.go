package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
)

// updatePanel güncelleme teklif eden sahte panel.
type updatePanel struct {
	mu        sync.Mutex
	offer     *agentupdate.Offer
	binary    []byte
	reports   []agentupdate.Report
	platforms []string
	srv       *httptest.Server
}

func newUpdatePanel(t *testing.T) *updatePanel {
	p := &updatePanel{}
	p.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.platforms = append(p.platforms, r.Header.Get("X-Probe-Platform"))
		switch r.URL.Path {
		case "/api/probe/jobs":
			json.NewEncoder(w).Encode(map[string]any{"poll_after": 1, "jobs": []Job{}, "update": p.offer})
		case "/api/probe/binary":
			w.Write(p.binary)
		case "/api/probe/update":
			var rep agentupdate.Report
			json.NewDecoder(r.Body).Decode(&rep)
			p.reports = append(p.reports, rep)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(p.srv.Close)
	return p
}

func (p *updatePanel) statuses() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, r := range p.reports {
		out = append(out, r.Status)
	}
	return out
}

// signedOffer test anahtarıyla imzalı teklif (doğrulama bu anahtara yönlenir).
func signedOffer(t *testing.T, version string, body []byte) *agentupdate.Offer {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	agentupdate.TestPublicKey = pub
	t.Cleanup(func() { agentupdate.TestPublicKey = nil })
	h := sha256.Sum256(body)
	m := agentupdate.Manifest{Version: version, OS: "linux", Arch: "amd64", SHA256: hex.EncodeToString(h[:])}
	sig, err := agentupdate.Sign(priv, m)
	if err != nil {
		t.Fatal(err)
	}
	return &agentupdate.Offer{Signed: agentupdate.Signed{Manifest: m, Sig: base64.StdEncoding.EncodeToString(sig)},
		URL: "/api/probe/binary?os=linux&arch=amd64"}
}

func runAgent(t *testing.T, p *updatePanel, exe string) (chan error, context.CancelFunc) {
	t.Helper()
	u := &agentupdate.Updater{Exe: exe, Version: "1.3.0", OS: "linux", Arch: "amd64"}
	c, err := New(Config{Server: p.srv.URL, Token: "upr_test", Version: "1.3.0", AllowInsecure: true, NoMetrics: true,
		Unit: time.Millisecond, FlushEvery: 10 * time.Millisecond, MaxBackoff: 20 * time.Millisecond, Updater: u})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()
	t.Cleanup(cancel)
	return done, cancel
}

func TestClientInstallsSignedUpdate(t *testing.T) {
	p := newUpdatePanel(t)
	exe := filepath.Join(t.TempDir(), "uptime")
	os.WriteFile(exe, []byte("eski"), 0o755)
	p.binary = []byte("yeni program 1.3.1")
	p.offer = signedOffer(t, "1.3.1", p.binary)

	done, _ := runAgent(t, p, exe)
	select {
	case err := <-done:
		if !errors.Is(err, agentupdate.ErrRestart) {
			t.Fatalf("güncellemeden sonra ErrRestart bekleniyordu: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ajan güncellemeden sonra durmadı")
	}
	if b, _ := os.ReadFile(exe); string(b) != "yeni program 1.3.1" {
		t.Fatalf("program değişmeli: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "eski" {
		t.Fatalf("eski program .old olarak kalmalı: %q", b)
	}
	if st := p.statuses(); len(st) == 0 || st[0] != agentupdate.StatusStarted {
		t.Fatalf("panele başladı bildirilmeli: %v", st)
	}
	p.mu.Lock()
	plat := p.platforms[0]
	p.mu.Unlock()
	if plat != "linux/amd64" {
		t.Fatalf("platform başlığı gönderilmeli: %q", plat)
	}
}

func TestClientRejectsTamperedBinary(t *testing.T) {
	p := newUpdatePanel(t)
	exe := filepath.Join(t.TempDir(), "uptime")
	os.WriteFile(exe, []byte("eski"), 0o755)
	p.offer = signedOffer(t, "1.3.1", []byte("imzalanan program"))
	p.binary = []byte("panelin sunduğu başka program")

	done, cancel := runAgent(t, p, exe)
	waitFor(t, "başarısızlık raporu", func() bool {
		st := p.statuses()
		return len(st) >= 2 && st[1] == agentupdate.StatusFailed
	})
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("başarısız güncellemede ajan normal çalışmayı sürdürmeli: %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "eski" {
		t.Fatalf("program değişmemeli: %q", b)
	}
	// Aynı sürüm bir süre yeniden denenmez (her yoklamada indirme döngüsü olmasın).
	if st := p.statuses(); len(st) != 2 {
		t.Fatalf("tek deneme bekleniyordu: %v", st)
	}
}

func TestClientRetriesBusyDownload(t *testing.T) {
	p := newUpdatePanel(t)
	exe := filepath.Join(t.TempDir(), "uptime")
	os.WriteFile(exe, []byte("eski"), 0o755)
	p.binary = []byte("yeni")
	p.offer = signedOffer(t, "1.3.1", p.binary)
	var busy atomic.Int32
	inner := p.srv.Config.Handler
	p.srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/probe/binary" {
			busy.Add(1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		inner.ServeHTTP(w, r)
	})
	done, cancel := runAgent(t, p, exe)
	waitFor(t, "indirme denemesi", func() bool { return busy.Load() > 0 })
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("ertelenen güncellemede ajan çalışmayı sürdürmeli: %v", err)
	}
	// 429 başarısızlık sayılmaz (panele failed gitmez) ve hemen yeniden denenmez.
	for _, st := range p.statuses() {
		if st == agentupdate.StatusFailed {
			t.Fatalf("429 başarısızlık sayılmamalı: %v", p.statuses())
		}
	}
	if n := busy.Load(); n != 1 {
		t.Fatalf("bekleme süresi dolmadan yeniden denenmemeli: %d", n)
	}
}
