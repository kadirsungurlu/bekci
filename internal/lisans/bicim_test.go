package lisans

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestAnahtarCozImza(t *testing.T) {
	p := testAnahtari(t)
	metin := kodla(t, p, OnekAnahtar, abonelikAnahtari())
	a, err := AnahtarCoz(metin)
	if err != nil {
		t.Fatalf("geçerli anahtar reddedildi: %v", err)
	}
	if a.Kimlik != "L-TEST" || !a.Bitis.Equal(t0.AddDate(1, 0, 0)) || !a.GuncellemeBitis.IsZero() {
		t.Fatalf("alanlar yanlış: %+v", a)
	}

	// Ön ek yanlış: anahtar metni kira olarak.
	if _, err := KiraCoz(metin); !errors.Is(err, ErrOnek) {
		t.Fatalf("önek reddi beklenirdi: %v", err)
	}
	// Bozuk: JSON parçasında tek bayt değişti.
	parca := strings.Split(metin, ".")
	j, _ := b64.DecodeString(parca[1])
	j[len(j)-2] ^= 1
	bozuk := parca[0] + "." + b64.EncodeToString(j) + "." + parca[2]
	if _, err := AnahtarCoz(bozuk); !errors.Is(err, ErrImza) {
		t.Fatalf("bozuk imza reddi beklenirdi: %v", err)
	}
	for _, m := range []string{"", "BEKCI-L1", "BEKCI-L1.x", "BEKCI-L1.!!.??", metin + ".x"} {
		if _, err := AnahtarCoz(m); err == nil {
			t.Fatalf("bozuk biçim kabul edildi: %q", m)
		}
	}
	// Başka anahtarla imzalı.
	baska := testAnahtari(t) // AcikAnahtarlar artık yalnız yeni anahtar
	_ = baska
	if _, err := AnahtarCoz(metin); !errors.Is(err, ErrImza) {
		t.Fatalf("başka anahtar reddi beklenirdi: %v", err)
	}
}

func TestAnahtarDondurme(t *testing.T) {
	p1 := testAnahtari(t)
	eski := AcikAnahtarlar[0]
	p2 := testAnahtari(t)
	AcikAnahtarlar = append(AcikAnahtarlar, eski)
	for _, p := range [2]ed25519.PrivateKey{p1, p2} {
		if _, err := AnahtarCoz(kodla(t, p, OnekAnahtar, abonelikAnahtari())); err != nil {
			t.Fatalf("döndürülmüş anahtar reddedildi: %v", err)
		}
	}
}

func TestSurumVeBilinmeyenAlan(t *testing.T) {
	p := testAnahtari(t)
	var m map[string]any
	b, _ := json.Marshal(abonelikAnahtari())
	_ = json.Unmarshal(b, &m)
	m["gelecek_alan"] = map[string]any{"x": 1}
	if _, err := AnahtarCoz(kodla(t, p, OnekAnahtar, m)); err != nil {
		t.Fatalf("bilinmeyen alan reddedilmemeli: %v", err)
	}
	m["v"] = 2
	if _, err := AnahtarCoz(kodla(t, p, OnekAnahtar, m)); !errors.Is(err, ErrSurum) {
		t.Fatalf("v=2 reddi beklenirdi: %v", err)
	}
}

func TestTurKurallari(t *testing.T) {
	p := testAnahtari(t)
	tests := []struct {
		ad  string
		deg func(a *Anahtar)
		ok  bool
	}{
		{"abonelik geçerli", func(a *Anahtar) {}, true},
		{"abonelik bitiş yok", func(a *Anahtar) { a.Bitis = Zaman{} }, false},
		{"abonelik cihazlı", func(a *Anahtar) { a.Cihaz = "abc" }, false},
		{"ömür geçerli", func(a *Anahtar) { a.Tur = TurOmurCevrimici; a.Bitis = Zaman{}; a.Cihaz = "abc" }, true},
		{"ömür cihazsız", func(a *Anahtar) { a.Tur = TurOmurCevrimdisi; a.Bitis = Zaman{} }, false},
		{"ömür bitişli", func(a *Anahtar) { a.Tur = TurOmurCevrimdisi; a.Cihaz = "abc" }, false},
		{"paket", func(a *Anahtar) { a.Paket = "kurumsal" }, false},
		{"tür", func(a *Anahtar) { a.Tur = "deneme" }, false},
	}
	for _, tt := range tests {
		a := abonelikAnahtari()
		tt.deg(a)
		_, err := AnahtarCoz(kodla(t, p, OnekAnahtar, a))
		if (err == nil) != tt.ok {
			t.Errorf("%s: err=%v", tt.ad, err)
		}
	}
	k := abonelikKirasi("P1")
	if _, err := KiraCoz(kodla(t, p, OnekKira, k)); err != nil {
		t.Fatalf("kira reddedildi: %v", err)
	}
	k.LisansBitis = Zaman{}
	if _, err := KiraCoz(kodla(t, p, OnekKira, k)); err == nil {
		t.Fatal("lisans_bitis'siz abonelik kirası kabul edildi")
	}
}

func TestDurumAcik(t *testing.T) {
	d := Durum{Etkin: true, Ozellikler: []string{"*"}}
	if !d.Acik(OzellikBetik) {
		t.Fatal("* hepsini açmalı")
	}
	d.Ozellikler = []string{OzellikTopoloji}
	if d.Acik(OzellikBetik) || !d.Acik(OzellikTopoloji) {
		t.Fatal("liste yanlış")
	}
	d.Etkin = false
	if d.Acik(OzellikTopoloji) {
		t.Fatal("etkin değilken kapalı olmalı")
	}
}
