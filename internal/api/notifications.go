package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

type notificationInput struct {
	Name          string          `json:"name"`
	Type          string          `json:"type"`
	Config        json.RawMessage `json:"config"`
	IsDefault     bool            `json:"is_default"`
	Active        *bool           `json:"active"`
	ApplyExisting bool            `json:"apply_existing"` // mevcut tüm monitörlere bağla
}

func (in *notificationInput) toNotification() (store.Notification, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return store.Notification{}, errors.New("Ad 1-100 karakter olmalı")
	}
	p, ok := notify.Get(in.Type)
	if !ok {
		return store.Notification{}, errors.New("Geçersiz bildirim tipi")
	}
	cfg, err := p.Normalize(in.Config)
	if err != nil {
		return store.Notification{}, err
	}
	active := true
	if in.Active != nil {
		active = *in.Active
	}
	return store.Notification{Name: in.Name, Type: in.Type, Config: cfg, IsDefault: in.IsDefault, Active: active}, nil
}

func masked(n store.Notification) store.Notification {
	n.Config = notify.MaskSecrets(n.Type, n.Config)
	return n
}

func (s *Server) listNotifications(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListNotifications(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	for i := range list {
		list[i] = masked(list[i])
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) createNotification(w http.ResponseWriter, r *http.Request) {
	var in notificationInput
	if !readJSON(w, r, &in) {
		return
	}
	n, err := in.toNotification()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.CreateNotification(r.Context(), &n, in.ApplyExisting); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "notification.create", "notification", n.ID, n.Name, n.Type)
	writeJSON(w, http.StatusCreated, masked(n))
}

func (s *Server) updateNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetNotification(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in notificationInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Type == old.Type {
		merged, err := notify.MergeSecrets(in.Type, in.Config, old.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Config = merged
	}
	n, err := in.toNotification()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n.ID, n.CreatedAt = id, old.CreatedAt
	if err := s.store.UpdateNotification(r.Context(), &n, in.ApplyExisting); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "notification.update", "notification", n.ID, n.Name, n.Type)
	writeJSON(w, http.StatusOK, masked(n))
}

func (s *Server) deleteNotification(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetNotification(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.store.DeleteNotification(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "notification.delete", "notification", id, old.Name, old.Type)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// testNotification formdaki (henüz kaydedilmemiş olabilir) ayarla test gönderir.
// Kayıtlı bir kanal düzenleniyorsa maskeli gizli alanlar kayıttan tamamlanır.
func (s *Server) testNotification(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID     int64           `json:"id"`
		Type   string          `json:"type"`
		Config json.RawMessage `json:"config"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	p, ok := notify.Get(in.Type)
	if !ok {
		writeError(w, http.StatusBadRequest, "Geçersiz bildirim tipi")
		return
	}
	cfg := in.Config
	if in.ID > 0 {
		old, err := s.store.GetNotification(r.Context(), in.ID)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if old.Type == in.Type {
			merged, err := notify.MergeSecrets(in.Type, cfg, old.Config)
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			cfg = merged
		}
	}
	cfg, err := p.Normalize(cfg)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.notifier.Test(in.Type, cfg); err != nil {
		// 502 değil: önündeki proxy (Cloudflare vb.) 502 gövdesini kendi sayfasıyla
		// değiştirebilir ve arayüz hatanın nedenini göremez.
		writeError(w, http.StatusUnprocessableEntity, "Gönderilemedi: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
