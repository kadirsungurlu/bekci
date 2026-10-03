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
	"slices"
	"strconv"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	pageCookie           = "uptime_sayfa"
	pageCookieMaxAge     = 30 * 24 * time.Hour
	publicDays           = 90
	publicHours          = 24
	publicRecentBeats    = 90 // "son kontroller" görünümünde en fazla çubuk (tek satır düzeni 90'a kadar gösterir)
	publicIncidentWindow = 14 * 86400
	publicIncidentLimit  = 100
)

// publicBar bir çubuk: "recent" görünümünde tek kontrol (up/down 0 veya 1;
// ikisi de 0 ise bekliyor/bakımda), diğerlerinde saat veya gün özeti.
type publicBar struct {
	T    int64 `json:"t"`
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type publicMonitor struct {
	Name   string `json:"name"`
	Status string `json:"status"` // up | down | pending | paused | maintenance
	// Uptime sayfanın UptimeWindow'u için: recent/24h görünümünde son 24 saat, 90d'de 90 gün.
	Uptime    *float64    `json:"uptime"`
	Uptime90d *float64    `json:"uptime_90d"` // eski istemciler için; yalnız 90d görünümünde dolu
	Bars      []publicBar `json:"bars"`
	Target    string      `json:"target,omitempty"`
	// Uptimes sayfada birden fazla uptime penceresi seçiliyse (uptime_windows)
	// pencere → yüzde (veri yoksa null). Eski istemciler yok sayar.
	Uptimes map[string]*float64 `json:"uptimes,omitempty"`
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

type publicPageView struct {
	Slug          string               `json:"slug"`
	Title         string               `json:"title"`
	Description   string               `json:"description"`
	Footer        string               `json:"footer"`
	HasLogo       bool                 `json:"has_logo"`
	LogoURL       *string              `json:"logo_url"`
	UpdatedAt     int64                `json:"updated_at"`
	Range         string               `json:"range"`         // recent | 24h | 90d
	UptimeWindow  string               `json:"uptime_window"` // 24h | 90d: monitör "uptime" yüzdelerinin penceresi
	Status        string               `json:"status"`        // up | partial | down | unknown
	Sections      []publicSection      `json:"sections"`
	Announcements []publicAnnouncement `json:"announcements"`
	Incidents     []publicIncident     `json:"incidents"`
	// Maintenance sayfadaki monitörleri etkileyen süren ve 7 gün içindeki
	// planlı bakımlar (bakım bloğu görünürse; eski istemciler yok sayar).
	Maintenance []publicMaintenance `json:"maintenance"`
	// ShowIncidents false ise olay bölümü sayfada hiç gösterilmez (Incidents boş gelir).
	ShowIncidents bool `json:"show_incidents"`
	// Collapsible true ise ziyaretçi grupları açıp kapatabilir.
	Collapsible bool `json:"collapsible"`
	// Lang sayfanın dili (tr | en): arayüz sayfayı bu dilde gösterir.
	Lang string `json:"lang"`
	// HasPassword sayfa şifreli (ziyaretçi açmış): RSS bağlantısı gösterilmez,
	// akış yalnızca şifre çerezi olan tarayıcıya verilir.
	HasPassword bool `json:"has_password"`
	// Layout yerleşim, genişlik ve bölüm sırası/görünürlüğü. Gizli duyuru ve
	// grup bölümlerinin verisi gönderilmez (boş liste).
	Layout store.PageLayout `json:"layout"`
	// IncidentDays olay bölümünün penceresi (gün); UptimeWindows monitör
	// satırlarında gösterilen uptime pencereleri (boşsa tek pencere: UptimeWindow).
	IncidentDays  int      `json:"incident_days"`
	UptimeWindows []string `json:"uptime_windows"`
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
	case store.StatusMaintenance:
		return "maintenance"
	}
	return "pending"
}

// overallStatus genel durum: duraklatılmış ve bakımdaki monitörler sayılmaz;
// bekleyen (tekrar deneniyor) monitör henüz kesinti sayılmaz.
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

// monitorData sayfadaki monitörlerin herkese açık verisi: durum, çubuklar,
// uptime yüzdesi ve istenirse hedef (Name: monitörün kendi adı). Kullanılan
// çubuk kapsamını ve uptime penceresini de taşır.
type monitorData struct {
	pub      map[int64]publicMonitor
	barRange string
	window   string
}

func (s *Server) publicMonitorData(ctx context.Context, ids []int64, barRange string, showTargets bool, now int64, windows ...string) (monitorData, error) {
	d := monitorData{pub: map[int64]publicMonitor{}}
	mons, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		return d, err
	}
	// Ek uptime pencereleri: 24h saatlik özetten, 7d/30d/90d günlük özetten.
	windows = store.NormalizeUptimeWindows(windows)
	var winHourly, winDaily map[int64][]store.Bucket
	if len(windows) > 0 {
		var winDays int
		for _, w := range windows {
			switch w {
			case "7d":
				winDays = max(winDays, 7)
			case "30d":
				winDays = max(winDays, 30)
			case "90d":
				winDays = max(winDays, 90)
			}
		}
		if slices.Contains(windows, "24h") {
			if winHourly, err = s.store.HourlyFor(ctx, ids, now-now%3600-(publicHours-1)*3600); err != nil {
				return d, err
			}
		}
		if winDays > 0 {
			if winDaily, err = s.store.DailyFor(ctx, ids, s.store.DayStarts(now, winDays)[0]); err != nil {
				return d, err
			}
		}
	}
	// Çubuk zaman dilimleri (recent dışındakiler için) ve özet verisi.
	var (
		slots   []int64
		buckets map[int64][]store.Bucket
		recent  map[int64][]store.Beat
	)
	switch barRange {
	case store.BarRange90d:
		slots = s.store.DayStarts(now, publicDays)
		buckets, err = s.store.DailyFor(ctx, ids, slots[0])
	case store.BarRange24h:
		first := now - now%3600 - (publicHours-1)*3600
		for i := 0; i < publicHours; i++ {
			slots = append(slots, first+int64(i)*3600)
		}
		buckets, err = s.store.HourlyFor(ctx, ids, first)
	default:
		barRange = store.BarRangeRecent
		recent, err = s.store.RecentBeats(ctx, ids, publicRecentBeats)
		if err == nil {
			// Yüzde için son 24 saatin saatlik özeti (liste ekranıyla aynı pencere).
			buckets, err = s.store.HourlyFor(ctx, ids, now-now%3600-(publicHours-1)*3600)
		}
	}
	if err != nil {
		return d, err
	}
	d.barRange, d.window = barRange, "24h"
	if barRange == store.BarRange90d {
		d.window = "90d"
	}
	for _, id := range ids {
		m, ok := mons[id]
		if !ok {
			continue // sayfaya eklendikten sonra silinmiş
		}
		pub := publicMonitor{Name: m.Name, Status: monitorStatus(m), Bars: []publicBar{}}
		var up, down int64
		for _, b := range buckets[m.ID] {
			up += b.Up
			down += b.Down
		}
		if barRange == store.BarRangeRecent {
			for _, b := range recent[m.ID] {
				bar := publicBar{T: b.Time}
				switch b.Status {
				case store.StatusUp:
					bar.Up = 1
				case store.StatusDown:
					bar.Down = 1
				}
				pub.Bars = append(pub.Bars, bar)
			}
		} else {
			bySlot := make(map[int64]store.Bucket, len(buckets[m.ID]))
			for _, b := range buckets[m.ID] {
				bySlot[b.Time] = b
			}
			for _, t := range slots {
				b := bySlot[t]
				pub.Bars = append(pub.Bars, publicBar{T: t, Up: b.Up, Down: b.Down})
			}
		}
		if up+down > 0 {
			pct := math.Round(100000*float64(up)/float64(up+down)) / 1000
			pub.Uptime = &pct
			if barRange == store.BarRange90d {
				pub.Uptime90d = &pct
			}
		}
		if showTargets {
			pub.Target = publicTarget(engine.Target(m))
		}
		if len(windows) > 0 {
			pub.Uptimes = make(map[string]*float64, len(windows))
			for _, w := range windows {
				var bs []store.Bucket
				if w == "24h" {
					bs = winHourly[m.ID]
				} else {
					days := map[string]int{"7d": 7, "30d": 30, "90d": 90}[w]
					since := s.store.DayStarts(now, days)[0]
					for _, b := range winDaily[m.ID] {
						if b.Time >= since {
							bs = append(bs, b)
						}
					}
				}
				pub.Uptimes[w] = uptimeOf(bs)
			}
		}
		d.pub[m.ID] = pub
	}
	return d, nil
}

