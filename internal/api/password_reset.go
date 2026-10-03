package api

// Kullanıcı e-postası ve şifremi unuttum (E-13).
//
//	POST /api/auth/email        {"password","email"}   kendi e-postasını değiştir (şifreyle onay)
//	POST /api/auth/forgot       {"login"}              sıfırlama bağlantısı iste (kullanıcı adı ya da e-posta)
//	POST /api/auth/reset        {"token","password"}   bağlantıdaki token ile yeni şifre
//
// "Şifremi unuttum" yalnızca Ayarlar → Giriş'te bir sistem e-posta kanalı
// seçiliyse çalışır (AppSettings.SystemMailChannelID): kanalın SMTP ayarı
// kullanılır, alıcı kullanıcının kendi e-postasıdır. Yanıt hesabın varlığını
// sızdırmaz: kullanıcı bulunsa da bulunmasa da aynı 200 döner, hız sınırı IP
// ve hesap başınadır. Token 30 dakika geçerli, tek kullanımlık; veritabanında
// yalnızca SHA-256 özeti tutulur. Sıfırlama kullanıcının tüm oturumlarını
// kapatır; iki adımlı doğrulama açıksa açık kalır.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	resetLifetime  = 30 * time.Minute
	forgotPerIP    = 10 // 15 dakikada IP başına istek
	forgotPerLogin = 3  // 15 dakikada hesap başına istek
	maxEmailLen    = 254
)

// Mailer sistem e-postası gönderimi; testler sahteyle değiştirir.
type Mailer func(ctx context.Context, cfg json.RawMessage, to, subject, text, html string) error

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/auth/email", s.auth(s.changeEmail))
		mux.HandleFunc("POST /api/auth/forgot", s.forgotPassword)
		mux.HandleFunc("POST /api/auth/reset", s.resetPassword)
	})
}

// normalizeEmail boş ("" = kaldır) ya da geçerli tek bir adres.
func normalizeEmail(e string) (string, error) {
	e = store.NormalizeEmail(e)
	if e == "" {
		return "", nil
	}
	if len(e) > maxEmailLen || strings.ContainsAny(e, " <>,;\"") {
		return "", errors.New("E-posta adresi geçersiz")
	}
	a, err := mail.ParseAddress(e)
	if err != nil || a.Address != e || !strings.Contains(e, "@") {
		return "", errors.New("E-posta adresi geçersiz")
	}
	return e, nil
}

// systemMailer sistem e-postası kanalı (ayarlardaki etkin e-posta kanalı); yoksa ok=false.
func (s *Server) systemMailer(ctx context.Context) (store.Notification, bool) {
	st, err := s.store.LoadSettings(ctx)
	if err != nil || st.SystemMailChannelID == 0 {
		return store.Notification{}, false
	}
	ch, err := s.store.GetNotification(ctx, st.SystemMailChannelID)
	if err != nil || ch.Type != "email" || !ch.Active {
		return store.Notification{}, false
	}
	return ch, true
}

// validateSystemMail ayarlardaki sistem e-posta kanalı var ve e-posta türünde mi?
func (s *Server) validateSystemMail(ctx context.Context, id int64) error {
	if id == 0 {
		return nil
	}
	ch, err := s.store.GetNotification(ctx, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && ch.Type != "email") {
		return errors.New("Sistem e-postası için bir e-posta (SMTP) kanalı seçin")
	}
	return err
}

// changeEmail kullanıcının kendi e-postası; şifre onayı ister (e-posta şifre
// sıfırlamanın anahtarıdır).
func (s *Server) changeEmail(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.APIKeyName != "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	var in struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !u.OIDC && !s.checkPassword(w, r, u, in.Password) {
		return
	}
	if err := s.store.SetUserEmail(r.Context(), u.ID, email); err != nil {
		if errors.Is(err, store.ErrEmailTaken) {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		s.dbError(w, err)
		return
	}
	u.Email = email
	s.audit(r, u, "user.email_change", "user", u.ID, u.Username, email)
	writeJSON(w, http.StatusOK, map[string]any{"user": viewOf(u)})
}

