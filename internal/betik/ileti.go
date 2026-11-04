package betik

import (
	"context"
	"encoding/json"
)

// JobList GET /api/probe/jobs yanıtındaki "script_jobs" alanı.
type JobList struct {
	Runs    []RunJob          `json:"runs,omitempty"`
	Revokes []json.RawMessage `json:"revokes,omitempty"`
	Cancel  []string          `json:"cancel,omitempty"`
}

// RunJob tek çalıştırma isteği. Zarflar ham tutulur: katı ayrıştırma
// (yinelenen anahtar) ajanda ParseEnvelope ile yapılır.
type RunJob struct {
	RunID    string          `json:"run_id"`
	Manifest json.RawMessage `json:"manifest"`
	Body     []byte          `json:"body"`   // base64
	Params   []byte          `json:"params"` // base64, ham JSON baytları
	Ticket   json.RawMessage `json:"ticket,omitempty"`
}

// Event POST /api/probe/script-result gövdesi (istek başına tek olay).
type Event struct {
	Kind    string          `json:"kind"`
	RunID   string          `json:"run_id,omitempty"`
	Code    string          `json:"code,omitempty"`
	Time    int64           `json:"time"`
	Receipt json.RawMessage `json:"receipt,omitempty"`
	Output  []byte          `json:"output,omitempty"` // base64
}

// SendFunc olayı panele iletir; HTTP durumu ve ağ hatası döner.
type SendFunc func(ctx context.Context, ev *Event) (int, error)

// Capability X-Probe-Scripts başlık değeri.
func Capability(on bool, reason string) string {
	if on {
		return "schemas=1;state=on"
	}
	return "schemas=1;state=off;reason=" + reason
}
