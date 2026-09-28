package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDocker Docker Engine API'sini bir unix soketinde taklit eder.
type fakeDocker struct {
	mu    sync.Mutex
	list  []dockerListItem
	stats map[string]map[string]any // id → stats yanıtı
	fail  bool
	calls int
	sock  string
}

func newFakeDocker(t *testing.T) *fakeDocker {
	t.Helper()
	dir, err := os.MkdirTemp("", "dk") // kısa yol: unix soket yolu 108 karakterle sınırlı
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	f := &fakeDocker{stats: map[string]map[string]any{}, sock: filepath.Join(dir, "d.sock")}
	ln, err := net.Listen("unix", f.sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls++
		if f.fail {
			w.WriteHeader(500)
			return
		}
		switch {
		case r.URL.Path == "/containers/json":
			json.NewEncoder(w).Encode(f.list)
		case strings.HasSuffix(r.URL.Path, "/stats"):
			if r.URL.Query().Get("stream") != "false" || r.URL.Query().Get("one-shot") != "true" {
				w.WriteHeader(400)
				return
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/stats")
			s, ok := f.stats[id]
			if !ok {
				w.WriteHeader(404)
				return
			}
			json.NewEncoder(w).Encode(s)
		default:
			w.WriteHeader(404)
		}
	}))
	srv.Listener = ln
	srv.Start()
	t.Cleanup(srv.Close)
	return f
}

func statsJSON(read time.Time, cpuNs uint64, usage, inactive, limit uint64, rx, tx uint64) map[string]any {
	return map[string]any{
		"read":         read.Format(time.RFC3339Nano),
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": cpuNs}},
		"memory_stats": map[string]any{"usage": usage, "limit": limit, "stats": map[string]any{"inactive_file": inactive}},
		"networks":     map[string]any{"eth0": map[string]any{"rx_bytes": rx, "tx_bytes": tx}, "eth1": map[string]any{"rx_bytes": 0, "tx_bytes": 1}},
	}
}

func TestContainers(t *testing.T) {
	fd := newFakeDocker(t)
	clk := newClock()
	t0 := clk.t
	idA, idB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	fd.list = []dockerListItem{{ID: idB, Names: []string{"/web"}}, {ID: idA, Names: []string{"/db"}}}
	fd.stats[idA] = statsJSON(t0, 1_000_000_000, 500, 100, 1<<40, 1000, 2000)
	fd.stats[idB] = statsJSON(t0, 5_000_000_000, 300, 400, 256, 0, 0)

	o := testOpts(clk)
	o.DockerSocket = fd.sock
	src := &fakeSource{host: Host{Threads: 4, MemTotal: 1 << 30}, mem: memStat{Total: 1 << 30}}
	c := newCollector(o, src, "")
	if _, ok := c.Collect(context.Background()); ok {
		t.Fatal("ilk örnek hazırlık olmalı")
	}

	// 10 sn sonra: db 10 sn'de 20 sn CPU kullandı → 4 çekirdekte %50.
	clk.add(10 * time.Second)
	fd.mu.Lock()
	fd.stats[idA] = statsJSON(t0.Add(10*time.Second), 21_000_000_000, 500, 100, 1<<40, 11_000, 2500)
	fd.stats[idB] = statsJSON(t0.Add(5*time.Second), 6_000_000_000, 300, 400, 256, 0, 0) // web: ölçüm 5 sn sonra, 1 sn CPU → %5
	idC := strings.Repeat("c", 64)
	fd.list = append(fd.list, dockerListItem{ID: idC, Names: []string{"/yeni"}})
	fd.stats[idC] = statsJSON(t0.Add(10*time.Second), 9_000_000_000, 50, 0, 0, 500, 500)
	fd.mu.Unlock()

	s, ok := c.Collect(context.Background())
	if !ok || !s.Host.Docker {
		t.Fatalf("docker erişilebilir olmalı: %+v", s.Host)
	}
	cs := s.Stats.Containers
	if len(cs) != 3 || cs[0].Name != "db" || cs[1].Name != "web" || cs[2].Name != "yeni" {
		t.Fatalf("ada göre sıralı üç konteyner bekleniyordu: %+v", cs)
	}
	db, web, yeni := cs[0], cs[1], cs[2]
	if db.ID != strings.Repeat("a", 12) || db.CPU != 50 || db.Mem != 400 || db.MemLimit != 0 || db.NetRxBps != 1000 || db.NetTxBps != 50 {
		t.Errorf("db %+v", db)
	}
	if web.CPU != 5 || web.Mem != 300 || web.MemLimit != 256 {
		t.Errorf("web %+v (inactive_file > usage ise usage kullanılır)", web)
	}
	if yeni.CPU != 0 || yeni.NetRxBps != 0 || yeni.Mem != 50 {
		t.Errorf("yeni konteynerin ilk örneğinde fark yok: %+v", yeni)
	}

	// Docker'a ulaşılamazsa konteyner yok, Docker=false; sonra geri gelirse
	// sayaçlar yeniden hazırlanır (arada kalan süre CPU'ya yazılmaz).
	fd.mu.Lock()
	fd.fail = true
	fd.mu.Unlock()
	clk.add(10 * time.Second)
	s, _ = c.Collect(context.Background())
	if s.Host.Docker || len(s.Stats.Containers) != 0 {
		t.Fatalf("docker hatası: %+v", s.Stats.Containers)
	}
	fd.mu.Lock()
	fd.fail = false
	fd.mu.Unlock()
	clk.add(10 * time.Second)
	s, _ = c.Collect(context.Background())
	if !s.Host.Docker || len(s.Stats.Containers) != 3 || s.Stats.Containers[0].CPU != 0 {
		t.Fatalf("geri geldi: %+v", s.Stats.Containers)
	}
}

