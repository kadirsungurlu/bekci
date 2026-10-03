package api

// OpenAPI belgesi: internal/apidocs/openapi.yaml gömülüdür ve GET /api/openapi.json ile
// JSON olarak sunulur (kimlik gerekmez; belge gizli bilgi içermez). YAML → JSON
// dönüşümü ilk istekte bir kez yapılır.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/kadirsungurlu/bekci/internal/apidocs"
	"gopkg.in/yaml.v3"
)

var openAPIJSON = sync.OnceValues(func() ([]byte, error) { return yamlToJSON(apidocs.OpenAPI) })

// yamlToJSON YAML belgesini JSON'a çevirir (anahtarlar dize olmalı).
func yamlToJSON(src []byte) ([]byte, error) {
	var doc any
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return nil, fmt.Errorf("openapi.yaml okunamadı: %w", err)
	}
	return json.Marshal(normalizeYAML(doc))
}

// normalizeYAML yaml.v3'ün map[string]any çıktısındaki iç içe map'leri JSON
// için güvenli hale getirir (anahtarı dize olmayan map varsa dizeye çevirir).
func normalizeYAML(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, val := range x {
			x[k] = normalizeYAML(val)
		}
		return x
	case map[any]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[fmt.Sprint(k)] = normalizeYAML(val)
		}
		return out
	case []any:
		for i := range x {
			x[i] = normalizeYAML(x[i])
		}
		return x
	}
	return v
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/openapi.json", s.openAPI)
	})
}

func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	b, err := openAPIJSON()
	if err != nil {
		s.log.Error("OpenAPI belgesi üretilemedi", "hata", err)
		writeError(w, http.StatusInternalServerError, "Sunucu hatası")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(b)
}
