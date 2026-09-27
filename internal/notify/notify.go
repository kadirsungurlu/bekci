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

	// Sunucu takibi: eşik uyarısı başladı / bitti (ProbeID dolu, MonitorID 0).
	KindServerAlert    = "server_alert"
	KindServerResolved = "server_resolved"
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

	// IncidentID doluysa her kanalın gönderim sonucu olayın işlem geçmişine
	// yazılır (docs/PLAN.md §13). Yalnızca monitör olaylarında (down/up/reminder).
	// IncidentURL olay sayfasının adresi; varsa "Detay" bağlantısı odur.
	IncidentID  int64
	IncidentURL string

	// Sunucu uyarıları: ProbeID doluysa olay bir sunucuya (ajana) aittir;
	// MonitorName sunucunun adı, Target host adıdır. Metric: cpu, mem, swap,
	// disk, load, temp, offline. Value ortalama değer (offline: veri gelmeyen
	// dakika), Threshold eşik, Minutes ortalama penceresi.
	ProbeID   int64
	Metric    string
	Value     float64
	Threshold float64
	Minutes   int
	Mount     string // disk uyarısında bölüm (ör. "/home")

	// Sample örnek bildirim (gerçek bir olay değil); metne not düşülür.
	Sample bool
}

// metricNames sunucu metriklerinin bildirimlerdeki adları.
var metricNames = map[string]string{
	"cpu": "CPU", "mem": "RAM", "swap": "Swap", "disk": "Disk", "load": "Yük", "temp": "Sıcaklık",
}

// FormatMetric sunucu metriğinin değerini birimiyle yazar: "%94", "1,25", "72 °C".
func FormatMetric(metric string, v float64) string {
	switch metric {
	case "load":
		return strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
	case "temp":
		return fmt.Sprintf("%.0f °C", v)
	}
	return fmt.Sprintf("%%%.0f", v)
}

// serverTitle sunucu uyarısının başlığı.
func (e Event) serverTitle() string {
	name := metricNames[e.Metric]
	if name == "" {
		name = e.Metric
	}
	switch {
	case e.Metric == "offline" && e.Kind == KindServerAlert:
		return "🔴 " + e.MonitorName + ": sunucudan veri gelmiyor"
	case e.Metric == "offline":
		return "🟢 " + e.MonitorName + ": tekrar veri gönderiyor"
	case e.Kind == KindServerAlert:
		detail := "ortalama"
		if e.Metric == "load" {
			detail = "ortalama, çekirdek başına"
		}
		if e.Mount != "" {
			name += " (" + e.Mount + ")"
		}
		return fmt.Sprintf("🔴 %s: %s %s (%d dk %s, eşik %s)", e.MonitorName, name,
			FormatMetric(e.Metric, e.Value), e.Minutes, detail, FormatMetric(e.Metric, e.Threshold))
	}
	if e.Mount != "" {
		name += " (" + e.Mount + ")"
	}
	return "🟢 " + e.MonitorName + ": " + name + " normale döndü"
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
	case KindServerAlert, KindServerResolved:
		return e.serverTitle()
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
	if e.Sample {
		b.WriteString("\n(Örnek bildirim — gerçek bir olay değil)")
	}
	if e.ProbeID != 0 {
		line("Sunucu", e.Target)
	} else {
		line("Hedef", e.Target)
	}
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
	case KindServerAlert, KindServerResolved:
		if e.Metric != "offline" && e.Kind == KindServerResolved {
			line("Son ortalama", FormatMetric(e.Metric, e.Value))
		}
		line("Ayrıntı", e.Message)
	}
	line("Zaman", e.Time.Local().Format("02.01.2006 15:04:05"))
	line("Detay", e.DetailURL())
	return b.String()
}

// DetailURL bildirimdeki "Detay" bağlantısı: olay sayfası, yoksa monitör sayfası.
func (e Event) DetailURL() string {
	if e.IncidentURL != "" {
		return e.IncidentURL
	}
	return e.URL
}

// IsProblem olayın kötü haber olup olmadığı (öncelik/renk seçimi için).
func (e Event) IsProblem() bool {
	return e.Kind == KindDown || e.Kind == KindReminder || e.Kind == KindCert || e.Kind == KindServerAlert
}

