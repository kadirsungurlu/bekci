package notify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/http/httptest"
	"net/mail"
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

// TestDoRequestNoBodyLeak: hata durumunda mesaj, uzak yanıt gövdesini içermez
// (iç servise yönlendirilen webhook/bildirim isteğinde gövde iç veri
// sızdırabilir); durum kodu ve Türkçe ipucu korunur.
func TestDoRequestNoBodyLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "DAHILI-SIZINTI-GOVDESI")
	}))
	defer srv.Close()

	err := doRequest(context.Background(), http.MethodGet, srv.URL, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("HTTP 500 hatası bekleniyordu: %v", err)
	}
	if strings.Contains(err.Error(), "DAHILI-SIZINTI-GOVDESI") {
		t.Errorf("uzak yanıt gövdesi hataya sızdı: %v", err)
	}

	// 401 yetki ipucu korunur ama gövde yine sızmaz.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, "gizli-401-govdesi")
	}))
	defer srv2.Close()
	err = doRequest(context.Background(), http.MethodGet, srv2.URL, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "yetki") {
		t.Fatalf("401 yetki ipucu bekleniyordu: %v", err)
	}
	if strings.Contains(err.Error(), "gizli-401-govdesi") {
		t.Errorf("uzak yanıt gövdesi hataya sızdı: %v", err)
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
			return c.path == "/uyari" && c.headers.Get("Priority") == "4" && c.headers.Get("Authorization") == "Bearer tk" && decodeHeader(c.headers.Get("Title")) == "ha.kadir.app çalışmıyor"
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
	merged, err := MergeSecrets("telegram", edited, stored)
	if err != nil {
		t.Fatal(err)
	}
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
		if got := FormatDuration("tr", d); got != want {
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

func decodeHeader(v string) string {
	out, err := new(mime.WordDecoder).DecodeHeader(v)
	if err != nil {
		return v
	}
	return out
}

// Bulgu: bağlantı hatasında Telegram token'ı (URL'nin parçası) hata
// mesajına, dolayısıyla loglara ve arayüze sızmamalı.
func TestTransportErrorRedactsSecretURL(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // kapalı port: bağlantı reddedilir
	old := telegramAPI
	telegramAPI = "http://" + addr
	defer func() { telegramAPI = old }()

	err := send(t, "telegram", map[string]any{"bot_token": "123456:GIZLI-TOKEN", "chat_id": "1"}, downEvent)
	if err == nil {
		t.Fatal("hata bekleniyordu")
	}
	if strings.Contains(err.Error(), "GIZLI-TOKEN") || !strings.Contains(err.Error(), addr) {
		t.Errorf("token sızdı veya sunucu adı yok: %v", err)
	}
	// Discord/Slack: webhook adresinin tamamı gizli.
	err = send(t, "discord", map[string]any{"webhook_url": "http://" + addr + "/api/webhooks/1/GIZLI"}, downEvent)
	if err == nil || strings.Contains(err.Error(), "GIZLI") {
		t.Errorf("webhook adresi sızdı: %v", err)
	}
}

func TestEmailRejectsAuthWithoutTLS(t *testing.T) {
	p, _ := Get("email")
	_, err := p.Normalize(json.RawMessage(`{"host":"smtp.x","security":"none","username":"a","password":"b","from":"a@x.com","to":"b@x.com"}`))
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("şifresiz bağlantıda kimlik doğrulama reddedilmeli: %v", err)
	}
}

// Güvenlik: maskeli gizli alan, hedef adresi değiştirilmiş ayarla birleştirilmez
// (aksi halde gizli bilgi saldırganın sunucusuna "test gönder" ile sızardı).
func TestMergeSecretsRejectsDestinationChange(t *testing.T) {
	stored := json.RawMessage(`{"url":"https://kanca.kadir.app/x","method":"POST","headers":"Authorization: Bearer gizli"}`)
	cases := []struct {
		cfg    string
		reject bool
	}{
		{`{"url":"https://kanca.kadir.app/x","method":"PUT","headers":"` + Mask + `"}`, false},
		{`{"url":"https://saldirgan.example/topla","method":"POST","headers":"` + Mask + `"}`, true},
		{`{"url":"https://saldirgan.example/topla","method":"POST","headers":"Authorization: yeni"}`, false},
	}
	for _, c := range cases {
		_, err := MergeSecrets("webhook", json.RawMessage(c.cfg), stored)
		if (err != nil) != c.reject {
			t.Errorf("%s: hata=%v, ret bekleniyor=%v", c.cfg, err, c.reject)
		}
	}
	// E-posta: SMTP sunucusu değişince şifre taşınmaz.
	storedMail := json.RawMessage(`{"host":"smtp.kadir.app","port":587,"password":"p","from":"a@b.c","to":"c@d.e"}`)
	if _, err := MergeSecrets("email", json.RawMessage(`{"host":"smtp.saldirgan.example","port":587,"password":"`+Mask+`","from":"a@b.c","to":"c@d.e"}`), storedMail); err != ErrSecretRebind {
		t.Errorf("SMTP sunucusu değişince ret bekleniyordu: %v", err)
	}
}

// E-posta: düz metin ve HTML alternatifleri; HTML'de konumlar ve kaçış.
func TestMailMultipartHTML(t *testing.T) {
	ev := Event{Kind: KindDown, Lang: "tr", MonitorName: "Ayder <Tesisat>", Target: "https://a.example",
		Time: time.Date(2026, 10, 1, 10, 58, 1, 0, time.Local), IncidentURL: "https://u.example/#/incidents/17",
		Locations: []LocationNote{{Name: "Ana sunucu", Message: "HTTP 403 Forbidden"}, {Name: "CP Server IST", NoData: true}}}
	raw := buildMail("a@example.com", "b@example.com", ev)
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	mt, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" {
		t.Fatalf("içerik türü %q %v", mt, err)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	parts := map[string]string{}
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		body, _ := io.ReadAll(quotedprintable.NewReader(p))
		ct, _, _ := mime.ParseMediaType(p.Header.Get("Content-Type"))
		parts[ct] = string(body)
	}
	if !strings.Contains(parts["text/plain"], "• CP Server IST: sonuç gelmiyor") {
		t.Fatalf("düz metin:\n%s", parts["text/plain"])
	}
	h := parts["text/html"]
	for _, want := range []string{"Çalışmıyor", "Ayder &lt;Tesisat&gt; çalışmıyor", "CP Server IST", "sonuç gelmiyor", `href="https://u.example/#/incidents/17"`, "Ayrıntıları aç"} {
		if !strings.Contains(h, want) {
			t.Fatalf("HTML'de %q yok:\n%s", want, h)
		}
	}
}
