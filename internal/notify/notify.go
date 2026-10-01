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
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/i18n"
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
	// yazılır (docs/PLAN.md §13): monitör olayları (down/up/reminder) ve sunucu
	// olayları (server_alert/server_resolved).
	// IncidentURL olay sayfasının adresi; varsa "Detay" bağlantısı odur.
	IncidentID  int64
	IncidentURL string

	// Sunucu uyarıları: ProbeID doluysa olay bir sunucuya (ajana) aittir;
	// MonitorName sunucunun adı, Target host adıdır. Metric: cpu, mem, swap,
	// disk, load, temp, net, offline. Value ortalama değer (offline: veri gelmeyen
	// dakika), Threshold eşik, Minutes ortalama penceresi.
	ProbeID   int64
	Metric    string
	Value     float64
	Threshold float64
	Minutes   int
	Mount     string // disk uyarısında bölüm (ör. "/home")

	// LastSeen çevrimdışı uyarısında son verinin zamanı; GoneMinutes değeri
	// artık gelmeyen metrikte kaç dakikadır gelmediği. "Ayrıntı" satırı
	// bunlardan bildirim dilinde üretilir (ikisi de boşsa Message).
	LastSeen    time.Time
	GoneMinutes int

	// Sample örnek bildirim (gerçek bir olay değil); metne not düşülür.
	Sample bool

	// Locations çok konumlu monitörde çalışmayan / sonuç gelmeyen konumlar:
	// doluysa "Neden" yerine her konum ayrı satırda yazılır.
	Locations []LocationNote

	// Lang bildirim metninin dili (tr/en). Boşsa Dispatcher ayarlardaki
	// bildirim dilini (AppSettings.NotifyLang) doldurur; yine boşsa tr.
	Lang string
}

// MetricName sunucu metriğinin bildirimlerdeki adı ("Yük" / "Load").
func MetricName(lang, metric string) string {
	if k := "metric." + metric; i18n.Has(k) {
		return i18n.T(lang, k)
	}
	return metric
}

// FormatMetric sunucu metriğinin değerini birimiyle yazar: tr "%94", "1,25",
// "72 °C"; en "94%", "1.25", "72 °C".
func FormatMetric(lang, metric string, v float64) string { return i18n.MetricValue(lang, metric, v) }

// serverTitle sunucu uyarısının başlığı.
func (e Event) serverTitle() string {
	l := e.Lang
	name := MetricName(l, e.Metric)
	switch {
	case e.Metric == "offline" && e.Kind == KindServerAlert:
		return i18n.T(l, "notify.server.offline", e.MonitorName)
	case e.Metric == "offline":
		return i18n.T(l, "notify.server.online", e.MonitorName)
	case e.Kind == KindServerAlert:
		detail := i18n.T(l, "notify.server.avg")
		if e.Metric == "load" {
			detail = i18n.T(l, "notify.server.avg_per_core")
		}
		if e.Mount != "" {
			name += " (" + e.Mount + ")"
		}
		return i18n.T(l, "notify.server.alert", e.MonitorName, name,
			FormatMetric(l, e.Metric, e.Value), e.Minutes, detail, FormatMetric(l, e.Metric, e.Threshold))
	}
	if e.Mount != "" {
		name += " (" + e.Mount + ")"
	}
	return i18n.T(l, "notify.server.resolved", e.MonitorName, name)
}

// Title kısa başlık (e-posta konusu, push başlığı).
func (e Event) Title() string {
	l := e.Lang
	switch e.Kind {
	case KindDown:
		return i18n.T(l, "notify.down.title", e.MonitorName)
	case KindUp:
		return i18n.T(l, "notify.up.title", e.MonitorName)
	case KindReminder:
		return i18n.T(l, "notify.reminder.title", e.MonitorName)
	case KindCert:
		if e.CertDays <= 0 {
			return i18n.T(l, "notify.cert.expired", e.MonitorName)
		}
		return i18n.TN(l, "notify.cert.expiring", e.CertDays, e.MonitorName, e.CertDays)
	case KindTest:
		return i18n.T(l, "notify.test.title")
	case KindServerAlert, KindServerResolved:
		return e.serverTitle()
	}
	return e.MonitorName
}

// LocationNote bildirimde bir konumun satırı. NoData: konumdan sonuç gelmiyor.
type LocationNote struct {
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
	NoData  bool   `json:"no_data,omitempty"`
}

// Text başlık dahil tam düz metin (başlığı ayrı alanda gitmeyen kanallar).
func (e Event) Text() string { return e.Title() + "\n" + e.Body() }

// Row bildirim gövdesinin bir satırı: etiket ve değer; Locations doluysa
// değer yerine her konum ayrı satırda yazılır.
type Row struct {
	Key       string // notify.field.* anahtarı (link, time… kanal biçimi için)
	Label     string
	Value     string
	Locations []LocRow
}

