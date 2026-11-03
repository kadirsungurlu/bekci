package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Toplu yazım (grup commit): eşzamanlı sonuçlar tek işlemde yazılır, her
// çağıran kendi sonucunu alır; işlem bozulursa sonuçlar tek tek yazılır.
func TestBeatBatch(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Istanbul")
	s, err := OpenWith(testTarget(t), loc, Options{BeatBatch: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	a, b := newMonitor(t, s, "a"), newMonitor(t, s, "b")
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 20 {
		for _, id := range []int64{a.ID, b.ID} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				errs <- s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: id, Time: int64(2_000_000 + i), Status: StatusUp, PingMs: 7}})
			}()
		}
	}
	// Var olmayan monitörün sonucu (silinmiş olabilir) diğerlerini düşürmemeli.
	wg.Add(1)
	go func() {
		defer wg.Done()
		s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: 987654, Time: 2_000_000, Status: StatusUp, PingMs: 7}})
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []int64{a.ID, b.ID} {
		beats, err := s.Beats(ctx, id, 0)
		if err != nil || len(beats) != 20 {
			t.Fatalf("monitör %d: %d kayıt (%v), 20 bekleniyordu", id, len(beats), err)
		}
		m, err := s.GetMonitor(ctx, id)
		if err != nil || m.LastCheckAt < 2_000_000 {
			t.Fatalf("monitör satırı güncellenmedi: %+v %v", m.LastCheckAt, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: a.ID, Time: 2_000_100, Status: StatusUp}}); err == nil {
		t.Fatal("kapatılmış veritabanına yazım hata vermeli")
	}
}
