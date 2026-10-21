package api

// Durum sayfası e-posta aboneliği (çift onaylı).
//
// Genel (giriş yok):
//
//	POST /api/public/pages/{slug}/subscribe          {"email","lang"}  -> 202 her durumda aynı
//	POST /api/public/subscriptions/confirm           {"token"}         -> 200 {"ok","page"} | 400
//	POST /api/public/subscriptions/unsubscribe?t=…   (one-click form ya da {"token"}) -> 200 | 400
//
// Yönetim (editör):
//
//	GET    /api/status-pages/{id}/subscribers
//	GET    /api/status-pages/{id}/subscribers.csv
//	DELETE /api/status-pages/{id}/subscribers/{sid}
//
// Sayfada abonelik kapalıysa, sayfa yayında değilse ya da sistem e-postası
// (Ayarlar > Giriş) yoksa genel uçlar 404 döner. Abone olma yanıtı adresin
// kayıtlı olup olmadığını sızdırmaz; e-posta arka planda gider. Onay
// belirteci 24 saat geçerli, tek kullanımlık; veritabanında yalnızca SHA-256
// özeti tutulur. Çıkış belirteci "<abone>.<bitiş>.<mac>" (mac: abonenin
// unsub_key'iyle HMAC-SHA256); her e-postada yeniden üretilir, abone silinince
// geçersiz olur. Çıkış ucu e-posta istemcilerinin one-click POST'u için CSRF
// başlığından muaftır (server.go csrf): yalnızca belirteç sahibinin kaydını siler.

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	subConfirmLifetime = 24 * time.Hour
	subUnsubLifetime   = 60 * 24 * time.Hour
	subResendGap       = 10 * time.Minute // aynı adrese iki onay postası arası en az
	subPerIP           = 5                // IP başına abone olma isteği / subIPWindow
	subIPWindow        = 15 * time.Minute
	subUnsubscribePath = "/api/public/subscriptions/unsubscribe"
)

// HeaderMailer ek başlıklı sistem e-postası (abone e-postaları); testler sahteyle değiştirir.
type HeaderMailer func(ctx context.Context, cfg json.RawMessage, to, subject, text, html string, headers map[string]string) error

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/status-pages/{id}/subscribers", s.editor(s.listSubscribers))
		mux.Handle("GET /api/status-pages/{id}/subscribers.csv", s.editor(s.exportSubscribers))
		mux.Handle("DELETE /api/status-pages/{id}/subscribers/{sid}", s.editor(s.deleteSubscriber))
		mux.HandleFunc("POST /api/public/pages/{slug}/subscribe", s.publicSubscribe)
		mux.HandleFunc("POST /api/public/subscriptions/confirm", s.publicConfirm)
		mux.HandleFunc("POST /api/public/subscriptions/unsubscribe", s.publicUnsubscribe)
	})
}

// isUnsubscribeRequest one-click çıkış isteği (CSRF başlığından muaf).
func isUnsubscribeRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == subUnsubscribePath
}

// subscribable sayfada abonelik şu an kullanılabilir mi (yayında, açık, sistem
// e-postası var); varsa sistem e-posta kanalını da döner.
func (s *Server) subscribable(ctx context.Context, p store.StatusPage) (store.Notification, bool) {
	if !p.Published || !p.Subscriptions {
		return store.Notification{}, false
	}
	return s.systemMailer(ctx)
}

// Belirteçler -------------------------------------------------------------------------

// unsubToken abonenin çıkış belirteci (exp: unix bitiş).
func unsubToken(sub store.PageSubscriber, exp int64) string {
	msg := strconv.FormatInt(sub.ID, 10) + "." + strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, []byte(sub.UnsubKey))
	mac.Write([]byte(msg))
	return msg + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// parseUnsubToken biçimi ve süresi geçerli belirtecin abone kimliği.
func parseUnsubToken(t string, now int64) (id, exp int64, ok bool) {
	parts := strings.Split(t, ".")
	if len(parts) != 3 || len(t) > 200 {
		return 0, 0, false
	}
	id, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[1], 10, 64)
	if err1 != nil || err2 != nil || id <= 0 || exp < now {
		return 0, 0, false
	}
	return id, exp, true
}

// validUnsubToken belirtecin imzası abonenin anahtarıyla uyuşuyor mu (sabit zamanlı).
func validUnsubToken(t string, sub store.PageSubscriber, exp int64) bool {
	return hmac.Equal([]byte(t), []byte(unsubToken(sub, exp)))
}

// subLinks abonenin sayfa adresi kökü ve sayfa yolu: özel alan adı varsa
// https://<alan adı>/ (akış bağlantılarıyla aynı kalıp), yoksa abone olurken
// kaydedilen taban + /durum/<kısa ad>.
func subLinks(p store.StatusPage, sub store.PageSubscriber) (base, page string) {
	if p.CustomDomain != "" {
		base = "https://" + p.CustomDomain
		return base, base + "/"
	}
	base = strings.TrimRight(sub.BaseURL, "/")
	return base, base + "/durum/" + p.Slug
}

