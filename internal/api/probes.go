package api

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/schedule"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Uzak kontrol noktaları --------------------------------------------------------------
//
// Aynı ikili/imaj başka bir sunucuda "uptime probe" olarak çalışır; kendisine
// atanan monitörleri kontrol edip sonuçları buraya bildirir. Kimlik doğrulama:
//
//	Authorization: Bearer upr_...
//
// Token biçimi "upr_" + 43 karakter base62 (~256 bit); yalnızca oluşturulurken
// (ve yenilenirken) bir kez gösterilir, veritabanında SHA-256 özeti tutulur.
// Bir kontrol noktası yalnızca kendisine atanmış aktif monitörleri görebilir
// ve yalnızca onlar için sonuç gönderebilir. Devre dışı bırakılan veya token'ı
// yenilenen kontrol noktasının istekleri hemen reddedilir.
//
// Kontrol noktası uç noktaları oturum çerezini hiç kullanmaz; bu yüzden CSRF
// başlığından muaftır (isProbeRequest). Kullanıcı uç noktaları da probe
// token'ını kabul etmez (API anahtarı "upk_" ile başlar).

const (
	probeTokenPrefix  = "upr_"
	probeTokenRandLen = 43
	probeTokenShown   = 12 // arayüzde gösterilen baş kısım: "upr_" + 8 karakter
	probeMaxCount     = 100
	probeMaxPerMon    = 50  // bir monitöre atanabilecek en fazla kontrol noktası
	probeTouchEvery   = 10  // son görülme zamanı en fazla 10 saniyede bir yazılır
	probeMaxBatch     = 500 // tek istekte en fazla sonuç
	probeMaxAge       = time.Hour
	probeRatePerMin   = 120 // kontrol noktası başına dakikada en fazla istek
	probeMaxMessage   = 500
	probeMaxBody      = 2 << 20 // sonuç gönderimi; ayrıntılar (olay sayfası) dahil
	probeVersionMax   = 50
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/probes", s.auth(s.listProbes))
		mux.Handle("POST /api/probes", s.admin(s.createProbe))
		mux.Handle("PUT /api/probes/{id}", s.admin(s.updateProbe))
		mux.Handle("DELETE /api/probes/{id}", s.admin(s.deleteProbe))
		mux.Handle("POST /api/probes/{id}/token", s.admin(s.regenerateProbeToken))

		mux.Handle("GET /api/monitors/{id}/locations", s.auth(s.getMonitorLocations))
		mux.Handle("PUT /api/monitors/{id}/locations", s.editor(s.putMonitorLocations))

		mux.Handle("GET /api/probe/jobs", s.probeOnly(s.probeJobs))
		mux.Handle("GET /api/probe/binary", s.binaryRateLimit(s.probeBinary))
		mux.Handle("POST /api/probe/results", s.probeOnly(s.probeResults))
	})
}

func newProbeToken() string {
	var b strings.Builder
	b.WriteString(probeTokenPrefix)
	max := big.NewInt(int64(len(base62)))
	for range probeTokenRandLen {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand hata vermez (Go 1.24+)
		}
		b.WriteByte(base62[n.Int64()])
	}
	return b.String()
}

// bearerProbeToken Authorization: Bearer upr_… başlığındaki token.
func bearerProbeToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, strings.HasPrefix(token, probeTokenPrefix) && len(token) <= 100
}

// isProbeRequest CSRF muafiyeti için: kontrol noktası uç noktasına probe
// token'ıyla gelen istek. Bu uç noktalar çerezle yetkilendirilmez.
func isProbeRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/probe/") {
		return false
	}
	_, ok := bearerProbeToken(r)
	return ok
}

// probeLimiter kontrol noktası başına dakikalık istek sınırı (sabit pencere).
type probeLimiter struct {
	mu sync.Mutex
	m  map[int64]*probeWindow
}

type probeWindow struct {
	start int64
	count int
}

func newProbeLimiter() *probeLimiter { return &probeLimiter{m: map[int64]*probeWindow{}} }

func (l *probeLimiter) allow(id int64, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	minute := now.Unix() / 60
	w := l.m[id]
	if w == nil || w.start != minute {
		w = &probeWindow{start: minute}
		l.m[id] = w
	}
	w.count++
	if w.count > probeRatePerMin {
		return false, int(60 - now.Unix()%60)
	}
	return true, 0
}

type ctxProbeKey struct{}

func probeFrom(r *http.Request) store.Probe {
	p, _ := r.Context().Value(ctxProbeKey{}).(store.Probe)
	return p
}

// probeOnly kontrol noktası kimlik doğrulaması: geçerli token, etkin kayıt ve
// istek sınırı. Son görülme zamanı, adres ve sürüm de burada güncellenir
// (ayrı bir "heartbeat" isteği yoktur; her istek bir yaşam belirtisidir).
func (s *Server) probeOnly(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqIP := clientIP(r)
		// Aynı IP'den çok sayıda başarısız istek: veritabanına gitmeden reddet.
		if blocked, wait := s.probeAuthRL.blocked(reqIP, s.now()); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeError(w, http.StatusTooManyRequests, "Çok fazla başarısız istek; biraz sonra tekrar deneyin")
			return
		}
		token, ok := bearerProbeToken(r)
		if !ok {
			s.probeAuthFail(w, reqIP, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı", "")
			return
		}
		p, err := s.store.ProbeByTokenHash(r.Context(), hashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			s.probeAuthFail(w, reqIP, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı", "geçersiz kontrol noktası token'ı")
			return
		}
		if err != nil {
			s.dbError(w, err)
			return
		}
		if !p.Active {
			writeError(w, http.StatusForbidden, "Kontrol noktası devre dışı")
			return
		}
		// IP kilidi: adres ailesi başına (IPv4 tam adres, IPv6 /64) ilk bağlantı
		// sabitler; sabitlenmiş ailede farklı adres reddedilir (probe_guard.go).
		allowed, err := s.enforceIPLock(r.Context(), &p, reqIP)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if !allowed {
			s.probeAuthFail(w, reqIP, http.StatusForbidden, "Bu ajan başka bir IP'ye kilitli; taşındıysa Ayarlar'dan kilidi sıfırlayın",
				"kilitli ajana farklı IP'den erişim reddedildi", "ajan", p.Name, "kilitli", p.LockedIP)
			return
		}
		now := s.now()
		if ok, wait := s.probeRL.allow(p.ID, now); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeError(w, http.StatusTooManyRequests, "Çok fazla istek; biraz sonra tekrar deneyin")
			return
		}
		ip, version := reqIP, cleanVersion(r.Header.Get("X-Probe-Version"))
		if now.Unix()-p.LastSeenAt >= probeTouchEvery || ip != p.LastIP || version != p.Version {
			if err := s.store.TouchProbe(r.Context(), p.ID, now.Unix(), ip, version); err != nil {
				s.log.Warn("kontrol noktası son görülme zamanı yazılamadı", "hata", err)
			}
			p.LastSeenAt, p.LastIP, p.Version = now.Unix(), ip, version
		}
		h(w, r.WithContext(context.WithValue(r.Context(), ctxProbeKey{}, p)))
	})
}