// uptimeOf özet kovalarının toplam uptime yüzdesi (veri yoksa nil).
func uptimeOf(bs []store.Bucket) *float64 {
	var up, down int64
	for _, b := range bs {
		up += b.Up
		down += b.Down
	}
	if up+down == 0 {
		return nil
	}
	pct := math.Round(100000*float64(up)/float64(up+down)) / 1000
	return &pct
}

// buildPublicPage sayfanın herkese açık JSON'unu hesaplar.
func (s *Server) buildPublicPage(ctx context.Context, p store.StatusPage) ([]byte, error) {
	now := s.now().Unix()
	ids := p.MonitorIDs()
	data, err := s.publicMonitorData(ctx, ids, p.BarRange, p.ShowTargets, now, p.UptimeWindows...)
	if err != nil {
		return nil, err
	}
	layout := store.NormalizeLayout(p.Layout, p.ShowIncidents)
	view := publicPageView{
		Slug: p.Slug, Title: p.Title, Description: p.Description, Footer: p.Footer,
		HasLogo: p.HasLogo, LogoURL: logoURL(p), UpdatedAt: now, Range: data.barRange, UptimeWindow: data.window,
		Sections: []publicSection{}, Announcements: []publicAnnouncement{}, Incidents: []publicIncident{}, Maintenance: []publicMaintenance{},
		Layout: layout, IncidentDays: p.IncidentDays, UptimeWindows: store.NormalizeUptimeWindows(p.UptimeWindows),
	}
	names := map[int64]string{}
	var statuses []string
	for _, sec := range p.Sections {
		ps := publicSection{Title: sec.Title, Monitors: []publicMonitor{}}
		for _, pm := range sec.Monitors {
			pub, ok := data.pub[pm.ID]
			if !ok {
				continue // sayfaya eklendikten sonra silinmiş
			}
			if pm.Name != "" {
				pub.Name = pm.Name
			}
			names[pm.ID] = pub.Name
			statuses = append(statuses, pub.Status)
			ps.Monitors = append(ps.Monitors, pub)
		}
		view.Sections = append(view.Sections, ps)
	}
	view.Status = overallStatus(statuses)
	// Gruplar bölümü gizliyse genel durum yine tüm monitörlerden hesaplanır,
	// monitör ayrıntıları gönderilmez.
	if !layout.Visible(store.BlockGroups) {
		view.Sections = []publicSection{}
	}

	if layout.Visible(store.BlockAnnouncements) {
		anns, err := s.store.ActiveAnnouncements(ctx, p.ID, now)
		if err != nil {
			return nil, err
		}
		view.Announcements = toPublicAnnouncements(anns)
	}
	if layout.Visible(store.BlockMaintenance) {
		mw, err := s.publicMaintenance(ctx, p, names, s.now())
		if err != nil {
			return nil, err
		}
		view.Maintenance = mw
	}
	view.ShowIncidents, view.Collapsible, view.Lang, view.HasPassword = p.ShowIncidents, p.Collapsible, i18n.Or(p.Lang), p.HasPassword
	if !p.ShowIncidents {
		return json.Marshal(view)
	}
	// Olay nedeni bilerek yok: iç IP ve hata ayrıntısı içerebilir. Elle açılan
	// olaylar başlık, önem ve güncellemeleriyle gelir.
	incs, err := s.publicIncidents(ctx, p, names, now)
	if err != nil {
		return nil, err
	}
	view.Incidents = incs
	return json.Marshal(view)
}

