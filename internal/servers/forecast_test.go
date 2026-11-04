package servers

import (
	"testing"
)

const gb = uint64(1) << 30

func line(from, step int64, n int, start, perStep, total uint64) []DiskPoint {
	out := make([]DiskPoint, n)
	for i := range n {
		out[i] = DiskPoint{T: from + int64(i)*step, Used: start + uint64(i)*perStep, Total: total}
	}
	return out
}

func TestForecastGrowing(t *testing.T) {
	// 100 GB'ın 50'si dolu, günde 2,4 GB: yaklaşık 20,8 gün.
	pts := line(0, 3600, 73, 40*gb, gb/10, 100*gb)
	now := pts[len(pts)-1].T
	f := Forecast("/", pts, now)
	if f.Status != ForecastGrowing || f.Days == nil {
		t.Fatalf("%+v", f)
	}
	free := float64(100*gb - pts[72].Used)
	want := free / float64(24*gb/10)
	if d := *f.Days - want; d < -0.2 || d > 0.2 {
		t.Fatalf("gün %v, beklenen %v", *f.Days, want)
	}
	if f.R2 < 0.99 {
		t.Fatalf("R2 %v", f.R2)
	}
}

func TestForecastLearningAndFlat(t *testing.T) {
	if f := Forecast("/", nil, 0); f.Status != ForecastLearning {
		t.Fatalf("%+v", f)
	}
	short := line(0, 3600, 6, 10*gb, gb, 100*gb) // 5 saat
	if f := Forecast("/", short, 18000); f.Status != ForecastLearning {
		t.Fatalf("kısa: %+v", f)
	}
	flat := line(0, 3600, 48, 50*gb, 0, 100*gb)
	if f := Forecast("/", flat, 47*3600); f.Status != ForecastFlat || f.Days != nil {
		t.Fatalf("düz: %+v", f)
	}
	down := make([]DiskPoint, 48)
	for i := range down { // yavaş azalma: dolmaz
		down[i] = DiskPoint{T: int64(i) * 3600, Used: 50*gb - uint64(i)*(gb/1000), Total: 100 * gb}
	}
	if f := Forecast("/", down, 47*3600); f.Status != ForecastFlat {
		t.Fatalf("azalan: %+v", f)
	}
}

// Temizlik (büyük düşüş) ve boyut büyütme eğimi böler: eski parça sayılmaz.
func TestForecastBreaks(t *testing.T) {
	a := line(0, 3600, 48, 40*gb, gb, 100*gb)           // hızlı dolum
	b := line(48*3600, 3600, 24, 20*gb, gb/100, 100*gb) // temizlik sonrası yavaş
	f := Forecast("/", append(a, b...), 71*3600)
	if f.Status != ForecastGrowing || f.PerDay > float64(gb) {
		t.Fatalf("temizlik: %+v", f)
	}
	c := line(0, 3600, 48, 40*gb, gb/10, 100*gb)
	d := line(48*3600, 3600, 5, 45*gb, gb/10, 200*gb) // disk büyütüldü, 5 nokta
	if f := Forecast("/", append(c, d...), 52*3600); f.Status != ForecastLearning {
		t.Fatalf("büyütme: %+v", f)
	}
}

func TestForecastWindowUsesLastWeek(t *testing.T) {
	// 30 gün yavaş, son 7 gün hızlı: tahmin hızlı olana, 30 günlük eğim ayrı alanda.
	old := line(0, 3600, 23*24, 10*gb, gb/1000, 1000*gb)
	last := old[len(old)-1]
	recent := line(last.T+3600, 3600, 7*24, last.Used, gb/10, 1000*gb)
	pts := append(old, recent...)
	now := pts[len(pts)-1].T
	f := Forecast("/", pts, now)
	if f.PerDay < float64(2*gb) || f.PerDay30 >= f.PerDay || f.PerDay30 == 0 {
		t.Fatalf("%+v", f)
	}
}

func TestFastRise(t *testing.T) {
	var h []point
	for i := range 10 {
		h = append(h, point{t: int64(i) * 60, disks: map[string]float64{"/": 50 + float64(i), "/a": 10}})
	}
	m, r, ok := fastRise(h, 60, 540)
	if !ok || m != "/" || r != 9 {
		t.Fatalf("%q %v %v", m, r, ok)
	}
	if _, _, ok := fastRise(h[:3], 60, 120); ok {
		t.Fatal("az örnekle karar verilmemeli")
	}
}
