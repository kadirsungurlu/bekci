package notify

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

// upEvent downEvent'in "up" karşılığı; birçok sağlayıcı trigger/resolve
// ayrımını test eder.
var upEvent = Event{
	Kind: KindUp, MonitorID: 3, MonitorName: "ha.kadir.app", MonitorType: "http",
	Target: "https://ha.kadir.app", Downtime: 5 * time.Minute,
	Time: time.Date(2026, 9, 27, 2, 20, 0, 0, time.UTC),
}

func TestTeams(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "teams", map[string]any{"webhook_url": srv.URL}, downEvent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.body, "AdaptiveCard") || !strings.Contains(c.body, `"attention"`) || !strings.Contains(c.body, "ha.kadir.app") {
		t.Errorf("kart yanlış: %s", c.body)
	}
	if err := send(t, "teams", map[string]any{"webhook_url": srv.URL}, upEvent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.body, `"good"`) {
		t.Errorf("up renk yanlış: %s", c.body)
	}
}

func TestGoogleChat(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "googlechat", map[string]any{"webhook_url": srv.URL}, downEvent); err != nil {
		t.Fatal(err)
	}
	var body map[string]string
	json.Unmarshal([]byte(c.body), &body)
	if !strings.Contains(body["text"], "ha.kadir.app çalışmıyor") {
		t.Errorf("gövde yanlış: %s", c.body)
	}
}

func TestMattermost(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "mattermost", map[string]any{"webhook_url": srv.URL}, downEvent); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["username"] != brand.Name || body["channel"] != nil {
		t.Errorf("varsayılanlar yanlış: %v", body)
	}
	if err := send(t, "mattermost", map[string]any{"webhook_url": srv.URL, "channel": "#uyari", "username": "Bot", "icon_url": srv.URL + "/i.png"}, downEvent); err != nil {
		t.Fatal(err)
	}
	json.Unmarshal([]byte(c.body), &body)
	if body["channel"] != "#uyari" || body["username"] != "Bot" || body["icon_url"] != srv.URL+"/i.png" {
		t.Errorf("özel alanlar yanlış: %v", body)
	}
}

func TestRocketchat(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "rocketchat", map[string]any{"webhook_url": srv.URL, "channel": "#genel", "alias": "Uptime-Bot"}, downEvent); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["channel"] != "#genel" || body["alias"] != "Uptime-Bot" {
		t.Errorf("gövde yanlış: %v", body)
	}
}

