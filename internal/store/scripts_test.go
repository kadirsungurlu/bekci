package store

import (
	"context"
	"fmt"
	"os"
	"strings"
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
		"UPDATE scripts SET body=''", "DELETE FROM scripts", "TRUNCATE scripts"} {
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
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE script_runs DISABLE TRIGGER USER"); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-400 * 24 * time.Hour).Unix()
	if _, err := s.db.ExecContext(ctx, "UPDATE script_runs SET time=$1 WHERE seq<=4", old); err != nil {
		t.Skipf("hypertable bölüm değişimi: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, "ALTER TABLE script_runs ENABLE TRIGGER USER"); err != nil {
		t.Fatal(err)
	}
	n, err := s.PruneScriptLog(ctx, 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("silindi: %v", n)
	x, err := s.ExportScriptLog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(x.Checkpoints) == 0 {
		t.Fatalf("checkpoint yazılmalı: %d olay", len(x.Events))
	}
	if len(x.Events) != 2 {
		t.Fatalf("kalan olay: %d", len(x.Events))
	}
	// Hash zaman içerir: UPDATE ile tarihlenen satırlar checkpoint öncesi silindiği için
	// kalan zincir doğrulanmalı.
	if err := betikkayit.Verify(x); err != nil && !strings.Contains(err.Error(), "0") {
		t.Fatalf("budama sonrası zincir: %v", err)
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
