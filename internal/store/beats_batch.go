package store

import (
	"context"
	"errors"
	"sync"
	"time"
)

// maxBeatBatch tek işlemde yazılan en fazla kontrol sonucu.
const maxBeatBatch = 500

// errStoreClosed kapatılmış veritabanına toplu yazım isteği.
var errStoreClosed = errors.New("veritabanı kapatıldı")

type beatReq struct {
	u    BeatUpdate
	done chan error
}

// beatBatcher kontrol sonuçlarını kısa bir pencerede biriktirip tek işlemde
// yazar (grup commit). Her işlem ham kayıt + iki özet + monitör satırını
// günceller; 5 sn aralıklı yüzlerce monitörde her sonucun ayrı işlemi (SQLite
// WAL'de her commit ayrı sayfa yazımı ve fsync) disk yazımını katlar. Çağıran
// yazım tamamlanana kadar bekler: RecordBeat'in anlamı (hata dönerse kayıt
// yok) değişmez, yalnızca en çok BeatBatch kadar gecikir.
type beatBatcher struct {
	once sync.Once
	q    chan beatReq
	stop chan struct{}
	done chan struct{}
	mu   sync.Mutex
	shut bool
}

func (s *Store) queueBeat(ctx context.Context, u BeatUpdate) error {
	b := &s.beats
	req := beatReq{u: u, done: make(chan error, 1)}
	b.mu.Lock()
	if b.shut {
		b.mu.Unlock()
		return errStoreClosed
	}
	b.once.Do(func() {
		b.q = make(chan beatReq, maxBeatBatch)
		b.stop, b.done = make(chan struct{}), make(chan struct{})
		go s.beatLoop()
	})
	select {
	case b.q <- req:
		b.mu.Unlock()
	case <-ctx.Done():
		b.mu.Unlock()
		return ctx.Err()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		// İstek kuyrukta; yazım yine de yapılabilir, çağıran beklemez.
		return ctx.Err()
	}
}

func (s *Store) beatLoop() {
	b := &s.beats
	defer close(b.done)
	window := s.opts.BeatBatch
	batch := make([]beatReq, 0, maxBeatBatch)
	for {
		select {
		case r := <-b.q:
			batch = append(batch[:0], r)
		case <-b.stop:
			s.drainBeats(batch[:0])
			return
		}
		timer := time.NewTimer(window)
	collect:
		for len(batch) < maxBeatBatch {
			select {
			case r := <-b.q:
				batch = append(batch, r)
			case <-timer.C:
				break collect
			case <-b.stop:
				break collect
			}
		}
		timer.Stop()
		s.flushBeats(batch)
	}
}

// drainBeats kapanışta kuyrukta kalanları yazar.
func (s *Store) drainBeats(batch []beatReq) {
	for {
		select {
		case r := <-s.beats.q:
			batch = append(batch, r)
		default:
			if len(batch) > 0 {
				s.flushBeats(batch)
			}
			return
		}
	}
}

// flushBeats birikmiş sonuçları tek işlemde yazar. İşlem başarısız olursa
// (ör. bu arada silinmiş monitörün kaydı) sonuçlar tek tek yazılır: bir
// sonucun hatası diğerlerini düşürmez.
func (s *Store) flushBeats(batch []beatReq) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := s.tx(ctx, func(tx *Tx) error {
		for _, r := range batch {
			if err := s.writeBeat(ctx, tx, r.u); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil || len(batch) == 1 {
		for _, r := range batch {
			r.done <- err
		}
		return
	}
	for _, r := range batch {
		r.done <- s.tx(ctx, func(tx *Tx) error { return s.writeBeat(ctx, tx, r.u) })
	}
}

// stopBeats toplu yazımı durdurur; kuyruktakiler yazıldıktan sonra döner.
func (s *Store) stopBeats() {
	b := &s.beats
	b.mu.Lock()
	started := b.q != nil && !b.shut
	b.shut = true
	b.mu.Unlock()
	if started {
		close(b.stop)
		<-b.done
	}
}
