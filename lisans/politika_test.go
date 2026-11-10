package lisans

import (
	"testing"
	"time"
)

func TestTasimaIzni(t *testing.T) {
	gun := 24 * time.Hour
	if ok, _ := TasimaIzni(time.Time{}, t0); !ok {
		t.Fatal("hiç taşınmamışsa izin olmalı")
	}
	ok, kalan := TasimaIzni(t0, t0.Add(29*gun))
	if ok || kalan != gun {
		t.Fatalf("29 gün: ok=%v kalan=%v", ok, kalan)
	}
	for _, n := range []int{30, 31} {
		if ok, k := TasimaIzni(t0, t0.Add(time.Duration(n)*gun)); !ok || k != 0 {
			t.Fatalf("%d gün: izin olmalı", n)
		}
	}
}
