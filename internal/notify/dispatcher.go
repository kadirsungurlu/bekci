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

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// sendTimeout tek bir kanala gönderim için süre sınırı.
const sendTimeout = 30 * time.Second

// defaultRetryDelays gerçek olay bildirimlerinde geçici hatalardan (ağ hatası,
// HTTP 5xx/429) sonra yeniden deneme bekleme süreleri: toplam 3 deneme. Test
// ve örnek bildirimleri yeniden denenmez (sonuç kullanıcıya hemen gösterilir).
var defaultRetryDelays = []time.Duration{5 * time.Second, 20 * time.Second}

// defaultSweepEvery ertelenmiş bildirim kuyruğunun ve eskalasyonların tarama aralığı.
const defaultSweepEvery = 15 * time.Second

// Dispatcher olayları monitöre bağlı kanallara arka planda gönderir.
// Yavaş bir kanal kontrol motorunu asla bekletmez.
//
// Kural hattı (kanal başına, sırayla; bkz. store/notification_rules.go):
//  1. Olay türü süzgeci (events): kanal bu türü almıyorsa atlanır.
//  2. Eşleştirme: gecikme veya sessiz saat kuralı olan kanalda düzelme ve
//     hatırlatma bildirimi yalnızca sorun bildirimi gönderilmişse gider
//     (🔴 gitmediyse 🟢 de gitmez).
//  3. Gecikme (delay_min): sorun başlangıcı kuyruğa alınır; vadesinde olay
//     hâlâ açıksa gönderilir, kapandıysa iptal edilir.
//  4. Sessiz saatler: kritik kipte 🔴 türler ve onların düzelmesi geçer;
//     diğer sorun başlangıçları pencerenin bitimine ertelenir, hatırlatmalar
//     atılır. "Hiçbiri" kipinde her şey ertelenir (hatırlatma hariç).
//  5. Gönderim (yeniden denemeli); sonuç olayın işlem geçmişine ve teslimat
//     tablosuna yazılır.
//
// Eskalasyon kanalları (escalate_min > 0) tarama turunda ele alınır: N
// dakikadır açık olan ve bu kanala henüz bildirilmemiş her olay için sorun
// bildirimi üretilir; olay kapanınca düzelme de aynı kanala gider.
// Test ve örnek bildirimleri kural hattından geçmez.
type Dispatcher struct {
	store *store.Store
	log   *slog.Logger
	wg    sync.WaitGroup

	stop     chan struct{} // kapanış: bekleyen yeniden denemeler iptal edilir
	stopOnce sync.Once

	retryDelays []time.Duration // yeniden deneme beklemeleri (testler kısaltır/kapatır)
	sweepEvery  time.Duration   // kuyruk/eskalasyon tarama aralığı (testler kısaltır)
	now         func() time.Time
	baseURL     string // eskalasyon bildirimlerindeki bağlantılar için

	sweepMu sync.Mutex // taramalar üst üste binmez

	escMu   sync.Mutex
	escDone map[[2]int64]bool // bu açılışta ele alınan (olay, kanal) eskalasyonları
}

func NewDispatcher(s *store.Store, log *slog.Logger) *Dispatcher {
	return &Dispatcher{store: s, log: log, stop: make(chan struct{}), retryDelays: defaultRetryDelays,
		sweepEvery: defaultSweepEvery, now: time.Now, escDone: map[[2]int64]bool{}}
}

