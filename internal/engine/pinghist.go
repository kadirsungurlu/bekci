package engine

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// PingHistLen monitör başına bellekte tutulan son kontrol sayısı (topoloji
// küçük grafiği). Her istekte heartbeats'e gidilmez: 1000 monitörde bu sorgu
// yanıt süresinin çoğunu yerdi.
const PingHistLen = 30

// pingHist monitör başına halka; değerler store.RecentPings ile aynı (ms,
// store.PingNone, store.PingDown).
type pingHist struct {
	mu sync.RWMutex
	m  map[int64][]int64
}

func (h *pingHist) add(id int64, status int, ms int64) {
	v := ms
	switch {
	case status == store.StatusDown:
		v = store.PingDown
	case ms < 0:
		v = store.PingNone
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = map[int64][]int64{}
	}
	s := h.m[id]
	if len(s) == PingHistLen {
		copy(s, s[1:])
		s[PingHistLen-1] = v
	} else {
		s = append(s, v)
	}
	h.m[id] = s
}

// seed DB'den gelen eski değerleri bellektekilerin önüne koyar.
func (h *pingHist) seed(old map[int64][]int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.m == nil {
		h.m = make(map[int64][]int64, len(old))
	}
	for id, vals := range old {
		s := append(slices.Clone(vals), h.m[id]...)
		if n := len(s) - PingHistLen; n > 0 {
			s = s[n:]
		}
		h.m[id] = s
	}
}

func (h *pingHist) get(id int64) []int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return slices.Clone(h.m[id])
}

// PingHistory monitörün son kontrol süreleri, eskiden yeniye; bkz. store.RecentPings.
func (e *Engine) PingHistory(id int64) []int64 { return e.pings.get(id) }

// seedPings açılışta bir kez, bloklamadan çalışır; hata yalnız grafiği boş bırakır.
func (e *Engine) seedPings(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	old, err := e.store.RecentPings(ctx, nil, PingHistLen)
	if err != nil {
		e.log.Warn("ping geçmişi okunamadı", "hata", err)
		return
	}
	e.pings.seed(old)
}
