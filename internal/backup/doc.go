// Package backup yedek dosyası biçimini ve başka sistemlerden (Uptime Kuma,
// UptimeRobot) gelen verinin bu biçime çevrilmesini içerir. Veritabanına
// yazmaz: çıktı her zaman bir Doc'tur ve API'deki tek içe aktarma hattından
// (doğrulama, çakışma kontrolü, tek işlemde kayıt) geçer.
package backup

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Yedek dosyası biçimi. LegacyFormat ürünün eski adıyla yazılmış yedekler
// (v1.2.x ve öncesi) içindir; okunur, yeni dosyalar Format ile yazılır.
const (
	Format       = "bekci"
	LegacyFormat = "uptime-kadir"
	Version      = 1
)

// IsFormat dosyanın "format" alanı bu uygulamanın (yeni ya da eski adlı) yedeği mi?
func IsFormat(f string) bool { return f == Format || f == LegacyFormat }

// SecretsWarning dışa aktarılan dosyanın başına yazılan uyarı.
const SecretsWarning = "Bu dosya bildirim token'ları, şifreler ve API anahtarları gibi gizli bilgileri açık halde içerir; güvenli bir yerde saklayın ve kimseyle paylaşmayın."

// Doc yedek dosyası. Monitörler bildirim kanallarına adlarıyla, etiketlere
// adlarıyla bağlanır; durum sayfaları ve grup monitörleri monitörlere dosya
// içi kimlikleriyle (Monitor.ID) başvurur.
type Doc struct {
	Format        string             `json:"format"`
	Version       int                `json:"version"`
	ExportedAt    int64              `json:"exported_at"`
	AppVersion    string             `json:"app_version,omitempty"`
	Warning       string             `json:"warning,omitempty"`
	Settings      *store.AppSettings `json:"settings,omitempty"`
	Tags          []Tag              `json:"tags"`
	Notifications []Notification     `json:"notifications"`
	Monitors      []Monitor          `json:"monitors"`
	StatusPages   []Page             `json:"status_pages"`
	// Users yalnızca istenirse (POST /api/export {"users": true}); eski
	// yedeklerde yok. Geri yüklemede her zaman birleştirilir: var olan
	// kullanıcı adı atlanır, yenisi şifre özetiyle eklenir.
	Users []User `json:"users,omitempty"`
	// SystemMailChannel sistem e-postası (şifre sıfırlama) kanalının adı;
	// settings.system_mail_channel_id bu kurulumun kimliğidir, taşınmaz.
	SystemMailChannel string `json:"system_mail_channel,omitempty"`
}

// MonitorLocations monitörün konum ayarı (bkz. store.LocationSetup).
type MonitorLocations struct {
	IncludeLocal  bool     `json:"include_local"`
	Probes        []string `json:"probes"` // kontrol noktası adları
	DownWhen      string   `json:"down_when"`
	NotifyPartial bool     `json:"notify_partial,omitempty"`
}

// User yedekteki kullanıcı. PasswordHash bcrypt özetidir (şifre değil);
// TOTPSecret ve RecoveryCodes (özetler) yalnızca "2FA sırları dahil"
// seçilirse yazılır. MonitorIDs dosya içi monitör kimlikleridir.
type User struct {
	Username           string    `json:"username"`
	DisplayName        string    `json:"display_name,omitempty"`
	Email              string    `json:"email,omitempty"`
	Role               string    `json:"role"`
	Disabled           bool      `json:"disabled,omitempty"`
	MustChangePassword bool      `json:"must_change_password,omitempty"`
	AllMonitors        bool      `json:"all_monitors"`
	MonitorIDs         []int64   `json:"monitor_ids,omitempty"`
	TagRules           []TagRule `json:"tag_rules,omitempty"`
	Lang               string    `json:"lang,omitempty"`
	Theme              string    `json:"theme,omitempty"`
	PasswordHash       string    `json:"password_hash"`
	TOTPSecret         string    `json:"totp_secret,omitempty"`
	RecoveryCodes      []string  `json:"recovery_codes,omitempty"`
	TwoFactorEnabled   bool      `json:"two_factor_enabled,omitempty"`
}

