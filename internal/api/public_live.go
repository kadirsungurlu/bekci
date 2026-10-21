package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Herkese açık durum sayfasının canlı akışı (SSE). Akışa olay verisi
// KONMAZ: yalnızca "sayfa değişti" sinyali gider; tarayıcı sayfayı mevcut
// herkese açık uçtan (zaten temizlenmiş veri) yeniden alır. Böylece iç panel
// olaylarının hiçbir alanı (mesaj, hedef, ping, kullanıcı) dışarı sızmaz.
//
// Motorun olayları tek bir iç aboneyle okunur; sayfaya ait monitörü etkileyen
// olaylar kısa bir pencerede biriktirilir (publicLiveFlush), sayfanın önbelleği
// boşaltılır ve o sayfanın izleyicilerine tek sinyal gönderilir.

const (
	publicLiveMaxStreams = 100 // tüm açık genel akışlar (panel akışlarına yer kalsın)
	publicLiveMaxPerIP   = 8
	publicLiveFlush      = 2 * time.Second
	publicLivePing       = 25 * time.Second
)

type publicLive struct {
	mu      sync.Mutex
	started bool
	subs    map[string]map[chan struct{}]bool // kısa ad → izleyici kanalları
	perIP   map[string]int
	total   int
	dirty   map[string]bool    // sinyal bekleyen kısa adlar
	all     bool               // tüm sayfalar sinyal bekliyor (olay/bakım/sayfa değişikliği)
	byMon   map[int64][]string // monitör → yayındaki sayfaların kısa adları
	stale   bool               // byMon yeniden kurulmalı
	flushAt *time.Timer
}

func newPublicLive() *publicLive {
	return &publicLive{subs: map[string]map[chan struct{}]bool{}, perIP: map[string]int{}, dirty: map[string]bool{}, stale: true}
}

// start iç aboneyi ilk izleyiciyle başlatır (hub yoksa hiçbir şey yapmaz).
func (s *Server) startPublicLive() {
	l := s.live
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.started || s.hub == nil {
		return
	}
	ch, _, ok := s.hub.Subscribe(0)
	if !ok {
		return // yer açılınca sonraki izleyici yeniden dener
	}
	l.started = true
	go func() {
		for msg := range ch {
			s.publicLiveEvent(msg)
		}
	}()
}

// publicLiveEvent motor olayını değerlendirir; ilgili sayfaları işaretler.
func (s *Server) publicLiveEvent(msg []byte) {
	var ev struct {
		Type string `json:"type"`
		Data struct {
			MonitorID int64 `json:"monitor_id"`
		} `json:"data"`
	}
	if json.Unmarshal(msg, &ev) != nil {
		return
	}
	l := s.live
	switch ev.Type {
	case "beat", "stats_reset", "locations":
		if ev.Data.MonitorID == 0 {
			return
		}
		l.mu.Lock()
		if len(l.subs) == 0 {
			l.mu.Unlock()
			return
		}
		stale := l.stale
		l.mu.Unlock()
		if stale {
			s.rebuildLiveIndex()
		}
		l.mu.Lock()
		for _, slug := range l.byMon[ev.Data.MonitorID] {
			if len(l.subs[slug]) > 0 {
				l.dirty[slug] = true
			}
		}
		s.scheduleLiveFlushLocked()
		l.mu.Unlock()
	case "incident", "maintenance":
		// Elle açılan olay ve bakım birden çok sayfayı etkileyebilir; seyrek olur.
		s.markAllLive()
	}
}

// markAllLive tüm açık sayfalara sinyal planlar (sayfa ayarı, olay, bakım).
func (s *Server) markAllLive() {
	l := s.live
	if l == nil {
		return
	}
	l.mu.Lock()
	l.stale = true
	if len(l.subs) > 0 {
		l.all = true
		s.scheduleLiveFlushLocked()
	}
	l.mu.Unlock()
}

