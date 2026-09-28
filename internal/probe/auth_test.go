package probe

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// 403 (devre dışı / IP kilidi) kontrolleri durdurur ama bekleyen sonuçları
// atmaz; erişim geri gelince gönderilir.
func TestForbiddenKeepsBuffer(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 5)} })
	c, _ := start(t, Config{Server: fs.srv.URL})
	waitFor(t, "iş başladı", func() bool { return len(c.Jobs()) == 1 })
	fs.set(func(f *fakeServer) { f.resCode = 503 })
	waitFor(t, "sonuç birikti", func() bool { return c.pending() > 0 })
	fs.set(func(f *fakeServer) { f.jobsCode = 403; f.resCode = 403 })
	waitFor(t, "iş durdu", func() bool { return len(c.Jobs()) == 0 })
	time.Sleep(100 * time.Millisecond) // birkaç yoklama/gönderim turu
	n := c.pending()
	if n == 0 {
		t.Fatal("403'te tampon boşaltılmamalı")
	}
	_, before, _ := fs.received()
	fs.set(func(f *fakeServer) { f.jobsCode = 200; f.resCode = 200 })
	waitFor(t, "saklanan sonuçlar gönderildi", func() bool { _, got, _ := fs.received(); return got-before >= n })
}

// 401 (geçersiz token) bekleyen sonuçları atar: hiçbir zaman kabul edilmezler.
func TestUnauthorizedClearsBuffer(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 5)}; f.resCode = 503 })
	c, _ := start(t, Config{Server: fs.srv.URL, MaxBackoff: time.Second})
	waitFor(t, "sonuç birikti", func() bool { return c.pending() > 0 })
	fs.set(func(f *fakeServer) { f.jobsCode = 401 })
	waitFor(t, "tampon boşaldı ve iş durdu", func() bool { return len(c.Jobs()) == 0 && c.pending() == 0 })
}

// Retry-After en fazla MaxBackoff kadar dikkate alınır: sunucu çok uzun bir
// süre istese de gönderim MaxBackoff sonra yeniden denenir.
func TestRetryAfterCapped(t *testing.T) {
	fs := newFakeServer(t)
	target, _ := site(t)
	fs.set(func(f *fakeServer) { f.jobs = []Job{httpJob(1, target.URL, 5)}; f.resCode = 429; f.retry = "3600" })
	c, _ := start(t, Config{Server: fs.srv.URL, MaxBackoff: 50 * time.Millisecond})
	waitFor(t, "sonuç birikti", func() bool { return c.pending() > 0 })
	time.Sleep(30 * time.Millisecond) // 429 + Retry-After: 3600 alındı
	fs.set(func(f *fakeServer) { f.resCode = 200 })
	waitFor(t, "MaxBackoff sonra gönderildi", func() bool { b, _, _ := fs.received(); return b > 0 })

	cl := &Client{cfg: Config{MaxBackoff: 60 * time.Second}}
	for v, want := range map[string]time.Duration{"": 0, "x": 0, "-5": 0, "7": 7 * time.Second, "60": time.Minute, "86400": time.Minute} {
		if got := cl.retryAfter(http.Header{"Retry-After": []string{v}}); got != want {
			t.Errorf("Retry-After %q → %v, %v bekleniyordu", v, got, want)
		}
	}
}

// Ana sunucunun yönlendirmesi izlenmez: token başka bir adrese (ör. http)
// tekrar gönderilmez.
func TestNoRedirectFollow(t *testing.T) {
	var leaked atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Add(1)
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(other.Close)
	var hits atomic.Int64
	main := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(main.Close)
	c, _ := start(t, Config{Server: main.URL})
	waitFor(t, "birkaç yoklama", func() bool { return hits.Load() >= 3 })
	if n := leaked.Load(); n != 0 {
		t.Fatalf("yönlendirme izlendi, token %d kez başka adrese gitti", n)
	}
	if c.cfg.HTTPClient.CheckRedirect == nil {
		t.Fatal("varsayılan istemci yönlendirmeyi engellemeli")
	}
}
