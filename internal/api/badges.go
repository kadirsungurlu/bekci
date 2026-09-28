package api

import (
	"fmt"
	"html"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Rozetler (Uptime Kuma tarzı SVG) ------------------------------------------------
//
//	GET /api/badge/{id}/status.svg
//	GET /api/badge/{id}/uptime.svg?duration=24h|7d|30d|90d
//	GET /api/badge/{id}/ping.svg?duration=24h|7d|30d|90d
//	GET /api/badge/{id}/cert-exp.svg
//
// Ortak parametreler: label, labelColor, color, upColor, downColor,
// pendingColor, style=flat|flat-square|for-the-badge.
//
// Herkese açıklık kuralı: rozet yalnızca monitör en az bir yayında ve şifresiz
// durum sayfasındaysa, ya da istek geçerli bir API anahtarı (veya oturum; ör.
// arayüzdeki rozet önizlemesi) taşıyor ve kullanıcı monitörü görebiliyorsa
// sunulur; aksi halde 404. Monitör adı rozete hiç yazılmaz (varsayılan
// etiketler geneldir), metinler XML'e kaçırılarak yazılır.

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /api/badge/{id}/{kind}", s.badge)
	})
}

const (
	badgeRatePerMin = 120 // kimliksiz rozet isteği: IP başına dakikada
	badgeAllowTTL   = 60 * time.Second
)

// badgeState rozet uç noktasının önbelleği ve IP hız sınırı.
//   - allow: hangi monitörlerin en az bir yayında ve şifresiz durum sayfasında
//     göründüğü; her istekte ListPages çalıştırmamak için kısa süre saklanır.
//   - ipWindow: kimliksiz isteklerin IP başına dakikalık sayacı (numaralandırma
//     ve maliyet saldırılarını yavaşlatır); probeLimiter ile aynı desen.
type badgeState struct {
	mu       sync.Mutex
	allow    map[int64]bool // nil: henüz yüklenmedi
	allowExp time.Time
	ipWindow map[string]*badgeWindow
}

type badgeWindow struct {
	start int64
	count int
}

func newBadgeState() *badgeState {
	return &badgeState{ipWindow: map[string]*badgeWindow{}}
}

// rate IP başına dakikalık istek sınırı (sabit pencere). Sınır aşıldıysa false.
func (b *badgeState) rate(ip string, now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	minute := now.Unix() / 60
	w := b.ipWindow[ip]
	if w == nil || w.start != minute {
		if len(b.ipWindow) > 10000 { // bellek şişmesin: eski pencereleri at
			for k, v := range b.ipWindow {
				if v.start != minute {
					delete(b.ipWindow, k)
				}
			}
		}
		w = &badgeWindow{start: minute}
		b.ipWindow[ip] = w
	}
	w.count++
	return w.count <= badgeRatePerMin
}

// publicBadgeMonitors yayında ve şifresiz sayfalarda görünen monitör
// kimliklerini kısa süreli önbellekten döner. Dönen harita salt okunurdur
// (kurulduktan sonra değiştirilmez), eşzamanlı okunması güvenlidir.
func (s *Server) publicBadgeMonitors(r *http.Request) (map[int64]bool, error) {
	now := s.now()
	b := s.badges
	b.mu.Lock()
	if b.allow != nil && now.Before(b.allowExp) {
		m := b.allow
		b.mu.Unlock()
		return m, nil
	}
	b.mu.Unlock()

	pages, err := s.store.ListPages(r.Context())
	if err != nil {
		return nil, err
	}
	m := map[int64]bool{}
	for _, p := range pages {
		if !p.Published || p.HasPassword {
			continue
		}
		for _, mid := range p.MonitorIDs() {
			m[mid] = true
		}
	}
	b.mu.Lock()
	b.allow, b.allowExp = m, now.Add(badgeAllowTTL)
	b.mu.Unlock()
	return m, nil
}

var namedColors = map[string]string{
	"brightgreen": "#4c1", "green": "#97ca00", "yellowgreen": "#a4a61d", "yellow": "#dfb317",
	"orange": "#fe7d37", "red": "#e05d44", "blue": "#007ec6", "lightgrey": "#9f9f9f",
	"lightgray": "#9f9f9f", "grey": "#555", "gray": "#555", "success": "#4c1",
	"important": "#fe7d37", "critical": "#e05d44", "informational": "#007ec6", "inactive": "#9f9f9f",
}

