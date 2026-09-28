package api

import (
	"encoding/json"
	"net/http"
	"slices"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// validateGroup grup monitörünün alt monitörlerini doğrular: hepsi var olmalı,
// grup kendini içeremez ve gruplar arasında döngü oluşamaz (grup içinde grup
// serbest). id yeni monitörde 0'dır. Hata varsa yanıtı yazar ve false döner.
func (s *Server) validateGroup(w http.ResponseWriter, r *http.Request, id int64, m store.Monitor) bool {
	if m.Type != check.TypeGroup {
		return true
	}
	cfg := check.GroupConfigOf(m.Config)
	if id > 0 && slices.Contains(cfg.MonitorIDs, id) {
		writeError(w, http.StatusBadRequest, "Grup kendisini alt monitör olarak içeremez")
		return false
	}
	existing, err := s.store.MonitorsByIDs(r.Context(), cfg.MonitorIDs)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	for _, cid := range cfg.MonitorIDs {
		if _, ok := existing[cid]; !ok {
			writeError(w, http.StatusBadRequest, "Seçilen alt monitörlerden biri bulunamadı")
			return false
		}
	}
	if id == 0 {
		return true // yeni monitörü henüz hiçbir grup içeremez: döngü olamaz
	}
	groups, err := s.store.MonitorsOfType(r.Context(), check.TypeGroup)
	if err != nil {
		s.dbError(w, err)
		return false
	}
	edges := map[int64][]int64{}
	for _, g := range groups {
		edges[g.ID] = check.GroupConfigOf(g.Config).MonitorIDs
	}
	edges[id] = cfg.MonitorIDs
	if reaches(edges, cfg.MonitorIDs, id) {
		writeError(w, http.StatusBadRequest, "Bu seçim döngü oluşturuyor: alt gruplardan biri bu grubu içeriyor")
		return false
	}
	return true
}

// reaches from düğümlerinden target'a grup kenarlarıyla ulaşılabiliyor mu?
func reaches(edges map[int64][]int64, from []int64, target int64) bool {
	seen := map[int64]bool{}
	stack := slices.Clone(from)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == target {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		stack = append(stack, edges[n]...)
	}
	return false
}

// afterMonitorDelete silinen monitörü grupların ayarlarından çıkarır ve bakım
// dizinini yeniler (bakım bağlantıları veritabanında kendiliğinden silinir;
// bellekteki dizinde eski kimlik kalıp yeni bir monitöre denk gelmesin).
func (s *Server) afterMonitorDelete(r *http.Request, id int64) {
	s.afterMonitorsDelete(r, []int64{id})
}

// afterMonitorsDelete afterMonitorDelete'in toplu hali (bakım dizini ve gruplar
// bir kez güncellenir).
func (s *Server) afterMonitorsDelete(r *http.Request, ids []int64) {
	if len(ids) == 0 {
		return
	}
	ctx := r.Context()
	if err := s.engine.ReloadMaintenance(ctx); err != nil {
		s.log.Error("bakım pencereleri yenilenemedi", "hata", err)
	}
	groups, err := s.store.MonitorsOfType(ctx, check.TypeGroup)
	if err != nil {
		s.log.Error("gruplar okunamadı", "hata", err)
		return
	}
	for _, g := range groups {
		if slices.Contains(ids, g.ID) {
			continue // kendisi de silindi
		}
		cfg := check.GroupConfigOf(g.Config)
		n := len(cfg.MonitorIDs)
		cfg.MonitorIDs = slices.DeleteFunc(cfg.MonitorIDs, func(id int64) bool { return slices.Contains(ids, id) })
		if len(cfg.MonitorIDs) == n {
			continue
		}
		b, _ := json.Marshal(cfg)
		if err := s.store.SetMonitorConfig(ctx, g.ID, b); err != nil {
			s.log.Error("grup ayarı güncellenemedi", "grup", g.Name, "hata", err)
			continue
		}
		if g.Active {
			if err := s.engine.Reload(ctx, g.ID); err != nil {
				s.log.Error("grup yeniden başlatılamadı", "grup", g.Name, "hata", err)
			}
		}
	}
}
