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
