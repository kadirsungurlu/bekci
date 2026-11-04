// Package betikkayit betik çalıştırma kaydının hash zinciri tanımı. Panel
// (store) ile çevrimdışı doğrulama aracı (cmd/bekci-imza) aynı tanımı kullanır;
// veritabanı bağımlılığı yoktur (docs/BETIK-GUVENLIK-680.md §8).
package betikkayit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Genesis zincirin ilk olayının önceki değeri.
const Genesis = "0000000000000000000000000000000000000000000000000000000000000000"

// Event zincirin bir olayı. Data ham JSON metnidir (yeniden kodlanmaz: özet bu
// baytlardan alınır).
type Event struct {
	Seq     int64  `json:"seq"`
	Time    int64  `json:"time"`
	RunID   string `json:"run_id"`
	Kind    string `json:"kind"`
	ActorID int64  `json:"actor_id"`
	Data    string `json:"data"`
	Prev    string `json:"prev"`
	Hash    string `json:"hash"`
}

// Hash sha256(önceki_hash || olay). Alan ayırıcı uzunluk önekidir: alan
// sınırları kaydırılarak aynı özet üretilemez.
func Hash(prev string, e Event) string {
	var b strings.Builder
	b.WriteString("bekci-script-log-v1\n")
	b.WriteString(prev)
	for _, f := range []string{strconv.FormatInt(e.Seq, 10), strconv.FormatInt(e.Time, 10),
		e.RunID, e.Kind, strconv.FormatInt(e.ActorID, 10), e.Data} {
		fmt.Fprintf(&b, "\n%d:%s", len(f), f)
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Checkpoint saklama nedeniyle düşürülen önekin son olayı.
type Checkpoint struct {
	Seq       int64  `json:"seq"`
	Hash      string `json:"hash"`
	Time      int64  `json:"time"`
	Cutoff    int64  `json:"cutoff"`
	CreatedAt int64  `json:"created_at"`
}

// Export dışa aktarım dosyası: `bekci-imza dogrula` bunu okur.
type Export struct {
	Format      int          `json:"format"`
	ExportedAt  int64        `json:"exported_at"`
	Checkpoints []Checkpoint `json:"checkpoints"`
	Events      []Event      `json:"events"`
	// Outputs zincire girmez; çıktılar sır içerebilir, burada yoktur.
}

// VerifyError zincirin bozulduğu ilk olay.
type VerifyError struct {
	Seq int64
	Why string
}

func (e *VerifyError) Error() string { return fmt.Sprintf("seq %d: %s", e.Seq, e.Why) }

// Verify olay dizisinin bütünlüğünü denetler: ardışık seq, prev bağı, özet,
// zamanın geri gitmemesi. İlk olay Genesis ya da son checkpoint'e dayanmalıdır.
func Verify(x Export) error {
	prev, next := Genesis, int64(1)
	if n := len(x.Checkpoints); n > 0 {
		c := x.Checkpoints[n-1]
		prev, next = c.Hash, c.Seq+1
	}
	var last int64
	for _, e := range x.Events {
		switch {
		case e.Seq != next:
			return &VerifyError{e.Seq, fmt.Sprintf("sıra boşluğu: %d bekleniyordu", next)}
		case e.Prev != prev:
			return &VerifyError{e.Seq, "önceki özet tutmuyor"}
		case Hash(prev, e) != e.Hash:
			return &VerifyError{e.Seq, "olay özeti tutmuyor"}
		case e.Time < last:
			return &VerifyError{e.Seq, "zaman geri gitmiş"}
		}
		prev, next, last = e.Hash, e.Seq+1, e.Time
	}
	return nil
}

// ParseExport katı okuma: bilinmeyen alan reddedilir.
func ParseExport(b []byte) (Export, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	var x Export
	if err := dec.Decode(&x); err != nil {
		return x, err
	}
	if x.Format != 1 {
		return x, fmt.Errorf("bilinmeyen biçim %d", x.Format)
	}
	return x, nil
}
