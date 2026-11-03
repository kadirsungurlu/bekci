package api

import (
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/mesh"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// GET /api/mesh: kontrol noktaları arası bağlantı matrisi ve yorum (yönetici).
// active=false: mesh kapalı ya da kalp atışı gönderen nokta 2'den az.

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/mesh", s.admin(s.meshMatrix))
	})
}

type meshNodeView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type meshViewResp struct {
	Active   bool           `json:"active"`
	Nodes    []meshNodeView `json:"nodes"`
	Pairs    []mesh.Pair    `json:"pairs"`
	Findings []string       `json:"findings"`
	Links    []meshLinkView `json:"links"`
}

// meshLinkView yönlü bağlantı: src, dst'ye ulaşıyor mu. State karardaki
// (bayatlık uygulanmış) durumdur; ayrıntı alanları yalnız kontrol noktaları
// arası ölçümde (ajan raporu) vardır, Main kenarlarında yoktur.
type meshLinkView struct {
	Src        int64  `json:"src"`
	Dst        int64  `json:"dst"`
	State      string `json:"state"`
	RTTms      *int64 `json:"rtt_ms,omitempty"`
	LossStreak *int   `json:"loss_streak,omitempty"`
	LastOKAt   *int64 `json:"last_ok_at,omitempty"`
}

// meshLinkViews çiftlerden iki yönlü bağlantı listesi; ölçüm ayrıntısı store'dan.
func meshLinkViews(pairs []mesh.Pair, stored []store.ProbeLink) []meshLinkView {
	type key struct{ s, d int64 }
	by := make(map[key]store.ProbeLink, len(stored))
	for _, l := range stored {
		by[key{l.Src, l.Dst}] = l
	}
	out := make([]meshLinkView, 0, 2*len(pairs))
	add := func(src, dst int64, st string) {
		v := meshLinkView{Src: src, Dst: dst, State: st}
		if l, ok := by[key{src, dst}]; ok && src != mesh.MainID && dst != mesh.MainID {
			rtt, loss, last := l.RTTms, l.LossStreak, l.LastOKAt
			v.RTTms, v.LossStreak, v.LastOKAt = &rtt, &loss, &last
		}
		out = append(out, v)
	}
	for _, p := range pairs {
		add(p.A, p.B, p.AB)
		add(p.B, p.A, p.BA)
	}
	return out
}

func (s *Server) meshMatrix(w http.ResponseWriter, r *http.Request) {
	rep, ok, err := s.engine.MeshSnapshot(r.Context(), s.now())
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp := meshViewResp{Active: ok, Nodes: []meshNodeView{}, Pairs: []mesh.Pair{}, Findings: []string{}, Links: []meshLinkView{}}
	if ok {
		lang := responseLang(w)
		for _, id := range rep.Verdict.Nodes {
			resp.Nodes = append(resp.Nodes, meshNodeView{ID: id, Name: rep.Names.In(lang, id)})
		}
		resp.Pairs = rep.Verdict.Pairs
		stored, err := s.store.ListProbeLinks(r.Context())
		if err != nil {
			s.dbError(w, err)
			return
		}
		resp.Links = meshLinkViews(rep.Verdict.Pairs, stored)
		if !rep.Verdict.Healthy() { // sağlıklıyken FindingLines "sağlıklı" satırı döner; arayüz kendi metnini yazar
			resp.Findings = mesh.FindingLines(lang, rep.Verdict, rep.Names)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
