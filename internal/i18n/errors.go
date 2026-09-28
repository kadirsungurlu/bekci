package i18n

import "strings"

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

// errTable errorsEN'in derlenmiş hali (bkz. table); kalıplar sabit metin
// uzunluğuna göre sıralıdır.
var errTable = &table{src: errorsEN}

func translateEN(msg string, depth int) (string, bool) {
	if en, ok := errTable.lookup(msg); ok {
		return en, true
	}
	if depth > 2 {
		return "", false
	}
	return errTable.match(msg, depth, nil, translateEN)
}
