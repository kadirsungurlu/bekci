package i18n

import (
	"fmt"
	"strings"
	"time"
)

// Duration süreyi kısa biçimde yazar.
// tr: "45 sn", "12 dk", "2 sa 5 dk", "3 gün 4 sa"
// en: "45s", "12m", "2h 5m", "3d 4h"
func Duration(lang string, d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Minute:
		return T(lang, "dur.sec", int(d.Seconds()))
	case d < time.Hour:
		return T(lang, "dur.min", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return T(lang, "dur.hour", h) + " " + T(lang, "dur.min", m)
		}
		return T(lang, "dur.hour", h)
	default:
		days := int(d.Hours()) / 24
		if h := int(d.Hours()) % 24; h > 0 {
			return T(lang, "dur.day", days) + " " + T(lang, "dur.hour", h)
		}
		return T(lang, "dur.day", days)
	}
}

// DateTime tarih ve saniyeli saat: tr "02.01.2006 15:04:05", en "2006-01-02 15:04:05".
func DateTime(lang string, t time.Time) string {
	if Or(lang) == EN {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format("02.01.2006 15:04:05")
}

// DateTimeMin tarih ve dakikalı saat: tr "02.01.2006 15:04", en "2006-01-02 15:04".
func DateTimeMin(lang string, t time.Time) string {
	if Or(lang) == EN {
		return t.Format("2006-01-02 15:04")
	}
	return t.Format("02.01.2006 15:04")
}

// Decimal sabit ondalıklı sayı; tr virgül, en nokta kullanır: 1,25 / 1.25.
func Decimal(lang string, v float64, digits int) string {
	s := fmt.Sprintf("%.*f", digits, v)
	if Or(lang) == TR {
		s = strings.Replace(s, ".", ",", 1)
	}
	return s
}

// Percent tam sayıya yuvarlanmış yüzde: tr "%94", en "94%".
func Percent(lang string, v float64) string {
	if Or(lang) == EN {
		return fmt.Sprintf("%.0f%%", v)
	}
	return fmt.Sprintf("%%%.0f", v)
}

// MetricValue sunucu metriğinin değerini birimiyle biçimler: yük ondalıklı,
// sıcaklık °C, ağ Mbit/s (1000 ve üstü Gbit/s), diğerleri yüzde.
func MetricValue(lang, metric string, v float64) string {
	switch metric {
	case "load":
		return Decimal(lang, v, 2)
	case "temp":
		return fmt.Sprintf("%.0f °C", v)
	case "net":
		if v >= 1000 {
			return Decimal(lang, v/1000, 2) + " Gbit/s"
		}
		return fmt.Sprintf("%.0f Mbit/s", v)
	}
	return Percent(lang, v)
}
