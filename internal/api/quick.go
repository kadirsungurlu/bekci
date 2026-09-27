package api

// Monitör listesinin hızlı işlemleri: kopyalama, istatistik sıfırlama, bildirim
// kanalı seçimi, toplu işlemler ve durum sayfasına monitör ekleme. Hepsi editör
// yetkisi ister ve işlem kaydına yazılır.

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// maxBulkMonitors tek toplu işlemde en fazla monitör sayısı.
const maxBulkMonitors = 1000

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("POST /api/monitors/{id}/clone", s.editor(s.cloneMonitor))
		mux.Handle("POST /api/monitors/{id}/reset-stats", s.editor(s.resetMonitorStats))
		mux.Handle("PUT /api/monitors/{id}/notifications", s.editor(s.setMonitorNotifications))
		mux.Handle("POST /api/monitors/bulk", s.editor(s.bulkMonitors))
		mux.Handle("POST /api/status-pages/{id}/monitors", s.editor(s.addPageMonitor))
	})
}

// visibleMonitor yoldaki monitörü okur; yoksa veya kullanıcı göremiyorsa 404
// yazar ve false döner.
func (s *Server) visibleMonitor(w http.ResponseWriter, r *http.Request) (store.Monitor, bool) {
	id, ok := pathID(w, r)
	if !ok {
		return store.Monitor{}, false
	}
	m, err := s.store.GetMonitor(r.Context(), id)
	if err == nil && !visibleTo(userFrom(r)).can(id) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return m, false
	}
	return m, true
}

// cloneName "<ad> (kopya)"; ad sınırı (100 karakter) aşılmasın diye kırpılır.
func cloneName(name string) string {
	const suffix = " (kopya)"
	max := 100 - utf8.RuneCountInString(suffix)
	if r := []rune(name); len(r) > max {
		name = strings.TrimSpace(string(r[:max]))
	}
	return name + suffix
}

// cloneMonitor: POST /api/monitors/{id}/clone. Ayarlar (gizli alanlar dahil)
// sunucu tarafında kopyalanır; arayüzdeki maskeli değerler hiç kullanılmaz.
// Kopya durdurulmuş olarak oluşur (aynı hedef için ikinci bildirim akışı
// kullanıcı düzenleyip başlatana kadar başlamasın). Push monitörüne yeni adres verilir.
func (s *Server) cloneMonitor(w http.ResponseWriter, r *http.Request) {
	src, ok := s.visibleMonitor(w, r)
	if !ok {
		return
	}
	token := ""
	if src.Type == check.TypePush {
		token = randomToken(24)
	}
	name := cloneName(src.Name)
	id, err := s.store.CloneMonitor(r.Context(), src.ID, name, false, token)
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "monitor.clone", "monitor", id, name, "kaynak: "+src.Name)
	s.respondMonitor(w, r, id, http.StatusCreated)
}

// resetMonitorStats: POST /api/monitors/{id}/reset-stats. Kontrol geçmişi,
// özetler ve bitmiş olaylar silinir; kontroller kesintisiz sürer.
func (s *Server) resetMonitorStats(w http.ResponseWriter, r *http.Request) {
	m, ok := s.visibleMonitor(w, r)
	if !ok {
		return
	}
	if err := s.store.ResetMonitorStats(r.Context(), m.ID); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "monitor.reset_stats", "monitor", m.ID, m.Name, "")
	// Açık sekmeler çubukları ve yüzdeleri yeniden yüklesin.
	s.hub.Publish("stats_reset", map[string]any{"monitor_id": m.ID})
	s.respondMonitor(w, r, m.ID, http.StatusOK)
}

