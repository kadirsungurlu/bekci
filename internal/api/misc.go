package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// summary liste ekranının sağındaki "Mevcut durum" ve "Son 24 saat" kutuları.
func (s *Server) summary(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.store.ListMonitors(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	now := s.now()
	hourly, err := s.store.HourlyAll(r.Context(), now.Unix()-now.Unix()%3600-23*3600)
	if err != nil {
		s.dbError(w, err)
		return
	}
	vis := visibleTo(userFrom(r))
	var up, down, pending, paused, maint, total int
	var sumUp, sumDown int64
	for _, m := range monitors {
		if !vis.can(m.ID) {
			continue
		}
		total++
		if !m.Active {
			paused++
			continue
		}
		switch {
		case m.Status == store.StatusMaintenance || s.engine.InMaintenance(m.ID, now):
			maint++
		case m.Status == store.StatusUp:
			up++
		case m.Status == store.StatusDown:
			down++
		default:
			pending++
		}
		for _, b := range hourly[m.ID] {
			sumUp += b.Up
			sumDown += b.Down
		}
	}
	since := now.Add(-24 * time.Hour).Unix()
	var incidents int
	if vis.all {
		incidents, err = s.store.CountIncidentsSince(r.Context(), since)
	} else {
		var list []store.Incident
		list, err = s.store.IncidentsFor(r.Context(), vis.list(), since, 10000)
		for _, in := range list {
			if in.StartedAt >= since {
				incidents++
			}
		}
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	var uptime *float64
	if sumUp+sumDown > 0 {
		pct := 100 * float64(sumUp) / float64(sumUp+sumDown)
		uptime = &pct
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"total": total, "up": up, "down": down, "pending": pending, "paused": paused, "maintenance": maint,
		"uptime_24h": uptime, "incidents_24h": incidents,
	})
}

func (s *Server) listIncidents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	kind := q.Get("kind")
	if !store.ValidKindGroup(kind) {
		writeError(w, http.StatusBadRequest, "Geçersiz olay türü")
		return
	}
	f := store.IncidentFilter{Before: before, Limit: limit, Kind: kind, Open: q.Get("open") == "1"}
	if vis := visibleTo(userFrom(r)); !vis.all {
		// Müşteri kısıtlı izleyici: izinli monitörlerin ve atanmış sunucuların olayları.
		f.MonitorIDs, f.ServerIDs = vis.list(), vis.serverList()
	}
	asCSV := q.Get("format") == "csv"
	var list []store.Incident
	var err error
	if asCSV {
		list, err = s.allIncidents(r.Context(), f)
	} else {
		list, err = s.store.ListIncidents(r.Context(), f)
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	if u := userFrom(r); !canSeeConfig(u) {
		groups, err := s.groupIDs(r)
		if err != nil {
			s.dbError(w, err)
			return
		}
		for k := range list {
			if store.IsServerIncident(list[k].Kind) {
				continue // neden sunucu metriğinden üretilir, temizlenecek bir şey yok
			}
			typ := ""
			if groups[list[k].MonitorID] {
				typ = "group"
			}
			list[k].Cause = viewerMessage(u, typ, store.StatusDown, list[k].Cause)
		}
	}
	lang := responseLang(w)
	localizeIncidents(lang, list)
	if asCSV {
		s.writeIncidentsCSV(w, lang, list)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// csvExportMax CSV dışa aktarımındaki en fazla satır (süzgeçle daraltılır).
const csvExportMax = 10000

// allIncidents süzgece uyan olayları sayfalayarak toplar (en fazla csvExportMax).
func (s *Server) allIncidents(ctx context.Context, f store.IncidentFilter) ([]store.Incident, error) {
	var out []store.Incident
	f.Limit = 500
	for len(out) < csvExportMax {
		page, err := s.store.ListIncidents(ctx, f)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		if len(page) < f.Limit {
			break
		}
		f.Before = page[len(page)-1].ID
	}
	if len(out) > csvExportMax {
		out = out[:csvExportMax]
	}
	return out, nil
}

// writeIncidentsCSV olay listesini CSV olarak yazar (UTF-8 BOM ile: Excel
// Türkçe karakterleri doğru açar). Zamanlar yerel saatte RFC 3339'dur; süre
// saniye cinsindendir (süren olayda şu ana kadar).
func (s *Server) writeIncidentsCSV(w http.ResponseWriter, lang string, list []store.Incident) {
	now := s.now()
	name := "olaylar"
	if lang == i18n.EN {
		name = "incidents"
	}
	h := w.Header()
	h.Set("Content-Type", "text/csv; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`-`+now.Format("2006-01-02")+`.csv"`)
	h.Set("Cache-Control", "no-store")
	w.Write([]byte("\xEF\xBB\xBF"))
	cw := csv.NewWriter(w)
	cw.Write([]string{"id", "kind", "source", "started_at", "resolved_at", "duration_seconds", "cause"})
	for _, in := range list {
		source := in.MonitorName
		if store.IsServerIncident(in.Kind) {
			source = in.ServerName
		}
		started := time.Unix(in.StartedAt, 0).Local().Format(time.RFC3339)
		resolved, end := "", now.Unix()
		if in.ResolvedAt > 0 {
			resolved, end = time.Unix(in.ResolvedAt, 0).Local().Format(time.RFC3339), in.ResolvedAt
		}
		cw.Write([]string{strconv.FormatInt(in.ID, 10), in.Kind, source, started, resolved,
			strconv.FormatInt(max(0, end-in.StartedAt), 10), in.Cause})
	}
	cw.Flush()
}

// groupIDs grup tipindeki monitörlerin kimlikleri (izleyici mesaj temizliği için).
func (s *Server) groupIDs(r *http.Request) (map[int64]bool, error) {
	monitors, err := s.store.ListMonitors(r.Context())
	if err != nil {
		return nil, err
	}
	out := map[int64]bool{}
	for _, m := range monitors {
		if m.Type == "group" {
			out[m.ID] = true
		}
	}
	return out, nil
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.store.LoadSettings(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	// Varsayılan User-Agent yalnızca bilgi (formda örnek olarak gösterilir).
	writeJSON(w, http.StatusOK, struct {
		store.AppSettings
		DefaultUserAgent string `json:"default_user_agent"`
	}{st, check.DefaultUserAgent()})
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	// GET yanıtı aynen geri gönderilebilsin: salt okunur alan kabul edilip yok sayılır.
	var body struct {
		store.AppSettings
		DefaultUserAgent string `json:"default_user_agent"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	in := body.AppSettings
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	old, _ := s.store.LoadSettings(r.Context())
	if err := s.store.SaveSettings(r.Context(), in); err != nil {
		s.dbError(w, err)
		return
	}
	s.engine.SetSettings(in)
	s.audit(r, store.User{}, "settings.update", "settings", 0, "", settingsChanges(old, in))
	writeJSON(w, http.StatusOK, in)
}

// settingsChanges işlem kaydı için değişen ayarların listesi (Türkçe saklanır).
func settingsChanges(old, in store.AppSettings) string {
	var changed []string
	add := func(cond bool, label string) {
		if cond {
			changed = append(changed, label)
		}
	}
	add(old.RetentionRawDays != in.RetentionRawDays, "ham kayıt saklama")
	add(old.RetentionHourlyDays != in.RetentionHourlyDays, "saatlik özet saklama")
	add(!slices.Equal(old.CertDays, in.CertDays), "SSL eşikleri")
	add(old.BackupKeep != in.BackupKeep, "yedek sayısı")
	add(old.NotifyLang != in.NotifyLang, "bildirim dili")
	add(old.CheckUserAgent != in.CheckUserAgent, "User-Agent")
	add(old.IncidentKeep() != in.IncidentKeep(), "olay saklama")
	add(old.CaptureKeep() != in.CaptureKeep(), "istek/yanıt saklama")
	add(old.AuditKeep() != in.AuditKeep(), "işlem kaydı saklama")
	if len(changed) == 0 {
		return "değişiklik yok"
	}
	return "değişen: " + strings.Join(changed, ", ")
}

// push: /api/push/{token}?status=up|down&msg=...&ping=123
// Cron işleri ve betikler tarafından çağrılır; oturum gerektirmez.
func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	// IP başına cömert bir sınır: token 192 bit, tahmin edilemez; sınır
	// yalnızca kaynak tüketimine (veritabanı sorgusu) karşıdır. Aynı
	// sunucudaki onlarca cron işi rahatça sığar.
	if ok, retry := s.pushRL.allow(clientIP(r), s.now()); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "error": "çok fazla istek; biraz sonra tekrar deneyin"})
		return
	}
	q := r.URL.Query()
	up := q.Get("status") != "down"
	msg := strings.ToValidUTF8(q.Get("msg"), "�")
	if r := []rune(msg); len(r) > 250 {
		msg = string(r[:250]) // bayta göre kesmek çok baytlı harfleri bozardı
	}
	ping := int64(-1)
	if p, err := strconv.ParseInt(q.Get("ping"), 10, 64); err == nil && p >= 0 {
		ping = p
	}
	err := s.engine.Push(r.Context(), r.PathValue("token"), up, msg, ping)
	switch {
	case errors.Is(err, engine.ErrPushNotFound):
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
	case errors.Is(err, engine.ErrPushPaused):
		writeJSON(w, http.StatusConflict, map[string]any{"ok": false, "error": err.Error()})
	case err != nil:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": "sunucu hatası"})
	default:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// events canlı olay akışı (Server-Sent Events).
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r)
	// Kullanıcı başına ve genel eşzamanlı bağlantı sınırı: kanal/gorutin
	// ayrılmadan önce kontrol edilir (aksi halde tek kullanıcı sınırsız SSE
	// açıp kaynak tüketebilirdi).
	ch, unsubscribe, ok := s.hub.Subscribe(u.ID)
	if !ok {
		writeError(w, http.StatusTooManyRequests, "Çok fazla eşzamanlı canlı bağlantı; açık sekmelerden birini kapatın")
		return
	}
	defer unsubscribe()

	rc := http.NewResponseController(w)
	// Bu bağlantı uzun ömürlü; sunucunun genel okuma/yazma süre sınırları
	// uygulanmaz. (Okuma sınırı dolunca net/http isteğin context'ini iptal
	// ederek akışı keser.)
	rc.SetReadDeadline(time.Time{})
	rc.SetWriteDeadline(time.Time{})
	// Yazmalar yine de süre sınırlıdır: her yazma öncesi kısa bir süre tanınır
	// (sseWriteTimeout); veri almayan yarı açık istemci sonsuza dek eşzamanlı
	// bağlantı kotasında yer tutmaz.
	write := func(format string, a ...any) bool {
		rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout))
		if _, err := fmt.Fprintf(w, format, a...); err != nil {
			return false
		}
		return rc.Flush() == nil
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	vis := visibleTo(u)
	lang := streamLang(u, r) // mesajlar bu bağlantının diline çevrilir (bkz. localizeEvent)
	var groups map[int64]bool
	if !canSeeConfig(u) {
		// İzleyici: canlı olay mesajları da temizlenir (grup kimlikleri bağlantı başında alınır).
		groups, _ = s.groupIDs(r)
	}

	if !write("retry: 5000\n\n") {
		return
	}
	// Cloudflare ve proxy'ler boşta kalan bağlantıyı kesmesin diye düzenli yorum
	// satırı. Aynı anda erişim yeniden doğrulanır: oturum kapatılan, anahtarı
	// iptal edilen, devre dışı bırakılan veya yetkisi değişen kullanıcının açık
	// akışı eski yetkiyle olay almaya devam etmez.
	every := s.sseRecheck
	if every <= 0 {
		every = 25 * time.Second
	}
	keepAlive := time.NewTicker(every)
	defer keepAlive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case msg, open := <-ch:
			if !open {
				return // aynı kullanıcının daha yeni bir akışı için kapatıldı
			}
			if !vis.all && !eventVisible(msg, vis) {
				continue
			}
			if msg = viewerEvent(u, msg, groups); msg == nil {
				continue
			}
			msg = localizeEvent(lang, msg)
			if !write("data: %s\n\n", msg) {
				return
			}
		case <-keepAlive.C:
			cur, _, ok := s.authenticate(r)
			if !ok || cur.ID != u.ID || cur.MustChangePassword ||
				store.RoleRank(cur.Role) < store.RoleRank(store.RoleViewer) {
				return
			}
			u = cur
			vis = visibleTo(u)
			lang = streamLang(u, r) // dil tercihi bağlantı açıkken değişmiş olabilir
			if !canSeeConfig(u) {
				if g, err := s.groupIDs(r); err == nil {
					groups = g
				}
			} else {
				groups = nil
			}
			if !write(": ping\n\n") {
				return
			}
		}
	}
}

// sseWriteTimeout canlı akışta tek bir yazmanın en uzun süresi.
const sseWriteTimeout = 30 * time.Second

// eventVisible kısıtlı kullanıcı için olayın izinli bir monitöre ait olup
// olmadığını söyler; monitöre bağlı olmayan olaylar gönderilmez.
func eventVisible(msg []byte, vis visibility) bool {
	var ev struct {
		Type string `json:"type"`
		Data struct {
			MonitorID int64 `json:"monitor_id"`
			ID        int64 `json:"id"` // "server" olayında sunucunun kimliği
		} `json:"data"`
	}
	if json.Unmarshal(msg, &ev) != nil {
		return false
	}
	// Bakım değişikliği olayı yalnızca bakım kimliği taşır (içerik yok); kısıtlı
	// izleyicinin ekranı da "Bakımda" durumunu hemen yenileyebilsin.
	if ev.Type == "maintenance" {
		return true
	}
	// Sunucu olayı yalnızca kendisine atanmış sunucu için gider.
	if ev.Type == "server" {
		return ev.Data.ID != 0 && vis.canServer(ev.Data.ID)
	}
	return ev.Data.MonitorID != 0 && vis.can(ev.Data.MonitorID)
}
