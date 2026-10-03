package api

// Manuel olaylar ve olay güncellemeleri (durum sayfası iletişimi).
//
//	POST   /api/incidents                       manuel olay aç (editör+)
//	PUT    /api/incidents/{id}                  başlık, önem, etkilenen monitörler (manuel; editör+)
//	DELETE /api/incidents/{id}                  manuel olayı sil (editör+)
//	POST   /api/incidents/{id}/updates          güncelleme ekle (manuel ve otomatik; editör+)
//	DELETE /api/incidents/{id}/updates/{uid}    güncellemeyi sil (editör+)
//
// Manuel olay bir durum sayfasına bağlıdır ve herkese açık sayfada başlığı,
// önemi ve güncellemeleriyle görünür; izleyici ve müşteri kullanıcılar
// yalnızca okur. Otomatik olaylara da güncelleme yazılabilir (ör. "nedeni
// bulduk, düzeltiyoruz"); otomatik olayın kapanışını monitör belirler, bu
// yüzden açık otomatik olaya "resolved" aşaması yazılamaz.

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/incidents", s.editor(s.createManualIncident))
		mux.Handle("PUT /api/incidents/{id}", s.editor(s.updateManualIncident))
		mux.Handle("DELETE /api/incidents/{id}", s.editor(s.deleteManualIncident))
		mux.Handle("POST /api/incidents/{id}/updates", s.editor(s.addIncidentUpdate))
		mux.Handle("DELETE /api/incidents/{id}/updates/{uid}", s.editor(s.deleteIncidentUpdate))
	})
}

type manualIncidentInput struct {
	PageID     int64   `json:"page_id"`
	Title      string  `json:"title"`
	Severity   string  `json:"severity"` // minor | major | critical
	MonitorIDs []int64 `json:"monitor_ids"`
	StartedAt  int64   `json:"started_at"` // 0: şimdi
	// İlk güncelleme (yalnızca eklemede).
	State string `json:"state"` // boş: investigating
	Body  string `json:"body"`
}

type incidentUpdateInput struct {
	State string `json:"state"`
	Body  string `json:"body"`
}

const (
	maxIncidentTitle = 200
	maxUpdateBody    = 5000
	maxAffected      = 200
)

func normalizeIncidentTitle(title string) (string, error) {
	title = strings.TrimSpace(title)
	if n := utf8.RuneCountInString(title); n < 1 || n > maxIncidentTitle {
		return "", badInput("Olay başlığı 1-200 karakter olmalı")
	}
	return title, nil
}

func normalizeSeverity(sev string) (string, error) {
	sev = strings.TrimSpace(sev)
	if sev == "" {
		sev = store.SeverityMajor
	}
	if !store.ValidSeverity(sev) {
		return "", badInput("Önem derecesi minor, major veya critical olmalı")
	}
	return sev, nil
}

func normalizeUpdate(in incidentUpdateInput) (incidentUpdateInput, error) {
	in.State = strings.TrimSpace(in.State)
	if in.State == "" {
		in.State = store.StateInvestigating
	}
	if !store.ValidState(in.State) {
		return in, badInput("Aşama investigating, identified, monitoring veya resolved olmalı")
	}
	in.Body = strings.TrimSpace(in.Body)
	if utf8.RuneCountInString(in.Body) > maxUpdateBody {
		return in, badInput("Güncelleme metni en fazla 5000 karakter olabilir")
	}
	return in, nil
}

// affectedMonitors etkilenen monitörleri doğrular (var olmalı; tekil).
func (s *Server) affectedMonitors(r *http.Request, ids []int64) ([]int64, error) {
	if len(ids) > maxAffected {
		return nil, badInput("En fazla 200 monitör seçilebilir")
	}
	seen := map[int64]bool{}
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	existing, err := s.store.MonitorsByIDs(r.Context(), out)
	if err != nil {
		return nil, err
	}
	for _, id := range out {
		if _, ok := existing[id]; !ok {
			return nil, badInput("Seçilen monitörlerden biri bulunamadı")
		}
	}
	return out, nil
}

