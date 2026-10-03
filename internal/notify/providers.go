package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

// Testlerde sahte sunucuya yönlendirmek için değiştirilebilir adresler.
var (
	telegramAPI = "https://api.telegram.org"
	pushoverAPI = "https://api.pushover.net/1/messages.json"
)

func init() {
	Register("whatsapp", whatsapp{})
	Register("telegram", telegram{})
	Register("discord", discord{})
	Register("slack", slack{})
	Register("webhook", webhook{})
	Register("ntfy", ntfy{})
	Register("gotify", gotify{})
	Register("pushover", pushover{})
}

// WhatsApp: kendi WP API ağ geçidi (POST /api/v1/messages, x-api-key) -------------

type whatsappConfig struct {
	URL          string `json:"url"`
	APIKey       string `json:"api_key"`
	To           string `json:"to"` // telefon (ülke koduyla, + olmadan) veya grup JID'i
	FromNumberID string `json:"from_number_id"`
}

var waRecipient = regexp.MustCompile(`^(\d{10,15}|[\d-]+@g\.us)$`)

type whatsapp struct{}

func (whatsapp) Secrets() []string { return []string{"api_key"} }

func (whatsapp) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c whatsappConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimRight(strings.TrimSpace(c.URL), "/")
	c.To = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(c.To), " ", ""), "+")
	if err := validURL("WP API adresi", c.URL); err != nil {
		return nil, err
	}
	if err := required("API anahtarı", c.APIKey); err != nil {
		return nil, err
	}
	if !waRecipient.MatchString(c.To) {
		return nil, invalid("Alıcı, ülke koduyla 10-15 haneli numara (ör. 905xxxxxxxxx) veya grup JID'i (…@g.us) olmalı")
	}
	return encode(c), nil
}

func (whatsapp) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c whatsappConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{"to": c.To, "body": ev.Text()}
	if c.FromNumberID != "" {
		payload["fromNumberId"] = c.FromNumberID
	}
	return postJSON(ctx, c.URL+"/api/v1/messages", payload, map[string]string{"x-api-key": c.APIKey})
}

// Telegram ------------------------------------------------------------------------

type telegramConfig struct {
	BotToken string `json:"bot_token"`
	ChatID   string `json:"chat_id"`
	ThreadID int64  `json:"thread_id"`
}

type telegram struct{}

func (telegram) Secrets() []string { return []string{"bot_token"} }

func (telegram) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c telegramConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.BotToken, c.ChatID = strings.TrimSpace(c.BotToken), strings.TrimSpace(c.ChatID)
	if err := required("Bot token", c.BotToken); err != nil {
		return nil, err
	}
	if err := required("Chat ID", c.ChatID); err != nil {
		return nil, err
	}
	return encode(c), nil
}

func (telegram) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c telegramConfig
	json.Unmarshal(raw, &c)
	payload := map[string]any{"chat_id": c.ChatID, "text": ev.Text(), "disable_web_page_preview": true}
	if c.ThreadID != 0 {
		payload["message_thread_id"] = c.ThreadID
	}
	return postJSON(ctx, telegramAPI+"/bot"+c.BotToken+"/sendMessage", payload, nil)
}

// Discord -------------------------------------------------------------------------

type webhookURLConfig struct {
	WebhookURL string `json:"webhook_url"`
}

func normalizeWebhookURL(raw json.RawMessage) (json.RawMessage, error) {
	var c webhookURLConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.WebhookURL = strings.TrimSpace(c.WebhookURL)
	if err := validURL("Webhook adresi", c.WebhookURL); err != nil {
		return nil, err
	}
	return encode(c), nil
}

type discord struct{}

func (discord) Secrets() []string { return []string{"webhook_url"} }

func (discord) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	return normalizeWebhookURL(raw)
}

func (discord) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c webhookURLConfig
	json.Unmarshal(raw, &c)
	text := ev.Text()
	if r := []rune(text); len(r) > 2000 {
		text = string(r[:1990]) + "…"
	}
	return postJSON(ctx, c.WebhookURL, map[string]any{"username": brand.Name, "content": text}, nil)
}

// Slack ---------------------------------------------------------------------------

type slack struct{}

func (slack) Secrets() []string { return []string{"webhook_url"} }

func (slack) Normalize(raw json.RawMessage) (json.RawMessage, error) { return normalizeWebhookURL(raw) }

