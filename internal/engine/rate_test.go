package engine

import (
	"testing"
	"time"
)

func TestResultRate(t *testing.T) {
	var r resultRate
	base := time.Unix(1_000_000, 0)
	for i := range 30 {
		r.add(base.Add(time.Duration(i)*time.Second), 2)
	}
	// 30 sn x 2 = 60 sonuç / 60 sn = 1/sn; içinde bulunulan saniye sayılmaz.
	if got := r.perSecond(base.Add(30 * time.Second)); got != 1 {
		t.Fatalf("hız = %v, beklenen 1", got)
	}
	r.add(base.Add(30*time.Second), 100)
	if got := r.perSecond(base.Add(30 * time.Second)); got != 1 {
		t.Fatalf("yarım saniye sayıldı: %v", got)
	}
	// Pencere dışına çıkan kovalar düşer.
	if got := r.perSecond(base.Add(200 * time.Second)); got != 0 {
		t.Fatalf("eski kovalar sayıldı: %v", got)
	}
	r.add(base, 0) // sıfır eklemek kovayı bozmaz
}
