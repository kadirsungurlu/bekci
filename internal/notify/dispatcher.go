package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// sendTimeout tek bir kanala gönderim için süre sınırı.
const sendTimeout = 30 * time.Second

// Dispatcher olayları monitöre bağlı kanallara arka planda gönderir.
// Yavaş bir kanal kontrol motorunu asla bekletmez.
type Dispatcher struct {
	store *store.Store
	log   *slog.Logger
	wg    sync.WaitGroup
}

func NewDispatcher(s *store.Store, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: s, log: log}
}

// Notify olayı monitörün etkin kanallarına gönderir (bloklamaz).
func (d *Dispatcher) Notify(ev Event) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		channels, err := d.store.NotificationsForMonitor(ctx, ev.MonitorID)
		cancel()
		if err != nil {
			d.log.Error("bildirim kanalları okunamadı", "monitor", ev.MonitorID, "hata", err)
			return
		}
		var inner sync.WaitGroup
		for _, ch := range channels {
			inner.Add(1)
			go func(ch store.Notification) {
				defer inner.Done()
				if err := d.send(ch.Type, ch.Config, ev); err != nil {
					d.log.Warn("bildirim gönderilemedi", "kanal", ch.Name, "tip", ch.Type, "olay", ev.Kind, "monitor", ev.MonitorName, "hata", err)
					return
				}
				d.log.Info("bildirim gönderildi", "kanal", ch.Name, "olay", ev.Kind, "monitor", ev.MonitorName)
			}(ch)
		}
		inner.Wait()
	}()
}

// Test verilen ayarla hemen bir test bildirimi gönderir ve sonucu döner.
func (d *Dispatcher) Test(typ string, cfg json.RawMessage) error {
	return d.send(typ, cfg, Event{Kind: KindTest, MonitorName: "Test", Time: time.Now()})
}

func (d *Dispatcher) send(typ string, cfg json.RawMessage, ev Event) error {
	p, ok := Get(typ)
	if !ok {
		return fmt.Errorf("bilinmeyen bildirim tipi: %s", typ)
	}
	ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
	defer cancel()
	return p.Send(ctx, cfg, ev)
}

// Wait kapanışta gönderilmekte olan bildirimlerin bitmesini bekler.
func (d *Dispatcher) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