type Tag struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Notification struct {
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Config    json.RawMessage `json:"config"`
	IsDefault bool            `json:"is_default"`
	Active    bool            `json:"active"`
	// Kurallar (eski yedeklerde yok → tüm türler, kural yok).
	Events      []string          `json:"events,omitempty"`
	QuietHours  *store.QuietHours `json:"quiet_hours,omitempty"`
	DelayMin    int               `json:"delay_min,omitempty"`
	EscalateMin int               `json:"escalate_min,omitempty"`
	Lang        string            `json:"lang,omitempty"`
	// TagRules etiket kuralları (etiket adıyla; eski yedeklerde yok).
	TagRules []TagRule `json:"tag_rules,omitempty"`
	Notes    []string  `json:"-"` // dönüştürmede oluşan uyarılar
}

// TagRule "etiket (= değer)" kuralı; etiket adıyla başvurur.
type TagRule struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type MonitorTag struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type Monitor struct {
	ID            int64           `json:"id"` // dosya içi kimlik
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Description   string          `json:"description"`
	Active        bool            `json:"active"`
	Interval      int             `json:"interval"`
	RetryInterval int             `json:"retry_interval"`
	MaxRetries    int             `json:"max_retries"`
	Timeout       int             `json:"timeout"`
	ResendEvery   int             `json:"resend_every"`
	UpsideDown    bool            `json:"upside_down"`
	Config        json.RawMessage `json:"config"`
	PushToken     string          `json:"push_token,omitempty"`
	SlowMs        int             `json:"slow_ms,omitempty"`       // yavaş yanıt eşiği (ms; 0 = kapalı)
	SlowChecks    int             `json:"slow_checks,omitempty"`   // ortalama penceresi (kontrol sayısı)
	DomainExpiry  *bool           `json:"domain_expiry,omitempty"` // alan adı bitiş uyarısı (yoksa açık)
	Notifications []string        `json:"notifications"`           // bildirim kanalı adları
	Tags          []MonitorTag    `json:"tags"`
	// Locations çok konumlu kontrol ayarı; kontrol noktaları adlarıyla (geri
	// yüklemede aynı adlı kontrol noktası aranır). Yoksa yalnızca ana sunucu.
	Locations *MonitorLocations `json:"locations,omitempty"`
	Notes     []string          `json:"-"`
}

type PageMonitor struct {
	MonitorID int64  `json:"monitor_id"` // Monitor.ID
	Name      string `json:"name,omitempty"`
}

type PageSection struct {
	Title    string        `json:"title"`
	Monitors []PageMonitor `json:"monitors"`
	// Tag etikete bağlı grup: etiketi taşıyan monitörler okunurken eklenir
	// (Monitors yalnızca açıkça eklenenleri taşır).
	Tag *TagRule `json:"tag,omitempty"`
}

type Announcement struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at,omitempty"`
}

// PageLayout durum sayfası dizilimi: yerleşim (list | grid | compact | rows),
// genişlik (narrow | wide), bölüm sırası/görünürlüğü ve uptime yüzdesi. Olaylar
// bölümünün görünürlüğü ShowIncidents'tır.
type PageLayout struct {
	Style  string      `json:"style"`
	Width  string      `json:"width"`
	Blocks []PageBlock `json:"blocks"`
	// ShowUptime eski yedeklerde yok → gösterilir.
	ShowUptime *bool `json:"show_uptime,omitempty"`
}

type PageBlock struct {
	ID      string `json:"id"`
	Visible bool   `json:"visible"`
}