var hexColorRe = regexp.MustCompile(`^#?([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// parseColor sadece adlandırılmış renkleri veya #rgb/#rrggbb kabul eder;
// dönen değer her zaman "#" ile başlayan onaltılık renktir (SVG'ye güvenle yazılır).
func parseColor(v string) (string, bool) {
	if c, ok := namedColors[strings.ToLower(v)]; ok {
		return c, true
	}
	if hexColorRe.MatchString(v) {
		return "#" + strings.ToLower(strings.TrimPrefix(v, "#")), true
	}
	return "", false
}

type badgeOpts struct {
	style                            string
	label                            string
	labelColor                       string
	color                            string // ping rozeti ve genel değer rengi
	upColor, downColor, pendingColor string
	warnColor, greyColor             string
	windowSec                        int64
	windowLabel                      string
	// lang rozet metinlerinin dili (?lang=tr|en; varsayılan tr — rozet
	// README'lere gömülür, tarayıcı diline göre değişmemeli).
	lang string
}

// badgeWindows süre → saniye; etiket i18n "badge.window.<süre>".
var badgeWindows = map[string]int64{"24h": 86400, "7d": 7 * 86400, "30d": 30 * 86400, "90d": 90 * 86400}

// cleanLabel etiketi sınırlar: en fazla 64 karakter, XML'de geçersiz kontrol
// karakterleri atılır (kaçırma ayrıca yazarken yapılır).
func cleanLabel(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '￾' || r == '￿' {
			return -1
		}
		return r
	}, s)
	if utf8.RuneCountInString(s) > 64 {
		s = string([]rune(s)[:64])
	}
	return strings.TrimSpace(s)
}

func parseBadgeOpts(r *http.Request) (badgeOpts, error) {
	q := r.URL.Query()
	o := badgeOpts{
		style: "flat", labelColor: "#555", color: "#007ec6",
		upColor: "#4c1", downColor: "#e05d44", pendingColor: "#dfb317",
		warnColor: "#dfb317", greyColor: "#9f9f9f", lang: i18n.Or(q.Get("lang")),
	}
	if st := q.Get("style"); st != "" {
		switch st {
		case "flat", "flat-square", "for-the-badge":
			o.style = st
		default:
			return o, fmt.Errorf("Geçersiz stil; flat, flat-square veya for-the-badge olmalı")
		}
	}
	for name, dst := range map[string]*string{
		"labelColor": &o.labelColor, "color": &o.color, "upColor": &o.upColor,
		"downColor": &o.downColor, "pendingColor": &o.pendingColor,
	} {
		if v := q.Get(name); v != "" {
			c, ok := parseColor(v)
			if !ok {
				return o, fmt.Errorf("Geçersiz renk (%s): #rgb, #rrggbb veya bir renk adı olmalı", name)
			}
			*dst = c
		}
	}
	d := q.Get("duration")
	if d == "" {
		d = "24h"
	}
	sec, ok := badgeWindows[d]
	if !ok {
		return o, fmt.Errorf("Geçersiz süre; 24h, 7d, 30d veya 90d olmalı")
	}
	o.windowSec, o.windowLabel = sec, i18n.T(o.lang, "badge.window."+d)
	o.label = cleanLabel(q.Get("label"))
	return o, nil
}

// badgeAllowed herkese açıklık kuralı (bkz. dosya başı).
func (s *Server) badgeAllowed(r *http.Request, id int64) (allowed, private bool, err error) {
	if secret, ok := bearerAPIKey(r); ok {
		if u, ok := s.userForAPIKey(r, secret); ok && store.RoleRank(u.Role) > 0 && visibleTo(u).can(id) {
			return true, true, nil
		}
	}
	if u, _, ok := s.currentUser(r); ok && visibleTo(u).can(id) {
		return true, true, nil
	}
	allow, err := s.publicBadgeMonitors(r)
	if err != nil {
		return false, false, err
	}
	return allow[id], false, nil
}

func (s *Server) badge(w http.ResponseWriter, r *http.Request) {
	kind := r.PathValue("kind")
	switch kind {
	case "status.svg", "uptime.svg", "ping.svg", "cert-exp.svg":
	default:
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	o, err := parseBadgeOpts(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	allowed, private, err := s.badgeAllowed(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Kimliksiz (herkese açık) istekler için IP başına hız sınırı; oturum veya
	// API anahtarıyla monitörü görebilen istekler (private) muaftır.
	if !private && !s.badges.rate(clientIP(r), s.now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Çok fazla istek; kısa süre sonra tekrar deneyin")
		return
	}
	if !allowed {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	m, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now().Unix()
	var label, value, color string
	l := o.lang
	switch kind {
	case "status.svg":
		label = i18n.T(l, "badge.status")
		switch {
		case !m.Active:
			value, color = i18n.T(l, "badge.paused"), o.greyColor
		case m.Status == store.StatusUp:
			value, color = i18n.T(l, "badge.up"), o.upColor
		case m.Status == store.StatusDown:
			value, color = i18n.T(l, "badge.down"), o.downColor
		case m.Status == store.StatusPending:
			value, color = i18n.T(l, "badge.pending"), o.pendingColor
		case m.Status == store.StatusMaintenance:
			value, color = i18n.T(l, "badge.maintenance"), o.greyColor
		default:
			value, color = i18n.T(l, "badge.unknown"), o.greyColor
		}
	case "uptime.svg":
		label = i18n.T(l, "badge.uptime", o.windowLabel)
		pct, ok, err := s.store.Uptime(r.Context(), id, now-o.windowSec)
		if err != nil {
			s.dbError(w, err)
			return
		}
		switch {
		case !ok:
			value, color = i18n.T(l, "badge.no_data"), o.greyColor
		case pct >= 99:
			value, color = fmtPercent(l, pct), o.upColor
		case pct >= 95:
			value, color = fmtPercent(l, pct), o.warnColor
		default:
			value, color = fmtPercent(l, pct), o.downColor
		}
	case "ping.svg":
		label = i18n.T(l, "badge.ping", o.windowLabel)
		avg, err := s.store.AvgPing(r.Context(), id, now-o.windowSec)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if avg < 0 {
			value, color = i18n.T(l, "badge.no_data"), o.greyColor
		} else {
			value, color = fmtThousands(l, avg)+" ms", o.color
		}
	case "cert-exp.svg":
		label = i18n.T(l, "badge.cert")
		switch {
		case m.CertExpiresAt == 0:
			value, color = i18n.T(l, "badge.cert_none"), o.greyColor
		case m.CertExpiresAt <= now:
			value, color = i18n.T(l, "badge.cert_expired"), o.downColor
		default:
			days := (m.CertExpiresAt - now) / 86400
			value, color = i18n.TN(l, "badge.days", int(days), days), o.upColor
			if days < 14 {
				color = o.warnColor
			}
		}
	}
	if o.label != "" {
		label = o.label
	}
	h := w.Header()
	h.Set("Content-Type", "image/svg+xml; charset=utf-8")
	if private {
		h.Set("Cache-Control", "private, max-age=60")
	} else {
		h.Set("Cache-Control", "public, max-age=60")
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(renderBadge(label, value, o.labelColor, color, o.style)))
}

// fmtPercent arayüzle aynı biçim: %99,95; yuvarlama aşağı, tam 100 ise %100.
func fmtPercent(lang string, v float64) string {
	en := i18n.Or(lang) == i18n.EN
	if v >= 100 {
		if en {
			return "100%"
		}
		return "%100"
	}
	f := math.Floor(v*100) / 100
	if en {
		return strconv.FormatFloat(f, 'f', 2, 64) + "%"
	}
	return "%" + strings.Replace(strconv.FormatFloat(f, 'f', 2, 64), ".", ",", 1)
}

// fmtThousands 1234 → tr "1.234", en "1,234".
func fmtThousands(lang string, n int64) string {
	sep := "."
	if i18n.Or(lang) == i18n.EN {
		sep = ","
	}
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + sep + s[i:]
	}
	return s
}

// SVG çizimi -----------------------------------------------------------------------

// textWidth Verdana 11px için yaklaşık metin genişliği (piksel).
func textWidth(s string, bold bool) float64 {
	var w float64
	for _, r := range s {
		switch {
		case strings.ContainsRune("iljt.,:;!|'I ()[]f", r):
			w += 3.8
		case strings.ContainsRune("mwMW%@", r):
			w += 10.5
		case unicode.IsUpper(r):
			w += 7.6
		case unicode.IsDigit(r):
			w += 7
		default:
			w += 6.4
		}
	}
	if bold {
		w *= 1.1
	}
	return w
}

// textColorFor açık zeminde koyu, koyu zeminde beyaz yazı.
func textColorFor(hex string) (fill, shadow string) {
	h := strings.TrimPrefix(hex, "#")
	if len(h) == 3 {
		h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
	}
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return "#fff", "#010101"
	}
	r, g, b := float64(v>>16&0xff), float64(v>>8&0xff), float64(v&0xff)
	if (0.299*r+0.587*g+0.114*b)/255 > 0.69 {
		return "#333", "#ccc"
	}
	return "#fff", "#010101"
}

// renderBadge shields.io görünümünde iki parçalı rozet. label ve value kaçırılır;
// renkler parseColor'dan geçmiş (#onaltılık) değerlerdir.
func renderBadge(label, value, labelColor, color, style string) string {
	title := html.EscapeString(label + ": " + value)
	if style == "for-the-badge" {
		label = strings.ToUpperSpecial(unicode.TurkishCase, label)
		value = strings.ToUpperSpecial(unicode.TurkishCase, value)
	}
	const pad = 10.0
	height, fontSize, textY, extraPad := 20, 11, 14, 0.0
	fontWeight, letterSpacing := "normal", 0.0
	if style == "for-the-badge" {
		height, fontSize, textY, extraPad = 28, 10, 18, 14
		fontWeight, letterSpacing = "bold", 1.2
	}
	bold := style == "for-the-badge"
	lw := math.Round(textWidth(label, bold) + float64(utf8.RuneCountInString(label))*letterSpacing + pad + extraPad)
	vw := math.Round(textWidth(value, bold) + float64(utf8.RuneCountInString(value))*letterSpacing + pad + extraPad)
	if label == "" {
		lw = 0
	}
	total := lw + vw
	lFill, lShadow := textColorFor(labelColor)
	vFill, vShadow := textColorFor(color)
	le, ve := html.EscapeString(label), html.EscapeString(value)

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%g" height="%d" role="img" aria-label="%s">`, total, height, title)
	fmt.Fprintf(&b, `<title>%s</title>`, title)
	rx := 0
	if style == "flat" {
		rx = 3
		b.WriteString(`<linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>`)
	}
	fmt.Fprintf(&b, `<clipPath id="r"><rect width="%g" height="%d" rx="%d" fill="#fff"/></clipPath>`, total, height, rx)
	fmt.Fprintf(&b, `<g clip-path="url(#r)"><rect width="%g" height="%d" fill="%s"/><rect x="%g" width="%g" height="%d" fill="%s"/>`,
		lw, height, labelColor, lw, vw, height, color)
	if style == "flat" {
		fmt.Fprintf(&b, `<rect width="%g" height="%d" fill="url(#s)"/>`, total, height)
	}
	b.WriteString(`</g>`)
	fmt.Fprintf(&b, `<g text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="%d" font-weight="%s" letter-spacing="%g">`,
		fontSize, fontWeight, letterSpacing)
	text := func(x float64, s, fill, shadow string) {
		if style == "flat" {
			fmt.Fprintf(&b, `<text x="%g" y="%d" fill="%s" fill-opacity=".3">%s</text>`, x, textY+1, shadow, s)
		}
		fmt.Fprintf(&b, `<text x="%g" y="%d" fill="%s">%s</text>`, x, textY, fill, s)
	}
	if lw > 0 {
		text(lw/2, le, lFill, lShadow)
	}
	text(lw+vw/2, ve, vFill, vShadow)
	b.WriteString(`</g></svg>`)
	return b.String()
}
