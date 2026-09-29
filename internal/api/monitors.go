package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// monitorSecrets monitör ayarlarında maskelenecek alanlar.
var monitorSecrets = map[string][]string{
	"http":      {"basic_pass", "proxy_pass", "tls_key", "oauth_client_secret", "headers"},
	"mysql":     {"password"},
	"postgres":  {"password"},
	"mssql":     {"password"},
	"redis":     {"password"},
	"mongodb":   {"uri"},
	"mqtt":      {"password"},
	"snmp":      {"community", "auth_password", "priv_password"},
	"grpc":      {"metadata"}, // genelde yetkilendirme başlığı taşır
	"websocket": {"headers"},  // genelde yetkilendirme başlığı taşır
}

func maskMonitorConfig(typ string, cfg json.RawMessage) json.RawMessage {
	keys := monitorSecrets[typ]
	if len(keys) == 0 {
		return cfg
	}
	var m map[string]any
	if json.Unmarshal(cfg, &m) != nil {
		return cfg
	}
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			m[k] = notify.Mask
		}
	}
	b, _ := json.Marshal(m)
	return b
}

// monitorSecretBound gizli alan maskeli (yani eski değeriyle) gönderildiğinde
// değişmemesi gereken alanlar. Hedef adres (host/port/url) zaten korunur; bu
// alanlar ise kayıtlı şifreyle başka bir sorgu çalıştırıp sonucunu okumayı,
// başka bir kullanıcı/veritabanı/anahtar/OID'e erişmeyi veya TLS'i kapatıp
// şifreyi açık ağda göndermeyi engeller. Değişiklik gerekiyorsa gizli alan
// yeniden girilmelidir.
var monitorSecretBound = map[string][]string{
	"http":      {"basic_user", "proxy_user", "oauth_client_id", "oauth_scopes", "method", "body"},
	"postgres":  {"username", "database", "query", "expected", "sslmode"},
	"mysql":     {"username", "database", "query", "expected", "tls"},
	"mssql":     {"username", "database", "query", "expected", "encrypt"},
	"redis":     {"username", "db", "tls", "key", "expected"},
	"mongodb":   {"database"},
	"mqtt":      {"username", "topic", "ignore_tls", "keyword", "json_path", "json_op", "json_expected"},
	"snmp":      {"version", "oid", "condition", "expected", "username", "auth_protocol", "priv_protocol"},
	"grpc":      {"service", "tls", "ignore_tls"},
	"websocket": {"send", "keyword", "ignore_tls"},
}

func mergeMonitorSecrets(typ string, newCfg, oldCfg json.RawMessage) (json.RawMessage, error) {
	keys := monitorSecrets[typ]
	if len(keys) == 0 {
		return newCfg, nil
	}
	var normalize func(json.RawMessage) (json.RawMessage, error)
	if c, ok := check.Get(typ); ok {
		normalize = c.Normalize
	}
	return notify.MergeSecretsBound(keys, monitorSecretBound[typ], normalize, newCfg, oldCfg)
}

type monitorView struct {
	store.Monitor
	Target          string             `json:"target"`
	NotificationIDs []int64            `json:"notification_ids"`
	Tags            []store.MonitorTag `json:"tags"`
	Uptime24h       *float64           `json:"uptime_24h"`
	Bars            []store.Bucket     `json:"bars"`
	InMaintenance   bool               `json:"in_maintenance"` // şu an etkin bir bakım penceresinde (durdurulmuşsa false)
	// Locations kontrol konumları (probes.go); varsayılan: yalnızca ana sunucu.
	Locations store.LocationSetup `json:"locations"`
	// OpenIncidentID süren olayın kimliği (listede "Olayı gör"); yoksa null.
	OpenIncidentID *int64 `json:"open_incident_id"`
	// Geniş ekran listesi için: son kontrollerin yanıt süreleri (eskiden yeniye;
	// store.PingDown başarısız, store.PingNone ölçümsüz), 7/30 günlük çalışma
	// oranı ve çok konumlu monitörde konumların canlı durumu.
	Pings     []int64       `json:"pings"`
	Uptime7d  *float64      `json:"uptime_7d"`
	Uptime30d *float64      `json:"uptime_30d"`
	LocStates []locationDot `json:"loc_states,omitempty"`
}

// recentPings listede küçük yanıt süresi grafiği için tutulan son kontrol sayısı.
const recentPings = 30

