package lisans

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

// testAnahtari geçici bir ed25519 çifti üretip AcikAnahtarlar'a koyar; test sonunda geri alır.
func testAnahtari(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eski := AcikAnahtarlar
	AcikAnahtarlar = []ed25519.PublicKey{pub}
	t.Cleanup(func() { AcikAnahtarlar = eski })
	return priv
}

func imzaci(p ed25519.PrivateKey) func([]byte) []byte {
	return func(b []byte) []byte { return ed25519.Sign(p, b) }
}

func kodla(t *testing.T, p ed25519.PrivateKey, onek string, v any) string {
	t.Helper()
	s, err := Kodla(onek, v, imzaci(p))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func cihazDizini(t *testing.T, uuid, mid string) string {
	t.Helper()
	d := t.TempDir()
	if uuid != "" {
		if err := os.WriteFile(filepath.Join(d, "product_uuid"), []byte(uuid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if mid != "" {
		if err := os.WriteFile(filepath.Join(d, "machine-id"), []byte(mid+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return d
}

func abonelikAnahtari() *Anahtar {
	return &Anahtar{V: 1, Kimlik: "L-TEST", Musteri: "Örnek AŞ", Paket: "pro", Tur: TurAbonelik,
		Bitis: Z(t0.AddDate(1, 0, 0)), Ozellikler: []string{"*"}, Verilis: Z(t0)}
}

func abonelikKirasi(panel string) *Kira {
	return &Kira{V: 1, Lisans: "L-TEST", Panel: panel, Tur: TurAbonelik, Musteri: "Örnek AŞ",
		LisansBitis: Z(t0.AddDate(1, 0, 0)), Ozellikler: []string{"*"}, Verilis: Z(t0),
		Gecerlilik: Z(t0.Add(KiraSuresi))}
}
