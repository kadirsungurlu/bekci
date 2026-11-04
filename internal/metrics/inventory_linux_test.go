//go:build linux

package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSystemctl(t *testing.T) {
	units := "nginx.service loaded active running A high performance web server\n" +
		"● eski.service not-found inactive dead eski.service\n" +
		"yedek.service loaded failed failed Yedek\n"
	files := "nginx.service enabled enabled\nyedek.service disabled enabled\n"
	got := parseSystemctl(units, files)
	if len(got) != 2 || got[0].Name != "nginx" || got[0].Enabled != "enabled" || got[0].Description != "A high performance web server" ||
		got[1].Active != "failed" || got[1].Enabled != "disabled" {
		t.Fatalf("%+v", got)
	}
}

func TestCgroupServices(t *testing.T) {
	sys, etc := t.TempDir(), t.TempDir()
	for _, d := range []string{"fs/cgroup/system.slice/nginx.service", "fs/cgroup/system.slice/cron.service", "fs/cgroup/system.slice/docker-1.scope"} {
		if err := os.MkdirAll(filepath.Join(sys, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	wants := filepath.Join(etc, "multi-user.target.wants")
	os.MkdirAll(wants, 0o755)
	os.Symlink("/lib/x", filepath.Join(wants, "nginx.service"))
	got := cgroupServices(sys, etc)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	for _, s := range got {
		if s.Active != "active" || (s.Name == "nginx") != (s.Enabled == "enabled") {
			t.Fatalf("%+v", s)
		}
	}
}

func TestParseProcNet(t *testing.T) {
	tcp := `  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode
   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 111 1 0 100 0 0 10 0
   1: 00000000:0050 00000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 222 1 0 100 0 0 10 0
   2: 0100007F:C350 0100007F:0050 01 00000000:00000000 00:00000000 00000000     0        0 333 1 0 100 0 0 10 0
`
	got := parseProcNet(tcp, "tcp")
	if len(got) != 2 || got[0].addr != "127.0.0.1" || got[0].port != 8080 || got[1].addr != "0.0.0.0" || got[1].port != 80 || got[1].inode != "222" {
		t.Fatalf("tcp: %+v", got)
	}
	tcp6 := "   0: 00000000000000000000000000000000:01BB 00000000000000000000000000000000:0000 0A 0:0 0:0 0 0 0 444 1 0\n"
	if g := parseProcNet(tcp6, "tcp"); len(g) != 1 || g[0].addr != "::" || g[0].port != 443 {
		t.Fatalf("tcp6: %+v", g)
	}
	udp := "   0: 00000000:0035 00000000:0000 07 0:0 0:0 0 0 0 555 2 0\n   1: 00000000:D431 00000000:0000 07 0:0 0:0 0 0 0 556 2 0\n"
	if g := parseProcNet(udp, "udp"); len(g) != 1 || g[0].port != 53 {
		t.Fatalf("udp (geçici port atlanmalı): %+v", g)
	}
}
