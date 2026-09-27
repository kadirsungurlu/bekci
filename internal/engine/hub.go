package engine

import (
	"encoding/json"
	"sync"
)

// Eşzamanlı SSE abonesi sınırları: tek bir kullanıcı ya da tüm sistem sınırsız
// bağlantı açıp belleği ve dosya tanıtıcılarını tüketemesin.
const (
	MaxSubsPerUser = 5   // tek kullanıcı kimliği başına
	MaxSubsGlobal  = 200 // tüm kullanıcılar toplamı
)

// Hub canlı olayları (SSE) abonelere dağıtır. Yavaş bir tarayıcı motoru
// bekletmesin diye gönderim bloklamaz; kuyruğu dolan aboneye o olay atlanır.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	perUser map[int64]int // kullanıcı kimliği → açık abone sayısı
	total   int           // tüm açık aboneler
}

func NewHub() *Hub {
	return &Hub{subs: map[chan []byte]struct{}{}, perUser: map[int64]int{}}
}

// Subscribe yeni abone kanalı ve aboneliği bitiren fonksiyonu döner. Sınıra
// ulaşıldıysa ok=false döner ve kanal/gorutin ayrılmaz. userID 0 ise yalnızca
// genel sınır uygulanır (kullanıcı kimliği bilinmeyen dahili aboneler).
func (h *Hub) Subscribe(userID int64) (<-chan []byte, func(), bool) {
	h.mu.Lock()
	if h.total >= MaxSubsGlobal || (userID != 0 && h.perUser[userID] >= MaxSubsPerUser) {
		h.mu.Unlock()
		return nil, nil, false
	}
	ch := make(chan []byte, 256)
	h.subs[ch] = struct{}{}
	h.total++
	if userID != 0 {
		h.perUser[userID]++
	}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.total--
			if userID != 0 {
				if h.perUser[userID]--; h.perUser[userID] <= 0 {
					delete(h.perUser, userID)
				}
			}
			h.mu.Unlock()
		})
	}, true
}

// Publish {"type": typ, "data": data} biçiminde olay yayınlar.
func (h *Hub) Publish(typ string, data any) {
	b, err := json.Marshal(map[string]any{"type": typ, "data": data})
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- b:
		default:
		}
	}
}
