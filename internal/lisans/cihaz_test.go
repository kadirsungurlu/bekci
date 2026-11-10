package lisans

import (
	"strings"
	"testing"
)

func TestCihazKimligi(t *testing.T) {
	d := cihazDizini(t, " 4C4C4544-0000 ", "abcdef0123")
	id, err := cihazKimligiDizinlerle(d)
	if err != nil {
		t.Fatal(err)
	}
	if id != cihazFormul("4c4c4544-0000", "abcdef0123") || len(id) != 32 {
		t.Fatalf("formül yanlış: %s", id)
	}
	// Eksik dosyalı ilk dizin atlanır, ikinciden okunur.
	eksik := cihazDizini(t, "", "abcdef0123")
	id2, err := cihazKimligiDizinlerle(eksik, d)
	if err != nil || id2 != id {
		t.Fatalf("sıra yanlış: %s %v", id2, err)
	}
	_, err = cihazKimligiDizinlerle(eksik)
	if err == nil || !strings.Contains(err.Error(), "product_uuid") || !strings.Contains(err.Error(), "/run/bekci-host") {
		t.Fatalf("hata iletisi yetersiz: %v", err)
	}
}
