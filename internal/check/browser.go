package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// BrowserEndpointEnv gerçek tarayıcı kontrolü için uzak Chrome DevTools uç
// noktasının okunduğu ortam değişkeni (monitör bazında override edilebilir).
const BrowserEndpointEnv = "BROWSER_WS_ENDPOINT"

// BrowserConfig Uptime Kuma'nın "HTTP(s) - Browser Engine" tipine benzer:
// sayfayı gerçek (uzak, container'da çalışan) bir Chrome ile açar ve render
// edilmiş sonucu denetler. Uygulamanın kendi Docker imajında tarayıcı YOKTUR;
// BROWSER_WS_ENDPOINT (veya monitöre özel "endpoint") ile dışarıdaki bir
// Chrome DevTools uç noktasına bağlanılır.
type BrowserConfig struct {
	URL                 string `json:"url"`
	Endpoint            string `json:"endpoint"` // boşsa BROWSER_WS_ENDPOINT kullanılır
	Keyword             string `json:"keyword"`
	WaitSelector        string `json:"wait_selector"`
	IgnoreTLS           bool   `json:"ignore_tls"`
	FailOnConsoleErrors bool   `json:"fail_on_console_errors"`
}

type browserChecker struct{}

func init() { Register("browser", browserChecker{}) }

func (browserChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c BrowserConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimSpace(c.URL)
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, invalid("Geçerli bir http:// veya https:// adresi girin")
	}
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.WaitSelector = strings.TrimSpace(c.WaitSelector)
	return encode(c), nil
}

func (browserChecker) Target(raw json.RawMessage) string {
	var c BrowserConfig
	json.Unmarshal(raw, &c)
	return c.URL
}

func (browserChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c BrowserConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}

	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(BrowserEndpointEnv))
	}
	if endpoint == "" {
		return down(fmt.Sprintf("Tarayıcı uç noktası ayarlanmamış (%s)", BrowserEndpointEnv))
	}

	wsURL, err := resolveBrowserEndpoint(ctx, endpoint)
	if err != nil {
		return down("Tarayıcı bağlantısı kurulamadı: " + describeErr(ctx, err))
	}

	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL)
	defer cancelAlloc()
	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var status int64
	var consoleErrs []string
	chromedp.ListenTarget(browserCtx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventResponseReceived:
			if e.Type == network.ResourceTypeDocument && status == 0 {
				status = e.Response.Status
			}
		case *runtime.EventExceptionThrown:
			if e.ExceptionDetails != nil {
				consoleErrs = append(consoleErrs, e.ExceptionDetails.Text)
			}
		case *runtime.EventConsoleAPICalled:
			if e.Type == "error" {
				consoleErrs = append(consoleErrs, consoleArgsText(e.Args))
			}
		}
	})

	var bodyText string
	actions := []chromedp.Action{network.Enable(), chromedp.Navigate(c.URL)}
	if c.WaitSelector != "" {
		actions = append(actions, chromedp.WaitVisible(c.WaitSelector, chromedp.ByQuery))
	}
	actions = append(actions, chromedp.Evaluate(`document.body ? document.body.innerText : ""`, &bodyText))

	start := time.Now()
	err = chromedp.Run(browserCtx, actions...)
	ping := msSince(start)
	if err != nil {
		return down("Tarayıcı bağlantısı kurulamadı: " + describeErr(ctx, err))
	}

	if status >= 400 {
		return Result{PingMs: ping, Message: fmt.Sprintf("HTTP %d", status)}
	}
	if c.Keyword != "" && !strings.Contains(bodyText, c.Keyword) {
		return Result{PingMs: ping, Message: "Kelime bulunamadı"}
	}
	if c.FailOnConsoleErrors && len(consoleErrs) > 0 {
		return Result{PingMs: ping, Message: "Konsol hatası: " + truncate(strings.Join(consoleErrs, "; "), 160)}
	}

	return Result{Up: true, PingMs: ping, Message: fmt.Sprintf("Sayfa yüklendi (%s sn)", formatSeconds(ping))}
}

func formatSeconds(ms int64) string {
	return strings.Replace(fmt.Sprintf("%.1f", float64(ms)/1000), ".", ",", 1)
}

func consoleArgsText(args []*runtime.RemoteObject) string {
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			continue
		}
		if a.Value != nil {
			parts = append(parts, string(a.Value))
		} else if a.Description != "" {
			parts = append(parts, a.Description)
		}
	}
	return strings.Join(parts, " ")
}

// resolveBrowserEndpoint verilen ws(s):// veya http(s):// taban adresinden
// /json/version'a bakarak gerçek webSocketDebuggerUrl'i çözer.
//
// Chrome, DNS rebinding saldırılarına karşı /json/version isteklerinde Host
// başlığının "localhost" veya bir IP adresi olmasını zorunlu kılar ("Host
// header is specified and is not an IP address or localhost" hatası). Docker
// içinde servis adıyla verilen bir uç nokta (ör. ws://browser:3000) bu yüzden
// önce IP adresine çözülür; Chrome webSocketDebuggerUrl'i de bu IP ile
// döndürür, böylece sonuç hâlâ container ağından erişilebilir kalır.
func resolveBrowserEndpoint(ctx context.Context, endpoint string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
		// zaten uygun
	default:
		return "", fmt.Errorf("desteklenmeyen uç nokta şeması: %s", u.Scheme)
	}
	if host := u.Hostname(); host != "localhost" && net.ParseIP(host) == nil {
		addrs, err := net.DefaultResolver.LookupHost(ctx, host)
		if err != nil {
			return "", fmt.Errorf("uç nokta adresi çözülemedi: %w", err)
		}
		if len(addrs) > 0 {
			if port := u.Port(); port != "" {
				u.Host = net.JoinHostPort(addrs[0], port)
			} else {
				u.Host = addrs[0]
			}
		}
	}
	u.Path = strings.TrimSuffix(u.Path, "/") + "/json/version"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("uç nokta %d döndürdü", resp.StatusCode)
	}
	var info struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&info); err != nil {
		return "", err
	}
	if info.WebSocketDebuggerURL == "" {
		return "", errors.New("webSocketDebuggerUrl bulunamadı")
	}
	return info.WebSocketDebuggerURL, nil
}