// SetBaseURL eskalasyon bildirimlerindeki monitör/olay bağlantıları için dış adres.
func (d *Dispatcher) SetBaseURL(u string) { d.baseURL = strings.TrimRight(u, "/") }

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
// gönderir (bloklamaz). Düzelme bildirimleri, sorun bildirimini eskalasyonla
// almış kanallara da gider.
func (d *Dispatcher) Notify(ev Event) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		var channels []store.Notification
		var err error
		if ev.ProbeID != 0 {
			channels, err = d.store.NotificationsForProbe(ctx, ev.ProbeID)
		} else {
			channels, err = d.store.NotificationsForMonitor(ctx, ev.MonitorID)
		}
		if err != nil {
			d.log.Error("bildirim kanalları okunamadı", "monitor", ev.MonitorID, "sunucu", ev.ProbeID, "hata", err)
			return
		}
		if ev.Lang == "" {
			ev.Lang = d.Lang(ctx)
		}
		// Onaylı ya da susturulmuş olayın hatırlatması hiçbir kanala gitmez
		// (motor zaten üretmez; başka bir yoldan gelirse burada da durur).
		if ev.Kind == KindReminder && ev.IncidentID != 0 {
			if why := d.mutedReason(ctx, ev.IncidentID, d.now()); why != "" {
				d.incidentEvent(ev, deliveryData{Event: ev.Kind, Skipped: why})
				return
			}
		}
		if problem := ProblemOf(ev.Kind); problem != "" && ev.IncidentID != 0 && ev.Kind != KindReminder {
			// Olay kapandı: gönderilmeyi bekleyen sorun bildirimleri iptal edilir;
			// sorun bildirimini eskalasyonla almış (bağlı olmayan) kanallar düzelmeyi de alır.
			d.cancelQueued(ctx, ev, problem)
			channels = d.withDelivered(ctx, channels, ev.IncidentID, problem)
		}
		if ev.Kind == KindAcked {
			// Onay bildirimi yalnızca bu türü açıkça seçen kanallara gider;
			// almayan kanallar için işlem geçmişine "atlandı" satırı yazılmaz
			// (her onayda kanal sayısı kadar gürültü olurdu).
			var opted []store.Notification
			for _, ch := range channels {
				if ch.Accepts(KindAcked) {
					opted = append(opted, ch)
				}
			}
			channels = opted
			if len(channels) == 0 {
				return
			}
		}
		// Monitör ve sunucu olaylarında gönderim sonucu olayın işlem geçmişine yazılır.
		if ev.IncidentID != 0 && len(channels) == 0 {
			d.incidentEvent(ev, deliveryData{Event: ev.Kind, None: true})
		}
		var inner sync.WaitGroup
		for _, ch := range channels {
			inner.Add(1)
			go func(ch store.Notification) {
				defer inner.Done()
				d.deliver(ch, ev, 0)
			}(ch)
		}
		inner.Wait()
	}()
}

