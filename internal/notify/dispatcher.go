package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// sendTimeout tek bir kanala gönderim için süre sınırı.
const sendTimeout = 30 * time.Second

// defaultRetryDelays gerçek olay bildirimlerinde geçici hatalardan (ağ hatası,
// HTTP 5xx/429) sonra yeniden deneme bekleme süreleri: toplam 3 deneme. Test
// ve örnek bildirimleri yeniden denenmez (sonuç kullanıcıya hemen gösterilir).
var defaultRetryDelays = []time.Duration{5 * time.Second, 20 * time.Second}

// Dispatcher olayları monitöre bağlı kanallara arka planda gönderir.
// Yavaş bir kanal kontrol motorunu asla bekletmez.
type Dispatcher struct {
	store *store.Store
	log   *slog.Logger
	wg    sync.WaitGroup

	stop     chan struct{} // kapanış: bekleyen yeniden denemeler iptal edilir
	stopOnce sync.Once

	retryDelays []time.Duration // yeniden deneme beklemeleri (testler kısaltır/kapatır)
}

func NewDispatcher(s *store.Store, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: s, log: log, stop: make(chan struct{}), retryDelays: defaultRetryDelays}
}

// retryable geçici sayılan gönderim hatası: ağ/zaman aşımı hataları ve
// HTTP 5xx / 429 yanıtları. Yetki (401/403), adres (404) ve ayar hataları
// yeniden denenmez.
func retryable(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return true
	}
	return retryStatusRe.MatchString(err.Error())
}

var retryStatusRe = regexp.MustCompile(`\bHTTP (5\d\d|429)\b`)

// sendRetry olayı kanala gönderir; geçici hatada retryDelays kadar bekleyip
// yeniden dener (kapanışta beklemez). Deneme sayısını ve son hatayı döner.
func (d *Dispatcher) sendRetry(typ string, cfg json.RawMessage, ev Event) (int, error) {
	var err error
	for attempt := 1; ; attempt++ {
		err = d.send(typ, cfg, ev)
		if err == nil || !retryable(err) || attempt > len(d.retryDelays) {
			return attempt, err
		}
		d.log.Warn("bildirim gönderilemedi, yeniden denenecek", "tip", typ, "olay", ev.Kind, "monitor", ev.MonitorName,
			"deneme", attempt, "bekleme", d.retryDelays[attempt-1], "hata", err)
		select {
		case <-time.After(d.retryDelays[attempt-1]):
		case <-d.stop:
			return attempt, err
		}
	}
}

// Notify olayı monitörün (ProbeID doluysa sunucunun) etkin kanallarına
// gönderir (bloklamaz).
func (d *Dispatcher) Notify(ev Event) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		var channels []store.Notification
		var err error
		if ev.ProbeID != 0 {
			channels, err = d.store.NotificationsForProbe(ctx, ev.ProbeID)
		} else {
			channels, err = d.store.NotificationsForMonitor(ctx, ev.MonitorID)
		}
		if err == nil && ev.Lang == "" {
			ev.Lang = d.Lang(ctx)
		}
		cancel()
		if err != nil {
			d.log.Error("bildirim kanalları okunamadı", "monitor", ev.MonitorID, "sunucu", ev.ProbeID, "hata", err)
			return
		}
		// Monitör ve sunucu olaylarında gönderim sonucu olayın işlem geçmişine yazılır.
		logIncident := ev.IncidentID != 0
		if logIncident && len(channels) == 0 {
			d.incidentEvent(ev, deliveryData{Event: ev.Kind, None: true})
		}
		var inner sync.WaitGroup
		for _, ch := range channels {
			inner.Add(1)
			go func(ch store.Notification) {
				defer inner.Done()
				attempts, err := d.sendRetry(ch.Type, ch.Config, ev)
				if logIncident {
					dd := deliveryData{Event: ev.Kind, ChannelID: ch.ID, Channel: ch.Name, Type: ch.Type, OK: err == nil}
					if attempts > 1 {
						dd.Attempts = attempts
					}
					if err != nil {
						dd.Error = SanitizeSendError(ch.Type, ch.Config, err)
					}
					d.incidentEvent(ev, dd)
				}
				if err != nil {
					d.log.Warn("bildirim gönderilemedi", "kanal", ch.Name, "tip", ch.Type, "olay", ev.Kind, "monitor", ev.MonitorName, "deneme", attempts, "hata", err)
					return
				}
				d.log.Info("bildirim gönderildi", "kanal", ch.Name, "olay", ev.Kind, "monitor", ev.MonitorName, "deneme", attempts)
			}(ch)
		}
		inner.Wait()
	}()
}

