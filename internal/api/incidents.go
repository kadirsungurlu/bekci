package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
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
		mux.Handle("GET /api/servers/{id}/incidents", s.auth(s.serversOnly(s.serverIncidents)))
	})
}

// incidentServer sunucu olayının sunucusu.
type incidentServer struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Active   bool   `json:"active"`
}

// canSeeIncident: sunucu olayı atanmış sunucular, monitör olayı (normal ve
// kısmi) izinli monitörler için görünür.
func canSeeIncident(u store.User, inc store.Incident) bool {
	vis := visibleTo(u)
	if store.IsServerIncident(inc.Kind) {
		return inc.ServerID > 0 && vis.canServer(inc.ServerID)
	}
	return vis.can(inc.MonitorID)
}

// localizeIncidents liste satırlarının nedenini yanıt diline çevirir; sunucu
// olaylarında neden olay verisinden o dilde yeniden üretilir.
func localizeIncidents(lang string, list []store.Incident) {
	for k := range list {
		list[k].Cause = incidentCause(lang, list[k])
	}
}

func incidentCause(lang string, inc store.Incident) string {
	if store.IsServerIncident(inc.Kind) && len(inc.Data) > 0 {
		return store.ServerIncidentCause(lang, inc.Kind, store.ParseServerIncidentData(inc.Data))
	}
	return i18n.Message(lang, inc.Cause)
}

// serverIncidents sunucu ayrıntısındaki olay listesi (son 50).
func (s *Server) serverIncidents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	list, err := s.store.ListIncidents(r.Context(), store.IncidentFilter{ServerID: id, Limit: 50})
	if err != nil {
		s.dbError(w, err)
		return
	}
	localizeIncidents(responseLang(w), list)
	writeJSON(w, http.StatusOK, list)
}

// serverIncident sunucu olayının ayrıntısı: sunucu, metrik verisi (olay
// satırının data'sı) ve işlem geçmişi. İstek/yanıt yakalaması yoktur.
func (s *Server) serverIncident(w http.ResponseWriter, r *http.Request, u store.User, inc store.Incident) {
	p, err := s.store.GetProbe(r.Context(), inc.ServerID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	events, err := s.store.IncidentEvents(r.Context(), inc.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	events = completeEvents(inc, events)
	lang := responseLang(w)
	srv := &incidentServer{ID: p.ID, Name: p.Name, Active: p.Active}
	var h metrics.Host
	if p.HostInfo != "" && json.Unmarshal([]byte(p.HostInfo), &h) == nil {
		srv.Hostname = h.Hostname
	}
	out := incidentDetailView{Incident: inc, Server: srv, Locations: []incidentLocationView{},
		Events: make([]store.IncidentEvent, 0, len(events))}
	out.Incident.Cause = incidentCause(lang, inc)
	for _, ev := range events {
		if ev.Kind == store.EventNotify && !canSeeConfig(u) {
			continue
		}
		if ev.Kind == store.EventDown {
			ev.Message = out.Incident.Cause // neden o dilde yeniden üretilir
		} else {
			ev.Message = i18n.Message(lang, ev.Message)
		}
		out.Events = append(out.Events, ev)
	}
	writeJSON(w, http.StatusOK, out)
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
	Incident  store.Incident          `json:"incident"`
	Monitor   incidentMonitor         `json:"monitor"`            // sunucu olayında boş
	Server    *incidentServer         `json:"server,omitempty"`   // yalnızca sunucu olayında
	Location  string                  `json:"location"`           // kök nedenin gözlendiği yer
	Locations []incidentLocationView  `json:"locations"`          // olay başında
	Events    []store.IncidentEvent   `json:"events"`             // yeniden eskiye
	Capture   *store.IncidentCapture  `json:"capture"`            // yalnızca yönetici
	Captures  []store.IncidentCapture `json:"captures,omitempty"` // yalnızca yönetici: Capture + diğer çalışmayan konumlarınki
	Details   bool                    `json:"details"`            // kullanıcı istek/yanıtı görebilir mi
}

func (s *Server) getIncident(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFrom(r)
	inc, err := s.store.GetIncident(r.Context(), id)
	if err == nil && !canSeeIncident(u, inc) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	if store.IsServerIncident(inc.Kind) {
		s.serverIncident(w, r, u, inc)
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
				out.Capture, out.Captures = splitCapture(c)
			}
		}
		out.Events = events
		localizeIncident(u, responseLang(w), m.Type, &out)
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
		case store.EventUp, store.EventReminder, store.EventEscalated, store.EventFromPartial:
			// data yalnızca süre / bağlı olayın kimliği
			// data yalnızca süre içerir
		default:
			ev.Data = nil
		}
		ev.Message = clean(ev.Message)
		out.Events = append(out.Events, ev)
	}
	localizeIncident(u, responseLang(w), m.Type, &out)
	writeJSON(w, http.StatusOK, out)
}

