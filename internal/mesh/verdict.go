// Package mesh kontrol noktaları (ve Main) arasındaki bağlantı ölçümlerini
// yorumlar. Saf paket: veritabanı, ağ ya da saat kullanmaz; hem panel hem
// ajan (yedek bildirici) aynı kararı üretir.
package mesh

import (
	"sort"
	"strconv"
	"strings"
)

// MainID ana sunucunun (panel) düğüm kimliği.
const MainID int64 = 0

// Yönlü ölçüm durumları (store.LinkUp/LinkDown/LinkUnknown ile aynı değerler).
const (
	StateUp      = "up"
	StateDown    = "down"
	StateUnknown = "unknown"
)

// Çift (iki yön birlikte) durumları.
const (
	PairUp      = "up"      // en az bir yön çalışıyor, diğeri kopuk değil
	PairDown    = "down"    // bilinen yönlerin hepsi kopuk
	PairOneWay  = "oneway"  // bir yön çalışıyor, diğeri kopuk
	PairUnknown = "unknown" // iki yön de ölçülemiyor / bayat
)

// Yorum türleri.
const (
	KindPartition    = "partition"    // ağ en az iki gruba bölündü
	KindNode         = "node"         // düğümün genel erişim sorunu
	KindLink         = "link"         // tek bağlantı sorunu (yol sorunu)
	KindOneWay       = "oneway"       // tek yönlü bağlantı
	KindInsufficient = "insufficient" // bağlantı koptu, yorum için yetersiz gözlem
)

// Edge yönlü ölçüm: Src, Dst'ye ulaşabiliyor mu. Bayat ölçüm çağıran
// tarafından StateUnknown'a çevrilmiş olmalıdır.
type Edge struct {
	Src   int64  `json:"src"`
	Dst   int64  `json:"dst"`
	State string `json:"state"`
}

// Pair iki düğüm arasındaki birleşik durum (A < B). AB: A'nın B'yi ölçümü.
type Pair struct {
	A     int64  `json:"a"`
	B     int64  `json:"b"`
	State string `json:"state"`
	AB    string `json:"ab"`
	BA    string `json:"ba"`
}

// Finding tek yorum. Nodes: partition dışındaki türlerde ilgili düğümler
// (node/insufficient tek ya da iki, link iki); oneway'de Nodes[0] -> Nodes[1]
// yönü çalışır, ters yön kopuktur. Groups yalnız partition'da.
type Finding struct {
	Kind   string    `json:"kind"`
	Nodes  []int64   `json:"nodes,omitempty"`
	Groups [][]int64 `json:"groups,omitempty"`
}

// Verdict değerlendirme sonucu.
type Verdict struct {
	Nodes      []int64   `json:"nodes"`
	Pairs      []Pair    `json:"pairs"`
	Components [][]int64 `json:"components"`
	Findings   []Finding `json:"findings"`
}

// Healthy yorum yoksa true (tüm bilinen bağlantılar sağlıklı).
func (v Verdict) Healthy() bool { return len(v.Findings) == 0 }

// Key yorumların kararlı özeti; değişim tespiti için.
func (v Verdict) Key() string {
	var b strings.Builder
	for _, f := range v.Findings {
		b.WriteString(f.Kind)
		b.WriteByte(':')
		writeIDs(&b, f.Nodes)
		for _, g := range f.Groups {
			b.WriteByte('|')
			writeIDs(&b, g)
		}
		b.WriteByte(';')
	}
	return b.String()
}

// NodeAffected düğüm herhangi bir yorumda geçiyor mu.
func (v Verdict) NodeAffected(id int64) bool {
	for _, f := range v.Findings {
		for _, n := range f.Nodes {
			if n == id {
				return true
			}
		}
	}
	return false
}

// Pair a ve b arasındaki çift (sıra önemsiz); yoksa bilinmeyen.
func (v Verdict) Pair(a, b int64) Pair {
	if a > b {
		a, b = b, a
	}
	for _, p := range v.Pairs {
		if p.A == a && p.B == b {
			return p
		}
	}
	return Pair{A: a, B: b, State: PairUnknown, AB: StateUnknown, BA: StateUnknown}
}

func writeIDs(b *strings.Builder, ids []int64) {
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatInt(id, 10))
	}
}

// combine iki yönün durumundan çift durumu.
func combine(ab, ba string) string {
	up := ab == StateUp || ba == StateUp
	down := ab == StateDown || ba == StateDown
	switch {
	case up && down:
		return PairOneWay
	case up:
		return PairUp
	case down:
		return PairDown
	}
	return PairUnknown
}

func norm(s string) string {
	if s == StateUp || s == StateDown {
		return s
	}
	return StateUnknown
}

