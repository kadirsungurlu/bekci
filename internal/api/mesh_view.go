package api

import (
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/mesh"
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
}

func (s *Server) meshMatrix(w http.ResponseWriter, r *http.Request) {
	rep, ok, err := s.engine.MeshSnapshot(r.Context(), s.now())
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp := meshViewResp{Active: ok, Nodes: []meshNodeView{}, Pairs: []mesh.Pair{}, Findings: []string{}}
	if ok {
		lang := responseLang(w)
		for _, id := range rep.Verdict.Nodes {
			resp.Nodes = append(resp.Nodes, meshNodeView{ID: id, Name: rep.Names.In(lang, id)})
		}
		resp.Pairs = rep.Verdict.Pairs
		if !rep.Verdict.Healthy() { // sağlıklıyken FindingLines "sağlıklı" satırı döner; arayüz kendi metnini yazar
			resp.Findings = mesh.FindingLines(lang, rep.Verdict, rep.Names)
		}
	}
	writeJSON(w, http.StatusOK, resp)
}