func (s *Server) createManualIncident(w http.ResponseWriter, r *http.Request) {
	var in manualIncidentInput
	if !readJSON(w, r, &in) {
		return
	}
	page, err := s.store.GetPage(r.Context(), in.PageID)
	if err != nil {
		if in.PageID <= 0 {
			writeError(w, http.StatusBadRequest, "Olayın bağlanacağı durum sayfasını seçin")
			return
		}
		s.dbError(w, err)
		return
	}
	title, err := normalizeIncidentTitle(in.Title)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	sev, err := normalizeSeverity(in.Severity)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	upd, err := normalizeUpdate(incidentUpdateInput{State: in.State, Body: in.Body})
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	ids, err := s.affectedMonitors(r, in.MonitorIDs)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	now := s.now().Unix()
	started := in.StartedAt
	if started <= 0 || started > now {
		started = now
	}
	u := userFrom(r)
	id, err := s.store.CreateManualIncident(r.Context(), store.ManualIncidentInput{
		PageID: page.ID, Title: title, Severity: sev, MonitorIDs: ids, StartedAt: started,
		CreatedBy: u.Username, UserID: u.ID, State: upd.State, Body: upd.Body,
	})
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "incident.create", "incident", id, title, "sayfa: "+page.Title+", "+sev)
	s.hub.Publish("incident", map[string]any{"incident_id": id, "kind": store.IncidentManual})
	inc, err := s.store.GetIncident(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, inc)
}

func (s *Server) manualIncident(w http.ResponseWriter, r *http.Request) (store.Incident, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return store.Incident{}, false
	}
	inc, err := s.store.GetIncident(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return inc, false
	}
	if inc.Kind != store.IncidentManual {
		writeError(w, http.StatusBadRequest, "Yalnızca elle açılan olaylar düzenlenebilir ve silinebilir")
		return inc, false
	}
	return inc, true
}

func (s *Server) updateManualIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.manualIncident(w, r)
	if !ok {
		return
	}
	var in manualIncidentInput
	if !readJSON(w, r, &in) {
		return
	}
	title, err := normalizeIncidentTitle(in.Title)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	sev, err := normalizeSeverity(in.Severity)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	ids, err := s.affectedMonitors(r, in.MonitorIDs)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.UpdateManualIncident(r.Context(), inc.ID, title, sev, ids); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "incident.update", "incident", inc.ID, title, sev)
	s.hub.Publish("incident", map[string]any{"incident_id": inc.ID, "kind": store.IncidentManual})
	out, err := s.store.GetIncident(r.Context(), inc.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteManualIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.manualIncident(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteManualIncident(r.Context(), inc.ID); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "incident.delete", "incident", inc.ID, inc.Title, "")
	s.hub.Publish("incident", map[string]any{"incident_id": inc.ID, "kind": store.IncidentManual, "deleted": true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) addIncidentUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	inc, err := s.store.GetIncident(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in incidentUpdateInput
	if !readJSON(w, r, &in) {
		return
	}
	in, err = normalizeUpdate(in)
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	if inc.Kind != store.IncidentManual && in.State == store.StateResolved && inc.ResolvedAt == 0 {
		writeError(w, http.StatusBadRequest, "Otomatik olay monitör düzelince kapanır; süren olaya \"çözüldü\" aşaması yazılamaz")
		return
	}
	u := userFrom(r)
	upd := store.IncidentUpdate{IncidentID: id, Time: s.now().Unix(), State: in.State, Body: in.Body, UserID: u.ID, Username: u.Username}
	if err := s.store.AddIncidentUpdate(r.Context(), &upd); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "incident.update_post", "incident", id, incidentLabel(inc), in.State)
	s.hub.Publish("incident", map[string]any{"incident_id": id, "kind": inc.Kind})
	writeJSON(w, http.StatusCreated, upd)
}

func (s *Server) deleteIncidentUpdate(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	uid, err := strconv.ParseInt(r.PathValue("uid"), 10, 64)
	if err != nil || uid <= 0 {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	inc, err := s.store.GetIncident(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteIncidentUpdate(r.Context(), id, uid); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "incident.update_delete", "incident", id, incidentLabel(inc), "")
	s.hub.Publish("incident", map[string]any{"incident_id": id, "kind": inc.Kind})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// incidentLabel işlem kaydı için olayın adı: başlık, monitör ya da sunucu adı.
func incidentLabel(inc store.Incident) string {
	switch {
	case inc.Title != "":
		return inc.Title
	case inc.MonitorName != "":
		return inc.MonitorName
	}
	return inc.ServerName
}
