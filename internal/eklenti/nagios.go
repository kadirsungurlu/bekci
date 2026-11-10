package eklenti

import (
	"strconv"
	"strings"
)

// ParseNagios, Nagios eklenti sözleşmesine göre ayrıştırır: dönüş kodu
// 0/1/2/3 = ok/warning/critical/unknown, diğer her kod (zaman aşımı, sinyal
// için negatif dahil) unknown. Çıktının ilk satırı "METİN | perfdata";
// sonraki satırlarda ilk '|' görüldükten sonraki her şey ek perfdata'dır.
func ParseNagios(exitCode int, out []byte) Result {
	res := Result{Status: Unknown, Metrics: []Metric{}}
	switch exitCode {
	case 0:
		res.Status = OK
	case 1:
		res.Status = Warning
	case 2:
		res.Status = Critical
	}
	out, res.Truncated = clip(out)
	text := strings.ToValidUTF8(string(out), "�")
	text = strings.ReplaceAll(text, "\r\n", "\n")

	first, rest, _ := strings.Cut(text, "\n")
	msg, perf, _ := strings.Cut(first, "|")
	res.Message = cleanMessage(msg)

	var pd strings.Builder
	pd.WriteString(perf)
	if i := strings.IndexByte(rest, '|'); i >= 0 {
		pd.WriteByte(' ')
		pd.WriteString(rest[i+1:])
	}
	c := newCollector(&res)
	parsePerfdata(pd.String(), c)
	return res
}

func isSpace(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f' }

// parsePerfdata, boşlukla ayrılmış 'etiket'=değer[UOM];warn;crit;min;max
// öğelerini okur. Tek tırnaklı etikette '' bir tırnak demektir. Bozuk öğe
// atlanır, okuma bir sonraki öğeden sürer.
func parsePerfdata(s string, c *collector) {
	i := 0
	for i < len(s) {
		for i < len(s) && isSpace(s[i]) {
			i++
		}
		if i >= len(s) {
			return
		}
		// Etiket.
		var label string
		ok := true
		if s[i] == '\'' {
			var b strings.Builder
			j := i + 1
			closed := false
			for j < len(s) {
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					closed = true
					j++
					break
				}
				b.WriteByte(s[j])
				j++
			}
			label = b.String()
			if !closed {
				// Kapanmayan tırnak: yalnız bu öğe (ilk boşluğa kadar) atılır.
				j = i
				for j < len(s) && !isSpace(s[j]) {
					j++
				}
				c.res.Dropped++
				i = j
				continue
			}
			i = j
			if i >= len(s) || s[i] != '=' {
				ok = false
			}
		} else {
			j := i
			for j < len(s) && s[j] != '=' && !isSpace(s[j]) {
				j++
			}
			label = s[i:j]
			i = j
			if i >= len(s) || s[i] != '=' {
				ok = false
			}
		}
		// Değer alanı: boşluğa kadar.
		j := i
		for j < len(s) && !isSpace(s[j]) {
			j++
		}
		if !ok {
			c.res.Dropped++
			i = j
			continue
		}
		field := s[i+1 : j]
		i = j
		m, good := perfItem(label, field)
		if !good {
			c.res.Dropped++
			continue
		}
		c.add(m)
	}
}

// perfItem, "değer[UOM];warn;crit;min;max" alanını çözer.
func perfItem(label, field string) (Metric, bool) {
	parts := strings.Split(field, ";")
	vu := parts[0]
	n := numPrefix(vu)
	if n == 0 {
		return Metric{}, false // "U" (belirsiz) dahil
	}
	raw := vu[:n]
	if !strings.Contains(raw, ".") {
		raw = strings.Replace(raw, ",", ".", 1) // yerel ondalık virgül
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || !finite(v) {
		return Metric{}, false
	}
	unit, f := unitFactor(vu[n:])
	m := Metric{Name: SanitizeName(label), Value: v * f, Unit: unit}
	at := func(k int) string {
		if k < len(parts) {
			return parts[k]
		}
		return ""
	}
	m.Warn = rangePtr(at(1), f)
	m.Crit = rangePtr(at(2), f)
	if x, err := num(strings.TrimSpace(at(3))); err == nil && finite(x*f) {
		x *= f
		m.Min = &x
	}
	if x, err := num(strings.TrimSpace(at(4))); err == nil && finite(x*f) {
		x *= f
		m.Max = &x
	}
	return m, true
}

// numPrefix, s'nin başındaki ondalık sayının uzunluğunu döner (işaret,
// tam kısım, tek ayırıcı '.' ya da ',', üs). Sayı yoksa 0.
func numPrefix(s string) int {
	i := 0
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && (s[i] == '.' || s[i] == ',') {
		k := i + 1
		frac := 0
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
			frac++
		}
		if frac > 0 || digits > 0 {
			i = k
			digits += frac
		}
	}
	if digits == 0 {
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		k := i + 1
		if k < len(s) && (s[k] == '-' || s[k] == '+') {
			k++
		}
		d := 0
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
			d++
		}
		if d > 0 {
			i = k
		}
	}
	return i
}
