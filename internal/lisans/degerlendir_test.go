package lisans

import (
	"errors"
	"testing"
	"time"
)

func abonelikGirdi() Girdi {
	return Girdi{Anahtar: abonelikAnahtari(), Kira: abonelikKirasi("P1"), PanelID: "P1",
		Simdi: t0.Add(time.Hour), EnIleri: t0, Derleme: t0.AddDate(0, -1, 0),
		Kurulum: t0.AddDate(-1, 0, 0), SonBasari: t0.Add(time.Hour), ProDerleme: true}
}

func beklenen(t *testing.T, d Durum, etkin bool, neden string) {
	t.Helper()
	if d.Etkin != etkin || d.Neden != neden {
		t.Fatalf("etkin=%v neden=%q bekleniyordu, gelen %+v", etkin, neden, d)
	}
}

func TestAbonelikKiraIleEtkin(t *testing.T) {
	p := testAnahtari(t)
	// Gerçek imzalı akış: kodla -> çöz -> değerlendir.
	a, err := AnahtarCoz(kodla(t, p, OnekAnahtar, abonelikAnahtari()))
	if err != nil {
		t.Fatal(err)
	}
	k, err := KiraCoz(kodla(t, p, OnekKira, abonelikKirasi("P1")))
	if err != nil {
		t.Fatal(err)
	}
	g := abonelikGirdi()
	g.Anahtar, g.Kira = a, k
	d := Degerlendir(g)
	beklenen(t, d, true, "")
	if d.Kaynak != KaynakLisans || d.Tur != TurAbonelik || !d.Acik(OzellikBetik) || d.Uyari != "" ||
		!d.Bitis.Equal(t0.AddDate(1, 0, 0)) || !d.KiraBitis.Equal(t0.Add(KiraSuresi)) || d.KalanGun == nil {
		t.Fatalf("durum yanlış: %+v", d)
	}
}

func TestSuresiGecmis(t *testing.T) {
	g := abonelikGirdi()
	g.Simdi = t0.Add(KiraSuresi)
	g.EnIleri = g.Simdi
	d := Degerlendir(g)
	beklenen(t, d, false, NedenKiraBitti)
	if d.Uyari != UyariKiraBitti {
		t.Fatalf("uyarı: %q", d.Uyari)
	}

	g = abonelikGirdi()
	g.Kira.LisansBitis = Z(t0)
	d = Degerlendir(g)
	beklenen(t, d, false, NedenSuresiDoldu)
	if d.Uyari != UyariSuresiDoldu {
		t.Fatalf("uyarı: %q", d.Uyari)
	}
}

func TestDerlemeLisansBitisSonrasi(t *testing.T) {
	g := abonelikGirdi()
	g.Derleme = t0.AddDate(1, 0, 1)
	beklenen(t, Degerlendir(g), false, NedenSuresiDoldu)
	g.Derleme = time.Time{} // geliştirme derlemesi: denetim atlanır
	beklenen(t, Degerlendir(g), true, "")
}

func TestSaatGeri(t *testing.T) {
	g := abonelikGirdi()
	g.EnIleri = g.Simdi.Add(11 * time.Minute)
	d := Degerlendir(g)
	beklenen(t, d, false, NedenSaatGeri)
	if d.Uyari != UyariSaatGeri {
		t.Fatalf("uyarı: %q", d.Uyari)
	}
	g.EnIleri = g.Simdi.Add(9 * time.Minute) // tolerans içinde
	beklenen(t, Degerlendir(g), true, "")

	// Deneme de kapanır.
	dg := Girdi{ProDerleme: true, Kurulum: t0, Simdi: t0.Add(time.Hour), EnIleri: t0.Add(2 * time.Hour)}
	beklenen(t, Degerlendir(dg), false, NedenSaatGeri)
}

func TestDeneme(t *testing.T) {
	g := Girdi{ProDerleme: true, Kurulum: t0, Simdi: t0.Add(29 * 24 * time.Hour), EnIleri: t0}
	d := Degerlendir(g)
	beklenen(t, d, true, "")
	if d.Kaynak != KaynakDeneme || !d.Acik(OzellikTopoloji) || d.Uyari != UyariDeneme ||
		!d.DenemeBitis.Equal(t0.Add(DenemeSuresi)) {
		t.Fatalf("deneme durumu yanlış: %+v", d)
	}
	g.Simdi = t0.Add(10 * 24 * time.Hour)
	if d := Degerlendir(g); d.Uyari != "" || !d.Etkin {
		t.Fatalf("erken deneme uyarısı: %+v", d)
	}
	g.Simdi = t0.Add(DenemeSuresi)
	beklenen(t, Degerlendir(g), false, NedenDenemeBitti)
}

func TestProDegil(t *testing.T) {
	g := abonelikGirdi()
	g.ProDerleme = false
	beklenen(t, Degerlendir(g), false, NedenProDegil)
	beklenen(t, Degerlendir(Girdi{Kurulum: t0, Simdi: t0}), false, NedenProDegil)
}

func omurAnahtari(tur, cihaz string) *Anahtar {
	a := abonelikAnahtari()
	a.Tur, a.Bitis, a.Cihaz = tur, Zaman{}, cihaz
	return a
}

