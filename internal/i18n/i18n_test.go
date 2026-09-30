package i18n

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

var verbs = regexp.MustCompile(`%(\[\d+\])?[sdvqf%]|%\.\d+f`)

func verbList(s string) []string {
	out := []string{}
	for _, v := range verbs.FindAllString(s, -1) {
		if v != "%%" {
			out = append(out, v)
		}
	}
	return out
}

// TestCatalog her anahtarın iki dilde de dolu olduğunu ve biçim fiillerinin
// (sayı, sıra, tür) aynı olduğunu denetler.
func TestCatalog(t *testing.T) {
	for key, m := range messages {
		if strings.TrimSpace(m.TR) == "" || strings.TrimSpace(m.EN) == "" {
			t.Errorf("%s: iki dil de dolu olmalı: %+v", key, m)
			continue
		}
		if a, b := verbList(m.TR), verbList(m.EN); fmt.Sprint(a) != fmt.Sprint(b) {
			t.Errorf("%s: biçim fiilleri farklı: tr %v, en %v", key, a, b)
		}
		if strings.HasSuffix(key, ".one") && !Has(strings.TrimSuffix(key, ".one")) {
			t.Errorf("%s: tekil biçimin çoğul anahtarı yok", key)
		}
	}
}

var turkishLetters = regexp.MustCompile(`[çğıöşüÇĞİÖŞÜ]`)

// TestErrorCatalog İngilizce hata metinlerinin fiil sayısının Türkçeyle
// uyuştuğunu, biçimlendirmede "%!" üretmediğini ve Türkçe harf içermediğini denetler.
func TestErrorCatalog(t *testing.T) {
	for tr, en := range errorsEN {
		if strings.TrimSpace(en) == "" {
			t.Errorf("%q: boş çeviri", tr)
			continue
		}
		trv := verbRe.FindAllString(tr, -1)
		env := enVerbRe.FindAllString(en, -1)
		if len(trv) != len(env) {
			t.Errorf("%q: fiil sayısı farklı (%d / %d): %q", tr, len(trv), len(env), en)
		}
		args := make([]any, len(env))
		for i := range args {
			args[i] = "X"
		}
		if out := fmt.Sprintf(enVerbRe.ReplaceAllString(en, "%${1}s"), args...); strings.Contains(out, "%!") {
			t.Errorf("%q: biçim hatası: %q", en, out)
		}
		if turkishLetters.MatchString(en) {
			t.Errorf("%q: çeviride Türkçe harf var: %q", tr, en)
		}
	}
}

func TestError(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Bulunamadı", "Not found"},
		{"Çok fazla hatalı deneme. 15 dakika sonra tekrar deneyin.", "Too many failed attempts. Try again in 15 minutes."},
		{"Geçersiz istek gövdesi: unexpected EOF", "Invalid request body: unexpected EOF"},
		// Cron hataları: kütüphanenin İngilizce metni yerine alan adı ve izin verilen değerler.
		{"Cron ifadesi 5 alandan oluşmalı (dakika saat ayın-günü ay haftanın-günü); 2 alan girildi",
			"A cron expression has 5 fields (minute hour day-of-month month day-of-week); 2 given"},
		{"Cron ifadesinin ayın günü alanı geçersiz: 32 (izin verilen: 1-31)", "Invalid day-of-month field in the cron expression: 32 (allowed: 1-31)"},
		{"Cron ifadesinin ay alanı geçersiz: FOO (izin verilen: 1-12 veya JAN-DEC)",
			"Invalid month field in the cron expression: FOO (allowed: 1-12 or JAN-DEC)"},
		// Bilinmeyen mesaj olduğu gibi kalır.
		{"Hiç görülmemiş bir mesaj", "Hiç görülmemiş bir mesaj"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Error(EN, c.in); got != c.want {
			t.Errorf("Error(en, %q) = %q, %q bekleniyordu", c.in, got, c.want)
		}
		if got := Error(TR, c.in); got != c.in {
			t.Errorf("Error(tr, %q) değişmemeli: %q", c.in, got)
		}
	}
	// Kalıptaki %s parçası da çevrilir; "; " ile birleşik mesajlar parça parça.
	if got := Error(EN, "Gönderilemedi: HTTP 401 (yetki hatası: anahtar/token doğru mu?)"); strings.Contains(got, "Gönderilemedi") || strings.Contains(got, "yetki") {
		t.Errorf("iç içe çeviri: %q", got)
	}
	if got := Error(EN, "Bulunamadı; Sunucu hatası"); got != "Not found; Server error" {
		t.Errorf("birleşik: %q", got)
	}
}

func TestNormalize(t *testing.T) {
	for in, want := range map[string]string{"tr": "tr", "EN": "en", "en-US": "en", "tr_TR": "tr", "de": "", "": "", "en;q=0.8": "en"} {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, %q bekleniyordu", in, got, want)
		}
	}
	if got := FromAcceptLanguage("de-DE,de;q=0.9,en-US;q=0.8,tr;q=0.7"); got != "en" {
		t.Errorf("Accept-Language: %q", got)
	}
	if Valid("") || Valid("de") || !Valid("en") || Valid("EN") {
		t.Error("Valid yanlış")
	}
	if Or("xx") != TR || Or("en") != EN {
		t.Error("Or yanlış")
	}
}

func TestTAndTN(t *testing.T) {
	if got := T(EN, "notify.down.title", "API"); got != "🔴 API is down" {
		t.Errorf("T: %q", got)
	}
	if got := T("", "notify.down.title", "API"); got != "🔴 API çalışmıyor" {
		t.Errorf("varsayılan dil tr olmalı: %q", got)
	}
	if got := T(EN, "yok.boyle.anahtar"); got != "yok.boyle.anahtar" {
		t.Errorf("bilinmeyen anahtar: %q", got)
	}
	if got := TN(EN, "badge.days", 1, 1); got != "1 day" {
		t.Errorf("tekil: %q", got)
	}
	if got := TN(EN, "badge.days", 5, 5); got != "5 days" {
		t.Errorf("çoğul: %q", got)
	}
	if got := TN(TR, "badge.days", 1, 1); got != "1 gün" {
		t.Errorf("tr tekil: %q", got)
	}
}

func TestFormat(t *testing.T) {
	d := 2*time.Hour + 5*time.Minute
	if got := Duration(TR, d); got != "2 sa 5 dk" {
		t.Errorf("tr süre: %q", got)
	}
	if got := Duration(EN, d); got != "2h 5m" {
		t.Errorf("en süre: %q", got)
	}
	if got := Duration(EN, (3*24+4)*time.Hour); got != "3d 4h" {
		t.Errorf("en gün: %q", got)
	}
	if got := Duration(EN, 45*time.Second); got != "45s" {
		t.Errorf("en sn: %q", got)
	}
	tm := time.Date(2026, 9, 28, 14, 5, 9, 0, time.UTC)
	if got := DateTime(TR, tm); got != "28.09.2026 14:05:09" {
		t.Errorf("tr tarih: %q", got)
	}
	if got := DateTime(EN, tm); got != "2026-09-28 14:05:09" {
		t.Errorf("en tarih: %q", got)
	}
	if Percent(TR, 94.4) != "%94" || Percent(EN, 94.4) != "94%" {
		t.Errorf("yüzde: %q %q", Percent(TR, 94.4), Percent(EN, 94.4))
	}
	if Decimal(TR, 1.254, 2) != "1,25" || Decimal(EN, 1.254, 2) != "1.25" {
		t.Error("ondalık")
	}
}
