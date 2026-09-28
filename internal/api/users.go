package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// İşlem kaydı ----------------------------------------------------------------------

// audit işlem kaydına satır ekler. actor boşsa istekteki kullanıcı kullanılır.
// Kayıt hatası isteği bozmaz, sadece loglanır.
func (s *Server) audit(r *http.Request, actor store.User, action, targetType string, targetID int64, targetName, detail string) {
	if actor.ID == 0 {
		actor = userFrom(r)
	}
	err := s.store.AddAudit(r.Context(), store.AuditEntry{
		UserID: actor.ID, Username: actor.Username, Action: action, TargetType: targetType,
		TargetID: targetID, TargetName: targetName, Detail: detail, IP: clientIP(r),
	})
	if err != nil {
		s.log.Error("işlem kaydı yazılamadı", "işlem", action, "hata", err)
	}
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := s.store.ListAudit(r.Context(), before, limit)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// Müşteri kısıtı --------------------------------------------------------------------

// visibility istekteki kullanıcının hangi monitörleri görebileceğini döner.
// all=true ise kısıt yoktur; aksi halde yalnızca ids içindekiler görünür.
type visibility struct {
	all     bool
	ids     map[int64]bool
	servers map[int64]bool // görebileceği sunucular (kısıtlıysa)
}

func (v visibility) can(id int64) bool { return v.all || v.ids[id] }

// canServer sunucunun (sunucu takibi) görülebilir olup olmadığını söyler.
func (v visibility) canServer(id int64) bool { return v.all || v.servers[id] }

func (v visibility) list() []int64 {
	out := make([]int64, 0, len(v.ids))
	for id := range v.ids {
		out = append(out, id)
	}
	return out
}

func visibleTo(u store.User) visibility {
	if !u.Restricted() {
		return visibility{all: true}
	}
	ids := make(map[int64]bool, len(u.MonitorIDs))
	for _, id := range u.MonitorIDs {
		ids[id] = true
	}
	servers := make(map[int64]bool, len(u.ServerIDs))
	for _, id := range u.ServerIDs {
		servers[id] = true
	}
	return visibility{ids: ids, servers: servers}
}

// canSeeConfig: monitör ayarları (başlıklar, gövde, şifreler), push token'ı ve
// bildirim bağlantıları yalnızca editör ve yöneticiye gösterilir.
func canSeeConfig(u store.User) bool {
	return store.RoleRank(u.Role) >= store.RoleRank(store.RoleEditor)
}

// canSeeCapture: olayı açan kontrolün ham istek/yanıt yakalaması (metot, adres,
// yanıt gövdesi ve başlıklar) yalnızca YÖNETİCİYE gösterilir. Editör olayın
// kendisini, işlem geçmişini ve kök nedeni görür ama bu ham yakalamayı görmez;
// böylece iç servislere yöneltilen kontrollerin yanıtları editörden sızmaz.
func canSeeCapture(u store.User) bool {
	return store.RoleRank(u.Role) >= store.RoleRank(store.RoleAdmin)
}

// Kullanıcı yönetimi (yönetici) -----------------------------------------------------

type userInput struct {
	Username    string  `json:"username"`
	DisplayName string  `json:"display_name"`
	Role        string  `json:"role"`
	Password    string  `json:"password"` // sadece eklemede
	Disabled    bool    `json:"disabled"`
	AllMonitors *bool   `json:"all_monitors"`
	MonitorIDs  []int64 `json:"monitor_ids"`
	ServerIDs   []int64 `json:"server_ids"` // kısıtlı izleyicinin görebileceği sunucular
}

// normalize ortak alanları doğrular ve store.User'a çevirir.
func (s *Server) normalizeUser(r *http.Request, in *userInput) (store.User, error) {
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	if utf8.RuneCountInString(in.DisplayName) > 100 {
		return store.User{}, errors.New("Görünen ad en fazla 100 karakter olabilir")
	}
	if store.RoleRank(in.Role) == 0 {
		return store.User{}, errors.New("Rol yönetici (admin), editör (editor) veya izleyici (viewer) olmalı")
	}
	u := store.User{DisplayName: in.DisplayName, Role: in.Role, Disabled: in.Disabled, AllMonitors: true, MonitorIDs: []int64{}, ServerIDs: []int64{}}
	// Monitör kısıtı sadece izleyicide anlamlı; diğer roller her şeyi görür.
	if in.Role == store.RoleViewer && in.AllMonitors != nil && !*in.AllMonitors {
		u.AllMonitors = false
		existing, err := s.store.MonitorsByIDs(r.Context(), in.MonitorIDs)
		if err != nil {
			return u, err
		}
		for _, id := range in.MonitorIDs {
			if _, ok := existing[id]; !ok {
				return u, errors.New("Seçilen monitörlerden biri bulunamadı")
			}
		}
		u.MonitorIDs = in.MonitorIDs
		servers, err := s.store.ProbesByIDs(r.Context(), in.ServerIDs)
		if err != nil {
			return u, err
		}
		for _, id := range in.ServerIDs {
			if p, ok := servers[id]; !ok || p.Kind != store.ProbeKindServer {
				return u, errors.New("Seçilen sunuculardan biri bulunamadı")
			}
		}
		if in.ServerIDs != nil {
			u.ServerIDs = in.ServerIDs
		}
	}
	return u, nil
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var in userInput
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
	u, err := s.normalizeUser(r, &in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if _, err := s.store.UserByName(r.Context(), in.Username); err == nil {
		writeError(w, http.StatusConflict, "Bu kullanıcı adı zaten kullanılıyor")
		return
	}
	u.Username = in.Username
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.CreateUser(r.Context(), &u, string(hash)); err != nil {
		if store.IsUniqueViolation(err) {
			writeError(w, http.StatusConflict, "Bu kullanıcı adı zaten kullanılıyor")
			return
		}
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "user.create", "user", u.ID, u.Username, "rol: "+u.Role)
	s.respondUser(w, r, u.ID, http.StatusCreated)
}

func (s *Server) respondUser(w http.ResponseWriter, r *http.Request, id int64, status int) {
	u, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, status, u)
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in userInput
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.normalizeUser(r, &in)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	me := userFrom(r)
	if id == me.ID && (u.Disabled || u.Role != old.Role) {
		writeError(w, http.StatusBadRequest, "Kendi rolünüzü değiştiremez veya hesabınızı devre dışı bırakamazsınız")
		return
	}
	u.ID, u.Username = id, old.Username
	if err := s.store.UpdateUser(r.Context(), &u); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			writeError(w, http.StatusBadRequest, "En az bir aktif yönetici kalmalı")
			return
		}
		s.dbError(w, err)
		return
	}
	detail := "rol: " + u.Role
	if u.Disabled {
		detail += ", devre dışı"
	}
	s.audit(r, store.User{}, "user.update", "user", u.ID, u.Username, detail)
	s.respondUser(w, r, id, http.StatusOK)
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if id == userFrom(r).ID {
		writeError(w, http.StatusBadRequest, "Kendi hesabınızı silemezsiniz")
		return
	}
	old, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			writeError(w, http.StatusBadRequest, "En az bir aktif yönetici kalmalı")
			return
		}
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "user.delete", "user", id, old.Username, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// resetUserPassword yöneticinin verdiği geçici şifre; kullanıcı ilk girişte
// değiştirmek zorunda, açık oturumları kapanır.
func (s *Server) resetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if err := validatePassword(in.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.store.UserByID(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcryptCost)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.SetPassword(r.Context(), id, string(hash), true); err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteUserSessions(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "user.password_reset", "user", id, u.Username, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
