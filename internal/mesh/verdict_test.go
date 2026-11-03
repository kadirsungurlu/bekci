package mesh

import (
	"reflect"
	"testing"
)

var five = []int64{0, 1, 2, 3, 4}

// full tüm yönlü kenarları up kurar, sonra ezmeleri uygular ("a>b" -> durum).
func full(over map[[2]int64]string) []Edge {
	var es []Edge
	for _, a := range five {
		for _, b := range five {
			if a == b {
				continue
			}
			st := StateUp
			if s, ok := over[[2]int64{a, b}]; ok {
				st = s
			}
			es = append(es, Edge{Src: a, Dst: b, State: st})
		}
	}
	return es
}

// both iki yönü aynı duruma çeker.
func both(m map[[2]int64]string, a, b int64, st string) {
	m[[2]int64{a, b}], m[[2]int64{b, a}] = st, st
}

func TestEvaluate(t *testing.T) {
	part := map[[2]int64]string{}
	for _, a := range []int64{0, 1, 2} {
		for _, b := range []int64{3, 4} {
			both(part, a, b, StateDown)
		}
	}
	node := map[[2]int64]string{}
	for _, o := range []int64{0, 1, 2, 4} {
		both(node, 3, o, StateDown)
	}
	// Ölü düğüm: kendi raporu bayat (unknown), diğerleri onu kopuk ölçüyor.
	dead := map[[2]int64]string{}
	for _, o := range []int64{0, 1, 2, 4} {
		dead[[2]int64{3, o}], dead[[2]int64{o, 3}] = StateUnknown, StateDown
	}
	link := map[[2]int64]string{}
	both(link, 1, 2, StateDown)
	oneway := map[[2]int64]string{{2, 1}: StateDown}
	mainPath := map[[2]int64]string{{0, 1}: StateDown, {1, 0}: StateUnknown}
	unmeasured := map[[2]int64]string{}
	for _, o := range []int64{0, 1, 2, 3} {
		both(unmeasured, 4, o, StateUnknown)
	}
	single := map[[2]int64]string{}
	for k, v := range unmeasured {
		single[k] = v
	}
	single[[2]int64{0, 4}] = StateDown

	cases := []struct {
		name string
		over map[[2]int64]string
		want []Finding
	}{
		{"sağlıklı", nil, []Finding{}},
		{"bölünme", part, []Finding{{Kind: KindPartition, Groups: [][]int64{{0, 1, 2}, {3, 4}}}}},
		{"düğüm", node, []Finding{{Kind: KindNode, Nodes: []int64{3}}}},
		{"ölü düğüm", dead, []Finding{{Kind: KindNode, Nodes: []int64{3}}}},
		{"tek bağlantı", link, []Finding{{Kind: KindLink, Nodes: []int64{1, 2}}}},
		{"Main yolu", mainPath, []Finding{{Kind: KindLink, Nodes: []int64{0, 1}}}},
		{"asimetrik", oneway, []Finding{{Kind: KindOneWay, Nodes: []int64{1, 2}}}},
		{"ölçülemiyor", unmeasured, []Finding{}},
		{"yetersiz gözlem", single, []Finding{{Kind: KindInsufficient, Nodes: []int64{0, 4}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := Evaluate(five, full(c.over))
			if !reflect.DeepEqual(v.Findings, c.want) {
				t.Fatalf("yorum = %+v, beklenen %+v", v.Findings, c.want)
			}
			if v.Healthy() != (len(c.want) == 0) {
				t.Fatalf("Healthy = %v", v.Healthy())
			}
		})
	}
}

func TestEvaluatePairsAndKey(t *testing.T) {
	v := Evaluate([]int64{2, 0, 1, 1}, []Edge{
		{0, 1, StateUp}, {1, 0, StateDown}, {1, 2, "garip"}, {9, 1, StateDown}, {1, 1, StateDown},
	})
	if !reflect.DeepEqual(v.Nodes, []int64{0, 1, 2}) || len(v.Pairs) != 3 {
		t.Fatalf("düğüm/çift: %v %d", v.Nodes, len(v.Pairs))
	}
	if p := v.Pair(1, 0); p.State != PairOneWay || p.AB != StateUp || p.BA != StateDown {
		t.Fatalf("çift 0-1 = %+v", p)
	}
	if p := v.Pair(1, 2); p.State != PairUnknown {
		t.Fatalf("çift 1-2 = %+v", p)
	}
	if v.Key() != "oneway:0,1;" || !v.NodeAffected(1) || v.NodeAffected(2) {
		t.Fatalf("anahtar %q", v.Key())
	}
	// Yalnız Main ve tüm kenarlar kopuk: düğüm sorunu değil (çalışan çift yok).
	all := Evaluate([]int64{0, 1, 2}, []Edge{{0, 1, StateDown}, {0, 2, StateDown}})
	if all.Key() != "insufficient:0,1;insufficient:0,2;" {
		t.Fatalf("hepsi kopuk: %q", all.Key())
	}
}
