package mesh

import (
	"strconv"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// Names düğüm kimliği -> görünen ad (Main dahil).
type Names map[int64]string

// Of düğümün adı; yoksa "#id".
func (n Names) Of(id int64) string {
	if s, ok := n[id]; ok && s != "" {
		return s
	}
	return "#" + strconv.FormatInt(id, 10)
}

// in düğümün adı dilde (Main'in varsayılan adı gibi katalog metinleri çevrilir).
func (n Names) In(lang string, id int64) string { return i18n.Message(lang, n.Of(id)) }

// FindingText yorumun dilde tek satırı.
func FindingText(lang string, f Finding, n Names) string {
	switch f.Kind {
	case KindPartition:
		groups := make([]string, 0, len(f.Groups))
		for _, g := range f.Groups {
			names := make([]string, 0, len(g))
			for _, id := range g {
				names = append(names, n.In(lang, id))
			}
			groups = append(groups, "["+strings.Join(names, ", ")+"]")
		}
		return i18n.T(lang, "mesh.finding.partition", strings.Join(groups, " | "))
	case KindNode:
		return i18n.T(lang, "mesh.finding.node", n.In(lang, f.Nodes[0]))
	case KindOneWay:
		a, b := n.In(lang, f.Nodes[0]), n.In(lang, f.Nodes[1])
		return i18n.T(lang, "mesh.finding.oneway", a, b, b, a)
	case KindLink, KindInsufficient:
		return i18n.T(lang, "mesh.finding."+f.Kind, n.In(lang, f.Nodes[0]), n.In(lang, f.Nodes[1]))
	}
	return f.Kind
}

// FindingLines tüm yorumlar (yoksa "Tüm bağlantılar sağlıklı.").
func FindingLines(lang string, v Verdict, n Names) []string {
	if v.Healthy() {
		return []string{i18n.T(lang, "mesh.healthy")}
	}
	out := make([]string, 0, len(v.Findings))
	for _, f := range v.Findings {
		out = append(out, FindingText(lang, f, n))
	}
	return out
}

// PairText çiftin dilde satırı ("A > B arası bağlantı sağlıklı").
func PairText(lang string, p Pair, n Names) string {
	a, b := n.In(lang, p.A), n.In(lang, p.B)
	if p.State == PairOneWay {
		from, to := a, b
		if p.AB != StateUp {
			from, to = b, a
		}
		return i18n.T(lang, "mesh.pair.oneway", a, b, from, to)
	}
	return i18n.T(lang, "mesh.pair."+p.State, a, b)
}

// PairLines tüm çiftler (kimlik sırasıyla).
func PairLines(lang string, v Verdict, n Names) []string {
	out := make([]string, 0, len(v.Pairs))
	for _, p := range v.Pairs {
		out = append(out, PairText(lang, p, n))
	}
	return out
}

// Report bildirim ve olay verisinde taşınan mesh özeti.
type Report struct {
	Verdict Verdict `json:"verdict"`
	Names   Names   `json:"names"`
	Owner   int64   `json:"owner"` // mesajı üreten düğüm (Main = 0)
}

// Probes yorumlarda geçen kontrol noktaları (Main hariç, artan, tekrarsız).
func (r Report) Probes() []int64 {
	seen := map[int64]bool{}
	var out []int64
	add := func(id int64) {
		if id != MainID && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, f := range r.Verdict.Findings {
		for _, id := range f.Nodes {
			add(id)
		}
		for _, g := range f.Groups {
			for _, id := range g {
				add(id)
			}
		}
	}
	sortIDs(out)
	return out
}

func sortIDs(s []int64) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
