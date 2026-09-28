package i18n

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// table Türkçe metin → İngilizce eşlemesinin derlenmiş halidir (Error ve
// Message ortak kullanır). Kaynak eşlemede iki tür girdi vardır:
//   - Düz metin: birebir eşleşir (map araması).
//   - Kalıp: %s, %d, %v, %q (ve %.Nf) içeren fmt biçim dizgesi. Her fiil
//     metindeki bir parçayı yakalar; İngilizce karşılıkta aynı sırayla
//     yerleştirilir (%[2]s gibi sıra belirtilebilir). "%%" düz % işaretidir.
//
// Kalıplar ilk kullanımda bir kez derlenir. Her kalıbın sabit baş ve son
// parçası ayrıca tutulur; düzenli ifade yalnızca metin bu parçalarla
// başlayıp bitiyorsa çalıştırılır (yüzlerce kalıpta bile ucuz eleme).
type table struct {
	src           map[string]string
	anchoredFirst bool // başı sabit kalıplar önce (iç içe sarmalayıcılar için; bkz. pattern.anchored)

	once     sync.Once
	exact    map[string]string
	patterns []pattern
}

type pattern struct {
	re     *regexp.Regexp
	en     string // Sprintf biçimi: tüm fiiller %s'ye çevrilmiş
	verbs  []byte // her yakalamanın fiili: 's', 'd', 'v', 'q', 'f'
	prefix string // ilk fiilden önceki sabit metin
	suffix string // son fiilden sonraki sabit metin
	lit    int    // sabit metin uzunluğu (daha belirgin kalıp önce denenir)
	// anchored: kalıbın başı sabit metin veya %d; başta metni yutabilecek bir
	// %s/%q yok ("Bakımda (%s)", "%d/%d konum çalışıyor — %s" evet; "%s kaydı
	// bulunamadı" hayır).
	anchored bool
}

// verbRe Türkçe anahtardaki fiiller (Error kataloğu); tokRe ayrıca "%%" ve
// "%.Nf" tanır.
var (
	verbRe   = regexp.MustCompile(`%(\[\d+\])?[sdvq]`)
	enVerbRe = regexp.MustCompile(`%(\[\d+\])?[sdvq]`)
	tokRe    = regexp.MustCompile(`%%|%(\[\d+\])?[sdvq]|%\.\d+f`)
	enTokRe  = regexp.MustCompile(`%(\[\d+\])?[sdvq]|%\.\d+f`)
)

func unescapePct(s string) string { return strings.ReplaceAll(s, "%%", "%") }

func (t *table) compile() {
	t.exact = make(map[string]string, len(t.src))
	for tr, en := range t.src {
		toks := tokRe.FindAllStringIndex(tr, -1)
		hasVerb := false
		for _, loc := range toks {
			if tr[loc[0]:loc[1]] != "%%" {
				hasVerb = true
				break
			}
		}
		if !hasVerb {
			if len(toks) > 0 { // yalnızca "%%": düz metin
				tr, en = unescapePct(tr), unescapePct(en)
			}
			t.exact[tr] = en
			continue
		}
		var b strings.Builder
		b.WriteString("^")
		p := pattern{}
		var lit strings.Builder // şu ana kadarki sabit parça (kaçışsız)
		first := true
		last := 0
		for _, loc := range toks {
			lit.WriteString(tr[last:loc[0]])
			tok := tr[loc[0]:loc[1]]
			last = loc[1]
			if tok == "%%" {
				lit.WriteString("%")
				continue
			}
			b.WriteString(regexp.QuoteMeta(lit.String()))
			p.lit += lit.Len()
			if first {
				p.prefix, first = lit.String(), false
				p.anchored = p.prefix != "" || tok[len(tok)-1] == 'd'
			}
			lit.Reset()
			v := tok[len(tok)-1]
			p.verbs = append(p.verbs, v)
			switch v {
			case 'd':
				b.WriteString(`(-?\d+)`)
			case 'f':
				b.WriteString(`(-?\d+(?:\.\d+)?)`)
			default:
				b.WriteString(`(.+?)`)
			}
		}
		lit.WriteString(tr[last:])
		p.suffix = unescapePct(lit.String())
		b.WriteString(regexp.QuoteMeta(p.suffix))
		p.lit += len(p.suffix)
		b.WriteString("$")
		p.re = regexp.MustCompile(b.String())
		p.en = enTokRe.ReplaceAllStringFunc(en, func(v string) string {
			if strings.HasSuffix(v, "f") {
				return "%s"
			}
			return v[:len(v)-1] + "s"
		})
		t.patterns = append(t.patterns, p)
	}
	// Daha uzun sabit metni olan kalıp önce: "%s gerekli" gibi genel kalıplar
	// daha özel olanları gölgelemesin. anchoredFirst: başı sabit olanlar önce
	// (en uzun baş önce) — "Bakımda (%s)" gibi dıştaki sarmalayıcı, içteki
	// "%s (sonuç gelmeyen: %s)" gibi başı serbest kalıptan önce eşleşsin.
	sort.SliceStable(t.patterns, func(i, j int) bool {
		a, b := t.patterns[i], t.patterns[j]
		if t.anchoredFirst && a.anchored != b.anchored {
			return a.anchored
		}
		if t.anchoredFirst && len(a.prefix) != len(b.prefix) {
			return len(a.prefix) > len(b.prefix)
		}
		if a.lit != b.lit {
			return a.lit > b.lit
		}
		return a.re.String() < b.re.String()
	})
}

func (t *table) lookup(msg string) (string, bool) {
	t.once.Do(t.compile)
	en, ok := t.exact[msg]
	return en, ok
}

// match msg'yi kalıplarla eşleştirir (düz metin araması hariç). anchored:
// yalnızca başı sabit (true) / başı serbest (false) kalıplar; nil: hepsi.
// sub yakalanan metin parçalarını çevirir (derinlik bir artırılarak).
func (t *table) match(msg string, depth int, anchored *bool, sub func(string, int) (string, bool)) (string, bool) {
	t.once.Do(t.compile)
	for i := range t.patterns {
		p := &t.patterns[i]
		if anchored != nil && p.anchored != *anchored {
			continue
		}
		if len(msg) < p.lit || !strings.HasPrefix(msg, p.prefix) || !strings.HasSuffix(msg, p.suffix) {
			continue
		}
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		args := make([]any, len(m)-1)
		for i, s := range m[1:] {
			if v := p.verbs[i]; v != 'd' && v != 'f' {
				if en, ok := sub(s, depth+1); ok {
					s = en
				}
			}
			args[i] = s
		}
		return fmt.Sprintf(p.en, args...), true
	}
	return "", false
}
