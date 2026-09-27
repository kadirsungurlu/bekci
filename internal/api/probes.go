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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
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
		token, ok := bearerProbeToken(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı")
			return
		}
		p, err := s.store.ProbeByTokenHash(r.Context(), hashToken(token))
		if errors.Is(err, store.ErrNotFound) {
			s.log.Warn("geçersiz kontrol noktası token'ı", "ip", clientIP(r))
			writeError(w, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı")
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
		now := s.now()
		if ok, wait := s.probeRL.allow(p.ID, now); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeError(w, http.StatusTooManyRequests, "Çok fazla istek; biraz sonra tekrar deneyin")
			return
		}
		ip, version := clientIP(r), cleanVersion(r.Header.Get("X-Probe-Version"))
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
	TokenPrefix  string `json:"token_prefix"`
	CreatedAt    int64  `json:"created_at"`
	LastIP       string `json:"last_ip"`
	Version      string `json:"version"`
	MonitorCount int    `json:"monitor_count"`
}

func (s *Server) probeSummaryOf(p store.Probe) probeSummary {
	return probeSummary{ID: p.ID, Name: p.Name, Active: p.Active, Online: engine.ProbeOnline(p, s.now()), LastSeenAt: p.LastSeenAt}
}

func (s *Server) probeAdminOf(p store.Probe, monitors int) probeAdminView {
	return probeAdminView{
		probeSummary: s.probeSummaryOf(p), TokenPrefix: p.TokenPrefix, CreatedAt: p.CreatedAt,
		LastIP: p.LastIP, Version: p.Version, MonitorCount: monitors,
	}
}