func TestMatrix(t *testing.T) {
	c, srv := newCapture(t)
	cfg := map[string]any{"homeserver_url": srv.URL, "access_token": "tok_x", "room_id": "!abc:matrix.org"}
	if err := send(t, "matrix", cfg, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.method != http.MethodPut || !strings.Contains(c.path, "/rooms/!abc:matrix.org/send/m.room.message/") ||
		c.headers.Get("Authorization") != "Bearer tok_x" {
		t.Errorf("istek yanlış: %s %s %v", c.method, c.path, c.headers)
	}
	var body map[string]string
	json.Unmarshal([]byte(c.body), &body)
	if !strings.Contains(body["formatted_body"], "<br/>") || !strings.Contains(body["body"], "Hedef") {
		t.Errorf("gövde yanlış: %v", body)
	}
	firstPath := c.path
	if err := send(t, "matrix", cfg, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path == firstPath {
		t.Errorf("txn id benzersiz olmalı: %s", c.path)
	}
}

func TestSignal(t *testing.T) {
	c, srv := newCapture(t)
	cfg := map[string]any{"url": srv.URL, "number": "+905551112233", "recipients": "+905553334455, +905556667788"}
	if err := send(t, "signal", cfg, downEvent); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	recips, _ := body["recipients"].([]any)
	if c.path != "/v2/send" || body["number"] != "+905551112233" || len(recips) != 2 {
		t.Errorf("gövde yanlış: %v (path=%s)", body, c.path)
	}
}

func TestPagerDuty(t *testing.T) {
	c, srv := newCapture(t)
	old := pagerdutyAPI
	pagerdutyAPI = srv.URL
	defer func() { pagerdutyAPI = old }()

	if err := send(t, "pagerduty", map[string]any{"routing_key": "rk_x"}, downEvent); err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["event_action"] != "trigger" || body["dedup_key"] != "uptime-monitor-3" {
		t.Errorf("down gövdesi yanlış: %v", body)
	}
	payload, _ := body["payload"].(map[string]any)
	if payload["severity"] != "critical" {
		t.Errorf("severity yanlış: %v", payload)
	}

	if err := send(t, "pagerduty", map[string]any{"routing_key": "rk_x"}, upEvent); err != nil {
		t.Fatal(err)
	}
	body = nil
	json.Unmarshal([]byte(c.body), &body)
	if body["event_action"] != "resolve" || body["dedup_key"] != "uptime-monitor-3" || body["payload"] != nil {
		t.Errorf("up gövdesi yanlış: %v", body)
	}

	certEv := downEvent
	certEv.Kind, certEv.CertDays = KindCert, 5
	if err := send(t, "pagerduty", map[string]any{"routing_key": "rk_x"}, certEv); err != nil {
		t.Fatal(err)
	}
	body = nil
	json.Unmarshal([]byte(c.body), &body)
	payload, _ = body["payload"].(map[string]any)
	if body["dedup_key"] != "uptime-monitor-3-cert" || payload["severity"] != "warning" {
		t.Errorf("cert gövdesi yanlış: %v", body)
	}
}

func TestOpsgenie(t *testing.T) {
	c, srv := newCapture(t)
	old := opsgenieAPIUS
	opsgenieAPIUS = srv.URL
	defer func() { opsgenieAPIUS = old }()

	if err := send(t, "opsgenie", map[string]any{"api_key": "key_x", "priority": "p2"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path != "/v2/alerts" || c.headers.Get("Authorization") != "GenieKey key_x" {
		t.Errorf("create isteği yanlış: %s %v", c.path, c.headers)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["alias"] != "uptime-monitor-3" || body["priority"] != "P2" {
		t.Errorf("gövde yanlış: %v", body)
	}

	if err := send(t, "opsgenie", map[string]any{"api_key": "key_x"}, upEvent); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, "/v2/alerts/uptime-monitor-3/close") {
		t.Errorf("close isteği yanlış: %s", c.path)
	}
}

func TestHomeAssistant(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "homeassistant", map[string]any{"url": srv.URL, "token": "tok_x", "service": "mobile_app_kadir_iphone"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path != "/api/services/notify/mobile_app_kadir_iphone" || c.headers.Get("Authorization") != "Bearer tok_x" {
		t.Errorf("istek yanlış: %s %v", c.path, c.headers)
	}
	var body map[string]string
	json.Unmarshal([]byte(c.body), &body)
	if !strings.Contains(body["title"], "çalışmıyor") {
		t.Errorf("gövde yanlış: %v", body)
	}
}

// netgsmCapture GET isteğinin tam URI'sini (sorgu dizesi dahil) saklayan basit sahte sunucu.
type netgsmCapture struct {
	mu  sync.Mutex
	uri string
}

func newNetgsmServer(t *testing.T, response string) (*netgsmCapture, *httptest.Server) {
	c := &netgsmCapture{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.mu.Lock()
		c.uri = r.URL.RequestURI()
		c.mu.Unlock()
		w.WriteHeader(200)
		w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return c, srv
}

func TestNetgsmSuccess(t *testing.T) {
	c, srv := newNetgsmServer(t, "00 123456")
	old := netgsmAPI
	netgsmAPI = srv.URL
	defer func() { netgsmAPI = old }()

	err := send(t, "netgsm", map[string]any{
		"usercode": "u", "password": "p", "msgheader": "UPTIME", "gsm": "0555 111 22 33, +90 555 444 55 66",
		"turkish_chars": true,
	}, downEvent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.uri, "usercode=u") || !strings.Contains(c.uri, "msgheader=UPTIME") ||
		!strings.Contains(c.uri, "gsmno=5551112233%2C905554445566") || !strings.Contains(c.uri, "dil=TR") {
		t.Errorf("istek parametreleri yanlış: %s", c.uri)
	}
}

func TestNetgsmErrorCode(t *testing.T) {
	_, srv := newNetgsmServer(t, "40")
	old := netgsmAPI
	netgsmAPI = srv.URL
	defer func() { netgsmAPI = old }()

	err := send(t, "netgsm", map[string]any{
		"usercode": "u", "password": "p", "msgheader": "UPTIME", "gsm": "5551112233",
	}, downEvent)
	if err == nil || !strings.Contains(err.Error(), "Netgsm hata 40") {
		t.Errorf("40 hatası bekleniyordu: %v", err)
	}
}

func TestNetgsmPhoneNormalize(t *testing.T) {
	cases := map[string]string{
		"5551112233":         "5551112233",
		"05551112233":        "5551112233",
		"+905551112233":      "905551112233",
		"905551112233":       "905551112233",
		"0090 555 111 22 33": "5551112233",
	}
	for in, want := range cases {
		got, ok := normalizeTRPhone(in)
		if !ok || got != want {
			t.Errorf("normalizeTRPhone(%q) = %q,%v; want %q", in, got, ok, want)
		}
	}
	if _, ok := normalizeTRPhone("123"); ok {
		t.Errorf("kısa numara kabul edilmemeli")
	}
}

func TestTwilio(t *testing.T) {
	c, srv := newCapture(t)
	old := twilioAPI
	twilioAPI = srv.URL
	defer func() { twilioAPI = old }()

	err := send(t, "twilio", map[string]any{
		"account_sid": "ACxxx", "auth_token": "tok_x", "from": "+15550001111", "to": "+905551112233",
	}, downEvent)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, "/Accounts/ACxxx/Messages.json") {
		t.Errorf("yol yanlış: %s", c.path)
	}
	if !strings.HasPrefix(c.headers.Get("Authorization"), "Basic ") {
		t.Errorf("Basic auth eksik: %v", c.headers)
	}
	if !strings.Contains(c.body, "To=%2B905551112233") {
		t.Errorf("gövde yanlış: %s", c.body)
	}
}

func TestPushbullet(t *testing.T) {
	c, srv := newCapture(t)
	old := pushbulletAPI
	pushbulletAPI = srv.URL
	defer func() { pushbulletAPI = old }()

	if err := send(t, "pushbullet", map[string]any{"access_token": "tok_x", "channel_tag": "uptime"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.headers.Get("Access-Token") != "tok_x" {
		t.Errorf("başlık yanlış: %v", c.headers)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["channel_tag"] != "uptime" || body["type"] != "note" {
		t.Errorf("gövde yanlış: %v", body)
	}

	p, _ := Get("pushbullet")
	if _, err := p.Normalize(json.RawMessage(`{"access_token":"t","channel_tag":"a","device_iden":"b"}`)); err == nil {
		t.Errorf("ikisi birden reddedilmeli")
	}
}

func TestBark(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "bark", map[string]any{"server": srv.URL, "device_key": "dk_x"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path != "/push" {
		t.Errorf("path yanlış: %s", c.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["device_key"] != "dk_x" || body["group"] != brand.Name || body["level"] != "critical" {
		t.Errorf("gövde yanlış: %v", body)
	}
}

func TestLine(t *testing.T) {
	c, srv := newCapture(t)
	old := lineAPI
	lineAPI = srv.URL
	defer func() { lineAPI = old }()

	to := "U" + strings.Repeat("a", 32)
	if err := send(t, "line", map[string]any{"channel_access_token": "tok_x", "to": to}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.headers.Get("Authorization") != "Bearer tok_x" {
		t.Errorf("başlık yanlış: %v", c.headers)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["to"] != to {
		t.Errorf("gövde yanlış: %v", body)
	}
}

func TestApprise(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "apprise", map[string]any{"server": srv.URL, "urls": "tgram://tok/chat, discord://webhook"}, downEvent); err != nil {
		t.Fatal(err)
	}
	if c.path != "/notify" {
		t.Errorf("path yanlış: %s", c.path)
	}
	var body map[string]any
	json.Unmarshal([]byte(c.body), &body)
	if body["type"] != "failure" || body["urls"] != "tgram://tok/chat,discord://webhook" {
		t.Errorf("gövde yanlış: %v", body)
	}
}

func TestNewProvidersNormalizeRejects(t *testing.T) {
	bad := map[string]string{
		"teams":         `{"webhook_url":"ftp://x"}`,
		"googlechat":    `{"webhook_url":""}`,
		"mattermost":    `{"webhook_url":"https://x","icon_url":"not-a-url"}`,
		"rocketchat":    `{"webhook_url":"https://x","avatar":"not-a-url"}`,
		"matrix":        `{"homeserver_url":"https://m","access_token":"t","room_id":"abc"}`,
		"signal":        `{"url":"https://s","number":"555","recipients":"a"}`,
		"pagerduty":     `{"routing_key":"rk","severity":"deadly"}`,
		"opsgenie":      `{"api_key":"k","region":"asia"}`,
		"homeassistant": `{"url":"https://h","token":"t","service":"Not Valid!"}`,
		"netgsm":        `{"usercode":"u","password":"p","msgheader":"H","gsm":"123"}`,
		"twilio":        `{"account_sid":"AC","auth_token":"t","from":"555","to":"+905551112233"}`,
		"pushbullet":    `{"access_token":"t","channel_tag":"a","device_iden":"b"}`,
		"bark":          `{"device_key":""}`,
		"line":          `{"channel_access_token":"t","to":"not-valid"}`,
		"apprise":       `{"server":"https://a","urls":""}`,
	}
	for typ, cfg := range bad {
		p, ok := Get(typ)
		if !ok {
			t.Fatalf("%s: kayıtlı değil", typ)
		}
		if _, err := p.Normalize(json.RawMessage(cfg)); err == nil {
			t.Errorf("%s: reddedilmeliydi", typ)
		}
	}
}

// Sırlar hata mesajlarına sızmamalı: webhook adresi tabanlı sağlayıcılarda
// bağlantı hatasında adresin tamamı; token tabanlılarda token görünmemeli.
func TestNewProvidersSecretsDontLeak(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	err := send(t, "teams", map[string]any{"webhook_url": "http://" + addr + "/gizli-webhook-yolu"}, downEvent)
	if err == nil || strings.Contains(err.Error(), "gizli-webhook-yolu") {
		t.Errorf("teams webhook adresi sızdı: %v", err)
	}

	old := netgsmAPI
	netgsmAPI = "http://" + addr
	defer func() { netgsmAPI = old }()
	err = send(t, "netgsm", map[string]any{
		"usercode": "u", "password": "GIZLI-SIFRE", "msgheader": "H", "gsm": "5551112233",
	}, downEvent)
	if err == nil || strings.Contains(err.Error(), "GIZLI-SIFRE") {
		t.Errorf("netgsm şifresi sızdı: %v", err)
	}
}

func TestNetgsmText(t *testing.T) {
	txt := netgsmText(downEvent)
	if strings.ContainsAny(txt, "🔴🟢⚠️✅") {
		t.Errorf("emoji SMS metninde olmamalı: %q", txt)
	}
	if !strings.Contains(txt, "HTTP 502") {
		t.Errorf("neden metinde eksik: %q", txt)
	}
}
