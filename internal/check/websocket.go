package check

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// WebSocketConfig bir WebSocket bağlantısı kurar, isteğe bağlı bir mesaj
// gönderir ve gelen ilk yanıtta bir kelime arar.
type WebSocketConfig struct {
	URL       string `json:"url"`
	Headers   string `json:"headers"` // "Ad: değer" satırları
	Send      string `json:"send"`
	Keyword   string `json:"keyword"`
	IgnoreTLS bool   `json:"ignore_tls"`
}

type webSocketChecker struct{}

func init() { Register("websocket", webSocketChecker{}) }

func (webSocketChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c WebSocketConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimSpace(c.URL)
	u, err := url.Parse(c.URL)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" {
		return nil, invalid("Geçerli bir ws:// veya wss:// adresi girin")
	}
	if _, err := parseHeaders(c.Headers); err != nil {
		return nil, err
	}
	return encode(c), nil
}

func (webSocketChecker) Target(raw json.RawMessage) string {
	var c WebSocketConfig
	json.Unmarshal(raw, &c)
	return c.URL
}

func (webSocketChecker) CertExpiryEnabled(json.RawMessage) bool { return true }

func (webSocketChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c WebSocketConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}

	hdrs, _ := parseHeaders(c.Headers)
	header := http.Header{}
	for _, h := range hdrs {
		header.Add(h[0], h[1])
	}
	httpClient := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: c.IgnoreTLS},
	}}

	start := time.Now()
	conn, resp, err := websocket.Dial(ctx, c.URL, &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: header})
	if err != nil {
		return down(describeErr(ctx, err))
	}
	defer conn.CloseNow()
	ping := msSince(start)

	var cert *CertInfo
	if resp != nil && resp.TLS != nil {
		cert = certInfoFromState(*resp.TLS)
	}

	if c.Send != "" {
		if err := conn.Write(ctx, websocket.MessageText, []byte(c.Send)); err != nil {
			return Result{PingMs: ping, Message: "Mesaj gönderilemedi: " + describeErr(ctx, err), Cert: cert}
		}
	}

	if c.Keyword != "" {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return Result{PingMs: ping, Message: "Yanıt okunamadı: " + describeErr(ctx, err), Cert: cert}
		}
		ping = msSince(start)
		if !strings.Contains(string(data), c.Keyword) {
			return Result{PingMs: ping, Message: fmt.Sprintf("Kelime bulunamadı: %q", c.Keyword), Cert: cert}
		}
	}

	conn.Close(websocket.StatusNormalClosure, "")
	return Result{Up: true, PingMs: ping, Message: "Bağlantı kuruldu", Cert: cert}
}
