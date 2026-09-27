package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// summary liste ekranının sağındaki "Mevcut durum" ve "Son 24 saat" kutuları.
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.store.ListMonitors(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now()
	hourly, err := s.store.HourlyAll(r.Context(), now.Unix()-now.Unix()%3600-23*3600)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var up, down, pending, paused int
	var sumUp, sumDown int64
	for _, m := range monitors {
		if !m.Active {
			paused++
			continue
		}
		switch m.Status {
		case store.StatusUp:
			up++
		case store.StatusDown:
			down++
		default:
			pending++
		}
		for _, b := range hourly[m.ID] {
			sumUp += b.Up
			sumDown += b.Down
		}
	}
	incidents, err := s.store.CountIncidentsSince(r.Context(), now.Add(-24*time.Hour).Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	var uptime *float64
	if sumUp+sumDown > 0 {
		pct := 100 * float64(sumUp) / float64(sumUp+sumDown)
		uptime = &pct
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": len(monitors), "up": up, "down": down, "pending": pending, "paused": paused,
		"uptime_24h": uptime, "incidents_24h": incidents,
	})
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	list, err := s.store.ListIncidents(r.Context(), store.IncidentFilter{Before: before, Limit: limit})
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.LoadSettings(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in store.AppSettings
	if !readJSON(w, r, &in) {
		return
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SaveSettings(r.Context(), in); err != nil {
		s.dbError(w, err)
		return
	}
	s.engine.SetSettings(in)
	writeJSON(w, http.StatusOK, in)
}

// push: /api/push/{token}?status=up|down&msg=...&ping=123
// Cron işleri ve betikler tarafından çağrılır; oturum gerektirmez.
func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	up := q.Get("status") != "down"
	msg := strings.ToValidUTF8(q.Get("msg"), "�")
	if r := []rune(msg); len(r) > 250 {
		msg = string(r[:250]) // bayta göre kesmek çok baytlı harfleri bozardı
	}
	ping := int64(-1)
	if p, err := strconv.ParseInt(q.Get("ping"), 10, 64); err == nil && p >= 0 {
		ping = p
	}
	err := s.engine.Push(r.Context(), r.PathValue("token"), up, msg, ping)
	switch {
	case errors.Is(err, engine.ErrPushNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	case errors.Is(err, engine.ErrPushPaused):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "sunucu hatası"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// events canlı olay akışı (Server-Sent Events).
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// Bu bağlantı uzun ömürlü; sunucunun genel okuma/yazma süre sınırları
	// uygulanmaz. (Okuma sınırı dolunca net/http isteğin context'ini iptal
	// ederek akışı keser.)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, unsubscribe := s.hub.Subscribe()
	defer unsubscribe()

	fmt.Fprint(w, "retry: 5000\n\n")
	if rc.Flush() != nil {
		return
	}
	// Cloudflare ve proxy'ler boşta kalan bağlantıyı kesmesin diye düzenli yorum satırı.
	keepAlive := time.NewTicker(25 * time.Second)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
		case <-keepAlive.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		}
		if rc.Flush() != nil {
			return
		}
	}
}
