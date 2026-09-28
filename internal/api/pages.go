package api

// Durum sayfaları: yönetim API'si (editör ve yönetici), önbellekler ve özel
// alan adı koruması. Herkese açık uç noktalar public.go'da.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
)

var slugRe = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$`)

const (
	maxPageSections = 20
	maxPageMonitors = 200
	maxLogoBytes    = 512 << 10
	publicCacheTTL  = 30 * time.Second
)

// Logo olarak kabul edilen türler. SVG bilerek yok: içinde betik taşıyabilir.
var logoTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}

var announcementSeverities = map[string]bool{"info": true, "warning": true, "danger": true, "success": true}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/status-pages", s.editor(s.listPages))
		mux.Handle("POST /api/status-pages", s.editor(s.createPage))
		mux.Handle("GET /api/status-pages/{id}", s.editor(s.getPage))
		mux.Handle("PUT /api/status-pages/{id}", s.editor(s.updatePage))
		mux.Handle("DELETE /api/status-pages/{id}", s.editor(s.deletePage))
		mux.Handle("GET /api/status-pages/{id}/preview", s.editor(s.previewPage))
		mux.Handle("GET /api/status-pages/{id}/logo", s.editor(s.getPageLogo))
		mux.Handle("PUT /api/status-pages/{id}/logo", s.editor(s.putPageLogo))
		mux.Handle("DELETE /api/status-pages/{id}/logo", s.editor(s.deletePageLogo))
		mux.Handle("GET /api/status-pages/{id}/announcements", s.editor(s.listAnnouncements))
		mux.Handle("POST /api/status-pages/{id}/announcements", s.editor(s.createAnnouncement))
		mux.Handle("PUT /api/announcements/{id}", s.editor(s.updateAnnouncement))
		mux.Handle("DELETE /api/announcements/{id}", s.editor(s.deleteAnnouncement))

		// Herkese açık (giriş gerektirmez). Şifre açma isteği de CSRF başlığı
		// (X-Uptime: 1) ister: arayüz zaten gönderir; başka bir sitenin formu
		// ziyaretçi adına şifre denemesi yapamaz.
		mux.HandleFunc("GET /api/public/resolve", s.publicResolve)
		mux.HandleFunc("GET /api/public/pages/{slug}", s.publicPage)
		mux.HandleFunc("POST /api/public/pages/{slug}/unlock", s.publicUnlock)
		mux.HandleFunc("GET /api/public/pages/{slug}/logo", s.publicLogo)
	})
}

// Önbellekler -------------------------------------------------------------------------

// pagesState durum sayfalarının bellekteki durumu: özel alan adı eşlemesi,
// herkese açık sayfa önbelleği ve şifre denemesi sınırı.
type pagesState struct {
	limiter *loginLimiter // şifre denemeleri; giriş sınırından ayrı

	mu      sync.RWMutex
	domains map[string]domainPage // nil: henüz yüklenmedi
	cache   map[string]*pageEntry // kısa ad → hazır sayfa
	gen     uint64                // her değişiklikte artar; eski hesaplama önbelleğe yazılmaz
	secret  []byte                // şifreli sayfa çerezi imza anahtarı

	buildMu sync.Mutex // aynı anda tek hesaplama: önbellek boşken gelen yığılma veritabanını yormasın
}

type domainPage struct {
	id        int64
	slug      string
	published bool
}

type pageEntry struct {
	page     store.StatusPage
	payload  []byte // herkese açık JSON
	logo     []byte
	logoType string
	expires  time.Time
}

func newPagesState() *pagesState {
	return &pagesState{limiter: newLoginLimiter(), cache: map[string]*pageEntry{}}
}

// reloadDomains alan adı eşlemesini veritabanından yeniden kurar.
func (s *Server) reloadDomains(ctx context.Context) error {
	list, err := s.store.ListPages(ctx)
	if err != nil {
		return err
	}
	m := make(map[string]domainPage)
	for _, p := range list {
		if p.CustomDomain != "" {
			m[p.CustomDomain] = domainPage{p.ID, p.Slug, p.Published}
		}
	}
	s.pages.mu.Lock()
	s.pages.domains = m
	s.pages.mu.Unlock()
	return nil
}

// pagesChanged sayfa veya duyuru değişince önbellekleri boşaltır.
func (s *Server) pagesChanged(ctx context.Context) {
	s.pages.mu.Lock()
	s.pages.gen++
	s.pages.cache = map[string]*pageEntry{}
	s.pages.mu.Unlock()
	if err := s.reloadDomains(ctx); err != nil {
		s.log.Error("durum sayfası alan adları yüklenemedi", "hata", err)
	}
}

// requestHost isteğin sunucu adı: port atılır, küçük harfe çevrilir.
func requestHost(r *http.Request) string {
	h := r.Host
	if host, _, err := net.SplitHostPort(h); err == nil {
		h = host
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

// domainPageFor istek bir durum sayfasının özel alan adına geldiyse o sayfayı döner.
func (s *Server) domainPageFor(r *http.Request) (domainPage, bool) {
	s.pages.mu.RLock()
	loaded := s.pages.domains != nil
	s.pages.mu.RUnlock()
	if !loaded {
		if err := s.reloadDomains(r.Context()); err != nil {
			s.log.Error("durum sayfası alan adları yüklenemedi", "hata", err)
			return domainPage{}, false
		}
	}
	s.pages.mu.RLock()
	defer s.pages.mu.RUnlock()
	d, ok := s.pages.domains[requestHost(r)]
	return d, ok
}

// customDomainOnly: istek bir durum sayfasının özel alan adına geldiyse yalnızca
// arayüz dosyaları, /healthz, /api/public/ ve /api/push/ sunulur; diğer tüm
// API uç noktaları (giriş, yönetim) 404 döner. Böylece özel alan adında
// oturum açılamaz, geçerli bir oturum çerezi olsa bile yönetim API'sine ulaşılamaz.
func (s *Server) customDomainOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := path.Clean("/" + r.URL.Path)
		if (strings.HasPrefix(p, "/api/") && !strings.HasPrefix(p, "/api/public/") && !strings.HasPrefix(p, "/api/push/")) || p == "/metrics" {
			if _, ok := s.domainPageFor(r); ok {
				writeError(w, http.StatusNotFound, "Bulunamadı")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Yönetim API'si --------------------------------------------------------------------

type pageInput struct {
	Slug         string              `json:"slug"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Footer       string              `json:"footer"`
	Sections     []store.PageSection `json:"sections"`
	CustomDomain string              `json:"custom_domain"`
	ShowTargets  *bool               `json:"show_targets"`
	Published    *bool               `json:"published"`
	// BarRange: recent (son kontroller, varsayılan) | 24h | 90d; yok = değişmez.
	BarRange *string `json:"bar_range"`
	// ShowIncidents: son 14 günün olayları herkese açık sayfada görünsün mü; yok = değişmez.
	ShowIncidents *bool `json:"show_incidents"`
	// Collapsible: gruplar açılıp kapanabilsin mi; yok = değişmez.
	Collapsible *bool `json:"collapsible"`
	// Lang: herkese açık sayfanın dili (tr | en); yok = değişmez (yeni sayfada tr).
	Lang *string `json:"lang"`
	// Password: yok/null = değişmez, "" = kaldır, dolu = yeni şifre.
	Password *string `json:"password"`
}

