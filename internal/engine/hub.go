package engine

import (
	"encoding/json"
	"sync"
)

// Hub canlı olayları (SSE) abonelere dağıtır. Yavaş bir tarayıcı motoru
// bekletmesin diye gönderim bloklamaz; kuyruğu dolan aboneye o olay atlanır.
type Hub struct {
	mu   sync.Mutex
	subs map[chan []byte]struct{}
}

func NewHub() *Hub { return &Hub{subs: map[chan []byte]struct{}{}} }

// Subscribe yeni abone kanalı ve aboneliği bitiren fonksiyonu döner.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 256)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
	}
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
