package i18n

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Error API'nin döndürdüğü Türkçe (kullanıcıya yönelik) mesajı istenen dile
// çevirir. Kaynak metin Türkçedir; tr için olduğu gibi döner.
//
// errorsEN (errors_en.go) Türkçe metin → İngilizce eşlemesidir. İki tür girdi vardır:
//   - Düz metin: birebir eşleşir ("Bulunamadı" → "Not found").
//   - Kalıp: %s, %d, %v içeren girdiler (fmt ile üretilen mesajlar). Her fiil
//     metindeki bir parçayı yakalar; İngilizce karşılıkta aynı sırayla
//     yerleştirilir (%[2]s gibi sıra belirtilebilir). Yakalanan %s/%v parçaları
//     da çevrilmeye çalışılır: "Telefon numaraları gerekli" → "%s gerekli"
//     kalıbı + "Telefon numaraları" girdisi → "Phone numbers is required".
//
// Bulunamayan mesaj Türkçe haliyle döner. "; " ile birleştirilmiş birden çok
// mesaj parça parça çevrilir.
func Error(lang, msg string) string {
	if Or(lang) != EN || msg == "" {
		return msg
	}
	if en, ok := translateEN(msg, 0); ok {
		return en
	}
	// "a; b" gibi birleşik mesajlar (ör. toplu doğrulama).
	if strings.Contains(msg, "; ") {
		parts := strings.Split(msg, "; ")
		changed := false
		for i, p := range parts {
			if en, ok := translateEN(p, 1); ok {
				parts[i], changed = en, true
			}
		}
		if changed {
			return strings.Join(parts, "; ")
		}
	}
	return msg
}

type errPattern struct {
	re    *regexp.Regexp
	en    string
	verbs []byte // her yakalamanın fiili: 's', 'd', 'v'
	lit   int    // sabit metin uzunluğu (daha belirgin kalıp önce denenir)
}

var (
	patternsOnce sync.Once
	patterns     []errPattern
	exactEN      map[string]string
)

var verbRe = regexp.MustCompile(`%(\[\d+\])?[sdvq]`)

func compilePatterns() {
	exactEN = map[string]string{}
	for tr, en := range errorsEN {
		if !verbRe.MatchString(tr) {
			exactEN[tr] = en
			continue
		}
		var b strings.Builder
		b.WriteString("^")
		var verbs []byte
		last := 0
		lit := 0
		for _, loc := range verbRe.FindAllStringIndex(tr, -1) {
			b.WriteString(regexp.QuoteMeta(tr[last:loc[0]]))
			lit += loc[0] - last
			v := tr[loc[1]-1]
			verbs = append(verbs, v)
			if v == 'd' {
				b.WriteString(`(-?\d+)`)
			} else {
				b.WriteString(`(.+?)`)
			}
			last = loc[1]
		}
		b.WriteString(regexp.QuoteMeta(tr[last:]))
		lit += len(tr) - last
		b.WriteString("$")
		patterns = append(patterns, errPattern{re: regexp.MustCompile(b.String()), en: en, verbs: verbs, lit: lit})
	}
	// Daha uzun sabit metni olan kalıp önce: "%s gerekli" gibi genel kalıplar
	// daha özel olanları gölgelemesin.
	sort.SliceStable(patterns, func(i, j int) bool {
		if patterns[i].lit != patterns[j].lit {
			return patterns[i].lit > patterns[j].lit
		}
		return patterns[i].re.String() < patterns[j].re.String()
	})
}

// enVerbRe İngilizce karşılıktaki fiiller; hepsi %s'ye çevrilir (yakalanan
// parçalar metin olarak yerleştirilir).
var enVerbRe = regexp.MustCompile(`%(\[\d+\])?[sdvq]`)

func translateEN(msg string, depth int) (string, bool) {
	patternsOnce.Do(compilePatterns)
	if en, ok := exactEN[msg]; ok {
		return en, true
	}
	if depth > 2 {
		return "", false
	}
	for _, p := range patterns {
		m := p.re.FindStringSubmatch(msg)
		if m == nil {
			continue
		}
		args := make([]any, len(m)-1)
		for i, s := range m[1:] {
			if p.verbs[i] != 'd' {
				if en, ok := translateEN(s, depth+1); ok {
					s = en
				}
			}
			args[i] = s
		}
		f := enVerbRe.ReplaceAllString(p.en, "%${1}s")
		return fmt.Sprintf(f, args...), true
	}
	return "", false
}
