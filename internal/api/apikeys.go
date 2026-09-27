package api

import (
	"crypto/rand"
	"math/big"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// API anahtarları -----------------------------------------------------------------
//
// Biçim: "upk_" + 43 karakter base62 (~256 bit). Anahtar yalnızca oluşturulurken
// bir kez gösterilir; veritabanında SHA-256 özeti tutulur. Kullanım:
//
//	Authorization: Bearer upk_...
//
// Etkin kullanıcı anahtarın sahibidir; rolü min(sahibin güncel rolü, anahtarın
// rolü). Sahibi devre dışı bırakılan/silinen anahtar geçersizdir. Anahtarla
// şifre değişimi zorunluluğu uygulanmaz (anahtar ayrı bir kimlik bilgisidir,
// oturum açma akışı değildir; anahtar ancak şifre değiştirildikten sonra
// oluşturulabilir). İki adımlı doğrulama da anahtarı etkilemez.
//
// Anahtar yönetimi (/api/api-keys), hesap güvenliği uç noktaları (/api/auth/…:
// şifre değişimi, iki adımlı doğrulama) ve iki adımlı doğrulama sıfırlama
// anahtarla kullanılamaz, gerçek oturum gerekir: sızan bir anahtar kendini
// çoğaltamaz veya hesabı ele geçiremez.

const (
	apiKeyPrefix     = "upk_"
	apiKeyRandomLen  = 43 // base62; 62^43 ≈ 2^256
	apiKeyShownLen   = 12 // arayüzde gösterilen baş kısım: "upk_" + 8 karakter
	apiKeyMaxPerUser = 50
	apiKeyTouchEvery = 60 // son kullanım zamanı en fazla dakikada bir yazılır
)

const base62 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

func newAPIKeySecret() string {
	var b strings.Builder
	b.WriteString(apiKeyPrefix)
	max := big.NewInt(int64(len(base62)))
	for range apiKeyRandomLen {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err) // crypto/rand hata vermez (Go 1.24+)
		}
		b.WriteByte(base62[n.Int64()])
	}
	return b.String()
}

func init() {
	RegisterAuthenticator(apiKeyAuthenticator)
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/api-keys", s.auth(s.listAPIKeys))
		mux.Handle("POST /api/api-keys", s.auth(s.createAPIKey))
		mux.Handle("DELETE /api/api-keys/{id}", s.auth(s.revokeAPIKey))
	})
}

// bearerAPIKey Authorization: Bearer upk_… başlığındaki anahtar.
func bearerAPIKey(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, strings.HasPrefix(token, apiKeyPrefix)
}

// isAPIKeyRequest CSRF muafiyeti için: istek API anahtarı taşıyor ve oturum
// çerezi taşımıyor. Tarayıcılar Authorization: Bearer başlığını başka bir
// siteden kendiliğinden eklemez (eklemek CORS ön kontrolü gerektirir), bu
// yüzden böyle bir istek CSRF ile üretilemez. Çerez de varsa muafiyet yok:
// role() önce çereze baktığından istek oturumla yetkilendirilirdi.
func isAPIKeyRequest(r *http.Request) bool {
	if _, ok := bearerAPIKey(r); !ok {
		return false
	}
	_, err := r.Cookie(sessionCookie)
	return err != nil
}

// sessionOnly anahtarla kullanılamayan, gerçek oturum gerektiren yollar.
// Yedek dışa/içe aktarma da buradadır: dışa aktarma tüm gizli bilgileri ve
// sayfa şifre özetlerini açık verir; sızan bir API anahtarı her şeyi
// dökememeli. Tarayıcı bu uçları oturum çereziyle kullandığından etkilenmez.
func sessionOnly(path string) bool {
	return strings.HasPrefix(path, "/api/auth/") ||
		path == "/api/api-keys" || strings.HasPrefix(path, "/api/api-keys/") ||
		path == "/api/export" || strings.HasPrefix(path, "/api/import") ||
		strings.Contains(path, "/2fa/")
}

func apiKeyAuthenticator(s *Server, r *http.Request) (store.User, bool) {
	secret, ok := bearerAPIKey(r)
	if !ok || sessionOnly(r.URL.Path) {
		return store.User{}, false
	}
	return s.userForAPIKey(r, secret)
}

