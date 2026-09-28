//go:build linux

package metrics

import (
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestUsageTimeout: bir bağlamanın statfs'i askıda kalırsa (ölü ağ diski gibi)
// usage diskTimeout içinde "bilinmiyor" döner ve diğer diskler etkilenmez;
// selectDisks tüm toplama boyunca süresiz bloklanmaz.
func TestUsageTimeout(t *testing.T) {
	const timeout = 100 * time.Millisecond
	released := make(chan struct{})
	t.Cleanup(func() { close(released) }) // askıdaki goroutine sızmasın

	s := &systemSource{
		root:        "", // stat atlanır (yalnızca statfs sınanır)
		diskTimeout: timeout,
		stat:        syscall.Stat,
		statfs: func(p string, fs *syscall.Statfs_t) error {
			if p == "/hang" {
				<-released // ölü bağlama: uzun süre bloklar
				return nil
			}
			fs.Bsize = 1
			fs.Blocks = 100
			fs.Bfree = 40
			fs.Bavail = 40
			return nil
		},
	}

	ms := []mount{
		{Dev: "8:1", Root: "/", Point: "/", FS: "ext4", Source: "/dev/sda1"},
		{Dev: "0:99", Root: "/", Point: "/hang", FS: "ext4", Source: "//nas/share"},
	}

	start := time.Now()
	got := selectDisks(ms, s.usage)
	elapsed := time.Since(start)

	// Askıdaki disk için en fazla timeout kadar beklenir; sağlam disk anında döner.
	if elapsed > time.Second {
		t.Fatalf("askıdaki disk toplamayı bloklamış olmamalı: %v geçti", elapsed)
	}
	if len(got) != 1 || got[0].Mount != "/" {
		t.Fatalf("yalnızca sağlam disk (/) dönmeliydi: %+v", got)
	}
	if got[0].Total != 100 || got[0].Used != 60 {
		t.Fatalf("/ doluluğu yanlış: %+v", got[0])
	}
}

// TestUsageOK: sağlam bir statfs ile usage doğru değerleri döndürür (zaman aşımı
// yolunun mutlu senaryoyu bozmadığını doğrular).
func TestUsageOK(t *testing.T) {
	s := &systemSource{
		root:        "",
		diskTimeout: 2 * time.Second,
		stat:        syscall.Stat,
		statfs: func(p string, fs *syscall.Statfs_t) error {
			fs.Bsize = 4096
			fs.Blocks = 100
			fs.Bfree = 30
			fs.Bavail = 20
			return nil
		},
	}
	total, used, ok := s.usage(mount{Point: "/data", FS: "ext4", Dev: "8:1"})
	if !ok || used != 70*4096 || total != 70*4096+20*4096 {
		t.Fatalf("usage: total=%d used=%d ok=%v", total, used, ok)
	}
}

// TestUsageHungMountBounded: askıda kalan bir bağlamanın statfs'i dönmedikçe
// sonraki toplamalar o bağlama için yeni goroutine açmaz (bilinmiyor sayar);
// goroutine sayısı toplama sayısıyla büyümez. Çağrı dönünce bağlama yeniden
// ölçülür.
func TestUsageHungMountBounded(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	rel := func() { once.Do(func() { close(release) }) }
	t.Cleanup(rel)
	var hungCalls atomic.Int32
	s := &systemSource{
		diskTimeout: 5 * time.Millisecond,
		stat:        syscall.Stat,
		statfs: func(p string, fs *syscall.Statfs_t) error {
			if p == "/hang" {
				hungCalls.Add(1)
				<-release
			}
			fs.Bsize, fs.Blocks, fs.Bfree, fs.Bavail = 1, 100, 40, 40
			return nil
		},
	}
	ms := []mount{
		{Dev: "8:1", Root: "/", Point: "/", FS: "ext4", Source: "/dev/sda1"},
		{Dev: "8:2", Root: "/", Point: "/hang", FS: "ext4", Source: "/dev/sdb1"},
	}
	selectDisks(ms, s.usage) // ilk toplama: askıdaki sorgu başlar
	base := runtime.NumGoroutine()
	for range 50 {
		if got := selectDisks(ms, s.usage); len(got) != 1 || got[0].Mount != "/" {
			t.Fatalf("yalnızca / dönmeli: %+v", got)
		}
	}
	if n := hungCalls.Load(); n != 1 {
		t.Fatalf("askıdaki bağlama %d kez sorgulandı, 1 bekleniyordu", n)
	}
	if g := runtime.NumGoroutine(); g > base+2 {
		t.Fatalf("goroutine birikiyor: %d → %d", base, g)
	}
	// Askıdaki çağrı dönünce bağlama yeniden ölçülür.
	rel()
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		busy := len(s.inflight)
		s.mu.Unlock()
		if busy == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("askıdaki sorgu bitince bağlama serbest kalmadı")
		}
		time.Sleep(time.Millisecond)
	}
	if got := selectDisks(ms, s.usage); len(got) != 2 || hungCalls.Load() != 2 {
		t.Fatalf("dönen bağlama yeniden ölçülmeli: %+v (%d çağrı)", got, hungCalls.Load())
	}
}

// TestSystemSourceNetFilterHostPaths: ağ süzgeci HOST_SYS ve HOST_PROC
// öneklerini kullanır (konteynerde host'un sysfs/procfs'i).
func TestSystemSourceNetFilterHostPaths(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("sys/class/net/eth0")
	mk("sys/class/net/vmbr0/bridge")
	mk("proc/1/net/vlan")
	mk("proc/1/net/vlan/vlan7")
	env := map[string]string{"HOST_SYS": filepath.Join(root, "sys") + "/", "HOST_PROC": filepath.Join(root, "proc"), "HOST_ROOT": root}
	src, _ := newSystemSource(func(k string) string { return env[k] })
	f, ok := src.(ioFilter)
	if !ok {
		t.Fatal("Linux kaynağı ioFilter olmalı")
	}
	for n, want := range map[string]bool{"eth0": true, "vmbr0": false, "vlan7": false, "eth0.7": false, "tap100i0": false, "lo": false} {
		if f.keepNet(n) != want {
			t.Errorf("%s → %v", n, !want)
		}
	}
	if !f.keepDisk("sda") || f.keepDisk("sda1") {
		t.Error("disk süzgeci")
	}
}
