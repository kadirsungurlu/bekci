package api

// Herkese açık durum sayfasının olay listesi (monitör kesintileri + elle
// açılan olaylar, güncellemeleriyle) ve planlı bakım bloğu.

import (
	"context"
	"sort"
	"time"

	"github.com/kadirsungurlu/bekci/internal/maintenance"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// publicUpdate olayın herkese açık bir güncellemesi (aşama + metin).
type publicUpdate struct {
	Time  int64  `json:"time"`
	State string `json:"state"` // investigating | identified | monitoring | resolved
	Body  string `json:"body"`
}

// publicIncident herkese açık sayfadaki bir olay. Monitör kesintisinde
// Monitor sayfadaki görünen ad, Kind "monitor"; elle açılan olayda Kind
// "manual", Title/Severity dolu ve Monitors etkilenen monitörlerin adları.
// Updates (varsa) yeniden eskiye. Neden hiçbir zaman gönderilmez.
type publicIncident struct {
	ID         int64          `json:"id,omitempty"`
	Kind       string         `json:"kind,omitempty"`
	Monitor    string         `json:"monitor"`
	Monitors   []string       `json:"monitors,omitempty"`
	Title      string         `json:"title,omitempty"`
	Severity   string         `json:"severity,omitempty"`
	State      string         `json:"state,omitempty"`
	StartedAt  int64          `json:"started_at"`
	ResolvedAt int64          `json:"resolved_at"`
	Updates    []publicUpdate `json:"updates,omitempty"`
}

// publicMaintenance sayfadaki monitörleri etkileyen süren / yaklaşan bakım.
type publicMaintenance struct {
	ID          int64    `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	StartsAt    int64    `json:"starts_at"`
	EndsAt      int64    `json:"ends_at"` // 0: süresi belirsiz (elle pencere)
	Ongoing     bool     `json:"ongoing"`
	Monitors    []string `json:"monitors"` // etkilenen monitörlerin sayfadaki adları (hepsi: boş liste + AllMonitors)
	AllMonitors bool     `json:"all_monitors"`
}

// maintenanceLookahead yaklaşan bakımların gösterileceği süre.
const maintenanceLookahead = 7 * 24 * time.Hour

// publicIncidents sayfanın olay penceresindeki monitör kesintileri ve elle
// açılan olaylar, güncellemeleriyle, yeniden eskiye. names sayfadaki görünen adlar.
func (s *Server) publicIncidents(ctx context.Context, p store.StatusPage, names map[int64]string, now int64) ([]publicIncident, error) {
	ids := p.MonitorIDs()
	window := incidentWindow(p)
	incs, err := s.store.IncidentsFor(ctx, ids, now-window, publicIncidentLimit)
	if err != nil {
		return nil, err
	}
	manual, err := s.store.ManualIncidentsForPage(ctx, p.ID, now-window, publicIncidentLimit)
	if err != nil {
		return nil, err
	}
	var all []int64
	for _, in := range incs {
		all = append(all, in.ID)
	}
	for _, in := range manual {
		all = append(all, in.ID)
	}
	updates, err := s.store.UpdatesForIncidents(ctx, all)
	if err != nil {
		return nil, err
	}
	affected, err := s.store.IncidentMonitorsFor(ctx, all)
	if err != nil {
		return nil, err
	}
	toUpdates := func(id int64) []publicUpdate {
		var out []publicUpdate
		for _, u := range updates[id] {
			out = append(out, publicUpdate{Time: u.Time, State: u.State, Body: u.Body})
		}
		return out
	}
	out := make([]publicIncident, 0, len(incs)+len(manual))
	for _, in := range incs {
		name, ok := names[in.MonitorID]
		if !ok {
			continue
		}
		out = append(out, publicIncident{ID: in.ID, Kind: store.IncidentMonitor, Monitor: name, StartedAt: in.StartedAt, ResolvedAt: in.ResolvedAt,
			State: in.State, Updates: toUpdates(in.ID)})
	}
	for _, in := range manual {
		pi := publicIncident{ID: in.ID, Kind: store.IncidentManual, Title: in.Title, Severity: in.Severity, State: in.State,
			StartedAt: in.StartedAt, ResolvedAt: in.ResolvedAt, Updates: toUpdates(in.ID), Monitors: []string{}}
		for _, mid := range affected[in.ID] {
			if name, ok := names[mid]; ok {
				pi.Monitors = append(pi.Monitors, name)
			}
		}
		pi.Monitor = in.Title
		out = append(out, pi)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartedAt > out[j].StartedAt })
	if len(out) > publicIncidentLimit {
		out = out[:publicIncidentLimit]
	}
	return out, nil
}

// publicMaintenance sayfadaki monitörleri etkileyen (ya da tüm monitörleri
// kapsayan) etkin bakım pencereleri: şu an süren ve 7 gün içinde başlayacak
// olanlar, başlangıca göre sıralı. Elle (süresiz) pencere yalnızca açıkken.
func (s *Server) publicMaintenance(ctx context.Context, p store.StatusPage, names map[int64]string, now time.Time) ([]publicMaintenance, error) {
	windows, err := s.store.ListMaintenance(ctx)
	if err != nil {
		return nil, err
	}
	out := []publicMaintenance{}
	horizon := now.Add(maintenanceLookahead)
	for _, m := range windows {
		if !m.Active {
			continue
		}
		var affected []string
		if !m.AllMonitors {
			for _, mid := range m.MonitorIDs {
				if name, ok := names[mid]; ok {
					affected = append(affected, name)
				}
			}
			if len(affected) == 0 {
				continue // bu sayfanın monitörlerini etkilemiyor
			}
		}
		status, start, end := maintenance.Status(m, now)
		switch status {
		case maintenance.StatusActive:
		case maintenance.StatusScheduled:
			if start == 0 || time.Unix(start, 0).After(horizon) {
				continue
			}
		default:
			continue
		}
		pm := publicMaintenance{ID: m.ID, Title: m.Title, Description: m.Description, StartsAt: start, EndsAt: end,
			Ongoing: status == maintenance.StatusActive, Monitors: []string{}, AllMonitors: m.AllMonitors}
		if affected != nil {
			pm.Monitors = affected
		}
		if pm.Ongoing && pm.StartsAt == 0 {
			pm.StartsAt = m.UpdatedAt // elle pencere: açıldığı an
		}
		out = append(out, pm)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Ongoing != out[j].Ongoing {
			return out[i].Ongoing
		}
		return out[i].StartsAt < out[j].StartsAt
	})
	return out, nil
}