func (slack) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c webhookURLConfig
	json.Unmarshal(raw, &c)
	return postJSON(ctx, c.WebhookURL, map[string]any{"text": ev.Text()}, nil)
}

// Genel webhook -------------------------------------------------------------------

type webhookConfig struct {
	URL     string `json:"url"`
	Method  string `json:"method"`
	Headers string `json:"headers"` // her satır "Ad: değer"; gizli sayılır (Authorization vb.)
	// Secret doluysa her istek X-Bekci-Signature başlığıyla imzalanır (bkz.
	// WebhookSignature); alıcı isteğin gerçekten bu sunucudan geldiğini doğrular.
	Secret string `json:"secret,omitempty"`
}

type webhook struct{}

func (webhook) Secrets() []string { return []string{"headers", "secret"} }

// WebhookSignatureHeader imzanın gönderildiği başlık.
const WebhookSignatureHeader = "X-Bekci-Signature"

// webhookSecretMax imza anahtarının en fazla uzunluğu.
const webhookSecretMax = 256

// WebhookSignature webhook gövdesinin imzası: "t=<unix>,v1=<hex>" biçiminde;
// v1 = HMAC-SHA256(secret, t + "." + body). Zaman damgası tekrar (replay)
// saldırısına karşı alıcının eski istekleri reddedebilmesi içindir.
func WebhookSignature(secret string, t int64, body []byte) string {
	ts := strconv.FormatInt(t, 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "t=" + ts + ",v1=" + hex.EncodeToString(mac.Sum(nil))
}

func (webhook) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c webhookConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.URL = strings.TrimSpace(c.URL)
	if err := validURL("Adres", c.URL); err != nil {
		return nil, err
	}
	c.Method = strings.ToUpper(strings.TrimSpace(c.Method))
	if c.Method == "" {
		c.Method = http.MethodPost
	}
	if c.Method != http.MethodPost && c.Method != http.MethodPut {
		return nil, invalid("Webhook metodu POST veya PUT olmalı")
	}
	if _, err := parseHeaderLines(c.Headers); err != nil {
		return nil, err
	}
	c.Secret = strings.TrimSpace(c.Secret)
	if len(c.Secret) > webhookSecretMax {
		return nil, invalid("İmza anahtarı en fazla %d karakter olabilir", webhookSecretMax)
	}
	return encode(c), nil
}

// WebhookPayload genel webhook'un gönderdiği JSON gövde.
type WebhookPayload struct {
	Event           string `json:"event"`
	Title           string `json:"title"`
	Text            string `json:"text"`
	Message         string `json:"message"`
	Time            string `json:"time"`
	DowntimeSeconds int64  `json:"downtime_seconds,omitempty"`
	CertDays        *int   `json:"cert_days,omitempty"`
	// Alan adı uyarısında: kayıt adı ve kalan gün.
	Domain     string         `json:"domain,omitempty"`
	DomainDays *int           `json:"domain_days,omitempty"`
	Monitor    WebhookMonitor `json:"monitor"`
	// Locations çok konumlu monitörde çalışmayan / sonuç gelmeyen konumlar.
	Locations []LocationNote `json:"locations,omitempty"`
	// Server yalnızca sunucu uyarılarında (server_alert, server_resolved)
	// doludur; o zaman Monitor sunucunun adını ve host adını taşır, kimliği 0'dır.
	Server *WebhookServer `json:"server,omitempty"`
	// Incident down/up/hatırlatmada olayın kimliği ve sayfası.
	Incident *WebhookIncident `json:"incident,omitempty"`
	// Escalated: eskalasyon kanalına N dakikadır süren olay için gönderildi.
	// Delayed: gecikme kuralı ya da sessiz saatler yüzünden ertelenip sonra
	// gönderildi; ElapsedSeconds o ana kadar geçen süre.
	Escalated      bool  `json:"escalated,omitempty"`
	Delayed        bool  `json:"delayed,omitempty"`
	ElapsedSeconds int64 `json:"elapsed_seconds,omitempty"`
	// Ack yalnızca "acked" olayında: onaylayan kullanıcı ve notu.
	Ack *WebhookAck `json:"ack,omitempty"`
	// Slow yalnızca slow / slow_resolved olaylarında: pencere ortalaması ve eşik.
	Slow *WebhookSlow `json:"slow,omitempty"`
}

