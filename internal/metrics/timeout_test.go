package metrics

import (
	"testing"
	"time"
)

func TestCallWithTimeout(t *testing.T) {
	// Süresinde dönen çağrı: sonuç ve timedOut=false.
	v, timedOut := callWithTimeout(time.Second, func() int { return 42 })
	if timedOut || v != 42 {
		t.Fatalf("hızlı çağrı: v=%d timedOut=%v", v, timedOut)
	}

	// Süreyi aşan çağrı: sıfır değer ve timedOut=true; asıl fn arka planda biter.
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	v, timedOut = callWithTimeout(20*time.Millisecond, func() int {
		<-release
		return 7
	})
	if !timedOut || v != 0 {
		t.Fatalf("yavaş çağrı: v=%d timedOut=%v", v, timedOut)
	}
}