// inputError kullanıcıya gösterilecek doğrulama hatası (400 veya 409).
type inputError struct {
	status int
	msg    string
}

func (e *inputError) Error() string { return e.msg }

func badInput(msg string) error { return &inputError{http.StatusBadRequest, msg} }

// writeInputError doğrulama hatasını uygun durum koduyla, diğer hataları sunucu hatası olarak yazar.
func (s *Server) writeInputError(w http.ResponseWriter, err error) {
	var ie *inputError
	if errors.As(err, &ie) {
		writeError(w, ie.status, ie.msg)
		return
	}
	s.dbError(w, err)
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// validHostname küçük harfli, noktalı bir alan adı mı (IP adresi değil).
func validHostname(h string) bool {
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	tld := labels[len(labels)-1]
	return strings.Trim(tld, "0123456789") != "" // son parça tamamen rakamsa IP adresidir
}

// isPageAdmin kullanıcının özel alan adı alanını değiştirme yetkisi var mı
// (yalnızca yönetici).
func isPageAdmin(u store.User) bool {
	return store.RoleRank(u.Role) >= store.RoleRank(store.RoleAdmin)
}

// baseHost BASE_URL'in sunucu adı (boş olabilir).
func (s *Server) baseHost() string {
	u, err := url.Parse(s.BaseURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

// normalizePage girdiyi doğrular ve sayfaya uygular. old nil ise yeni sayfa.
// reqHost isteğin geldiği sunucu adı (özel alan adı kendini kilitlemesin diye).
// isAdmin özel alan adı alanını değiştirme yetkisi (yalnızca yönetici).
func (s *Server) normalizePage(ctx context.Context, in *pageInput, old *store.StatusPage, reqHost string, isAdmin bool) (store.StatusPage, error) {
	p := store.StatusPage{Published: true, BarRange: store.BarRangeRecent, ShowIncidents: true}
	if old != nil {
		p = *old
	}
	p.Slug = strings.ToLower(strings.TrimSpace(in.Slug))
	if !slugRe.MatchString(p.Slug) {
		return p, badInput("Adres 1-50 karakter olmalı; küçük harf, rakam ve tire kullanılabilir (başta ve sonda tire olamaz)")
	}
	p.Title = strings.TrimSpace(in.Title)
	if n := runes(p.Title); n < 1 || n > 100 {
		return p, badInput("Başlık 1-100 karakter olmalı")
	}
	p.Description = strings.TrimSpace(in.Description)
	if runes(p.Description) > 1000 {
		return p, badInput("Açıklama en fazla 1000 karakter olabilir")
	}
	p.Footer = strings.TrimSpace(in.Footer)
	if runes(p.Footer) > 500 {
		return p, badInput("Alt bilgi en fazla 500 karakter olabilir")
	}

	if len(in.Sections) > maxPageSections {
		return p, badInput(fmt.Sprintf("En fazla %d grup eklenebilir", maxPageSections))
	}
	sections := make([]store.PageSection, 0, len(in.Sections))
	seen := map[int64]bool{}
	var ids []int64
	for _, sec := range in.Sections {
		sec.Title = strings.TrimSpace(sec.Title)
		if runes(sec.Title) > 100 {
			return p, badInput("Grup adı en fazla 100 karakter olabilir")
		}
		mons := make([]store.PageMonitor, 0, len(sec.Monitors))
		for _, m := range sec.Monitors {
			m.Name = strings.TrimSpace(m.Name)
			if runes(m.Name) > 100 {
				return p, badInput("Görünen ad en fazla 100 karakter olabilir")
			}
			if seen[m.ID] {
				return p, badInput("Bir monitör sayfaya yalnızca bir kez eklenebilir")
			}
			seen[m.ID] = true
			ids = append(ids, m.ID)
			mons = append(mons, m)
		}
		sec.Monitors = mons
		sections = append(sections, sec)
	}
	if len(ids) > maxPageMonitors {
		return p, badInput(fmt.Sprintf("Bir sayfada en fazla %d monitör olabilir", maxPageMonitors))
	}
	existing, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		return p, err
	}
	for _, id := range ids {
		if _, ok := existing[id]; !ok {
			return p, badInput("Seçilen monitörlerden biri bulunamadı")
		}
	}
	p.Sections = sections

	p.CustomDomain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(in.CustomDomain)), ".")
	// Özel alan adını yalnızca yönetici belirleyebilir veya değiştirebilir;
	// editör alanı değiştirmeyen bir kaydı yine de saklayabilir. Böylece bir
	// editör sayfayı kendi alan adına (ör. uygulamanın adresine) yönlendirip
	// yönetim arayüzünü kilitleyemez.
	oldDomain := ""
	if old != nil {
		oldDomain = old.CustomDomain
	}
	if p.CustomDomain != oldDomain && !isAdmin {
		return p, &inputError{http.StatusForbidden, "Özel alan adını yalnızca yöneticiler değiştirebilir"}
	}
	if p.CustomDomain != "" {
		if !validHostname(p.CustomDomain) {
			return p, badInput("Geçerli bir alan adı girin (ör. durum.ornek.com); http:// ve / olmadan")
		}
		// Uygulamanın kendi adresi (BASE_URL) ya da isteğin geldiği adres
		// olamaz; aksi halde o alan adında yönetim API'si (giriş dahil)
		// kapanır ve kimse arayüze erişemez. BASE_URL boşken de isteğin
		// geldiği adres kontrol edilir.
		if base := s.baseHost(); (base != "" && p.CustomDomain == base) || p.CustomDomain == reqHost {
			return p, badInput("Özel alan adı uygulamanın kendi adresi olamaz")
		}
	}
	if in.ShowTargets != nil {
		p.ShowTargets = *in.ShowTargets
	}
	if in.BarRange != nil {
		if !store.ValidBarRange(*in.BarRange) {
			return p, badInput("Çubuk görünümü recent, 24h veya 90d olmalı")
		}
		p.BarRange = *in.BarRange
	}
	if in.ShowIncidents != nil {
		p.ShowIncidents = *in.ShowIncidents
	}
	if in.Collapsible != nil {
		p.Collapsible = *in.Collapsible
	}
	if in.Lang != nil {
		if !i18n.Valid(*in.Lang) {
			return p, badInput("Sayfa dili tr veya en olmalı")
		}
		p.Lang = *in.Lang
	}
	if in.Published != nil {
		p.Published = *in.Published
	}
	if in.Password != nil {
		if *in.Password == "" {
			p.PasswordHash = ""
		} else {
			if runes(*in.Password) < 4 || len(*in.Password) > 72 {
				return p, badInput("Sayfa şifresi 4-72 karakter olmalı")
			}
			hash, err := bcrypt.GenerateFromPassword([]byte(*in.Password), bcryptCost)
			if err != nil {
				return p, err
			}
			p.PasswordHash = string(hash)
		}
	}
	p.HasPassword = p.PasswordHash != ""

	// Benzersizlik: kullanıcıya anlaşılır mesaj (yazımdaki UNIQUE hatası yedek kontrol).
	if other, err := s.store.PageBySlug(ctx, p.Slug); err == nil && other.ID != p.ID {
		return p, errSlugTaken
	} else if err != nil && !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	if p.CustomDomain != "" {
		if other, err := s.store.PageByDomain(ctx, p.CustomDomain); err == nil && other.ID != p.ID {
			return p, errDomainTaken
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return p, err
		}
	}
	return p, nil
}

