package engine

import (
	"sync"
	"time"
)

// Kontrol noktası iş listesi sürümü ----------------------------------------------------
//
// Kontrol noktaları iş listesini uzun yoklamayla (long-poll) alır: API, ajanın
// elindeki liste güncelse isteği bir süre bekletir ve bir değişiklik olunca
// hemen yanıtlar (internal/api/probes.go, probeJobs). Bunun için motor, iş
// listesini veya metrik aralığını etkileyebilecek her değişiklikte (monitör
// ekleme/düzenleme/silme/durdurma/başlatma, konum ayarı, kontrol noktası
// ayarları) sürümü artırır ve bekleyenleri uyandırır.
//
// Sürüm tüm kontrol noktaları için ortaktır; artış "listen değişmiş olabilir"
// anlamına gelir (bekleyen her istek listeyi yeniden okur). Değişiklikler
// seyrektir, gereksiz uyanma ucuz bir sorgudur. Başlangıç değeri açılış anının
// milisaniyesidir: yeniden başlatmadan önce alınmış bir sürüm (?since=) yeni
// süreçtekiyle eşleşmez, ajan listeyi hemen alır.

type jobsSignal struct {
	mu  sync.Mutex
	ver int64
	ch  chan struct{} // bir sonraki artışta kapatılır
}

func newJobsSignal() *jobsSignal {
	return &jobsSignal{ver: time.Now().UnixMilli(), ch: make(chan struct{})}
}

// JobsVersion iş listesinin güncel sürümü ve bir sonraki değişiklikte kapanacak
// kanal. Sürüm, liste okunmadan ÖNCE alınmalıdır: okuma sırasında gelen
// değişiklik böylece kaçmaz (sürüm eskide kalır, ajan tekrar sorar).
func (e *Engine) JobsVersion() (int64, <-chan struct{}) {
	e.jobs.mu.Lock()
	defer e.jobs.mu.Unlock()
	return e.jobs.ver, e.jobs.ch
}

// JobsChanged iş listesi sürümünü artırır ve bekleyen uzun yoklamaları
// uyandırır. Değişiklik veritabanına yazıldıktan SONRA çağrılmalıdır.
func (e *Engine) JobsChanged() {
	e.jobs.mu.Lock()
	defer e.jobs.mu.Unlock()
	e.jobs.ver++
	close(e.jobs.ch)
	e.jobs.ch = make(chan struct{})
}
