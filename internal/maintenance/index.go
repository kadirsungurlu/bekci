package maintenance

import (
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Index aktif pencerelerin monitörlere göre dizinidir. Kurulduktan sonra
// değişmez; motor yenisini kurup atomik olarak değiştirir.
type Index struct {
	all       []*Schedule           // tüm monitörleri etkileyenler
	byMonitor map[int64][]*Schedule // monitöre özel olanlar
}

// NewIndex aktif pencereleri derler. Derlenemeyen pencere (ör. bozuk kayıt)
// atlanır ve onErr ile bildirilir.
func NewIndex(windows []store.Maintenance, onErr func(store.Maintenance, error)) *Index {
	ix := &Index{byMonitor: map[int64][]*Schedule{}}
	for _, w := range windows {
		if !w.Active {
			continue
		}
		s, err := Compile(w)
		if err != nil {
			if onErr != nil {
				onErr(w, err)
			}
			continue
		}
		if w.AllMonitors {
			ix.all = append(ix.all, s)
			continue
		}
		for _, id := range w.MonitorIDs {
			ix.byMonitor[id] = append(ix.byMonitor[id], s)
		}
	}
	return ix
}

// InMaintenance monitör t anında bir bakım penceresinde mi?
func (ix *Index) InMaintenance(monitorID int64, t time.Time) bool {
	if ix == nil {
		return false
	}
	for _, s := range ix.all {
		if s.ActiveAt(t) {
			return true
		}
	}
	for _, s := range ix.byMonitor[monitorID] {
		if s.ActiveAt(t) {
			return true
		}
	}
	return false
}
