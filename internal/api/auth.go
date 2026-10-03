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
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
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

// Authenticator çerez dışı kimlik doğrulama yöntemleri (ör. API anahtarı)
// için kanca: istekten kullanıcıyı çıkarabiliyorsa ok=true döner. Özellik
// dosyaları init() içinde RegisterAuthenticator ile ekler.
type Authenticator func(s *Server, r *http.Request) (u store.User, ok bool)

var extraAuthenticators []Authenticator

func RegisterAuthenticator(a Authenticator) { extraAuthenticators = append(extraAuthenticators, a) }

// authenticate istekteki oturum çerezini, yoksa kayıtlı diğer yöntemleri (API
// anahtarı) veritabanından doğrular ve güncel kullanıcıyı döner.
func (s *Server) authenticate(r *http.Request) (store.User, string, bool) {
	u, sess, ok := s.currentUser(r)
	if !ok {
		for _, a := range extraAuthenticators {
			if u, ok = a(s, r); ok {
				break
			}
		}
	}
	return u, sess, ok
}

// userFrom auth ile sarılmış bir istekteki kullanıcı.
func userFrom(r *http.Request) store.User {
	u, _ := r.Context().Value(userKey).(store.User)
	return u
}

// auth en az izleyici yetkisi gerektirir (her giriş yapmış kullanıcı).
func (s *Server) auth(h http.HandlerFunc) http.Handler { return s.role(store.RoleViewer, h) }

// editor en az editör yetkisi gerektirir.
func (s *Server) editor(h http.HandlerFunc) http.Handler { return s.role(store.RoleEditor, h) }

// admin yönetici yetkisi gerektirir.
func (s *Server) admin(h http.HandlerFunc) http.Handler { return s.role(store.RoleAdmin, h) }

// passwordChangeAllowed şifre değişimi zorunluyken erişilebilen uç noktalar.
var passwordChangeAllowed = map[string]bool{"/api/auth/password": true, "/api/auth/preferences": true}

// role oturum (veya kayıtlı başka bir kimlik doğrulama) ve en az min rolünü
// gerektirir. Yetki her istekte veritabanındaki güncel rolden kontrol edilir;
// rolü düşürülen kullanıcı yeni yetkisiyle hemen sınırlanır.
func (s *Server) role(min string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, sess, ok := s.authenticate(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "Oturum açmanız gerekiyor")
			return
		}
		setResponseLang(w, u.Lang)
		if u.MustChangePassword && !passwordChangeAllowed[r.URL.Path] {
			writeJSON(w, http.StatusForbidden, map[string]string{
				"error": "Devam etmeden önce şifrenizi değiştirmeniz gerekiyor", "code": "password_change_required",
			})
			return
		}
		if store.RoleRank(u.Role) < store.RoleRank(min) {
			writeError(w, http.StatusForbidden, "Bu işlem için yetkiniz yok")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, sessionKey, sess)
		h(w, r.WithContext(ctx))
	})
}

