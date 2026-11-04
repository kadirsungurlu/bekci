package engine

import (
	"sync"
	"time"
)

// rateWindow sonuç hızının ölçüldüğü pencere (saniye).
const rateWindow = 60

// resultRate son rateWindow saniyedeki sonuç sayısını saniye kovalarında tutar
// (canlı topoloji: "saniyedeki sonuç"). Sayılanlar: panelin kendi denetim
// sonucu (beat kaydı) ve kontrol noktalarından gelen konum sonuçları.
type resultRate struct {
	mu      sync.Mutex
	buckets [rateWindow]int64
	secs    [rateWindow]int64 // kovanın ait olduğu unix saniye
}

func (r *resultRate) add(t time.Time, n int) {
	if n <= 0 {
		return
	}
	sec := t.Unix()
	i := int(sec % rateWindow)
	r.mu.Lock()
	if r.secs[i] != sec {
		r.secs[i], r.buckets[i] = sec, 0
	}
	r.buckets[i] += int64(n)
	r.mu.Unlock()
}

// perSecond tamamlanmış son rateWindow saniyenin ortalaması (içinde bulunulan
// saniye yarım olduğundan sayılmaz).
func (r *resultRate) perSecond(now time.Time) float64 {
	cur := now.Unix()
	var sum int64
	r.mu.Lock()
	for i := range r.buckets {
		if age := cur - r.secs[i]; age >= 1 && age <= rateWindow {
			sum += r.buckets[i]
		}
	}
	r.mu.Unlock()
	return float64(sum) / rateWindow
}

// ResultsPerSecond son bir dakikadaki ortalama sonuç hızı (sonuç/sn).
func (e *Engine) ResultsPerSecond(now time.Time) float64 { return e.rate.perSecond(now) }