// cleanVersion sürüm başlığını kısa ve yazdırılabilir tutar.
func cleanVersion(v string) string {
	v = strings.Map(func(r rune) rune {
		if r > unicode.MaxASCII || !unicode.IsPrint(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(v))
	if len(v) > probeVersionMax {
		v = v[:probeVersionMax]
	}
	return v
}

// Yönetim ------------------------------------------------------------------------------

// probeSummary herkesin (izleyici dahil) görebildiği bilgi.
type probeSummary struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Active     bool   `json:"active"`
	Online     bool   `json:"online"`
	LastSeenAt int64  `json:"last_seen_at"`
}

// probeAdminView yöneticinin gördüğü bilgi (token'ın kendisi hiçbir zaman yok).
type probeAdminView struct {
	probeSummary
	Kind         string `json:"kind"` // location | server
	TokenPrefix  string `json:"token_prefix"`
	CreatedAt    int64  `json:"created_at"`
	LastIP       string `json:"last_ip"`
	Version      string `json:"version"`
	MonitorCount int    `json:"monitor_count"`
	Metrics      bool   `json:"metrics"`   // sunucu metrikleri toplanıyor mu
	IPLock       bool   `json:"ip_lock"`   // yalnızca kilitli IP'den bağlanabilir mi
	LockedIP     string `json:"locked_ip"` // sabitlenmiş IP (boş: henüz bağlanmadı)
	// NotifyOffline: 90 sn istek gelmeyince ve tekrar gelince NotificationIDs
	// kanallarına bildirim gider, probe_offline olayı açılır/kapanır.
	NotifyOffline   bool    `json:"notify_offline"`
	NotificationIDs []int64 `json:"notification_ids"`
}

func (s *Server) probeSummaryOf(p store.Probe) probeSummary {
	// Uzun yoklama bağlantısı koptuysa beklemeden çevrimdışı (konum kartlarıyla aynı anda).
	online := engine.ProbeOnline(p, s.now()) && !s.engine.ProbeDisconnected(p.ID)
	return probeSummary{ID: p.ID, Name: p.Name, Active: p.Active, Online: online, LastSeenAt: p.LastSeenAt}
}

func (s *Server) probeAdminOf(p store.Probe, monitors int, notificationIDs []int64) probeAdminView {
	if notificationIDs == nil {
		notificationIDs = []int64{}
	}
	return probeAdminView{
		probeSummary: s.probeSummaryOf(p), Kind: p.Kind, TokenPrefix: p.TokenPrefix, CreatedAt: p.CreatedAt,
		LastIP: p.LastIP, Version: p.Version, MonitorCount: monitors, Metrics: p.Metrics,
		IPLock: p.IPLock, LockedIP: p.LockedIP, NotifyOffline: p.NotifyOffline, NotificationIDs: notificationIDs,
	}
}

// listProbes yalnızca kontrol noktaları (sunucular /api/servers altında).
func (s *Server) listProbes(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProbesOfKind(r.Context(), store.ProbeKindLocation)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if u := userFrom(r); u.Role != store.RoleAdmin {
		// Kısıtlı izleyici (müşteri) yalnızca görebildiği monitörlerde
		// kullanılan kontrol noktalarının adlarını görür.
		var used map[int64]bool
		if vis := visibleTo(u); !vis.all {
			locs, err := s.store.AllMonitorLocations(r.Context())
			if err != nil {
				s.dbError(w, err)
				return
			}
			used = map[int64]bool{}
			for mid, l := range locs {
				if vis.can(mid) {
					for _, pid := range l.ProbeIDs {
						used[pid] = true
					}
				}
			}
		}
		out := make([]probeSummary, 0, len(list))
		for _, p := range list {
			if used == nil || used[p.ID] {
				out = append(out, s.probeSummaryOf(p))
			}
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	counts, err := s.store.ProbeMonitorCounts(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	channels, err := s.store.AllProbeNotificationIDs(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := make([]probeAdminView, len(list))
	for i, p := range list {
		out[i] = s.probeAdminOf(p, counts[p.ID], channels[p.ID])
	}
	writeJSON(w, http.StatusOK, out)
}

// validateProbeName adı doğrular; hata varsa yanıtı yazar.
func (s *Server) validateProbeName(w http.ResponseWriter, r *http.Request, name, kind string, id int64) bool {
	if name == "" || utf8.RuneCountInString(name) > 100 {
		writeError(w, http.StatusBadRequest, "Ad 1-100 karakter olmalı")
		return false
	}
	taken, err := s.store.ProbeNameTaken(r.Context(), name, kind, id)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	if taken {
		writeError(w, http.StatusConflict, "Bu adda bir "+kindNoun(kind)+" zaten var")
		return false
	}
	return true
}

// kindNoun ajan türünün Türkçe adı (mesajlar için).
func kindNoun(kind string) string {
	if kind == store.ProbeKindServer {
		return "sunucu"
	}
	return "kontrol noktası"
}

// probeSetup yeni token'ı ve kurulum komutlarını içeren yanıt. Kontrol
// noktasına docker_command, sunucuya host'u gören docker_agent, systemd ve
// windows komutları (servers.go) verilir.
func (s *Server) probeSetup(r *http.Request, p store.Probe, token string, monitors int) map[string]any {
	server := s.BaseURL
	if server == "" {
		scheme := "http"
		if isHTTPS(r) {
			scheme = "https"
		}
		server = scheme + "://" + r.Host
	}
	if !validInstallServer(server) {
		// Kurulum komutuna gömülecek adres güvenli karakter kümesinde değilse
		// kabuk kaçışına karşı yalnızca şema+host'a indirgenir.
		if u, err := url.Parse(server); err == nil && u.Host != "" {
			server = u.Scheme + "://" + u.Host
		}
		if !validInstallServer(server) {
			server = "https://SUNUCU-ADRESINIZ"
		}
	}
	lang := userLang(r)
	out := map[string]any{"probe": s.probeAdminOf(p, monitors, nil), "token": token, "server_url": server}
	if p.Kind == store.ProbeKindServer {
		dockerAgent, systemd, windows := s.serverSetupCommands(server, token)
		out["docker_agent"], out["systemd"], out["windows"] =
			localizeScript(lang, dockerAgent), localizeScript(lang, systemd), localizeScript(lang, windows)
		return out
	}
	amd, arm := s.linuxSHAs()
	out["docker_command"] = localizeScript(lang, dockerInstallCommand(probeEnvPath, installEnvLines(server, token),
		"docker run -d --name uptime-probe --restart unless-stopped "+probeHardenFlags, s.ProbeImage, "uptime-probe-bin", amd, arm))
	return out
}

// agentPlatformRe ?os= ve ?arch= değerleri (dosya adına girdiği için sıkı).
var agentPlatformRe = regexp.MustCompile(`^[a-z0-9]{1,16}$`)

// probeBinary kontrol noktasına bu sunucuda çalışan programın kendisini verir
// (aynı ikili "uptime probe" ile kontrol noktası olarak çalışır). Başka bir
// platform istenirse (?os=windows&arch=amd64) AgentDir'deki
// uptime-<os>-<arch>[.exe] dosyası verilir; eksik parametre sunucunun kendi
// değeridir. Token istemez (herkese açık, IP başına hız sınırlı): programın
// içinde sır yoktur; kurulumun bütünlüğünü ve kaynağını komuta gömülü SHA-256
// doğrular. Böylece kurulum ve yeniden başlatmada token hiçbir sürecin
// argümanlarında (curl/wget -H …) görünmez.
func (s *Server) probeBinary(w http.ResponseWriter, r *http.Request) {
	goos, goarch := r.URL.Query().Get("os"), r.URL.Query().Get("arch")
	if goos == "" {
		goos = runtime.GOOS
	}
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	if !agentPlatformRe.MatchString(goos) || !agentPlatformRe.MatchString(goarch) {
		writeError(w, http.StatusBadRequest, "Geçersiz platform (ör. os=windows&arch=amd64)")
		return
	}
	name, own := "uptime", goos == runtime.GOOS && goarch == runtime.GOARCH
	file := "uptime-" + goos + "-" + goarch
	if goos == "windows" {
		name, file = "uptime.exe", file+".exe"
	}
	var path string
	if own {
		exe, err := os.Executable()
		if err != nil {
			s.log.Error("program yolu bulunamadı", "hata", err)
			writeError(w, http.StatusInternalServerError, "Program dosyası bulunamadı")
			return
		}
		path = exe
	} else if s.AgentDir != "" {
		path = filepath.Join(s.AgentDir, file)
	}
	f, err := os.Open(path)
	if err != nil && !own && (path == "" || errors.Is(err, os.ErrNotExist)) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("Bu sunucuda %s/%s için ajan programı yok. Resmi Docker imajı Windows (amd64) "+
			"programını içerir; kendiniz derlediyseniz programı AGENT_DIR klasörüne %s adıyla koyun.", goos, goarch, file))
		return
	}
	if err != nil {
		s.log.Error("program dosyası açılamadı", "dosya", path, "hata", err)
		writeError(w, http.StatusInternalServerError, "Program dosyası açılamadı")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Program dosyası okunamadı")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	h.Set("X-Uptime-Version", s.version)
	h.Set("X-Uptime-Platform", goos+"/"+goarch)
	if sum := s.agentBinarySHA256(goos, goarch); sum != "" {
		h.Set("X-Uptime-SHA256", sum)
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// createProbe kontrol noktası ekler (POST /api/probes).
func (s *Server) createProbe(w http.ResponseWriter, r *http.Request) {
	s.createAgent(w, r, store.ProbeKindLocation)
}

// createServer takip edilecek sunucu ekler (POST /api/servers).
func (s *Server) createServer(w http.ResponseWriter, r *http.Request) {
	s.createAgent(w, r, store.ProbeKindServer)
}

func (s *Server) createAgent(w http.ResponseWriter, r *http.Request, kind string) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !s.validateProbeName(w, r, in.Name, kind, 0) {
		return
	}
	n, err := s.store.CountProbes(r.Context(), kind)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if n >= probeMaxCount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("En fazla %d %s eklenebilir", probeMaxCount, kindNoun(kind)))
		return
	}
	token := newProbeToken()
	p := store.Probe{Kind: kind, Name: in.Name, Active: true, IPLock: true, CreatedAt: s.now().Unix(), Hash: hashToken(token), TokenPrefix: token[:probeTokenShown]}
	if err := s.store.CreateProbe(r.Context(), &p); err != nil {
		if store.IsUniqueViolation(err) {
			writeError(w, http.StatusConflict, "Token üretilemedi, tekrar deneyin")
			return
		}
		s.dbError(w, err)
		return
	}
	// Sunucu uyarıları için varsayılan bildirim kanalları hemen seçili gelir.
	if kind == store.ProbeKindServer {
		if err := s.store.AttachDefaultProbeNotifications(r.Context(), p.ID); err != nil {
			s.log.Error("varsayılan bildirim kanalları bağlanamadı", "hata", err)
		}
	}
	s.log.Info(kindNoun(kind)+" eklendi", "ad", p.Name)
	action := "probe.create"
	if kind == store.ProbeKindServer {
		action = "server.create"
	}
	s.audit(r, store.User{}, action, "probe", p.ID, p.Name, "")
	writeJSON(w, http.StatusCreated, s.probeSetup(r, p, token, 0))
}

func (s *Server) updateProbe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Etkinlik ve metrik ayarı ajanın iş listesi yanıtını etkiler: bekleyen
	// uzun yoklama değişiklikten sonra hemen yanıtlanır.
	defer s.engine.JobsChanged()
	var in struct {
		Name    string `json:"name"`
		Active  *bool  `json:"active"`
		Metrics *bool  `json:"metrics"`  // sunucu metrikleri; yoksa değişmez
		IPLock  *bool  `json:"ip_lock"`  // IP kilidi; yoksa değişmez
		ResetIP bool   `json:"reset_ip"` // kilitli IP'yi sıfırla (yeniden sabitlensin)
		// Kontrol noktası: çevrimdışı bildirimi ve kanalları; yoksa değişmez.
		NotifyOffline   *bool    `json:"notify_offline"`
		NotificationIDs *[]int64 `json:"notification_ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !s.validateProbeName(w, r, in.Name, old.Kind, id) {
		return
	}
	var channelIDs []int64
	if in.NotificationIDs != nil && old.Kind == store.ProbeKindLocation {
		channelIDs = slices.Compact(slices.Sorted(slices.Values(*in.NotificationIDs)))
		if channelIDs == nil {
			channelIDs = []int64{} // boş liste: tüm kanallar kaldırılır
		}
		if _, err := s.notificationIDs(r, &channelIDs); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	active := old.Active
	if in.Active != nil {
		active = *in.Active
	}
	if err := s.store.UpdateProbe(r.Context(), id, in.Name, active); err != nil {
		s.dbError(w, err)
		return
	}
	metricsOn := old.Metrics
	if in.Metrics != nil && *in.Metrics != old.Metrics && old.Kind == store.ProbeKindServer {
		metricsOn = *in.Metrics
		if err := s.store.SetProbeMetrics(r.Context(), id, metricsOn); err != nil {
			s.dbError(w, err)
			return
		}
	}
	var changes []string
	if in.IPLock != nil && *in.IPLock != old.IPLock {
		if err := s.store.SetProbeIPLock(r.Context(), id, *in.IPLock); err != nil {
			s.dbError(w, err)
			return
		}
		changes = append(changes, map[bool]string{true: "IP kilidi açıldı", false: "IP kilidi kapatıldı"}[*in.IPLock])
	} else if in.ResetIP && old.LockedIP != "" {
		if err := s.store.ResetProbeIP(r.Context(), id); err != nil {
			s.dbError(w, err)
			return
		}
		changes = append(changes, "IP kilidi sıfırlandı")
	}
	if in.NotifyOffline != nil && *in.NotifyOffline != old.NotifyOffline && old.Kind == store.ProbeKindLocation {
		if err := s.store.SetProbeNotifyOffline(r.Context(), id, *in.NotifyOffline); err != nil {
			s.dbError(w, err)
			return
		}
		changes = append(changes, map[bool]string{true: "çevrimdışı bildirimi açıldı", false: "çevrimdışı bildirimi kapatıldı"}[*in.NotifyOffline])
	}
	if channelIDs != nil {
		oldIDs, err := s.store.ProbeNotificationIDs(r.Context(), id)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if !sameIDs(oldIDs, channelIDs) {
			if err := s.store.SetProbeNotifications(r.Context(), id, channelIDs); err != nil {
				s.dbError(w, err)
				return
			}
			changes = append(changes, "bildirim kanalları değişti")
		}
	}
	if in.Name != old.Name {
		changes = append(changes, "ad: "+old.Name+" → "+in.Name)
	}
	if active != old.Active {
		changes = append(changes, map[bool]string{true: "etkinleştirildi", false: "devre dışı bırakıldı"}[active])
	}
	if active != old.Active {
		// Etkinlik değişti: eski bağlantı kaydı ve kopukluk işareti kalmasın;
		// yeniden etkinleşen ajan ilk isteğiyle normal akışa döner.
		s.probeConns.forget(id)
		s.engine.SetProbeConnected(id, true)
	}
	if len(changes) > 0 {
		// Ad mesajlarda, etkinlik konum listesinde kullanılır: monitörler yeniden yüklenir.
		s.reloadProbeMonitors(r.Context(), id, nil)
	}
	if metricsOn != old.Metrics {
		changes = append(changes, map[bool]string{true: "metrik toplama açıldı", false: "metrik toplama kapatıldı"}[metricsOn])
	}
	// Sunucu ekranı yalnızca sunucuları dinler; kontrol noktası oraya yayınlanmaz.
	if old.Kind == store.ProbeKindServer {
		if (active && !old.Active) || (metricsOn && !old.Metrics) {
			s.servers.Arm(id)
		}
		if len(changes) > 0 {
			s.servers.Publish(r.Context(), id)
		}
	}
	s.audit(r, store.User{}, "probe.update", "probe", id, in.Name, strings.Join(changes, ", "))
	s.respondProbe(w, r, id)
}

func (s *Server) respondProbe(w http.ResponseWriter, r *http.Request, id int64) {
	p, err := s.store.GetProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	ids, err := s.store.ProbeMonitorIDs(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	channels, err := s.store.ProbeNotificationIDs(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.probeAdminOf(p, len(ids), channels))
}

// reloadProbeMonitors kontrol noktasına atanmış (veya ids ile verilen)
// monitörleri yeniden başlatır.
func (s *Server) reloadProbeMonitors(ctx context.Context, probeID int64, ids []int64) {
	if ids == nil {
		var err error
		if ids, err = s.store.ProbeMonitorIDs(ctx, probeID); err != nil {
			s.log.Error("kontrol noktasının monitörleri okunamadı", "hata", err)
			return
		}
	}
	for _, id := range ids {
		if err := s.engine.Reload(ctx, id); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.log.Error("monitör yeniden başlatılamadı", "id", id, "hata", err)
		}
		// Açık ayrıntı sayfaları konum listesini hemen tazelesin (silinen,
		// devre dışı bırakılan konum sonraki kontrolü beklemeden kalksın).
		if s.hub != nil {
			s.hub.Publish("locations", map[string]any{"monitor_id": id, "changed": true})
		}
	}
}

func (s *Server) deleteProbe(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.store.GetProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	affected, err := s.store.DeleteProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.probePolls.forget(id)
	s.probeConns.forget(id)
	s.engine.SetProbeConnected(id, true) // kayıt kalmasın
	s.engine.JobsChanged()               // bekleyen isteği yanıtlanır (401): ajan hemen durur
	s.reloadProbeMonitors(r.Context(), id, affected)
	s.servers.Forget(id)
	s.log.Info("kontrol noktası silindi", "kontrol_noktasi", p.Name)
	detail := ""
	if len(affected) > 0 {
		detail = fmt.Sprintf("%d monitörden çıkarıldı", len(affected))
	}
	s.audit(r, store.User{}, "probe.delete", "probe", id, p.Name, detail)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) regenerateProbeToken(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	token := newProbeToken()
	if err := s.store.SetProbeToken(r.Context(), id, hashToken(token), token[:probeTokenShown]); err != nil {
		s.dbError(w, err)
		return
	}
	s.engine.JobsChanged() // eski token'la bekleyen istek hemen 401 alır
	p, err := s.store.GetProbe(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	ids, err := s.store.ProbeMonitorIDs(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "probe.token", "probe", id, p.Name, "")
	writeJSON(w, http.StatusOK, s.probeSetup(r, p, token, len(ids)))
}

// Monitör konumları ------------------------------------------------------------------------

type locationsView struct {
	store.LocationSetup
	// Supported: monitör tipi kontrol noktasında çalıştırılabilir mi (push ve grup hayır).
	Supported bool `json:"supported"`
	// Locations: konumların canlı durumu (yalnızca çok konumlu, çalışan monitörde dolu).
	Locations []engine.LocationStatus `json:"locations"`
}

func (s *Server) locationsOf(ctx context.Context, m store.Monitor) (locationsView, error) {
	setup, err := s.store.MonitorLocations(ctx, m.ID)
	if err != nil {
		return locationsView{}, err
	}
	v := locationsView{LocationSetup: setup, Supported: engine.RemoteCapable(m.Type), Locations: []engine.LocationStatus{}}
	if st, ok := s.engine.LocationStatuses(m.ID); ok {
		v.Locations = st
	}
	return v, nil
}

// locationStatusCode konum durum metnini beat durum koduna çevirir (viewerMessage için).
func locationStatusCode(status string) int {
	switch status {
	case "up":
		return store.StatusUp
	case "down":
		return store.StatusDown
	}
	return store.StatusPending
}

func (s *Server) getMonitorLocations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFrom(r)
	m, err := s.store.GetMonitor(r.Context(), id)
	if err == nil && !visibleTo(u).can(id) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	v, err := s.locationsOf(r.Context(), m)
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Konum mesajları da kontrol hata metinleridir (adres, kimlik bilgisi
	// içerebilir); izleyiciye diğer mesajlar gibi temizlenmiş gider ve herkese
	// yanıt dilinde (Türkçe saklanır). Dilim motorun yayınladığı ortak
	// kopyadır; değiştirmeden önce kopyalanır.
	if lang := responseLang(w); !canSeeConfig(u) || lang != i18n.TR {
		v.Locations = slices.Clone(v.Locations)
		for i, l := range v.Locations {
			v.Locations[i].Message = displayMessage(u, lang, m.Type, locationStatusCode(l.Status), l.Message)
			v.Locations[i].Name = locationName(lang, l.Name)
		}
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) putMonitorLocations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in struct {
		IncludeLocal  *bool   `json:"include_local"`
		ProbeIDs      []int64 `json:"probe_ids"`
		DownWhen      string  `json:"down_when"`
		NotifyPartial bool    `json:"notify_partial"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	setup := store.LocationSetup{IncludeLocal: in.IncludeLocal == nil || *in.IncludeLocal, DownWhen: in.DownWhen, ProbeIDs: []int64{}, NotifyPartial: in.NotifyPartial}
	if setup.DownWhen == "" {
		setup.DownWhen = store.DownWhenAny
	}
	for _, pid := range in.ProbeIDs {
		if !slices.Contains(setup.ProbeIDs, pid) {
			setup.ProbeIDs = append(setup.ProbeIDs, pid)
		}
	}
	slices.Sort(setup.ProbeIDs)
	switch {
	case !store.ValidDownWhen(setup.DownWhen):
		writeError(w, http.StatusBadRequest, "Kesinti kuralı any, majority veya all olmalı")
		return
	case !setup.IncludeLocal && len(setup.ProbeIDs) == 0:
		writeError(w, http.StatusBadRequest, "En az bir konum seçin")
		return
	case len(setup.ProbeIDs) > probeMaxPerMon:
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Bir monitöre en fazla %d kontrol noktası atanabilir", probeMaxPerMon))
		return
	case !engine.RemoteCapable(m.Type) && setup.Configured():
		writeError(w, http.StatusBadRequest, "Push ve grup monitörleri kontrol noktalarında çalıştırılamaz")
		return
	}
	probes, err := s.store.ProbesByIDs(r.Context(), setup.ProbeIDs)
	if err != nil {
		s.dbError(w, err)
		return
	}
	names := []string{}
	if setup.IncludeLocal {
		names = append(names, engine.LocalName)
	}
	for _, pid := range setup.ProbeIDs {
		p, ok := probes[pid]
		if !ok || p.Kind != store.ProbeKindLocation {
			writeError(w, http.StatusBadRequest, "Seçilen kontrol noktası bulunamadı")
			return
		}
		names = append(names, p.Name)
	}
	if err := s.store.SetMonitorLocations(r.Context(), id, setup); err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.engine.Reload(r.Context(), id); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.log.Error("monitör yeniden başlatılamadı", "monitor", m.Name, "hata", err)
	}
	s.audit(r, store.User{}, "monitor.locations", "monitor", id, m.Name,
		"konumlar: "+strings.Join(names, ", ")+"; kural: "+i18n.DownWhenLabel(setup.DownWhen)+
			map[bool]string{true: "; konum kesintisinde bildirim", false: ""}[setup.NotifyPartial])
	v, err := s.locationsOf(r.Context(), m)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

// Kontrol noktası protokolü ---------------------------------------------------------------

// probeJob kontrol noktasına giden iş: monitörün tam ayarı (şifreler dahil;
// kontrol noktası bunlar olmadan kontrol yapamaz). Süreler saniye cinsinden.
type probeJob struct {
	ID            int64           `json:"id"`
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Interval      int             `json:"interval"`
	RetryInterval int             `json:"retry_interval"`
	Timeout       int             `json:"timeout"`
	Config        json.RawMessage `json:"config"`
	// PhaseMs monitörün kontrol ızgarasındaki kayması (bkz. schedule): ajan
	// ana sunucuyla aynı anlarda kontrol eder. Eski ajanlar yok sayar.
	PhaseMs int64 `json:"phase_ms"`
	// MaxRetries tekrar deneme hakkı: ajan art arda bu kadar başarısızlıktan
	// sonra (konum "çalışmıyor") normal aralığa döner, ana sunucu gibi.
	MaxRetries int `json:"max_retries"`
}

// assignedJobs kontrol noktasının çalıştırabileceği aktif monitörleri döner.
func (s *Server) assignedJobs(ctx context.Context, probeID int64) ([]store.Monitor, error) {
	list, err := s.store.ProbeJobs(ctx, probeID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(list, func(m store.Monitor) bool { return !engine.RemoteCapable(m.Type) }), nil
}

// Uzun yoklama (long-poll) ------------------------------------------------------------
//
// Ajan iş listesini GET /api/probe/jobs ile alır. Elindeki liste güncelse istek
// en fazla probeHold bekletilir; bu sırada iş listesini etkileyen bir
// değişiklik olursa (engine.JobsChanged: monitör ekleme/düzenleme/durdurma,
// konum ayarı, ajan ayarı) hemen yanıtlanır. Yanıttaki poll_after 1'dir: ajan
// yanıtı alınca 1 sn sonra yeniden sorar ve yine bekletilir. Böylece yeni monitör
// ajana ~1 sn içinde ulaşır, boşta ajan dakikada ~3 istek atar.
//
// "Liste güncel mi?" sorusu:
//   - yeni ajanlar ?since=<son aldığı sürüm> gönderir: sürüm güncelse beklet,
//     değilse (veya sunucu yeniden başladıysa) hemen yanıtla;
//   - eski ajanlar (sürüm göndermez, kurulu sürümleri sabit) için sunucu, ajana
//     en son hangi sürümü verdiğini bellekte tutar (probePolls). Son teslimden
//     sonra değişiklik yoksa ve önceki istek probeFreshFor içindeyse bekletilir;
//     aksi halde (ilk istek, uzun ara — ör. ajan yeniden başladı) hemen yanıtlanır.
//     Hızlı yeniden başlayan eski ajanın ilk isteği en fazla probeHold bekler
//     (eskiden 30 sn'lik yoklama aralığından kısa).
//
// Bekletilen istek kimlik doğrulaması, IP kilidi ve istek sınırından (probeOnly)
// geçmiştir ve sınıra bir kez sayılır. Uyanınca kontrol noktası yeniden okunur:
// bu sırada devre dışı bırakılan/silinen/token'ı yenilenen ajana liste
// verilmez. Kapanışta istek context'i iptal olur (BaseContext): bekleme hemen
// biter, güncel liste verilir.

const (
	// probeHold bekletme süresi: ajanın HTTP zaman aşımından (30 sn), sunucunun
	// okuma (30 sn) / yazma (60 sn) sınırından, Cloudflare'in 100 sn'sinden ve
	// çevrimdışı sayılma süresinden (engine.ProbeOfflineAfter, 90 sn) epey kısa.
	probeHold = 20 * time.Second
	// probeFreshFor eski ajanın önceki iş listesi isteği bundan eskiyse elindeki
	// liste bilinmiyor sayılır (yeniden başlamış olabilir), hemen yanıtlanır.
	probeFreshFor = 60 * time.Second
	// probeCoalesce uyandıran değişiklikten sonra kısa bekleme: art arda gelen
	// değişiklikler (toplu işlem, içe aktarma) tek yanıtta birleşir.
	probeCoalesce = 250 * time.Millisecond
	// probeLongPollAfter uzun yoklamada ajanın bir sonraki isteğe kadar beklemesi
	// (sn). Eski ajanlar 0 veya eksik değeri 30 sayar; 1 güvenlidir.
	probeLongPollAfter = 1
)

// probePolls ajan başına son teslim edilen iş listesi sürümü ve zamanı (yalnızca
// bellekte; sunucu yeniden başlayınca boşalır, ilk istekler hemen yanıtlanır).
// probeReconnectWait sunucu iş listesi isteğini yanıtladıktan sonra ajanın
// yeniden bağlanması için beklenen süre (ajan poll_after=1 ile ~1 sn içinde
// döner). Bu sürede yeni istek gelmezse bağlantı kopmuş sayılır.
const probeReconnectWait = 5 * time.Second

// probeConns ajan başına açık iş listesi istekleri.
type probeConns struct {
	mu sync.Mutex
	m  map[int64]*probeConn
}

type probeConn struct {
	open int
	gen  uint64 // her yeni istekte artar: bekleyen kontrol yeni bağlantıyı görür
}

func newProbeConns() *probeConns { return &probeConns{m: map[int64]*probeConn{}} }

func (pc *probeConns) begin(id int64) {
	pc.mu.Lock()
	c := pc.m[id]
	if c == nil {
		c = &probeConn{}
		pc.m[id] = c
	}
	c.open++
	c.gen++
	pc.mu.Unlock()
}

// end isteğin bittiğini kaydeder; açık istek kaldı mı ve son kuşak.
func (pc *probeConns) end(id int64) (open int, gen uint64) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	c := pc.m[id]
	if c == nil {
		return 0, 0
	}
	c.open = max(c.open-1, 0)
	return c.open, c.gen
}

// idle gen'den beri yeni istek gelmedi ve açık istek yok.
func (pc *probeConns) idle(id int64, gen uint64) bool {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	c := pc.m[id]
	return c != nil && c.open == 0 && c.gen == gen
}

func (pc *probeConns) forget(id int64) {
	pc.mu.Lock()
	delete(pc.m, id)
	pc.mu.Unlock()
}

// probeRequestDone iş listesi isteği bitince çağrılır. Ajan bağlantıyı kendisi
// kopardıysa (durduruldu, silindi, çöktü) konumları hemen "sonuç yok" olur.
// Sunucu normal yanıt verdiyse ajan ~1 sn içinde yeniden bağlanır;
// probeReconnectWait içinde gelmezse kopmuş sayılır.
func (s *Server) probeRequestDone(id int64, ctx context.Context) {
	open, gen := s.probeConns.end(id)
	if open > 0 {
		return
	}
	if ctx.Err() != nil {
		s.engine.SetProbeConnected(id, false)
		return
	}
	wait := s.probeReconnect
	if wait <= 0 {
		wait = probeReconnectWait
	}
	time.AfterFunc(wait, func() {
		if s.probeConns.idle(id, gen) {
			s.engine.SetProbeConnected(id, false)
		}
	})
}

type probePolls struct {
	mu sync.Mutex
	m  map[int64]probePollMark
}

type probePollMark struct {
	version int64
	at      time.Time
}

func newProbePolls() *probePolls { return &probePolls{m: map[int64]probePollMark{}} }

func (pp *probePolls) get(id int64) (probePollMark, bool) {
	pp.mu.Lock()
	defer pp.mu.Unlock()
	m, ok := pp.m[id]
	return m, ok
}

func (pp *probePolls) set(id, version int64, at time.Time) {
	pp.mu.Lock()
	pp.m[id] = probePollMark{version, at}
	pp.mu.Unlock()
}

func (pp *probePolls) forget(id int64) {
	pp.mu.Lock()
	delete(pp.m, id)
	pp.mu.Unlock()
}

// shouldHold ajanın elindeki iş listesi güncel mi (istek bekletilebilir mi)?
func (s *Server) shouldHold(r *http.Request, probeID, version int64, now time.Time) bool {
	if v := r.URL.Query().Get("since"); v != "" {
		since, err := strconv.ParseInt(v, 10, 64)
		return err == nil && since == version
	}
	mark, ok := s.probePolls.get(probeID)
	return ok && mark.version == version && now.Sub(mark.at) <= probeFreshFor
}

// holdJobs değişiklik, süre dolması veya iptal olana kadar bekler. Beklemeden
// sonra ajan hâlâ geçerliyse güncel kaydını döner; değilse yanıtı yazar ve
// ok=false döner.
func (s *Server) holdJobs(w http.ResponseWriter, r *http.Request, p store.Probe, changed <-chan struct{}) (store.Probe, bool) {
	hold := s.probeHold
	if hold <= 0 {
		hold = probeHold
	}
	// Bekleme sunucunun genel okuma/yazma sınırlarına takılmasın (okuma sınırı
	// dolunca net/http isteğin context'ini iptal eder). Desteklenmezse (testte
	// ResponseRecorder) yok sayılır; probeHold bu sınırlardan zaten kısa.
	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Now().Add(hold + 30*time.Second))
	rc.SetWriteDeadline(time.Now().Add(hold + 30*time.Second))
	t := time.NewTimer(hold)
	defer t.Stop()
	select {
	case <-changed:
		// Art arda gelen değişiklikler tek yanıtta birleşsin.
		select {
		case <-time.After(probeCoalesce):
		case <-r.Context().Done():
		}
	case <-t.C:
	case <-r.Context().Done():
	}
	// İptal (ajan bağlantıyı kapattı veya sunucu kapanıyor): kısa süreli ayrı
	// bir context ile yanıtlanır; bağlantı kapandıysa yazma sessizce başarısız olur.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	cur, err := s.store.GetProbe(ctx, p.ID)
	switch {
	case errors.Is(err, store.ErrNotFound) || (err == nil && cur.Hash != p.Hash):
		writeError(w, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı")
		return p, false
	case err != nil:
		s.dbError(w, err)
		return p, false
	case !cur.Active:
		writeError(w, http.StatusForbidden, "Kontrol noktası devre dışı")
		return p, false
	}
	return cur, true
}

func (s *Server) probeJobs(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	if p.Kind == store.ProbeKindLocation && r.URL.Query().Has("since") {
		// Uzun yoklama yapan ajan: bağlantısı kopunca konumları beklemeden
		// "sonuç yok" sayılır (eski ajanlar since göndermez, 3 aralık kuralı).
		s.probeConns.begin(p.ID)
		s.engine.SetProbeConnected(p.ID, true)
		defer s.probeRequestDone(p.ID, r.Context())
	}
	// Sürüm listeden ÖNCE okunur: okuma sırasında gelen değişiklik kaçmaz.
	version, changed := s.engine.JobsVersion()
	if s.shouldHold(r, p.ID, version, s.now()) {
		var ok bool
		if p, ok = s.holdJobs(w, r, p, changed); !ok {
			return
		}
		version, _ = s.engine.JobsVersion()
	}
	ctx := r.Context()
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	// Sunucu ajanı monitör kontrolü yapmaz (konum olarak atanamaz).
	var list []store.Monitor
	if p.Kind == store.ProbeKindLocation {
		var err error
		if list, err = s.assignedJobs(ctx, p.ID); err != nil {
			s.dbError(w, err)
			return
		}
	}
	jobs := make([]probeJob, len(list))
	for i, m := range list {
		jobs[i] = probeJob{ID: m.ID, Name: m.Name, Type: m.Type, Interval: m.Interval,
			RetryInterval: m.RetryInterval, Timeout: m.Timeout, Config: m.Config, PhaseMs: schedule.PhaseMs(m.ID), MaxRetries: m.MaxRetries}
	}
	s.probePolls.set(p.ID, version, s.now())
	writeJSON(w, http.StatusOK, map[string]any{
		"probe":      map[string]any{"id": p.ID, "name": p.Name},
		"poll_after": probeLongPollAfter,
		// Listenin sürümü: yeni ajanlar sonraki istekte ?since= ile geri gönderir.
		"version": version,
		"jobs":    jobs,
		// Sunucu metriklerinin örnek aralığı (sn); 0: metrik gönderme.
		"metrics_interval": servers.IntervalFor(p),
		// Sunucunun saati (unix ms): ajan kontrol ızgarasını buna göre kurar,
		// iki makinenin saati farklı olsa da kontroller aynı anda yapılır.
		"server_time": time.Now().UnixMilli(),
		// Kontrol isteklerinin User-Agent'ı: tüm konumlar aynı değerle gider
		// (güvenlik duvarında tek kuralla izin verilebilsin).
		"user_agent": check.UserAgent(),
	})
}

// probeResultIn kontrol noktasından gelen tek sonuç. Zamanlar unix milisaniye
// ve kontrol noktasının saatine göredir; sunucu, isteğin sent_at alanıyla
// farkı alarak kendi saatine çevirir (iki makinenin saati farklı olabilir).
type probeResultIn struct {
	MonitorID    int64  `json:"monitor_id"`
	Time         int64  `json:"time"`
	Up           bool   `json:"up"`
	PingMs       int64  `json:"ping_ms"`
	Message      string `json:"message"`
	CertNotAfter int64  `json:"cert_not_after"` // unix saniye; 0: yok
	CertIssuer   string `json:"cert_issuer"`
	// Detail başarısız HTTP kontrolünün isteği ve yanıtı (isteğe bağlı; eski
	// kontrol noktaları göndermez). Sunucuda yeniden sınırlanır ve maskelenir.
	Detail *check.Detail `json:"detail"`
}

type probeRejected struct {
	Index     int    `json:"index"`
	MonitorID int64  `json:"monitor_id"`
	Error     string `json:"error"`
}

func (s *Server) probeResults(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	if p.Kind != store.ProbeKindLocation {
		// Sunucu ajanına monitör atanamaz; yine de sonuç kabul edilmez.
		writeError(w, http.StatusForbidden, "Sunucu ajanı kontrol sonucu gönderemez")
		return
	}
	// Bilinmeyen alanlar kabul edilir: yeni sürüm bir kontrol noktası eski bir
	// sunucuya da sonuç gönderebilsin.
	var in struct {
		SentAt  int64           `json:"sent_at"`
		Results []probeResultIn `json:"results"`
	}
	body := http.MaxBytesReader(w, r.Body, probeMaxBody)
	if err := json.NewDecoder(body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
			return
		}
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi: "+err.Error())
		return
	}
	io.Copy(io.Discard, body)
	if len(in.Results) > probeMaxBatch {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("Tek istekte en fazla %d sonuç gönderilebilir", probeMaxBatch))
		return
	}
	jobs, err := s.assignedJobs(r.Context(), p.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	assigned := make(map[int64]store.Monitor, len(jobs))
	for _, m := range jobs {
		assigned[m.ID] = m
	}
	now := s.now()
	sentAt := in.SentAt
	if sentAt <= 0 {
		sentAt = now.UnixMilli()
	}
	accepted := make([]engine.ProbeResult, 0, len(in.Results))
	rejected := []probeRejected{}
	for i, res := range in.Results {
		mon, ok := assigned[res.MonitorID]
		if !ok {
			rejected = append(rejected, probeRejected{i, res.MonitorID, "Monitör bu kontrol noktasına atanmamış"})
			continue
		}
		age := time.Duration(max(sentAt-res.Time, 0)) * time.Millisecond
		if res.Time <= 0 || age > probeMaxAge {
			rejected = append(rejected, probeRejected{i, res.MonitorID, "Sonuç zamanı geçersiz veya çok eski"})
			continue
		}
		cr := check.Result{Up: res.Up, PingMs: res.PingMs, Message: cleanProbeText(res.Message, probeMaxMessage)}
		if cr.PingMs < 0 {
			cr.PingMs = -1
		}
		cr.PingMs = min(cr.PingMs, 600_000)
		if res.CertNotAfter > 0 {
			cr.Cert = &check.CertInfo{NotAfter: time.Unix(res.CertNotAfter, 0), Issuer: cleanProbeText(res.CertIssuer, 200)}
		}
		if res.Detail != nil && !cr.Up {
			// Önce maskele, sonra kırp: kırpma sınırına denk gelen gizli değerin
			// bir kısmı açıkta kalmasın.
			check.MaskDetail(mon.Type, mon.Config, res.Detail)
			res.Detail.Sanitize()
			cr.Detail = res.Detail
		}
		accepted = append(accepted, engine.ProbeResult{MonitorID: res.MonitorID, Time: now.Add(-age), Result: cr})
	}
	// Aynı monitörün sonuçları zamana göre sıralı işlensin.
	slices.SortStableFunc(accepted, func(a, b engine.ProbeResult) int { return a.Time.Compare(b.Time) })
	s.engine.ProbeResults(p.ID, accepted)
	writeJSON(w, http.StatusOK, map[string]any{"accepted": len(accepted), "rejected": rejected})
}

// cleanProbeText geçersiz UTF-8 ve kontrol karakterlerini temizler, kısaltır.
func cleanProbeText(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > n {
		s = string([]rune(s)[:n]) + "…"
	}
	return s
}
