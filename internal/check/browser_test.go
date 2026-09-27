package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrowserNormalize(t *testing.T) {
	c, _ := Get("browser")
	bad := []string{`{}`, `{"url":"ftp://x"}`, `{"url":"http://x","bilinmeyen":1}`}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"url":" https://kadir.app ","keyword":"merhaba"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg BrowserConfig
	json.Unmarshal(norm, &cfg)
	if cfg.URL != "https://kadir.app" || cfg.Keyword != "merhaba" {
		t.Errorf("normalize yanlış: %+v", cfg)
	}
}

// TestBrowserNoEndpoint uç nokta hiç yapılandırılmamışsa Normalize'ın hâlâ
// başarılı olduğunu, Check'in ise düzgün bir DOWN mesajı döndürdüğünü doğrular
// (gerçek bir tarayıcı gerektirmez).
func TestBrowserNoEndpoint(t *testing.T) {
	old, had := os.LookupEnv(BrowserEndpointEnv)
	os.Unsetenv(BrowserEndpointEnv)
	defer func() {
		if had {
			os.Setenv(BrowserEndpointEnv, old)
		}
	}()

	r := run(t, "browser", map[string]any{"url": "http://example.com"})
	if r.Up || !strings.Contains(r.Message, BrowserEndpointEnv) {
		t.Errorf("uç nokta hatası bekleniyordu: %+v", r)
	}
}

// TestBrowserBadEndpoint yanlış/erişilemeyen bir uç noktayla DOWN sonucu ve
// açıklayıcı bir mesaj döndüğünü doğrular (gerçek tarayıcı gerektirmez).
func TestBrowserBadEndpoint(t *testing.T) {
	r := run(t, "browser", map[string]any{"url": "http://example.com", "endpoint": "http://127.0.0.1:1"})
	if r.Up || !strings.Contains(r.Message, "Tarayıcı bağlantısı kurulamadı") {
		t.Errorf("bağlantı hatası bekleniyordu: %+v", r)
	}
}

// TestBrowserIntegration gerçek bir uzak Chrome DevTools uç noktası
// gerektirir; sadece UPTIME_IT_BROWSER ayarlıyken çalışır. Manuel doğrulama
// için: bir chromedp/headless-shell (veya browserless/chromium) container'ı
// ve erişilebilir bir test sayfası aynı Docker ağında çalıştırılıp
// BROWSER_WS_ENDPOINT / UPTIME_IT_BROWSER_URL ortam değişkenleri verilir.
func TestBrowserIntegration(t *testing.T) {
	if os.Getenv("UPTIME_IT_BROWSER") == "" {
		t.Skip("UPTIME_IT_BROWSER ayarlı değil; gerçek tarayıcı gerektiren entegrasyon testi atlanıyor")
	}
	target := os.Getenv("UPTIME_IT_BROWSER_URL")
	if target == "" {
		t.Fatal("UPTIME_IT_BROWSER_URL (tarayıcı container'ının erişebileceği bir test sayfası) gerekli")
	}
	if os.Getenv(BrowserEndpointEnv) == "" {
		t.Fatalf("%s gerekli", BrowserEndpointEnv)
	}

	c, _ := Get("browser")
	cfg, err := c.Normalize(json.RawMessage(fmt.Sprintf(`{"url":%q,"keyword":"Merhaba"}`, target)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	r := c.Check(ctx, cfg)
	if !r.Up {
		t.Fatalf("tarayıcı kontrolü başarısız: %+v", r)
	}
	if r.PingMs < 1 {
		t.Errorf("yükleme süresi ölçülmedi: %+v", r)
	}
}
