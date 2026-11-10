// Package eklenti, özel metrik eklentilerinin ve yerleşik denetimlerin ortak
// sonuç şeklini (Result) ve eklenti çıktısı ayrıştırıcılarını içerir
// (docs/GUVENLIK-EKLENTI-730.md B.2, G3). Saf pakettir: G/Ç, saat ya da
// global durum yoktur; ajan ve panel aynı kodu kullanır.
//
// Desteklenen biçimler:
//   - Nagios: dönüş kodu + "METİN | perfdata" (çok satırlı uzun çıktı dahil)
//   - JSON: bizim şemamız ({"status","message","metrics":{...}}) ya da
//     düz biçim (üst düzey sayısal anahtarlar, "units", "status" 1/0, "msg")
package eklenti

import (
	"math"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Sınırlar (B.1, B.2).
const (
	MaxOutput     = 64 << 10 // ayrıştırılan çıktı üst sınırı (bayt)
	MaxMetrics    = 50       // eklenti başına metrik
	MaxMessage    = 512      // ileti (karakter)
	MaxNameLen    = 64       // metrik adı
	maxUnitLen    = 16       // tanınmayan birim metni
	maxRangeInput = 64       // eşik metni (bayt)
)

// Status, denetimin durumudur.
type Status string

const (
	OK       Status = "ok"
	Warning  Status = "warning"
	Critical Status = "critical"
	Unknown  Status = "unknown"
)

// Valid, s'nin dört durumdan biri olup olmadığını söyler.
func (s Status) Valid() bool {
	switch s {
	case OK, Warning, Critical, Unknown:
		return true
	}
	return false
}

// Metric, tek bir ölçümdür. Value taban birimdedir (saniye, bayt); Warn/Crit
// de aynı birime çevrilmiştir.
type Metric struct {
	Name  string   `json:"name"`
	Value float64  `json:"value"`
	Unit  string   `json:"unit,omitempty"`
	Warn  *Range   `json:"warn,omitempty"`
	Crit  *Range   `json:"crit,omitempty"`
	Min   *float64 `json:"min,omitempty"`
	Max   *float64 `json:"max,omitempty"`
}

// Result, eklenti ya da yerleşik denetim sonucunun ortak şeklidir (G3).
type Result struct {
	Status  Status   `json:"status"`
	Message string   `json:"message"`
	Metrics []Metric `json:"metrics"`
	// Dropped, atılan metrik sayısıdır (geçersiz ad/değer, yinelenen ad,
	// NaN/Inf, sınır aşımı). Eklenti yazarına ipucu olarak gösterilir.
	Dropped int `json:"dropped,omitempty"`
	// Truncated, çıktının MaxOutput'ta kesildiğini söyler.
	Truncated bool `json:"truncated,omitempty"`
}

var nameRe = regexp.MustCompile(`^[a-zA-Z0-9_.\-]{1,64}$`)

// ValidName, adın metrik adı kuralına uyup uymadığını söyler.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// SanitizeName, etiketi metrik adına çevirir: izin verilmeyen her karakter
// '_' olur, 64 karakterde kesilir. Boş etiket için "" döner.
func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= MaxNameLen {
			break
		}
		switch {
		case r < utf8.RuneSelf && (r == '_' || r == '.' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')):
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// cleanMessage, iletiyi tek satıra indirir, denetim karakterlerini atar,
// geçersiz UTF-8'i düzeltir ve MaxMessage karakterde keser.
func cleanMessage(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	n := 0
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = b.Len() > 0
			continue
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			continue
		}
		if n >= MaxMessage {
			break
		}
		if space {
			if n+1 >= MaxMessage {
				break
			}
			b.WriteByte(' ')
			n++
			space = false
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// collector, metrikleri sınırlar altında toplar.
type collector struct {
	res  *Result
	seen map[string]bool
}

func newCollector(r *Result) *collector {
	return &collector{res: r, seen: map[string]bool{}}
}

// add, metriği doğrular ve ekler; atılırsa Dropped artar.
func (c *collector) add(m Metric) {
	if m.Name == "" || !ValidName(m.Name) || c.seen[m.Name] || !finite(m.Value) ||
		len(c.res.Metrics) >= MaxMetrics {
		c.res.Dropped++
		return
	}
	if m.Min != nil && !finite(*m.Min) {
		m.Min = nil
	}
	if m.Max != nil && !finite(*m.Max) {
		m.Max = nil
	}
	c.seen[m.Name] = true
	c.res.Metrics = append(c.res.Metrics, m)
}

// clip, girdiyi MaxOutput'a indirir; mümkünse son satır sonunda keser.
func clip(out []byte) ([]byte, bool) {
	if len(out) <= MaxOutput {
		return out, false
	}
	out = out[:MaxOutput]
	if i := strings.LastIndexByte(string(out), '\n'); i > 0 {
		out = out[:i]
	}
	return out, true
}

// Parse, biçime göre ayrıştırır ("nagios" ya da "json"). JSON hatası
// unknown durumlu sonuç olarak döner. exitCode < 0 zaman aşımı/sinyal
// demektir ve her biçimde unknown verir.
func Parse(format string, exitCode int, out []byte) Result {
	if format == "json" {
		r, err := ParseJSON(out)
		if err != nil {
			r = Result{Status: Unknown, Message: cleanMessage("invalid JSON output: " + err.Error()),
				Metrics: []Metric{}, Truncated: r.Truncated}
		}
		if exitCode < 0 {
			r.Status = Unknown
		}
		return r
	}
	return ParseNagios(exitCode, out)
}