// userForAPIKey anahtarı doğrular ve etkin kullanıcıyı döner.
func (s *Server) userForAPIKey(r *http.Request, secret string) (store.User, bool) {
	if !strings.HasPrefix(secret, apiKeyPrefix) || len(secret) > 100 {
		return store.User{}, false
	}
	ctx := r.Context()
	now := s.now().Unix()
	k, err := s.store.APIKeyByHash(ctx, hashToken(secret))
	if err != nil || !k.Usable(now) {
		return store.User{}, false
	}
	u, err := s.store.UserByID(ctx, k.UserID)
	if err != nil || u.Disabled {
		return store.User{}, false
	}
	if store.RoleRank(k.Role) < store.RoleRank(u.Role) {
		u.Role = k.Role
	}
	u.MustChangePassword = false
	if now-k.LastUsedAt >= apiKeyTouchEvery {
		if err := s.store.TouchAPIKey(ctx, k.ID, now); err != nil {
			s.log.Warn("API anahtarı son kullanım zamanı yazılamadı", "hata", err)
		}
	}
	return u, true
}

// apiKeyView arayüze giden anahtar bilgisi (özet ve anahtarın kendisi yok).
type apiKeyView struct {
	store.APIKey
	Status string `json:"status"` // active | expired | revoked
}

func (s *Server) viewAPIKey(k store.APIKey) apiKeyView {
	st := "active"
	switch {
	case k.RevokedAt != 0:
		st = "revoked"
	case !k.Usable(s.now().Unix()):
		st = "expired"
	}
	return apiKeyView{APIKey: k, Status: st}
}

// listAPIKeys kullanıcının kendi anahtarları; yönetici ?all=1 ile herkesinkini görür.
func (s *Server) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	owner := u.ID
	if r.URL.Query().Get("all") == "1" {
		if u.Role != store.RoleAdmin {
			writeError(w, http.StatusForbidden, "Bu işlem için yetkiniz yok")
			return
		}
		owner = 0
	}
	list, err := s.store.ListAPIKeys(r.Context(), owner)
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := make([]apiKeyView, len(list))
	for i, k := range list {
		out[i] = s.viewAPIKey(k)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) createAPIKey(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	var in struct {
		Name      string `json:"name"`
		Role      string `json:"role"`
		ExpiresAt int64  `json:"expires_at"` // unix saniye; 0/boş: süresiz
	}
	if !readJSON(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	now := s.now().Unix()
	switch {
	case in.Name == "" || utf8.RuneCountInString(in.Name) > 100:
		writeError(w, http.StatusBadRequest, "Anahtar adı 1-100 karakter olmalı")
		return
	case store.RoleRank(in.Role) == 0:
		writeError(w, http.StatusBadRequest, "Rol yönetici (admin), editör (editor) veya izleyici (viewer) olmalı")
		return
	case store.RoleRank(in.Role) > store.RoleRank(u.Role):
		writeError(w, http.StatusBadRequest, "Anahtara kendi rolünüzden yüksek bir yetki veremezsiniz")
		return
	case in.ExpiresAt < 0 || (in.ExpiresAt != 0 && in.ExpiresAt <= now):
		writeError(w, http.StatusBadRequest, "Son kullanma tarihi gelecekte olmalı")
		return
	case in.ExpiresAt > now+10*365*86400:
		writeError(w, http.StatusBadRequest, "Son kullanma tarihi en fazla 10 yıl sonrası olabilir")
		return
	}
	n, err := s.store.CountUsableAPIKeys(r.Context(), u.ID, now)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if n >= apiKeyMaxPerUser {
		writeError(w, http.StatusBadRequest, "En fazla 50 geçerli API anahtarınız olabilir; kullanmadıklarınızı iptal edin")
		return
	}
	secret := newAPIKeySecret()
	k := store.APIKey{
		UserID: u.ID, Username: u.Username, Name: in.Name, Prefix: secret[:apiKeyShownLen],
		Role: in.Role, CreatedAt: now, ExpiresAt: in.ExpiresAt, Hash: hashToken(secret),
	}
	if err := s.store.CreateAPIKey(r.Context(), &k); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "apikey.create", "apikey", k.ID, k.Name, "rol: "+k.Role)
	writeJSON(w, http.StatusCreated, map[string]any{"key": s.viewAPIKey(k), "secret": secret})
}

// revokeAPIKey anahtarı iptal eder: sahibi veya yönetici. Başkasının anahtarı
// yöneticiye değilse 404 (varlığı da söylenmez).
func (s *Server) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	u := userFrom(r)
	k, err := s.store.GetAPIKey(r.Context(), id)
	if err == nil && k.UserID != u.ID && u.Role != store.RoleAdmin {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.RevokeAPIKey(r.Context(), id, s.now().Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	detail := ""
	if k.UserID != u.ID {
		detail = "sahibi: " + k.Username
	}
	s.audit(r, store.User{}, "apikey.revoke", "apikey", k.ID, k.Name, detail)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
