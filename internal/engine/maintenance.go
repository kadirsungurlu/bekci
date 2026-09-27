package engine

import (
	"context"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/maintenance"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// ReloadMaintenance bakım pencerelerini veritabanından okuyup bellekteki
// dizini yeniler. Açılışta ve pencere/monitör eklenip silindiğinde çağrılır;
// kontrol başına veritabanına gidilmez.
func (e *Engine) ReloadMaintenance(ctx context.Context) error {
	windows, err := e.store.ListMaintenance(ctx)
	if err != nil {
		return err
	}
	ix := maintenance.NewIndex(windows, func(w store.Maintenance, err error) {
		e.log.Error("bakım penceresi derlenemedi", "pencere", w.Title, "hata", err)
	})
	e.maint.Store(ix)
	return nil
}

// InMaintenance monitör t anında bir bakım penceresinde mi?
func (e *Engine) InMaintenance(monitorID int64, t time.Time) bool {
	return e.maint.Load().InMaintenance(monitorID, t)
}

// monitorStatuses grup monitörlerinin alt monitör durumlarını okur (grup
// aralığında bir sorgu).
func (e *Engine) monitorStatuses(ctx context.Context, ids []int64) (map[int64]check.ChildStatus, error) {
	ms, err := e.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]check.ChildStatus, len(ms))
	for id, m := range ms {
		out[id] = check.ChildStatus{ID: id, Name: m.Name, Active: m.Active, Status: m.Status}
	}
	return out, nil
}
