package mesh

import (
	"math"
	"sort"
)

// Topoloji şeması yerleşimi (saf). Panel SVG'si (web/src/lib/meshlayout.ts)
// ve bildirim PNG'si (render.go) aynı koordinatları kullanır; iki taraf
// testdata/layout.json fikstürüyle karşılaştırılır. Formülü değiştirirsen
// ikisini birlikte değiştir ve fikstürü yenile: go test ./internal/mesh/ -run Layout -update

// Şema ölçüleri (SVG birimi; PNG bunu 2x çizer).
const (
	TopoBoxW    = 128 // düğüm kutusu genişliği
	TopoBoxH    = 36  // düğüm kutusu yüksekliği
	TopoPad     = 16  // kenar boşluğu
	TopoSummary = 40  // alttaki özet satırı yüksekliği
	topoMinR    = 110 // en küçük çember yarıçapı
	topoArc     = 160 // düğüm başına çember yayı
)

// TopoNode şemadaki düğüm; X,Y kutu merkezi.
type TopoNode struct {
	ID   int64 `json:"id"`
	X    int   `json:"x"`
	Y    int   `json:"y"`
	Main bool  `json:"main,omitempty"`
}

// TopoLink iki düğüm arası çizgi (A < B). State çift durumu (PairUp...);
// tek yönlüde Forward true ise A->B çalışır, false ise B->A.
type TopoLink struct {
	A       int64  `json:"a"`
	B       int64  `json:"b"`
	State   string `json:"state"`
	Forward bool   `json:"forward,omitempty"`
}

// TopoSummaryCounts özet satırı için çift sayıları.
type TopoSummaryCounts struct {
	Up      int `json:"up"`
	Down    int `json:"down"`
	OneWay  int `json:"oneway"`
	Unknown int `json:"unknown"`
}

// Topology çizilecek şema.
type Topology struct {
	Width   int               `json:"width"`
	Height  int               `json:"height"`
	Nodes   []TopoNode        `json:"nodes"`
	Links   []TopoLink        `json:"links"`
	Summary TopoSummaryCounts `json:"summary"`
}

// LayoutSize n düğüm için çember yarıçapı, merkez ve tuval boyutu.
func LayoutSize(n int) (r, cx, cy, w, h int) {
	r = int(math.Round(float64(n*topoArc) / (2 * math.Pi)))
	if r < topoMinR {
		r = topoMinR
	}
	cx = TopoPad + TopoBoxW/2 + r
	cy = TopoPad + TopoBoxH/2 + r
	if n <= 2 { // iki düğüm: dikey çizgi, yatay boşluk gereksiz
		cx = TopoPad + TopoBoxW/2
	}
	return r, cx, cy, 2 * cx, 2*cy + TopoSummary
}

// LayoutPoints sıradaki i. düğümün merkezi; 0. düğüm (Main) tepede,
// diğerleri saat yönünde eşit aralıklı.
func LayoutPoints(n int) [][2]int {
	r, cx, cy, _, _ := LayoutSize(n)
	pts := make([][2]int, n)
	for i := range n {
		a := -math.Pi/2 + 2*math.Pi*float64(i)/float64(n)
		pts[i] = [2]int{int(math.Round(float64(cx) + float64(r)*math.Cos(a))), int(math.Round(float64(cy) + float64(r)*math.Sin(a)))}
	}
	return pts
}

// BuildTopology karardan şemayı üretir. Main (varsa) ilk sırada, kontrol
// noktaları kimliğe göre artan sırada yerleşir.
func BuildTopology(v Verdict) Topology {
	ids := append([]int64(nil), v.Nodes...)
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] }) // MainID = 0 en başta
	_, _, _, w, h := LayoutSize(len(ids))
	t := Topology{Width: w, Height: h, Nodes: make([]TopoNode, 0, len(ids)), Links: []TopoLink{}}
	for i, p := range LayoutPoints(len(ids)) {
		t.Nodes = append(t.Nodes, TopoNode{ID: ids[i], X: p[0], Y: p[1], Main: ids[i] == MainID})
	}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			p := v.Pair(ids[i], ids[j])
			l := TopoLink{A: p.A, B: p.B, State: p.State}
			switch p.State {
			case PairUp:
				t.Summary.Up++
			case PairDown:
				t.Summary.Down++
			case PairOneWay:
				t.Summary.OneWay++
				l.Forward = p.AB == StateUp
			default:
				l.State = PairUnknown
				t.Summary.Unknown++
			}
			t.Links = append(t.Links, l)
		}
	}
	return t
}
