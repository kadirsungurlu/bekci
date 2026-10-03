package api

// Oturum / cihaz yönetimi (#13).
//
//	GET    /api/auth/sessions          kullanıcının açık oturumları (cihaz, IP, son görülme; current işaretli)
//	DELETE /api/auth/sessions/{id}     tek oturumu kapat
//	DELETE /api/auth/sessions          bu oturum dışındaki hepsini kapat
//
// Son görülme zamanı her istekte değil, oturum başına en fazla 5 dakikada bir
// yazılır (SQLite tek yazıcı; yazma yağmuru olmasın).

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

const sessionTouchEvery = 5 * time.Minute

// sessionTouches oturum özeti → son yazılan görülme zamanı (bellekte).
type sessionTouches struct {
	mu sync.Mutex
	m  map[string]time.Time
}

func newSessionTouches() *sessionTouches { return &sessionTouches{m: map[string]time.Time{}} }

// due oturumun son görülme zamanı yazılmalı mı (ve yazıldı say).
func (t *sessionTouches) due(hash string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.m[hash]; ok && now.Sub(last) < sessionTouchEvery {
		return false
	}
	if len(t.m) > 20000 {
		for k, v := range t.m {
			if now.Sub(v) > sessionTouchEvery {
				delete(t.m, k)
			}
		}
	}
	t.m[hash] = now
	return true
}

func (t *sessionTouches) forget(hash string) {
	t.mu.Lock()
	delete(t.m, hash)
	t.mu.Unlock()
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/auth/sessions", s.auth(s.listSessions))
		mux.Handle("DELETE /api/auth/sessions/{id}", s.auth(s.revokeSession))
		mux.Handle("DELETE /api/auth/sessions", s.auth(s.revokeOtherSessions))
	})
}

// touchSession istek bir oturum çereziyle geldiyse son görülme zamanını (seyrek) yazar.
func (s *Server) touchSession(r *http.Request, hash string) {
	if hash == "" {
		return
	}
	now := s.now()
	if !s.sessTouch.due(hash, now) {
		return
	}
	if err := s.store.TouchSession(r.Context(), hash, now.Unix(), clientIP(r)); err != nil {
		s.log.Debug("oturum son görülme yazılamadı", "hata", err)
	}
}

// sessionInfo yeni oturumun cihaz bilgisi (User-Agent kısaltılır).
func sessionInfo(r *http.Request, via string) store.SessionInfo {
	ua := r.UserAgent()
	if len(ua) > 200 {
		ua = ua[:200]
	}
	return store.SessionInfo{IP: clientIP(r), UserAgent: ua, Via: via}
}

func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cur, _ := r.Context().Value(sessionKey).(string)
	list, err := s.store.ListSessions(r.Context(), u.ID, cur, s.now().Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	if u.APIKeyName != "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	id := r.PathValue("id")
	if len(id) != 16 {
		writeError(w, http.StatusNotFound, "Bulunamadı")
		return
	}
	if err := s.store.DeleteSessionByID(r.Context(), u.ID, id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, u, "user.session_revoke", "user", u.ID, u.Username, "1 oturum")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) revokeOtherSessions(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	cur, _ := r.Context().Value(sessionKey).(string)
	if u.APIKeyName != "" || cur == "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	before, err := s.store.ListSessions(r.Context(), u.ID, cur, s.now().Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteOtherSessions(r.Context(), u.ID, cur); err != nil {
		s.dbError(w, err)
		return
	}
	n := max(0, len(before)-1)
	s.audit(r, u, "user.session_revoke", "user", u.ID, u.Username, strconv.Itoa(n)+" oturum")
	writeJSON(w, http.StatusOK, map[string]int{"revoked": n})
}
