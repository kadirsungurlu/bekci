package metrics

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Çekirdek günlüğündeki OOM satırı (genel ve cgroup sınırı için aynı son ek):
//
//	Out of memory: Killed process 1234 (java) total-vm:...
//	Memory cgroup out of memory: Killed process 1234 (java) total-vm:...
var oomLine = regexp.MustCompile(`Killed process (\d+) \(([^)]*)\)`)

type oomProc struct {
	pid  int
	name string
}

// parseOOMLine /dev/kmsg kaydından öldürülen süreci çıkarır
// ("öncelik,sıra,zaman,bayrak;ileti").
func parseOOMLine(rec string) (oomProc, bool) {
	if i := strings.IndexByte(rec, ';'); i >= 0 {
		rec = rec[i+1:]
	}
	m := oomLine.FindStringSubmatch(rec)
	if m == nil {
		return oomProc{}, false
	}
	pid, _ := strconv.Atoi(m[1])
	return oomProc{pid: pid, name: m[2]}, true
}

// parseOOMCount /proc/vmstat içindeki oom_kill sayacı (çekirdek >= 4.13; bütün
// sistem için kümülatif, konteyner sınırındaki öldürmeler dahil).
func parseOOMCount(vmstat string) (uint64, bool) {
	for line := range strings.Lines(vmstat) {
		if v, ok := strings.CutPrefix(line, "oom_kill "); ok {
			n, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
			return n, err == nil
		}
	}
	return 0, false
}

// oomEvents günlükte adı okunan süreçleri ve sayaçtaki artışı birleştirir:
// aynı ad tek olayda toplanır; sayaç adlardan fazla arttıysa (ör. /dev/kmsg
// okunamıyor) kalanı adsız olaydır.
func oomEvents(procs []oomProc, delta uint64, now time.Time) []HostEvent {
	var out []HostEvent
	for _, p := range procs {
		if i := slices.IndexFunc(out, func(e HostEvent) bool { return e.Subject == p.name }); i >= 0 {
			out[i].Count++
			continue
		}
		out = append(out, HostEvent{Time: now.Unix(), Kind: HostEventOOM, Subject: p.name, PID: p.pid, Count: 1})
	}
	if delta > uint64(len(procs)) {
		out = append(out, HostEvent{Time: now.Unix(), Kind: HostEventOOM, Count: int(min(delta-uint64(len(procs)), 1<<20))})
	}
	return out
}
