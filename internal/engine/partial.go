package engine

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kısmi kesinti (kind = partial) ----------------------------------------------------
//
// Çok konumlu monitörde en az bir konum tekrar denemelerinden sonra da
// çalışmıyorken (locDown) monitörün genel durumu kesinti kuralı (any /
// majority / all) yüzünden çalışıyorsa kısmi kesinti olayı açılır. "İlk
// sonuç bekleniyor" (waiting) ve "tekrar deneniyor" (retrying) konumlar olay
// açmaz. Kısmi olay:
//
//   - bildirim göndermez, uptime'a etki etmez (kontrol kayıtları değişmez),
//     herkese açık durum sayfalarında görünmez;
//   - konumların çalışmaz/çalışır olmasını işlem geçmişine yazar (konum
//     kaydı), olay başında çalışmayan konumların istek/yanıt ayrıntısını saklar;
//   - hiçbir konum çalışmıyor değilken (ve sonucu kesinleşmemiş konum yokken)
//     kapanır; konum kaldırılırsa (runner yeniden başlar, konum listede
//     olmaz) ve monitör durdurulursa (API) da kapanır;
//   - monitör tamamen çalışmaz olursa (DOWN) "tam kesintiye dönüştü" kaydıyla
//     kapanır ve normal olay açılır; normal olayın geçmişine "kısmi kesintiden
//     dönüştü" yazılır. İki olay birbirinin kimliğini data.incident_id'de taşır.
//     (Aynı olayın türünü yükseltmek yerine bu yol seçildi: normal olayın
//     başlangıcı, kesinti süresi ve bildirimleri tam kesintinin başladığı
//     andan hesaplanır; kısmi dönem ayrı kalır.)
//
// Bakım penceresinde yeni kısmi olay açılmaz; sürenin konum değişimleri yazılır.

// partialStep kısmi kesinti olayını açar, günceller veya kapatır; process
// kaydı yazdıktan (ve normal olayı açıp/kapattıktan) sonra çağırır.
func (r *runner) partialStep(ctx context.Context, now time.Time, status int, inMaint bool) {
	if r.locs == nil {
		return
	}
	st := r.locs.statuses(now, r.rules())
	var down []LocationStatus
	unsettled := false // sonucu henüz kesinleşmemiş konum
	for _, s := range st {
		switch s.Status {
		case locDown:
			down = append(down, s)
		case locRetrying, locWaiting:
			unsettled = true
		case locUnknown:
			// Bu olayda çalışmayan konumun sonucu gelmiyor (ör. ajan yeniden
			// başlıyor): düzeldiği bilinmeden olay kapanmaz; yoksa her kısa
			// kopuşta olay kapanıp yeniden açılır.
			if slices.Contains(r.partialFailed, s.Name) {
				unsettled = true
			}
		}
	}
	switch {
	case r.partialID == 0:
		if status == store.StatusUp && !inMaint && r.incidentID == 0 && len(down) > 0 {
			r.openPartial(ctx, now, down)
		}
	case status == store.StatusDown:
		r.escalatePartial(ctx, now)
	default:
		// Yeniden başlatmadan sonra konumların ilk sonuçları gelene kadar (son
		// bekleyen konumun sonucunun geldiği çağrı dahil) yalnızca mevcut durum
		// öğrenilir: olay başındaki durum ikinci kez yazılmasın.
		learn := r.partialLearn
		if !unsettled {
			r.partialLearn = false
		}
		evs := r.locationChanges(now, &r.partialPrev, learn)
		r.addEvents(ctx, r.partialID, evs...)
		r.notePartialLocations(ctx, down)
		if len(down) == 0 && !unsettled {
			r.closePartial(ctx, now, partialResolvedMessage(st))
		}
	}
}

// partialResolvedMessage kısmi olayın çözülme mesajı: sonuç gelmeyen konum
// varsa adları eklenir.
func partialResolvedMessage(st []LocationStatus) string {
	var stale []string
	for _, s := range st {
		if s.Status == locUnknown {
			stale = append(stale, s.Name)
		}
	}
	msg := "Tüm konumlar çalışıyor"
	if len(stale) > 0 {
		msg += " (ulaşılamayan: " + nameList(stale) + ")"
	}
	return msg
}