type Page struct {
	Slug          string         `json:"slug"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Footer        string         `json:"footer"`
	CustomDomain  string         `json:"custom_domain,omitempty"`
	PasswordHash  string         `json:"password_hash,omitempty"` // bcrypt; şifrenin kendisi değil
	ShowTargets   bool           `json:"show_targets"`
	BarRange      string         `json:"bar_range,omitempty"`      // recent | 24h | 90d (eski yedeklerde yok → recent)
	ShowIncidents *bool          `json:"show_incidents,omitempty"` // eski yedeklerde yok → gösterilir
	Collapsible   bool           `json:"collapsible,omitempty"`
	Lang          string         `json:"lang,omitempty"`           // tr | en (eski yedeklerde yok → tr)
	Layout        *PageLayout    `json:"layout,omitempty"`         // eski yedeklerde yok → varsayılan dizilim
	IncidentDays  int            `json:"incident_days,omitempty"`  // olay penceresi (7/14/30/90; yok → 14)
	UptimeWindows []string       `json:"uptime_windows,omitempty"` // uptime pencereleri (yok → çubuk kapsamına göre)
	Published     bool           `json:"published"`
	Sections      []PageSection  `json:"sections"`
	Logo          []byte         `json:"logo,omitempty"` // base64
	LogoType      string         `json:"logo_type,omitempty"`
	Announcements []Announcement `json:"announcements"`
}

// Skipped dönüştürülemeyen bir kayıt.
type Skipped struct {
	Kind   string // monitor | notification
	Name   string
	Type   string // kaynak sistemdeki tip
	Reason string
}

// Result bir dönüştürmenin çıktısı.
type Result struct {
	Doc      *Doc
	Skipped  []Skipped
	Warnings []string // belirli bir kayda bağlı olmayan notlar
}

func newResult() *Result {
	return &Result{Doc: &Doc{Format: Format, Version: Version, Tags: []Tag{}, Notifications: []Notification{},
		Monitors: []Monitor{}, StatusPages: []Page{}}}
}

func (r *Result) skip(kind, name, typ, reason string) {
	r.Skipped = append(r.Skipped, Skipped{Kind: kind, Name: name, Type: typ, Reason: reason})
}

// Ortak yardımcılar -------------------------------------------------------------------

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
var shortHexRe = regexp.MustCompile(`^#[0-9a-fA-F]{3}$`)

// DefaultTagColor rengi bilinmeyen etiketler için.
const DefaultTagColor = "#64748b"

// NormalizeColor #rgb ve #rrggbb biçimlerini #rrggbb'ye çevirir; tanınmayan
// renkler (ör. CSS adları) için varsayılanı döner.
func NormalizeColor(c string) string {
	c = strings.TrimSpace(c)
	switch {
	case hexColorRe.MatchString(c):
		return strings.ToLower(c)
	case shortHexRe.MatchString(c):
		return strings.ToLower("#" + c[1:2] + c[1:2] + c[2:3] + c[2:3] + c[3:4] + c[3:4])
	}
	return DefaultTagColor
}

func clamp(v, lo, hi int) int {
	return max(lo, min(v, hi))
}

// UniqueName aynı adı taşıyan kayıtları "Ad (2)" biçiminde ayırır.
func UniqueName(name string, used map[string]bool) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = "Adsız"
	}
	if r := []rune(base); len(r) > 90 {
		base = string(r[:90])
	}
	n := base
	for i := 2; used[strings.ToLower(n)]; i++ {
		n = fmt.Sprintf("%s (%d)", base, i)
	}
	used[strings.ToLower(n)] = true
	return n
}

// kv esnek anahtar/değer satırı: SQLite satırı veya JSON nesnesi. Farklı
// sürümlerde aynı alanın farklı adları (snake_case / camelCase) olduğu için
// erişimciler birden çok anahtar alır ve ilk dolu olanı kullanır.
type kv map[string]any

func (m kv) raw(keys ...string) (any, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			return v, true
		}
	}
	return nil, false
}

func (m kv) str(keys ...string) string {
	v, ok := m.raw(keys...)
	if !ok {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	case []byte:
		return string(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int64:
		return strconv.FormatInt(x, 10)
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func (m kv) num(keys ...string) (float64, bool) {
	v, ok := m.raw(keys...)
	if !ok {
		return 0, false
	}
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string, []byte:
		f, err := strconv.ParseFloat(strings.TrimSpace(m.str(keys...)), 64)
		return f, err == nil
	}
	return 0, false
}

func (m kv) int(keys ...string) int {
	f, _ := m.num(keys...)
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return int(math.Round(max(-1e9, min(f, 1e9))))
}

func (m kv) bool(keys ...string) bool {
	v, ok := m.raw(keys...)
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	if s, ok := v.(string); ok {
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "true" || s == "1" || s == "yes" || s == "on"
	}
	f, _ := m.num(keys...)
	return f != 0
}

// has anahtarın var olup olmadığını (değeri null olsa bile) söyler.
func (m kv) has(key string) bool {
	_, ok := m[key]
	return ok
}
