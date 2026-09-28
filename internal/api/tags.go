package api

// Etiketler: yönetim (editör+), listeleme (her kullanıcı) ve monitör etiketleri.

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/store"
)

const (
	defaultTagColor    = "#64748b"
	maxTagNameLen      = 50
	maxTagValueLen     = 100
	maxTagsPerMonitor  = 50
	maxTagsTotal       = 1000
	tagNotFoundMessage = "Seçilen etiket bulunamadı"
)

var tagColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/tags", s.auth(s.listTags))
		mux.Handle("POST /api/tags", s.editor(s.createTag))
		mux.Handle("PUT /api/tags/{id}", s.editor(s.updateTag))
		mux.Handle("DELETE /api/tags/{id}", s.editor(s.deleteTag))
		mux.Handle("PUT /api/monitors/{id}/tags", s.editor(s.setMonitorTags))
	})
}

// normalizeTag etiket adını ve rengini doğrular; renk boşsa varsayılan kullanılır.
func normalizeTag(name, color string) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > maxTagNameLen {
		return "", "", errors.New("Etiket adı 1-50 karakter olmalı")
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", "", errors.New("Etiket adı kontrol karakteri içeremez")
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = defaultTagColor
	}
	if !tagColorRe.MatchString(color) {
		return "", "", errors.New("Renk #rrggbb biçiminde olmalı (ör. #2563eb)")
	}
	return name, strings.ToLower(color), nil
}

func normalizeTagValue(v string) (string, error) {
	v = strings.TrimSpace(v)
	if utf8.RuneCountInString(v) > maxTagValueLen {
		return "", errors.New("Etiket değeri en fazla 100 karakter olabilir")
	}
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", errors.New("Etiket değeri kontrol karakteri içeremez")
	}
	return v, nil
}

type tagView struct {
	store.Tag
	MonitorCount int `json:"monitor_count"`
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	links, err := s.store.MonitorTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Kısıtlı izleyici yalnızca görebildiği monitörleri sayar.
	u := userFrom(r)
	vis := visibleTo(u)
	counts := map[int64]int{}
	for mid, list := range links {
		if !vis.can(mid) {
			continue
		}
		seen := map[int64]bool{}
		for _, t := range list {
			if !seen[t.ID] {
				seen[t.ID] = true
				counts[t.ID]++
			}
		}
	}
	// Kısıtlı izleyiciye yalnızca görebildiği monitörlerde kullanılan etiketler
	// döner; hiç kullanmadığı etiketlerin adı/değeri/rengi sızmaz. Kısıtsız
	// kullanıcıda tüm etiketler (sayı 0 olsa da) görünür.
	restricted := u.Restricted()
	out := make([]tagView, 0, len(tags))
	for _, t := range tags {
		if restricted && counts[t.ID] == 0 {
			continue
		}
		out = append(out, tagView{Tag: t, MonitorCount: counts[t.ID]})
	}
	writeJSON(w, http.StatusOK, out)
}

type tagInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (s *Server) writeTagError(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrTagExists) {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	s.dbError(w, err)
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var in tagInput
	if !readJSON(w, r, &in) {
		return
	}
	name, color, err := normalizeTag(in.Name, in.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if len(tags) >= maxTagsTotal {
		writeError(w, http.StatusBadRequest, "En fazla 1000 etiket oluşturulabilir")
		return
	}
	t := store.Tag{Name: name, Color: color}
	if err := s.store.CreateTag(r.Context(), &t); err != nil {
		s.writeTagError(w, err)
		return
	}
	s.audit(r, store.User{}, "tag.create", "tag", t.ID, t.Name, t.Color)
	writeJSON(w, http.StatusCreated, tagView{Tag: t})
}

func (s *Server) updateTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetTag(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in tagInput
	if !readJSON(w, r, &in) {
		return
	}
	name, color, err := normalizeTag(in.Name, in.Color)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	t := store.Tag{ID: id, Name: name, Color: color, CreatedAt: old.CreatedAt}
	if err := s.store.UpdateTag(r.Context(), &t); err != nil {
		s.writeTagError(w, err)
		return
	}
	detail := ""
	if old.Name != t.Name {
		detail = "eski ad: " + old.Name
	}
	s.audit(r, store.User{}, "tag.update", "tag", id, t.Name, detail)
	writeJSON(w, http.StatusOK, tagView{Tag: t})
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetTag(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteTag(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "tag.delete", "tag", id, old.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// setMonitorTags: PUT /api/monitors/{id}/tags, gövde [{tag_id, value}]; listeyi
// tamamen değiştirir (boş liste tüm etiketleri kaldırır).
func (s *Server) setMonitorTags(w http.ResponseWriter, r *http.Request) {
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
	var in []store.MonitorTagInput
	if !readJSON(w, r, &in) {
		return
	}
	if len(in) > maxTagsPerMonitor {
		writeError(w, http.StatusBadRequest, "Bir monitöre en fazla 50 etiket eklenebilir")
		return
	}
	tags, err := s.store.ListTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	exists := make(map[int64]bool, len(tags))
	for _, t := range tags {
		exists[t.ID] = true
	}
	seen := map[store.MonitorTagInput]bool{}
	list := make([]store.MonitorTagInput, 0, len(in))
	for _, t := range in {
		if !exists[t.TagID] {
			writeError(w, http.StatusBadRequest, tagNotFoundMessage)
			return
		}
		if t.Value, err = normalizeTagValue(t.Value); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !seen[t] {
			seen[t] = true
			list = append(list, t)
		}
	}
	if err := s.store.SetMonitorTags(r.Context(), id, list); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "monitor.tags", "monitor", id, m.Name, strconv.Itoa(len(list))+" etiket")
	all, err := s.store.MonitorTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := all[id]
	if out == nil {
		out = []store.MonitorTag{}
	}
	writeJSON(w, http.StatusOK, out)
}

// filterByTag ?tag=<id> filtresi: yalnızca o etikete sahip monitörler kalır.
// Hata olursa yanıtı kendisi yazar ve false döner.
func (s *Server) filterByTag(w http.ResponseWriter, r *http.Request, monitors []store.Monitor, raw string) ([]store.Monitor, bool) {
	tagID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || tagID <= 0 {
		writeError(w, http.StatusBadRequest, "Geçersiz etiket")
		return nil, false
	}
	links, err := s.store.MonitorTags(r.Context())
	if err != nil {
		s.dbError(w, err)
		return nil, false
	}
	out := monitors[:0:0]
	for _, m := range monitors {
		for _, t := range links[m.ID] {
			if t.ID == tagID {
				out = append(out, m)
				break
			}
		}
	}
	return out, true
}
