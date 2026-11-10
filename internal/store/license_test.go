package store

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestLisansAyarlari(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	t0 := time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)

	// Eşzamanlı ilk açılış: tek panel kimliği ve kurulum zamanı kalır.
	var wg sync.WaitGroup
	ids := make([]string, 4)
	for i := range ids {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := s.LisansHazirla(ctx, t0.Add(time.Duration(i)*time.Hour))
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = a.PanelID
		}()
	}
	wg.Wait()
	a, err := s.LisansHazirla(ctx, t0.Add(48*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(a.PanelID) {
		t.Fatalf("panel kimliği UUID v4 değil: %q", a.PanelID)
	}
	for _, id := range ids {
		if id != a.PanelID {
			t.Fatalf("panel kimliği değişti: %v / %s", ids, a.PanelID)
		}
	}
	if a.Kurulum.IsZero() || a.Kurulum.After(t0.Add(4*time.Hour)) {
		t.Fatalf("kurulum zamanı ezildi: %v", a.Kurulum)
	}

	// En ileri zaman yalnız ileri yazılır.
	if got, _ := s.LisansEnIleriYaz(ctx, t0.Add(time.Hour)); !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("en ileri: %v", got)
	}
	if got, _ := s.LisansEnIleriYaz(ctx, t0); !got.Equal(t0.Add(time.Hour)) {
		t.Fatalf("en ileri geri yazıldı: %v", got)
	}

	if err := s.LisansYaz(ctx, map[string]string{LisansAnahtar: "BEKCI-L1.x.y", LisansKira: "BEKCI-K1.x.y", LisansSonHata: "h"}); err != nil {
		t.Fatal(err)
	}
	if err := s.LisansYaz(ctx, map[string]string{"smtp": "x"}); err == nil {
		t.Fatal("lisans dışı anahtar yazılmamalı")
	}
	if err := s.LisansSil(ctx, LisansKira); err != nil {
		t.Fatal(err)
	}
	a, _ = s.LisansOku(ctx)
	if a.Anahtar != "BEKCI-L1.x.y" || a.Kira != "" || a.SonHata != "h" || !a.EnIleri.Equal(t0.Add(time.Hour)) {
		t.Fatalf("okunan: %+v", a)
	}
}
