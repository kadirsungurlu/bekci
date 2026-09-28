package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/engine"
	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// Olay ayrıntıları (docs/PLAN.md §13) -------------------------------------------------
//
// Editör ve yönetici işlem geçmişinin tamamını görür (bildirim kanallarının
// adları ve gönderim hataları dahil). Olayı açan kontrolün ham istek/yanıtı
// (metot, adres, yanıt gövdesi ve başlıkları) YALNIZCA yöneticiye gösterilir;
// editör bunu görmez (iç servislere yöneltilen kontrollerin yanıtları sızmasın).
// İzleyiciye mesajlar temizlenerek gider; bildirim kayıtları, kullanıcı adları,
// istek/yanıt ve hedef adresin sorgu kısmı gösterilmez.
// Müşteri kısıtlı izleyici yalnızca izinli monitörlerin olaylarını görür
// (diğerleri 404). Herkese açık durum sayfaları bu uç noktayı kullanmaz.

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/incidents/{id}", s.auth(s.getIncident))
	})
}

type incidentMonitor struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
	Active bool   `json:"active"`
	Status int    `json:"status"`
}

type incidentLocationView struct {
	ProbeID int64  `json:"probe_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type incidentDetailView struct {
	Incident  store.Incident         `json:"incident"`
	Monitor   incidentMonitor        `json:"monitor"`
	Location  string                 `json:"location"`  // kök nedenin gözlendiği yer
	Locations []incidentLocationView `json:"locations"` // olay başında
	Events    []store.IncidentEvent  `json:"events"`    // yeniden eskiye
	Capture   *store.IncidentCapture `json:"capture"`   // yalnızca editör ve yönetici
	Details   bool                   `json:"details"`   // kullanıcı istek/yanıtı görebilir mi
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFrom(r)
	inc, err := s.store.GetIncident(r.Context(), id)
	if err == nil && !visibleTo(u).can(inc.MonitorID) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	m, err := s.store.GetMonitor(r.Context(), inc.MonitorID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	events, err := s.store.IncidentEvents(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	full := canSeeConfig(u)     // editör+: tüm işlem geçmişi, bildirim kayıtları, adresin tamamı
	capture := canSeeCapture(u) // yalnızca yönetici: ham istek/yanıt yakalaması (gövde + başlıklar)
	out := incidentDetailView{
		Incident: inc,
		Monitor: incidentMonitor{ID: m.ID, Name: m.Name, Type: m.Type, Target: engine.Target(m),
			Active: m.Active, Status: m.Status},
		Locations: []incidentLocationView{},
		Details:   capture,
	}
	events = completeEvents(inc, events)

	// Kök nedenin yeri ve olay başındaki konumlar: en eski başlangıç kaydından.
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != store.EventDown {
			continue
		}
		out.Location = events[i].Location
		var d struct {
			Locations []incidentLocationView `json:"locations"`
		}
		if json.Unmarshal(events[i].Data, &d) == nil && d.Locations != nil {
			out.Locations = d.Locations
		}
		break
	}

	if full {
		// Ham istek/yanıt yakalaması yalnızca yöneticiye verilir; editör olayın
		// kendisini ve tüm işlem geçmişini görür ama yakalamayı görmez.
		if capture {
			c, ok, err := s.store.GetIncidentCapture(r.Context(), id)
			if err != nil {
				s.dbError(w, err)
				return
			}
			if ok {
				out.Capture = &c
			}
		}
		out.Events = events
		writeJSON(w, http.StatusOK, out)
		return
	}

	// İzleyici: mesajlar temizlenir, bildirim kayıtları ve kullanıcı adları atılır.
	clean := func(msg string) string { return viewerMessage(u, m.Type, store.StatusDown, msg) }
	out.Monitor.Target = publicTarget(out.Monitor.Target)
	out.Incident.Cause = clean(out.Incident.Cause)
	for i := range out.Locations {
		out.Locations[i].Message = clean(out.Locations[i].Message)
	}
	out.Events = make([]store.IncidentEvent, 0, len(events))
	for _, ev := range events {
		switch ev.Kind {
		case store.EventNotify:
			continue
		case store.EventRetry, store.EventLocation:
			// data yalnızca sayaç/durum içerir
		case store.EventUp, store.EventReminder:
			// data yalnızca süre içerir
		default:
			ev.Data = nil
		}
		ev.Message = clean(ev.Message)
		out.Events = append(out.Events, ev)
	}
	writeJSON(w, http.StatusOK, out)
}

// completeEvents eski (işlem geçmişi tutulmadan önceki) olaylar için
// başlangıç ve çözülme kayıtlarını olay satırından üretir; sonuç yeniden eskiye sıralıdır.
func completeEvents(inc store.Incident, events []store.IncidentEvent) []store.IncidentEvent {
	hasStart, hasEnd := false, false
	for _, ev := range events {
		switch ev.Kind {
		case store.EventDown:
			hasStart = true
		case store.EventUp, store.EventPaused:
			hasEnd = true
		case store.EventEdited:
			var d struct {
				Closed bool `json:"closed"`
			}
			if json.Unmarshal(ev.Data, &d) == nil && d.Closed {
				hasEnd = true
			}
		}
	}
	if !hasStart {
		events = append(events, store.IncidentEvent{Time: inc.StartedAt, Kind: store.EventDown, Message: inc.Cause})
	}
	if inc.ResolvedAt > 0 && !hasEnd {
		events = append(events, store.IncidentEvent{Time: inc.ResolvedAt, Kind: store.EventUp,
			Data: store.EventData(map[string]int64{"downtime": inc.ResolvedAt - inc.StartedAt})})
	}
	if !hasStart || (inc.ResolvedAt > 0 && !hasEnd) {
		slices.SortStableFunc(events, func(a, b store.IncidentEvent) int {
			if a.Time != b.Time {
				return int(b.Time - a.Time)
			}
			return int(b.ID - a.ID)
		})
	}
	return events
}
