package check

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// assertNoGoroutineLeak f'yi n kez çalıştırır; goroutine sayısı kalıcı olarak
// artmamalı (her kontrolden boşta kalan bağlantı okuma/yazma goroutine'leri
// bırakırsa sayı n ile orantılı büyür).
func assertNoGoroutineLeak(t *testing.T, n int, f func()) {
	t.Helper()
	f() // ısınma: test sunucusunun ve paketin tek seferlik goroutine'leri
	time.Sleep(50 * time.Millisecond)
	base := runtime.NumGoroutine()
	for range n {
		f()
	}
	const slack = 5
	got := runtime.NumGoroutine()
	for deadline := time.Now().Add(3 * time.Second); got > base+slack && time.Now().Before(deadline); {
		time.Sleep(20 * time.Millisecond)
		got = runtime.NumGoroutine()
	}
	if got > base+slack {
		t.Fatalf("goroutine sızıntısı: %d kontrolden sonra %d → %d", n, base, got)
	}
}

// Bulgu: Docker kontrolü her seferinde yeni bir keep-alive Transport kuruyor,
// boşta kalan bağlantılar hiç kapanmıyordu (50 kontrol → +150 goroutine).
func TestDockerNoConnectionLeak(t *testing.T) {
	srv := fakeDockerServer(t)
	assertNoGoroutineLeak(t, 50, func() {
		if r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "saglikli"}); !r.Up {
			t.Fatalf("kontrol başarısız: %+v", r)
		}
	})
}

// Bulgu: WebSocket el sıkışması başarısız olunca (upgrade yok) bağlantı
// kontrole özel Transport'un havuzunda boşta kalıyordu.
func TestWebSocketNoConnectionLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	assertNoGoroutineLeak(t, 50, func() {
		if r := run(t, "websocket", map[string]any{"url": wsURL}); r.Up {
			t.Fatalf("404 veren sunucu DOWN olmalı: %+v", r)
		}
	})
}

// fakeMQTTClient bağlanması testçe serbest bırakılan sahte istemci.
type fakeMQTTClient struct {
	mqtt.Client // kullanılmayan yöntemler
	release     chan error
	disconnects atomic.Int32
}

func (c *fakeMQTTClient) Connect() mqtt.Token {
	tok := &fakeToken{done: make(chan struct{})}
	go func() {
		tok.err = <-c.release
		close(tok.done)
	}()
	return tok
}

func (c *fakeMQTTClient) Disconnect(uint) { c.disconnects.Add(1) }

type fakeToken struct {
	done chan struct{}
	err  error
}

func (t *fakeToken) Wait() bool { <-t.done; return true }
func (t *fakeToken) WaitTimeout(d time.Duration) bool {
	select {
	case <-t.done:
		return true
	case <-time.After(d):
		return false
	}
}
func (t *fakeToken) Done() <-chan struct{} { return t.done }
func (t *fakeToken) Error() error          { return t.err }

// Bulgu: bağlantı zaman aşımından hemen sonra kurulursa istemci hiç
// kapatılmıyordu.
func TestMQTTConnectTimeoutClosesLateConnection(t *testing.T) {
	c := &fakeMQTTClient{release: make(chan error)}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	timedOut, err := mqttConnect(ctx, c)
	if !timedOut || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("zaman aşımı bekleniyordu: %v %v", timedOut, err)
	}
	c.release <- nil // bağlantı geç de olsa kuruldu
	deadline := time.Now().Add(2 * time.Second)
	for c.disconnects.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c.disconnects.Load() != 1 {
		t.Fatal("geç kurulan bağlantı kapatılmalı")
	}

	// Geç gelen bağlantı hatasında kapatılacak bir şey yok.
	c2 := &fakeMQTTClient{release: make(chan error)}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel2()
	if timedOut, _ := mqttConnect(ctx2, c2); !timedOut {
		t.Fatal("zaman aşımı bekleniyordu")
	}
	c2.release <- errors.New("reddedildi")
	time.Sleep(20 * time.Millisecond)
	if c2.disconnects.Load() != 0 {
		t.Fatal("başarısız bağlantıda Disconnect çağrılmamalı")
	}

	// Zamanında kurulan bağlantı: hata yok, zaman aşımı yok.
	c3 := &fakeMQTTClient{release: make(chan error, 1)}
	c3.release <- nil
	if timedOut, err := mqttConnect(context.Background(), c3); timedOut || err != nil {
		t.Fatalf("başarılı bağlantı: %v %v", timedOut, err)
	}
}