func (s *Server) listProbes(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProbes(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if userFrom(r).Role != store.RoleAdmin {
		out := make([]probeSummary, len(list))
		for i, p := range list {
			out[i] = s.probeSummaryOf(p)
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	counts, err := s.store.ProbeMonitorCounts(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := make([]probeAdminView, len(list))
	for i, p := range list {
		out[i] = s.probeAdminOf(p, counts[p.ID])
	}
	writeJSON(w, http.StatusOK, out)
}

// validateProbeName adı doğrular; hata varsa yanıtı yazar.
func (s *Server) validateProbeName(w http.ResponseWriter, r *http.Request, name string, id int64) bool {
	if name == "" || utf8.RuneCountInString(name) > 100 {
		writeError(w, http.StatusBadRequest, "Ad 1-100 karakter olmalı")
		return false
	}
	taken, err := s.store.ProbeNameTaken(r.Context(), name, id)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	if taken {
		writeError(w, http.StatusConflict, "Bu adda bir kontrol noktası zaten var")
		return false
	}
	return true
}

// probeSetup yeni token'ı ve kurulum komutunu içeren yanıt.
func (s *Server) probeSetup(r *http.Request, p store.Probe, token string, monitors int) map[string]any {
	server := s.BaseURL
	if server == "" {
		scheme := "http"
		if isHTTPS(r) {
			scheme = "https"
		}
		server = scheme + "://" + r.Host
	}
	image := s.ProbeImage
	if image == "" {
		image = "uptime"
	}
	cmd := fmt.Sprintf("docker run -d --name uptime-probe --restart unless-stopped -e PROBE_SERVER=%s -e PROBE_TOKEN=%s %s probe",
		server, token, image)
	return map[string]any{"probe": s.probeAdminOf(p, monitors), "token": token, "server_url": server, "docker_command": cmd}
}

func (s *Server) createProbe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !s.validateProbeName(w, r, in.Name, 0) {
		return
	}
	n, err := s.store.CountProbes(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if n >= probeMaxCount {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("En fazla %d kontrol noktası eklenebilir", probeMaxCount))
		return
	}
	token := newProbeToken()
	p := store.Probe{Name: in.Name, Active: true, CreatedAt: s.now().Unix(), Hash: hashToken(token), TokenPrefix: token[:probeTokenShown]}
	if err := s.store.CreateProbe(r.Context(), &p); err != nil {
		if store.IsUniqueViolation(err) {
			writeError(w, http.StatusConflict, "Token üretilemedi, tekrar deneyin")
			return
		}
		s.dbError(w, err)
		return
	}
	s.log.Info("kontrol noktası eklendi", "kontrol_noktasi", p.Name)
	s.audit(r, store.User{}, "probe.create", "probe", p.ID, p.Name, "")
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
	var in struct {
		Name   string `json:"name"`
		Active *bool  `json:"active"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !s.validateProbeName(w, r, in.Name, id) {
		return
	}
	active := old.Active
	if in.Active != nil {
		active = *in.Active
	}
	if err := s.store.UpdateProbe(r.Context(), id, in.Name, active); err != nil {
		s.dbError(w, err)
		return
	}
	var changes []string
	if in.Name != old.Name {
		changes = append(changes, "ad: "+old.Name+" → "+in.Name)
	}
	if active != old.Active {
		changes = append(changes, map[bool]string{true: "etkinleştirildi", false: "devre dışı bırakıldı"}[active])
	}
	if len(changes) > 0 {
		// Ad mesajlarda, etkinlik konum listesinde kullanılır: monitörler yeniden yüklenir.
		s.reloadProbeMonitors(r.Context(), id, nil)
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
	writeJSON(w, http.StatusOK, s.probeAdminOf(p, len(ids)))
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
	s.reloadProbeMonitors(r.Context(), id, affected)
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

func (s *Server) getMonitorLocations(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMonitor(r.Context(), id)
	if err == nil && !visibleTo(userFrom(r)).can(id) {
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
		IncludeLocal *bool   `json:"include_local"`
		ProbeIDs     []int64 `json:"probe_ids"`
		DownWhen     string  `json:"down_when"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	setup := store.LocationSetup{IncludeLocal: in.IncludeLocal == nil || *in.IncludeLocal, DownWhen: in.DownWhen, ProbeIDs: []int64{}}
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
		if !ok {
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
		"konumlar: "+strings.Join(names, ", ")+"; kural: "+setup.DownWhen)
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
}

// assignedJobs kontrol noktasının çalıştırabileceği aktif monitörleri döner.
func (s *Server) assignedJobs(ctx context.Context, probeID int64) ([]store.Monitor, error) {
	list, err := s.store.ProbeJobs(ctx, probeID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(list, func(m store.Monitor) bool { return !engine.RemoteCapable(m.Type) }), nil
}

func (s *Server) probeJobs(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	list, err := s.assignedJobs(r.Context(), p.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	jobs := make([]probeJob, len(list))
	for i, m := range list {
		jobs[i] = probeJob{ID: m.ID, Name: m.Name, Type: m.Type, Interval: m.Interval,
			RetryInterval: m.RetryInterval, Timeout: m.Timeout, Config: m.Config}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"probe":      map[string]any{"id": p.ID, "name": p.Name},
		"poll_after": engine.ProbePollAfter,
		"jobs":       jobs,
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
}

type probeRejected struct {
	Index     int    `json:"index"`
	MonitorID int64  `json:"monitor_id"`
	Error     string `json:"error"`
}

func (s *Server) probeResults(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	// Bilinmeyen alanlar kabul edilir: yeni sürüm bir kontrol noktası eski bir
	// sunucuya da sonuç gönderebilsin.
	var in struct {
		SentAt  int64           `json:"sent_at"`
		Results []probeResultIn `json:"results"`
	}
	body := http.MaxBytesReader(w, r.Body, 1<<20)
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
	assigned := make(map[int64]bool, len(jobs))
	for _, m := range jobs {
		assigned[m.ID] = true
	}
	now := s.now()
	sentAt := in.SentAt
	if sentAt <= 0 {
		sentAt = now.UnixMilli()
	}
	accepted := make([]engine.ProbeResult, 0, len(in.Results))
	rejected := []probeRejected{}
	for i, res := range in.Results {
		if !assigned[res.MonitorID] {
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
