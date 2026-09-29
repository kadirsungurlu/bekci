package api

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Tek sayfalık uygulamanın durum kodu ------------------------------------------------
//
// Arayüz bilinmeyen yollarda da index.html ile açılır (istemci tarafı
// yönlendirme). Herkese açık durum sayfası yollarında bu, olmayan veya
// yayında olmayan (taslak) sayfanın da "200 OK" dönmesi demekti: arama
// motorları ve izleme araçları boş sayfayı var sanıyordu. Artık böyle bir
// istekte aynı HTML (arayüz "bulunamadı" görünümünü gösterir) 404 durum
// koduyla gönderilir.

// spaStatus index.html'in hangi durum koduyla sunulacağını söyler (p temizlenmiş yol):
//   - /durum/<kısa-ad>: sayfa yoksa veya yayında değilse 404;
//   - yayında olmayan bir sayfanın özel alan adına gelen istek: 404 (yönetim
//     arayüzü özel alan adında açılmaz, gösterilecek sayfa da yok);
//   - aksi halde 200. Veritabanı hatasında 200 (sayfa yine de denensin).
func (s *Server) spaStatus(r *http.Request, p string) int {
	if s.store == nil || s.pages == nil {
		return http.StatusOK // yalnızca arayüz dosyalarıyla kurulmuş sunucu (testler)
	}
	if rest, ok := strings.CutPrefix(p, "/durum/"); ok && !strings.Contains(rest, "/") {
		slug := strings.ToLower(rest)
		if !slugRe.MatchString(slug) {
			return http.StatusNotFound
		}
		pg, err := s.store.PageBySlug(r.Context(), slug)
		switch {
		case errors.Is(err, store.ErrNotFound):
			return http.StatusNotFound
		case err != nil:
			s.log.Error("durum sayfası okunamadı", "hata", err)
			return http.StatusOK
		case !pg.Published:
			return http.StatusNotFound
		}
		return http.StatusOK
	}
	if d, ok := s.domainPageFor(r); ok && !d.published {
		return http.StatusNotFound
	}
	return http.StatusOK
}

// serveIndexStatus index.html'i verilen (200 dışı) durum koduyla yazar.
// http.ServeContent her zaman 200/304 döndüğü için elle yazılır.
func serveIndexStatus(w http.ResponseWriter, r *http.Request, f fs.File, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		io.Copy(w, f)
	}
}
