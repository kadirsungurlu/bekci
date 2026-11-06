package api

import (
	"compress/gzip"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/sync/errgroup"
)

// GET /api/topology: canlı topoloji sayfasının tek istekte ihtiyaç duyduğu
// her şey. Yetki diğer uçlarla aynı: kısıtlı izleyici yalnız izinli monitörleri,
// onların kullandığı kontrol noktalarını (yalnız ad + çevrimiçi) ve atanmış
// sunucuları görür; mesh ve kontrol noktası ayrıntısı yalnız yöneticiye,
// bildirim kanalları düzenleyici ve üstüne (GET /api/notifications gibi).
//
// ?view=summary: ilişki sayfasının kullandığı hafif yanıt. Monitör başına konum
// nesneleri (locations) ve mesh ayrıntısı gönderilmez; konum kimlikleri ve sonuç
// listeleri (loc_ids, loc_bad, loc_off) yeterlidir. Ölçüm: docs/TOPOLOJI-OLCEK.md.

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
	Locations   []topoLocation `json:"locations,omitzero"` // ?view=summary ile gönderilmez
	// Konum özeti: LocIDs denetleyen noktalar (0: ana sunucu), LocBad son sonucu
	// hatalı olanlar, LocOff sonucu gelmeyen ya da karara katılmayan (OOB) olanlar;
	// ikisinde de olmayan nokta "ok" sayılır.
	LocIDs []int64 `json:"loc_ids"`
	LocBad []int64 `json:"loc_bad,omitempty"`
	LocOff []int64 `json:"loc_off,omitempty"`
	// Sorun: Message hata metni (yalnız down/partial), Since geçerli durumun
	// başlangıcı (açık olay varsa olayın başlangıcı).
	Message string `json:"message,omitempty"`
	Since   int64  `json:"since,omitempty"`
	// Series son kontrol süreleri (ms, eskiden yeniye, en çok 30; -1 ölçüm yok, -2 başarısız).
	Series       []int64           `json:"series,omitempty"`
	LastIncident *topoLastIncident `json:"last_incident,omitempty"`
	Tags         []int64           `json:"tags,omitempty"`      // yalnız düzenleyici ve üstü; resp.tags
	Customers    []int64           `json:"customers,omitempty"` // yalnız yönetici; resp.customers
	Channels     []int64           `json:"channels,omitempty"`  // yalnız düzenleyici ve üstü
	ServerID     int64             `json:"server_id,omitempty"` // görünür sunucuysa
}

// topoLastIncident monitörün en yeni kesintisi; ResolvedAt 0: sürüyor.
type topoLastIncident struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	StartedAt  int64  `json:"started_at"`
	ResolvedAt int64  `json:"resolved_at,omitempty"`
	Cause      string `json:"cause"`
	Acked      bool   `json:"acked,omitempty"`
}

type topoTag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// topoCustomer kısıtlı izleyici kullanıcı (müşteri); monitör ve sunucuların customers alanı.
type topoCustomer struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type topoProbe struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
	// Late: çevrimiçi ama kalp atışı iki aralıktır gelmiyor (eşikten önce erken uyarı).
	Late bool `json:"late,omitempty"`
	// Yalnız yönetici: panel RTT (son kalp atışı, ms; yok: ölçülmedi / eski ajan),
	// etkin hat (primary | oob), mesh eş sayısı (ulaşılan / toplam), sürüm.
	PanelRTTms *int64 `json:"panel_rtt_ms,omitempty"`
	Uplink     string `json:"uplink,omitempty"`
	OOBName    string `json:"oob_name,omitempty"` // yalnız OOB tanımlıysa: hattın görünen adı
	Peers      *int   `json:"peers,omitempty"`
	PeersUp    *int   `json:"peers_up,omitempty"`
	Version    string `json:"version,omitempty"`
	LastSeenAt int64  `json:"last_seen_at"`
	// Üst bilgi (boş: belirtilmemiş). Provider yalnız yöneticiye.
	Region   string `json:"region,omitempty"`
	Code     string `json:"code,omitempty"`
	Provider string `json:"provider,omitempty"`
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
	Role       string   `json:"role,omitempty"`
	Region     string   `json:"region,omitempty"` // konum
	Customers  []int64  `json:"customers,omitempty"`
	Channels   []int64  `json:"channels,omitempty"` // yalnız düzenleyici ve üstü
	// Alert açık sunucu olayının metni, AlertSince başlangıcı.
	Alert      string `json:"alert,omitempty"`
	AlertSince int64  `json:"alert_since,omitempty"`
}