// withDelivered olayın sorun bildirimini almış ama listede olmayan kanalları
// (eskalasyon) ekler.
func (d *Dispatcher) withDelivered(ctx context.Context, channels []store.Notification, incidentID int64, problem string) []store.Notification {
	ids, err := d.store.DeliveredChannels(ctx, incidentID, problem)
	if err != nil || len(ids) == 0 {
		return channels
	}
	have := map[int64]bool{}
	for _, ch := range channels {
		have[ch.ID] = true
	}
	var missing []int64
	for _, id := range ids {
		if !have[id] {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return channels
	}
	extra, err := d.store.NotificationsByIDs(ctx, missing)
	if err != nil {
		return channels
	}
	return append(channels, extra...)
}

// cancelQueued olay kapanınca bekleyen sorun bildirimlerini siler ve işlem
// geçmişine yazar: gönderilmeyen 🔴 için 🟢 de gitmeyecek.
func (d *Dispatcher) cancelQueued(ctx context.Context, ev Event, problem string) {
	rows, err := d.store.QueuedForIncident(ctx, ev.IncidentID)
	if err != nil {
		d.log.Error("bekleyen bildirimler okunamadı", "olay", ev.IncidentID, "hata", err)
		return
	}
	for _, q := range rows {
		if !IsProblemStart(q.Event) {
			continue
		}
		if err := d.store.DeleteQueued(ctx, q.ID); err != nil {
			continue
		}
		name, typ := d.channelName(ctx, q.NotificationID)
		d.incidentEvent(ev, deliveryData{Event: q.Event, ChannelID: q.NotificationID, Channel: name, Type: typ, Skipped: SkipCancelled, Reason: q.Reason})
	}
}

func (d *Dispatcher) channelName(ctx context.Context, id int64) (string, string) {
	chs, err := d.store.NotificationsByIDs(ctx, []int64{id})
	if err != nil || len(chs) == 0 {
		return "", ""
	}
	return chs[0].Name, chs[0].Type
}

// Atlama nedenleri (işlem geçmişindeki bildirim kaydının skipped alanı).
const (
	SkipFilter    = "filter"    // kanal bu olay türünü almıyor
	SkipUnpaired  = "unpaired"  // sorun bildirimi gitmediği için düzelme/hatırlatma da gitmedi
	SkipQuiet     = "quiet"     // sessiz saatler (hatırlatma)
	SkipCancelled = "cancelled" // ertelenmiş sorun bildirimi, olay kapandığı için iptal
	SkipDuplicate = "duplicate" // aynı kanala bu olay için zaten gönderilmiş (eskalasyon/gecikme çakışması)
	SkipLevel     = "level"     // kanal bu sunucuda yalnızca başka seviyeyi alıyor
	SkipAcked     = "acked"     // olay onaylandı: hatırlatma / eskalasyon gitmez
	SkipSnoozed   = "snoozed"   // olay susturuldu: süre dolunca devam eder
)

// mutedReason olayın hatırlatma/eskalasyonu susturulmuşsa nedeni (SkipAcked
// | SkipSnoozed), değilse "".
func (d *Dispatcher) mutedReason(ctx context.Context, incidentID int64, now time.Time) string {
	if incidentID == 0 {
		return ""
	}
	inc, err := d.store.GetIncident(ctx, incidentID)
	if err != nil || inc.ResolvedAt != 0 {
		return ""
	}
	switch {
	case inc.AckedAt > 0:
		return SkipAcked
	case inc.SnoozedUntil > now.Unix():
		return SkipSnoozed
	}
	return ""
}

// deliver kural hattını uygular ve gerekirse gönderir. queueID 0 değilse olay
// kuyruktan geliyor (gecikme yeniden uygulanmaz; erteleme kaydı yerinde güncellenir).
func (d *Dispatcher) deliver(ch store.Notification, ev Event, queueID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	now := d.now()
	if ch.Lang != "" {
		ev.Lang = i18n.Or(ch.Lang)
	}
	base := deliveryData{Event: ev.Kind, ChannelID: ch.ID, Channel: ch.Name, Type: ch.Type}
	finish := func() {
		if queueID != 0 {
			d.store.DeleteQueued(ctx, queueID)
		}
	}
	skip := func(reason string) {
		dd := base
		dd.Skipped = reason
		d.incidentEvent(ev, dd)
		finish()
	}
	// 1. Olay türü süzgeci; sunucu bağında seviye süzgeci (kanal yalnızca
	// uyarı ya da yalnızca kritik alıyorsa; düzelmede olayın en yüksek seviyesi de sayılır).
	if !ch.Accepts(ev.Kind) {
		skip(SkipFilter)
		return
	}
	if ch.BindLevel != "" && ev.ProbeID != 0 && (ev.Kind == KindServerAlert || ev.Kind == KindServerResolved) {
		lvl := ev.Level
		if lvl == "" {
			lvl = LevelCritical
		}
		if ch.BindLevel != lvl && ch.BindLevel != ev.PeakLevel {
			skip(SkipLevel)
			return
		}
	}
	// 2. Eşleştirme: kurallı kanalda 🟢 ve hatırlatma yalnızca 🔴 gittiyse.
	if problem := ProblemOf(ev.Kind); problem != "" && ev.IncidentID != 0 && ch.HasRules() {
		ok, err := d.store.Delivered(ctx, ev.IncidentID, ch.ID, problem)
		if err != nil {
			d.log.Error("teslimat kaydı okunamadı", "olay", ev.IncidentID, "hata", err)
		} else if !ok {
			skip(SkipUnpaired)
			return
		}
	}
	// Kuyruktan gelen sorun bildirimi: olay bu arada kapanmış ya da aynı kanala
	// başka yoldan (eskalasyon) gitmiş olabilir.
	if queueID != 0 && IsProblemStart(ev.Kind) && ev.IncidentID != 0 {
		inc, err := d.store.GetIncident(ctx, ev.IncidentID)
		if err != nil || inc.ResolvedAt != 0 {
			skip(SkipCancelled)
			return
		}
		if done, _ := d.store.Delivered(ctx, ev.IncidentID, ch.ID, ev.Kind); done {
			skip(SkipDuplicate)
			return
		}
		ev.Elapsed = now.Sub(time.Unix(inc.StartedAt, 0))
	}
	// 3. Gecikme.
	if queueID == 0 && ch.DelayMin > 0 && IsProblemStart(ev.Kind) && ev.IncidentID != 0 {
		d.defer_(ctx, ch, ev, store.QueueDelay, now.Add(time.Duration(ch.DelayMin)*time.Minute), 0)
		return
	}
	// 4. Sessiz saatler.
	if ch.Quiet != nil {
		if active, end := ch.Quiet.Active(now); active {
			switch {
			case ch.Quiet.Mode == store.QuietCritical && (IsCritical(ev.Kind) || IsCritical(ProblemOf(ev.Kind)) && ev.Kind != KindReminder):
				// 🔴 ve onların 🟢'si sessiz saatte de geçer.
			case ev.Kind == KindReminder:
				skip(SkipQuiet)
				return
			default:
				d.defer_(ctx, ch, ev, store.QueueQuiet, end, queueID)
				return
			}
		}
	}
	// 5. Gönderim.
	if queueID != 0 && !ev.Escalated {
		ev.Delayed = true
	}
	attempts, err := d.sendRetry(ch.Type, ch.Config, ev)
	if ev.IncidentID != 0 {
		if rerr := d.store.RecordDelivery(ctx, ev.IncidentID, ch.ID, ev.Kind, now.Unix()); rerr != nil {
			d.log.Error("teslimat kaydı yazılamadı", "olay", ev.IncidentID, "hata", rerr)
		}
		dd := base
		dd.OK, dd.Escalated, dd.Delayed = err == nil, ev.Escalated, ev.Delayed
		if ev.Delayed {
			dd.Reason = ev.DeferReason
		}
		if attempts > 1 {
			dd.Attempts = attempts
		}
		if err != nil {
			dd.Error = SanitizeSendError(ch.Type, ch.Config, err)
		}
		d.incidentEvent(ev, dd)
	}
	finish()
	if err != nil {
		d.log.Warn("bildirim gönderilemedi", "kanal", ch.Name, "tip", ch.Type, "olay", ev.Kind, "monitor", ev.MonitorName, "deneme", attempts, "hata", err)
		return
	}
	d.log.Info("bildirim gönderildi", "kanal", ch.Name, "olay", ev.Kind, "monitor", ev.MonitorName, "deneme", attempts, "ertelenmis", ev.Delayed, "eskalasyon", ev.Escalated)
}

// defer_ bildirimi kuyruğa alır (queueID doluysa kaydın vadesini günceller)
// ve işlem geçmişine ertelemeyi yazar.
func (d *Dispatcher) defer_(ctx context.Context, ch store.Notification, ev Event, reason string, due time.Time, queueID int64) {
	ev.DeferReason = reason
	if queueID != 0 {
		// Zaten kuyrukta (ör. gecikme bitti ama sessiz saatteyiz): vade güncellenir, kayıt tekrarlanmaz.
		if err := d.store.RescheduleQueued(ctx, queueID, due.Unix(), reason); err != nil {
			d.log.Error("bildirim ertelenemedi", "kanal", ch.Name, "hata", err)
		}
		return
	} else {
		payload, err := json.Marshal(ev)
		if err != nil {
			d.log.Error("bildirim kuyruğa yazılamadı", "kanal", ch.Name, "hata", err)
			return
		}
		added, err := d.store.EnqueueNotification(ctx, store.QueuedNotification{
			IncidentID: ev.IncidentID, NotificationID: ch.ID, Event: ev.Kind, Reason: reason,
			DueAt: due.Unix(), CreatedAt: d.now().Unix(), Payload: string(payload),
		})
		if err != nil {
			d.log.Error("bildirim kuyruğa yazılamadı", "kanal", ch.Name, "hata", err)
			return
		}
		if !added {
			return // aynı olay için zaten bekliyor
		}
	}
	d.log.Info("bildirim ertelendi", "kanal", ch.Name, "olay", ev.Kind, "monitor", ev.MonitorName, "neden", reason, "vade", due.Format(time.RFC3339))
	d.incidentEvent(ev, deliveryData{Event: ev.Kind, ChannelID: ch.ID, Channel: ch.Name, Type: ch.Type, Deferred: due.Unix(), Reason: reason})
}

// Run kuyruk ve eskalasyon taramasını ctx iptal edilene kadar sürdürür.
func (d *Dispatcher) Run(ctx context.Context) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		t := time.NewTicker(d.sweepEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				d.Sweep(ctx)
			}
		}
	}()
}

