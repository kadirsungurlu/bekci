package mesh

import (
	"encoding/json"
	"flag"
	"os"
	"reflect"
	"testing"
)

var updateFixture = flag.Bool("update", false, "testdata/layout.json fikstürünü yeniden yaz")

// layoutCase web/e2e/meshlayout.test.mjs ile ortak fikstür satırı.
type layoutCase struct {
	N      int      `json:"n"`
	Width  int      `json:"width"`
	Height int      `json:"height"`
	Points [][2]int `json:"points"`
}

func TestLayoutFixture(t *testing.T) {
	var got []layoutCase
	for n := 2; n <= 12; n++ {
		_, _, _, w, h := LayoutSize(n)
		got = append(got, layoutCase{N: n, Width: w, Height: h, Points: LayoutPoints(n)})
	}
	const path = "testdata/layout.json"
	if *updateFixture {
		b, _ := json.MarshalIndent(got, "", " ")
		if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var want []layoutCase
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("yerleşim fikstürden farklı; bilinçliyse -update ile yenile ve TS aynasını güncelle")
	}
	for _, c := range got { // kutular tuvalin içinde
		for _, p := range c.Points {
			if p[0]-TopoBoxW/2 < 0 || p[0]+TopoBoxW/2 > c.Width || p[1]-TopoBoxH/2 < 0 || p[1]+TopoBoxH/2 > c.Height-TopoSummary {
				t.Fatalf("n=%d nokta %v tuval dışında", c.N, p)
			}
		}
	}
}

func TestBuildTopology(t *testing.T) {
	v := Verdict{
		Nodes: []int64{7, 0, 3},
		Pairs: []Pair{
			{A: 0, B: 3, State: PairUp, AB: StateUp, BA: StateUp},
			{A: 0, B: 7, State: PairOneWay, AB: StateDown, BA: StateUp},
			{A: 3, B: 7, State: PairDown, AB: StateDown, BA: StateDown},
		},
	}
	tp := BuildTopology(v)
	if len(tp.Nodes) != 3 || tp.Nodes[0].ID != 0 || !tp.Nodes[0].Main || tp.Nodes[1].ID != 3 {
		t.Fatalf("düğüm sırası: %+v", tp.Nodes)
	}
	if tp.Nodes[0].Y >= tp.Nodes[1].Y {
		t.Fatalf("Main tepede olmalı: %+v", tp.Nodes)
	}
	if len(tp.Links) != 3 || tp.Links[1].State != PairOneWay || tp.Links[1].Forward {
		t.Fatalf("bağlantılar: %+v", tp.Links)
	}
	if tp.Summary != (TopoSummaryCounts{Up: 1, Down: 1, OneWay: 1}) {
		t.Fatalf("özet: %+v", tp.Summary)
	}
	// çifti olmayan bağlantı ölçülemiyor sayılır
	tp = BuildTopology(Verdict{Nodes: []int64{0, 1}})
	if tp.Links[0].State != PairUnknown || tp.Summary.Unknown != 1 {
		t.Fatalf("bilinmeyen: %+v", tp)
	}
}