func (s *Server) rebuildLiveIndex() {
	list, err := s.store.ListPages(context.Background())
	if err != nil {
		return
	}
	m := map[int64][]string{}
	for _, p := range list {
		if !p.Published {
			continue
		}
		for _, id := range p.MonitorIDs() {
			m[id] = append(m[id], p.Slug)
		}
	}
	l := s.live
	l.mu.Lock()
	l.byMon, l.stale = m, false
	l.mu.Unlock()
}

// scheduleLiveFlushLocked bekleyen sinyalleri kısa süre sonra gönderir (l.mu tutulurken).
func (s *Server) scheduleLiveFlushLocked() {
	l := s.live
	if l.flushAt != nil || (!l.all && len(l.dirty) == 0) {
		return
	}
	l.flushAt = time.AfterFunc(publicLiveFlush, s.flushLive)
}

func (s *Server) flushLive() {
	l := s.live
	l.mu.Lock()
	l.flushAt = nil
	var slugs []string
	if l.all {
		for slug := range l.subs {
			slugs = append(slugs, slug)
		}
	} else {
		for slug := range l.dirty {
			slugs = append(slugs, slug)
		}
	}
	l.all, l.dirty = false, map[string]bool{}
	l.mu.Unlock()
	if len(slugs) == 0 {
		return
	}
	// Önce önbellek: sinyali alan tarayıcı taze veriyi bulmalı.
	ps := s.pages
	ps.mu.Lock()
	for _, slug := range slugs {
		delete(ps.cache, slug)
	}
	ps.gen++
	ps.mu.Unlock()
	l.mu.Lock()
	for _, slug := range slugs {
		for ch := range l.subs[slug] {
			select {
			case ch <- struct{}{}:
			default: // zaten bekleyen sinyal var
			}
		}
	}
	l.mu.Unlock()
}

func (l *publicLive) add(slug, ip string) (chan struct{}, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total >= publicLiveMaxStreams || l.perIP[ip] >= publicLiveMaxPerIP {
		return nil, false
	}
	ch := make(chan struct{}, 1)
	if l.subs[slug] == nil {
		l.subs[slug] = map[chan struct{}]bool{}
	}
	l.subs[slug][ch] = true
	l.perIP[ip]++
	l.total++
	return ch, true
}

func (l *publicLive) remove(slug, ip string, ch chan struct{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.subs[slug], ch)
	if len(l.subs[slug]) == 0 {
		delete(l.subs, slug)
	}
	if l.perIP[ip]--; l.perIP[ip] <= 0 {
		delete(l.perIP, ip)
	}
	l.total--
}

// publicEvents GET /api/public/pages/{slug}/events: sayfanın canlı sinyal akışı.
// Yalnızca "data: {"type":"update"}" satırları ve yorum (ping) satırları gönderir.
func (s *Server) publicEvents(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	e, err := s.publicEntry(r.Context(), slug)
	if err != nil {
		s.dbError(w, err)
		return
	}
	if e == nil {
		writeError(w, http.StatusNotFound, "Durum sayfası bulunamadı")
		return
	}
	if e.page.HasPassword && !s.unlocked(r, e.page) {
		writeError(w, http.StatusUnauthorized, "Bu sayfa şifre korumalı")
		return
	}
	ip := clientIP(r)
	ch, ok := s.live.add(slug, ip)
	if !ok {
		writeError(w, http.StatusTooManyRequests, "Çok fazla canlı bağlantı")
		return
	}
	defer s.live.remove(slug, ip, ch)
	s.startPublicLive()

	rc := http.NewResponseController(w)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})
	write := func(line string) bool {
		rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		if _, err := w.Write([]byte(line)); err != nil {
			return false
		}
		return rc.Flush() == nil
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	if !write("retry: 5000\n\n") {
		return
	}
	ping := time.NewTicker(publicLivePing)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			if !write("data: {\"type\":\"update\"}\n\n") {
				return
			}
		case <-ping.C:
			// Sayfa yayından kalktıysa veya şifrelendiyse akış kapanır.
			cur, err := s.publicEntry(r.Context(), slug)
			if err != nil || cur == nil || (cur.page.HasPassword && !s.unlocked(r, cur.page)) {
				return
			}
			if !write(": ping\n\n") {
				return
			}
		}
	}
}
