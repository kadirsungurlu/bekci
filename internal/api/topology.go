package api

import (
	"net/http"
	"slices"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// GET /api/topology: canlı topoloji sayfasının tek istekte ihtiyaç duyduğu
// her şey. Yetki diğer uçlarla aynı: kısıtlı izleyici yalnız izinli monitörleri,
// onların kullandığı kontrol noktalarını (yalnız ad + çevrimiçi) ve atanmış
// sunucuları görür; mesh ve kontrol noktası ayrıntısı yalnız yöneticiye,
// bildirim kanalları düzenleyici ve üstüne (GET /api/notifications gibi).

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/topology", s.auth(s.topology))
	})
}

type topoStats struct {
	ProbesOnline  int      `json:"probes_online"`
	ProbesTotal   int      `json:"probes_total"`
	ResultsPerSec *float64 `json:"results_per_sec,omitempty"` // yalnız tüm monitörleri görene
	OpenIncidents int      `json:"open_incidents"`
}

// topoLocation monitörün bir konumdaki son sonucu. Status: up | down |
// retrying | waiting | unknown (sonuç gelmiyor); OOB: karara katılmıyor.
type topoLocation struct {
	ProbeID     int64  `json:"probe_id"` // 0: ana sunucu
	Name        string `json:"name"`
	Status      string `json:"status"`
	OOB         bool   `json:"oob,omitempty"`
	LastCheckAt int64  `json:"last_check_at"`
	PingMs      int64  `json:"ping_ms"`
}

// topoMonitor State: up | partial | down | pending | paused | maintenance.
type topoMonitor struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	State       string         `json:"state"`
	LastCheckAt int64          `json:"last_check_at"`
	PingMs      int64          `json:"ping_ms"`
	Locations   []topoLocation `json:"locations"`
}

type topoProbe struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	// Yalnız yönetici: panel RTT (son kalp atışı, ms; yok: ölçülmedi / eski ajan),
	// etkin hat (primary | oob), mesh eş sayısı (ulaşılan / toplam), sürüm.
	PanelRTTms *int64 `json:"panel_rtt_ms,omitempty"`
	Uplink     string `json:"uplink,omitempty"`
	Peers      *int   `json:"peers,omitempty"`
	PeersUp    *int   `json:"peers_up,omitempty"`
	Version    string `json:"version,omitempty"`
	LastSeenAt int64  `json:"last_seen_at"`
}

// topoServer sunucu ajanının son metriği; Firing eşiği aşılan ölçütler.
type topoServer struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	State      string   `json:"state"`
	CPU        *float64 `json:"cpu,omitempty"`
	Mem        *float64 `json:"mem,omitempty"`
	Disk       *float64 `json:"disk,omitempty"`
	Firing     []string `json:"firing"`
	Version    string   `json:"version"`
	MetricsAt  int64    `json:"metrics_at"`
	LastSeenAt int64    `json:"last_seen_at"`
}