// LocRow konum satırı (bildirim dilinde). NoData: sonuç gelmiyor.
type LocRow struct {
	Name, Message string
	NoData        bool
}

// Notes test/örnek bildirim açıklamaları (gövdenin başında).
func (e Event) Notes() []string {
	var out []string
	if e.Kind == KindTest {
		out = append(out, i18n.T(e.Lang, "notify.test.body", brand.Name))
	}
	if e.Sample {
		out = append(out, i18n.T(e.Lang, "notify.sample.note"))
	}
	return out
}

// Rows gövde satırları (düz metin ve e-posta HTML'i aynı satırlardan üretilir).
func (e Event) Rows() []Row {
	l := e.Lang
	var rows []Row
	add := func(key, v string) {
		if v != "" {
			rows = append(rows, Row{Key: key, Label: i18n.T(l, "notify.field."+key), Value: v})
		}
	}
	if e.ProbeID != 0 {
		add("server", e.Target)
	} else {
		add("target", e.Target)
	}
	reason := func() {
		if len(e.Locations) == 0 {
			add("reason", e.LocalMessage())
			return
		}
		r := Row{Key: "locations", Label: i18n.T(l, "notify.field.locations")}
		for _, n := range e.Locations {
			lr := LocRow{Name: n.Name, Message: i18n.Message(l, n.Message), NoData: n.NoData}
			if n.NoData {
				lr.Message = i18n.T(l, "notify.loc.no_data")
			}
			r.Locations = append(r.Locations, lr)
		}
		rows = append(rows, r)
	}
	switch e.Kind {
	case KindDown:
		reason()
	case KindUp:
		add("downtime", i18n.Duration(l, e.Downtime))
	case KindReminder:
		add("downtime", i18n.Duration(l, e.Downtime))
		reason()
	case KindCert:
		add("expires", i18n.DateTimeMin(l, e.CertExpires.Local()))
		add("issuer", e.CertIssuer)
	case KindServerAlert, KindServerResolved:
		if e.Metric != "offline" && e.Kind == KindServerResolved {
			add("last_avg", FormatMetric(l, e.Metric, e.Value))
		}
		add("info", e.info())
	}
	add("time", i18n.DateTime(l, e.Time.Local()))
	add("link", e.DetailURL())
	return rows
}

// Body başlıksız gövde: başlığı ayrı gönderen kanallar (ntfy, Gotify,
// Pushover, e-posta konusu…) başlığı gövdede tekrarlamasın.
func (e Event) Body() string {
	var lines []string
	lines = append(lines, e.Notes()...)
	for _, r := range e.Rows() {
		if r.Locations == nil {
			lines = append(lines, r.Label+": "+r.Value)
			continue
		}
		lines = append(lines, r.Label+":")
		for _, lr := range r.Locations {
			lines = append(lines, "• "+lr.Name+": "+lr.Message)
		}
	}
	return strings.Join(lines, "\n")
}

