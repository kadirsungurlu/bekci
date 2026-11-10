package api

import (
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Salt okunur API anahtarı ve yönetici CLI'sının (bin/bekci) okuma uçları.
//
// Kapsam izin listesidir: burada adı geçmeyen uç (yeni eklenenler dahil) read
// kapsamlı anahtara kapalıdır. Rol izleyici olduğundan yanıtlar da izleyici
// görünümüdür: monitör ayarı, bildirim bağlantısı, IP kilidi ve mesh olayları
// gelmez; sahibi kısıtlı izleyiciyse yalnız ona atananlar görünür.

// readScopeExact tam eşleşen yollar.
var readScopeExact = map[string]bool{
	"/api/summary":          true,
	"/api/servers":          true,
	"/api/monitors":         true,
	"/api/incidents":        true,
	"/api/certs":            true,
	"/api/server-events":    true,
	"/api/security/summary": true,
}

// readScopeRe kimlik içeren yollar.
var readScopeRe = regexp.MustCompile(`^/api/(servers/[0-9]+(/(stats|timeline|events|inventory|incidents|security))?|monitors/[0-9]+|incidents/[0-9]+)$`)

func readScopeAllows(method, path string) bool {
	if method != http.MethodGet && method != http.MethodHead {
		return false
	}
	return readScopeExact[path] || readScopeRe.MatchString(path)
}

// readKeyBlocked istek geçerli bir salt okunur anahtarla izin listesi dışına mı
// gidiyor: yanıt 401 yerine 403 olsun, istemci "anahtar yanlış" sanmasın. Yalnız
// kimlik doğrulama başarısız olunca çalışır.
func (s *Server) readKeyBlocked(r *http.Request) bool {
	secret, ok := bearerAPIKey(r)
	if !ok || len(secret) > 100 {
		return false
	}
	k, err := s.store.APIKeyByHash(r.Context(), hashToken(secret))
	return err == nil && k.Usable(s.now().Unix()) && k.Scope == store.ScopeRead &&
		!readScopeAllows(r.Method, r.URL.Path)
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/certs", s.auth(s.serversOnly(s.listCerts)))
		mux.Handle("GET /api/server-events", s.auth(s.serversOnly(s.listServerEvents)))
	})
}

// visibleServers kullanıcının görebildiği sunucular, kimlikten ada.
func (s *Server) visibleServers(r *http.Request) (map[int64]string, error) {
	probes, err := s.store.ListProbesOfKind(r.Context(), store.ProbeKindServer)
	if err != nil {
		return nil, err
	}
	vis := visibleTo(userFrom(r))
	out := make(map[int64]string, len(probes))
	for _, p := range probes {
		if vis.canServer(p.ID) {
			out[p.ID] = p.Name
		}
	}
	return out, nil
}

type certView struct {
	store.InvCert
	Server   string `json:"server"`
	DaysLeft int64  `json:"days_left"` // tam gün; bitmişse eksi
}

// listCerts tüm sunucuların sertifikaları, bitişi en yakın olan önce. Yoksayılanlar
// ignored=1 ile gelir; days=N yalnız N gün içinde bitenleri verir.
func (s *Server) listCerts(w http.ResponseWriter, r *http.Request) {
	names, err := s.visibleServers(r)
	if err != nil {
		s.dbError(w, err)
		return
	}
	list, err := s.store.AllCerts(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	q := r.URL.Query()
	days, limited := int64(0), false
	if n, err := strconv.ParseInt(q.Get("days"), 10, 32); err == nil && n >= 0 {
		days, limited = n, true
	}
	now := s.now().Unix()
	out := []certView{}
	for _, c := range list {
		name, ok := names[c.ServerID]
		if !ok || (c.Ignored && q.Get("ignored") != "1") {
			continue
		}
		left := floorDays(c.NotAfter - now)
		if limited && left > days {
			continue
		}
		out = append(out, certView{InvCert: c, Server: name, DaysLeft: left})
	}
	writeJSON(w, http.StatusOK, map[string]any{"certs": out})
}

func floorDays(sec int64) int64 {
	d := sec / 86400
	if sec < 0 && sec%86400 != 0 {
		d--
	}
	return d
}

type timelineItem struct {
	store.TimelineEvent
	ServerID int64  `json:"server_id"`
	Server   string `json:"server"`
}

// listServerEvents görünür sunucuların olayları, yeniden eskiye. Süzgeçler:
// since, before (unix), kind, level (virgülle), limit (1-200, varsayılan 50).
func (s *Server) listServerEvents(w http.ResponseWriter, r *http.Request) {
	names, err := s.visibleServers(r)
	if err != nil {
		s.dbError(w, err)
		return
	}
	q := r.URL.Query()
	f := store.TimelineFilter{Limit: 50}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		f.Limit = min(n, 200)
	}
	limit := f.Limit
	f.Limit++ // timelinePage: aynı saniyedeki satırlar sayfa sınırında bölünmesin
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	f.Since, _ = strconv.ParseInt(q.Get("since"), 10, 64)
	for k := range strings.SplitSeq(q.Get("kind"), ",") {
		if timelineKindRe.MatchString(k) && len(f.Kinds) < 20 {
			f.Kinds = append(f.Kinds, k)
		}
	}
	for l := range strings.SplitSeq(q.Get("level"), ",") {
		if l == store.EventInfo || l == store.EventWarning || l == store.EventError {
			f.Levels = append(f.Levels, l)
		}
	}
	for id := range names {
		f.ProbeIDs = append(f.ProbeIDs, id)
	}
	slices.Sort(f.ProbeIDs)
	list, err := s.store.TimelineEvents(r.Context(), f)
	if err != nil {
		s.dbError(w, err)
		return
	}
	list, more := timelinePage(list, limit)
	out := make([]timelineItem, len(list))
	for i, e := range list {
		out[i] = timelineItem{TimelineEvent: e, ServerID: e.ProbeID, Server: names[e.ProbeID]}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out, "more": more})
}