// topoChannel LastSend: bu açılıştan beri son gönderim (yok: henüz gönderilmedi).
type topoChannel struct {
	ID       int64              `json:"id"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Active   bool               `json:"active"`
	LastSend *notify.SendStatus `json:"last_send,omitempty"`
}

type topoIncident struct {
	ID          int64  `json:"id"`
	Kind        string `json:"kind"`
	MonitorID   int64  `json:"monitor_id,omitempty"`
	MonitorName string `json:"monitor_name,omitempty"`
	ServerID    int64  `json:"server_id,omitempty"`
	ServerName  string `json:"server_name,omitempty"`
	StartedAt   int64  `json:"started_at"`
	Cause       string `json:"cause"`
	Acked       bool   `json:"acked"`
}

type topologyResp struct {
	Now       int64          `json:"now"`
	Admin     bool           `json:"admin"`
	Stats     topoStats      `json:"stats"`
	Monitors  []topoMonitor  `json:"monitors"`
	Probes    []topoProbe    `json:"probes"`
	Mesh      *meshViewResp  `json:"mesh,omitempty"` // yalnız yönetici
	Servers   []topoServer   `json:"servers"`
	Channels  []topoChannel  `json:"channels,omitempty"` // yalnız düzenleyici ve üstü
	Incidents []topoIncident `json:"incidents"`
}

// topoMaxIncidents açık olay özetinin üst sınırı (sayaç tüm açık olayları sayar).
const topoMaxIncidents = 50

func monitorState(m store.Monitor, inMaint, partial bool) string {
	switch {
	case !m.Active:
		return "paused"
	case m.Status == store.StatusMaintenance || inMaint:
		return "maintenance"
	case m.Status == store.StatusDown:
		return "down"
	case m.Status == store.StatusPending:
		return "pending"
	case partial:
		return "partial"
	}
	return "up"
}

func (s *Server) topology(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := userFrom(r)
	vis := visibleTo(u)
	admin := isPageAdmin(u)
	lang := responseLang(w)
	now := s.now()
	resp := topologyResp{Now: now.Unix(), Admin: admin, Monitors: []topoMonitor{}, Probes: []topoProbe{},
		Servers: []topoServer{}, Incidents: []topoIncident{}}

	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	partials, err := s.store.OpenPartialIncidentIDs(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindLocation)
	if err != nil {
		s.dbError(w, err)
		return
	}
	localName := s.engine.LocalLocationName()
	probeNames := make(map[int64]string, len(probes))
	for _, p := range probes {
		probeNames[p.ID] = p.Name
	}
	used := map[int64]bool{} // görünür monitörlerin kullandığı kontrol noktaları
	for _, m := range monitors {
		if !vis.can(m.ID) {
			continue
		}
		_, partial := partials[m.ID]
		tm := topoMonitor{ID: m.ID, Name: m.Name, Type: m.Type, State: monitorState(m, m.Active && s.engine.InMaintenance(m.ID, now), partial),
			LastCheckAt: m.LastCheckAt, PingMs: m.LastPingMs, Locations: []topoLocation{}}
		if st, ok := s.engine.LocationStatuses(m.ID); ok {
			for _, l := range st {
				name := l.Name
				if l.ProbeID == engine.LocalProbeID {
					name = localName
				} else {
					used[l.ProbeID] = true
				}
				tm.Locations = append(tm.Locations, topoLocation{ProbeID: l.ProbeID, Name: name, Status: l.Status, OOB: l.OOB,
					LastCheckAt: l.LastCheckAt, PingMs: l.PingMs})
			}
		} else if m.Active {
			// Yalnız ana sunucudan denetlenen monitör: tek konum, durum monitörün kendisi.
			tm.Locations = append(tm.Locations, topoLocation{Name: localName, Status: localStatus(m), LastCheckAt: m.LastCheckAt, PingMs: m.LastPingMs})
		}
		resp.Monitors = append(resp.Monitors, tm)
	}

	var links []store.ProbeLink
	if admin {
		mv, stored, err := s.meshView(ctx, lang)
		if err != nil {
			s.dbError(w, err)
			return
		}
		resp.Mesh, links = &mv, stored
	}
	for _, p := range probes {
		if !vis.all && !used[p.ID] {
			continue
		}
		online := s.engine.ProbeOnline(p, now)
		resp.Stats.ProbesTotal++
		if online {
			resp.Stats.ProbesOnline++
		}
		tp := topoProbe{ID: p.ID, Name: p.Name, Online: online, LastSeenAt: p.LastSeenAt}
		if admin {
			tp.Version, tp.Uplink = p.Version, p.Uplink
			if tp.Uplink == "" {
				tp.Uplink = store.UplinkPrimary
			}
			peers, up := 0, 0
			for _, l := range links {
				if l.Src != p.ID {
					continue
				}
				if l.Dst == store.MeshMainID {
					if l.RTTms > 0 && online {
						rtt := l.RTTms
						tp.PanelRTTms = &rtt
					}
					continue
				}
				if _, known := probeNames[l.Dst]; !known {
					continue
				}
				peers++
				if l.State == store.LinkUp {
					up++
				}
			}
			tp.Peers, tp.PeersUp = &peers, &up
		}
		resp.Probes = append(resp.Probes, tp)
	}
	if vis.all {
		rps := s.engine.ResultsPerSecond(now)
		resp.Stats.ResultsPerSec = &rps
	}

	if !u.Restricted() || len(u.ServerIDs) > 0 {
		srv, err := s.store.ListProbesOfKind(ctx, store.ProbeKindServer)
		if err != nil {
			s.dbError(w, err)
			return
		}
		srv = slices.DeleteFunc(srv, func(p store.Probe) bool { return !vis.canServer(p.ID) })
		rules, err := s.store.AllServerAlerts(ctx)
		if err != nil {
			s.dbError(w, err)
			return
		}
		for _, p := range srv {
			v := s.servers.View(ctx, p, rules[p.ID], false)
			ts := topoServer{ID: v.ID, Name: v.Name, State: v.State, Firing: v.Firing, Version: v.Version, MetricsAt: v.MetricsAt, LastSeenAt: v.LastSeenAt}
			if ts.Firing == nil {
				ts.Firing = []string{}
			}
			if st := v.Latest; st != nil {
				cpu, mem, disk := st.CPU, st.MemPct(), st.DiskPct()
				ts.CPU, ts.Mem, ts.Disk = &cpu, &mem, &disk
			}
			resp.Servers = append(resp.Servers, ts)
		}
	}

	if store.RoleRank(u.Role) >= store.RoleRank(store.RoleEditor) {
		chs, err := s.store.ListNotifications(ctx)
		if err != nil {
			s.dbError(w, err)
			return
		}
		sends := s.notifier.LastSends()
		resp.Channels = make([]topoChannel, 0, len(chs))
		for _, c := range chs {
			tc := topoChannel{ID: c.ID, Name: c.Name, Type: c.Type, Active: c.Active}
			if st, ok := sends[c.ID]; ok {
				tc.LastSend = &st
			}
			resp.Channels = append(resp.Channels, tc)
		}
	}

	f := store.IncidentFilter{Open: true, NoMesh: !admin}
	if !vis.all {
		f.MonitorIDs, f.ServerIDs = vis.list(), vis.serverList()
	}
	f.Limit = 500 // sayaç bu sınıra kadar kesin; özet ilk topoMaxIncidents
	open, err := s.store.ListIncidents(ctx, f)
	if err != nil {
		s.dbError(w, err)
		return
	}
	resp.Stats.OpenIncidents = len(open)
	for i, in := range open {
		if i == topoMaxIncidents {
			break
		}
		resp.Incidents = append(resp.Incidents, topoIncident{ID: in.ID, Kind: in.Kind, MonitorID: in.MonitorID, MonitorName: in.MonitorName,
			ServerID: in.ServerID, ServerName: in.ServerName, StartedAt: in.StartedAt, Cause: i18n.Message(lang, incidentCause(lang, in)),
			Acked: in.AckedAt != 0})
	}
	writeJSON(w, http.StatusOK, resp)
}

// localStatus tek konumlu monitörün konum durumu (engine.LocationStatus değerleriyle).
func localStatus(m store.Monitor) string {
	switch {
	case m.LastCheckAt == 0:
		return "waiting"
	case m.Status == store.StatusUp || m.Status == store.StatusMaintenance:
		return "up"
	case m.Status == store.StatusDown:
		return "down"
	}
	return "retrying"
}
