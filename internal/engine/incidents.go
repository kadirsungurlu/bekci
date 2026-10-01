package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Olay ayrıntıları (docs/PLAN.md §13) ------------------------------------------------
//
// Runner olayın işlem geçmişini (incident_events) ve olayı açan kontrolün
// istek/yanıt kaydını (incident_captures) yazar. Olay açılmadan önceki
// başarısız denemeler bellekte tutulur ve olay açılınca kendi zamanlarıyla
// yazılır; olay açılmadan monitör düzelirse atılır. Bildirim gönderim
// sonuçlarını dağıtıcı (notify) yazar. Yazma hataları yalnızca loglanır:
// işlem geçmişi izlemenin kendisini asla durdurmaz.

// maxPreDown olay açılmadan önce bellekte tutulan en fazla deneme kaydı.
const maxPreDown = 20

// IncidentURL arayüzdeki olay sayfasının adresi.
func (e *Engine) IncidentURL(id int64) string {
	if e.cfg.BaseURL == "" {
		return ""
	}
	return e.cfg.BaseURL + "/#/incidents/" + strconv.FormatInt(id, 10)
}

// maxOtherCaptures olay kaydına eklenen diğer konumların en fazla ayrıntı sayısı.
const maxOtherCaptures = 3

// captureData incident_captures.data: olayı açan (öncelikli konumun) ayrıntısı;
// çok konumluda diğer çalışmayan konumların ayrıntıları "others" altında
// (eski kayıtlarda yok). check.Detail alanları üst düzeyde kalır: eski
// okuyucular kaydı değişmeden çözer.
type captureData struct {
	*check.Detail
	Others []store.IncidentCapture `json:"others,omitempty"`
}

// incidentLocation olay başındaki bir konumun durumu (down kaydının data'sı).
type incidentLocation struct {
	ProbeID int64  `json:"probe_id"`
	Name    string `json:"name"`
	Status  string `json:"status"` // up | down | retrying | unknown
	Message string `json:"message,omitempty"`
}

// addEvents işlem geçmişine yazar (hata yalnızca loglanır).
func (r *runner) addEvents(ctx context.Context, id int64, evs ...store.IncidentEvent) {
	if id == 0 || len(evs) == 0 {
		return
	}
	if err := r.e.store.AddIncidentEvents(ctx, id, evs...); err != nil {
		r.e.log.Error("olay kaydı yazılamadı", "monitor", r.m.Name, "hata", err)
	}
}

// rememberRetry olay açılmadan önceki başarısız denemeyi bellekte tutar.
func (r *runner) rememberRetry(now time.Time, res check.Result) {
	ev := store.IncidentEvent{Time: now.Unix(), Kind: store.EventRetry, Message: res.Message}
	if r.locs == nil {
		ev.Location = LocalName
		ev.Data = store.EventData(map[string]int{"attempt": r.retries, "max": r.m.MaxRetries})
	}
	if len(r.preDown) >= maxPreDown {
		r.preDown = r.preDown[1:]
	}
	r.preDown = append(r.preDown, ev)
}