// localizeIncident olay ayrıntısındaki Türkçe saklanan metinleri (neden,
// konum mesajları, işlem geçmişi, ana sunucunun konum adı, yakalanan
// isteğin hata metni) yanıt diline çevirir. İşlem geçmişi arayüzde tür +
// data ile kurulsa bile message yedek olarak çevrilir. Konum kaydının
// data.message'ı ham kontrol mesajıdır: izleyici için ayrıca temizlenir.
func localizeIncident(u store.User, lang, monitorType string, out *incidentDetailView) {
	out.Incident.Cause = i18n.Message(lang, out.Incident.Cause)
	out.Location = locationName(lang, out.Location)
	for i := range out.Locations {
		out.Locations[i].Name = locationName(lang, out.Locations[i].Name)
		out.Locations[i].Message = i18n.Message(lang, out.Locations[i].Message)
	}
	for i := range out.Events {
		ev := &out.Events[i]
		ev.Message = i18n.Message(lang, ev.Message)
		ev.Location = locationName(lang, ev.Location)
		switch ev.Kind {
		case store.EventLocation:
			ev.Data = mapEventData(ev.Data, func(d map[string]any) {
				if msg, ok := d["message"].(string); ok && msg != "" {
					st, _ := d["status"].(string)
					d["message"] = displayMessage(u, lang, monitorType, locationStatusCode(st), msg)
				}
			})
		case store.EventDown:
			if lang == i18n.TR {
				continue
			}
			ev.Data = mapEventData(ev.Data, func(d map[string]any) {
				locs, _ := d["locations"].([]any)
				for _, l := range locs {
					if lm, ok := l.(map[string]any); ok {
						if msg, ok := lm["message"].(string); ok {
							lm["message"] = i18n.Message(lang, msg)
						}
						if name, ok := lm["name"].(string); ok {
							lm["name"] = locationName(lang, name)
						}
					}
				}
			})
		}
	}
	if out.Capture != nil {
		c := localizeCapture(lang, *out.Capture)
		out.Capture = &c
	}
	for i := range out.Captures {
		out.Captures[i] = localizeCapture(lang, out.Captures[i])
	}
}

// localizeCapture yakalamanın Türkçe saklanan metinlerini (bağlantı hatası,
// ikili gövde açıklaması, tanının ham hata/mesajı, konum adı) çevirir. Ham
// sürücü hataları zaten İngilizcedir; çeviri kataloğunda yoksa değişmez.
func localizeCapture(lang string, c store.IncidentCapture) store.IncidentCapture {
	c.Location = locationName(lang, c.Location)
	if lang == i18n.TR {
		return c
	}
	var d map[string]any
	if json.Unmarshal(c.Detail, &d) != nil || d == nil {
		return c
	}
	if e, ok := d["error"].(string); ok {
		d["error"] = i18n.Message(lang, e)
	}
	if bin, _ := d["body_binary"].(bool); bin {
		if b, ok := d["body"].(string); ok {
			d["body"] = i18n.Message(lang, b) // "(ikili içerik, 2048 bayt)"
		}
	}
	if g, ok := d["diag"].(map[string]any); ok {
		if e, ok := g["raw_error"].(string); ok {
			g["raw_error"] = i18n.Message(lang, e)
		}
	}
	if b, err := json.Marshal(d); err == nil {
		c.Detail = b
	}
	return c
}

// splitCapture saklanan kaydı öncelikli yakalama ve konum listesi olarak
// döner: çok konumlu olayda diğer konumların ayrıntıları kaydın "others"
// alanındadır (bkz. engine.captureData); öncelikli ayrıntıdan çıkarılır.
func splitCapture(c store.IncidentCapture) (*store.IncidentCapture, []store.IncidentCapture) {
	var d map[string]json.RawMessage
	if json.Unmarshal(c.Detail, &d) == nil && d != nil {
		if raw, ok := d["others"]; ok {
			var others []store.IncidentCapture
			json.Unmarshal(raw, &others)
			delete(d, "others")
			if b, err := json.Marshal(d); err == nil {
				c.Detail = b
			}
			return &c, append([]store.IncidentCapture{c}, others...)
		}
	}
	return &c, []store.IncidentCapture{c}
}

// mapEventData işlem geçmişi kaydının data nesnesini değiştirir; çözülemezse
// olduğu gibi döner.
func mapEventData(raw json.RawMessage, f func(map[string]any)) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return raw
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil || d == nil {
		return raw
	}
	f(d)
	b, err := json.Marshal(d)
	if err != nil {
		return raw
	}
	return b
}

// locationName ana sunucunun konum adını ("Ana sunucu") yanıt diline çevirir;
// kullanıcının verdiği konum adları değişmez.
func locationName(lang, name string) string {
	if name == engine.LocalName {
		return i18n.Message(lang, name)
	}
	return name
}

// completeEvents eski (işlem geçmişi tutulmadan önceki) olaylar için
// başlangıç ve çözülme kayıtlarını olay satırından üretir; sonuç yeniden eskiye sıralıdır.
func completeEvents(inc store.Incident, events []store.IncidentEvent) []store.IncidentEvent {
	hasStart, hasEnd := false, false
	for _, ev := range events {
		switch ev.Kind {
		case store.EventDown:
			hasStart = true
		case store.EventUp, store.EventPaused, store.EventEscalated:
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
