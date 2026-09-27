// Package notify bildirim kanallarını içerir. Her servis Provider arayüzünü
// uygular ve init() içinde Register ile kendini kaydeder.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Olay türleri.
const (
	KindDown     = "down"
	KindUp       = "up"
	KindReminder = "reminder"
	KindCert     = "cert"
	KindTest     = "test"
)

// Event gönderilecek bildirimin içeriği.
type Event struct {
	Kind        string
	MonitorID   int64
	MonitorName string
	MonitorType string
	Target      string
	Message     string        // hata nedeni / son kontrol mesajı
	Time        time.Time     // olay zamanı
	Downtime    time.Duration // up ve reminder için kesinti süresi
	CertDays    int           // cert: kalan gün
	CertExpires time.Time     // cert: bitiş tarihi
	CertIssuer  string
	URL         string // monitörün arayüzdeki adresi (varsa)
}

// Title kısa başlık (e-posta konusu, push başlığı).
func (e Event) Title() string {
	switch e.Kind {
	case KindDown:
		return "🔴 " + e.MonitorName + " çalışmıyor"
	case KindUp:
		return "🟢 " + e.MonitorName + " tekrar çalışıyor"
	case KindReminder:
		return "🔴 " + e.MonitorName + " hâlâ çalışmıyor"
	case KindCert:
		if e.CertDays <= 0 {
			return "⚠️ " + e.MonitorName + ": SSL sertifikasının süresi doldu"
		}
		return fmt.Sprintf("⚠️ %s: SSL sertifikası %d gün içinde bitiyor", e.MonitorName, e.CertDays)
	case KindTest:
		return "✅ Test bildirimi"
	}
	return e.MonitorName
}

// Text başlık dahil tam düz metin.
func (e Event) Text() string {
	var b strings.Builder
	b.WriteString(e.Title())
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "\n%s: %s", k, v)
		}
	}
	if e.Kind == KindTest {
		b.WriteString("\nUptime bildirim kanalınız çalışıyor.")
	}
	line("Hedef", e.Target)
	switch e.Kind {
	case KindDown:
		line("Neden", e.Message)
	case KindUp:
		line("Kesinti süresi", FormatDuration(e.Downtime))
	case KindReminder:
		line("Kesinti süresi", FormatDuration(e.Downtime))
		line("Neden", e.Message)
	case KindCert:
		line("Bitiş", e.CertExpires.Local().Format("02.01.2006 15:04"))
		line("Veren", e.CertIssuer)
	}
	line("Zaman", e.Time.Local().Format("02.01.2006 15:04:05"))
	line("Detay", e.URL)
	return b.String()
}

// IsProblem olayın kötü haber olup olmadığı (öncelik/renk seçimi için).
func (e Event) IsProblem() bool {
	return e.Kind == KindDown || e.Kind == KindReminder || e.Kind == KindCert
}

// FormatDuration süreyi Türkçe kısa biçimde yazar: "45 sn", "12 dk", "2 sa 5 dk", "3 gün 4 sa".
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d sn", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d dk", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%d sa %d dk", h, m)
		}
		return fmt.Sprintf("%d sa", h)
	default:
		days := int(d.Hours()) / 24
		if h := int(d.Hours()) % 24; h > 0 {
			return fmt.Sprintf("%d gün %d sa", days, h)
		}
		return fmt.Sprintf("%d gün", days)
	}
}

type Provider interface {
	// Normalize ayarı doğrular ve varsayılanları doldurur.
	Normalize(cfg json.RawMessage) (json.RawMessage, error)
	Send(ctx context.Context, cfg json.RawMessage, ev Event) error
	// Secrets API'de maskelenecek alan adları.
	Secrets() []string
}

var registry = map[string]Provider{}

func Register(name string, p Provider) { registry[name] = p }

func Get(name string) (Provider, bool) {
	p, ok := registry[name]
	return p, ok
}

func Types() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ValidationError kullanıcı girdisindeki hatadır (API'de 400).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func invalid(format string, args ...any) error { return ValidationError(fmt.Sprintf(format, args...)) }

func decode(cfg json.RawMessage, v any) error {
	if len(cfg) == 0 || string(cfg) == "null" {
		cfg = json.RawMessage("{}")
	}
	dec := json.NewDecoder(bytes.NewReader(cfg))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("Geçersiz ayar: %v", err)
	}
	return nil
}

func encode(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

func required(name, v string) error {
	if strings.TrimSpace(v) == "" {
		return invalid("%s gerekli", name)
	}
	return nil
}

func validURL(name, v string) error {
	u, err := url.Parse(strings.TrimSpace(v))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return invalid("%s geçerli bir http(s) adresi olmalı", name)
	}
	return nil
}

// Maskeleme ------------------------------------------------------------------

// Mask API yanıtlarında gizli alanların yerine konan değer.
const Mask = "••••••"

// MaskSecrets gizli alanları maskeleyerek döner (boş olanlar boş kalır).
func MaskSecrets(typ string, cfg json.RawMessage) json.RawMessage {
	p, ok := Get(typ)
	if !ok {
		return cfg
	}
	var m map[string]any
	if json.Unmarshal(cfg, &m) != nil {
		return cfg
	}
	for _, k := range p.Secrets() {
		if s, ok := m[k].(string); ok && s != "" {
			m[k] = Mask
		}
	}
	return encode(m)
}

// MergeSecrets düzenlemede maskeli gelen gizli alanları eski değerle doldurur;
// böylece arayüz gizli değeri hiç görmeden kaydedebilir.
func MergeSecrets(typ string, newCfg, oldCfg json.RawMessage) json.RawMessage {
	p, ok := Get(typ)
	if !ok {
		return newCfg
	}
	var nm, om map[string]any
	if json.Unmarshal(newCfg, &nm) != nil {
		return newCfg
	}
	json.Unmarshal(oldCfg, &om)
	for _, k := range p.Secrets() {
		if nm[k] == Mask {
			nm[k] = om[k]
		}
	}
	return encode(nm)
}

// HTTP yardımcıları ---------------------------------------------------------------

var httpClient = &http.Client{Timeout: 20 * time.Second}

func doRequest(ctx context.Context, method, u string, body io.Reader, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "Uptime-Kadir/1.0")
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return redactURLError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return nil
}

// redactURLError ağ hatasındaki tam adresi çıkarır. Telegram token'ı ve
// Discord/Slack webhook adresleri gizli bilgidir; hata mesajları loglara ve
// arayüze gittiği için sadece şema ve sunucu adı kalır.
func redactURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	host := "?"
	if u, perr := url.Parse(ue.URL); perr == nil {
		host = u.Scheme + "://" + u.Host
	}
	return fmt.Errorf("%s %s/…: %w", ue.Op, host, ue.Err)
}

func postJSON(ctx context.Context, u string, payload any, headers map[string]string) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	h := map[string]string{"Content-Type": "application/json"}
	for k, v := range headers {
		h[k] = v
	}
	return doRequest(ctx, http.MethodPost, u, bytes.NewReader(b), h)
}