// Sweep tek tarama turu (testlerden de çağrılır): vadesi gelen ertelenmiş
// bildirimler gönderilir, eskalasyon kanalları için açık olaylar denetlenir.
func (d *Dispatcher) Sweep(ctx context.Context) {
	if !d.sweepMu.TryLock() {
		return
	}
	defer d.sweepMu.Unlock()
	now := d.now()
	due, err := d.store.DueNotifications(ctx, now.Unix(), 200)
	if err != nil {
		if ctx.Err() == nil {
			d.log.Error("bildirim kuyruğu okunamadı", "hata", err)
		}
		return
	}
	for _, q := range due {
		chs, err := d.store.NotificationsByIDs(ctx, []int64{q.NotificationID})
		if err != nil {
			continue
		}
		var ev Event
		if json.Unmarshal([]byte(q.Payload), &ev) != nil || len(chs) == 0 {
			// Kanal silinmiş/kapatılmış ya da kayıt bozuk: atılır.
			d.store.DeleteQueued(ctx, q.ID)
			continue
		}
		ev.DeferReason = q.Reason
		d.deliver(chs[0], ev, q.ID)
	}
	d.escalate(ctx, now)
}

// escalationKinds eskalasyonun kapsadığı olay türleri.
var escalationKinds = []string{store.IncidentMonitor, store.IncidentServerOffline, store.IncidentServerAlert, store.IncidentProbeOffline}