// incidentWindow sayfanın olay penceresi (saniye; ayarı yoksa 14 gün).
func incidentWindow(p store.StatusPage) int64 {
	if store.ValidIncidentDays(p.IncidentDays) {
		return int64(p.IncidentDays) * 86400
	}
	return publicIncidentWindow
}

func toPublicAnnouncements(anns []store.Announcement) []publicAnnouncement {
	out := make([]publicAnnouncement, 0, len(anns))
	for _, a := range anns {
		out = append(out, publicAnnouncement{
			ID: a.ID, Title: a.Title, Body: a.Body, Severity: a.Severity, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
		})
	}
	return out
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
	setResponseLang(w, e.page.Lang)
	if e.page.HasPassword && !s.unlocked(r, e.page) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "Bu sayfa şifre korumalı", "password_required": true,
			"title": e.page.Title, "has_logo": e.page.HasLogo, "logo_url": logoURL(e.page),
			"lang": i18n.Or(e.page.Lang),
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
	setResponseLang(w, e.page.Lang)
	if !e.page.HasPassword {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	// Kilit yalnızca IP + sayfa başına: bir saldırgan (ya da şifresini
	// unutan tek ziyaretçi) sayfayı herkese kapatamaz. Giriş sınırından ayrı.
	ip := clientIP(r)
	ipKey := "sayfa:" + slug + " " + ip
	lim := s.pages.limiter
	if ok, wait := lim.allowKeys(s.now(), ipKey); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Çok fazla hatalı deneme. "+strconv.Itoa(int(wait.Minutes())+1)+" dakika sonra tekrar deneyin.")
		return
	}
	if len(in.Password) > 72 || bcrypt.CompareHashAndPassword([]byte(e.page.PasswordHash), []byte(in.Password)) != nil {
		lim.failKey(ipKey, loginMaxPerIP, s.now())
		s.log.Warn("durum sayfası için hatalı şifre", "ip", ip, "sayfa", slug)
		writeError(w, http.StatusUnauthorized, "Şifre hatalı")
		return
	}
	lim.reset(ipKey)
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
