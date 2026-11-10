package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Sunucu güvenlik durumu (iş 731): bekleyen güncellemeler ve yeniden başlatma
// gereksinimi (internal/metrics/security.go). Eski ajan bu uca hiç gelmez, GET
// boş döner (received_at 0) ve arayüz "ajan güncellenmeli" der.
func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/servers/{id}/security", s.auth(s.serversOnly(s.getSecurity)))
		mux.Handle("POST /api/probe/security", s.probeOnly(s.probeSecurity))
	})
}

func (s *Server) probeSecurity(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	var in metrics.Security
	body := http.MaxBytesReader(w, r.Body, metrics.MaxSecurityBytes)
	if err := json.NewDecoder(body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
			return
		}
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi: "+err.Error())
		return
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
		return
	}
	if err := s.servers.IngestSecurity(r.Context(), p, in); err != nil {
		s.dbError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// securityView GET yanıtı: saklanan durum + kuralın eşiği (arayüz "X gündür
// bekliyor" uyarısını aynı eşikle boyar) + raporlama aralığı.
type securityView struct {
	store.ServerSecurity
	Interval     int     `json:"interval"`
	AgeThreshold float64 `json:"age_threshold,omitempty"` // sec_updates_age eşiği (gün; kural yoksa/kapalıysa 0)
	Now          int64   `json:"now"`
}

func (s *Server) getSecurity(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	sec, err := s.store.LoadSecurity(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := securityView{ServerSecurity: sec, Interval: servers.SecurityIntervalFor(p), Now: s.now().Unix()}
	rules, err := s.store.ServerAlerts(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	for _, a := range rules {
		if a.Metric == servers.MetricSecUpdatesAge && a.Active {
			out.AgeThreshold = a.Threshold
		}
	}
	writeJSON(w, http.StatusOK, out)
}
