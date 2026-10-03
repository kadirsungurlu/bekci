package maintenance

import (
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
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

// NewServerIndex sunucuları (ajanları) kapsayan etkin pencerelerin dizini:
// all_servers olanlar ve sunucu bazında seçilenler. Monitör dizininden ayrı
// kurulur; servers.Service kullanır.
func NewServerIndex(windows []store.Maintenance, onErr func(store.Maintenance, error)) *Index {
	ix := &Index{byMonitor: map[int64][]*Schedule{}}
	for _, w := range windows {
		if !w.Active || (!w.AllServers && len(w.ServerIDs) == 0) {
			continue
		}
		s, err := Compile(w)
		if err != nil {
			if onErr != nil {
				onErr(w, err)
			}
			continue
		}
		if w.AllServers {
			ix.all = append(ix.all, s)
			continue
		}
		for _, id := range w.ServerIDs {
			ix.byMonitor[id] = append(ix.byMonitor[id], s)
		}
	}
	return ix
}

// InMaintenance monitör (sunucu dizininde: ajan) t anında bir bakım penceresinde mi?
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
