package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

const (
	sessionCookie   = "uptime_oturum"
	sessionLifetime = 30 * 24 * time.Hour
)

// bcryptCost testlerde düşürülür (race detector altında 12 çok yavaş).
var bcryptCost = 12

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{3,32}$`)

// dummyHash kullanıcı bulunamadığında da bcrypt karşılaştırması yapılsın diye
// kullanılır; yanıt süresinden kullanıcı adının var olup olmadığı anlaşılmaz.
var dummyHash = sync.OnceValue(func() []byte {
	h, _ := bcrypt.GenerateFromPassword([]byte("uptime-dummy-password"), bcryptCost)
	return h
})

type ctxKey int

const (
	userKey ctxKey = iota
	sessionKey
)

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func randomToken(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) error {
	token := randomToken(32)
	exp := s.now().Add(sessionLifetime)
	if err := s.store.CreateSession(r.Context(), hashToken(token), u.ID, exp); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: exp,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
	return nil
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) currentUser(r *http.Request) (store.User, string, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return store.User{}, "", false
	}
	h := hashToken(c.Value)
	u, err := s.store.SessionUser(r.Context(), h)
	if err != nil {
		return store.User{}, "", false
	}
	return u, h, true
}

// auth oturum gerektiren uç noktaları sarar.
func (s *Server) auth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, sess, ok := s.currentUser(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Oturum açmanız gerekiyor")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionKey, sess)
		h(w, r.WithContext(ctx))
	})
}

type userView struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp := map[string]any{"setup_needed": n == 0, "user": nil, "version": s.version}
	if u, _, ok := s.currentUser(r); ok {
		resp["user"] = userView{u.ID, u.Username}
	}
	writeJSON(w, http.StatusOK, resp)
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func validatePassword(p string) error {
	if utf8.RuneCountInString(p) < 8 {
		return errors.New("Şifre en az 8 karakter olmalı")
	}
	if len(p) > 72 {
		return errors.New("Şifre en fazla 72 bayt olabilir")
	}
	return nil
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !readJSON(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	if !usernameRe.MatchString(in.Username) {
		writeError(w, http.StatusBadRequest, "Kullanıcı adı 3-32 karakter olmalı; harf, rakam, nokta, tire ve alt çizgi kullanılabilir")
		return
	}
	if err := validatePassword(in.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		s.dbError(w, err)
		return
	}
	u, err := s.store.CreateFirstUser(r.Context(), in.Username, string(hash))
	if err != nil {
		writeError(w, http.StatusConflict, "Kurulum zaten tamamlanmış")
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.dbError(w, err)
		return
	}
	s.log.Info("ilk kurulum tamamlandı", "kullanıcı", u.Username)
	writeJSON(w, http.StatusOK, map[string]any{"user": userView{u.ID, u.Username}})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !readJSON(w, r, &in) {
		return
	}
	in.Username = strings.TrimSpace(in.Username)
	ip := clientIP(r)
	if ok, wait := s.limiter.allow(ip, in.Username, s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Çok fazla hatalı deneme. "+strconv.Itoa(int(wait.Minutes())+1)+" dakika sonra tekrar deneyin.")
		return
	}
	u, err := s.store.UserByName(r.Context(), in.Username)
	hash := dummyHash()
	if err == nil {
		hash = []byte(u.PasswordHash)
	} else if !errors.Is(err, store.ErrNotFound) {
		s.dbError(w, err)
		return
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(in.Password)) != nil || err != nil {
		s.limiter.fail(ip, in.Username, s.now())
		s.log.Warn("hatalı giriş denemesi", "ip", ip, "kullanıcı", in.Username)
		writeError(w, http.StatusUnauthorized, "Kullanıcı adı veya şifre hatalı")
		return
	}
	s.limiter.success(ip)
	if err := s.startSession(w, r, u); err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userView{u.ID, u.Username}})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		s.store.DeleteSession(r.Context(), hashToken(c.Value))
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(store.User)
	sess := r.Context().Value(sessionKey).(string)
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(in.Current)) != nil {
		writeError(w, http.StatusBadRequest, "Mevcut şifre hatalı")
		return
	}
	if err := validatePassword(in.New); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.New), bcryptCost)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.UpdatePassword(r.Context(), u.ID, string(hash)); err != nil {
		s.dbError(w, err)
		return
	}
	// Diğer cihazlardaki oturumlar kapanır, bu oturum açık kalır.
	s.store.DeleteOtherSessions(r.Context(), u.ID, sess)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// cloudflareNets Cloudflare'in kenar sunucu adresleri
// (https://www.cloudflare.com/ips/). CF-Connecting-IP başlığına sadece istek
// gerçekten bunlardan birinden geldiyse güvenilir; aksi halde sunucuya
// doğrudan bağlanan biri bu başlığı uydurup giriş sınırını aşabilirdi.
var cloudflareNets = mustCIDRs(
	"173.245.48.0/20", "103.21.244.0/22", "103.22.200.0/22", "103.31.4.0/22",
	"141.101.64.0/18", "108.162.192.0/18", "190.93.240.0/20", "188.114.96.0/20",
	"197.234.240.0/22", "198.41.128.0/17", "162.158.0.0/15", "104.16.0.0/13",
	"104.24.0.0/14", "172.64.0.0/13", "131.0.72.0/22",
	"2400:cb00::/32", "2606:4700::/32", "2803:f800::/32", "2405:b500::/32",
	"2405:8100::/32", "2a06:98c0::/29", "2c0f:f248::/32",
)

func mustCIDRs(cidrs ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(cidrs))
	for i, c := range cidrs {
		out[i] = netip.MustParsePrefix(c)
	}
	return out
}

func inNets(ip netip.Addr, nets []netip.Prefix) bool {
	ip = ip.Unmap()
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP isteği yapan istemcinin adresini bulur. Zincir:
// istemci → Cloudflare → Traefik (aynı makinede, özel ağ) → uygulama.
//  1. Doğrudan bağlanan özel ağdaki proxy (Traefik) ise gerçek bağlanan adres,
//     Traefik'in X-Forwarded-For'a eklediği son değerdir (ilk değerler
//     istemci tarafından uydurulabilir).
//  2. O adres bir Cloudflare sunucusuysa asıl istemci CF-Connecting-IP'dedir.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	if peer.IsPrivate() || peer.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if p, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				peer = p
			}
		}
	}
	if inNets(peer, cloudflareNets) {
		if cf, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); err == nil {
			return cf.Unmap().String()
		}
	}
	return peer.Unmap().String()
}

// loginLimiter hatalı giriş denemelerini sınırlar:
//   - IP başına: 15 dakikada 5 hata → o IP 15 dakika bekler.
//   - Kullanıcı adı başına: 15 dakikada 50 hata → o kullanıcı adı 15 dakika
//     kilitlenir. Dağıtık (çok IP'li) tahmin saldırısını yavaşlatır; eşik
//     yüksek tutulduğu için tek bir saldırganın yönetici hesabını kolayca
//     kilitlemesi mümkün değildir. (Tüm girişleri kilitleyen genel sınır
//     bilerek yoktur: herkesin girişini engellemek için kullanılabilirdi.)
type loginLimiter struct {
	mu   sync.Mutex
	keys map[string]*attempts
}

type attempts struct {
	count  int
	first  time.Time
	locked time.Time
}

const (
	loginWindow      = 15 * time.Minute
	loginMaxPerIP    = 5
	loginMaxPerUser  = 50
	loginLockoutTime = 15 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{keys: map[string]*attempts{}} }

func limiterKeys(ip, username string) [2]string {
	return [2]string{"ip:" + ip, "user:" + strings.ToLower(strings.TrimSpace(username))}
}

func (l *loginLimiter) allow(ip, username string, now time.Time) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range limiterKeys(ip, username) {
		if a := l.keys[k]; a != nil && now.Before(a.locked) {
			wait = max(wait, a.locked.Sub(now))
		}
	}
	return wait == 0, wait
}

func (l *loginLimiter) fail(ip, username string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keys := limiterKeys(ip, username)
	for i, k := range keys {
		a := l.keys[k]
		if a == nil {
			a = &attempts{}
			l.keys[k] = a
		}
		if now.Sub(a.first) > loginWindow {
			a.count, a.first = 0, now
		}
		a.count++
		limit := loginMaxPerIP
		if i == 1 {
			limit = loginMaxPerUser
		}
		if a.count >= limit {
			a.locked = now.Add(loginLockoutTime)
		}
	}
	// Bellek şişmesin: süresi geçmiş kayıtları ara ara temizle.
	if len(l.keys) > 10000 {
		for k, v := range l.keys {
			if now.Sub(v.first) > loginWindow && now.After(v.locked) {
				delete(l.keys, k)
			}
		}
	}
}

// success başarılı girişte sadece o IP'nin sayacını sıfırlar; kullanıcı adı
// sayacı dağıtık saldırıyı izlemeye devam eder.
func (l *loginLimiter) success(ip string) {
	l.mu.Lock()
	delete(l.keys, "ip:"+ip)
	l.mu.Unlock()
}
