package lisans

import (
	"math"
	"time"
)

// Sabitler.
const (
	DenemeSuresi      = 30 * 24 * time.Hour
	SaatToleransi     = 10 * time.Minute
	UlasilamiyorEsigi = 36 * time.Hour
	denemeUyariSuresi = 7 * 24 * time.Hour
)

// Kaynaklar.
const (
	KaynakYok    = "yok"
	KaynakDeneme = "deneme"
	KaynakLisans = "lisans"
)

// Neden kodları.
const (
	NedenSaatGeri        = "saat_geri"
	NedenProDegil        = "pro_degil"
	NedenDenemeBitti     = "deneme_bitti"
	NedenCihazYok        = "cihaz_yok"
	NedenCihaz           = "cihaz"
	NedenGuncellemeBitis = "guncelleme_bitis"
	NedenKiraYok         = "kira_yok"
	NedenKiraUyusmaz     = "kira_uyusmaz"
	NedenKiraBitti       = "kira_bitti"
	NedenSuresiDoldu     = "suresi_doldu"
)

// Uyarı kodları (öncelik sırası: saat_geri > suresi_doldu|kira_bitti > sunucu_ulasilamiyor > bitis_N > deneme).
const (
	UyariSaatGeri           = "saat_geri"
	UyariSuresiDoldu        = "suresi_doldu"
	UyariKiraBitti          = "kira_bitti"
	UyariSunucuUlasilamiyor = "sunucu_ulasilamiyor"
	UyariBitis1             = "bitis_1"
	UyariBitis7             = "bitis_7"
	UyariBitis14            = "bitis_14"
	UyariDeneme             = "deneme"
)

// Girdi Degerlendir'in tüm girdisidir (saf fonksiyon; saat ve ortam dışarıdan verilir).
type Girdi struct {
	Anahtar    *Anahtar
	Kira       *Kira
	PanelID    string
	CihazID    string
	CihazHata  error
	Simdi      time.Time
	EnIleri    time.Time // panelin gördüğü en ileri zaman (lisans.en_ileri)
	Derleme    time.Time // sıfır = bilinmiyor, derleme denetimleri atlanır
	Kurulum    time.Time
	SonBasari  time.Time // son başarılı kira yenilemesi
	ProDerleme bool
}

// Durum değerlendirme sonucudur; API doğrudan JSON olarak yazar.
type Durum struct {
	Etkin       bool      `json:"etkin"`
	Kaynak      string    `json:"kaynak"`
	Neden       string    `json:"neden,omitempty"`
	Ozellikler  []string  `json:"ozellikler"`
	Tur         string    `json:"tur,omitempty"`
	Musteri     string    `json:"musteri,omitempty"`
	Bitis       time.Time `json:"bitis,omitzero"`
	KiraBitis   time.Time `json:"kira_bitis,omitzero"`
	KalanGun    *int      `json:"kalan_gun,omitempty"`
	Uyari       string    `json:"uyari,omitempty"`
	DenemeBitis time.Time `json:"deneme_bitis,omitzero"`
}

func kalanGun(simdi, son time.Time) int {
	return int(math.Ceil(son.Sub(simdi).Hours() / 24))
}

func kapali(d Durum, neden string) Durum {
	d.Etkin = false
	d.Neden = neden
	d.Ozellikler = []string{}
	if neden == NedenSuresiDoldu || neden == NedenKiraBitti {
		d.Uyari = neden
	}
	return d
}

func sonra(derleme, sinir time.Time) bool {
	return !derleme.IsZero() && !sinir.IsZero() && derleme.After(sinir)
}

func ozellikKopya(o []string) []string {
	if o == nil {
		return []string{}
	}
	return append([]string(nil), o...)
}