// info sunucu uyarısının "Ayrıntı" satırı: yapılandırılmış alanlardan (dile
// göre) üretilir, yoksa Message.
func (e Event) info() string {
	switch {
	case !e.LastSeen.IsZero():
		return i18n.T(e.Lang, "notify.server.last_data", i18n.DateTime(e.Lang, e.LastSeen.Local()))
	case e.GoneMinutes > 0:
		return i18n.T(e.Lang, "notify.server.value_gone", e.GoneMinutes)
	}
	return e.LocalMessage()
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
// Örnek ve test bildirimleri ayrı bir ad alanı kullanır: gerçek monitör/sunucu
// kimliğini taşısalar bile gerçek olayları açıp kapatamazlar.
func (e Event) AlertKey() string {
	if e.Kind == KindTest {
		return "uptime-test"
	}
	prefix := "uptime-"
	if e.Sample {
		prefix = "uptime-sample-"
	}
	switch {
	case e.ProbeID != 0:
		return fmt.Sprintf("%sserver-%d-%s", prefix, e.ProbeID, e.Metric)
	case e.Kind == KindCert:
		return fmt.Sprintf("%smonitor-%d-cert", prefix, e.MonitorID)
	}
	return fmt.Sprintf("%smonitor-%d", prefix, e.MonitorID)
}

// FormatDuration süreyi kısa biçimde yazar (tr: "45 sn", "2 sa 5 dk"; en: "45s", "2h 5m").
func FormatDuration(lang string, d time.Duration) string { return i18n.Duration(lang, d) }

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

// ErrSecretBound maskeli gizli alan, gizli bilgiye bağlı bir ayar (sorgu,
// kullanıcı adı, veritabanı vb.) değiştirilmiş halde gönderildi.
var ErrSecretBound = ValidationError("Bağlantı ayarları (sorgu, kullanıcı adı, veritabanı vb.) değiştiği için şifre/token gibi gizli alanları yeniden girmeniz gerekiyor")

// destinationKeys gizli bilginin gönderildiği yeri belirleyen alanlar. Bunlardan
// biri değişirse kayıtlı gizli bilgi maskeli değerle yeni hedefe taşınamaz;
// aksi halde gizli bilgiyi hiç görmemiş biri adresi kendi sunucusuna çevirip
// "test gönder" ile onu ele geçirebilirdi.
var destinationKeys = []string{
	"url", "host", "port", "server", "endpoint", "webhook_url", "homeserver_url",
	"broker_url", "target", "proxy_url", "oauth_token_url", "token_url",
}

// DestinationChanged yeni ayarda hedef alanlarından biri eskisinden farklı mı?
// secrets verilirse, kendisi gizli olup maskeli gönderilen hedef alanı
// değişmemiş sayılır: ör. Discord'da webhook_url hem hedef hem gizlidir;
// arayüz onu maskeli geri gönderir ve birleştirmede eski değer korunur.
func DestinationChanged(nm, om map[string]any, secrets ...string) bool {
	norm := func(v any) string {
		if v == nil {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
	}
	for _, k := range destinationKeys {
		if nm[k] == Mask && slices.Contains(secrets, k) {
			continue
		}
		if norm(nm[k]) != norm(om[k]) {
			return true
		}
	}
	return false
}

// boundChanged gizli bilgiye bağlı alanlardan biri değişti mi? Boş değerler
// (yok, "", 0, false) eşit sayılır; metinler büyük/küçük harf duyarlıdır
// (SQL sorgusu gibi).
func boundChanged(keys []string, nm, om map[string]any) bool {
	norm := func(v any) string {
		switch x := v.(type) {
		case nil:
			return ""
		case bool:
			if !x {
				return ""
			}
		case float64:
			if x == 0 {
				return ""
			}
		case string:
			return strings.TrimSpace(x)
		}
		b, _ := json.Marshal(v)
		return string(b)
	}
	for _, k := range keys {
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
	return MergeSecretsBound(secrets, nil, nil, newCfg, oldCfg)
}

// MergeSecretsBound MergeSecretsFor gibidir; ek olarak bound alanlarından biri
// değiştiyse de maskeli değeri kabul etmez (ErrSecretBound). Ör. veritabanı
// monitöründe kayıtlı şifreyle başka bir sorgu çalıştırıp sonucunu okumayı
// engeller: sorgu değişiyorsa şifre yeniden girilmelidir. normalize verilirse
// karşılaştırma iki ayarın normalize edilmiş halleri üzerinde yapılır (eksik
// gönderilen alan varsayılanıyla eşit sayılsın diye).
func MergeSecretsBound(secrets, bound []string, normalize func(json.RawMessage) (json.RawMessage, error), newCfg, oldCfg json.RawMessage) (json.RawMessage, error) {
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
	if DestinationChanged(nm, om, secrets...) {
		return nil, ErrSecretRebind
	}
	for _, k := range secrets {
		if nm[k] == Mask {
			nm[k] = om[k]
		}
	}
	merged := encode(nm)
	if len(bound) > 0 {
		a, b := nm, om
		if normalize != nil {
			if na, err := normalize(merged); err == nil {
				if nb, err := normalize(oldCfg); err == nil {
					var ma, mb map[string]any
					if json.Unmarshal(na, &ma) == nil && json.Unmarshal(nb, &mb) == nil {
						a, b = ma, mb
					}
				}
			}
		}
		if boundChanged(bound, a, b) {
			return nil, ErrSecretBound
		}
	}
	return merged, nil
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
		// Uzak yanıt gövdesi mesaja EKLENMEZ: iç bir servise yönlendirilen
		// istekte gövde/hata sayfası kullanıcıya iç veri sızdırabilir. Yalnızca
		// durum kodu ve (varsa) Türkçe ipucu bırakılır; gövde okunmadan atılır.
		msg := fmt.Sprintf("HTTP %d", resp.StatusCode)
		switch {
		case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
			msg += " (yetki hatası: anahtar/token doğru mu?)"
		case resp.StatusCode == http.StatusNotFound:
			msg += " (adres bulunamadı: yol doğru mu?)"
		case resp.StatusCode == http.StatusBadGateway || resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusGatewayTimeout:
			msg += " (hedef sunucu yanıt vermiyor: adres doğru ve servis çalışıyor mu?)"
		}
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
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

// LocalMessage kontrol mesajı bildirim dilinde: kayıtlı (Türkçe) kontrol
// mesajları okunurken çevrilir (bkz. i18n.Message); bilinmeyen metin aynen döner.
func (e Event) LocalMessage() string { return i18n.Message(e.Lang, e.Message) }
