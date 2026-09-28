package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/engine"
	"github.com/kadirsungurlu/uptime-kadir-app/internal/notify"
	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// Kullanıcı başına giden bildirim (test ve örnek) hız sınırı: bir editör
// dışarıya sınırsız istek gönderemesin ya da örnek diziyle mesaj yağmuru
// yapamasın.
const (
	notifTestPerMin     = 10 // test: kullanıcı başına dakikada
	notifSampleInterval = 60 // örnek dizisi: kullanıcı başına saniyede bir kez
)

// notifyLimiter test (dakikalık sabit pencere) ve örnek (asgari aralık)
// isteklerini kullanıcı kimliğine göre sınırlar.
type notifyLimiter struct {
	mu      sync.Mutex
	test    map[int64]*notifWindow
	samples map[int64]int64 // kullanıcı → son örnek dizisi zamanı (unix)
}

type notifWindow struct {
	start int64
	count int
}

func newNotifyLimiter() *notifyLimiter {
	return &notifyLimiter{test: map[int64]*notifWindow{}, samples: map[int64]int64{}}
}

func (l *notifyLimiter) allowTest(uid int64, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	minute := now.Unix() / 60
	w := l.test[uid]
	if w == nil || w.start != minute {
		w = &notifWindow{start: minute}
		l.test[uid] = w
	}
	w.count++
	return w.count <= notifTestPerMin
}

// allowSample izin verirse zamanı kaydeder; aksi halde kalan bekleme (saniye).
func (l *notifyLimiter) allowSample(uid int64, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	sec := now.Unix()
	if last, ok := l.samples[uid]; ok && sec-last < notifSampleInterval {
		return false, int(notifSampleInterval - (sec - last))
	}
	l.samples[uid] = sec
	return true, 0
}

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
	if !s.notifyRL.allowTest(userFrom(r).ID, s.now()) {
		w.Header().Set("Retry-After", "60")
		writeError(w, http.StatusTooManyRequests, "Çok fazla test isteği; bir dakika sonra tekrar deneyin")
		return
	}
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

// sampleNotifications kayıtlı kanala her bildirim türünden birer örnek
// gönderir (arka planda, sırayla). Adlar paneldeki ilk monitör ve sunucudan
// alınır; mesajlarda "örnek bildirim" notu vardır. Sonuçlar loga yazılır.
func (s *Server) sampleNotifications(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ch, err := s.store.GetNotification(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Örnek dizisi ~10 mesaj gönderir; kullanıcı başına en fazla 60 saniyede bir.
	if ok, wait := s.notifyRL.allowSample(userFrom(r).ID, s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(wait))
		writeError(w, http.StatusTooManyRequests, "Örnek bildirimler çok sık istendi; "+strconv.Itoa(wait)+" saniye sonra tekrar deneyin")
		return
	}
	n := notify.SampleNames{}
	if mons, err := s.store.ListMonitors(r.Context()); err == nil {
		for _, m := range mons {
			if m.Type == "http" {
				n.Monitor, n.MonitorID, n.Target = m.Name, m.ID, engine.Target(m)
				n.MonitorURL = s.engine.MonitorURL(m.ID)
				break
			}
		}
	}
	if list, err := s.store.ListProbesOfKind(r.Context(), store.ProbeKindServer); err == nil && len(list) > 0 {
		p := list[0]
		v := s.servers.View(r.Context(), p, nil, true)
		n.Server, n.ServerID, n.ServerURL = p.Name, p.ID, s.servers.URL(p.ID)
		if v.Host != nil {
			n.Host = v.Host.Hostname
		}
		if v.Latest != nil && len(v.Latest.Disks) > 0 {
			n.DiskMount = v.Latest.Disks[0].Mount
		}
	}
	events := notify.SampleEvents(n, s.now())
	name := ch.Name
	s.notifier.SendSamples(ch.Type, ch.Config, events, 2*time.Second, func(ev notify.Event, err error) {
		if err != nil {
			s.log.Warn("örnek bildirim gönderilemedi", "kanal", name, "olay", ev.Kind, "metrik", ev.Metric, "hata", err)
			return
		}
		s.log.Info("örnek bildirim gönderildi", "kanal", name, "olay", ev.Kind, "metrik", ev.Metric)
	})
	s.audit(r, store.User{}, "notification.samples", "notification", id, ch.Name, fmt.Sprintf("%d örnek", len(events)))
	writeJSON(w, http.StatusAccepted, map[string]int{"count": len(events)})
}