// Evaluate düğümler ve yönlü ölçümlerden kararı üretir. Kurallar:
//   - Bileşenler: up ya da tek yönlü çiftlerle bağlı düğüm grupları.
//   - Düğüm sorunu: bilinen çifti >= 2, hiçbiri çalışmıyor ve diğer düğümler
//     arasında en az bir çalışan çift var. O düğümün kopuk çiftleri ayrıca
//     yorumlanmaz (bastırılır).
//   - Bölünme: boyutu >= 2 olan en az iki bileşen ve aralarında en az bir
//     bilinen (kopuk) çift; aralarındaki kopuk çiftler ayrıca yorumlanmaz.
//   - Tek bağlantı: kopuk çiftin iki ucu aynı bileşende (başka yoldan ulaşılıyor).
//   - Yetersiz gözlem: kalan kopuk çiftler (ör. bilinen çifti < 2 olan uç).
//   - Tek yönlü: bir yön çalışıyor, diğeri kopuk.
func Evaluate(nodes []int64, edges []Edge) Verdict {
	ns := append([]int64(nil), nodes...)
	sort.Slice(ns, func(i, j int) bool { return ns[i] < ns[j] })
	ns = dedup(ns)
	idx := make(map[int64]int, len(ns))
	for i, n := range ns {
		idx[n] = i
	}
	type key struct{ s, d int64 }
	dir := map[key]string{}
	for _, e := range edges {
		_, okS := idx[e.Src]
		_, okD := idx[e.Dst]
		if !okS || !okD || e.Src == e.Dst {
			continue
		}
		dir[key{e.Src, e.Dst}] = norm(e.State)
	}
	get := func(s, d int64) string {
		if v, ok := dir[key{s, d}]; ok {
			return v
		}
		return StateUnknown
	}

	v := Verdict{Nodes: ns, Pairs: []Pair{}, Components: [][]int64{}, Findings: []Finding{}}
	parent := make([]int, len(ns))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	known := make([]int, len(ns))
	working := make([]bool, len(ns))
	anyWorking := 0
	for i := 0; i < len(ns); i++ {
		for j := i + 1; j < len(ns); j++ {
			a, b := ns[i], ns[j]
			p := Pair{A: a, B: b, AB: get(a, b), BA: get(b, a)}
			p.State = combine(p.AB, p.BA)
			v.Pairs = append(v.Pairs, p)
			if p.State == PairUnknown {
				continue
			}
			known[i]++
			known[j]++
			if p.State == PairUp || p.State == PairOneWay {
				working[i], working[j] = true, true
				anyWorking++
				if ri, rj := find(i), find(j); ri != rj {
					parent[rj] = ri
				}
			}
		}
	}

	// Bileşenler (düğüm sırasına göre, her biri artan).
	compOf := make([]int, len(ns))
	byRoot := map[int]int{}
	for i := range ns {
		r := find(i)
		c, ok := byRoot[r]
		if !ok {
			c = len(v.Components)
			byRoot[r] = c
			v.Components = append(v.Components, nil)
		}
		compOf[i] = c
		v.Components[c] = append(v.Components[c], ns[i])
	}

	// Düğüm sorunu: diğerleri arasında çalışan çift = düğümün kendisine ait
	// olmayan en az bir çalışan çift. Düğüm hiç çalışmadığı için anyWorking
	// sayısı tümüyle diğerlerine aittir.
	nodeIssue := make([]bool, len(ns))
	for i, n := range ns {
		if known[i] >= 2 && !working[i] && anyWorking > 0 {
			nodeIssue[i] = true
			v.Findings = append(v.Findings, Finding{Kind: KindNode, Nodes: []int64{n}})
		}
	}

	// Bölünme.
	big := map[int]bool{}
	for c, members := range v.Components {
		if len(members) >= 2 {
			big[c] = true
		}
	}
	partition := false
	if len(big) >= 2 {
		for _, p := range v.Pairs {
			ca, cb := compOf[idx[p.A]], compOf[idx[p.B]]
			if p.State == PairDown && ca != cb && big[ca] && big[cb] {
				partition = true
				break
			}
		}
	}
	if partition {
		f := Finding{Kind: KindPartition}
		for c, members := range v.Components {
			if big[c] {
				f.Groups = append(f.Groups, members)
			}
		}
		v.Findings = append(v.Findings, f)
	}

	for _, p := range v.Pairs {
		ia, ib := idx[p.A], idx[p.B]
		switch p.State {
		case PairOneWay:
			from, to := p.A, p.B
			if p.AB != StateUp {
				from, to = p.B, p.A
			}
			v.Findings = append(v.Findings, Finding{Kind: KindOneWay, Nodes: []int64{from, to}})
		case PairDown:
			ca, cb := compOf[ia], compOf[ib]
			switch {
			case nodeIssue[ia] || nodeIssue[ib]:
			case partition && ca != cb && big[ca] && big[cb]:
			case ca == cb:
				v.Findings = append(v.Findings, Finding{Kind: KindLink, Nodes: []int64{p.A, p.B}})
			default:
				v.Findings = append(v.Findings, Finding{Kind: KindInsufficient, Nodes: []int64{p.A, p.B}})
			}
		}
	}
	order := map[string]int{KindPartition: 0, KindNode: 1, KindLink: 2, KindOneWay: 3, KindInsufficient: 4}
	sort.SliceStable(v.Findings, func(i, j int) bool { return order[v.Findings[i].Kind] < order[v.Findings[j].Kind] })
	return v
}

func dedup(s []int64) []int64 {
	out := s[:0]
	for i, x := range s {
		if i == 0 || x != s[i-1] {
			out = append(out, x)
		}
	}
	return out
}
