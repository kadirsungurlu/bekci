package api

import (
	"errors"
	"net/http"

	"github.com/kadirsa1105/uptime-kadir-app/internal/maintenance"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Bakım pencereleri: editör ve yönetici yönetir; izleyiciler yalnızca
// görebildikleri monitörleri etkileyen pencereleri okuyabilir.
func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/maintenance", s.auth(s.listMaintenance))
		mux.Handle("POST /api/maintenance", s.editor(s.createMaintenance))
		mux.Handle("GET /api/maintenance/{id}", s.auth(s.getMaintenance))
		mux.Handle("PUT /api/maintenance/{id}", s.editor(s.updateMaintenance))
		mux.Handle("DELETE /api/maintenance/{id}", s.editor(s.deleteMaintenance))
		mux.Handle("POST /api/maintenance/{id}/pause", s.editor(s.pauseMaintenance))
		mux.Handle("POST /api/maintenance/{id}/resume", s.editor(s.resumeMaintenance))
	})
}

// maintenanceInput ekleme/düzenleme gövdesi. Stratejiyle ilgisiz alanlar
// gönderilebilir, kaydedilirken temizlenir.
type maintenanceInput struct {
	Title           string  `json:"title"`
	Description     string  `json:"description"`
	Active          *bool   `json:"active"` // gönderilmezse true
	Strategy        string  `json:"strategy"`
	Timezone        string  `json:"timezone"`
	Start           string  `json:"start"`
	End             string  `json:"end"`
	Weekdays        []int   `json:"weekdays"`
	StartTime       string  `json:"start_time"`
	EndTime         string  `json:"end_time"`
	DateFrom        string  `json:"date_from"`
	DateTo          string  `json:"date_to"`
	Cron            string  `json:"cron"`
	DurationMinutes int     `json:"duration_minutes"`
	AllMonitors     bool    `json:"all_monitors"`
	MonitorIDs      []int64 `json:"monitor_ids"`
}

type maintenanceView struct {
	store.Maintenance
	Status    string `json:"status"`     // active | scheduled | ended | inactive
	NextStart int64  `json:"next_start"` // sürüyorsa şu anki, değilse sonraki tekrarın başlangıcı (unix); yoksa 0
	NextEnd   int64  `json:"next_end"`
}

func (s *Server) maintenanceView(m store.Maintenance) maintenanceView {
	st, start, end := maintenance.Status(m, s.now())
	return maintenanceView{Maintenance: m, Status: st, NextStart: start, NextEnd: end}
}

// visibleMaintenance kısıtlı izleyici için pencereyi süzer: tüm monitörleri
// etkileyen veya görebildiği bir monitörü içeren pencereler görünür, monitör
// listesi de görebildikleriyle sınırlanır.
func visibleMaintenance(vis visibility, m store.Maintenance) (store.Maintenance, bool) {
	if vis.all {
		return m, true
	}
	ids := []int64{}
	for _, id := range m.MonitorIDs {
		if vis.can(id) {
			ids = append(ids, id)
		}
	}
	m.MonitorIDs = ids
	return m, m.AllMonitors || len(ids) > 0
}

