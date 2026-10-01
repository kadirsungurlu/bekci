package store

import (
	"slices"
	"strings"
)

// 24: durum sayfasında olay penceresi ve uptime pencereleri.
//
//   - incident_days: "Son N günün olayları" bölümünün penceresi (7 | 14 | 30 | 90;
//     varsayılan 14 = önceki sabit değer).
//   - uptime_windows: monitör satırlarında gösterilecek uptime yüzdelerinin
//     pencereleri, virgülle ("24h,30d,90d"). Boş = eski davranış: çubuk
//     kapsamına göre tek pencere (24 saat ya da 90 gün).
func init() {
	RegisterMigration(24, `
ALTER TABLE status_pages ADD COLUMN incident_days INTEGER NOT NULL DEFAULT 14;
ALTER TABLE status_pages ADD COLUMN uptime_windows TEXT NOT NULL DEFAULT '';
`)
}

// DefaultIncidentDays herkese açık sayfadaki olay penceresi (gün).
const DefaultIncidentDays = 14

// IncidentDayOptions seçilebilen olay pencereleri (gün).
var IncidentDayOptions = []int{7, 14, 30, 90}

// ValidIncidentDays seçenek geçerli mi?
func ValidIncidentDays(d int) bool { return slices.Contains(IncidentDayOptions, d) }

func incidentDaysOr(d int) int {
	if ValidIncidentDays(d) {
		return d
	}
	return DefaultIncidentDays
}

// UptimeWindowOptions seçilebilen uptime pencereleri (sabit sıra).
var UptimeWindowOptions = []string{"24h", "7d", "30d", "90d"}

// NormalizeUptimeWindows bilinmeyen ve tekrarlanan pencereleri atar, sabit
// sıraya dizer (boş = varsayılan tek pencere).
func NormalizeUptimeWindows(in []string) []string {
	out := []string{}
	for _, w := range UptimeWindowOptions {
		for _, x := range in {
			if strings.TrimSpace(x) == w {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

func encodeWindows(ws []string) string { return strings.Join(NormalizeUptimeWindows(ws), ",") }

func decodeWindows(s string) []string {
	if s == "" {
		return []string{}
	}
	return NormalizeUptimeWindows(strings.Split(s, ","))
}
