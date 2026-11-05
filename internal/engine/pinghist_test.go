package engine

import (
	"slices"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestPingHist(t *testing.T) {
	var h pingHist
	h.add(1, store.StatusUp, 10)
	h.add(1, store.StatusDown, 0)
	h.add(1, store.StatusUp, -1)
	if got := h.get(1); !slices.Equal(got, []int64{10, store.PingDown, store.PingNone}) {
		t.Fatalf("değerler: %v", got)
	}
	got := h.get(1)
	got[0] = 99
	if h.get(1)[0] != 10 {
		t.Fatal("get kopya vermeli")
	}
	for i := range PingHistLen + 5 {
		h.add(2, store.StatusUp, int64(i))
	}
	s := h.get(2)
	if len(s) != PingHistLen || s[0] != 5 || s[PingHistLen-1] != int64(PingHistLen+4) {
		t.Fatalf("halka: %d %v", len(s), s)
	}
	// Tohum bellektekilerin önüne geçer, taşanı atar, bellektekini ezmez.
	h.seed(map[int64][]int64{1: {1, 2}, 3: {7}})
	if got := h.get(1); !slices.Equal(got, []int64{1, 2, 10, store.PingDown, store.PingNone}) {
		t.Fatalf("tohum: %v", got)
	}
	if got := h.get(3); !slices.Equal(got, []int64{7}) {
		t.Fatalf("yeni tohum: %v", got)
	}
}