func TestOmurCevrimdisiCihaz(t *testing.T) {
	p := testAnahtari(t)
	asil, err := cihazKimligiDizinlerle(cihazDizini(t, "uuid-asil", "mid-1"))
	if err != nil {
		t.Fatal(err)
	}
	metin := kodla(t, p, OnekAnahtar, omurAnahtari(TurOmurCevrimdisi, asil))
	a, err := AnahtarCoz(metin)
	if err != nil {
		t.Fatal(err)
	}
	g := Girdi{Anahtar: a, PanelID: "P1", CihazID: asil, Simdi: t0, ProDerleme: true}
	d := Degerlendir(g)
	beklenen(t, d, true, "")

	// Farklı product_uuid (aynı machine-id).
	baska, err := cihazKimligiDizinlerle(cihazDizini(t, "uuid-baska", "mid-1"))
	if err != nil || baska == asil {
		t.Fatalf("farklı uuid aynı kimlik verdi: %v", err)
	}
	g.CihazID = baska
	beklenen(t, Degerlendir(g), false, NedenCihaz)

	g.CihazID, g.CihazHata = "", errors.New("okunamadı")
	beklenen(t, Degerlendir(g), false, NedenCihazYok)
}

func TestYedektenBaskaVM(t *testing.T) {
	asil, _ := cihazKimligiDizinlerle(cihazDizini(t, "uuid-a", "mid-a"))
	yeni, _ := cihazKimligiDizinlerle(cihazDizini(t, "uuid-b", "mid-b"))
	a := omurAnahtari(TurOmurCevrimici, asil)
	k := abonelikKirasi("P1")
	k.Tur, k.Cihaz, k.LisansBitis = TurOmurCevrimici, asil, Zaman{}
	g := Girdi{Anahtar: a, Kira: k, PanelID: "P1", CihazID: asil, Simdi: t0.Add(time.Hour),
		SonBasari: t0, ProDerleme: true}
	beklenen(t, Degerlendir(g), true, "")
	g.CihazID = yeni // aynı DB yedeği (anahtar+kira+panel), başka VM
	beklenen(t, Degerlendir(g), false, NedenCihaz)
}

func TestKiraUyusmaz(t *testing.T) {
	c := "cihaz-1"
	k := abonelikKirasi("BASKA-PANEL")
	k.Tur, k.Cihaz, k.LisansBitis = TurOmurCevrimici, c, Zaman{}
	g := Girdi{Anahtar: omurAnahtari(TurOmurCevrimici, c), Kira: k, PanelID: "P1", CihazID: c,
		Simdi: t0.Add(time.Hour), ProDerleme: true}
	beklenen(t, Degerlendir(g), false, NedenKiraUyusmaz)
	g.Kira = nil
	beklenen(t, Degerlendir(g), false, NedenKiraYok)
}

func TestGuncellemeBitis(t *testing.T) {
	c := "cihaz-1"
	a := omurAnahtari(TurOmurCevrimdisi, c)
	a.GuncellemeBitis = Z(t0)
	g := Girdi{Anahtar: a, CihazID: c, Simdi: t0.AddDate(1, 0, 0), ProDerleme: true}
	g.Derleme = t0.Add(time.Hour)
	beklenen(t, Degerlendir(g), false, NedenGuncellemeBitis)
	g.Derleme = t0.Add(-time.Hour)
	beklenen(t, Degerlendir(g), true, "")

	// Kiradaki guncelleme_bitis de aynı kural.
	ga := abonelikGirdi()
	ga.Kira.GuncellemeBitis = Z(t0.AddDate(0, -2, 0))
	beklenen(t, Degerlendir(ga), false, NedenGuncellemeBitis)
	ga.Kira.GuncellemeBitis = Z(t0)
	beklenen(t, Degerlendir(ga), true, "")
}

func TestUyariEsikleri(t *testing.T) {
	tests := []struct {
		kalan time.Duration
		uyari string
	}{
		{20 * 24 * time.Hour, ""},
		{14 * 24 * time.Hour, UyariBitis14},
		{8 * 24 * time.Hour, UyariBitis14},
		{7 * 24 * time.Hour, UyariBitis7},
		{2 * 24 * time.Hour, UyariBitis7},
		{24 * time.Hour, UyariBitis1},
		{time.Hour, UyariBitis1},
	}
	for _, tt := range tests {
		g := abonelikGirdi()
		g.Kira.LisansBitis = Z(g.Simdi.Add(tt.kalan))
		g.Kira.Gecerlilik = Z(g.Simdi.Add(30 * 24 * time.Hour))
		g.Derleme = time.Time{}
		if d := Degerlendir(g); d.Uyari != tt.uyari || !d.Etkin {
			t.Errorf("kalan %v: uyarı %q bekleniyordu, gelen %+v", tt.kalan, tt.uyari, d)
		}
	}
	// Çevrimdışı kira bitişi.
	c := "cihaz-1"
	k := abonelikKirasi("P1")
	k.Tur, k.Cihaz, k.LisansBitis, k.Cevrimdisi = TurOmurCevrimici, c, Zaman{}, true
	k.Gecerlilik = Z(t0.Add(6 * 24 * time.Hour))
	g := Girdi{Anahtar: omurAnahtari(TurOmurCevrimici, c), Kira: k, PanelID: "P1", CihazID: c,
		Simdi: t0, ProDerleme: true}
	if d := Degerlendir(g); d.Uyari != UyariBitis7 || d.KalanGun == nil || *d.KalanGun != 6 {
		t.Fatalf("çevrimdışı kira uyarısı: %+v", d)
	}

	// Sunucuya ulaşılamıyor: bitis_N'den önce gelir.
	ga := abonelikGirdi()
	ga.Kira.LisansBitis = Z(ga.Simdi.Add(3 * 24 * time.Hour))
	ga.SonBasari = ga.Simdi.Add(-37 * time.Hour)
	if d := Degerlendir(ga); d.Uyari != UyariSunucuUlasilamiyor || !d.Etkin {
		t.Fatalf("sunucu_ulasilamiyor beklenirdi: %+v", d)
	}
	ga.SonBasari = ga.Simdi.Add(-35 * time.Hour)
	if d := Degerlendir(ga); d.Uyari != UyariBitis7 {
		t.Fatalf("bitis_7 beklenirdi: %+v", d)
	}
}