// Genel uçlar -------------------------------------------------------------------------

func (s *Server) publicSubscribe(w http.ResponseWriter, r *http.Request) {
	e, err := s.publicEntry(r.Context(), strings.ToLower(r.PathValue("slug")))
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	// Önbellekteki sayfa 30 sn eski olabilir: güncel kayıt.
	p, err := s.store.GetPage(r.Context(), e.page.ID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	mailer, ok := s.subscribable(r.Context(), p)
	if !ok {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if p.HasPassword && !s.unlocked(r, p) {
		writeError(w, http.StatusUnauthorized, "Bu sayfa şifre korumalı")
		return
	}
	now := s.now()
	if ok, wait := s.subs.ipRL.allow(clientIP(r), now); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeError(w, http.StatusTooManyRequests, "Çok fazla deneme, biraz sonra tekrar deneyin")
		return
	}
	var in struct {
		Email string `json:"email"`
		Lang  string `json:"lang"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	email, err := normalizeEmail(in.Email)
	if err != nil || email == "" {
		writeError(w, http.StatusBadRequest, "E-posta adresi geçersiz")
		return
	}
	lang := i18n.Or(p.Lang)
	if i18n.Valid(in.Lang) {
		lang = in.Lang
	}
	accepted := map[string]bool{"ok": true}
	ctx := r.Context()
	sub, err := s.store.PageSubscriberByEmail(ctx, p.ID, email)
	token := randomToken(32)
	switch {
	case err == nil && sub.ConfirmedAt > 0:
		writeJSON(w, http.StatusAccepted, accepted) // zaten abone: sessizce
		return
	case err == nil:
		if now.Unix()-sub.ConfirmSentAt < int64(subResendGap/time.Second) {
			writeJSON(w, http.StatusAccepted, accepted) // son onay postası çok yeni
			return
		}
		sub.Lang, sub.BaseURL = lang, s.publicBase(r)
		if err := s.store.RenewSubscriberConfirm(ctx, sub.ID, hashToken(token), now.Add(subConfirmLifetime).Unix(), now.Unix(), sub.Lang, sub.BaseURL); err != nil {
			s.dbError(w, err)
			return
		}
	case errors.Is(err, store.ErrNotFound):
		n, err := s.store.CountPageSubscribers(ctx, p.ID)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if n >= store.MaxPageSubscribers {
			s.log.Warn("durum sayfası abone sınırı dolu; kayıt alınmadı", "sayfa", p.Slug, "sınır", store.MaxPageSubscribers)
			writeJSON(w, http.StatusAccepted, accepted)
			return
		}
		key := make([]byte, 32)
		rand.Read(key)
		sub = store.PageSubscriber{PageID: p.ID, Email: email, Lang: lang, BaseURL: s.publicBase(r), CreatedAt: now.Unix(),
			ConfirmHash: hashToken(token), ConfirmExpires: now.Add(subConfirmLifetime).Unix(), ConfirmSentAt: now.Unix(),
			UnsubKey: hex.EncodeToString(key)}
		if err := s.store.CreatePageSubscriber(ctx, &sub); err != nil {
			// Aynı anda gelen ikinci istek (UNIQUE): ilki postayı gönderir.
			s.log.Warn("abone kaydedilemedi", "sayfa", p.Slug, "hata", err)
			writeJSON(w, http.StatusAccepted, accepted)
			return
		}
	default:
		s.dbError(w, err)
		return
	}
	_, pageURL := subLinks(p, sub)
	link := pageURL + "?abonelik=onay&t=" + url.QueryEscape(token)
	subject, text, html := subConfirmMail(sub.Lang, p.Title, link)
	// Gönderim arka planda: SMTP yavaşlığı yanıttan adresin durumunu açık etmesin.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.subMailer(ctx, mailer.Config, sub.Email, subject, text, html, nil); err != nil {
			s.log.Error("abonelik onay e-postası gönderilemedi", "sayfa", p.Slug, "hata", notify.SanitizeSendError("email", mailer.Config, err))
		}
	}()
	writeJSON(w, http.StatusAccepted, accepted)
}

// subConfirmMail onay e-postasının konusu, düz metni ve HTML'i.
func subConfirmMail(lang, title, link string) (subject, text, html string) {
	subject = i18n.T(lang, "mail.sub.confirm.subject", title)
	notes := []string{i18n.T(lang, "mail.sub.confirm.intro", title), i18n.T(lang, "mail.sub.confirm.expires"), i18n.T(lang, "mail.sub.confirm.ignore")}
	text = subject + "\n\n" + strings.Join(notes, "\n") + "\n\n" + link + "\n"
	html = notify.RenderMail(lang, i18n.T(lang, "mail.sub.confirm.status"), subject, notes, nil, link, i18n.T(lang, "mail.sub.confirm.button"))
	return subject, text, html
}

func (s *Server) publicConfirm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.systemMailer(r.Context()); !ok {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	const bad = "Onay bağlantısı geçersiz veya süresi dolmuş"
	in.Token = strings.TrimSpace(in.Token)
	if in.Token == "" || len(in.Token) > 100 {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	ctx := r.Context()
	hash := hashToken(in.Token)
	sub, err := s.store.PageSubscriberByConfirmHash(ctx, hash)
	if errors.Is(err, store.ErrNotFound) || (err == nil && sub.ConfirmExpires < s.now().Unix()) {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	p, err := s.store.GetPage(ctx, sub.PageID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if _, ok := s.subscribable(ctx, p); !ok {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if err := s.store.ConfirmPageSubscriber(ctx, sub.ID, hash, s.now().Unix()); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, http.StatusBadRequest, bad)
			return
		}
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "page": p.Title})
}

func (s *Server) publicUnsubscribe(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.systemMailer(r.Context()); !ok {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	token := r.URL.Query().Get("t")
	if token == "" && strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		var in struct {
			Token string `json:"token"`
		}
		if !readJSON(w, r, &in) {
			return
		}
		token = in.Token
	} else {
		// One-click gövdesi ("List-Unsubscribe=One-Click") okunur ama gerekmez.
		io.Copy(io.Discard, io.LimitReader(r.Body, 4<<10))
	}
	const bad = "Çıkış bağlantısı geçersiz veya süresi dolmuş"
	token = strings.TrimSpace(token)
	id, exp, ok := parseUnsubToken(token, s.now().Unix())
	if !ok {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	ctx := r.Context()
	sub, err := s.store.GetPageSubscriber(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) // zaten çıkmış
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	if !validUnsubToken(token, sub, exp) {
		writeError(w, http.StatusBadRequest, bad)
		return
	}
	p, err := s.store.GetPage(ctx, sub.PageID)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if _, ok := s.subscribable(ctx, p); !ok {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if err := s.store.DeletePageSubscriber(ctx, 0, sub.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
		s.dbError(w, err)
		return
	}
	s.subs.forget(sub.ID)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// Yönetim uçları ---------------------------------------------------------------------

type subscriberView struct {
	ID          int64  `json:"id"`
	Email       string `json:"email"`
	Confirmed   bool   `json:"confirmed"`
	CreatedAt   int64  `json:"created_at"`
	ConfirmedAt int64  `json:"confirmed_at"`
}

// subscribersPage yol kimliğindeki sayfa (yoksa yanıtı yazar).
func (s *Server) subscribersPage(w http.ResponseWriter, r *http.Request) (store.StatusPage, []store.PageSubscriber, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return store.StatusPage{}, nil, false
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return p, nil, false
	}
	list, err := s.store.ListPageSubscribers(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return p, nil, false
	}
	return p, list, true
}

func (s *Server) listSubscribers(w http.ResponseWriter, r *http.Request) {
	_, list, ok := s.subscribersPage(w, r)
	if !ok {
		return
	}
	_, available := s.systemMailer(r.Context())
	items := make([]subscriberView, 0, len(list))
	confirmed, pending := 0, 0
	for _, sub := range list {
		if sub.ConfirmedAt > 0 {
			confirmed++
		} else {
			pending++
		}
		items = append(items, subscriberView{ID: sub.ID, Email: sub.Email, Confirmed: sub.ConfirmedAt > 0,
			CreatedAt: sub.CreatedAt, ConfirmedAt: sub.ConfirmedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": available, "count": confirmed, "pending": pending, "items": items})
}

// csvCell elektronik tabloda formül olarak çalışabilecek hücrelere ' öneki.
func csvCell(v string) string {
	if v != "" && strings.ContainsRune("=+-@\t\r", rune(v[0])) {
		return "'" + v
	}
	return v
}

func (s *Server) exportSubscribers(w http.ResponseWriter, r *http.Request) {
	p, list, ok := s.subscribersPage(w, r)
	if !ok {
		return
	}
	ts := func(t int64) string {
		if t == 0 {
			return ""
		}
		return time.Unix(t, 0).UTC().Format(time.RFC3339)
	}
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="aboneler-%s.csv"`, p.Slug))
	h.Set("Cache-Control", "no-store")
	cw := csv.NewWriter(w)
	cw.Write([]string{"email", "status", "created_at", "confirmed_at"})
	for _, sub := range list {
		status := "pending"
		if sub.ConfirmedAt > 0 {
			status = "confirmed"
		}
		cw.Write([]string{csvCell(sub.Email), status, ts(sub.CreatedAt), ts(sub.ConfirmedAt)})
	}
	cw.Flush()
}

func (s *Server) deleteSubscriber(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	sid, err := strconv.ParseInt(r.PathValue("sid"), 10, 64)
	if err != nil || sid <= 0 {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	p, err := s.store.GetPage(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	sub, err := s.store.GetPageSubscriber(r.Context(), sid)
	if err == nil && sub.PageID != id {
		err = store.ErrNotFound
	}
	if err == nil {
		err = s.store.DeletePageSubscriber(r.Context(), id, sid)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.subs.forget(sid)
	s.audit(r, store.User{}, "status_page.subscriber_delete", "status_page", id, p.Title, sub.Email)
	w.WriteHeader(http.StatusNoContent)
}