// openIncident olayı açar; bekleyen denemeleri, başlangıç kaydını ve istek/yanıt
// kaydını yazar. Olay açılamazsa incidentID 0 kalır (bildirim yine gider).
func (r *runner) openIncident(ctx context.Context, now time.Time, res check.Result) {
	pre := r.preDown
	r.preDown = nil
	id, err := r.e.store.StartIncident(ctx, r.m.ID, now.Unix(), res.Message)
	if err != nil {
		r.e.log.Error("olay açılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	r.incidentID, r.lastCause, r.maintLogged, r.locLearn = id, res.Message, false, false
	if r.unresolved != nil && r.unresolved.id == id {
		// Eski olay hâlâ kapatılamadı ve yeni kesinti onun devamı sayıldı
		// (açık olay varken yenisi açılmaz): artık kapatılmaya çalışılmaz.
		r.unresolved = nil
	}

	var locs []incidentLocation
	where := LocalName
	c := locCapture{detail: res.Detail, at: now, loc: LocalName}
	if r.locs == nil {
		locs = []incidentLocation{{ProbeID: LocalProbeID, Name: LocalName, Status: locDown, Message: res.Message}}
	} else {
		var failing []string
		locs, failing = r.locationSnapshot(now, &r.locPrev)
		where = nameList(failing)
		c = r.failingCapture(now)
	}
	evs := append(pre, store.IncidentEvent{
		Time: now.Unix(), Kind: store.EventDown, Location: where, Message: res.Message,
		Data: store.EventData(downData{Locations: locs, Monitor: r.snapshot()}),
	})
	r.addEvents(ctx, id, evs...)
	r.saveCapture(ctx, id, c)
}

// MonitorSnapshot olay başladığı andaki monitör (başlangıç kaydının
// data.monitor alanı): olay ayrıntısı, monitör sonradan düzenlense de olayın
// yaşandığı tip ve hedefi gösterir.
type MonitorSnapshot struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
}

// downData olayın başlangıç kaydının data alanı: konumların o anki durumu ve monitör.
type downData struct {
	Locations []incidentLocation `json:"locations"`
	Monitor   MonitorSnapshot    `json:"monitor"`
}

func (r *runner) snapshot() MonitorSnapshot {
	return MonitorSnapshot{Name: r.m.Name, Type: r.m.Type, Target: Target(r.m)}
}

// locCapture olay kaydına yazılacak istek/yanıt ayrıntısı.
type locCapture struct {
	detail *check.Detail
	at     time.Time
	loc    string
	others []store.IncidentCapture
}

// locationSnapshot konumların şu anki durumu (olayın başlangıç kaydı için)
// ve çalışmayanların adları; prev işlem geçmişine yazılan son durumlar olarak kurulur.
func (r *runner) locationSnapshot(now time.Time, prev *map[int64]locMark) ([]incidentLocation, []string) {
	st := r.locs.statuses(now, r.rules())
	var locs []incidentLocation
	var failing []string
	*prev = make(map[int64]locMark, len(st))
	for _, s := range st {
		locs = append(locs, incidentLocation{ProbeID: s.ProbeID, Name: s.Name, Status: s.Status, Message: s.Message})
		(*prev)[s.ProbeID] = locMark{s.Status, s.Message}
		if s.Status == locDown {
			failing = append(failing, s.Name)
		}
	}
	return locs, failing
}

// failingCapture çalışmayan konumların istek/yanıt ayrıntıları. Öncelik: ana
// sunucu, sonra sıradaki ilk çalışmayan konum. Diğer çalışmayan konumların
// ayrıntıları da (en fazla maxOtherCaptures) aynı kayda eklenir: aynı hata
// farklı yerlerden karşılaştırılabilsin.
func (r *runner) failingCapture(now time.Time) locCapture {
	var c locCapture
	for _, l := range r.locs.locs {
		if l.detail == nil || classify(l, now, r.rules()) != locDown {
			continue
		}
		if c.detail == nil {
			c.detail, c.at, c.loc = l.detail, l.at, l.name
			continue
		}
		if len(c.others) < maxOtherCaptures {
			if b, err := json.Marshal(l.detail); err == nil {
				c.others = append(c.others, store.IncidentCapture{Time: l.at.Unix(), Location: l.name, Detail: b})
			}
		}
	}
	return c
}

// saveCapture olayın istek/yanıt kaydını yazar (yoksa bir şey yapmaz).
func (r *runner) saveCapture(ctx context.Context, id int64, c locCapture) {
	if c.detail == nil || id == 0 {
		return
	}
	b, err := json.Marshal(captureData{Detail: c.detail, Others: c.others})
	if err == nil {
		err = r.e.store.SaveIncidentCapture(ctx, id, c.at.Unix(), c.loc, b)
	}
	if err != nil {
		r.e.log.Error("olayın istek/yanıt kaydı yazılamadı", "monitor", r.m.Name, "hata", err)
	}
}

// incidentProgress olay sürerken değişenleri (bakım, konum durumu, hata
// mesajı) işlem geçmişine yazar. status bu kontrolün kaydedilen durumudur.
func (r *runner) incidentProgress(ctx context.Context, now time.Time, status int, inMaint bool, res check.Result) {
	if r.incidentID == 0 {
		return
	}
	// Bakım kaydı idempotenttir (MarkIncidentMaint): bakım dizini
	// yenilenirken (ReloadMaintenance) veya yeniden başlatmadan önce zaten
	// yazıldıysa ikinci kez yazılmaz.
	switch {
	case inMaint && !r.maintLogged:
		r.maintLogged = true
		r.markMaint(ctx, now, true)
	case !inMaint && r.maintLogged:
		r.maintLogged = false
		r.markMaint(ctx, now, false)
	}
	if inMaint {
		return
	}
	var evs []store.IncidentEvent
	// Çok konumluda birleşik mesaj konum listesini de içerir; değişimler konum
	// başına yazılır (locationChanges).
	if r.locs != nil {
		// Yeniden başlatmadan sonra konumların ilk sonuçları gelene kadar (son
		// bekleyen konumun sonucunun geldiği çağrı dahil) yalnızca mevcut durum
		// öğrenilir: olay sürerken zaten yazılmış "çalışmıyor" kaydı, konum
		// sonuçları yeniden gelince ikinci kez yazılmasın.
		learn := r.locLearn
		if learn && !unsettled(r.locs.statuses(now, r.rules())) {
			r.locLearn = false
		}
		evs = append(evs, r.locationChanges(now, &r.locPrev, learn)...)
	} else if status == store.StatusDown && res.Message != r.lastCause {
		r.lastCause = res.Message
		evs = append(evs, store.IncidentEvent{Time: now.Unix(), Kind: store.EventChange, Message: res.Message})
	}
	r.addEvents(ctx, r.incidentID, evs...)
}

// unsettled sonucu henüz kesinleşmemiş (tekrar deneniyor / ilk sonuç
// bekleniyor) konum var mı.
func unsettled(st []LocationStatus) bool {
	for _, s := range st {
		if s.Status == locRetrying || s.Status == locWaiting {
			return true
		}
	}
	return false
}

// locMark bir konumun işlem geçmişine en son yazılan durumu ve hatası.
type locMark struct{ status, msg string }

// markMaint açık olayın işlem geçmişine bakım başlangıcını/bitişini yazar (hata yalnızca loglanır).
func (r *runner) markMaint(ctx context.Context, now time.Time, start bool) {
	if _, err := r.e.store.MarkIncidentMaint(ctx, r.incidentID, now.Unix(), start); err != nil {
		r.e.log.Error("olay kaydı yazılamadı", "monitor", r.m.Name, "hata", err)
	}
}

// locationChanges olay sürerken durumu (veya çalışmazken hatası) değişen
// konumlar; prev işlem geçmişine yazılan son durumlardır. "Tekrar deneniyor"
// ve "ilk sonuç bekleniyor" ara durumdur, kaydedilmez; yeniden başlatma
// sonrası ilk çağrı yalnızca mevcut durumu öğrenir. learn: bu çağrıda da
// yalnızca öğrenilir (kısmi olayda konumların ilk sonuçları beklenirken).
func (r *runner) locationChanges(now time.Time, prevp *map[int64]locMark, learn bool) []store.IncidentEvent {
	st := r.locs.statuses(now, r.rules())
	first := *prevp == nil || learn
	if *prevp == nil {
		*prevp = make(map[int64]locMark, len(st))
	}
	prevs := *prevp
	var out []store.IncidentEvent
	for _, s := range st {
		prev := prevs[s.ProbeID]
		if s.Status == locRetrying || s.Status == locWaiting || (prev.status == s.Status && (s.Status != locDown || prev.msg == s.Message)) {
			continue
		}
		prevs[s.ProbeID] = locMark{s.Status, s.Message}
		if first {
			continue
		}
		if s.Status == locDown && prev.status == locDown {
			out = append(out, store.IncidentEvent{Time: now.Unix(), Kind: store.EventChange, Location: s.Name, Message: s.Message})
			continue
		}
		msg := "Kontrol noktasına ulaşılamıyor"
		switch s.Status {
		case locUp:
			msg = "Çalışıyor"
		case locDown:
			msg = "Çalışmıyor"
			if s.Message != "" {
				msg += ": " + s.Message
			}
		}
		// Data: arayüz metni dile göre kurabilsin diye durum ve ham kontrol mesajı
		// (Message Türkçe özet olarak kalır; eski kayıtlarda data.message yok).
		out = append(out, store.IncidentEvent{Time: now.Unix(), Kind: store.EventLocation, Location: s.Name,
			Message: msg, Data: store.EventData(map[string]string{"status": s.Status, "message": s.Message})})
	}
	return out
}

// retryResolve UP'a dönüşte kapatılamamış olayı (r.unresolved) çözülme
// anıyla kapatır ve çözülme kaydını yazar. Hata olursa sonraki sonuçta
// yeniden denenir.
func (r *runner) retryResolve(ctx context.Context) {
	u := r.unresolved
	if u == nil {
		return
	}
	started, err := r.e.store.ResolveIncident(ctx, r.m.ID, u.at.Unix())
	if err != nil {
		r.e.log.Error("olay kapatılamadı, sonraki kontrolde yeniden denenecek", "monitor", r.m.Name, "hata", err)
		return
	}
	r.unresolved = nil
	if started == 0 {
		return // bu arada başka yoldan kapatılmış
	}
	downtime := u.at.Sub(time.Unix(started, 0))
	r.resolveEvent(ctx, u.id, u.at, u.msg, downtime)
	r.e.log.Info("açık kalan olay kapatıldı", "monitor", r.m.Name, "olay", u.id, "kesinti", downtime.Round(time.Second))
}

// resolveEvent olayın çözülme kaydı.
func (r *runner) resolveEvent(ctx context.Context, id int64, now time.Time, msg string, downtime time.Duration) {
	r.addEvents(ctx, id, store.IncidentEvent{Time: now.Unix(), Kind: store.EventUp, Message: msg,
		Data: store.EventData(map[string]int64{"downtime": int64(downtime / time.Second)})})
}