// IsRecovery sorunun bittiğini bildiren olay mı (monitör tekrar çalışıyor,
// sunucu uyarısı bitti). Olay kapatan servisler (PagerDuty, Opsgenie) için.
func (e Event) IsRecovery() bool { return e.Kind == KindUp || e.Kind == KindServerResolved }

// AlertKey olayın dış servislerdeki kimliği: aynı sorunun başlangıç ve bitiş
// olayları aynı anahtarı taşır (PagerDuty dedup_key, Opsgenie alias).
func (e Event) AlertKey() string {
	switch {
	case e.ProbeID != 0:
		return fmt.Sprintf("uptime-server-%d-%s", e.ProbeID, e.Metric)
	case e.Kind == KindCert:
		return fmt.Sprintf("uptime-monitor-%d-cert", e.MonitorID)
	}
	return fmt.Sprintf("uptime-monitor-%d", e.MonitorID)
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

// ErrSecretRebind maskeli gizli alan, hedef adres değiştirilmiş bir ayarla
// birlikte gönderildi.
var ErrSecretRebind = ValidationError("Hedef adres değiştiği için şifre/token gibi gizli alanları yeniden girmeniz gerekiyor")

// destinationKeys gizli bilginin gönderildiği yeri belirleyen alanlar. Bunlardan
// biri değişirse kayıtlı gizli bilgi maskeli değerle yeni hedefe taşınamaz;
// aksi halde gizli bilgiyi hiç görmemiş biri adresi kendi sunucusuna çevirip
// "test gönder" ile onu ele geçirebilirdi.
var destinationKeys = []string{
	"url", "host", "port", "server", "endpoint", "webhook_url", "homeserver_url",
	"broker_url", "target", "proxy_url", "oauth_token_url", "token_url",
}

// DestinationChanged yeni ayarda hedef alanlarından biri eskisinden farklı mı?
func DestinationChanged(nm, om map[string]any) bool {
	norm := func(v any) string {
		if v == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	}
	for _, k := range destinationKeys {
		if norm(nm[k]) != norm(om[k]) {
			return true
		}
	}
	return false
}

// MergeSecretsFor düzenlemede maskeli gelen gizli alanları eski değerle
// doldurur; böylece arayüz gizli değeri hiç görmeden kaydedebilir. Hedef adres
// değiştiyse maskeli değer kabul edilmez (ErrSecretRebind).
func MergeSecretsFor(secrets []string, newCfg, oldCfg json.RawMessage) (json.RawMessage, error) {
	var nm, om map[string]any
	if json.Unmarshal(newCfg, &nm) != nil {
		return newCfg, nil
	}
	json.Unmarshal(oldCfg, &om)
	masked := false
	for _, k := range secrets {
		if nm[k] == Mask {
			masked = true
		}
	}
	if !masked {
		return newCfg, nil
	}
	if DestinationChanged(nm, om) {
		return nil, ErrSecretRebind
	}
	for _, k := range secrets {
		if nm[k] == Mask {
			nm[k] = om[k]
		}
	}
	return encode(nm), nil
}

// MergeSecrets bildirim kanalı için MergeSecretsFor.
func MergeSecrets(typ string, newCfg, oldCfg json.RawMessage) (json.RawMessage, error) {
	p, ok := Get(typ)
	if !ok {
		return newCfg, nil
	}
	return MergeSecretsFor(p.Secrets(), newCfg, oldCfg)
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
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			msg += " (yetki hatası: anahtar/token doğru mu?)"
		case resp.StatusCode == http.StatusNotFound:
			msg += " (adres bulunamadı: yol doğru mu?)"
		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
			msg += " (hedef sunucu yanıt vermiyor: adres doğru ve servis çalışıyor mu?)"
		}
		// HTML hata sayfaları (proxy) mesajı kalabalıklaştırır; yalnızca düz metin/JSON eklenir.
		if t := strings.TrimSpace(string(snippet)); t != "" && !strings.HasPrefix(t, "<") {
			msg += ": " + t
		}
		return errors.New(msg)
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
