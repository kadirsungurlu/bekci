package api

import (
	"encoding/json"
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/mesh"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// meshIncidentView mesh olayının son yorumu, yanıt dilinde.
type meshIncidentView struct {
	Owner    string         `json:"owner"`
	Findings []string       `json:"findings"`
	Pairs    []meshPairView `json:"pairs"`
}

type meshPairView struct {
	A     int64  `json:"a"`
	B     int64  `json:"b"`
	State string `json:"state"` // up | down | oneway | unknown
	Text  string `json:"text"`
}

func meshView(lang string, r mesh.Report) *meshIncidentView {
	v := &meshIncidentView{Owner: r.Names.In(lang, r.Owner), Findings: mesh.FindingLines(lang, r.Verdict, r.Names),
		Pairs: make([]meshPairView, 0, len(r.Verdict.Pairs))}
	for _, p := range r.Verdict.Pairs {
		v.Pairs = append(v.Pairs, meshPairView{A: p.A, B: p.B, State: p.State, Text: mesh.PairText(lang, p, r.Names)})
	}
	return v
}

// meshIncident mesh olayının ayrıntısı: son yorum, tüm bağlantılar ve işlem
// geçmişi (yorum değişimleri o dilde yeniden üretilir).
func (s *Server) meshIncident(w http.ResponseWriter, r *http.Request, u store.User, inc store.Incident, updates []store.IncidentUpdate) {
	events, err := s.store.IncidentEvents(r.Context(), inc.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	events = completeEvents(inc, events)
	lang := responseLang(w)
	out := incidentDetailView{Incident: inc, Locations: []incidentLocationView{},
		Server: &incidentServer{Name: i18n.T(lang, "mesh.incident.name"), Active: true},
		Events: make([]store.IncidentEvent, 0, len(events)), Updates: updates}
	out.Incident.Cause = incidentCause(lang, inc)
	var rep mesh.Report
	if json.Unmarshal(inc.Data, &rep) == nil {
		out.Mesh = meshView(lang, rep)
	}
	for _, ev := range events {
		switch ev.Kind {
		case store.EventDown, store.EventMeshChange:
			ev.Message = store.MeshIncidentCause(lang, string(ev.Data))
			ev.Data = nil
		default:
			ev.Message = i18n.Message(lang, ev.Message)
		}
		out.Events = append(out.Events, ev)
	}
	writeJSON(w, http.StatusOK, out)
}
