package betikajan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

const (
	logFile       = "kayit.log"
	maxRejectHour = 120      // saatte en çok bu kadar red satırı
	maxLogReject  = 64 << 20 // kayit.log bu boyutu aşınca red yazılmaz, makbuz yazılır
	maxQueueFiles = 256
	maxQueueBytes = 32 << 20
)

// Journal yerel tanık kaydı: panelin erişemediği kayit.log + journald. Makbuz
// satırları budanmaz; red satırları hız ve boyutla sınırlıdır.
type Journal struct {
	dir string
	log *slog.Logger

	mu       sync.Mutex
	winStart time.Time
	winCount int
}

type logLine struct {
	Time    int64           `json:"t"`
	Kind    string          `json:"kind"`
	RunID   string          `json:"run_id,omitempty"`
	Script  string          `json:"script,omitempty"`
	Code    string          `json:"code,omitempty"`
	Receipt json.RawMessage `json:"receipt,omitempty"`
}

func (j *Journal) path() string { return filepath.Join(j.dir, logFile) }

func (j *Journal) append(l logLine) error {
	f, err := os.OpenFile(j.path(), os.O_APPEND|os.O_CREATE|os.O_WRONLY|noFollow, 0o640)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(mustJSON(l), '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// Receipt makbuz satırı: yerel kayda, sonra journald'a.
func (j *Journal) Receipt(now time.Time, env []byte, r *betik.Receipt) {
	if err := j.append(logLine{Time: now.Unix(), Kind: betik.EventReceipt, RunID: r.RunID, Receipt: env}); err != nil {
		j.log.Error("betik kaydı yazılamadı", "hata", err)
	}
	j.log.Info("betik bitti", "run_id", r.RunID, "betik", r.ScriptID, "sürüm", r.Version,
		"sonuç", r.Status, "çıkış", r.ExitCode, "seq", r.Seq)
}

// Reject red satırı; hız ve boyut sınırını aşan satır yazılmaz.
func (j *Journal) Reject(now time.Time, runID, script, code string) {
	j.mu.Lock()
	if now.Sub(j.winStart) >= time.Hour {
		j.winStart, j.winCount = now, 0
	}
	j.winCount++
	over := j.winCount > maxRejectHour
	j.mu.Unlock()
	if over {
		return
	}
	j.log.Warn("betik reddedildi", "run_id", runID, "betik", script, "kod", code)
	if fi, err := os.Stat(j.path()); err == nil && fi.Size() > maxLogReject {
		return
	}
	if err := j.append(logLine{Time: now.Unix(), Kind: betik.EventRejected, RunID: runID, Script: script, Code: code}); err != nil {
		j.log.Error("betik kaydı yazılamadı", "hata", err)
	}
}

// Note işletim notu (onar, kapat, saat ileri atladı).
func (j *Journal) Note(now time.Time, code string) {
	j.log.Warn("betik notu", "kod", code)
	if err := j.append(logLine{Time: now.Unix(), Kind: betik.EventNotice, Code: code}); err != nil {
		j.log.Error("betik kaydı yazılamadı", "hata", err)
	}
}

// Tail kayıttaki son makbuzun sırası ve özeti (durum dosyası kaybolunca zincir
// buradan sürer). Makbuz yoksa 0, "".
func (j *Journal) Tail() (int64, string, error) {
	data, err := os.ReadFile(j.path())
	if os.IsNotExist(err) {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	var seq int64
	var prev string
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var l logLine
		if err := dec.Decode(&l); err != nil {
			break
		}
		if l.Kind != betik.EventReceipt {
			continue
		}
		env, err := betik.ParseEnvelope(l.Receipt)
		if err != nil {
			continue
		}
		r, err := betik.ParseReceipt(env.Payload)
		if err != nil {
			continue
		}
		seq, prev = r.Seq, env.PayloadDigest()
	}
	return seq, prev, nil
}

// diskQueue panele iletilmeyi bekleyen makbuzlar: panel ulaşılamazsa sınırlı
// bekler, dolunca en eskisi düşer (kayit.log'da kalır, seq boşluğu oradan
// doğrulanır).
type diskQueue struct{ dir string }

func (q *diskQueue) names() []string {
	ents, err := os.ReadDir(q.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if filepath.Ext(e.Name()) == ".json" {
			out = append(out, e.Name())
		}
	}
	slices.Sort(out) // ad sıfır dolgulu seq
	return out
}

// put olayı yazar; düşürülen kayıt sayısını döndürür.
func (q *diskQueue) put(seq int64, ev *betik.Event) (int, error) {
	if err := os.MkdirAll(q.dir, 0o700); err != nil {
		return 0, err
	}
	name := filepath.Join(q.dir, seqName(seq))
	if err := atomicWrite(name, mustJSON(ev), 0o600); err != nil {
		return 0, err
	}
	names := q.names()
	var total int64
	sizes := make([]int64, len(names))
	for i, n := range names {
		if fi, err := os.Stat(filepath.Join(q.dir, n)); err == nil {
			sizes[i] = fi.Size()
			total += fi.Size()
		}
	}
	dropped := 0
	for i := 0; i < len(names)-1 && (len(names)-i > maxQueueFiles || total > maxQueueBytes); i++ {
		os.Remove(filepath.Join(q.dir, names[i]))
		total -= sizes[i]
		dropped++
	}
	return dropped, nil
}

func (q *diskQueue) get(name string) (*betik.Event, error) {
	data, err := os.ReadFile(filepath.Join(q.dir, name))
	if err != nil {
		return nil, err
	}
	var ev betik.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		return nil, err
	}
	return &ev, nil
}

func (q *diskQueue) del(name string) { os.Remove(filepath.Join(q.dir, name)) }

func seqName(seq int64) string { return fmt.Sprintf("%020d.json", seq) }