var (
	errSlugTaken   = &inputError{http.StatusConflict, "Bu adres başka bir durum sayfasında kullanılıyor"}
	errDomainTaken = &inputError{http.StatusConflict, "Bu alan adı başka bir durum sayfasında kullanılıyor"}
)

// conflictError yazım sırasındaki benzersizlik hatasını anlaşılır hataya çevirir.
func conflictError(err error) error {
	switch store.PageConflict(err) {
	case "slug":
		return errSlugTaken
	case "custom_domain":
		return errDomainTaken
	}
	return err
}

// pageViews yönetim ekranı için sayfalar: silinmiş monitörler gruplardan
// çıkarılır (aksi halde sayfa kaydedilirken "bulunamadı" hatası alınırdı).
func (s *Server) pageViews(ctx context.Context, pages []store.StatusPage) ([]store.StatusPage, error) {
	var ids []int64
	for _, p := range pages {
		ids = append(ids, p.MonitorIDs()...)
	}
	existing, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i, p := range pages {
		sections := make([]store.PageSection, len(p.Sections))
		for j, sec := range p.Sections {
			mons := make([]store.PageMonitor, 0, len(sec.Monitors))
			for _, m := range sec.Monitors {
				if _, ok := existing[m.ID]; ok {
					mons = append(mons, m)
				}
			}
			sections[j] = store.PageSection{Title: sec.Title, Monitors: mons}
		}
		pages[i].Sections = sections
	}
	return pages, nil
}

