package engine

import (
	"encoding/json"
	"sync"
)

// Eşzamanlı SSE abonesi sınırları: tek bir kullanıcı ya da tüm sistem sınırsız
// bağlantı açıp belleği ve dosya tanıtıcılarını tüketemesin.
const (
	// MaxSubsPerUser tek kullanıcı kimliği başına açık akış. Sınıra ulaşan
	// kullanıcının yeni bağlantısı reddedilmez; en eski akışı kapatılır.
	// Kapanan sekme veya uyuyan telefonun bağlantısı sunucu fark edene kadar
	// (en fazla bir keepalive) sayılmaya devam eder; reddetmek bu arada açılan
	// yeni sekmeyi "Bağlantı yok" durumunda bırakırdı.
	MaxSubsPerUser = 10
	MaxSubsGlobal  = 200 // tüm kullanıcılar toplamı
)

// Hub canlı olayları (SSE) abonelere dağıtır. Yavaş bir tarayıcı motoru
// bekletmesin diye gönderim bloklamaz; kuyruğu dolan aboneye o olay atlanır.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan []byte]*subscriber
	perUser map[int64][]chan []byte // kullanıcı kimliği → açık akışlar (eskiden yeniye)
	total   int                     // tüm açık aboneler
}

type subscriber struct {
	userID int64
}

func NewHub() *Hub {
	return &Hub{subs: map[chan []byte]*subscriber{}, perUser: map[int64][]chan []byte{}}
}

// Subscribe yeni abone kanalı ve aboneliği bitiren fonksiyonu döner. Genel
// sınıra ulaşıldıysa ok=false döner ve kanal/gorutin ayrılmaz. Kullanıcı kendi
// sınırına ulaştıysa en eski akışının kanalı kapatılır (okuyan taraf kanal
// kapanınca akışı bitirmelidir). userID 0 ise yalnızca genel sınır uygulanır
// (kullanıcı kimliği bilinmeyen dahili aboneler).
func (h *Hub) Subscribe(userID int64) (<-chan []byte, func(), bool) {
	h.mu.Lock()
	if userID != 0 && len(h.perUser[userID]) >= MaxSubsPerUser {
		h.dropLocked(h.perUser[userID][0])
	}
	if h.total >= MaxSubsGlobal {
		h.mu.Unlock()
		return nil, nil, false
	}
	ch := make(chan []byte, 256)
	h.subs[ch] = &subscriber{userID: userID}
	h.total++
	if userID != 0 {
		h.perUser[userID] = append(h.perUser[userID], ch)
	}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		if _, ok := h.subs[ch]; ok {
			h.dropLocked(ch)
		}
		h.mu.Unlock()
	}, true
}

// dropLocked aboneyi kaldırır ve kanalını kapatır (h.mu tutulurken).
func (h *Hub) dropLocked(ch chan []byte) {
	s, ok := h.subs[ch]
	if !ok {
		return
	}
	delete(h.subs, ch)
	h.total--
	if s.userID != 0 {
		list := h.perUser[s.userID]
		for i, c := range list {
			if c == ch {
				list = append(list[:i], list[i+1:]...)
				break
			}
		}
		if len(list) == 0 {
			delete(h.perUser, s.userID)
		} else {
			h.perUser[s.userID] = list
		}
	}
	close(ch)
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
