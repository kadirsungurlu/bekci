package engine

import (
	"context"
	"encoding/json"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/mesh"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// meshEval Main'in mesh kararı (yalnız watchProbes döngüsünden kullanılır).
// Yorum değişince karar bir panel aralığı ertelenir (tüm düğümlerin taze
// raporu toplanır); değişim sürerse olay açılır/güncellenir/kapanır ve tek
// bildirim gider (tüm bağlantılar + yorum).
type meshEval struct {
	started      time.Time
	loaded       bool
	committed    string  // bildirilen yorumun anahtarı ("" = sağlıklı)
	probes       []int64 // açık olay boyunca yorumda geçen kontrol noktaları
	pending      string
	pendingSet   bool
	pendingSince time.Time
}

// meshNodes mesh kararına katılan kontrol noktaları: etkin ve kalp atışı
// gönderen (eski ajan mesh ölçmez, çevrimdışı bildirimi eskisi gibi tekil gider).
// Mesh kapalıysa ya da düğüm sayısı 2'den azsa (Main + 1 probe yorum için
// yetersiz) nil.
func (e *Engine) meshNodes(probes []store.Probe) map[int64]store.Probe {
	if !e.Settings().MeshOn() {
		return nil
	}
	out := map[int64]store.Probe{}
	for _, p := range probes {
		if _, ok := e.lastHeartbeat(p.ID); ok && p.Active {
			out[p.ID] = p
		}
	}
	if len(out) < 2 {
		return nil
	}
	return out
}

// meshReport düğümler, çevrimiçi durumu ve probe_links'ten kararı üretir.
// Probe'un kendi raporladığı kenarlar, probe çevrimdışıysa ya da rapor
// iki panel aralığından eskiyse "ölçülemiyor" sayılır (durmuş ajanın son "up"ı kalmaz).
func (e *Engine) meshReport(nodes map[int64]store.Probe, online map[int64]bool, links []store.ProbeLink, now time.Time) mesh.Report {
	set := e.Settings()
	ids := []int64{mesh.MainID}
	names := mesh.Names{mesh.MainID: e.LocalLocationName()}
	var edges []mesh.Edge
	for id, p := range nodes {
		ids = append(ids, id)
		names[id] = p.Name
		main, back := mesh.StateDown, mesh.StateUnknown
		if online[id] {
			main, back = mesh.StateUp, mesh.StateUp
		}
		edges = append(edges, mesh.Edge{Src: mesh.MainID, Dst: id, State: main}, mesh.Edge{Src: id, Dst: mesh.MainID, State: back})
	}
	for _, l := range links {
		p, ok := nodes[l.Src]
		if !ok || l.Dst == mesh.MainID {
			continue
		}
		if _, ok := nodes[l.Dst]; !ok {
			continue
		}
		t := store.EffectiveProbeTiming(p, set)
		// Ajan her panel aralığında raporlar; iki aralık sessizlik = bayat.
		maxAge := 2 * time.Duration(t.PanelSec) * time.Second
		st := l.State
		if !online[l.Src] || now.Sub(time.Unix(l.UpdatedAt, 0)) > maxAge {
			st = mesh.StateUnknown
		}
		edges = append(edges, mesh.Edge{Src: l.Src, Dst: l.Dst, State: st})
	}
	return mesh.Report{Verdict: mesh.Evaluate(ids, edges), Names: names, Owner: mesh.MainID}
}

// evalMesh watchProbes taramasının sonunda çağrılır.
func (e *Engine) evalMesh(ctx context.Context, probes []store.Probe, online map[int64]bool, now time.Time) {
	m := &e.meshEv
	if m.started.IsZero() {
		m.started = now
	}
	set := e.Settings()
	t := set.MeshTiming()
	delay := time.Duration(t.PanelSec) * time.Second
	// Açılışta kalp atışları yeniden gelene kadar herkes çevrimdışı görünür.
	if now.Sub(m.started) < t.OfflineAfter()+delay {
		return
	}
	if !m.loaded {
		m.loaded = true
		if id, data, err := e.store.OpenMeshIncidentData(ctx); err == nil && id > 0 {
			var r mesh.Report
			m.committed = "?"
			if json.Unmarshal([]byte(data), &r) == nil && !r.Verdict.Healthy() {
				m.committed, m.probes = r.Verdict.Key(), r.Probes()
			}
		}
	}
	nodes := e.meshNodes(probes)
	var r mesh.Report
	if nodes != nil {
		links, err := e.store.ListProbeLinks(ctx)
		if err != nil {
			e.log.Error("mesh bağlantıları okunamadı", "hata", err)
			return
		}
		r = e.meshReport(nodes, online, links, now)
	}
	key := r.Verdict.Key()
	if key == m.committed {
		m.pendingSet = false
		return
	}
	if nodes == nil {
		// Mesh kapandı ya da yetersiz düğüm: açık olay bildirimsiz kapanır.
		if _, _, err := e.store.ResolveMeshIncident(ctx, now.Unix()); err != nil {
			e.log.Error("mesh olayı kapatılamadı", "hata", err)
			return
		}
		m.committed, m.probes, m.pendingSet = "", nil, false
		return
	}
	if !m.pendingSet || key != m.pending {
		m.pending, m.pendingSet, m.pendingSince = key, true, now
		return
	}
	if now.Sub(m.pendingSince) < delay {
		return
	}
	m.pendingSet = false
	e.commitMesh(ctx, r, key, probes, now)
}

// commitMesh yorumu olaya yazar ve bildirir.
func (e *Engine) commitMesh(ctx context.Context, r mesh.Report, key string, probes []store.Probe, now time.Time) {
	m := &e.meshEv
	ev := notify.Event{Kind: notify.KindMesh, MonitorType: "probe", Time: now, Mesh: &r}
	if r.Verdict.Healthy() {
		id, started, err := e.store.ResolveMeshIncident(ctx, now.Unix())
		if err != nil {
			e.log.Error("mesh olayı kapatılamadı", "hata", err)
			return
		}
		ev.Kind, ev.IncidentID, ev.Downtime = notify.KindMeshResolved, id, now.Sub(time.Unix(started, 0))
		ev.MeshProbes = notifiable(probes, m.probes)
		m.committed, m.probes = "", nil
		e.log.Info("kontrol noktası bağlantıları düzeldi")
		if id != 0 {
			e.sendMesh(ev)
		}
		return
	}
	data, _ := json.Marshal(r)
	id, opened, err := e.store.OpenMeshIncident(ctx, now.Unix(), store.MeshIncidentCause(i18n.TR, string(data)), string(data))
	if err != nil {
		e.log.Error("mesh olayı yazılamadı", "hata", err)
		return
	}
	m.committed, m.probes = key, unionIDs(m.probes, r.Probes())
	ev.IncidentID, ev.MeshProbes = id, notifiable(probes, r.Probes())
	e.log.Warn("kontrol noktası bağlantı sorunu", "yorum", key, "yeni", opened)
	e.sendMesh(ev)
}

func (e *Engine) sendMesh(ev notify.Event) {
	if ev.Mesh != nil {
		ev.Image = e.meshImage(*ev.Mesh)
	}
	if e.cfg.BaseURL != "" {
		ev.URL = e.cfg.BaseURL + "/#/settings/probes"
		if ev.IncidentID != 0 {
			ev.IncidentURL = e.IncidentURL(ev.IncidentID)
		}
	}
	e.hub.Publish("incident", map[string]any{"incident_id": ev.IncidentID, "kind": store.IncidentMesh})
	e.notifier.Notify(ev)
}

// meshImageTimeout şema çiziminin üst sınırı; aşılırsa bildirim yalnız metin.
var meshImageTimeout = 200 * time.Millisecond

// meshImage bildirime eklenen topoloji PNG'si (olay başına bir kez). Dil
// ayarlardaki bildirim dili; kanala özel dil seçilmişse metin o dilde, şema
// bu dilde kalır. Hata ya da süre aşımında nil.
func (e *Engine) meshImage(r mesh.Report) []byte {
	lang := i18n.Default
	if s := e.settings.Load(); s != nil {
		lang = i18n.Or(s.NotifyLang)
	}
	ch := make(chan []byte, 1)
	go func() {
		b, err := mesh.RenderPNG(mesh.BuildTopology(r.Verdict), r.Names, lang)
		if err != nil {
			e.log.Warn("mesh şeması çizilemedi", "hata", err)
			b = nil
		}
		ch <- b
	}()
	select {
	case b := <-ch:
		return b
	case <-time.After(meshImageTimeout):
		e.log.Warn("mesh şeması süre aşımı; bildirim yalnız metin")
		return nil
	}
}

// notifiable "çevrimdışında bildir" açık olan kontrol noktaları.
func notifiable(probes []store.Probe, ids []int64) []int64 {
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int64
	for _, p := range probes {
		if want[p.ID] && p.NotifyOffline {
			out = append(out, p.ID)
		}
	}
	return out
}

func unionIDs(a, b []int64) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, s := range [][]int64{a, b} {
		for _, id := range s {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// MeshSnapshot arayüz için güncel bağlantı kararı. Mesh kapalıysa ya da
// kalp atışı gönderen kontrol noktası 2'den azsa ok=false.
func (e *Engine) MeshSnapshot(ctx context.Context, now time.Time) (mesh.Report, bool, error) {
	probes, err := e.store.ListProbesOfKind(ctx, store.ProbeKindLocation)
	if err != nil {
		return mesh.Report{}, false, err
	}
	nodes := e.meshNodes(probes)
	if nodes == nil {
		return mesh.Report{}, false, nil
	}
	links, err := e.store.ListProbeLinks(ctx)
	if err != nil {
		return mesh.Report{}, false, err
	}
	online := make(map[int64]bool, len(nodes))
	for id, p := range nodes {
		online[id] = e.ProbeOnline(p, now)
	}
	return e.meshReport(nodes, online, links, now), true, nil
}