func (s *Server) listPages(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListPages(r.Context())
	if err == nil {
		list, err = s.pageViews(r.Context(), list)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) respondPage(w http.ResponseWriter, r *http.Request, id int64, status int) {
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	list, err := s.pageViews(r.Context(), []store.StatusPage{p})
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, status, list[0])
}

func (s *Server) getPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	s.respondPage(w, r, id, http.StatusOK)
}

// pageDetail işlem kaydı için kısa özet.
func pageDetail(p store.StatusPage) string {
	parts := []string{"/durum/" + p.Slug}
	if p.CustomDomain != "" {
		parts = append(parts, "alan adı: "+p.CustomDomain)
	}
	if !p.Published {
		parts = append(parts, "yayında değil")
	}
	if p.HasPassword {
		parts = append(parts, "şifreli")
	}
	return strings.Join(parts, ", ")
}

func (s *Server) createPage(w http.ResponseWriter, r *http.Request) {
	var in pageInput
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.normalizePage(r.Context(), &in, nil, requestHost(r), isPageAdmin(userFrom(r)))
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.CreatePage(r.Context(), &p); err != nil {
		s.writeInputError(w, conflictError(err))
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "status_page.create", "status_page", p.ID, p.Title, pageDetail(p))
	s.respondPage(w, r, p.ID, http.StatusCreated)
}

func (s *Server) updatePage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in pageInput
	if !readJSON(w, r, &in) {
		return
	}
	p, err := s.normalizePage(r.Context(), &in, &old, requestHost(r), isPageAdmin(userFrom(r)))
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.UpdatePage(r.Context(), &p); err != nil {
		s.writeInputError(w, conflictError(err))
		return
	}
	s.pagesChanged(r.Context())
	detail := pageDetail(p)
	switch {
	case old.HasPassword && !p.HasPassword:
		detail += ", şifre kaldırıldı"
	case p.PasswordHash != old.PasswordHash:
		detail += ", şifre değiştirildi"
	}
	s.audit(r, store.User{}, "status_page.update", "status_page", id, p.Title, detail)
	s.respondPage(w, r, id, http.StatusOK)
}