// escalate eskalasyon kanalları için N dakikadır açık olan olayları bildirir.
func (d *Dispatcher) escalate(ctx context.Context, now time.Time) {
	channels, err := d.store.NotificationsEscalating(ctx)
	if err != nil || len(channels) == 0 {
		return
	}
	for _, ch := range channels {
		incs, err := d.store.OpenIncidentsBefore(ctx, escalationKinds, now.Add(-time.Duration(ch.EscalateMin)*time.Minute).Unix())
		if err != nil {
			continue
		}
		for _, inc := range incs {
			key := [2]int64{inc.ID, ch.ID}
			d.escMu.Lock()
			done := d.escDone[key]
			d.escMu.Unlock()
			if done {
				continue
			}
			// Onaylı / susturulmuş olay eskalasyona gitmez; işaretlenmez ki
			// onay geri alınınca ya da susturma bitince eskalasyon yapılsın.
			if inc.Muted(now.Unix()) {
				continue
			}
			ev, ok := d.eventFromIncident(ctx, inc)
			if !ok {
				continue
			}
			if sent, _ := d.store.Delivered(ctx, inc.ID, ch.ID, ev.Kind); sent {
				d.markEscalated(key)
				continue // bağlı kanal zaten aldı (ya da daha önce eskalasyon gitti)
			}
			if queued, _ := d.store.QueuedExists(ctx, inc.ID, ch.ID, ev.Kind); queued {
				continue // gecikme / sessiz saat ertelemesi bekliyor
			}
			ev.Lang = d.Lang(ctx)
			ev.Escalated = true
			ev.Elapsed = now.Sub(time.Unix(inc.StartedAt, 0))
			d.markEscalated(key)
			d.deliver(ch, ev, 0)
		}
	}
}

func (d *Dispatcher) markEscalated(key [2]int64) {
	d.escMu.Lock()
	d.escDone[key] = true
	d.escMu.Unlock()
}