func TestContainersNoSocket(t *testing.T) {
	clk := newClock()
	o := testOpts(clk)
	o.DockerSocket = filepath.Join(t.TempDir(), "yok.sock")
	c := newCollector(o, &fakeSource{}, "")
	c.Collect(context.Background())
	clk.add(time.Second)
	s, _ := c.Collect(context.Background())
	if s.Host.Docker || s.Stats.Containers != nil {
		t.Fatal("soket yokken Docker kapalı sayılmalı")
	}
}

// Yavaş Docker: toplam süre sınırı aşılınca Collect yine döner.
func TestContainersTimeout(t *testing.T) {
	dir, _ := os.MkdirTemp("", "dk")
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "d.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	block := make(chan struct{})
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/containers/json":
			fmt.Fprint(w, `[{"Id":"x1","Names":["/a"]},{"Id":"x2","Names":["/b"]}]`)
			return
		case "/containers/x2/stats":
			fmt.Fprint(w, `{"cpu_stats":{"cpu_usage":{"total_usage":1000}},"memory_stats":{"usage":4096}}`)
			return
		}
		<-block
	}))
	srv.Listener = ln
	srv.Start()
	defer srv.Close()
	defer close(block)

	clk := newClock()
	o := testOpts(clk)
	o.DockerSocket, o.DockerTimeout = sock, 100*time.Millisecond
	c := newCollector(o, &fakeSource{host: Host{Threads: 1}}, "")
	start := time.Now()
	c.Collect(context.Background())
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("zaman aşımı uygulanmadı: %v", el)
	}
	clk.add(time.Second)
	s, _ := c.Collect(context.Background())
	if !s.Host.Docker || len(s.Stats.Containers) != 1 || s.Stats.Containers[0].Name != "b" || s.Stats.Containers[0].Mem != 4096 {
		t.Fatalf("istatistiği okunamayan konteyner sıfır değerlerle gönderilmemeli, okunan gönderilmeli: %+v", s.Stats.Containers)
	}
}

func TestContainerMath(t *testing.T) {
	sec := time.Second
	for _, c := range []struct {
		prev, cur uint64
		el        time.Duration
		threads   int
		want      float64
	}{
		{0, 1e9, sec, 1, 100},
		{0, 1e9, sec, 4, 25},
		{0, 3e9, 2 * sec, 8, 18.75},
		{5e9, 1e9, sec, 4, 0}, // yeniden başladı
		{0, 1e9, 0, 4, 0},
		{0, 1e9, sec, 0, 0},
		{0, 9e9, sec, 1, 100}, // sınır
	} {
		if got := containerCPU(c.prev, c.cur, c.el, c.threads); got != c.want {
			t.Errorf("%+v → %v", c, got)
		}
	}
	if m := containerMem(1000, map[string]uint64{"total_inactive_file": 300, "inactive_file": 100}); m != 700 {
		t.Errorf("cgroup v1: %d", m)
	}
	if m := containerMem(1000, map[string]uint64{"inactive_file": 100}); m != 900 {
		t.Errorf("cgroup v2: %d", m)
	}
	if m := containerMem(1000, nil); m != 1000 {
		t.Errorf("istatistiksiz: %d", m)
	}
}
