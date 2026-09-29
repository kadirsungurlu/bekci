package probe

import (
	"net/http"
	"testing"
	"time"
)

// Ajan iş listesi sürümünü ?since= ile geri gönderir; sürüm döndürmeyen (eski)
// sunucuda since hep 0 kalır.
func TestJobsSince(t *testing.T) {
	f := newFakeServer(t)
	f.set(func(f *fakeServer) { f.version = 42 })
	_, stop := start(t, Config{Server: f.srv.URL})
	waitFor(t, "iki iş listesi isteği", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.sinces) >= 2
	})
	stop()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sinces[0] != "0" || f.sinces[1] != "42" {
		t.Fatalf("since değerleri %v; [0 42 …] bekleniyordu", f.sinces)
	}

	old := newFakeServer(t)
	_, stop2 := start(t, Config{Server: old.srv.URL})
	waitFor(t, "iki iş listesi isteği", func() bool {
		old.mu.Lock()
		defer old.mu.Unlock()
		return len(old.sinces) >= 2
	})
	stop2()
	old.mu.Lock()
	defer old.mu.Unlock()
	for _, s := range old.sinces {
		if s != "0" {
			t.Fatalf("eski sunucuda since 0 kalmalı: %v", old.sinces)
		}
	}
}

// İş listesi isteğinin zaman aşımı uzun yoklamanın bekletmesinden (20 sn) epey
// uzun; diğer istekler genel 30 sn'yi korur.
func TestJobsTimeout(t *testing.T) {
	c, err := New(Config{Server: "https://uptime.example", Token: "upr_x"})
	if err != nil {
		t.Fatal(err)
	}
	if c.jobsHTTP.Timeout != jobsTimeout || c.cfg.HTTPClient.Timeout != 30*time.Second {
		t.Fatalf("zaman aşımları: iş %v, genel %v", c.jobsHTTP.Timeout, c.cfg.HTTPClient.Timeout)
	}
	if c.jobsHTTP.CheckRedirect == nil {
		t.Fatal("yönlendirme koruması iş listesi isteğinde de olmalı")
	}
	own := &http.Client{} // zaman aşımı yok: olduğu gibi kullanılır
	c, _ = New(Config{Server: "https://uptime.example", Token: "upr_x", HTTPClient: own})
	if c.jobsHTTP != own {
		t.Fatal("zaman aşımsız istemci değiştirilmemeli")
	}
}

// Sonradan eklenen iş ~1 birim içinde, ilk listedeki işler min(aralık, 10)
// içine yayılarak başlar.
func TestFirstDelay(t *testing.T) {
	c, err := New(Config{Server: "https://uptime.example", Token: "upr_x"})
	if err != nil {
		t.Fatal(err)
	}
	j := Job{Interval: 300}
	var maxQuick, maxSpread time.Duration
	for range 2000 {
		maxQuick = max(maxQuick, c.firstDelay(j, true))
		maxSpread = max(maxSpread, c.firstDelay(j, false))
	}
	if maxQuick > time.Second {
		t.Fatalf("yeni iş en geç 1 sn içinde başlamalı: %v", maxQuick)
	}
	if maxSpread <= time.Second || maxSpread > 10*time.Second {
		t.Fatalf("ilk liste 10 sn içine yayılmalı: %v", maxSpread)
	}
	if d := c.firstDelay(Job{Interval: 2}, false); d > 2*time.Second {
		t.Fatalf("kısa aralıkta kaydırma aralığı aşmamalı: %v", d)
	}
}

// Sonradan eklenen işin ilk sonucu gönderim aralığı (FlushEvery) beklenmeden gider.
func TestNewJobFirstResultFlushedEarly(t *testing.T) {
	f := newFakeServer(t)
	s, _ := site(t)
	c, _ := start(t, Config{Server: f.srv.URL, FlushEvery: time.Hour})
	waitFor(t, "ilk iş listesi", func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.sinces) >= 1
	})
	f.set(func(f *fakeServer) { f.jobs = []Job{httpJob(7, s.URL, 60000)} })
	waitFor(t, "yeni iş", func() bool { return len(c.Jobs()) == 1 })
	waitFor(t, "ilk sonuç gönderimi", func() bool { n, _, _ := f.received(); return n >= 1 })
}
