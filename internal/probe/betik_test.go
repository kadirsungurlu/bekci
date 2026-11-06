package probe

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
	"github.com/kadirsungurlu/bekci/internal/betikajan"
)

const wireBody = "echo tamam\n"

type wireExec struct {
	mu   sync.Mutex
	runs int
}

func (e *wireExec) Run(context.Context, betikajan.Spec) betikajan.Outcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.runs++
	return betikajan.Outcome{Output: []byte("o"), OutputSHA256: betik.Digest([]byte("o"))}
}
func (e *wireExec) Stop(string) error        { return nil }
func (e *wireExec) Units() ([]string, error) { return nil, nil }
func (e *wireExec) count() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.runs
}

// scriptPanel betik bilen (ya da bilmeyen) sahte panel.
type scriptPanel struct {
	mu       sync.Mutex
	old      bool // eski panel: alan yok, script-result 404
	once     *betik.JobList
	headers  []string
	keys     []string
	events   []betik.Event
	statuses int
	polls    int
	binary   int
	offer    any
}

func (p *scriptPanel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch r.URL.Path {
	case "/api/probe/jobs":
		p.polls++
		p.headers = append(p.headers, r.Header.Get(betik.HeaderCapability))
		p.keys = append(p.keys, r.Header.Get(betik.HeaderAgentKey))
		resp := map[string]any{"poll_after": 1, "jobs": []Job{}}
		if p.once != nil && !p.old {
			resp["script_jobs"] = p.once
			p.once = nil
		}
		if p.offer != nil {
			resp["update"] = p.offer
		}
		json.NewEncoder(w).Encode(resp)
	case "/api/probe/script-result":
		if p.old {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var ev betik.Event
		json.NewDecoder(r.Body).Decode(&ev)
		p.events = append(p.events, ev)
		w.WriteHeader(http.StatusNoContent)
	case "/api/probe/binary":
		p.binary++
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (p *scriptPanel) snapshot() ([]betik.Event, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]betik.Event(nil), p.events...), p.polls
}

type wire struct {
	yayin ed25519.PrivateKey
	exec  *wireExec
	agent *betikajan.Agent
	paths betikajan.Paths
	host  string
}

func newWire(t *testing.T) *wire {
	t.Helper()
	_, yayin, _ := ed25519.GenerateKey(rand.Reader)
	_, onay, _ := ed25519.GenerateKey(rand.Reader)
	d := t.TempDir()
	w := &wire{yayin: yayin, exec: &wireExec{}, paths: betikajan.Paths{
		Config: filepath.Join(d, "c", "betik.json"), StateDir: filepath.Join(d, "s"), LogDir: filepath.Join(d, "l"),
	}}
	host, err := betikajan.Enable(w.paths, betikajan.EnableOptions{
		Yayin:  []string{betik.EncodePublicKey(yayin.Public().(ed25519.PublicKey))},
		Onay:   []string{betik.EncodePublicKey(onay.Public().(ed25519.PublicKey))},
		Groups: []string{"web"},
		Allow:  []betikajan.Allow{{ID: "temizle", MinApproval: "none", SHA256: betik.Digest([]byte(wireBody))}},
	})
	if err != nil {
		t.Fatal(err)
	}
	w.host = host
	plat := betikajan.Platform{OK: true}
	w.agent = betikajan.New(betikajan.Options{Paths: w.paths, Exec: w.exec, Platform: &plat})
	return w
}

func (w *wire) job(t *testing.T, body string) *betik.JobList {
	t.Helper()
	now := time.Now().Unix()
	m := betik.Manifest{
		Schema: 1, ID: "temizle", Version: 1, SHA256: betik.Digest([]byte(wireBody)), Interpreter: "sh", RunAs: "root",
		Platforms: []string{"linux"}, Approval: "none", Groups: []string{"web"}, TimeoutSec: 60, OutputMax: 4096,
		MaxPerDay: 10, NotBefore: now - 10, NotAfter: now + 1000,
	}
	pay, _ := json.Marshal(&m)
	env, err := betik.Sign(w.yayin, betik.KindManifest, pay)
	if err != nil {
		t.Fatal(err)
	}
	return &betik.JobList{Runs: []betik.RunJob{{RunID: strings.Repeat("a", 32), Manifest: env, Body: []byte(body), Params: []byte("{}")}}}
}

// startWire ajanı çalıştırır, bitince durdurur.
func startWire(t *testing.T, srv *httptest.Server, cfg Config) {
	t.Helper()
	cfg.Server, cfg.Token, cfg.Version = srv.URL, "upr_test", "1.7.0"
	cfg.Unit, cfg.NoMetrics, cfg.NoInventory = 5*time.Millisecond, true, true
	cfg.HTTPClient = srv.Client()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { c.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if f() {
			return
		}
	}
	t.Fatal(what)
}

func TestScriptRunOverTLS(t *testing.T) {
	w := newWire(t)
	p := &scriptPanel{once: w.job(t, wireBody)}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()
	startWire(t, srv, Config{Scripts: w.agent})
	eventually(t, "makbuz gelmedi", func() bool {
		ev, _ := p.snapshot()
		return len(ev) >= 2
	})
	ev, _ := p.snapshot()
	if ev[0].Kind != betik.EventAccepted || ev[1].Kind != betik.EventReceipt || w.exec.count() != 1 {
		t.Fatalf("%+v", ev)
	}
	p.mu.Lock()
	h, key := p.headers[0], p.keys[0]
	p.mu.Unlock()
	if h != "schemas=1;state=on" {
		t.Fatal(h)
	}
	if k, err := betik.ParsePublicKey(key); err != nil || betik.KeyID(k) != w.host {
		t.Fatal(key, err)
	}
}

func TestScriptTamperedOverTLS(t *testing.T) {
	w := newWire(t)
	p := &scriptPanel{once: w.job(t, "rm -rf /\n")}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()
	startWire(t, srv, Config{Scripts: w.agent})
	eventually(t, "red gelmedi", func() bool {
		ev, _ := p.snapshot()
		return len(ev) >= 1
	})
	ev, _ := p.snapshot()
	if ev[0].Kind != betik.EventRejected || ev[0].Code != betik.CodeBodyMismatch || w.exec.count() != 0 {
		t.Fatalf("%+v", ev)
	}
}

// Eski panel: alan yok, uç 404. Ajan yoklamayı sürdürür, hiçbir şey çalışmaz.
func TestScriptOldPanel(t *testing.T) {
	w := newWire(t)
	p := &scriptPanel{old: true, once: w.job(t, wireBody)}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()
	startWire(t, srv, Config{Scripts: w.agent})
	eventually(t, "yoklama sürmedi", func() bool {
		_, n := p.snapshot()
		return n >= 3
	})
	if w.exec.count() != 0 {
		t.Fatal("eski panelde çalıştı")
	}
}

// http panel: yetenek kapalı görünür, iş reddedilir.
func TestScriptInsecurePanel(t *testing.T) {
	w := newWire(t)
	p := &scriptPanel{once: w.job(t, wireBody)}
	srv := httptest.NewServer(p)
	defer srv.Close()
	startWire(t, srv, Config{Scripts: w.agent, AllowInsecure: true})
	eventually(t, "red gelmedi", func() bool {
		ev, _ := p.snapshot()
		return len(ev) >= 1
	})
	ev, _ := p.snapshot()
	p.mu.Lock()
	h, key := p.headers[0], p.keys[0]
	p.mu.Unlock()
	if ev[0].Code != betik.CodeInsecurePanel || w.exec.count() != 0 || !strings.Contains(h, "reason="+betik.ReasonInsecure) || key != "" {
		t.Fatalf("%+v %q %q", ev, h, key)
	}
}

// Kurulum yok: kapalı başlığı gider, iş reddedilir, anahtar açıklanmaz.
func TestScriptDisabledAgent(t *testing.T) {
	plat := betikajan.Platform{OK: true}
	d := t.TempDir()
	e := &wireExec{}
	a := betikajan.New(betikajan.Options{Paths: betikajan.Paths{Config: d + "/yok.json", StateDir: d + "/s", LogDir: d + "/l"}, Exec: e, Platform: &plat})
	w := newWire(t)
	p := &scriptPanel{once: w.job(t, wireBody)}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()
	startWire(t, srv, Config{Scripts: a})
	eventually(t, "red gelmedi", func() bool {
		ev, _ := p.snapshot()
		return len(ev) >= 1
	})
	ev, _ := p.snapshot()
	p.mu.Lock()
	h, key := p.headers[0], p.keys[0]
	p.mu.Unlock()
	if ev[0].Code != betik.CodeDisabled || e.count() != 0 || !strings.Contains(h, "state=off") || key != "" {
		t.Fatalf("%+v %q %q", ev, h, key)
	}
}

// Betik desteği olmayan ajan başlık göndermez.
func TestNoScriptsNoHeader(t *testing.T) {
	p := &scriptPanel{}
	srv := httptest.NewTLSServer(p)
	defer srv.Close()
	startWire(t, srv, Config{})
	eventually(t, "yoklama yok", func() bool {
		_, n := p.snapshot()
		return n >= 1
	})
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.headers[0] != "" || p.keys[0] != "" {
		t.Fatal(p.headers[0], p.keys[0])
	}
}

type busyAgent struct{ busy bool }

func (b *busyAgent) Capability() (string, string)              { return "schemas=1;state=on", "" }
func (b *busyAgent) Busy() bool                                { return b.busy }
func (b *busyAgent) Handle(*betik.JobList)                     {}
func (b *busyAgent) SetPanelSecure(bool)                       {}
func (b *busyAgent) Run(ctx context.Context, _ betik.SendFunc) { <-ctx.Done() }

// Betik çalışırken güncelleme uygulanmaz.
func TestUpdateWaitsForScript(t *testing.T) {
	p := newUpdatePanel(t)
	exe := filepath.Join(t.TempDir(), "uptime")
	writeExe(t, exe)
	p.binary = []byte("yeni")
	p.offer = signedOffer(t, "1.3.1", p.binary)
	s := &busyAgent{busy: true}
	c := &Client{cfg: Config{Scripts: s}, updateRefused: map[string]time.Time{}}
	c.tryUpdate(context.Background(), *p.offer)
	if st := p.statuses(); len(st) != 0 {
		t.Fatalf("betik sürerken güncelleme başladı: %v", st)
	}
}

func writeExe(t *testing.T, p string) {
	t.Helper()
	if err := os.WriteFile(p, []byte("eski"), 0o755); err != nil {
		t.Fatal(err)
	}
}