func (s *Server) listMaintenance(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListMaintenance(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	vis := visibleTo(userFrom(r))
	out := make([]maintenanceView, 0, len(list))
	for _, m := range list {
		if m, ok := visibleMaintenance(vis, m); ok {
			out = append(out, s.maintenanceView(m))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMaintenance(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	m, ok = visibleMaintenance(visibleTo(userFrom(r)), m)
	if !ok {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	writeJSON(w, http.StatusOK, s.maintenanceView(m))
}

// toMaintenance girdiyi doğrular; monitörlerin var olduğunu da kontrol eder.
// Hata varsa yanıtı yazar ve false döner.
func (s *Server) toMaintenance(w http.ResponseWriter, r *http.Request, in maintenanceInput) (store.Maintenance, bool) {
	m := store.Maintenance{
		Title: in.Title, Description: in.Description, Active: in.Active == nil || *in.Active,
		Strategy: in.Strategy, Timezone: in.Timezone, Start: in.Start, End: in.End,
		Weekdays: in.Weekdays, StartTime: in.StartTime, EndTime: in.EndTime,
		DateFrom: in.DateFrom, DateTo: in.DateTo, Cron: in.Cron, DurationMinutes: in.DurationMinutes,
		AllMonitors: in.AllMonitors, MonitorIDs: in.MonitorIDs,
	}
	if err := maintenance.Normalize(&m, s.now()); err != nil {
		var ve maintenance.ValidationError
		if errors.As(err, &ve) {
			writeError(w, http.StatusBadRequest, err.Error())
		} else {
			s.dbError(w, err)
		}
		return m, false
	}
	existing, err := s.store.MonitorsByIDs(r.Context(), m.MonitorIDs)
	if err != nil {
		s.dbError(w, err)
		return m, false
	}
	for _, id := range m.MonitorIDs {
		if _, ok := existing[id]; !ok {
			writeError(w, http.StatusBadRequest, "Seçilen monitörlerden biri bulunamadı")
			return m, false
		}
	}
	return m, true
}

// maintenanceChanged motorun bellekteki bakım dizinini yeniler ve canlı akışa
// haber verir (arayüz listeyi ve "bakımda" rozetlerini yeniden yükler).
// Olay monitöre bağlı olmadığından kısıtlı izleyicilere gitmez.
func (s *Server) maintenanceChanged(r *http.Request, id int64) {
	if err := s.engine.ReloadMaintenance(r.Context()); err != nil {
		s.log.Error("bakım pencereleri yenilenemedi", "hata", err)
	}
	s.hub.Publish("maintenance", map[string]any{"maintenance_id": id})
}

func (s *Server) respondMaintenance(w http.ResponseWriter, r *http.Request, id int64, status int) {
	m, err := s.store.GetMaintenance(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, status, s.maintenanceView(m))
}

func (s *Server) createMaintenance(w http.ResponseWriter, r *http.Request) {
	var in maintenanceInput
	if !readJSON(w, r, &in) {
		return
	}
	m, ok := s.toMaintenance(w, r, in)
	if !ok {
		return
	}
	if err := s.store.CreateMaintenance(r.Context(), &m); err != nil {
		s.dbError(w, err)
		return
	}
	s.maintenanceChanged(r, m.ID)
	s.audit(r, store.User{}, "maintenance.create", "maintenance", m.ID, m.Title, m.Strategy)
	s.respondMaintenance(w, r, m.ID, http.StatusCreated)
}

func (s *Server) updateMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetMaintenance(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	var in maintenanceInput
	if !readJSON(w, r, &in) {
		return
	}
	m, ok := s.toMaintenance(w, r, in)
	if !ok {
		return
	}
	m.ID = id
	if err := s.store.UpdateMaintenance(r.Context(), &m); err != nil {
		s.dbError(w, err)
		return
	}
	s.maintenanceChanged(r, id)
	s.audit(r, store.User{}, "maintenance.update", "maintenance", id, m.Title, m.Strategy)
	s.respondMaintenance(w, r, id, http.StatusOK)
}

func (s *Server) deleteMaintenance(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetMaintenance(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteMaintenance(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.maintenanceChanged(r, id)
	s.audit(r, store.User{}, "maintenance.delete", "maintenance", id, old.Title, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) setMaintenanceActive(w http.ResponseWriter, r *http.Request, active bool) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetMaintenance(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.SetMaintenanceActive(r.Context(), id, active); err != nil {
		s.dbError(w, err)
		return
	}
	s.maintenanceChanged(r, id)
	action := "maintenance.pause"
	if active {
		action = "maintenance.resume"
	}
	s.audit(r, store.User{}, action, "maintenance", id, old.Title, "")
	s.respondMaintenance(w, r, id, http.StatusOK)
}

func (s *Server) pauseMaintenance(w http.ResponseWriter, r *http.Request) {
	s.setMaintenanceActive(w, r, false)
}

func (s *Server) resumeMaintenance(w http.ResponseWriter, r *http.Request) {
	s.setMaintenanceActive(w, r, true)
}
