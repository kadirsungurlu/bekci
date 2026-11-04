//go:build linux

package metrics

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// İlk okuma başlangıç noktasıdır: ajan açılmadan önceki öldürmeler bildirilmez.
func TestOOMWatchCounter(t *testing.T) {
	dir := t.TempDir()
	write := func(n string) {
		if err := os.WriteFile(filepath.Join(dir, "vmstat"), []byte("oom_kill "+n+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("3")
	w := &oomWatch{vmstat: filepath.Join(dir, "vmstat"), fd: -1}
	now := time.Unix(1000, 0)
	if got := w.poll(now); len(got) != 0 {
		t.Fatalf("başlangıç: %+v", got)
	}
	if got := w.poll(now); len(got) != 0 {
		t.Fatalf("artış yok: %+v", got)
	}
	write("5")
	if got := w.poll(now); len(got) != 1 || got[0].Count != 2 || got[0].Subject != "" {
		t.Fatalf("%+v", got)
	}
}
