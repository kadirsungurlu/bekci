//go:build linux

package metrics

import (
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// oomWatch OOM öldürmelerini izler. Sayaç (HOST_PROC/vmstat) her yerde okunur
// ve kaçırmaz; süreç adı /dev/kmsg'den gelir, ayrıcalıksız konteyner ajanında
// bu aygıt kapalıdır ve olay adsız yazılır. Ajan başladığında önceki öldürmeler
// sayılmaz (başlangıç noktası ilk okumadır).
type oomWatch struct {
	vmstat string
	fd     int
	last   uint64
	ready  bool
}

func newOOMWatch(proc string) *oomWatch {
	w := &oomWatch{vmstat: proc + "/vmstat", fd: -1}
	fd, err := unix.Open("/dev/kmsg", unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return w
	}
	if _, err := unix.Seek(fd, 0, unix.SEEK_END); err != nil {
		unix.Close(fd)
		return w
	}
	w.fd = fd
	return w
}

// readLog son okumadan beri yazılan kayıtlardaki öldürmeler. Halka tampon
// taşarsa EPIPE gelir, okuma kaldığı yerden sürer.
func (w *oomWatch) readLog() []oomProc {
	if w.fd < 0 {
		return nil
	}
	var out []oomProc
	buf := make([]byte, 8192)
	for range 4096 {
		n, err := unix.Read(w.fd, buf)
		if errors.Is(err, syscall.EPIPE) {
			continue
		}
		if err != nil || n <= 0 {
			break
		}
		if p, ok := parseOOMLine(string(buf[:n])); ok && len(out) < MaxEvents {
			out = append(out, p)
		}
	}
	return out
}

func (w *oomWatch) poll(now time.Time) []HostEvent {
	procs := w.readLog()
	var delta uint64
	if b, err := os.ReadFile(w.vmstat); err == nil {
		if n, ok := parseOOMCount(string(b)); ok {
			if w.ready && n > w.last {
				delta = n - w.last
			}
			w.last, w.ready = n, true
		}
	}
	return oomEvents(procs, delta, now)
}

func (s *systemSource) hostEvents(now time.Time) []HostEvent {
	s.oomOnce.Do(func() { s.oom = newOOMWatch(s.proc) })
	return s.oom.poll(now)
}
