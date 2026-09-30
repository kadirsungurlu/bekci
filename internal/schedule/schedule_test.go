package schedule

import (
	"testing"
	"time"
)

func TestNextOnGrid(t *testing.T) {
	const phase = 12_345 // ms
	every := time.Minute
	base := time.Unix(1_800_000_000, 0)
	for _, after := range []time.Time{base, base.Add(17 * time.Second), base.Add(59 * time.Second), base.Add(-3 * time.Hour)} {
		n := Next(after, every, phase)
		if !n.After(after) || n.Sub(after) > every {
			t.Fatalf("Next(%v) = %v: aralık dışında", after, n)
		}
		if got := (n.UnixMilli() - phase) % every.Milliseconds(); got != 0 {
			t.Fatalf("Next(%v) = %v ızgarada değil (kalan %d ms)", after, n, got)
		}
	}
	// Tam ızgara anında: bir sonrakine geçilir (kesin olarak sonra).
	on := Next(base, every, phase)
	if got := Next(on, every, phase); got.Sub(on) != every {
		t.Fatalf("ızgara anından sonraki: %v, beklenen +1 dk", got.Sub(on))
	}
}

func TestRetryGridContainsIntervalGrid(t *testing.T) {
	// 20 sn'lik tekrar deneme ızgarası 60 sn'lik ızgaranın anlarını içerir:
	// başarısız kontrolden sonra iki taraf da aynı tekrar deneme anlarına geçer.
	const phase = 777
	at := Next(time.Unix(1_800_000_000, 0), time.Minute, phase)
	if got := Next(at.Add(-time.Millisecond), 20*time.Second, phase); !got.Equal(at) {
		t.Fatalf("20 sn ızgarası 60 sn anını içermiyor: %v != %v", got, at)
	}
}

func TestPlannerNoDoubleAndNoSkip(t *testing.T) {
	p := Planner{PhaseMs: PhaseMs(42)}
	every := 10 * time.Second
	now := time.Unix(1_800_000_000, 500)
	d := p.Delay(now, every, true)
	if d < every/2 || d > every+every/2 {
		t.Fatalf("ilk bekleme %v: [%v, %v] dışında", d, every/2, every+every/2)
	}
	due := now.Add(d)
	// Zamanlayıcı 1 ms erken uyandı ve kontrol anında bitti: aynı an tekrar
	// planlanmaz, bir sonraki ızgara anı seçilir.
	if got := p.Delay(due.Add(-time.Millisecond), every, true); due.Add(-time.Millisecond).Add(got) != due.Add(every) {
		t.Fatalf("erken uyanma: sonraki an %v, beklenen %v", due.Add(-time.Millisecond).Add(got), due.Add(every))
	}
	// Yavaş kontrol (aralığın %80'i): sıradaki ızgara anı atlanmaz.
	due = due.Add(every)
	if got := p.Delay(due.Add(8*time.Second), every, true); got != 2*time.Second {
		t.Fatalf("yavaş kontrolden sonra bekleme %v, beklenen 2s", got)
	}
	// Kontrolden 1 sn sonra tekrar deneme aralığına (5 sn) geçiş: planlanmış
	// 10 sn'lik an beklenmez, 5 sn ızgarasındaki ilk an seçilir.
	ran := due.Add(10 * time.Second)
	p.Delay(ran, every, true)
	if got := p.Delay(ran.Add(time.Second), 5*time.Second, false); got != 4*time.Second {
		t.Fatalf("tekrar deneme aralığına geçiş: bekleme %v, beklenen 4s", got)
	}
}

func TestPhaseStableAndSpread(t *testing.T) {
	if PhaseMs(7) != PhaseMs(7) {
		t.Fatal("kayma sabit değil")
	}
	seen := map[int64]bool{}
	for id := int64(1); id <= 50; id++ {
		p := PhaseMs(id)
		if p < 0 || p >= phaseSpan {
			t.Fatalf("kayma aralık dışında: %d", p)
		}
		seen[p%60_000/1000] = true // 1 dk'lık aralıkta hangi saniye
	}
	if len(seen) < 25 {
		t.Fatalf("50 monitör 1 dk içinde yalnızca %d farklı saniyeye düştü", len(seen))
	}
}