// locationDot listedeki konum noktası (ayrıntı ve mesaj detay sayfasında).
type locationDot struct {
	ProbeID int64  `json:"probe_id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	PingMs  int64  `json:"ping_ms"`
}

// hourlyBars son 24 saatin saatlik kovalarını, boş saatleri de doldurarak döner.
func hourlyBars(now time.Time, buckets []store.Bucket) ([]store.Bucket, *float64) {
	start := now.Unix() - now.Unix()%3600 - 23*3600
	byTime := make(map[int64]store.Bucket, len(buckets))
	for _, b := range buckets {
		byTime[b.Time] = b
	}
	bars := make([]store.Bucket, 24)
	var up, down int64
	for i := range bars {
		t := start + int64(i)*3600
		b, ok := byTime[t]
		if !ok {
			b = store.Bucket{Time: t, PingAvg: -1, PingMin: -1, PingMax: -1}
		}
		bars[i] = b
		up += b.Up
		down += b.Down
	}
	if up+down == 0 {
		return bars, nil
	}
	pct := 100 * float64(up) / float64(up+down)
	return bars, &pct
}

func (s *Server) buildViews(r *http.Request, monitors []store.Monitor) ([]monitorView, error) {
	now := s.now()
	hourly, err := s.store.HourlyAll(r.Context(), now.Unix()-now.Unix()%3600-23*3600)
	if err != nil {
		return nil, err
	}
	links, err := s.store.MonitorNotificationIDs(r.Context())
	if err != nil {
		return nil, err
	}
	locs, err := s.store.AllMonitorLocations(r.Context())
	if err != nil {
		return nil, err
	}
	tags, err := s.store.MonitorTags(r.Context())
	if err != nil {
		return nil, err
	}
	// Tek monitörlük yanıtta (detay, kaydetme) yalnızca o monitör sorgulanır;
	// listede tüm monitörler tek sorguyla (monitör başına sorgu yok).
	var only []int64
	if len(monitors) == 1 {
		only = []int64{monitors[0].ID}
	}
	pings, err := s.store.RecentPings(r.Context(), only, recentPings)
	if err != nil {
		return nil, err
	}
	windows, err := s.store.UptimeWindows(r.Context(), only, now.Unix()-7*86400, now.Unix()-30*86400)
	if err != nil {
		return nil, err
	}
	u := userFrom(r)
	vis, full := visibleTo(u), canSeeConfig(u)
	lang := userLang(r) // son mesaj Türkçe saklanır; yanıt diline çevrilir
	// Açık olay yalnızca çalışmayan (veya tekrar denenen) monitörlerde aranır;
	// sorgu monitör dizinini kullanır, olay tablosu taranmaz.
	var troubled []int64
	for _, m := range monitors {
		if vis.can(m.ID) && (m.Status == store.StatusDown || m.Status == store.StatusPending) {
			troubled = append(troubled, m.ID)
		}
	}
	openIncidents, err := s.store.OpenIncidentIDs(r.Context(), troubled)
	if err != nil {
		return nil, err
	}
	out := make([]monitorView, 0, len(monitors))
	for _, m := range monitors {
		if !vis.can(m.ID) {
			continue
		}
		bars, up := hourlyBars(now, hourly[m.ID])
		// Hedef, gizli alanlar maskelenmeden önce hesaplanır (ör. MongoDB URI'sinden
		// kimlik bilgisi atılmış sunucu adresi); Target zaten şifre içermez.
		target := engine.Target(m)
		m.Config = maskMonitorConfig(m.Type, m.Config)
		ids := links[m.ID]
		if ids == nil {
			ids = []int64{}
		}
		if !full {
			// İzleyici: adrese gömülü kullanıcı adı/şifre ve sorgu (token olabilir),
			// ayarlar, push token'ı ve bildirim bağlantıları gizli.
			target = publicTarget(target)
			m.LastMessage = viewerMessage(u, m.Type, m.Status, m.LastMessage)
			m.Config, m.PushToken, ids = json.RawMessage("{}"), "", []int64{}
		}
		m.LastMessage = i18n.Message(lang, m.LastMessage)
		if m.Type == check.TypeGroup {
			target = i18n.Message(lang, target) // "3 monitör"
		}
		loc, ok := locs[m.ID]
		if !ok {
			loc = store.DefaultLocations()
		}
		mt := tags[m.ID]
		if mt == nil {
			mt = []store.MonitorTag{}
		}
		var incident *int64
		if iid, ok := openIncidents[m.ID]; ok {
			incident = &iid
		}
		p := pings[m.ID]
		if p == nil {
			p = []int64{}
		}
		v := monitorView{Monitor: m, Target: target, NotificationIDs: ids, Tags: mt, Uptime24h: up, Bars: bars,
			InMaintenance: m.Active && s.engine.InMaintenance(m.ID, now), Locations: loc, OpenIncidentID: incident, Pings: p}
		if w := windows[m.ID]; w != nil {
			v.Uptime7d, v.Uptime30d = w[0], w[1]
		}
		if st, ok := s.engine.LocationStatuses(m.ID); ok {
			v.LocStates = make([]locationDot, len(st))
			for i, l := range st {
				v.LocStates[i] = locationDot{ProbeID: l.ProbeID, Name: locationName(lang, l.Name), Status: l.Status, PingMs: l.PingMs}
			}
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Server) listMonitors(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.store.ListMonitors(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if tag := r.URL.Query().Get("tag"); tag != "" {
		var ok bool
		if monitors, ok = s.filterByTag(w, r, monitors, tag); !ok {
			return
		}
	}
	views, err := s.buildViews(r, monitors)
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	m, err := s.store.GetMonitor(r.Context(), id)
	if err == nil && !visibleTo(userFrom(r)).can(id) {
		err = store.ErrNotFound
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	views, err := s.buildViews(r, []store.Monitor{m})
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now().Unix()
	uptime := map[string]*float64{}
	for key, days := range map[string]int64{"24h": 1, "7d": 7, "30d": 30, "90d": 90} {
		pct, ok, err := s.store.Uptime(r.Context(), id, now-days*86400)
		if err != nil {
			s.dbError(w, err)
			return
		}
		if ok {
			uptime[key] = &pct
		} else {
			uptime[key] = nil
		}
	}
	avg, err := s.store.AvgPing(r.Context(), id, now-86400)
	if err != nil {
		s.dbError(w, err)
		return
	}
	incidentStart, _ := s.store.OpenIncidentStart(r.Context(), id)
	incidentID, _ := s.store.OpenIncidentID(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{
		"monitor": views[0], "uptime": uptime, "avg_ping_24h": avg, "open_incident_since": incidentStart,
		"open_incident_id": incidentID,
	})
}

// incidentNote monitörün açık olayı varsa işlem geçmişine kullanıcı işlemini
// yazar (düzenleme, durdurma). closed: bu işlem olayı kapatıyor.
func (s *Server) incidentNote(r *http.Request, monitorID int64, kind string, closed bool) {
	msg := "Monitör ayarları değiştirildi"
	switch {
	case kind == store.EventPaused:
		msg = "Monitör durduruldu; olay kapatıldı"
	case closed:
		msg = "Monitörün hedefi değiştirildi; olay kapatıldı"
	}
	u := userFrom(r)
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	ev := store.IncidentEvent{Time: s.now().Unix(), Kind: kind, Message: msg,
		Data: store.EventData(map[string]any{"user": name, "closed": closed})}
	if err := s.store.AddOpenIncidentEvent(r.Context(), monitorID, ev); err != nil {
		s.log.Error("olay kaydı yazılamadı", "monitor", monitorID, "hata", err)
	}
}

// monitorInput monitör ekleme/düzenleme gövdesi.
type monitorInput struct {
	Name            string          `json:"name"`
	Type            string          `json:"type"`
	Description     string          `json:"description"`
	Interval        int             `json:"interval"`
	RetryInterval   int             `json:"retry_interval"`
	MaxRetries      int             `json:"max_retries"`
	Timeout         int             `json:"timeout"`
	ResendEvery     int             `json:"resend_every"`
	UpsideDown      bool            `json:"upside_down"`
	Config          json.RawMessage `json:"config"`
	NotificationIDs *[]int64        `json:"notification_ids"` // null: varsayılan kanallar
}

func between(v, lo, hi int) bool { return v >= lo && v <= hi }

// toMonitor girdiyi doğrular; hata mesajı kullanıcıya gösterilir.
func (in *monitorInput) toMonitor() (store.Monitor, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 100 {
		return store.Monitor{}, errors.New("Ad 1-100 karakter olmalı")
	}
	if utf8.RuneCountInString(in.Description) > 500 {
		return store.Monitor{}, errors.New("Açıklama en fazla 500 karakter olabilir")
	}
	checker, ok := check.Get(in.Type)
	if !ok {
		return store.Monitor{}, errors.New("Geçersiz monitör tipi")
	}
	if in.Interval == 0 {
		in.Interval = 60
	}
	if in.RetryInterval == 0 {
		in.RetryInterval = in.Interval
	}
	if in.Timeout == 0 {
		in.Timeout = 30
	}
	switch {
	case !between(in.Interval, 20, 86400):
		return store.Monitor{}, errors.New("Kontrol aralığı 20 saniye ile 24 saat arasında olmalı")
	case !between(in.RetryInterval, 20, 86400):
		return store.Monitor{}, errors.New("Tekrar deneme aralığı 20 saniye ile 24 saat arasında olmalı")
	case !between(in.MaxRetries, 0, 20):
		return store.Monitor{}, errors.New("Tekrar deneme sayısı 0-20 olmalı")
	case !between(in.Timeout, 1, 300):
		return store.Monitor{}, errors.New("Zaman aşımı 1-300 saniye olmalı")
	case !between(in.ResendEvery, 0, 10000):
		return store.Monitor{}, errors.New("Hatırlatma sıklığı 0-10000 olmalı")
	}
	cfg, err := checker.Normalize(in.Config)
	if err != nil {
		return store.Monitor{}, err
	}
	return store.Monitor{
		Name: in.Name, Type: in.Type, Description: in.Description, Active: true,
		Interval: in.Interval, RetryInterval: in.RetryInterval, MaxRetries: in.MaxRetries,
		Timeout: in.Timeout, ResendEvery: in.ResendEvery, UpsideDown: in.UpsideDown, Config: cfg,
	}, nil
}

// notificationIDs listeyi doğrular; nil ise varsayılan kanallar kullanılır.
func (s *Server) notificationIDs(r *http.Request, in *[]int64) ([]int64, error) {
	if in == nil {
		return s.store.DefaultNotificationIDs(r.Context())
	}
	exists, err := s.store.ExistingNotificationIDs(r.Context(), *in)
	if err != nil {
		return nil, err
	}
	for _, id := range *in {
		if !exists[id] {
			return nil, errors.New("Seçilen bildirim kanalı bulunamadı")
		}
	}
	return *in, nil
}

func (s *Server) createMonitor(w http.ResponseWriter, r *http.Request) {
	var in monitorInput
	if !readJSON(w, r, &in) {
		return
	}
	m, err := in.toMonitor()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.validateGroup(w, r, 0, m) {
		return
	}
	ids, err := s.notificationIDs(r, in.NotificationIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.Type == check.TypePush {
		m.PushToken = randomToken(24)
	}
	if err := s.store.CreateMonitor(r.Context(), &m, ids); err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.engine.Reload(r.Context(), m.ID); err != nil {
		s.log.Error("monitör başlatılamadı", "monitor", m.Name, "hata", err)
	}
	s.log.Info("monitör eklendi", "monitor", m.Name, "tip", m.Type)
	s.audit(r, store.User{}, "monitor.create", "monitor", m.ID, m.Name, m.Type)
	s.respondMonitor(w, r, m.ID, http.StatusCreated)
}

func (s *Server) respondMonitor(w http.ResponseWriter, r *http.Request, id int64, status int) {
	m, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	views, err := s.buildViews(r, []store.Monitor{m})
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, status, views[0])
}

func (s *Server) updateMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in monitorInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Type == old.Type {
		merged, err := mergeMonitorSecrets(in.Type, in.Config, old.Config)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Config = merged
	}
	m, err := in.toMonitor()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.validateGroup(w, r, id, m) {
		return
	}
	var ids []int64
	if in.NotificationIDs == nil {
		// Alan gönderilmediyse mevcut bağlantılar korunur.
		links, err := s.store.MonitorNotificationIDs(r.Context())
		if err != nil {
			s.dbError(w, err)
			return
		}
		ids = links[id]
	} else if ids, err = s.notificationIDs(r, in.NotificationIDs); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	m.ID = id
	m.PushToken = old.PushToken
	if m.Type == check.TypePush && m.PushToken == "" {
		m.PushToken = randomToken(24)
	} else if m.Type != check.TypePush {
		m.PushToken = ""
	}

	// Hedef değiştiyse eski durum ve açık olay yeni hedefe ait değildir.
	targetChanged := m.Type != old.Type || engine.Target(m) != engine.Target(old)
	s.engine.Remove(id)
	s.incidentNote(r, id, store.EventEdited, targetChanged)
	if targetChanged {
		if _, err := s.store.ResolveIncident(r.Context(), id, s.now().Unix()); err != nil {
			s.dbError(w, err)
			return
		}
	}
	if err := s.store.UpdateMonitor(r.Context(), &m, ids, targetChanged); err != nil {
		s.engine.Reload(r.Context(), id) // kayıt başarısızsa eski hali çalışmaya devam etsin
		s.dbError(w, err)
		return
	}
	if err := s.engine.Reload(r.Context(), id); err != nil {
		s.log.Error("monitör yeniden başlatılamadı", "monitor", m.Name, "hata", err)
	}
	s.audit(r, store.User{}, "monitor.update", "monitor", id, m.Name, "")
	s.respondMonitor(w, r, id, http.StatusOK)
}

func (s *Server) deleteMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	old, err := s.store.GetMonitor(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	defer s.engine.JobsChanged() // silme yazıldıktan sonra: kontrol noktaları işi hemen bıraksın
	s.engine.Remove(id)
	if err := s.store.DeleteMonitor(r.Context(), id); err != nil {
		s.dbError(w, err)
		return
	}
	s.afterMonitorDelete(r, id)
	s.audit(r, store.User{}, "monitor.delete", "monitor", id, old.Name, "")
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) pauseMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	defer s.engine.JobsChanged() // durdurma yazıldıktan sonra: kontrol noktaları işi hemen bıraksın
	s.engine.Remove(id)
	if err := s.store.SetMonitorActive(r.Context(), id, false); err != nil {
		s.dbError(w, err)
		return
	}
	// Durdurulan monitörün açık kesintisi kapanır; tekrar başlatıldığında
	// geçen süre kesinti sayılmaz.
	s.incidentNote(r, id, store.EventPaused, true)
	if _, err := s.store.ResolveIncident(r.Context(), id, s.now().Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "monitor.pause", "monitor", id, "", "")
	s.respondMonitor(w, r, id, http.StatusOK)
}

func (s *Server) resumeMonitor(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.store.SetMonitorActive(r.Context(), id, true); err != nil {
		s.dbError(w, err)
		return
	}
	if err := s.engine.Reload(r.Context(), id); err != nil {
		s.log.Error("monitör başlatılamadı", "id", id, "hata", err)
	}
	s.audit(r, store.User{}, "monitor.resume", "monitor", id, "", "")
	s.respondMonitor(w, r, id, http.StatusOK)
}

// monitorSeries detay grafiği: 24 saat ham kayıt; 7/30 gün saatlik; 90 gün günlük.
func (s *Server) monitorSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	mon, err := s.store.GetMonitor(r.Context(), id)
	if err != nil || !visibleTo(userFrom(r)).can(id) {
		if err == nil {
			err = store.ErrNotFound
		}
		s.dbError(w, err)
		return
	}
	u := userFrom(r)
	now := s.now().Unix()
	rng := r.URL.Query().Get("range")
	switch rng {
	case "", "24h":
		beats, err := s.store.Beats(r.Context(), id, now-86400)
		if err != nil {
			s.dbError(w, err)
			return
		}
		type point struct {
			T int64  `json:"t"`
			S int    `json:"s"`
			P int64  `json:"p"`
			M string `json:"m,omitempty"`
		}
		lang := responseLang(w)
		pts := make([]point, len(beats))
		for i, b := range beats {
			pts[i] = point{T: b.Time, S: b.Status, P: b.PingMs}
			if b.Status != store.StatusUp {
				pts[i].M = displayMessage(u, lang, mon.Type, b.Status, b.Message) // başarılı kontrollerin mesajı gereksiz yük
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"range": "24h", "kind": "raw", "points": pts})
	case "7d", "30d", "90d":
		days := map[string]int64{"7d": 7, "30d": 30, "90d": 90}[rng]
		daily := rng == "90d"
		buckets, err := s.store.Series(r.Context(), id, now-days*86400, daily)
		if err != nil {
			s.dbError(w, err)
			return
		}
		kind := "hourly"
		if daily {
			kind = "daily"
		}
		writeJSON(w, http.StatusOK, map[string]any{"range": rng, "kind": kind, "points": buckets})
	default:
		writeError(w, http.StatusBadRequest, "Geçersiz aralık")
	}
}

func (s *Server) monitorIncidents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.store.GetMonitor(r.Context(), id); err != nil || !visibleTo(userFrom(r)).can(id) {
		if err == nil {
			err = store.ErrNotFound
		}
		s.dbError(w, err)
		return
	}
	list, err := s.store.ListIncidents(r.Context(), store.IncidentFilter{MonitorID: id, Limit: 50})
	if err != nil {
		s.dbError(w, err)
		return
	}
	if u := userFrom(r); !canSeeConfig(u) {
		m, _ := s.store.GetMonitor(r.Context(), id)
		for k := range list {
			list[k].Cause = viewerMessage(u, m.Type, store.StatusDown, list[k].Cause)
		}
	}
	lang := responseLang(w)
	for k := range list {
		list[k].Cause = i18n.Message(lang, list[k].Cause)
	}
	writeJSON(w, http.StatusOK, list)
}
