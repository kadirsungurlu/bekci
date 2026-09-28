// Package api arayüzün kullandığı REST API'yi, canlı olay akışını (SSE),
// push adresini ve gömülü arayüz dosyalarını sunar.
package api

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/servers"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

type Server struct {
	store    *store.Store
	engine   *engine.Engine
	hub      *engine.Hub
	notifier *notify.Dispatcher
	log      *slog.Logger
	static   fs.FS
	version  string
	limiter  *loginLimiter
	now      func() time.Time
	pages    *pagesState    // durum sayfası önbellekleri (pages.go)
	probeRL  *probeLimiter  // kontrol noktası istek sınırı (probes.go)
	badges   *badgeState    // rozet önbelleği ve IP hız sınırı (badges.go)
	notifyRL *notifyLimiter // bildirim test/örnek hız sınırı (notifications.go)
	servers  *servers.Service

	// BaseURL uygulamanın dış adresi (BASE_URL); durum sayfası özel alan adı
	// bu adresin sunucu adıyla aynı olamaz. Boş olabilir.
	BaseURL string
	// ProbeImage kontrol noktası kurulum komutunda gösterilecek Docker imajı
	// (PROBE_IMAGE); boşsa "uptime".
	ProbeImage string
	// AgentDir başka platformlar için derlenmiş ajan programlarının klasörü
	// (AGENT_DIR; uptime-<os>-<arch>[.exe]). GET /api/probe/binary?os=…&arch=…
	// buradan sunar.
	AgentDir string
}

// DefaultAgentDir Docker imajında ajan programlarının bulunduğu klasör.
const DefaultAgentDir = "/usr/local/share/uptime/agents"

func New(st *store.Store, e *engine.Engine, hub *engine.Hub, n *notify.Dispatcher, log *slog.Logger, static fs.FS, version string) *Server {
	s := &Server{
		store: st, engine: e, hub: hub, notifier: n, log: log, static: static, version: version,
		limiter: newLoginLimiter(), now: time.Now, pages: newPagesState(), probeRL: newProbeLimiter(),
		badges:   newBadgeState(),
		notifyRL: newNotifyLimiter(),
		AgentDir: DefaultAgentDir,
	}
	var (
		pub servers.Publisher
		not servers.Notifier
	)
	if hub != nil {
		pub = hub
	}
	if n != nil {
		not = n
	}
	s.servers = servers.New(st, pub, not, log)
	// Sunucu takibi API'nin saatini kullanır (testler s.now'ı değiştirir).
	s.servers.SetClock(func() time.Time { return s.now() })
	return s
}

// Servers sunucu takibi servisi (main çevrimdışı taramasını başlatır).
func (s *Server) Servers() *servers.Service { return s.servers }

// extraRoutes özellik dosyalarının init() içinde RegisterRoutes ile eklediği
// rotalar; paralel geliştirmede server.go'da çakışma olmasın diye.
var extraRoutes []func(s *Server, mux *http.ServeMux)

func RegisterRoutes(f func(s *Server, mux *http.ServeMux)) { extraRoutes = append(extraRoutes, f) }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /api/push/{token}", s.push)
	mux.HandleFunc("POST /api/push/{token}", s.push)

	mux.HandleFunc("GET /api/auth/state", s.authState)
	mux.HandleFunc("POST /api/auth/setup", s.setup)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.Handle("POST /api/auth/password", s.auth(s.changePassword))

	mux.Handle("GET /api/summary", s.auth(s.summary))
	mux.Handle("GET /api/monitors", s.auth(s.listMonitors))
	mux.Handle("POST /api/monitors", s.editor(s.createMonitor))
	mux.Handle("GET /api/monitors/{id}", s.auth(s.getMonitor))
	mux.Handle("PUT /api/monitors/{id}", s.editor(s.updateMonitor))
	mux.Handle("DELETE /api/monitors/{id}", s.editor(s.deleteMonitor))
	mux.Handle("POST /api/monitors/{id}/pause", s.editor(s.pauseMonitor))
	mux.Handle("POST /api/monitors/{id}/resume", s.editor(s.resumeMonitor))
	mux.Handle("GET /api/monitors/{id}/series", s.auth(s.monitorSeries))
	mux.Handle("GET /api/monitors/{id}/incidents", s.auth(s.monitorIncidents))
	mux.Handle("GET /api/incidents", s.auth(s.listIncidents))

	mux.Handle("GET /api/notifications", s.editor(s.listNotifications))
	mux.Handle("POST /api/notifications", s.editor(s.createNotification))
	mux.Handle("PUT /api/notifications/{id}", s.editor(s.updateNotification))
	mux.Handle("DELETE /api/notifications/{id}", s.editor(s.deleteNotification))
	mux.Handle("POST /api/notifications/test", s.editor(s.testNotification))
	mux.Handle("POST /api/notifications/{id}/samples", s.editor(s.sampleNotifications))

	mux.Handle("GET /api/settings", s.admin(s.getSettings))
	mux.Handle("PUT /api/settings", s.admin(s.putSettings))
	mux.Handle("GET /api/events", s.auth(s.events))

	mux.Handle("GET /api/users", s.admin(s.listUsers))
	mux.Handle("POST /api/users", s.admin(s.createUser))
	mux.Handle("PUT /api/users/{id}", s.admin(s.updateUser))
	mux.Handle("DELETE /api/users/{id}", s.admin(s.deleteUser))
	mux.Handle("POST /api/users/{id}/password", s.admin(s.resetUserPassword))
	mux.Handle("GET /api/audit", s.admin(s.listAudit))

	// Özellik dosyalarının kendi rotaları (RegisterRoutes).
	for _, f := range extraRoutes {
		f(s, mux)
	}

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Bulunamadı")
	})
	mux.HandleFunc("/", s.serveStatic)

	// customDomainOnly: durum sayfasının özel alan adından gelen isteklerde
	// yönetim API'si kapalıdır (pages.go).
	return s.recoverer(s.logRequests(securityHeaders(s.customDomainOnly(csrf(mux)))))
}

