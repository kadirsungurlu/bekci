package notify

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// capture sahte bir HTTP sunucusu; gelen son isteği saklar.
type capture struct {
	mu      sync.Mutex
	method  string
	path    string
	headers http.Header
	body    string
	status  int
}

func newCapture(t *testing.T) (*capture, *httptest.Server) {
	c := &capture{status: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.method, c.path, c.headers, c.body = r.Method, r.URL.Path, r.Header, string(b)
		st := c.status
		c.mu.Unlock()
		w.WriteHeader(st)
		io.WriteString(w, `{"ok":true}`)
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

var downEvent = Event{
	Kind: KindDown, MonitorID: 3, MonitorName: "ha.kadir.app", MonitorType: "http",
	Target: "https://ha.kadir.app", Message: "HTTP 502 Bad Gateway",
	Time: time.Date(2026, 9, 27, 2, 15, 0, 0, time.UTC),
}

func send(t *testing.T, typ string, cfg map[string]any, ev Event) error {
	t.Helper()
	p, ok := Get(typ)
	if !ok {
		t.Fatalf("tip kayıtlı değil: %s", typ)
	}
	raw, _ := json.Marshal(cfg)
	norm, err := p.Normalize(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.Send(ctx, norm, ev)
}

func TestWhatsApp(t *testing.T) {
	c, srv := newCapture(t)
	err := send(t, "whatsapp", map[string]any{"url": srv.URL + "/", "api_key": "wagw_x", "to": "+90 555 111 2233"}, downEvent)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	json.Unmarshal([]byte(c.body), &body)
	if c.path != "/api/v1/messages" || c.headers.Get("x-api-key") != "wagw_x" || body["to"] != "905551112233" || !strings.Contains(body["body"], "HTTP 502") {
		t.Errorf("istek yanlış: %s %v %s", c.path, c.headers, c.body)
	}
}

func TestTelegramAndErrors(t *testing.T) {
	c, srv := newCapture(t)
	old := telegramAPI
	telegramAPI = srv.URL
	defer func() { telegramAPI = old }()

	if err := send(t, "telegram", map[string]any{"bot_token": "123:ABC", "chat_id": "-100"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path != "/bot123:ABC/sendMessage" || !strings.Contains(c.body, `"chat_id":"-100"`) {
		t.Errorf("istek yanlış: %s %s", c.path, c.body)
	}
	c.status = 401
	if err := send(t, "telegram", map[string]any{"bot_token": "123:ABC", "chat_id": "-100"}, downEvent); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("401 hatası bekleniyordu: %v", err)
	}
}

func TestWebhookPayload(t *testing.T) {
	c, srv := newCapture(t)
	up := downEvent
	up.Kind, up.Downtime = KindUp, 12*time.Minute
	if err := send(t, "webhook", map[string]any{"url": srv.URL + "/hook", "headers": "Authorization: Bearer x"}, up); err != nil {
		t.Fatal(err)
	}
	var p WebhookPayload
	if err := json.Unmarshal([]byte(c.body), &p); err != nil {
		t.Fatal(err)
	}
	if c.headers.Get("Authorization") != "Bearer x" || p.Event != "up" || p.DowntimeSeconds != 720 || p.Monitor.Name != "ha.kadir.app" || !strings.Contains(p.Text, "12 dk") {
		t.Errorf("gövde yanlış: %+v", p)
	}
}

func TestOtherHTTPProviders(t *testing.T) {
	c, srv := newCapture(t)
	oldPO := pushoverAPI
	pushoverAPI = srv.URL + "/po"
	defer func() { pushoverAPI = oldPO }()

	cases := []struct {
		typ   string
		cfg   map[string]any
		check func() bool
	}{
		{"discord", map[string]any{"webhook_url": srv.URL + "/d"}, func() bool { return c.path == "/d" && strings.Contains(c.body, "ha.kadir.app çalışmıyor") }},
		{"slack", map[string]any{"webhook_url": srv.URL + "/s"}, func() bool { return c.path == "/s" && strings.Contains(c.body, `"text"`) }},
		{"ntfy", map[string]any{"server": srv.URL, "topic": "uyari", "token": "tk"}, func() bool {
			return c.path == "/uyari" && c.headers.Get("Priority") == "4" && c.headers.Get("Authorization") == "Bearer tk" && c.headers.Get("Title") == "ha.kadir.app çalışmıyor"
		}},
		{"gotify", map[string]any{"server": srv.URL, "app_token": "g"}, func() bool { return c.path == "/message" && c.headers.Get("X-Gotify-Key") == "g" }},
		{"pushover", map[string]any{"user_key": "u", "app_token": "a", "priority": 1}, func() bool { return c.path == "/po" && strings.Contains(c.body, "priority=1") }},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			if err := send(t, tc.typ, tc.cfg, downEvent); err != nil {
				t.Fatal(err)
			}
			if !tc.check() {
				t.Errorf("istek yanlış: %s %s %v", c.path, c.body, c.headers)
			}
		})
	}
}

// fakeSMTP çok basit bir SMTP sunucusu; alınan DATA'yı döner.
func fakeSMTP(t *testing.T) (addr string, got chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	got = make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		w := func(s string) { io.WriteString(conn, s+"\r\n") }
		w("220 test")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					got <- data.String()
					w("250 ok")
					continue
				}
				data.WriteString(line)
				continue
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250 test")
			case cmd == "DATA":
				inData = true
				w("354 go")
			case cmd == "QUIT":
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String(), got
}

func TestEmail(t *testing.T) {
	addr, got := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	var p int
	json.Unmarshal([]byte(port), &p)
	err := send(t, "email", map[string]any{
		"host": host, "port": p, "security": "none",
		"from": "Uptime <uptime@kadir.app>", "to": "kadir@example.com, diger@example.com",
	}, downEvent)
	if err != nil {
		t.Fatal(err)
	}
	msg := <-got
	if !strings.Contains(msg, "Subject: =?utf-8?q?") || !strings.Contains(msg, "To: <kadir@example.com>, <diger@example.com>") || !strings.Contains(msg, "HTTP 502") {
		t.Errorf("e-posta yanlış:\n%s", msg)
	}
}

func TestNormalizeRejects(t *testing.T) {
	bad := map[string]string{
		"whatsapp": `{"url":"https://wp","api_key":"k","to":"123"}`,
		"telegram": `{"bot_token":"","chat_id":"1"}`,
		"discord":  `{"webhook_url":"ftp://x"}`,
		"webhook":  `{"url":"https://x","method":"GET"}`,
		"ntfy":     `{"topic":"a","priority":9}`,
		"email":    `{"host":"h","from":"bozuk","to":"a@b.c"}`,
		"pushover": `{"user_key":"u","app_token":"a","priority":2}`,
		"gotify":   `{"server":"https://g"}`,
	}
	for typ, cfg := range bad {
		p, _ := Get(typ)
		if _, err := p.Normalize(json.RawMessage(cfg)); err == nil {
			t.Errorf("%s: reddedilmeliydi", typ)
		}
	}
}

func TestMaskAndMerge(t *testing.T) {
	stored := json.RawMessage(`{"bot_token":"123:ABC","chat_id":"-100","thread_id":0}`)
	masked := MaskSecrets("telegram", stored)
	if strings.Contains(string(masked), "123:ABC") || !strings.Contains(string(masked), Mask) {
		t.Fatalf("maskelenmedi: %s", masked)
	}
	// Arayüz maskeli değeri geri gönderir, sadece chat_id'yi değiştirir.
	edited := json.RawMessage(strings.Replace(string(masked), "-100", "-200", 1))
	merged := MergeSecrets("telegram", edited, stored)
	var m map[string]any
	json.Unmarshal(merged, &m)
	if m["bot_token"] != "123:ABC" || m["chat_id"] != "-200" {
		t.Errorf("birleştirme yanlış: %s", merged)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:              "45 sn",
		12 * time.Minute:              "12 dk",
		2*time.Hour + 5*time.Minute:   "2 sa 5 dk",
		3 * time.Hour:                 "3 sa",
		(3*24 + 4) * time.Hour:        "3 gün 4 sa",
		48*time.Hour + 30*time.Minute: "2 gün",
	}
	for d, want := range cases {
		if got := FormatDuration(d); got != want {
			t.Errorf("%v → %q, %q bekleniyordu", d, got, want)
		}
	}
}

func TestEventText(t *testing.T) {
	ev := Event{Kind: KindCert, MonitorName: "kadir.app", Target: "https://kadir.app", CertDays: 7,
		CertExpires: time.Now().Add(7 * 24 * time.Hour), CertIssuer: "Let's Encrypt", Time: time.Now()}
	txt := ev.Text()
	if !strings.Contains(txt, "7 gün içinde") || !strings.Contains(txt, "Let's Encrypt") {
		t.Errorf("sertifika metni yanlış:\n%s", txt)
	}
}