func (s *Server) deletePage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeletePage(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "status_page.delete", "status_page", id, old.Title, "/durum/"+old.Slug)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// previewPage yayında olmayan veya şifreli sayfanın herkese açık görünümü
// (önbelleğe alınmaz, şifre sorulmaz).
func (s *Server) previewPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	data, err := s.buildPublicPage(r.Context(), p)
	if err != nil {
		s.dbError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
}

// Logo ------------------------------------------------------------------------------

func writeLogo(w http.ResponseWriter, data []byte, typ, cacheControl string) {
	h := w.Header()
	h.Set("Content-Type", typ)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", cacheControl)
	h.Set("Content-Security-Policy", "default-src 'none'")
	w.Write(data)
}

func (s *Server) getPageLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	data, typ, err := s.store.PageLogo(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeLogo(w, data, typ, "private, no-cache")
}

// putPageLogo ham gövdeyle logo yükler. Başlıktaki tür, içeriğin gerçek
// türüyle (http.DetectContentType) aynı olmalı; SVG kabul edilmez.
func (s *Server) putPageLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	declared, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !logoTypes[declared] {
		writeError(w, http.StatusUnsupportedMediaType, "Logo PNG, JPEG veya WebP olmalı")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, maxLogoBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "Logo okunamadı")
		return
	}
	if len(data) > maxLogoBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "Logo en fazla 512 KB olabilir")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "Logo dosyası boş")
		return
	}
	if http.DetectContentType(data) != declared {
		writeError(w, http.StatusUnsupportedMediaType, "Dosya içeriği PNG, JPEG veya WebP değil")
		return
	}
	if err := s.store.SetPageLogo(r.Context(), id, data, declared); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "status_page.logo", "status_page", id, p.Title, fmt.Sprintf("%s, %d KB", declared, (len(data)+1023)/1024))
	s.respondPage(w, r, id, http.StatusOK)
}

func (s *Server) deletePageLogo(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.SetPageLogo(r.Context(), id, nil, ""); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "status_page.logo_delete", "status_page", id, p.Title, "")
	s.respondPage(w, r, id, http.StatusOK)
}

// Duyurular -------------------------------------------------------------------------

type announcementInput struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
}

func (s *Server) normalizeAnnouncement(in announcementInput, a *store.Announcement) error {
	a.Title = strings.TrimSpace(in.Title)
	if n := runes(a.Title); n < 1 || n > 200 {
		return badInput("Duyuru başlığı 1-200 karakter olmalı")
	}
	a.Body = strings.TrimSpace(in.Body)
	if runes(a.Body) > 5000 {
		return badInput("Duyuru metni en fazla 5000 karakter olabilir")
	}
	a.Severity = in.Severity
	if a.Severity == "" {
		a.Severity = "info"
	}
	if !announcementSeverities[a.Severity] {
		return badInput("Önem derecesi info, warning, danger veya success olmalı")
	}
	if in.StartsAt < 0 || in.EndsAt < 0 {
		return badInput("Geçersiz zaman")
	}
	a.StartsAt = in.StartsAt
	if a.StartsAt == 0 {
		a.StartsAt = s.now().Unix()
	}
	a.EndsAt = in.EndsAt
	if a.EndsAt != 0 && a.EndsAt <= a.StartsAt {
		return badInput("Bitiş zamanı başlangıçtan sonra olmalı")
	}
	return nil
}

func (s *Server) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetPage(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	list, err := s.store.ListAnnouncements(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createAnnouncement(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in announcementInput
	if !readJSON(w, r, &in) {
		return
	}
	a := store.Announcement{PageID: id}
	if err := s.normalizeAnnouncement(in, &a); err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.CreateAnnouncement(r.Context(), &a); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "announcement.create", "announcement", a.ID, a.Title, "sayfa: "+p.Title+", "+a.Severity)
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) updateAnnouncement(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.store.GetAnnouncement(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in announcementInput
	if !readJSON(w, r, &in) {
		return
	}
	if err := s.normalizeAnnouncement(in, &a); err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.UpdateAnnouncement(r.Context(), &a); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "announcement.update", "announcement", a.ID, a.Title, a.Severity)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) deleteAnnouncement(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	a, err := s.store.GetAnnouncement(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteAnnouncement(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.pagesChanged(r.Context())
	s.audit(r, store.User{}, "announcement.delete", "announcement", id, a.Title, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