// Ara katmanlar ------------------------------------------------------------------

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// csrf: API'yi değiştiren istekler özel bir başlık taşımalı. Başka bir siteden
// gelen form veya basit istek bu başlığı ekleyemez (CORS ön kontrolüne takılır).
// Push adresi dışarıdan çağrıldığı için muaftır. Oturum çerezi olmadan API
// anahtarıyla (Authorization: Bearer upk_…) gelen istekler de muaftır:
// tarayıcı bu başlığı başka siteden kendiliğinden eklemez (bkz. isAPIKeyRequest).
// Kontrol noktası uç noktaları (/api/probe/…) da aynı gerekçeyle muaftır
// (bkz. isProbeRequest).
func csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/push/") &&
			r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("X-Uptime") != "1" &&
			!isAPIKeyRequest(r) && !isProbeRequest(r) {
			writeError(w, http.StatusForbidden, "Geçersiz istek")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int)        { r.status = code; r.ResponseWriter.WriteHeader(code) }
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(rec, r)
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/api/events" {
			return
		}
		level := slog.LevelDebug
		if rec.status >= 500 {
			level = slog.LevelWarn
		}
		s.log.Log(r.Context(), level, "istek", "yöntem", r.Method, "yol", maskLogPath(r.URL.Path), "durum", rec.status, "süre", time.Since(start).Round(time.Millisecond))
	})
}

// maskLogPath push adresindeki gizli token'ı loga yazmadan önce gizler:
// /api/push/{token} → /api/push/***. Token bir sırdır; log dosyasına düşmemeli.
func maskLogPath(p string) string {
	if strings.HasPrefix(p, "/api/push/") {
		return "/api/push/***"
	}
	return p
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler {
					panic(p)
				}
				s.log.Error("istek paniği", "yol", r.URL.Path, "panik", p)
				writeError(w, http.StatusInternalServerError, "Sunucu hatası")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Yardımcılar ------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// readJSON gövdeyi v'ye çözer; hata varsa 400 yazar ve false döner.
func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi: "+err.Error())
		return false
	}
	return true
}

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return 0, false
	}
	return id, true
}

// dbError veritabanı hatasını uygun HTTP yanıtına çevirir.
func (s *Server) dbError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	s.log.Error("veritabanı hatası", "hata", err)
	writeError(w, http.StatusInternalServerError, "Sunucu hatası")
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		http.Error(w, "veritabanı erişilemiyor", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	io.WriteString(w, "ok")
}

// Arayüz dosyaları -------------------------------------------------------------------

func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if name == "" {
		name = "index.html"
	}
	f, err := s.static.Open(name)
	if err != nil {
		// Dosya uzantılı (ör. /assets/x.js, /manifest.webmanifest) veya assets/
		// altındaki eksik dosya gerçekten yoktur: HTML değil 404 dönülür. Aksi halde
		// eski sürümün betiği yerine index.html gelir; tarayıcı veya servis çalışanı
		// onu betik ya da simge sanıp önbelleğe alırdı.
		if path.Ext(name) != "" || strings.HasPrefix(name, "assets/") {
			http.NotFound(w, r)
			return
		}
		// Tek sayfalık uygulama: bilinmeyen (uzantısız) yollar arayüze gider.
		name = "index.html"
		if f, err = s.static.Open(name); err != nil {
			http.Error(w, "Arayüz derlenmemiş", http.StatusNotFound)
			return
		}
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(name, "assets/") {
		// Vite dosya adlarına içerik özeti ekler; sonsuza kadar önbelleğe alınabilir.
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		// index.html, sw.js (servis çalışanı güncellemesi hemen görülsün),
		// manifest ve simgeler her istekte doğrulanır.
		w.Header().Set("Cache-Control", "no-cache")
	}
	// Go'nun yerleşik MIME tablosunda .webmanifest yok; kurulabilirlik için doğru
	// tür gerekir. .js her ortamda (sistemde mime.types olmasa da) JavaScript'tir.
	switch path.Ext(name) {
	case ".webmanifest":
		w.Header().Set("Content-Type", "application/manifest+json")
	case ".js", ".mjs":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	}
	http.ServeContent(w, r, name, st.ModTime(), f.(io.ReadSeeker))
}
