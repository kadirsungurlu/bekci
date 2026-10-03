package api

// Olay onaylama (ack) ve susturma (snooze) — E-11.
//
//	POST   /api/incidents/{id}/ack      {"note": "…"}   olayı onayla (editör+)
//	DELETE /api/incidents/{id}/ack                       onayı geri al
//	POST   /api/incidents/{id}/snooze   {"minutes": 60}  N dakika sustur (1 dk – 7 gün)
//	DELETE /api/incidents/{id}/snooze                    susturmayı kaldır
//
// Onaylı olayda hatırlatma bildirimi ve eskalasyon gitmez; kapanış bildirimi
// yine gider. Susturma süreli onaydır: süre dolunca hatırlatma ve eskalasyon
// kendiliğinden devam eder. Kuyrukta bekleyen ilk sorun bildirimi (gecikme /
// sessiz saat) onaydan etkilenmez. Onay, "acked" türünü açıkça seçen
// kanallara bildirilir (webhook'ta ack alanı).

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	maxAckNote       = 500
	maxSnoozeMinutes = 7 * 24 * 60
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/incidents/{id}/ack", s.editor(s.ackIncident))
		mux.Handle("DELETE /api/incidents/{id}/ack", s.editor(s.unackIncident))
		mux.Handle("POST /api/incidents/{id}/snooze", s.editor(s.snoozeIncident))
		mux.Handle("DELETE /api/incidents/{id}/snooze", s.editor(s.unsnoozeIncident))
	})
}

// ackTarget olayı okur ve görünürlüğünü denetler; kapanmış olayda 409 yazar.
func (s *Server) ackTarget(w http.ResponseWriter, r *http.Request) (store.Incident, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return store.Incident{}, false
	}
	inc, err := s.store.GetIncident(r.Context(), id)
	if err == nil && !canSeeIncident(userFrom(r), inc) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return inc, false
	}
	if inc.ResolvedAt != 0 {
		writeError(w, http.StatusConflict, "Kapanmış olay onaylanamaz veya susturulamaz")
		return inc, false
	}
	return inc, true
}

func (s *Server) ackError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrIncidentClosed) {
		writeError(w, http.StatusConflict, "Kapanmış olay onaylanamaz veya susturulamaz")
		return
	}
	s.dbError(w, err)
}

// respondIncident güncel olay satırını döner ve canlı akışa "incident" yazar.
func (s *Server) respondIncident(w http.ResponseWriter, r *http.Request, id int64) {
	inc, err := s.store.GetIncident(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	inc.Cause = incidentCause(responseLang(w), inc)
	s.hub.Publish("incident", map[string]any{"incident_id": inc.ID, "kind": inc.Kind})
	writeJSON(w, http.StatusOK, inc)
}

func (s *Server) ackIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.ackTarget(w, r)
	if !ok {
		return
	}
	var in struct {
		Note string `json:"note"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > maxAckNote {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Not en fazla %d karakter olabilir", maxAckNote))
		return
	}
	u := userFrom(r)
	now := s.now()
	if err := s.store.AckIncident(r.Context(), inc.ID, u.Username, in.Note, now.Unix()); err != nil {
		s.ackError(w, err)
		return
	}
	s.audit(r, store.User{}, "incident.ack", "incident", inc.ID, incidentLabel(inc), in.Note)
	s.notifyAcked(r, inc, u.Username, in.Note, now)
	s.respondIncident(w, r, inc.ID)
}

func (s *Server) unackIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.ackTarget(w, r)
	if !ok {
		return
	}
	if err := s.store.UnackIncident(r.Context(), inc.ID, userFrom(r).Username, s.now().Unix()); err != nil {
		s.ackError(w, err)
		return
	}
	s.audit(r, store.User{}, "incident.unack", "incident", inc.ID, incidentLabel(inc), "")
	s.respondIncident(w, r, inc.ID)
}

func (s *Server) snoozeIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.ackTarget(w, r)
	if !ok {
		return
	}
	var in struct {
		Minutes int `json:"minutes"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Minutes < 1 || in.Minutes > maxSnoozeMinutes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Susturma süresi 1-%d dakika olmalı", maxSnoozeMinutes))
		return
	}
	now := s.now()
	until := now.Add(time.Duration(in.Minutes) * time.Minute).Unix()
	if err := s.store.SnoozeIncident(r.Context(), inc.ID, until, userFrom(r).Username, now.Unix()); err != nil {
		s.ackError(w, err)
		return
	}
	s.audit(r, store.User{}, "incident.snooze", "incident", inc.ID, incidentLabel(inc), fmt.Sprintf("%d dk", in.Minutes))
	s.respondIncident(w, r, inc.ID)
}

func (s *Server) unsnoozeIncident(w http.ResponseWriter, r *http.Request) {
	inc, ok := s.ackTarget(w, r)
	if !ok {
		return
	}
	if err := s.store.UnsnoozeIncident(r.Context(), inc.ID, userFrom(r).Username, s.now().Unix()); err != nil {
		s.ackError(w, err)
		return
	}
	s.audit(r, store.User{}, "incident.unsnooze", "incident", inc.ID, incidentLabel(inc), "")
	s.respondIncident(w, r, inc.ID)
}

// notifyAcked onayı olayın kanallarına "acked" olayı olarak bildirir (yalnızca
// bu türü açıkça seçen kanallar alır). Manuel olayın kanalı yoktur.
func (s *Server) notifyAcked(r *http.Request, inc store.Incident, by, note string, now time.Time) {
	if s.notifier == nil || inc.Kind == store.IncidentManual {
		return
	}
	ev := notify.Event{Kind: notify.KindAcked, Time: now, IncidentID: inc.ID, IncidentURL: s.engine.IncidentURL(inc.ID),
		AckedBy: by, AckNote: note, Downtime: now.Sub(time.Unix(inc.StartedAt, 0)), Message: inc.Cause}
	if store.IsServerIncident(inc.Kind) {
		p, err := s.store.GetProbe(r.Context(), inc.ServerID)
		if err != nil {
			return
		}
		ev.ProbeID, ev.MonitorName = p.ID, p.Name
		ev.MonitorType = "server"
		if inc.Kind == store.IncidentProbeOffline {
			ev.MonitorType = "probe"
		}
		ev.URL = s.servers.URL(p.ID)
	} else {
		m, err := s.store.GetMonitor(r.Context(), inc.MonitorID)
		if err != nil {
			return
		}
		ev.MonitorID, ev.MonitorName, ev.MonitorType, ev.URL = m.ID, m.Name, m.Type, s.engine.MonitorURL(m.ID)
		if c, ok := check.Get(m.Type); ok {
			ev.Target = c.Target(m.Config)
		} else {
			ev.Target = engine.Target(m)
		}
	}
	s.notifier.Notify(ev)
}
