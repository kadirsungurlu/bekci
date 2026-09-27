package engine

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
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
	r.incidentID, r.lastCause, r.maintLogged = id, res.Message, false

	var locs []incidentLocation
	where := LocalName
	detail, detailAt, detailLoc := res.Detail, now, LocalName
	if r.locs == nil {
		locs = []incidentLocation{{ProbeID: LocalProbeID, Name: LocalName, Status: locDown, Message: res.Message}}
	} else {
		st := r.locs.statuses(now, r.staleAfter(), r.m.MaxRetries)
		var failing []string
		r.locPrev = make(map[int64]string, len(st))
		for _, s := range st {
			locs = append(locs, incidentLocation{ProbeID: s.ProbeID, Name: s.Name, Status: s.Status, Message: s.Message})
			r.locPrev[s.ProbeID] = s.Status
			if s.Status == locDown {
				failing = append(failing, s.Name)
			}
		}
		where = nameList(failing)
		detail, detailLoc = nil, ""
		// Öncelik: ana sunucu, sonra sıradaki ilk çalışmayan konum.
		for _, l := range r.locs.locs {
			if l.detail != nil && classify(l, now, r.staleAfter(), r.m.MaxRetries) == locDown {
				detail, detailAt, detailLoc = l.detail, l.at, l.name
				break
			}
		}
	}
	evs := append(pre, store.IncidentEvent{
		Time: now.Unix(), Kind: store.EventDown, Location: where, Message: res.Message,
		Data: store.EventData(map[string]any{"locations": locs}),
	})
	r.addEvents(ctx, id, evs...)
	if detail != nil {
		b, err := json.Marshal(detail)
		if err == nil {
			err = r.e.store.SaveIncidentCapture(ctx, id, detailAt.Unix(), detailLoc, b)
		}
		if err != nil {
			r.e.log.Error("olayın istek/yanıt kaydı yazılamadı", "monitor", r.m.Name, "hata", err)
		}
	}
}

// incidentProgress olay sürerken değişenleri (bakım, konum durumu, hata
// mesajı) işlem geçmişine yazar. status bu kontrolün kaydedilen durumudur.
func (r *runner) incidentProgress(ctx context.Context, now time.Time, status int, inMaint bool, res check.Result) {
	if r.incidentID == 0 {
		return
	}
	var evs []store.IncidentEvent
	switch {
	case inMaint && !r.maintLogged:
		r.maintLogged = true
		evs = append(evs, store.IncidentEvent{Time: now.Unix(), Kind: store.EventMaintStart,
			Message: "Bakım penceresi başladı; kontroller sürüyor, bildirim gönderilmiyor"})
	case !inMaint && r.maintLogged:
		r.maintLogged = false
		evs = append(evs, store.IncidentEvent{Time: now.Unix(), Kind: store.EventMaintEnd, Message: "Bakım penceresi bitti"})
	}
	if inMaint {
		r.addEvents(ctx, r.incidentID, evs...)
		return
	}
	if r.locs != nil {
		evs = append(evs, r.locationChanges(now)...)
	}
	if status == store.StatusDown && res.Message != r.lastCause {
		r.lastCause = res.Message
		evs = append(evs, store.IncidentEvent{Time: now.Unix(), Kind: store.EventChange, Message: res.Message})
	}
	r.addEvents(ctx, r.incidentID, evs...)
}

// locationChanges olay sürerken durumu değişen konumlar. "Tekrar deneniyor"
// ara durumdur, kaydedilmez; yeniden başlatma sonrası ilk çağrı yalnızca
// mevcut durumu öğrenir.
func (r *runner) locationChanges(now time.Time) []store.IncidentEvent {
	st := r.locs.statuses(now, r.staleAfter(), r.m.MaxRetries)
	first := r.locPrev == nil
	if first {
		r.locPrev = make(map[int64]string, len(st))
	}
	var out []store.IncidentEvent
	for _, s := range st {
		if s.Status == locRetrying || r.locPrev[s.ProbeID] == s.Status {
			continue
		}
		r.locPrev[s.ProbeID] = s.Status
		if first {
			continue
		}
		msg := "Sonuç gelmiyor"
		switch s.Status {
		case locUp:
			msg = "Çalışıyor"
		case locDown:
			msg = "Çalışmıyor"
			if s.Message != "" {
				msg += ": " + s.Message
			}
		}
		out = append(out, store.IncidentEvent{Time: now.Unix(), Kind: store.EventLocation, Location: s.Name,
			Message: msg, Data: store.EventData(map[string]string{"status": s.Status})})
	}
	return out
}

// resolveEvent olayın çözülme kaydı.
func (r *runner) resolveEvent(ctx context.Context, id int64, now time.Time, msg string, downtime time.Duration) {
	r.addEvents(ctx, id, store.IncidentEvent{Time: now.Unix(), Kind: store.EventUp, Message: msg,
		Data: store.EventData(map[string]int64{"downtime": int64(downtime / time.Second)})})
}
