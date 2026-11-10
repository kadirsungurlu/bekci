package eklenti

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// Range, Nagios eşik aralığıdır. Varsayılan anlam: değer [Start, End]
// dışındaysa uyarı. Inside (@) ise değer aralığın içindeyse (uçlar dahil)
// uyarı. Sınırsız uçlar -Inf/+Inf ile tutulur.
//
// Söz dizimi: "10" (0..10), "10:" (10..+inf), "~:10" (-inf..10),
// "10:20", "@10:20" (içerideyse uyarı).
type Range struct {
	Start  float64
	End    float64
	Inside bool
}

var errRange = errors.New("geçersiz aralık")

// ParseRange, Nagios aralık metnini çözer.
func ParseRange(s string) (Range, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > maxRangeInput {
		return Range{}, errRange
	}
	r := Range{Start: 0, End: math.Inf(1)}
	if s[0] == '@' {
		r.Inside = true
		s = s[1:]
	}
	start, end, hasColon := strings.Cut(s, ":")
	if !hasColon {
		v, err := num(start)
		if err != nil {
			return Range{}, errRange
		}
		r.End = v
	} else {
		switch start {
		case "~":
			r.Start = math.Inf(-1)
		case "":
			// "":20 Nagios'ta 0:20 sayılır.
		default:
			v, err := num(start)
			if err != nil {
				return Range{}, errRange
			}
			r.Start = v
		}
		if end != "" {
			v, err := num(end)
			if err != nil {
				return Range{}, errRange
			}
			r.End = v
		}
	}
	if r.Start > r.End {
		return Range{}, errRange
	}
	return r, nil
}

// num, sonlu ondalık sayı çözer ("inf", "nan", onaltılık reddedilir).
func num(s string) (float64, error) {
	if s == "" || !numeric(s) {
		return 0, errRange
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || !finite(v) {
		return 0, errRange
	}
	return v, nil
}

// numeric, s'nin yalnız ondalık sayı karakterlerinden oluştuğunu denetler.
func numeric(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && c != '.' && c != '-' && c != '+' && c != 'e' && c != 'E' {
			return false
		}
	}
	return true
}

// Alert, v değerinin bu eşiği aşıp aşmadığını söyler.
func (r Range) Alert(v float64) bool {
	in := v >= r.Start && v <= r.End
	if r.Inside {
		return in
	}
	return !in
}

// scale, uçları birim çarpanıyla taban birime çevirir.
func (r Range) scale(f float64) Range {
	if f == 1 {
		return r
	}
	r.Start *= f
	r.End *= f
	return r
}

// String, aralığı Nagios söz diziminde (kısa biçim) yazar.
func (r Range) String() string {
	var b strings.Builder
	if r.Inside {
		b.WriteByte('@')
	}
	fs := func(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }
	switch {
	case r.Start == 0 && !math.IsInf(r.End, 1):
		b.WriteString(fs(r.End))
	default:
		if math.IsInf(r.Start, -1) {
			b.WriteByte('~')
		} else {
			b.WriteString(fs(r.Start))
		}
		b.WriteByte(':')
		if !math.IsInf(r.End, 1) {
			b.WriteString(fs(r.End))
		}
	}
	return b.String()
}

// MarshalJSON, aralığı Nagios metni olarak yazar.
func (r Range) MarshalJSON() ([]byte, error) { return json.Marshal(r.String()) }

// UnmarshalJSON, Nagios metni ya da sayı kabul eder.
func (r *Range) UnmarshalJSON(b []byte) error {
	var s string
	if len(b) > 0 && b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	} else {
		s = string(b)
	}
	v, err := ParseRange(s)
	if err != nil {
		return err
	}
	*r = v
	return nil
}

// rangePtr, metni çözer; boş ya da geçersizse nil döner (yalnız eşik atılır).
func rangePtr(s string, f float64) *Range {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	r, err := ParseRange(s)
	if err != nil {
		return nil
	}
	r = r.scale(f)
	// Taşma: çevrilen uç sonsuza giderse anlamlı yönde değilse at.
	if math.IsNaN(r.Start) || math.IsNaN(r.End) || math.IsInf(r.Start, 1) || math.IsInf(r.End, -1) {
		return nil
	}
	return &r
}

// unitFactor, birimi taban birime çevirir (saniye, bayt). Bayt katları
// 1024 tabanlıdır (Nagios eklentilerinin yerleşik alışkanlığı). Tanınmayan
// ama düzgün birim aynen kalır; düzgün değilse birim boşaltılır.
func unitFactor(u string) (string, float64) {
	switch u {
	case "":
		return "", 1
	case "s":
		return "s", 1
	case "ms":
		return "s", 1e-3
	case "us", "µs":
		return "s", 1e-6
	case "%":
		return "%", 1
	case "B":
		return "B", 1
	case "KB", "kB", "KiB":
		return "B", 1 << 10
	case "MB", "MiB":
		return "B", 1 << 20
	case "GB", "GiB":
		return "B", 1 << 30
	case "TB", "TiB":
		return "B", 1 << 40
	case "c":
		return "c", 1
	}
	if len(u) > maxUnitLen {
		return "", 1
	}
	for i := 0; i < len(u); i++ {
		c := u[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '/' || c == '_') {
			return "", 1
		}
	}
	return u, 1
}