// topoChannel LastSend: son gönderim (DB'de; yok: henüz gönderilmedi; başarısızsa
// LastSend.Error hata metni). Sent24h son 24 saatteki gönderim, Failed24h bunun başarısızı.
type topoChannel struct {
	ID        int64              `json:"id"`
	Name      string             `json:"name"`
	Type      string             `json:"type"`
	Active    bool               `json:"active"`
	LastSend  *notify.SendStatus `json:"last_send,omitempty"`
	Sent24h   int                `json:"sent_24h"`
	Failed24h int                `json:"failed_24h"`
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
	Channels  []topoChannel  `json:"channels,omitzero"` // yalnız düzenleyici ve üstü
	Incidents []topoIncident `json:"incidents"`
	// Görünür monitörlerin etiketleri, ana sunucunun konum adı (loc_ids içindeki 0),
	// müşteriler (yalnız yönetici).
	Tags      []topoTag      `json:"tags"`
	LocalName string         `json:"local_name"`
	Customers []topoCustomer `json:"customers,omitzero"`
}

// topoMaxIncidents açık olay özetinin üst sınırı (sayaç tüm açık olayları sayar).
const topoMaxIncidents = 50

// topoReadLimit aynı anda çalışan DB okuması.
const topoReadLimit = 4

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
		Servers: []topoServer{}, Incidents: []topoIncident{}, Tags: []topoTag{}}
	summary := r.URL.Query().Get("view") == "summary"
	editor := store.RoleRank(u.Role) >= store.RoleRank(store.RoleEditor)

	// Okumalar paralel: 1000 monitörde sıralı okuma tek başına yanıt süresinin yarısı
	// (docs/TOPOLOJI-OLCEK.md). Kısıt bağlantı havuzunu tek isteğe kaptırmamak için.
	var (
		monitors                        []store.Monitor
		partials                        map[int64]int64
		probes, srv                     []store.Probe
		tags                            map[int64][]store.MonitorTag // izleyiciden gizli (GET /api/monitors gibi)
		open                            []store.Incident
		lastInc                         map[int64]store.Incident
		monChannels, srvChannels, byTag map[int64][]int64
		users                           []store.User
		links                           []store.ProbeLink
		rules                           map[int64][]store.ServerAlert
		chs                             []store.Notification
		sends                           map[int64]notify.SendStatus
		counts                          map[int64]store.ChannelCount
	)
	// Olaylar: sayaç ve özet aşağıda; monitör ve sunucu satırları da kullanır.
	f := store.IncidentFilter{Open: true, NoMesh: !admin}
	if !vis.all {
		f.MonitorIDs, f.ServerIDs = vis.list(), vis.serverList()
	}
	f.Limit = 500 // sayaç bu sınıra kadar kesin; özet ilk topoMaxIncidents
	wantServers := !u.Restricted() || len(u.ServerIDs) > 0
	var g errgroup.Group
	g.SetLimit(topoReadLimit)
	read := func(fn func() error) { g.Go(fn) }
	read(func() (err error) { monitors, err = s.store.ListMonitors(ctx); return })
	read(func() (err error) { partials, err = s.store.OpenPartialIncidentIDs(ctx); return })
	read(func() (err error) { probes, err = s.store.ListProbesOfKind(ctx, store.ProbeKindLocation); return })
	read(func() (err error) { open, err = s.store.ListIncidents(ctx, f); return })
	read(func() (err error) { lastInc, err = s.store.LastMonitorIncidents(ctx); return })
	if editor {
		read(func() (err error) { tags, err = s.store.MonitorTags(ctx); return })
		read(func() (err error) { monChannels, err = s.store.MonitorNotificationIDs(ctx); return })
		read(func() (err error) { byTag, err = s.store.TagNotificationIDs(ctx); return })
		read(func() (err error) { srvChannels, err = s.store.AllProbeNotificationIDs(ctx); return })
		read(func() (err error) { chs, err = s.store.ListNotifications(ctx); return })
		read(func() (err error) { sends, err = s.notifier.LastSends(ctx); return })
		read(func() (err error) { counts, err = s.store.ChannelSendCounts(ctx, now.Unix()-86400); return })
	}
	if admin {
		read(func() (err error) { users, err = s.store.ListUsers(ctx); return })
		if summary {
			read(func() (err error) { links, err = s.store.ListProbeLinks(ctx); return })
		}
	}
	if wantServers {
		read(func() (err error) { srv, err = s.store.ListProbesOfKind(ctx, store.ProbeKindServer); return })
		read(func() (err error) { rules, err = s.store.AllServerAlerts(ctx); return })
	}
	if err := g.Wait(); err != nil {
		s.dbError(w, err)
		return
	}
	groups := map[int64]bool{}
	for _, m := range monitors {
		if m.Type == "group" {
			groups[m.ID] = true
		}
	}
	// causeOf olay nedenini GET /api/incidents ile aynı sınırda verir: izleyici ham
	// hata metnini görmez. Sunucu olayının nedeni metrikten üretilir, temizlenmez.
	causeOf := func(in store.Incident) string {
		if store.IsServerIncident(in.Kind) {
			return i18n.Message(lang, incidentCause(lang, in))
		}
		typ := ""
		if groups[in.MonitorID] {
			typ = "group"
		}
		return displayMessage(u, lang, typ, store.StatusDown, incidentCause(lang, in))
	}
	openMonitor, openServer := map[int64]store.Incident{}, map[int64]store.Incident{}
	for _, in := range open {
		if in.MonitorID != 0 && openMonitor[in.MonitorID].ID == 0 {
			openMonitor[in.MonitorID] = in
		}
		if in.ServerID != 0 && openServer[in.ServerID].ID == 0 {
			openServer[in.ServerID] = in
		}
	}
	customerOfMon, customerOfSrv := map[int64][]int64{}, map[int64][]int64{}
	for id, list := range byTag {
		monChannels[id] = slices.Compact(slices.Sorted(slices.Values(append(monChannels[id], list...))))
	}
	if admin {
		for _, cu := range users {
			if !cu.Restricted() || cu.Disabled || len(cu.MonitorIDs)+len(cu.ServerIDs) == 0 {
				continue
			}
			name := cu.DisplayName
			if name == "" {
				name = cu.Username
			}
			resp.Customers = append(resp.Customers, topoCustomer{ID: cu.ID, Name: name})
			for _, id := range cu.MonitorIDs {
				customerOfMon[id] = append(customerOfMon[id], cu.ID)
			}
			for _, id := range cu.ServerIDs {
				customerOfSrv[id] = append(customerOfSrv[id], cu.ID)
			}
		}
	}
	usedTags := map[int64]bool{}
	localName := s.engine.LocalLocationName()
	resp.LocalName = localName
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
		var msg string
		tm := topoMonitor{ID: m.ID, Name: m.Name, Type: m.Type, State: monitorState(m, m.Active && s.engine.InMaintenance(m.ID, now), partial),
			LastCheckAt: m.LastCheckAt, PingMs: m.LastPingMs, Locations: []topoLocation{},
			Series: s.engine.PingHistory(m.ID), Customers: customerOfMon[m.ID], Channels: monChannels[m.ID]}
		if m.ServerID != 0 && vis.canServer(m.ServerID) {
			tm.ServerID = m.ServerID
		}
		for _, mt := range tags[m.ID] {
			if !slices.Contains(tm.Tags, mt.ID) { // aynı etiket farklı değerlerle bağlı olabilir
				tm.Tags = append(tm.Tags, mt.ID)
			}
			if !usedTags[mt.ID] {
				usedTags[mt.ID] = true
				resp.Tags = append(resp.Tags, topoTag{ID: mt.ID, Name: mt.Name, Color: mt.Color})
			}
		}
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
				if msg == "" && (l.Status == "down" || l.Status == "retrying") {
					msg = l.Message
				}
			}
		} else if m.Active {
			// Yalnız ana sunucudan denetlenen monitör: tek konum, durum monitörün kendisi.
			tm.Locations = append(tm.Locations, topoLocation{Name: localName, Status: localStatus(m), LastCheckAt: m.LastCheckAt, PingMs: m.LastPingMs})
		}
		tm.summarizeLocations()
		if summary {
			tm.Locations = nil
		}
		if tm.State == "down" || tm.State == "partial" {
			if msg == "" {
				msg = m.LastMessage
			}
			tm.Message = displayMessage(u, lang, m.Type, m.Status, msg)
		}
		tm.Since = m.LastChangeAt
		if in, ok := openMonitor[m.ID]; ok {
			tm.Since = in.StartedAt
		}
		if in, ok := lastInc[m.ID]; ok {
			tm.LastIncident = &topoLastIncident{ID: in.ID, Kind: in.Kind, StartedAt: in.StartedAt, ResolvedAt: in.ResolvedAt,
				Cause: causeOf(in), Acked: in.AckedAt != 0}
		}
		resp.Monitors = append(resp.Monitors, tm)
	}

	if admin && !summary {
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
		tp := topoProbe{ID: p.ID, Name: p.Name, Online: online, LastSeenAt: p.LastSeenAt, Region: p.Region, Code: p.Code}
		tp.Late = online && s.engine.ProbeLate(p, now)
		if admin {
			tp.Version, tp.Uplink, tp.Provider = p.Version, p.Uplink, p.Provider
			if tp.Uplink == "" {
				tp.Uplink = store.UplinkPrimary
			}
			if p.OOBIP != "" {
				tp.OOBName = p.OOBLabel()
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

	if wantServers {
		srv = slices.DeleteFunc(srv, func(p store.Probe) bool { return !vis.canServer(p.ID) })
		for _, p := range srv {
			v := s.servers.View(ctx, p, rules[p.ID], false)
			ts := topoServer{ID: v.ID, Name: v.Name, State: v.State, Firing: v.Firing, Version: v.Version, MetricsAt: v.MetricsAt, LastSeenAt: v.LastSeenAt,
				Role: p.Role, Region: p.Region, Customers: customerOfSrv[p.ID], Channels: srvChannels[p.ID]}
			if in, ok := openServer[p.ID]; ok {
				ts.Alert, ts.AlertSince = causeOf(in), in.StartedAt
			}
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

	if editor {
		resp.Channels = make([]topoChannel, 0, len(chs))
		for _, c := range chs {
			tc := topoChannel{ID: c.ID, Name: c.Name, Type: c.Type, Active: c.Active,
				Sent24h: counts[c.ID].Sent, Failed24h: counts[c.ID].Failed}
			if st, ok := sends[c.ID]; ok {
				tc.LastSend = &st
			}
			resp.Channels = append(resp.Channels, tc)
		}
	}

	resp.Stats.OpenIncidents = len(open)
	for i, in := range open {
		if i == topoMaxIncidents {
			break
		}
		resp.Incidents = append(resp.Incidents, topoIncident{ID: in.ID, Kind: in.Kind, MonitorID: in.MonitorID, MonitorName: in.MonitorName,
			ServerID: in.ServerID, ServerName: in.ServerName, StartedAt: in.StartedAt, Cause: causeOf(in),
			Acked: in.AckedAt != 0})
	}
	writeTopology(w, r, resp)
}

// writeTopology yanıtı istemci kabul ediyorsa sıkıştırır: 1000 monitörlük yanıt
// sık yoklanır, panel her kurulumda sıkıştıran bir vekilin arkasında değildir.
func writeTopology(w http.ResponseWriter, r *http.Request, resp topologyResp) {
	if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
	defer gz.Close()
	json.NewEncoder(gz).Encode(resp)
}

// summarizeLocations konum listesini kimlik + sonuç listelerine çevirir.
func (tm *topoMonitor) summarizeLocations() {
	tm.LocIDs = make([]int64, 0, len(tm.Locations))
	for _, l := range tm.Locations {
		tm.LocIDs = append(tm.LocIDs, l.ProbeID)
		switch {
		case l.OOB:
			tm.LocOff = append(tm.LocOff, l.ProbeID)
		case l.Status == "down" || l.Status == "retrying":
			tm.LocBad = append(tm.LocBad, l.ProbeID)
		case l.Status != "up":
			tm.LocOff = append(tm.LocOff, l.ProbeID)
		}
	}
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
