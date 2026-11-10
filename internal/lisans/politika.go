package lisans

import "time"

// KiraSuresi lisans sunucusunun verdiği olağan kiranın ömrüdür.
const KiraSuresi = 14 * 24 * time.Hour

// TasimaAraligi abonelik lisansının başka panele taşınabilme aralığıdır.
const TasimaAraligi = 30 * 24 * time.Hour

// TasimaIzni abonelik lisansının başka panel kimliğine taşınmasına izin verilip verilmediğini ve
// izin yoksa kalan bekleme süresini döndürür (30 günde 1; hiç taşınmamışsa izin var).
func TasimaIzni(sonTasima, simdi time.Time) (bool, time.Duration) {
	if sonTasima.IsZero() {
		return true, 0
	}
	gecen := simdi.Sub(sonTasima)
	if gecen >= TasimaAraligi {
		return true, 0
	}
	return false, TasimaAraligi - gecen
}