// eventFromIncident açık olaydan sorun bildirimini yeniden kurar (eskalasyon).
func (d *Dispatcher) eventFromIncident(ctx context.Context, inc store.Incident) (Event, bool) {
	ev := Event{Time: time.Unix(inc.StartedAt, 0), Message: inc.Cause, IncidentID: inc.ID}
	if d.baseURL != "" {
		ev.IncidentURL = fmt.Sprintf("%s/#/incidents/%d", d.baseURL, inc.ID)
	}
	switch inc.Kind {
	case store.IncidentMonitor:
		m, err := d.store.GetMonitor(ctx, inc.MonitorID)
		if err != nil {
			return ev, false
		}
		ev.Kind, ev.MonitorID, ev.MonitorName, ev.MonitorType = KindDown, m.ID, m.Name, m.Type
		if c, ok := check.Get(m.Type); ok {
			ev.Target = c.Target(m.Config)
		}
		if d.baseURL != "" {
			ev.URL = fmt.Sprintf("%s/#/monitors/%d", d.baseURL, m.ID)
		}
	case store.IncidentServerOffline, store.IncidentServerAlert, store.IncidentProbeOffline:
		p, err := d.store.GetProbe(ctx, inc.ServerID)
		if err != nil {
			return ev, false
		}
		data := store.ParseServerIncidentData(inc.Data)
		ev.ProbeID, ev.MonitorName, ev.Metric = p.ID, p.Name, data.Metric
		ev.Value, ev.Threshold, ev.Minutes, ev.Mount = data.Value, data.Threshold, data.Minutes, data.Mount
		if data.LastSeen > 0 {
			ev.LastSeen = time.Unix(data.LastSeen, 0)
		}
		if inc.Kind == store.IncidentProbeOffline {
			ev.Kind, ev.MonitorType, ev.Target, ev.Metric = KindProbeOffline, "probe", p.LastIP, "offline"
			if d.baseURL != "" {
				ev.URL = d.baseURL + "/#/settings/probes"
			}
		} else {
			ev.Kind, ev.MonitorType = KindServerAlert, "server"
			if inc.Kind == store.IncidentServerOffline {
				ev.Metric = "offline"
			}
			var h struct {
				Hostname string `json:"hostname"`
			}
			if p.HostInfo != "" && json.Unmarshal([]byte(p.HostInfo), &h) == nil {
				ev.Target = h.Hostname
			}
			if d.baseURL != "" {
				ev.URL = fmt.Sprintf("%s/#/servers/%d", d.baseURL, p.ID)
			}
		}
	default:
		return ev, false
	}
	return ev, true
}

// deliveryData olayın işlem geçmişindeki bildirim kaydının data alanı.
type deliveryData struct {
	Event     string `json:"event"` // down | up | reminder | …
	ChannelID int64  `json:"channel_id,omitempty"`
	Channel   string `json:"channel,omitempty"`
	Type      string `json:"type,omitempty"`
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	None      bool   `json:"none,omitempty"`     // monitöre bağlı etkin kanal yok
	Attempts  int    `json:"attempts,omitempty"` // birden fazla denendiyse deneme sayısı
	// Kural hattı: Skipped atlama nedeni (filter | unpaired | quiet | cancelled |
	// duplicate); Deferred ertelendiği vade (unix) ve Reason (delay | quiet);
	// Escalated / Delayed gönderimin niteliği.
	Skipped   string `json:"skipped,omitempty"`
	Deferred  int64  `json:"deferred,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Escalated bool   `json:"escalated,omitempty"`
	Delayed   bool   `json:"delayed,omitempty"`
}

// incidentEvent gönderim sonucunu olayın işlem geçmişine yazar. Zaman gönderimin
// bittiği andır (yavaş kanal geç görünür).
func (d *Dispatcher) incidentEvent(ev Event, dd deliveryData) {
	if ev.IncidentID == 0 {
		return
	}
	msg := "Bildirim gönderildi"
	switch {
	case dd.None:
		msg = "Bağlı etkin bildirim kanalı yok"
	case dd.Deferred != 0:
		msg = "Bildirim ertelendi"
	case dd.Skipped != "":
		msg = "Bildirim gönderilmedi"
	case !dd.OK:
		msg = "Bildirim gönderilemedi"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := d.store.AddIncidentEvents(ctx, ev.IncidentID, store.IncidentEvent{
		Time: d.now().Unix(), Kind: store.EventNotify, Message: msg, Data: store.EventData(dd),
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
// Kural hattından geçmez (süzgeç, sessiz saat, gecikme uygulanmaz).
func (d *Dispatcher) Test(typ string, cfg json.RawMessage) error {
	return d.TestLang(typ, cfg, "")
}

// TestLang Test gibi; lang doluysa metin o dilde (kanalın dili).
func (d *Dispatcher) TestLang(typ string, cfg json.RawMessage, lang string) error {
	if lang == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		lang = d.Lang(ctx)
		cancel()
	}
	return d.send(typ, cfg, Event{Kind: KindTest, MonitorName: "Test", Time: d.now(), Lang: i18n.Or(lang)})
}

// SendSamples örnek olayları arka planda, aralarında gap bekleyerek sırayla
// gönderir (mesajlaşma servisleri art arda gelen mesajları sınırlayabilir).
// done her olayın sonucuyla (nil: gönderildi) çağrılır. Kapanışta Wait bekler.
// Kural hattından geçmez.
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
