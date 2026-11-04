package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betikkayit"
	"github.com/kadirsungurlu/bekci/internal/store/pgtestdb"
)

func openScriptStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("UPTIME_TEST_PG")
	if dsn == "" {
		t.Skip("UPTIME_TEST_PG yok")
	}
	s, err := Open(pgtestdb.Target(t, dsn), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func scriptEvents(t *testing.T, s *Store, n int) {
	t.Helper()
	ctx := context.Background()
	for i := range n {
		err := s.ScriptTx(ctx, func(l *ScriptLog) error {
			_, e := l.Append(ctx, fmt.Sprintf("%032x", i), "requested", 1, map[string]any{"i": i})
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestScriptLogAppendOnly(t *testing.T) {
	s := openScriptStore(t)
	ctx := context.Background()
	scriptEvents(t, s, 5)
	if err := s.AddScript(ctx, ScriptRow{ScriptID: "a", Version: 1, Envelope: "{}", Body: []byte("x"), SHA256: "x", Approval: "none", Manifest: "{}", KeyID: "k", NotBefore: 1, NotAfter: 2, UploadedByName: "u", UploadedAt: 1}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{"UPDATE script_runs SET data='{}'", "DELETE FROM script_runs", "TRUNCATE script_runs",
		"UPDATE scripts SET body=''", "DELETE FROM scripts", "TRUNCATE scripts", "TRUNCATE script_log_checkpoint"} {
		if _, err := s.db.ExecContext(ctx, q); err == nil {
			t.Errorf("%q geçmemeli", q)
		}
	}
	// GUC'yu oturumda elle açmak yetmez diye değil: saklama yalnız kesim öncesini siler.
	x, err := s.ExportScriptLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := betikkayit.Verify(x); err != nil || len(x.Events) != 5 {
		t.Fatalf("zincir: %v %d", err, len(x.Events))
	}
}

// TestScriptLogTamper DB yazma yetkili saldırganı canlandırır: tetikleyici
// kaldırılıp satır değiştirilirse çevrimdışı doğrulama yakalar.
func TestScriptLogTamper(t *testing.T) {
	s := openScriptStore(t)
	ctx := context.Background()
	scriptEvents(t, s, 4)
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE script_runs DISABLE TRIGGER USER"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE script_runs SET data='{"i":99}' WHERE seq=2`); err != nil {
		t.Fatal(err)
	}
	x, err := s.ExportScriptLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := betikkayit.Verify(x); err == nil {
		t.Fatal("değişiklik yakalanmalı")
	}
}

func TestScriptLogPruneCheckpoint(t *testing.T) {
	s := openScriptStore(t)
	ctx := context.Background()
	scriptEvents(t, s, 6)
	// Negatif saklama: kesim 30 günlük parçanın ötesinde, tüm olaylar saklama dışı (Timescale parça
	// sınırı eski tarihli satır yazdırmadığı için tarih değiştirilmez).
	if _, err := s.PruneScriptLog(ctx, -60*24*time.Hour); err != nil {
		t.Fatal(err)
	}
	x, err := s.ExportScriptLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Checkpoints) == 0 {
		t.Fatalf("checkpoint yazılmalı: %d olay", len(x.Events))
	}
	if err := betikkayit.Verify(x); err != nil {
		t.Fatalf("budama sonrası zincir: %v", err)
	}
	// Elle silme hâlâ yasak, zincir checkpoint üzerinden sürer.
	scriptEvents(t, s, 2)
	if x, err = s.ExportScriptLog(ctx); err != nil {
		t.Fatal(err)
	}
	if err := betikkayit.Verify(x); err != nil {
		t.Fatalf("devam eden zincir: %v", err)
	}
}

func TestScriptQueueEmergency(t *testing.T) {
	s := openScriptStore(t)
	ctx := context.Background()
	p := &Probe{Name: "s1", Kind: ProbeKindServer}
	if err := s.CreateProbe(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := s.ScriptTx(ctx, func(l *ScriptLog) error {
		for i, del := range []int64{0, 5} {
			id := fmt.Sprintf("%032x", i)
			if e := l.Enqueue(ctx, ScriptJob{RunID: id, ProbeID: p.ID, ScriptID: "a", Version: 1, Params: "{}", QueuedAt: 1, ExpiresAt: 9e9}); e != nil {
				return e
			}
			if del != 0 {
				if _, e := l.MarkDelivered(ctx, id, del); e != nil {
					return e
				}
			}
		}
		pending, running, e := l.SetStopped(ctx, true, "k", 7)
		if e != nil {
			return e
		}
		if len(pending) != 1 || len(running) != 1 {
			t.Fatalf("durdurma: %v %v", pending, running)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ScriptJobsFor(ctx, p.ID, 10)
	if err != nil || len(jobs) != 1 || !jobs[0].Cancel {
		t.Fatalf("yalnız iptal kalmalı: %+v %v", jobs, err)
	}
}
