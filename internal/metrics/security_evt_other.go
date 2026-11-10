//go:build !windows

package metrics

import (
	"context"
	"time"
)

// winEventLog Windows Security günlüğü (4625/4740) yalnız Windows'ta okunur.
const winEventLog = false

func (c *Collector) readEventLog(context.Context, time.Time) (string, string) {
	return "", NoteUnsupported
}
