package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Sunucu envanteri: ajanın topladığı servis, konteyner, port, sertifika ve
// disk bölümü listesi (internal/metrics/inventory.go). Eski ajan bu uca hiç
// gelmez, GET boş döner (collected_at 0) ve arayüz "ajan güncellenmeli" der.
func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/servers/{id}/inventory", s.auth(s.serversOnly(s.getInventory)))
		mux.Handle("PUT /api/servers/{id}/inventory/certs/{fingerprint}", s.editor(s.putInventoryCert))
		mux.Handle("POST /api/probe/inventory", s.probeOnly(s.probeInventory))
	})
}

func (s *Server) probeInventory(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	var in metrics.Inventory
	body := http.MaxBytesReader(w, r.Body, metrics.MaxInventoryBytes)
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
	if err := s.servers.IngestInventory(r.Context(), p, in); err != nil {
		s.dbError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getInventory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	inv, err := s.store.LoadInventory(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

// putInventoryCert sertifikanın uyarısını kapatır/açar. Açık uyarı bir sonraki
// değerlendirmede kapanır (ajan turu ya da saatlik denetim beklenmez).
func (s *Server) putInventoryCert(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in struct {
		Ignored bool `json:"ignored"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi")
		return
	}
	fp := r.PathValue("fingerprint")
	if err := s.store.SetCertIgnored(r.Context(), id, fp, in.Ignored); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusNotFound, "Sertifika bulunamadı")
			return
		}
		s.dbError(w, err)
		return
	}
	detail := "sertifika uyarısı açıldı"
	if in.Ignored {
		detail = "sertifika uyarısı kapatıldı"
	}
	s.audit(r, store.User{}, "server.cert_ignore", "server", id, p.Name, detail)
	s.servers.CheckCert(r.Context(), p)
	w.WriteHeader(http.StatusNoContent)
}
