package api

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// İki adımlı doğrulama (TOTP, RFC 6238) ------------------------------------------
//
// SHA1, 6 hane, 30 saniye; saat kayması için ±1 adım kabul edilir. Onaylanan
// her kodun adımı kaydedilir, aynı kod (veya daha eski bir kod) ikinci kez
// kullanılamaz. Kurtarma kodları 10 adet, tek kullanımlık, özetleri saklanır.
//
// Giriş: şifre doğruysa ve 2FA açıksa /api/auth/login oturum açmaz,
// {two_factor_required, challenge} döner; /api/auth/login/2fa challenge ve
// kodla oturumu açar. Challenge 5 dakika geçerli, tek kullanımlık; hatalı
// kodlar giriş sınırlayıcısına sayılır ve 5 hatalı koddan sonra challenge silinir.
//
// Not: TOTP sırrı veritabanında düz metin tutulur (şifreleme anahtarı da aynı
// sunucuda duracağından anlamlı bir koruma sağlamazdı); kurulumdan sonra API
// hiçbir yerde sırrı geri döndürmez.

const (
	totpPeriod           = 30
	totpDigits           = 6
	totpSkew             = 1
	totpIssuer           = "Uptime"
	recoveryCodeCount    = 10
	challengeLifetime    = 5 * time.Minute
	challengeMaxAttempts = 5
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// totpCode RFC 6238 / RFC 4226 kodu.
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	v := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, v%1000000)
}

// totpMatch kod ±1 adım içinde geçerliyse eşleşen adımı döner.
func totpMatch(secretB32, code string, now time.Time) (int64, bool) {
	secret, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil || len(secret) == 0 || len(code) != totpDigits {
		return 0, false
	}
	cur := now.Unix() / totpPeriod
	// En yeni adımdan başlanır: aynı kod birden fazla adıma denk gelirse
	// (pratikte olmaz) en büyük adım kaydedilir.
	for d := int64(totpSkew); d >= -totpSkew; d-- {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, cur+d)), []byte(code)) == 1 {
			return cur + d, true
		}
	}
	return 0, false
}

// normalizeCode kullanıcının girdiği kodu sadeleştirir: boşluk ve tireler
// atılır, küçük harfe çevrilir ("123 456" → "123456", "ABCDE-FGHIJ" → "abcdefghij").
func normalizeCode(c string) string {
	c = strings.ToLower(c)
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '\t' {
			return -1
		}
		return r
	}, c)
}

func isTOTPFormat(c string) bool {
	if len(c) != totpDigits {
		return false
	}
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// newRecoveryCodes 10 kod üretir: "abcde-fghij" (base32, 50 bit) ve özetleri.
func newRecoveryCodes() (codes, hashes []string) {
	enc := base32.StdEncoding.WithPadding(base32.NoPadding)
	for range recoveryCodeCount {
		b := make([]byte, 7)
		rand.Read(b)
		c := strings.ToLower(enc.EncodeToString(b))[:10]
		codes = append(codes, c[:5]+"-"+c[5:])
		hashes = append(hashes, hashToken(c))
	}
	return codes, hashes
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/auth/2fa", s.auth(s.twoFactorStatus))
		mux.Handle("POST /api/auth/2fa/setup", s.auth(s.twoFactorSetup))
		mux.Handle("POST /api/auth/2fa/enable", s.auth(s.twoFactorEnable))
		mux.Handle("POST /api/auth/2fa/disable", s.auth(s.twoFactorDisable))
		mux.Handle("POST /api/auth/2fa/recovery-codes", s.auth(s.twoFactorNewRecoveryCodes))
		mux.HandleFunc("POST /api/auth/login/2fa", s.loginTwoFactor)
		mux.Handle("POST /api/users/{id}/2fa/reset", s.admin(s.resetUserTwoFactor))
	})
}

// limited giriş sınırlayıcısına takılmışsa 429 yazar ve true döner.
func (s *Server) limited(w http.ResponseWriter, ip, username string) bool {
	ok, wait := s.limiter.allow(ip, username, s.now())
	if ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
	writeError(w, http.StatusTooManyRequests, "Çok fazla hatalı deneme. "+strconv.Itoa(int(wait.Minutes())+1)+" dakika sonra tekrar deneyin.")
	return true
}