// Degerlendir docs/LISANS-TASARIM.md "Değerlendirme" sırasını (1-7) birebir uygular.
func Degerlendir(g Girdi) Durum {
	d := Durum{Kaynak: KaynakYok, Ozellikler: []string{}}

	// 1. Saat geri alma (deneme dahil).
	if !g.EnIleri.IsZero() && g.Simdi.Before(g.EnIleri.Add(-SaatToleransi)) {
		d.Neden = NedenSaatGeri
		d.Uyari = UyariSaatGeri
		return d
	}
	// 2. Pro derlemesi değil.
	if !g.ProDerleme {
		d.Neden = NedenProDegil
		return d
	}
	// 3. Anahtar yok: deneme.
	if g.Anahtar == nil {
		kurulum := g.Kurulum
		if kurulum.IsZero() {
			kurulum = g.Simdi
		}
		d.DenemeBitis = kurulum.Add(DenemeSuresi)
		if g.Simdi.Before(d.DenemeBitis) {
			d.Etkin = true
			d.Kaynak = KaynakDeneme
			d.Ozellikler = []string{OzellikHepsi}
			k := kalanGun(g.Simdi, d.DenemeBitis)
			d.KalanGun = &k
			if d.DenemeBitis.Sub(g.Simdi) <= denemeUyariSuresi {
				d.Uyari = UyariDeneme
			}
			return d
		}
		d.Neden = NedenDenemeBitti
		return d
	}

	a := g.Anahtar
	d.Kaynak = KaynakLisans
	d.Tur = a.Tur
	d.Musteri = a.Musteri

	// 4. Cihaza bağlı (ömür boyu) anahtar.
	if a.Tur == TurOmurCevrimici || a.Tur == TurOmurCevrimdisi {
		if g.CihazHata != nil || g.CihazID == "" {
			return kapali(d, NedenCihazYok)
		}
		if g.CihazID != a.Cihaz {
			return kapali(d, NedenCihaz)
		}
		if sonra(g.Derleme, a.GuncellemeBitis.Time) {
			return kapali(d, NedenGuncellemeBitis)
		}
	}
	// 5. Çevrimdışı ömür boyu: kira gerekmez.
	if a.Tur == TurOmurCevrimdisi {
		d.Etkin = true
		d.Ozellikler = ozellikKopya(a.Ozellikler)
		return d
	}

	// 6. Kira gerekir.
	k := g.Kira
	if k == nil {
		return kapali(d, NedenKiraYok)
	}
	if k.Lisans != a.Kimlik || k.Panel != g.PanelID || k.Cihaz != a.Cihaz {
		return kapali(d, NedenKiraUyusmaz)
	}
	if k.Musteri != "" {
		d.Musteri = k.Musteri
	}
	d.KiraBitis = k.Gecerlilik.Time
	if a.Tur == TurAbonelik {
		d.Bitis = k.LisansBitis.Time
	}
	if !g.Simdi.Before(k.Gecerlilik.Time) {
		return kapali(d, NedenKiraBitti)
	}
	if a.Tur == TurAbonelik && !g.Simdi.Before(k.LisansBitis.Time) {
		return kapali(d, NedenSuresiDoldu)
	}
	// 7. Derleme zamanı bilinmiyorsa sonra() denetimi atlar.
	if sonra(g.Derleme, k.LisansBitis.Time) {
		return kapali(d, NedenSuresiDoldu)
	}
	if sonra(g.Derleme, k.GuncellemeBitis.Time) {
		return kapali(d, NedenGuncellemeBitis)
	}

	d.Etkin = true
	d.Ozellikler = ozellikKopya(k.Ozellikler)

	// Kalan gün: abonelik bitişi ya da çevrimdışı kira bitişi (hangisi önce).
	var son time.Time
	if a.Tur == TurAbonelik {
		son = k.LisansBitis.Time
	}
	if k.Cevrimdisi && (son.IsZero() || k.Gecerlilik.Before(son)) {
		son = k.Gecerlilik.Time
	}
	if !son.IsZero() {
		n := kalanGun(g.Simdi, son)
		d.KalanGun = &n
	}

	switch {
	case !k.Cevrimdisi && !g.SonBasari.IsZero() && g.Simdi.Sub(g.SonBasari) > UlasilamiyorEsigi:
		d.Uyari = UyariSunucuUlasilamiyor
	case d.KalanGun != nil && *d.KalanGun <= 1:
		d.Uyari = UyariBitis1
	case d.KalanGun != nil && *d.KalanGun <= 7:
		d.Uyari = UyariBitis7
	case d.KalanGun != nil && *d.KalanGun <= 14:
		d.Uyari = UyariBitis14
	}
	return d
}