type userView struct {
	ID                 int64  `json:"id"`
	Username           string `json:"username"`
	DisplayName        string `json:"display_name"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
	AllMonitors        bool   `json:"all_monitors"`
	TwoFactorEnabled   bool   `json:"two_factor_enabled"`
	// Servers Sunucular ekranı açık mı: kısıtsız kullanıcıda her zaman, kısıtlı
	// izleyicide kendisine en az bir sunucu atanmışsa.
	Servers bool `json:"servers"`
	// Lang arayüz dili tercihi (tr | en); "" = tarayıcı dili.
	Lang string `json:"lang"`
	// Email isteğe bağlı e-posta; OIDC hesap bir SSO sağlayıcısına bağlı;
	// Theme arayüz teması ("" sistem | light | dark).
	Email string `json:"email"`
	OIDC  bool   `json:"oidc"`
	Theme string `json:"theme"`
}

func viewOf(u store.User) userView {
	return userView{u.ID, u.Username, u.DisplayName, u.Role, u.MustChangePassword, u.AllMonitors || u.Role != store.RoleViewer,
		u.TwoFactorEnabled, !u.Restricted() || len(u.ServerIDs) > 0, u.Lang, u.Email, u.OIDC, u.Theme}
}

// validThemes arayüz teması tercihleri.
var validThemes = map[string]bool{"": true, "light": true, "dark": true}

// updatePreferences: PUT /api/auth/preferences {"lang": "tr" | "en" | ""}.
// Kullanıcının kendi arayüz dili; "" tarayıcı diline döner.
func (s *Server) updatePreferences(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Lang  *string `json:"lang"`
		Theme *string `json:"theme"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Theme != nil {
		if !validThemes[*in.Theme] {
			writeError(w, http.StatusBadRequest, "Tema light, dark ya da boş (sistem) olmalı")
			return
		}
		if err := s.store.SetUserTheme(r.Context(), u.ID, *in.Theme); err != nil {
			s.dbError(w, err)
			return
		}
		u.Theme = *in.Theme
	}
	if in.Lang != nil {
		lang := *in.Lang
		if lang != "" && !i18n.Valid(lang) {
			writeError(w, http.StatusBadRequest, "Dil tr veya en olmalı")
			return
		}
		if err := s.store.SetUserLang(r.Context(), u.ID, lang); err != nil {
			s.dbError(w, err)
			return
		}
		u.Lang = lang
		setResponseLang(w, lang)
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": viewOf(u)})
}

func (s *Server) authState(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.CountUsers(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp := map[string]any{"setup_needed": n == 0, "user": nil, "version": s.version}
	// Giriş ekranı: şifre sıfırlama bağlantısı (sistem e-postası ayarlıysa) ve SSO düğmesi.
	_, mailOK := s.systemMailer(r.Context())
	resp["password_reset"] = mailOK
	resp["oidc"] = s.oidcPublic(r.Context())
	if u, _, ok := s.currentUser(r); ok {
		resp["user"] = viewOf(u)
		setResponseLang(w, u.Lang)
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
	if commonPasswords[strings.ToLower(strings.TrimSpace(p))] {
		return errors.New("Bu şifre çok yaygın; tahmin edilmesi zor başka bir şifre seçin")
	}
	return nil
}

// commonPasswords sızıntı listelerinde en sık görülen, 8+ karakterli şifreler
// (küçük harfle). Kısa bir yerleşik liste: amaç sözlük saldırısına ilk
// dakikada düşecek şifreleri engellemektir, tam bir politika motoru değil.
var commonPasswords = func() map[string]bool {
	list := []string{
		"password", "password1", "password12", "password123", "passw0rd", "p@ssw0rd", "p@ssword",
		"12345678", "123456789", "1234567890", "123456789a", "1234567891", "123123123", "1q2w3e4r",
		"1q2w3e4r5t", "qwerty123", "qwertyuiop", "qwerty12345", "abc12345", "abcd1234", "abcdefgh",
		"iloveyou", "sunshine", "princess", "football", "baseball", "superman", "trustno1",
		"welcome1", "welcome123", "letmein1", "whatever", "starwars", "computer", "internet",
		"michael1", "jennifer", "charlie1", "monkey123", "dragon123", "shadow123", "master123",
		"admin123", "admin1234", "administrator", "root1234", "changeme", "default1", "secret123",
		"test1234", "testtest", "deneme123", "sifre123", "sifre1234", "parola123", "parola1234",
		"istanbul", "galatasaray", "fenerbahce", "besiktas", "trabzonspor", "turkiye1", "merhaba1",
		"11111111", "00000000", "88888888", "987654321", "1qaz2wsx", "zaq12wsx", "asdfghjk",
		"asdfghjkl", "aa123456", "a1234567", "uptime123", "bekci123",
	}
	m := make(map[string]bool, len(list))
	for _, p := range list {
		m[p] = true
	}
	return m
}()

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
	s.audit(r, u, "user.setup", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"user": viewOf(u)})
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
		s.audit(r, store.User{}, "login.fail", "user", u.ID, in.Username, "")
		writeError(w, http.StatusUnauthorized, "Kullanıcı adı veya şifre hatalı")
		return
	}
	if !u.TwoFactorEnabled {
		// 2FA açıksa IP sayacı ancak kod da doğrulanınca sıfırlanır; yoksa şifreyi
		// bilen biri her seferinde yeniden giriş yaparak kod denemesi sınırını aşardı.
		s.limiter.success(ip)
	}
	if u.Disabled {
		// Şifre doğru ama hesap kapalı: bunu söylemek bilgi sızdırmaz (şifreyi bilen kişiye söyleniyor).
		writeError(w, http.StatusForbidden, "Hesabınız devre dışı bırakılmış; yöneticinize başvurun")
		return
	}
	if u.TwoFactorEnabled {
		s.beginTwoFactorLogin(w, r, u) // oturum /api/auth/login/2fa'da açılır (twofactor.go)
		return
	}
	if err := s.startSession(w, r, u); err != nil {
		s.dbError(w, err)
		return
	}
	s.store.TouchLastLogin(r.Context(), u.ID)
	s.audit(r, u, "login.success", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"user": viewOf(u)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		s.store.DeleteSession(r.Context(), hashToken(c.Value))
	}
	clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	sess, _ := r.Context().Value(sessionKey).(string)
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
	s.audit(r, u, "user.password_change", "user", u.ID, u.Username, "")
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

// trustedProxies TRUSTED_PROXY ortam değişkenindeki güvenilir proxy ağları
// (virgülle ayrılmış CIDR). Ayarlıysa X-Forwarded-For / CF-Connecting-IP
// başlıklarına yalnızca doğrudan bağlanan adres bu ağlardan biriyse güvenilir;
// aksi halde RemoteAddr kullanılır. Boşsa geriye dönük uyumluluk için eski
// davranış geçerlidir (tüm özel/loopback adresler güvenilir).
var trustedProxies = sync.OnceValue(func() []netip.Prefix { return parseCIDRList(os.Getenv("TRUSTED_PROXY")) })

// parseCIDRList virgülle ayrılmış CIDR listesini ayrıştırır; geçersizler atlanır.
func parseCIDRList(s string) []netip.Prefix {
	var out []netip.Prefix
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if p, err := netip.ParsePrefix(part); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// clientIP isteği yapan istemcinin adresini bulur. Zincir:
// istemci → Cloudflare → Traefik (aynı makinede, özel ağ) → uygulama.
//  1. Doğrudan bağlanan güvenilir proxy (Traefik) ise gerçek bağlanan adres,
//     Traefik'in X-Forwarded-For'a eklediği son değerdir (ilk değerler
//     istemci tarafından uydurulabilir).
//  2. O adres bir Cloudflare sunucusuysa asıl istemci CF-Connecting-IP'dedir.
//
// TRUSTED_PROXY ayarlıysa yalnızca oradaki ağlar güvenilir; ayarlı değilse
// (varsayılan) tüm özel/loopback adresler güvenilir sayılır.
func clientIP(r *http.Request) string {
	return clientIPUsing(r, trustedProxies())
}

// clientIPUsing clientIP'nin, güvenilir proxy ağları dışarıdan verilen
// sürümüdür (test edilebilirlik için). trusted boşsa eski davranış geçerlidir.
func clientIPUsing(r *http.Request, trusted []netip.Prefix) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	proxyMode := len(trusted) > 0
	var trust bool
	if proxyMode {
		trust = inNets(peer, trusted)
	} else {
		trust = peer.IsPrivate() || peer.IsLoopback()
	}
	if trust {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if p, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
				peer = p
			}
		}
	}
	// CF-Connecting-IP yalnızca istek gerçekten bir Cloudflare kenarından
	// geldiyse; TRUSTED_PROXY ayarlıyken ayrıca doğrudan bağlanan adres
	// güvenilir olmalı (aksi halde başlık uydurulabilirdi).
	if (!proxyMode || trust) && inNets(peer, cloudflareNets) {
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
	keys := limiterKeys(ip, username)
	return l.allowKeys(now, keys[:]...)
}

// allowKeys verilen anahtarlardan biri kilitliyse false ve kalan bekleme.
func (l *loginLimiter) allowKeys(now time.Time, keys ...string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var wait time.Duration
	for _, k := range keys {
		if a := l.keys[k]; a != nil && now.Before(a.locked) {
			wait = max(wait, a.locked.Sub(now))
		}
	}
	return wait == 0, wait
}

func (l *loginLimiter) fail(ip, username string, now time.Time) {
	keys := limiterKeys(ip, username)
	l.failKey(keys[0], loginMaxPerIP, now)
	l.failKey(keys[1], loginMaxPerUser, now)
}

// failKey anahtarın hata sayacını artırır; pencere içinde limit'e ulaşınca
// anahtarı kilitler.
func (l *loginLimiter) failKey(key string, limit int, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.keys[key]
	if a == nil {
		a = &attempts{}
		l.keys[key] = a
	}
	if now.Sub(a.first) > loginWindow {
		a.count, a.first = 0, now
	}
	a.count++
	if a.count >= limit {
		a.locked = now.Add(loginLockoutTime)
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
func (l *loginLimiter) success(ip string) { l.reset("ip:" + ip) }

// reset anahtarın sayacını siler.
func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.keys, key)
	l.mu.Unlock()
}