// setMonitorNotifications: PUT /api/monitors/{id}/notifications, gövde
// {"notification_ids": [..]}; listeyi tamamen değiştirir.
func (s *Server) setMonitorNotifications(w http.ResponseWriter, r *http.Request) {
	m, ok := s.visibleMonitor(w, r)
	if !ok {
		return
	}
	var in struct {
		NotificationIDs []int64 `json:"notification_ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ids := slices.Compact(slices.Sorted(slices.Values(in.NotificationIDs)))
	if _, err := s.notificationIDs(r, &ids); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.store.SetMonitorNotifications(r.Context(), m.ID, ids); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "monitor.notifications", "monitor", m.ID, m.Name, strconv.Itoa(len(ids))+" kanal")
	s.respondMonitor(w, r, m.ID, http.StatusOK)
}

// Toplu işlemler ------------------------------------------------------------------

type bulkInput struct {
	IDs    []int64 `json:"ids"`
	Action string  `json:"action"` // pause | resume | delete | add_tag | remove_tag | add_notification | remove_notification
	// add_tag / remove_tag
	TagID int64  `json:"tag_id"`
	Value string `json:"value"`
	// add_notification / remove_notification
	NotificationID int64 `json:"notification_id"`
}

type bulkResult struct {
	Changed  int           `json:"changed"`  // gerçekten değişen monitör sayısı
	Monitors []monitorView `json:"monitors"` // işlemden sonraki hali (silmede boş)
	Deleted  []int64       `json:"deleted"`
}

// bulkMonitors: POST /api/monitors/bulk. Tüm monitörler önce doğrulanır; biri
// yoksa hiçbir değişiklik yapılmaz. Her monitör için ayrı işlem kaydı yazılır
// (monitörün geçmişinde görünsün diye).
func (s *Server) bulkMonitors(w http.ResponseWriter, r *http.Request) {
	var in bulkInput
	if !readJSON(w, r, &in) {
		return
	}
	ids := slices.Compact(slices.Sorted(slices.Values(in.IDs)))
	if len(ids) == 0 {
		writeError(w, http.StatusBadRequest, "En az bir monitör seçin")
		return
	}
	if len(ids) > maxBulkMonitors {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Tek seferde en fazla %d monitör seçilebilir", maxBulkMonitors))
		return
	}
	ctx := r.Context()
	found, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		s.dbError(w, err)
		return
	}
	vis := visibleTo(userFrom(r))
	mons := make([]store.Monitor, 0, len(ids))
	for _, id := range ids {
		m, ok := found[id]
		if !ok || !vis.can(id) {
			writeError(w, http.StatusBadRequest, "Seçilen monitörlerden biri bulunamadı")
			return
		}
		mons = append(mons, m)
	}

	res := bulkResult{Monitors: []monitorView{}, Deleted: []int64{}}
	const detail = "toplu işlem"
	switch in.Action {
	case "pause":
		for _, m := range mons {
			if !m.Active {
				continue
			}
			s.engine.Remove(m.ID)
			if err := s.store.SetMonitorActive(ctx, m.ID, false); err != nil {
				s.dbError(w, err)
				return
			}
			s.incidentNote(r, m.ID, store.EventPaused, true)
			if _, err := s.store.ResolveIncident(ctx, m.ID, s.now().Unix()); err != nil {
				s.dbError(w, err)
				return
			}
			res.Changed++
			s.audit(r, store.User{}, "monitor.pause", "monitor", m.ID, m.Name, detail)
		}
	case "resume":
		for _, m := range mons {
			if m.Active {
				continue
			}
			if err := s.store.SetMonitorActive(ctx, m.ID, true); err != nil {
				s.dbError(w, err)
				return
			}
			if err := s.engine.Reload(ctx, m.ID); err != nil {
				s.log.Error("monitör başlatılamadı", "id", m.ID, "hata", err)
			}
			res.Changed++
			s.audit(r, store.User{}, "monitor.resume", "monitor", m.ID, m.Name, detail)
		}
	case "delete":
		for _, m := range mons {
			s.engine.Remove(m.ID)
			if err := s.store.DeleteMonitor(ctx, m.ID); err != nil && !errors.Is(err, store.ErrNotFound) {
				s.dbError(w, err)
				return
			}
			res.Changed++
			res.Deleted = append(res.Deleted, m.ID)
			s.audit(r, store.User{}, "monitor.delete", "monitor", m.ID, m.Name, detail)
		}
		s.afterMonitorsDelete(r, res.Deleted)
		writeJSON(w, http.StatusOK, res)
		return
	case "add_tag", "remove_tag":
		if !s.bulkTag(w, r, in, mons, &res) {
			return
		}
	case "add_notification", "remove_notification":
		if !s.bulkNotification(w, r, in, mons, &res) {
			return
		}
	default:
		writeError(w, http.StatusBadRequest, "Geçersiz işlem")
		return
	}

	after, err := s.store.MonitorsByIDs(ctx, ids)
	if err != nil {
		s.dbError(w, err)
		return
	}
	list := make([]store.Monitor, 0, len(ids))
	for _, id := range ids {
		if m, ok := after[id]; ok {
			list = append(list, m)
		}
	}
	if res.Monitors, err = s.buildViews(r, list); err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// bulkTag etiketi seçili monitörlere ekler veya kaldırır. Hata varsa yanıtı
// yazar ve false döner.
func (s *Server) bulkTag(w http.ResponseWriter, r *http.Request, in bulkInput, mons []store.Monitor, res *bulkResult) bool {
	ctx := r.Context()
	tag, err := s.store.GetTag(ctx, in.TagID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, tagNotFoundMessage)
		return false
	}
	if err != nil {
		s.dbError(w, err)
		return false
	}
	value, err := normalizeTagValue(in.Value)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	current, err := s.store.MonitorTags(ctx)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	has := func(mid int64, anyValue bool) bool {
		return slices.ContainsFunc(current[mid], func(t store.MonitorTag) bool {
			return t.ID == tag.ID && (anyValue || t.Value == value)
		})
	}
	var changed []store.Monitor
	for _, m := range mons {
		if in.Action == "add_tag" && !has(m.ID, false) {
			if len(current[m.ID]) >= maxTagsPerMonitor {
				writeError(w, http.StatusBadRequest, fmt.Sprintf("“%s” monitöründe zaten %d etiket var", m.Name, maxTagsPerMonitor))
				return false
			}
			changed = append(changed, m)
		} else if in.Action == "remove_tag" && has(m.ID, true) {
			changed = append(changed, m)
		}
	}
	ids := make([]int64, len(changed))
	for i, m := range changed {
		ids[i] = m.ID
	}
	text := tag.Name
	if value != "" && in.Action == "add_tag" {
		text += ": " + value
	}
	detail := "etiket eklendi: " + text
	if in.Action == "add_tag" {
		err = s.store.AddMonitorsTag(ctx, ids, tag.ID, value)
	} else {
		err = s.store.RemoveMonitorsTag(ctx, ids, tag.ID)
		detail = "etiket kaldırıldı: " + text
	}
	if err != nil {
		s.dbError(w, err)
		return false
	}
	for _, m := range changed {
		s.audit(r, store.User{}, "monitor.tags", "monitor", m.ID, m.Name, detail)
	}
	res.Changed = len(changed)
	return true
}

// bulkNotification bildirim kanalını seçili monitörlere ekler veya çıkarır.
func (s *Server) bulkNotification(w http.ResponseWriter, r *http.Request, in bulkInput, mons []store.Monitor, res *bulkResult) bool {
	ctx := r.Context()
	ch, err := s.store.GetNotification(ctx, in.NotificationID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "Seçilen bildirim kanalı bulunamadı")
		return false
	}
	if err != nil {
		s.dbError(w, err)
		return false
	}
	links, err := s.store.MonitorNotificationIDs(ctx)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	add := in.Action == "add_notification"
	var changed []store.Monitor
	var ids []int64
	for _, m := range mons {
		if slices.Contains(links[m.ID], ch.ID) != add {
			changed = append(changed, m)
			ids = append(ids, m.ID)
		}
	}
	detail := "kanal eklendi: " + ch.Name
	if add {
		err = s.store.AddMonitorsNotification(ctx, ids, ch.ID)
	} else {
		err = s.store.RemoveMonitorsNotification(ctx, ids, ch.ID)
		detail = "kanal çıkarıldı: " + ch.Name
	}
	if err != nil {
		s.dbError(w, err)
		return false
	}
	for _, m := range changed {
		s.audit(r, store.User{}, "monitor.notifications", "monitor", m.ID, m.Name, detail)
	}
	res.Changed = len(changed)
	return true
}

// Durum sayfasına monitör ekleme -----------------------------------------------------

type pageMonitorInput struct {
	MonitorID int64 `json:"monitor_id"`
	// Section mevcut grubun sırası (0'dan başlar); -1 yeni grup açar.
	Section      int    `json:"section"`
	SectionTitle string `json:"section_title"` // yeni grubun adı (boş olabilir)
	Name         string `json:"name"`          // sayfada görünen ad; boşsa monitörün adı
}

// addPageMonitor: POST /api/status-pages/{id}/monitors. Sayfanın geri kalanına
// dokunmadan tek monitör ekler; doğrulama sayfa düzenlemesiyle aynıdır.
func (s *Server) addPageMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	old, err := s.store.GetPage(ctx, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in pageMonitorInput
	if !readJSON(w, r, &in) {
		return
	}
	mon, err := s.store.GetMonitor(ctx, in.MonitorID)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusBadRequest, "Seçilen monitör bulunamadı")
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Silinmiş monitörler gruplardan çıkarılmış hali üzerinde çalışılır.
	views, err := s.pageViews(ctx, []store.StatusPage{old})
	if err != nil {
		s.dbError(w, err)
		return
	}
	p := views[0]
	if slices.Contains(p.MonitorIDs(), mon.ID) {
		writeError(w, http.StatusConflict, "Bu monitör zaten bu sayfada")
		return
	}
	sections := slices.Clone(p.Sections)
	entry := store.PageMonitor{ID: mon.ID, Name: in.Name}
	switch {
	case in.Section == -1:
		sections = append(sections, store.PageSection{Title: in.SectionTitle, Monitors: []store.PageMonitor{entry}})
	case in.Section >= 0 && in.Section < len(sections):
		sec := sections[in.Section]
		sec.Monitors = append(slices.Clone(sec.Monitors), entry)
		sections[in.Section] = sec
	default:
		writeError(w, http.StatusBadRequest, "Seçilen grup bulunamadı")
		return
	}
	pin := pageInput{
		Slug: p.Slug, Title: p.Title, Description: p.Description, Footer: p.Footer,
		Sections: sections, CustomDomain: p.CustomDomain,
		ShowTargets: &p.ShowTargets, Published: &p.Published, BarRange: &p.BarRange, ShowIncidents: &p.ShowIncidents,
		Collapsible: &p.Collapsible,
	}
	// Özel alan adı değişmiyor (mevcut sayfadan alınıyor); editör de kaydedebilir.
	np, err := s.normalizePage(ctx, &pin, &old, requestHost(r), isPageAdmin(userFrom(r)))
	if err != nil {
		s.writeInputError(w, err)
		return
	}
	if err := s.store.UpdatePage(ctx, &np); err != nil {
		s.writeInputError(w, conflictError(err))
		return
	}
	s.pagesChanged(ctx)
	s.audit(r, store.User{}, "status_page.update", "status_page", id, np.Title, "monitör eklendi: "+mon.Name)
	s.respondPage(w, r, id, http.StatusOK)
}
