package api

import (
	"context"
	"net/http"

	"github.com/kadirsungurlu/bekci/kanca"
)

// Pro modülünün kanca.Rota ile kaydettiği rotalar (bkz. docs/LISANS-TASARIM.md "Kancalar").
func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		for _, r := range kanca.Rotalar() {
			h := r.Isleyici
			if r.Ozellik != "" {
				h = s.proKapi(r.Ozellik, h)
			}
			switch r.Erisim {
			case kanca.Genel:
				mux.HandleFunc(r.Desen, h)
			case kanca.Oturum:
				mux.Handle(r.Desen, s.auth(h))
			default: // kanca.Yonetici; bilinmeyen değer en sıkı düzeye düşer
				mux.Handle(r.Desen, s.admin(h))
			}
		}
	})
}

// SetProOzellik lisans değerlendirmesini bağlar (nil: tüm Pro özellikleri kapalı).
// Sunucu istek almadan önce bir kez çağrılır.
func (s *Server) SetProOzellik(f func(string) bool) { s.proOzellik = f }

// ProAcik lisansta özellik açık mı?
func (s *Server) ProAcik(ozellik string) bool { return s.proAcik(ozellik) }

func (s *Server) proAcik(ozellik string) bool {
	return s.proOzellik != nil && s.proOzellik(ozellik)
}

// proKapi lisansta özellik yoksa yalnızca okuma (GET/HEAD) geçer; değiştiren
// istekler 402 code=pro_lisans alır.
func (s *Server) proKapi(ozellik string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.proAcik(ozellik) || r.Method == http.MethodGet || r.Method == http.MethodHead {
			h(w, r)
			return
		}
		writeJSON(w, http.StatusPaymentRequired, map[string]string{
			"error": "Bu işlem için Pro lisansı gerekli", "code": "pro_lisans",
		})
	}
}

// KancaPanel Pro başlangıç kancalarına verilen panel bağlamı.
func (s *Server) KancaPanel() *kanca.Panel {
	dialect := "sqlite"
	if s.store.Postgres() {
		dialect = "postgres"
	}
	return &kanca.Panel{
		DB:      s.store.SQLDB(),
		Dialect: dialect,
		Log:     s.log,
		Ozellik: s.proAcik,
		Bildir:  s.kancaBildir,
	}
}

// kancaBildir Pro kodunun sistem bildirimi. Şimdilik yalnız günlüğe yazar;
// yönetici kanallarına gönderim lisans bağlantısıyla birlikte eklenecek.
func (s *Server) kancaBildir(_ context.Context, konu, metin string) error {
	s.log.Warn("pro bildirimi", "konu", konu, "metin", metin)
	return nil
}