// verifySecondFactor 6 haneli TOTP kodunu veya kurtarma kodunu doğrular ve
// harcar. method "totp" veya "recovery".
func (s *Server) verifySecondFactor(r *http.Request, userID int64, code string) (method string, ok bool, err error) {
	code = normalizeCode(code)
	if code == "" || len(code) > 32 {
		return "", false, nil
	}
	if isTOTPFormat(code) {
		secret, enabled, err := s.store.TOTPSecret(r.Context(), userID)
		if err != nil || !enabled {
			return "", false, err
		}
		step, match := totpMatch(secret, code, s.now())
		if !match {
			return "", false, nil
		}
		ok, err := s.store.UseTOTPStep(r.Context(), userID, step)
		return "totp", ok, err
	}
	ok, err = s.store.UseRecoveryCode(r.Context(), userID, hashToken(code), s.now().Unix())
	return "recovery", ok, err
}

func (s *Server) twoFactorStatus(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	left := 0
	if u.TwoFactorEnabled {
		var err error
		if left, err = s.store.RecoveryCodesLeft(r.Context(), u.ID); err != nil {
			s.dbError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": u.TwoFactorEnabled, "recovery_codes_left": left})
}

// checkPassword şifre yanlışsa sınırlayıcıya sayar, 400 yazar ve false döner.
func (s *Server) checkPassword(w http.ResponseWriter, r *http.Request, u store.User, password string) bool {
	ip := clientIP(r)
	if s.limited(w, ip, u.Username) {
		return false
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		s.limiter.fail(ip, u.Username, s.now())
		writeError(w, http.StatusBadRequest, "Şifre hatalı")
		return false
	}
	return true
}

// checkCode ikinci adım kodu yanlışsa sınırlayıcıya sayar, 400 yazar ve false döner.
func (s *Server) checkCode(w http.ResponseWriter, r *http.Request, u store.User, code string) bool {
	_, ok, err := s.verifySecondFactor(r, u.ID, code)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	if !ok {
		s.limiter.fail(clientIP(r), u.Username, s.now())
		writeError(w, http.StatusBadRequest, "Doğrulama kodu hatalı veya daha önce kullanılmış")
		return false
	}
	return true
}

// twoFactorSetup yeni bir sır üretir (henüz etkin değil); /enable ile onaylanır.
func (s *Server) twoFactorSetup(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) || !s.checkPassword(w, r, u, in.Password) {
		return
	}
	if u.TwoFactorEnabled {
		writeError(w, http.StatusConflict, "İki adımlı doğrulama zaten açık")
		return
	}
	raw := make([]byte, 20)
	rand.Read(raw)
	secret := b32.EncodeToString(raw)
	if err := s.store.SetPendingTOTP(r.Context(), u.ID, secret); err != nil {
		if errors.Is(err, store.ErrTwoFactorEnabled) {
			writeError(w, http.StatusConflict, "İki adımlı doğrulama zaten açık")
			return
		}
		s.dbError(w, err)
		return
	}
	q := url.Values{}
	q.Set("secret", secret)
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", strconv.Itoa(totpDigits))
	q.Set("period", strconv.Itoa(totpPeriod))
	otpURL := "otpauth://totp/" + url.PathEscape(totpIssuer+":"+u.Username) + "?" + q.Encode()
	resp := map[string]any{"secret": secret, "otpauth_url": otpURL}
	if png, err := qrcode.Encode(otpURL, qrcode.Medium, 256); err == nil {
		resp["qr_png"] = "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
	}
	writeJSON(w, http.StatusOK, resp)
}

// twoFactorEnable kurulumdaki sırrı ilk kodla onaylar ve kurtarma kodlarını
// (bir kez) döner. Diğer cihazlardaki oturumlar kapanır.
func (s *Server) twoFactorEnable(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if s.limited(w, clientIP(r), u.Username) {
		return
	}
	secret, enabled, err := s.store.TOTPSecret(r.Context(), u.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if enabled {
		writeError(w, http.StatusConflict, "İki adımlı doğrulama zaten açık")
		return
	}
	if secret == "" {
		writeError(w, http.StatusBadRequest, "Önce kurulumu başlatın")
		return
	}
	step, ok := totpMatch(secret, normalizeCode(in.Code), s.now())
	if !ok {
		s.limiter.fail(clientIP(r), u.Username, s.now())
		writeError(w, http.StatusBadRequest, "Doğrulama kodu hatalı; telefonunuzun saatinin doğru olduğundan emin olun")
		return
	}
	codes, hashes := newRecoveryCodes()
	if err := s.store.EnableTOTP(r.Context(), u.ID, secret, step, hashes); err != nil {
		if errors.Is(err, store.ErrTwoFactorEnabled) {
			writeError(w, http.StatusConflict, "Kurulum başka bir yerde değişti; yeniden başlatın")
			return
		}
		s.dbError(w, err)
		return
	}
	sess, _ := r.Context().Value(sessionKey).(string)
	s.store.DeleteOtherSessions(r.Context(), u.ID, sess)
	s.audit(r, u, "user.2fa_enable", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// twoFactorDisable şifre ve geçerli bir kod (TOTP veya kurtarma kodu) ister.
func (s *Server) twoFactorDisable(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !u.TwoFactorEnabled {
		writeError(w, http.StatusBadRequest, "İki adımlı doğrulama zaten kapalı")
		return
	}
	if !s.checkPassword(w, r, u, in.Password) || !s.checkCode(w, r, u, in.Code) {
		return
	}
	if err := s.store.DisableTwoFactor(r.Context(), u.ID); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "user.2fa_disable", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// twoFactorNewRecoveryCodes eski kurtarma kodlarını geçersiz kılıp yenilerini üretir.
func (s *Server) twoFactorNewRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if !u.TwoFactorEnabled {
		writeError(w, http.StatusBadRequest, "İki adımlı doğrulama kapalı")
		return
	}
	if !s.checkPassword(w, r, u, in.Password) || !s.checkCode(w, r, u, in.Code) {
		return
	}
	codes, hashes := newRecoveryCodes()
	if err := s.store.ReplaceRecoveryCodes(r.Context(), u.ID, hashes); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "user.2fa_recovery_codes", "user", u.ID, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

// resetUserTwoFactor yönetici, telefonunu ve kurtarma kodlarını kaybeden
// kullanıcının iki adımlı doğrulamasını kapatır. Kendi 2FA'sını buradan
// kapatamaz (şifre + kod isteyen normal yol kullanılmalı).
func (s *Server) resetUserTwoFactor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == userFrom(r).ID {
		writeError(w, http.StatusBadRequest, "Kendi iki adımlı doğrulamanızı buradan sıfırlayamazsınız; Hesabım bölümünden kapatın")
		return
	}
	u, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DisableTwoFactor(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "user.2fa_reset", "user", id, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Giriş ---------------------------------------------------------------------------

// beginTwoFactorLogin şifresi doğrulanan kullanıcı için challenge oluşturur;
// oturum açılmaz.
func (s *Server) beginTwoFactorLogin(w http.ResponseWriter, r *http.Request, u store.User) {
	token := randomToken(32)
	now := s.now()
	if err := s.store.CreateLoginChallenge(r.Context(), hashToken(token), u.ID, now.Add(challengeLifetime).Unix(), now.Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"two_factor_required": true, "challenge": token})
}

func (s *Server) loginTwoFactor(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Challenge string `json:"challenge"`
		Code      string `json:"code"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	expired := func() {
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "Giriş süresi doldu; kullanıcı adı ve şifrenizle tekrar giriş yapın", "code": "challenge_expired",
		})
	}
	ch := hashToken(in.Challenge)
	uid, err := s.store.LoginChallenge(r.Context(), ch, s.now().Unix())
	if errors.Is(err, store.ErrNotFound) || in.Challenge == "" {
		expired()
		return
	} else if err != nil {
		s.dbError(w, err)
		return
	}
	u, err := s.store.UserByID(r.Context(), uid)
	if err != nil {
		expired()
		return
	}
	ip := clientIP(r)
	if s.limited(w, ip, u.Username) {
		return
	}
	method, ok, err := s.verifySecondFactor(r, u.ID, in.Code)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !ok {
		s.limiter.fail(ip, u.Username, s.now())
		if err := s.store.FailLoginChallenge(r.Context(), ch, challengeMaxAttempts); err != nil {
			s.log.Error("giriş denemesi güncellenemedi", "hata", err)
		}
		s.log.Warn("hatalı iki adımlı doğrulama kodu", "ip", ip, "kullanıcı", u.Username)
		s.audit(r, store.User{}, "login.2fa_fail", "user", u.ID, u.Username, "")
		writeError(w, http.StatusUnauthorized, "Doğrulama kodu hatalı veya daha önce kullanılmış")
		return
	}
	// Tek kullanımlık: eşzamanlı ikinci istek burada elenir.
	if consumed, err := s.store.ConsumeLoginChallenge(r.Context(), ch); err != nil || !consumed {
		expired()
		return
	}
	if u.Disabled {
		writeError(w, http.StatusForbidden, "Hesabınız devre dışı bırakılmış; yöneticinize başvurun")
		return
	}
	s.limiter.success(ip)
	if err := s.startSession(w, r, u); err != nil {
		s.dbError(w, err)
		return
	}
	s.store.TouchLastLogin(r.Context(), u.ID)
	detail := "2FA"
	if method == "recovery" {
		detail = "2FA kurtarma kodu"
	}
	s.audit(r, u, "login.success", "user", u.ID, u.Username, detail)
	writeJSON(w, http.StatusOK, map[string]any{"user": viewOf(u)})
}
