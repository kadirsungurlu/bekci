package metrics

import (
	"testing"
	"time"
)

func TestParseOOMLine(t *testing.T) {
	cases := []struct {
		rec  string
		pid  int
		name string
		ok   bool
	}{
		{"3,1234,99,-;Out of memory: Killed process 4242 (java) total-vm:100kB, anon-rss:10kB", 4242, "java", true},
		{"3,1235,99,-;Memory cgroup out of memory: Killed process 7 (python3) total-vm:1kB", 7, "python3", true},
		{"6,5,1,-;oom-kill:constraint=CONSTRAINT_NONE", 0, "", false},
		{"6,5,1,-;usb 1-1: new device", 0, "", false},
	}
	for _, c := range cases {
		p, ok := parseOOMLine(c.rec)
		if ok != c.ok || p.pid != c.pid || p.name != c.name {
			t.Errorf("%q: %+v %v", c.rec, p, ok)
		}
	}
}

func TestParseOOMCount(t *testing.T) {
	if n, ok := parseOOMCount("pgfault 1\noom_kill 12\nthp 3\n"); !ok || n != 12 {
		t.Fatalf("%d %v", n, ok)
	}
	if _, ok := parseOOMCount("pgfault 1\n"); ok {
		t.Fatal("eski çekirdekte sayaç yok")
	}
}

func TestOOMEvents(t *testing.T) {
	now := time.Unix(1000, 0)
	got := oomEvents([]oomProc{{1, "a"}, {2, "a"}, {3, "b"}}, 5, now)
	if len(got) != 3 || got[0].Subject != "a" || got[0].Count != 2 || got[1].Subject != "b" || got[2].Subject != "" || got[2].Count != 2 {
		t.Fatalf("%+v", got)
	}
	// Sayaç adlardan az artmışsa (farklı anlarda okundu) adsız olay çıkmaz.
	if got := oomEvents([]oomProc{{1, "a"}}, 0, now); len(got) != 1 {
		t.Fatalf("%+v", got)
	}
	if got := oomEvents(nil, 0, now); len(got) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestSanitizeEvents(t *testing.T) {
	s := Sample{Events: make([]HostEvent, MaxEvents+5)}
	s.Sanitize()
	if len(s.Events) != MaxEvents || s.Events[0].Count != 1 {
		t.Fatalf("%d %+v", len(s.Events), s.Events[0])
	}
}