// deliveryData olayın işlem geçmişindeki bildirim kaydının data alanı.
type deliveryData struct {
	Event     string `json:"event"` // down | up | reminder
	ChannelID int64  `json:"channel_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Type      string `json:"type,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	None      bool   `json:"none,omitempty"`     // monitöre bağlı etkin kanal yok
	Attempts  int    `json:"attempts,omitempty"` // birden fazla denendiyse deneme sayısı
}

// incidentEvent gönderim sonucunu olayın işlem geçmişine yazar. Zaman gönderimin
// bittiği andır (yavaş kanal geç görünür).
func (d *Dispatcher) incidentEvent(ev Event, dd deliveryData) {
	msg := "Bildirim gönderildi"
	switch {
	case dd.None:
		msg = "Bağlı etkin bildirim kanalı yok"
	case !dd.OK:
		msg = "Bildirim gönderilemedi"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := d.store.AddIncidentEvents(ctx, ev.IncidentID, store.IncidentEvent{
		Time: time.Now().Unix(), Kind: store.EventNotify, Message: msg, Data: store.EventData(dd),
	})
	if err != nil {
		d.log.Error("bildirim kaydı yazılamadı", "olay", ev.IncidentID, "hata", err)
	}
}

// maxSendError işlem geçmişindeki gönderim hatasının en fazla uzunluğu.
const maxSendError = 200

var errURLRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>/]+[^\s"'<>]*`)

// SanitizeSendError gönderim hatasını arayüzde gösterilecek hale getirir:
// adreslerin yalnızca şema ve sunucu adı kalır (Telegram token'ı, webhook
// adresleri gizli bilgidir), kanal ayarındaki gizli değerler maskelenir, kısaltılır.
func SanitizeSendError(typ string, cfg json.RawMessage, err error) string {
	msg := redactURLError(err).Error()
	msg = errURLRe.ReplaceAllStringFunc(msg, func(s string) string {
		u, perr := url.Parse(s)
		if perr != nil || u.Host == "" {
			return "…"
		}
		if u.Path == "" && u.RawQuery == "" && u.User == nil {
			return s
		}
		return u.Scheme + "://" + u.Host + "/…"
	})
	if p, ok := Get(typ); ok {
		var m map[string]any
		if json.Unmarshal(cfg, &m) == nil {
			for _, k := range p.Secrets() {
				if v, ok := m[k].(string); ok && len(v) >= 4 {
					msg = strings.ReplaceAll(msg, v, Mask)
				}
			}
		}
	}
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > maxSendError {
		msg = string(r[:maxSendError]) + "…"
	}
	return msg
}

// Lang bildirim metinlerinin dili: ayarlardaki bildirim dili
// (AppSettings.NotifyLang); okunamazsa varsayılan (tr).
func (d *Dispatcher) Lang(ctx context.Context) string {
	st, err := d.store.LoadSettings(ctx)
	if err != nil {
		return i18n.Default
	}
	return i18n.Or(st.NotifyLang)
}

// Test verilen ayarla hemen bir test bildirimi gönderir ve sonucu döner.
func (d *Dispatcher) Test(typ string, cfg json.RawMessage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	lang := d.Lang(ctx)
	cancel()
	return d.send(typ, cfg, Event{Kind: KindTest, MonitorName: "Test", Time: time.Now(), Lang: lang})
}

// SendSamples örnek olayları arka planda, aralarında gap bekleyerek sırayla
// gönderir (mesajlaşma servisleri art arda gelen mesajları sınırlayabilir).
// done her olayın sonucuyla (nil: gönderildi) çağrılır. Kapanışta Wait bekler.
func (d *Dispatcher) SendSamples(typ string, cfg json.RawMessage, events []Event, gap time.Duration, done func(Event, error)) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		for i, ev := range events {
			if i > 0 {
				time.Sleep(gap)
			}
			done(ev, d.send(typ, cfg, ev))
		}
	}()
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

// Stop kapanışı bildirir: yeniden deneme için bekleyen gönderimler beklemeyi
// keser (son deneme yapılmaz). Ardından Wait çağrılır.
func (d *Dispatcher) Stop() { d.stopOnce.Do(func() { close(d.stop) }) }

// Wait gönderilmekte olan bildirimlerin bitmesini bekler (en fazla timeout).
func (d *Dispatcher) Wait(timeout time.Duration) {
	done := make(chan struct{})
	go func() { d.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}
