// Package schedule ana sunucu ile kontrol noktalarının (ajanlar) aynı monitörü
// aynı anda kontrol etmesi için ortak zaman ızgarasını tanımlar. Kontrol
// anları unix zamanına göre phase + k·aralık'tır; iki taraf aynı kaymayı
// (phase) ve aralığı kullandığından saatleri eşitse aynı anda kontrol eder.
package schedule

import (
	"hash/fnv"
	"strconv"
	"time"
)

// phaseSpan kaymanın alındığı aralık (1 gün): her aralıkta (5 sn, 1 dk, 1 sa…)
// monitörler ızgaraya düzgün yayılır, aynı ana yığılmaz.
const phaseSpan = 24 * 60 * 60 * 1000

// PhaseMs monitörün ızgaradaki sabit kayması (milisaniye). Yalnızca monitör
// kimliğine bağlıdır: yeniden başlatma, aralık değişikliği veya ajan tarafı
// aynı değeri bulur.
func PhaseMs(monitorID int64) int64 {
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(monitorID, 10)))
	return int64(h.Sum64() % phaseSpan)
}

// Next after'dan kesin olarak sonraki ilk ızgara anı: phase + k·every.
// every <= 0 ise after döner.
func Next(after time.Time, every time.Duration, phaseMs int64) time.Time {
	if every <= 0 {
		return after
	}
	phase := time.Duration(phaseMs) * time.Millisecond
	n := after.UnixNano() - int64(phase)
	k := n / int64(every)
	if n%int64(every) < 0 {
		k-- // negatifte aşağı yuvarla
	}
	return time.Unix(0, (k+1)*int64(every)+int64(phase))
}

// Planner bir monitörün sıradaki kontrol anını tutar.
type Planner struct {
	PhaseMs int64
	due     time.Time // son planlanan ızgara anı (sıfır: henüz yok)
}

// Delay now'dan sıradaki kontrol anına kadar bekleme süresi. ran: bir kontrol
// az önce yapıldı (zamanlayıcı çaldı); false: zamanlayıcı yalnızca yeniden
// kuruluyor (ör. durum değişti, tekrar deneme aralığına geçildi).
//
// İlk çağrıda (ilk kontrol ızgaraya bağlı değildi: yeniden başlatmada
// yayılarak yapılır) en az every/2 beklenir ki ızgaraya geçerken iki kontrol
// art arda gelmesin. Kontrolden sonra planlanan anın gerisine düşülmez: erken
// uyanan zamanlayıcı veya saat kayması aynı anı iki kez kontrol ettirmez.
func (p *Planner) Delay(now time.Time, every time.Duration, ran bool) time.Duration {
	from := now
	switch {
	case p.due.IsZero():
		from = now.Add(every / 2)
	case ran && p.due.After(now) && p.due.Sub(now) < every/4:
		// Zamanlayıcı planlanan andan az önce uyandı: o an sayılır.
		from = p.due
	}
	p.due = Next(from, every, p.PhaseMs)
	return p.due.Sub(now)
}