// openPartial kısmi kesinti olayını açar.
func (r *runner) openPartial(ctx context.Context, now time.Time, down []LocationStatus) {
	names := make([]string, len(down))
	parts := make([]string, len(down))
	for i, s := range down {
		names[i] = s.Name
		parts[i] = s.Name + ": " + s.Message
	}
	cause := strings.Join(parts, "; ")
	id, err := r.e.store.StartPartialIncident(ctx, r.m.ID, now.Unix(), cause,
		store.EventData(store.PartialIncidentData{Locations: names}))
	if err != nil || id == 0 {
		if err != nil {
			r.e.log.Error("konum kesintisi olayı açılamadı", "monitor", r.m.Name, "hata", err)
		}
		return
	}
	r.partialID, r.partialFailed, r.partialLearn = id, names, false
	locs, _ := r.locationSnapshot(now, &r.partialPrev)
	r.addEvents(ctx, id, store.IncidentEvent{
		Time: now.Unix(), Kind: store.EventDown, Location: nameList(names), Message: cause,
		Data: store.EventData(downData{Locations: locs, Monitor: r.snapshot()}),
	})
	r.saveCapture(ctx, id, r.failingCapture(now))
	r.e.log.Warn("konum kesintisi", "monitor", r.m.Name, "konumlar", nameList(names))
}

// notePartialLocations olay sürerken ilk kez çalışmayan konumları olayın verisine ekler.
func (r *runner) notePartialLocations(ctx context.Context, down []LocationStatus) {
	changed := false
	for _, s := range down {
		if !slices.Contains(r.partialFailed, s.Name) {
			r.partialFailed = append(r.partialFailed, s.Name)
			changed = true
		}
	}
	if !changed {
		return
	}
	if err := r.e.store.SetIncidentData(ctx, r.partialID,
		store.EventData(store.PartialIncidentData{Locations: r.partialFailed})); err != nil {
		r.e.log.Error("konum kesintisi olayı güncellenemedi", "monitor", r.m.Name, "hata", err)
	}
}

// closePartial kısmi olayı kapatır ve çözülme kaydını yazar. Kapatılamazsa
// sonraki sonuçta yeniden denenir.
func (r *runner) closePartial(ctx context.Context, now time.Time, msg string) {
	id, started, err := r.e.store.ResolvePartialIncident(ctx, r.m.ID, now.Unix())
	if err != nil {
		r.e.log.Error("konum kesintisi olayı kapatılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	if id != 0 {
		r.resolveEvent(ctx, id, now, msg, now.Sub(time.Unix(started, 0)))
		r.e.log.Info("konum kesintisi bitti", "monitor", r.m.Name)
	}
	r.partialID, r.partialPrev, r.partialFailed, r.partialLearn = 0, nil, nil, false
}

// escalatePartial monitör tamamen çalışmaz olunca kısmi olayı "tam kesintiye
// dönüştü" kaydıyla kapatır; normal olaya (açıldıysa) bağlantı kaydı yazar.
func (r *runner) escalatePartial(ctx context.Context, now time.Time) {
	pid := r.partialID
	r.addEvents(ctx, pid, store.IncidentEvent{Time: now.Unix(), Kind: store.EventEscalated,
		Message: "Tam kesintiye dönüştü", Data: store.EventData(map[string]int64{"incident_id": r.incidentID})})
	if _, _, err := r.e.store.ResolvePartialIncident(ctx, r.m.ID, now.Unix()); err != nil {
		r.e.log.Error("konum kesintisi olayı kapatılamadı", "monitor", r.m.Name, "hata", err)
		return
	}
	if r.incidentID != 0 {
		r.addEvents(ctx, r.incidentID, store.IncidentEvent{Time: now.Unix(), Kind: store.EventFromPartial,
			Message: "Konum kesintisinden dönüştü", Data: store.EventData(map[string]int64{"incident_id": pid})})
	}
	r.partialID, r.partialPrev, r.partialFailed, r.partialLearn = 0, nil, nil, false
}

// restorePartial yeniden başlatmada açık kısmi olayı yükler. Monitör artık
// çok konumlu değilse (konum ayarı kaldırıldı) olay kapatılır.
func (r *runner) restorePartial(ctx context.Context) {
	id, err := r.e.store.OpenPartialIncidentID(ctx, r.m.ID)
	if err != nil || id == 0 {
		return
	}
	if r.locs == nil {
		now := r.e.now()
		ev := store.IncidentEvent{Time: now.Unix(), Kind: store.EventUp, Message: "Konum ayarı kaldırıldı; olay kapatıldı"}
		if inc, err := r.e.store.GetIncident(ctx, id); err == nil {
			ev.Data = store.EventData(map[string]int64{"downtime": max(0, now.Unix()-inc.StartedAt)})
		}
		if err := r.e.store.ClosePartialIncident(ctx, r.m.ID, now.Unix(), ev); err != nil {
			r.e.log.Error("konum kesintisi olayı kapatılamadı", "monitor", r.m.Name, "hata", err)
		}
		return
	}
	r.partialID, r.partialLearn = id, true
	if inc, err := r.e.store.GetIncident(ctx, id); err == nil && len(inc.Data) > 0 {
		var d store.PartialIncidentData
		if json.Unmarshal(inc.Data, &d) == nil {
			r.partialFailed = d.Locations
		}
	}
}
