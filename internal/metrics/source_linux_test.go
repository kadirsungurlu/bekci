//go:build linux

package metrics

import (
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