// forgotPassword her durumda aynı yanıtı verir; e-posta yalnızca hesap varsa,
// e-postası kayıtlıysa, hesap açıksa ve sistem e-postası ayarlıysa gider.
func (s *Server) forgotPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Login string `json:"login"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Login = strings.TrimSpace(in.Login)
	if in.Login == "" || utf8.RuneCountInString(in.Login) > maxEmailLen {
		writeError(w, http.StatusBadRequest, "Kullanıcı adı veya e-posta gerekli")
		return
	}
	ip := clientIP(r)
	now := s.now()
	keys := []string{"forgot-ip:" + ip, "forgot-login:" + strings.ToLower(in.Login)}
	if ok, wait := s.limiter.allowKeys(now, keys...); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Çok fazla sıfırlama isteği; biraz sonra tekrar deneyin")
		return
	}
	s.limiter.failKey(keys[0], forgotPerIP, now)
	s.limiter.failKey(keys[1], forgotPerLogin, now)
	ok := map[string]bool{"ok": true}
	mailer, mailOK := s.systemMailer(r.Context())
	if !mailOK {
		writeJSON(w, http.StatusOK, ok) // ayar yok: sessizce (arayüz bağlantıyı zaten göstermez)
		return
	}
	u, err := s.store.UserByLogin(r.Context(), in.Login)
	if err != nil || u.Disabled || u.Email == "" {
		// Bulunamayan hesap için de aynı süre ve yanıt (zamanlama farkı en aza).
		bcrypt.CompareHashAndPassword(dummyHash(), []byte("x"))
		s.audit(r, store.User{}, "login.reset_request", "user", 0, in.Login, "eşleşen hesap yok")
		writeJSON(w, http.StatusOK, ok)
		return
	}
	token := randomToken(32)
	if err := s.store.CreatePasswordReset(r.Context(), hashToken(token), u.ID, now.Unix(), now.Add(resetLifetime).Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	link := s.externalURL(r) + "/#/reset/" + url.PathEscape(token)
	lang := i18n.Or(u.Lang)
	if u.Lang == "" {
		lang = requestLang(r)
	}
	subject, text, html := resetMail(lang, u.Username, link, int(resetLifetime.Minutes()))
	s.audit(r, store.User{}, "login.reset_request", "user", u.ID, u.Username, "")
	// Gönderim arka planda: SMTP yavaşlığı yanıt süresinden hesabın varlığını açık etmesin.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mailer(ctx, mailer.Config, u.Email, subject, text, html); err != nil {
			s.log.Error("şifre sıfırlama e-postası gönderilemedi", "kullanıcı", u.Username, "hata", notify.SanitizeSendError("email", mailer.Config, err))
		}
	}()
	writeJSON(w, http.StatusOK, ok)
}

// resetMail sıfırlama e-postasının konusu, düz metni ve HTML'i.
func resetMail(lang, username, link string, minutes int) (subject, text, html string) {
	subject = i18n.T(lang, "mail.reset.subject")
	notes := []string{i18n.T(lang, "mail.reset.intro", username), i18n.T(lang, "mail.reset.expires", minutes), i18n.T(lang, "mail.reset.ignore")}
	text = subject + "\n\n" + strings.Join(notes, "\n") + "\n\n" + link + "\n"
	html = notify.RenderMail(lang, i18n.T(lang, "mail.reset.status"), subject, notes, nil, link, i18n.T(lang, "mail.reset.button"))
	return subject, text, html
}

// externalURL bağlantılar için dış adres: BASE_URL, yoksa isteğin şeması + sunucu adı.
func (s *Server) externalURL(r *http.Request) string {
	if s.BaseURL != "" {
		return strings.TrimRight(s.BaseURL, "/")
	}
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

// resetPassword bağlantıdaki token ile yeni şifre.
func (s *Server) resetPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ip := clientIP(r)
	now := s.now()
	if ok, wait := s.limiter.allowKeys(now, "reset-ip:"+ip); !ok {
		w.Header().Set("Retry-After", fmt.Sprint(int(wait.Seconds())+1))
		writeError(w, http.StatusTooManyRequests, "Çok fazla hatalı deneme. "+fmt.Sprint(int(wait.Minutes())+1)+" dakika sonra tekrar deneyin.")
		return
	}
	invalid := func() {
		s.limiter.failKey("reset-ip:"+ip, loginMaxPerIP, now)
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Sıfırlama bağlantısı geçersiz ya da süresi dolmuş; yeniden isteyin", "code": "reset_invalid"})
	}
	if in.Token == "" || len(in.Token) > 128 {
		invalid()
		return
	}
	h := hashToken(in.Token)
	uid, err := s.store.PasswordResetUser(r.Context(), h, now.Unix())
	if errors.Is(err, store.ErrNotFound) {
		invalid()
		return
	} else if err != nil {
		s.dbError(w, err)
		return
	}
	if err := validatePassword(in.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.UserByID(r.Context(), uid)
	if err != nil || u.Disabled {
		invalid()
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		s.dbError(w, err)
		return
	}
	used, err := s.store.UsePasswordReset(r.Context(), h, u.ID, string(hash), now.Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !used {
		invalid()
		return
	}
	s.limiter.reset("reset-ip:" + ip)
	s.audit(r, store.User{}, "user.password_reset_self", "user", u.ID, u.Username, "")
	s.log.Info("şifre sıfırlama bağlantısıyla şifre değiştirildi", "kullanıcı", u.Username, "ip", ip)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": u.Username})
}
