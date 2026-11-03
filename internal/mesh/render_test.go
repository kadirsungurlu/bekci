package mesh

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// renderCases altın dosyalı örnekler: 3 düğüm (tüm durumlar, Türkçe adlar)
// ve 8 düğüm. Bilinçli görsel değişiklikte: go test ./internal/mesh/ -run Render -update
func renderCases() []struct {
	file  string
	v     Verdict
	names Names
	lang  string
} {
	v3 := Verdict{Nodes: []int64{0, 3, 7}, Pairs: []Pair{
		{A: 0, B: 3, State: PairUp, AB: StateUp, BA: StateUp},
		{A: 0, B: 7, State: PairOneWay, AB: StateDown, BA: StateUp},
		{A: 3, B: 7, State: PairDown, AB: StateDown, BA: StateDown},
	}, Findings: []Finding{{}}}
	n3 := Names{0: "Ana sunucu", 3: "İstanbul Çekmeköy", 7: "Şişli ölçüm noktası ğüşıöçİ uzun ad"}
	ids := []int64{0, 1, 2, 3, 4, 5, 6, 7}
	v8 := Verdict{Nodes: ids}
	n8 := Names{0: "Ana sunucu"}
	for i, a := range ids {
		for _, b := range ids[i+1:] {
			p := Pair{A: a, B: b, State: PairUp, AB: StateUp, BA: StateUp}
			switch {
			case a == 5 || b == 5:
				p.State, p.AB, p.BA = PairDown, StateDown, StateDown
			case a == 2 && b == 6:
				p.State, p.AB, p.BA = PairOneWay, StateUp, StateDown
			case b == 7 && a > 2:
				p.State = PairUnknown
			}
			v8.Pairs = append(v8.Pairs, p)
		}
	}
	for _, id := range ids[1:] {
		n8[id] = []string{"", "Frankfurt", "Amsterdam", "Gaziantep", "Muğla", "Düzce", "Iğdır", "Singapur"}[id]
	}
	healthy := Verdict{Nodes: []int64{0, 1, 2}, Pairs: []Pair{
		{A: 0, B: 1, State: PairUp}, {A: 0, B: 2, State: PairUp}, {A: 1, B: 2, State: PairUp},
	}}
	return []struct {
		file  string
		v     Verdict
		names Names
		lang  string
	}{
		{"topo3.png", v3, n3, "tr"},
		{"topo8.png", v8, n8, "tr"},
		{"topo3-ok-en.png", healthy, Names{0: "Main server", 1: "Berlin", 2: "Paris"}, "en"},
	}
}

func TestRenderPNG(t *testing.T) {
	for _, c := range renderCases() {
		tp := BuildTopology(c.v)
		data, err := RenderPNG(tp, c.names, c.lang)
		if err != nil {
			t.Fatalf("%s: %v", c.file, err)
		}
		if len(data) > 150<<10 {
			t.Errorf("%s: %d bayt, sınır 150 KB", c.file, len(data))
		}
		if !raceOn {
			d := time.Hour // en iyi 5 deneme: yüklü makinede gürültüyü ayıklar
			for range 5 {
				start := time.Now()
				if _, err := RenderPNG(tp, c.names, c.lang); err != nil {
					t.Fatal(err)
				}
				d = min(d, time.Since(start))
			}
			if d > 50*time.Millisecond {
				t.Errorf("%s: çizim %v, sınır 50 ms", c.file, d)
			}
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("%s: png çözülemedi: %v", c.file, err)
		}
		if b := img.Bounds(); b.Dx() != tp.Width*renderScale || b.Dy() != tp.Height*renderScale {
			t.Fatalf("%s: boyut %v", c.file, b)
		}
		golden := filepath.Join("testdata", c.file)
		if *updateFixture {
			if err := os.WriteFile(golden, data, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		f, err := os.Open(golden)
		if err != nil {
			t.Fatalf("%s: altın dosya yok (-update ile üret): %v", c.file, err)
		}
		want, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if r := diffRatio(img, want); r > 0.005 {
			t.Errorf("%s: altın dosyadan farklı piksel oranı %.4f (eşik 0.005); bilinçliyse -update", c.file, r)
		}
	}
}

// diffRatio kanal farkı 48'i aşan piksellerin oranı (kenar yumuşatma toleransı).
func diffRatio(a, b image.Image) float64 {
	if a.Bounds() != b.Bounds() {
		return 1
	}
	r := a.Bounds()
	bad := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			r1, g1, b1, _ := a.At(x, y).RGBA()
			r2, g2, b2, _ := b.At(x, y).RGBA()
			if absd(r1, r2) > 48<<8 || absd(g1, g2) > 48<<8 || absd(b1, b2) > 48<<8 {
				bad++
			}
		}
	}
	return float64(bad) / float64(r.Dx()*r.Dy())
}

func absd(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func TestTopologySummary(t *testing.T) {
	tp := Topology{Summary: TopoSummaryCounts{Up: 2, Down: 1, Unknown: 3}}
	if got := TopologySummary("tr", tp); got != "2 sağlıklı · 1 koptu · 3 ölçülemiyor" {
		t.Fatalf("özet: %q", got)
	}
	if got := TopologySummary("en", Topology{Summary: TopoSummaryCounts{Up: 3}}); got != "Two-way reachability OK" {
		t.Fatalf("özet: %q", got)
	}
}

func BenchmarkRenderPNG8(b *testing.B) {
	c := renderCases()[1]
	tp := BuildTopology(c.v)
	for b.Loop() {
		if _, err := RenderPNG(tp, c.names, c.lang); err != nil {
			b.Fatal(err)
		}
	}
}
