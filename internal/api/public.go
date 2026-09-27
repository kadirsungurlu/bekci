package api

// Herkese açık durum sayfası uç noktaları (giriş gerektirmez).
//
// Gizlilik: monitör kimlikleri ve ayarları, olay nedenleri (iç IP ve hata
// ayrıntısı içerebilir), push token'ları ve sayfada olmayan monitörler asla
// gösterilmez. Hedef adresler yalnızca sayfada "hedefleri göster" açıksa ve
// kullanıcı adı/şifre/sorgu kısmı atılarak gösterilir.
//
// Yük: sayfa verisi 30 saniye bellekte tutulur; sayfa veya duyuru değişince
// önbellek hemen boşaltılır.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

const (
	pageCookie           = "uptime_sayfa"
	pageCookieMaxAge     = 30 * 24 * time.Hour
	publicDays           = 90
	publicIncidentWindow = 14 * 86400
	publicIncidentLimit  = 100
)

type publicBar struct {
	T    int64 `json:"t"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type publicMonitor struct {
	Name      string      `json:"name"`
	Status    string      `json:"status"` // up | down | pending | paused
	Uptime90d *float64    `json:"uptime_90d"`
	Bars      []publicBar `json:"bars"`
	Target    string      `json:"target,omitempty"`
}

type publicSection struct {
	Title    string          `json:"title"`
	Monitors []publicMonitor `json:"monitors"`
}

type publicAnnouncement struct {
	ID       int64  `json:"id"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Severity string `json:"severity"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
}

type publicIncident struct {
	Monitor    string `json:"monitor"`
	StartedAt  int64  `json:"started_at"`
	ResolvedAt int64  `json:"resolved_at"`
}

type publicPageView struct {
	Slug          string               `json:"slug"`
	Title         string               `json:"title"`
	Description   string               `json:"description"`
	Footer        string               `json:"footer"`
	HasLogo       bool                 `json:"has_logo"`
	LogoURL       *string              `json:"logo_url"`
	UpdatedAt     int64                `json:"updated_at"`
	Status        string               `json:"status"` // up | partial | down | unknown
	Sections      []publicSection      `json:"sections"`
	Announcements []publicAnnouncement `json:"announcements"`
	Incidents     []publicIncident     `json:"incidents"`
}

func logoURL(p store.StatusPage) *string {
	if !p.HasLogo {
		return nil
	}
	// Sürüm parametresi: logo değişince tarayıcı önbelleği (1 saat) atlanır.
	u := "/api/public/pages/" + p.Slug + "/logo?v=" + strconv.FormatInt(p.UpdatedAt, 10)
	return &u
}

func monitorStatus(m store.Monitor) string {
	if !m.Active {
		return "paused"
	}
	switch m.Status {
	case store.StatusUp:
		return "up"
	case store.StatusDown:
		return "down"
	}
	return "pending"
}

// overallStatus genel durum: duraklatılmışlar sayılmaz; bekleyen (tekrar
// deneniyor) monitör henüz kesinti sayılmaz.
func overallStatus(statuses []string) string {
	var up, down, pending int
	for _, st := range statuses {
		switch st {
		case "up":
			up++
		case "down":
			down++
		case "pending":
			pending++
		}
	}
	switch {
	case up == 0 && down == 0:
		return "unknown"
	case down == 0:
		return "up"
	case up == 0 && pending == 0:
		return "down"
	}
	return "partial"
}

// publicTarget hedef adresinden kullanıcı adı/şifre, sorgu ve parça kısmını atar.
func publicTarget(t string) string {
	if u, err := url.Parse(t); err == nil && u.Scheme != "" && u.Host != "" {
		u.User, u.RawQuery, u.Fragment, u.ForceQuery = nil, "", "", false
		return u.String()
	}
	return t
}

// buildPublicPage sayfanın herkese açık JSON'unu hesaplar.
func (s *Server) buildPublicPage(ctx context.Context, p store.StatusPage) ([]byte, error) {
	now := s.now().Unix()
	ids := p.MonitorIDs()
	mons, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	days := s.store.DayStarts(now, publicDays)
	daily, err := s.store.DailyFor(ctx, ids, days[0])
	if err != nil {
		return nil, err
	}
	view := publicPageView{
		Slug: p.Slug, Title: p.Title, Description: p.Description, Footer: p.Footer,
		HasLogo: p.HasLogo, LogoURL: logoURL(p), UpdatedAt: now,
		Sections: []publicSection{}, Announcements: []publicAnnouncement{}, Incidents: []publicIncident{},
	}
	names := map[int64]string{}
	var statuses []string
	for _, sec := range p.Sections {
		ps := publicSection{Title: sec.Title, Monitors: []publicMonitor{}}
		for _, pm := range sec.Monitors {
			m, ok := mons[pm.ID]
			if !ok {
				continue // sayfaya eklendikten sonra silinmiş
			}
			name := pm.Name
			if name == "" {
				name = m.Name
			}
			names[m.ID] = name
			pub := publicMonitor{Name: name, Status: monitorStatus(m), Bars: make([]publicBar, len(days))}
			byDay := make(map[int64]store.Bucket, len(daily[m.ID]))
			for _, b := range daily[m.ID] {
				byDay[b.Time] = b
			}
			var up, down int64
			for i, d := range days {
				b := byDay[d]
				pub.Bars[i] = publicBar{T: d, Up: b.Up, Down: b.Down}
				up += b.Up
				down += b.Down
			}
			if up+down > 0 {
				pct := math.Round(100000*float64(up)/float64(up+down)) / 1000
				pub.Uptime90d = &pct
			}
			if p.ShowTargets {
				pub.Target = publicTarget(engine.Target(m))
			}
			statuses = append(statuses, pub.Status)
			ps.Monitors = append(ps.Monitors, pub)
		}
		view.Sections = append(view.Sections, ps)
	}
	view.Status = overallStatus(statuses)

	anns, err := s.store.ActiveAnnouncements(ctx, p.ID, now)
	if err != nil {
		return nil, err
	}
	for _, a := range anns {
		view.Announcements = append(view.Announcements, publicAnnouncement{
			ID: a.ID, Title: a.Title, Body: a.Body, Severity: a.Severity, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
		})
	}
	// Olay nedeni bilerek yok: iç IP ve hata ayrıntısı içerebilir.
	incs, err := s.store.IncidentsFor(ctx, ids, now-publicIncidentWindow, publicIncidentLimit)
	if err != nil {
		return nil, err
	}
	for _, in := range incs {
		if name, ok := names[in.MonitorID]; ok {
			view.Incidents = append(view.Incidents, publicIncident{Monitor: name, StartedAt: in.StartedAt, ResolvedAt: in.ResolvedAt})
		}
	}
	return json.Marshal(view)
}

// publicEntry yayındaki sayfanın önbellekteki hazır halini döner; sayfa yoksa
// veya yayında değilse nil. Bulunamayan kısa adlar önbelleğe alınmaz (bellek
// şişirilemesin); onların maliyeti tek bir indeksli sorgudur.
func (s *Server) publicEntry(ctx context.Context, slug string) (*pageEntry, error) {
	if !slugRe.MatchString(slug) {
		return nil, nil
	}
	ps := s.pages
	get := func() (*pageEntry, uint64) {
		ps.mu.RLock()
		defer ps.mu.RUnlock()
		if e := ps.cache[slug]; e != nil && s.now().Before(e.expires) {
			return e, ps.gen
		}
		return nil, ps.gen
	}
	if e, _ := get(); e != nil {
		return e, nil
	}
	ps.buildMu.Lock()
	defer ps.buildMu.Unlock()
	e, gen := get() // beklerken başka istek hesaplamış olabilir
	if e != nil {
		return e, nil
	}
	p, err := s.store.PageBySlug(ctx, slug)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !p.Published) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e = &pageEntry{page: p, expires: s.now().Add(publicCacheTTL)}
	if e.payload, err = s.buildPublicPage(ctx, p); err != nil {
		return nil, err
	}
	if p.HasLogo {
		if e.logo, e.logoType, err = s.store.PageLogo(ctx, p.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
			return nil, err
		}
	}
	ps.mu.Lock()
	if ps.gen == gen { // hesaplama sırasında sayfa değiştiyse eski veri yazılmaz
		ps.cache[slug] = e
	}
	ps.mu.Unlock()
	return e, nil
}

// pageSecret şifreli sayfa çerezlerinin HMAC anahtarı (ilk kullanımda oluşur).
func (s *Server) pageSecret(ctx context.Context) ([]byte, error) {
	s.pages.mu.RLock()
	key := s.pages.secret
	s.pages.mu.RUnlock()
	if key != nil {
		return key, nil
	}
	v, err := s.store.Secret(ctx, "page_secret", func() string {
		b := make([]byte, 32)
		rand.Read(b)
		return hex.EncodeToString(b)
	})
	if err != nil {
		return nil, err
	}
	s.pages.mu.Lock()
	s.pages.secret = []byte(v)
	s.pages.mu.Unlock()
	return []byte(v), nil
}

// unlockToken sayfa kimliği ve güncel şifre özetine bağlı imza: şifre
// değişince (veya kaldırılıp yeniden konunca) eski çerezler geçersiz olur.
func (s *Server) unlockToken(ctx context.Context, p store.StatusPage) (string, error) {
	key, err := s.pageSecret(ctx)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(strconv.FormatInt(p.ID, 10) + "\x00" + p.PasswordHash))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func (s *Server) unlocked(r *http.Request, p store.StatusPage) bool {
	want, err := s.unlockToken(r.Context(), p)
	if err != nil {
		s.log.Error("durum sayfası anahtarı okunamadı", "hata", err)
		return false
	}
	for _, c := range r.CookiesNamed(pageCookie) {
		if hmac.Equal([]byte(c.Value), []byte(want)) {
			return true
		}
	}
	return false
}

// publicResolve istek bir durum sayfasının özel alan adına geldiyse sayfanın
// kısa adını döner; arayüz kökte hangi sayfayı göstereceğini buradan öğrenir.
func (s *Server) publicResolve(w http.ResponseWriter, r *http.Request) {
	if d, ok := s.domainPageFor(r); ok && d.published {
		writeJSON(w, http.StatusOK, map[string]any{"slug": d.slug})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slug": nil})
}

func (s *Server) publicPage(w http.ResponseWriter, r *http.Request) {
	e, err := s.publicEntry(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if e.page.HasPassword && !s.unlocked(r, e.page) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "Bu sayfa şifre korumalı", "password_required": true,
			"title": e.page.Title, "has_logo": e.page.HasLogo, "logo_url": logoURL(e.page),
		})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	if e.page.HasPassword {
		h.Set("Cache-Control", "private, no-store")
	} else {
		h.Set("Cache-Control", "public, max-age=30")
	}
	w.Write(e.payload)
}

// publicUnlock şifreli sayfayı açar: doğru şifrede 30 gün geçerli, HttpOnly
// bir çerez verilir. Denemeler IP+sayfa başına sınırlanır (giriş sınırıyla
// aynı kurallar, ayrı sayaçlar).
func (s *Server) publicUnlock(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	slug := r.PathValue("slug")
	e, err := s.publicEntry(r.Context(), slug)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if !e.page.HasPassword {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	ip := clientIP(r)
	ipKey, pageKey := ip+" "+slug, "sayfa:"+slug
	lim := s.pages.limiter
	if ok, wait := lim.allow(ipKey, pageKey, s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Çok fazla hatalı deneme. "+strconv.Itoa(int(wait.Minutes())+1)+" dakika sonra tekrar deneyin.")
		return
	}
	if len(in.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(e.page.PasswordHash), []byte(in.Password)) != nil {
		lim.fail(ipKey, pageKey, s.now())
		s.log.Warn("durum sayfası için hatalı şifre", "ip", ip, "sayfa", slug)
		writeError(w, http.StatusUnauthorized, "Şifre hatalı")
		return
	}
	lim.success(ipKey)
	token, err := s.unlockToken(r.Context(), e.page)
	if err != nil {
		s.dbError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		// Çerez sadece bu sayfanın API yoluna gider (logo dahil); başka sayfalara ve yönetim API'sine gitmez.
		Name: pageCookie, Value: token, Path: "/api/public/pages/" + e.page.Slug,
		Expires: s.now().Add(pageCookieMaxAge), MaxAge: int(pageCookieMaxAge.Seconds()),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// publicLogo yayındaki sayfanın logosu. Logo şifreli sayfalarda da açıktır
// (şifre ekranında gösterilir).
func (s *Server) publicLogo(w http.ResponseWriter, r *http.Request) {
	e, err := s.publicEntry(r.Context(), r.PathValue("slug"))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil || len(e.logo) == 0 {
		writeError(w, http.StatusNotFound, "Logo bulunamadı")
		return
	}
	writeLogo(w, e.logo, e.logoType, "public, max-age=3600")
}
