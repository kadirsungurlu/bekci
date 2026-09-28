// Package i18n sunucu tarafındaki metinlerin (bildirimler, rozetler, API hata
// mesajları) dil desteğidir. Diller: tr (varsayılan, kaynak metin) ve en.
//
// Üç parça vardır:
//   - T / TN: anahtarlı mesaj kataloğu (messages.go). Her girdide iki dil
//     birlikte durur; eksik çeviri derlenmez, biçim fiilleri testte denetlenir.
//   - Error: API'nin döndürdüğü Türkçe hata metnini İngilizceye çevirir
//     (errors.go + errors_en.go). Anahtar Türkçe metnin kendisidir; böylece
//     işleyiciler değişmeden merkezi yazıcıda (api.writeError) çevrilir.
//   - Biçimlendirme: süre, tarih, yüzde, ondalık (format.go).
package i18n

import (
	"fmt"
	"strings"
)

// Desteklenen diller.
const (
	TR      = "tr"
	EN      = "en"
	Default = TR
)

// Langs desteklenen diller (sıra: varsayılan önce).
var Langs = []string{TR, EN}

// Normalize dil kodunu desteklenen bir dile indirger: "en-US" → "en",
// "TR" → "tr"; desteklenmiyorsa "".
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.IndexAny(s, "-_;"); i >= 0 {
		s = s[:i]
	}
	switch s {
	case TR, EN:
		return s
	}
	return ""
}

// Or dil geçerliyse onu, değilse varsayılanı (tr) döner.
func Or(lang string) string {
	if l := Normalize(lang); l != "" {
		return l
	}
	return Default
}

// Valid lang desteklenen bir dil mi? (boş geçersizdir)
func Valid(lang string) bool { return Normalize(lang) == lang && lang != "" }

// FromAcceptLanguage Accept-Language başlığındaki ilk desteklenen dili döner;
// yoksa "". Ağırlıklar (q=) sırayla verildiği varsayılır (tarayıcılar öyle
// gönderir).
func FromAcceptLanguage(h string) string {
	for _, part := range strings.Split(h, ",") {
		if l := Normalize(part); l != "" {
			return l
		}
	}
	return ""
}

// Msg bir mesajın iki dildeki biçimi (fmt biçim dizgesi).
type Msg struct{ TR, EN string }

func (m Msg) in(lang string) string {
	if lang == EN && m.EN != "" {
		return m.EN
	}
	return m.TR
}

// T anahtarlı mesajı verilen dilde biçimler. Bilinmeyen anahtar anahtarın
// kendisini döner (testler katalogdaki her anahtarı denetler).
func T(lang, key string, args ...any) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	f := m.in(Or(lang))
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}

// TN sayıya göre tekil/çoğul seçer: n == 1 ise ve katalogda key+".one" varsa
// o kullanılır (Türkçede çoğul eki gerekmediğinden .one girdilerinin TR'si
// genellikle çoğulla aynıdır). n ilk biçim argümanı olarak da verilmez;
// gerekiyorsa args içinde ayrıca geçilmelidir.
func TN(lang, key string, n int, args ...any) string {
	if n == 1 {
		if _, ok := messages[key+".one"]; ok {
			return T(lang, key+".one", args...)
		}
	}
	return T(lang, key, args...)
}

// Has katalogda anahtar var mı?
func Has(key string) bool {
	_, ok := messages[key]
	return ok
}