type WebhookSlow struct {
	AvgMs       int `json:"avg_ms"`
	ThresholdMs int `json:"threshold_ms"`
	Checks      int `json:"checks"`
}

type WebhookAck struct {
	By   string `json:"by"`
	Note string `json:"note,omitempty"`
}

type WebhookIncident struct {
	ID  int64  `json:"id"`
	URL string `json:"url,omitempty"`
}

type WebhookServer struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Metric    string  `json:"metric"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	Minutes   int     `json:"minutes"`
	// Level uyarı seviyesi (warning | critical; eski kurallarda boş = kritik);
	// Container "container" metriğinde konteyner adı; BootTime yeniden
	// başlatma bildiriminde yeni açılış (unix).
	Level     string `json:"level,omitempty"`
	Container string `json:"container,omitempty"`
	BootTime  int64  `json:"boot_time,omitempty"`
}

type WebhookMonitor struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
	URL    string `json:"url,omitempty"`
}

func (webhook) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c webhookConfig
	json.Unmarshal(raw, &c)
	p := WebhookPayload{
		Event: ev.Kind, Title: ev.Title(), Text: ev.Text(), Message: ev.LocalMessage(),
		Time:            ev.Time.Format(time.RFC3339),
		DowntimeSeconds: int64(ev.Downtime.Seconds()),
		Monitor:         WebhookMonitor{ID: ev.MonitorID, Name: ev.MonitorName, Type: ev.MonitorType, Target: ev.Target, URL: ev.URL},
		Locations:       ev.Locations,
		Escalated:       ev.Escalated, Delayed: ev.Delayed, ElapsedSeconds: int64(ev.Elapsed.Seconds()),
	}
	if ev.Kind == KindCert {
		d := ev.CertDays
		p.CertDays = &d
	}
	if ev.Kind == KindDomain {
		d := ev.DomainDays
		p.Domain, p.DomainDays = ev.Domain, &d
	}
	if ev.IncidentID != 0 {
		p.Incident = &WebhookIncident{ID: ev.IncidentID, URL: ev.IncidentURL}
	}
	if ev.Kind == KindAcked {
		p.Ack = &WebhookAck{By: ev.AckedBy, Note: ev.AckNote}
	}
	if ev.Kind == KindSlow || ev.Kind == KindSlowResolved {
		p.Slow = &WebhookSlow{AvgMs: int(ev.Value), ThresholdMs: int(ev.Threshold), Checks: ev.Checks}
	}
	if ev.ProbeID != 0 {
		p.Server = &WebhookServer{ID: ev.ProbeID, Name: ev.MonitorName, Metric: ev.Metric,
			Value: ev.Value, Threshold: ev.Threshold, Minutes: ev.Minutes, Level: ev.Level}
		if ev.Metric == "container" {
			p.Server.Container = ev.Mount
		}
		if !ev.BootTime.IsZero() {
			p.Server.BootTime = ev.BootTime.Unix()
		}
	}
	b, _ := json.Marshal(p)
	headers := map[string]string{"Content-Type": "application/json"}
	lines, _ := parseHeaderLines(c.Headers)
	for _, h := range lines {
		headers[h[0]] = h[1]
	}
	if c.Secret != "" {
		headers[WebhookSignatureHeader] = WebhookSignature(c.Secret, time.Now().Unix(), b)
	}
	return doRequest(ctx, c.Method, c.URL, bytes.NewReader(b), headers)
}

func parseHeaderLines(s string) ([][2]string, error) {
	var out [][2]string
	for i, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		k = strings.TrimSpace(k)
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			return nil, invalid("Başlık satırı %d geçersiz; biçim \"Ad: değer\" olmalı", i+1)
		}
		out = append(out, [2]string{k, strings.TrimSpace(v)})
	}
	return out, nil
}

// ntfy ----------------------------------------------------------------------------

type ntfyConfig struct {
	Server   string `json:"server"`
	Topic    string `json:"topic"`
	Token    string `json:"token"`
	Priority int    `json:"priority"` // 1-5, sorunlarda kullanılır
}

type ntfy struct{}

func (ntfy) Secrets() []string { return []string{"token"} }

func (ntfy) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c ntfyConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	if c.Server == "" {
		c.Server = "https://ntfy.sh"
	}
	if err := validURL("Sunucu", c.Server); err != nil {
		return nil, err
	}
	c.Topic = strings.TrimSpace(c.Topic)
	if err := required("Konu (topic)", c.Topic); err != nil {
		return nil, err
	}
	if c.Priority == 0 {
		c.Priority = 4
	}
	if c.Priority < 1 || c.Priority > 5 {
		return nil, invalid("Öncelik 1-5 arasında olmalı")
	}
	return encode(c), nil
}

func (ntfy) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c ntfyConfig
	json.Unmarshal(raw, &c)
	prio := 3
	if ev.IsProblem() {
		prio = c.Priority
	}
	// Başlık diğer kanallardaki gibi durum noktasıyla (🔴/🟢) gider; ntfy
	// etiketi (🚨/✅) eklenmez. ntfy, ASCII dışı başlıklar için RFC 2047
	// kodlamasını destekler; ham UTF-8 başlık araya giren proxy'lerde bozulabilir.
	headers := map[string]string{
		"Title":    mime.QEncoding.Encode("utf-8", ev.Title()),
		"Priority": strconv.Itoa(prio),
	}
	if u := ev.DetailURL(); u != "" {
		headers["Click"] = u
	}
	if c.Token != "" {
		headers["Authorization"] = "Bearer " + c.Token
	}
	return doRequest(ctx, http.MethodPost, c.Server+"/"+url.PathEscape(c.Topic), strings.NewReader(ev.Body()), headers)
}

// Gotify --------------------------------------------------------------------------

type gotifyConfig struct {
	Server   string `json:"server"`
	AppToken string `json:"app_token"`
	Priority int    `json:"priority"`
}

type gotify struct{}

func (gotify) Secrets() []string { return []string{"app_token"} }

func (gotify) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c gotifyConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Server = strings.TrimRight(strings.TrimSpace(c.Server), "/")
	if err := validURL("Sunucu", c.Server); err != nil {
		return nil, err
	}
	if err := required("Uygulama token'ı", c.AppToken); err != nil {
		return nil, err
	}
	if c.Priority == 0 {
		c.Priority = 8
	}
	if c.Priority < 1 || c.Priority > 10 {
		return nil, invalid("Öncelik 1-10 arasında olmalı")
	}
	return encode(c), nil
}

func (gotify) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c gotifyConfig
	json.Unmarshal(raw, &c)
	prio := 4
	if ev.IsProblem() {
		prio = c.Priority
	}
	return postJSON(ctx, c.Server+"/message", map[string]any{
		"title": ev.Title(), "message": ev.Body(), "priority": prio,
	}, map[string]string{"X-Gotify-Key": c.AppToken})
}

// Pushover ------------------------------------------------------------------------

type pushoverConfig struct {
	UserKey  string `json:"user_key"`
	AppToken string `json:"app_token"`
	Device   string `json:"device"`
	Priority int    `json:"priority"` // -2..2, sorunlarda kullanılır
}

type pushover struct{}

func (pushover) Secrets() []string { return []string{"user_key", "app_token"} }

func (pushover) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c pushoverConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	if err := required("Kullanıcı anahtarı", c.UserKey); err != nil {
		return nil, err
	}
	if err := required("Uygulama token'ı", c.AppToken); err != nil {
		return nil, err
	}
	// 2 (acil) onay/tekrar parametreleri ister; sade tutmak için 1'e kadar izin verilir.
	if c.Priority < -2 || c.Priority > 1 {
		return nil, invalid("Öncelik -2 ile 1 arasında olmalı")
	}
	return encode(c), nil
}

func (pushover) Send(ctx context.Context, raw json.RawMessage, ev Event) error {
	var c pushoverConfig
	json.Unmarshal(raw, &c)
	prio := 0
	if ev.IsProblem() {
		prio = c.Priority
	}
	form := url.Values{
		"token": {c.AppToken}, "user": {c.UserKey},
		"title": {ev.Title()}, "message": {ev.Body()}, "priority": {strconv.Itoa(prio)},
	}
	if c.Device != "" {
		form.Set("device", c.Device)
	}
	if u := ev.DetailURL(); u != "" {
		form.Set("url", u)
	}
	return doRequest(ctx, http.MethodPost, pushoverAPI, strings.NewReader(form.Encode()),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
}

// Derleme zamanı kontrolü: tüm sağlayıcılar arayüzü uyguluyor.
var _ = []Provider{whatsapp{}, telegram{}, discord{}, slack{}, webhook{}, ntfy{}, gotify{}, pushover{}, email{}}
