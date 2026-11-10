package api

import (
	"net/http"
	"sort"
)

// Filo güvenlik özeti (iş 736, docs/GUVENLIK-EKLENTI-730.md §A.5): sunucu
// listesinin üstündeki şerit ve salt okunur anahtarla CLI. Yalnız kullanıcının
// görebildiği sunucular sayılır. Ücretsiz (§A.8).

const (
	// secAvSigMaxH av_signature_age kuralının varsayılanı (3 gün).
	secAvSigMaxH = 72
	// secIndexStaleSec paket dizini bundan eskiyse "güncellemeler eksik görünebilir".
	secIndexStaleSec = 7 * 86400
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/security/summary", s.auth(s.serversOnly(s.securitySummary)))
	})
}

// secFleetItem bir kategorideki sunucu; alanlar kategoriye göre dolar.
type secFleetItem struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Count     int    `json:"count,omitempty"`      // security_updates: bekleyen güvenlik paketi
	Oldest    int64  `json:"oldest,omitempty"`     // security_updates: en eski first_seen
	Since     int64  `json:"since,omitempty"`      // reboot: panelin ilk gördüğü an
	AvOK      *int   `json:"av_ok,omitempty"`      // av_problem
	SigAgeH   *int   `json:"sig_age_h,omitempty"`  // av_problem
	IndexTime int64  `json:"index_time,omitempty"` // stale_index
}

type secFleetGroup struct {
	Count   int            `json:"count"`
	Servers []secFleetItem `json:"servers"`
}

func newSecGroup() *secFleetGroup { return &secFleetGroup{Servers: []secFleetItem{}} }

func (g *secFleetGroup) add(it secFleetItem) {
	g.Count++
	g.Servers = append(g.Servers, it)
}

func (s *Server) securitySummary(w http.ResponseWriter, r *http.Request) {
	names, err := s.visibleServers(r)
	if err != nil {
		s.dbError(w, err)
		return
	}
	sums, err := s.store.SecSummaries(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	ids := make([]int64, 0, len(names))
	for id := range names {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if names[ids[i]] != names[ids[j]] {
			return names[ids[i]] < names[ids[j]]
		}
		return ids[i] < ids[j]
	})
	upd, reboot, fw, av, stale := newSecGroup(), newSecGroup(), newSecGroup(), newSecGroup(), newSecGroup()
	now := s.now().Unix()
	reporting := 0
	for _, id := range ids {
		v, ok := sums[id]
		if !ok {
			continue
		}
		reporting++
		base := secFleetItem{ID: id, Name: names[id]}
		if v.UpdSecurity > 0 {
			it := base
			it.Count, it.Oldest = v.UpdSecurity, v.OldestSecurity
			upd.add(it)
		}
		if v.RebootRequired {
			it := base
			it.Since = v.RebootSince
			reboot.add(it)
		}
		if v.FwActive == 0 {
			fw.add(base)
		}
		if v.AvOK == 0 || v.AvSigAgeH > secAvSigMaxH {
			it := base
			it.AvOK, it.SigAgeH = &v.AvOK, &v.AvSigAgeH
			av.add(it)
		}
		if v.IndexTime > 0 && now-v.IndexTime > secIndexStaleSec {
			it := base
			it.IndexTime = v.IndexTime
			stale.add(it)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"servers":          len(ids),
		"reporting":        reporting,
		"security_updates": upd,
		"reboot":           reboot,
		"firewall_off":     fw,
		"av_problem":       av,
		"stale_index":      stale,
	})
}
