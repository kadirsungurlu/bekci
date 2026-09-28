package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

type incidentResp struct {
	Incident  store.Incident         `json:"incident"`
	Monitor   incidentMonitor        `json:"monitor"`
	Location  string                 `json:"location"`
	Locations []incidentLocationView `json:"locations"`
	Events    []store.IncidentEvent  `json:"events"`
	Capture   *struct {
		Time     int64        `json:"time"`
		Location string       `json:"location"`
		Detail   check.Detail `json:"detail"`
	} `json:"capture"`
	Details bool `json:"details"`
}

func (r incidentResp) kinds() []string {
	out := make([]string, len(r.Events))
	for i, ev := range r.Events {
		out[i] = ev.Kind
	}
	return out
}

func (r incidentResp) has(kind string) bool {
	for _, ev := range r.Events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}

// Uçtan uca: monitör çöker → olay, işlem geçmişi, istek/yanıt ve bildirim
// kayıtları → düzelir. Yetkiler: editör her şeyi, izleyici temizlenmiş özeti,
// müşteri kısıtlı izleyici yalnızca izinli monitörü görür.
func TestIncidentDetailEndToEnd(t *testing.T) {
	admin := setupAdmin(t)

	var failing atomic.Bool
	failing.Store(true)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !failing.Load() {
			io.WriteString(w, "ok")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "cerez-gizli"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":"bad request","key":%q}`, r.Header.Get("X-Api-Key"))
	}))
	defer site.Close()
	hookSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer hookSrv.Close()

	admin.mustDo("POST", "/api/notifications", map[string]any{
		"name": "Kadir kancası", "type": "webhook", "is_default": true, "config": map[string]any{"url": hookSrv.URL},
	}, nil, 201)
	var mon monitorView
	cfg := map[string]any{"url": site.URL + "/durum?token=sorgu-gizli", "headers": "X-Api-Key: anahtar-gizli-123"}
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "http", "interval": 20, "retry_interval": 20, "max_retries": 1, "timeout": 300, "config": cfg,
	}, &mon, 201)
	other := admin.push("Başka")

	// Olay açılır ve bildirim kaydı düşer.
	var list []store.Incident
	waitFor(t, "olay", func() bool {
		admin.mustDo("GET", "/api/incidents", nil, &list, 200)
		return len(list) == 1
	})
	id := list[0].ID
	path := fmt.Sprintf("/api/incidents/%d", id)
	var full incidentResp
	waitFor(t, "bildirim kaydı", func() bool {
		admin.mustDo("GET", path, nil, &full, 200)
		return full.has(store.EventNotify)
	})
	var md struct {
		OpenIncidentID int64 `json:"open_incident_id"`
	}
	admin.mustDo("GET", fmt.Sprintf("/api/monitors/%d", mon.ID), nil, &md, 200)
	if md.OpenIncidentID != id {
		t.Fatalf("open_incident_id %d, %d bekleniyordu", md.OpenIncidentID, id)
	}

	// Yönetici: tam ayrıntı.
	if !full.Details || full.Incident.ResolvedAt != 0 || full.Location != "Ana sunucu" || len(full.Locations) != 1 ||
		full.Monitor.Type != "http" || !strings.Contains(full.Monitor.Target, "token=sorgu-gizli") {
		t.Fatalf("özet: %+v", full)
	}
	if !full.has(store.EventRetry) || !full.has(store.EventDown) {
		t.Fatalf("kayıtlar: %v", full.kinds())
	}
	c := full.Capture
	if c == nil || c.Location != "Ana sunucu" || c.Detail.Status != 400 || c.Detail.Method != "GET" {
		t.Fatalf("yakalama: %+v", c)
	}
	if v, _ := headerValue(c.Detail.RequestHeaders, "X-Api-Key"); v != notify.Mask {
		t.Errorf("istek başlığı maskelenmedi: %q", v)
	}
	if v, _ := headerValue(c.Detail.ResponseHeaders, "Set-Cookie"); v != notify.Mask {
		t.Errorf("Set-Cookie maskelenmedi: %q", v)
	}
	if strings.Contains(c.Detail.Body, "anahtar-gizli-123") || !strings.Contains(c.Detail.Body, "bad request") {
		t.Errorf("gövde: %s", c.Detail.Body)
	}
	var notif map[string]any
	for _, ev := range full.Events {
		if ev.Kind == store.EventNotify {
			json.Unmarshal(ev.Data, &notif)
		}
	}
	if notif["channel"] != "Kadir kancası" || notif["ok"] != true || notif["event"] != "down" {
		t.Errorf("bildirim kaydı: %v", notif)
	}

	// Olay sürerken düzenleme işlem geçmişine yazılır (kullanıcı adıyla).
	cfg["headers"] = notify.Mask
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", mon.ID), map[string]any{
		"name": "Site 2", "type": "http", "interval": 20, "retry_interval": 20, "max_retries": 1, "timeout": 300, "config": cfg,
	}, nil, 200)

	editor, _ := admin.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := admin.newUser("viewer1", store.RoleViewer, nil)
	allowed, _ := admin.newUser("musteri1", store.RoleViewer, []int64{mon.ID})
	denied, _ := admin.newUser("musteri2", store.RoleViewer, []int64{other.ID})

	// Editör: tüm işlem geçmişini görür (bildirim kaydı, düzenleme dahil) ama
	// ham istek/yanıt yakalamasını GÖRMEZ (yalnızca yönetici).
	var ed incidentResp
	editor.mustDo("GET", path, nil, &ed, 200)
	if ed.Details || ed.Capture != nil {
		t.Fatalf("editör ham yakalamayı görmemeli: details=%v capture=%v", ed.Details, ed.Capture)
	}
	if !ed.has(store.EventEdited) || !ed.has(store.EventNotify) || !ed.has(store.EventDown) {
		t.Fatalf("editör işlem geçmişini görmeli: %v", ed.kinds())
	}

	for name, cl := range map[string]*env{"izleyici": viewer, "müşteri": allowed} {
		var raw json.RawMessage
		cl.mustDo("GET", path, nil, &raw, 200)
		var v incidentResp
		json.Unmarshal(raw, &v)
		if v.Details || v.Capture != nil || v.has(store.EventNotify) || !v.has(store.EventDown) || !v.has(store.EventEdited) {
			t.Errorf("%s: %+v", name, v.kinds())
		}
		for _, secret := range []string{"sorgu-gizli", "Kadir kancası", "anahtar-gizli", "cerez-gizli", "editor", "kadir\""} {
			if strings.Contains(string(raw), secret) {
				t.Errorf("%s yanıtında %q var: %s", name, secret, raw)
			}
		}
	}
	denied.mustDo("GET", path, nil, nil, 404)
	admin.mustDo("GET", "/api/incidents/999999", nil, nil, 404)
	admin.mustDo("GET", "/api/incidents/abc", nil, nil, 404)

	// Düzelir: çözülme kaydı (ve ardından bildirimi) en üstte.
	failing.Store(false)
	waitFor(t, "çözüldü", func() bool {
		admin.mustDo("GET", path, nil, &full, 200)
		return full.Incident.ResolvedAt > 0 && full.has(store.EventUp)
	})
	waitFor(t, "up bildirim kaydı", func() bool {
		admin.mustDo("GET", path, nil, &full, 200)
		n := 0
		for _, ev := range full.Events {
			if ev.Kind == store.EventNotify {
				n++
			}
		}
		return n == 2
	})
}

// Kontrol noktasından gelen ayrıntı sunucuda sınırlanır ve monitör ayarıyla
// maskelenir; ayrıntı göndermeyen (eski) kontrol noktası da çalışır.
func TestProbeResultDetail(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	ctx := context.Background()
	a, b := admin.newProbe("CP Server İstanbul"), admin.newProbe("Eski")
	var m1, m2 monitorView
	for i, mp := range []*monitorView{&m1, &m2} {
		admin.mustDo("POST", "/api/monitors", map[string]any{
			"name": fmt.Sprintf("m%d", i+1), "type": "http", "interval": 5000, "timeout": 300,
			"config": map[string]any{"url": "https://ha.example", "headers": "Authorization: Bearer tok-gizli-999"},
		}, mp, 201)
	}
	admin.setLocations(m1.ID, map[string]any{"include_local": false, "probe_ids": []int64{a.Probe.ID}, "down_when": "any"}, 200)
	admin.setLocations(m2.ID, map[string]any{"include_local": false, "probe_ids": []int64{b.Probe.ID}, "down_when": "any"}, 200)
	// Konum ayarından önce ana sunucudan yapılmış olabilecek ilk kontrol (test
	// ortamında adres çözülemez → olay) temizlensin: durdur/başlat.
	for _, m := range []monitorView{m1, m2} {
		admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", m.ID), nil, nil, 200)
		admin.mustDo("POST", fmt.Sprintf("/api/monitors/%d/resume", m.ID), nil, nil, 200)
	}

	now := time.Now().UnixMilli()
	detail := map[string]any{
		"method": "GET", "url": "https://ha.example", "status": 400, "status_text": "Bad Request",
		"request_headers":  []map[string]string{{"name": "Authorization", "value": "Bearer tok-gizli-999"}},
		"response_headers": []map[string]string{{"name": "X-Echo", "value": "tok-gizli-999"}},
		"body":             strings.Repeat("x", 40<<10),
	}
	code, _, body := admin.rawReq("POST", "/api/probe/results", bearer(a.Token), map[string]any{"sent_at": now, "results": []map[string]any{
		{"monitor_id": m1.ID, "time": now, "up": false, "ping_ms": -1, "message": "HTTP 400 Bad Request", "detail": detail},
	}})
	if code != 200 {
		t.Fatalf("sonuç: %d %s", code, body)
	}
	// Eski kontrol noktası: detail alanı yok.
	code, _, body = admin.rawReq("POST", "/api/probe/results", bearer(b.Token), map[string]any{"sent_at": now, "results": []map[string]any{
		{"monitor_id": m2.ID, "time": now, "up": false, "ping_ms": -1, "message": "Zaman aşımı"},
	}})
	if code != 200 {
		t.Fatalf("eski sonuç: %d %s", code, body)
	}
	waitFor(t, "iki olay", func() bool {
		i1, _ := f.st.OpenIncidentID(ctx, m1.ID)
		i2, _ := f.st.OpenIncidentID(ctx, m2.ID)
		return i1 > 0 && i2 > 0
	})

	i1, _ := f.st.OpenIncidentID(ctx, m1.ID)
	var r1 incidentResp
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", i1), nil, &r1, 200)
	if r1.Location != "CP Server İstanbul" || r1.Capture == nil || r1.Capture.Location != "CP Server İstanbul" {
		t.Fatalf("konum/yakalama: %q %+v", r1.Location, r1.Capture)
	}
	d := r1.Capture.Detail
	if len(d.Body) > check.DetailBodyMax || !d.BodyTruncated || d.Status != 400 {
		t.Errorf("sınırlanmadı: %d %v", len(d.Body), d.BodyTruncated)
	}
	if v, _ := headerValue(d.RequestHeaders, "Authorization"); v != notify.Mask {
		t.Errorf("istek başlığı: %q", v)
	}
	if v, _ := headerValue(d.ResponseHeaders, "X-Echo"); strings.Contains(v, "tok-gizli") {
		t.Errorf("yanıt başlığı: %q", v)
	}

	i2, _ := f.st.OpenIncidentID(ctx, m2.ID)
	var r2 incidentResp
	admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", i2), nil, &r2, 200)
	if r2.Capture != nil || !r2.has(store.EventDown) || r2.Location != "Eski" {
		t.Fatalf("eski kontrol noktası: %+v", r2)
	}

	// Çok büyük gövde reddedilir (2 MB).
	code, _, _ = admin.rawReq("POST", "/api/probe/results", bearer(a.Token), map[string]any{"sent_at": now, "results": []map[string]any{
		{"monitor_id": m1.ID, "time": now, "up": false, "message": strings.Repeat("y", 3<<20)},
	}})
	if code != http.StatusRequestEntityTooLarge {
		t.Fatalf("büyük gövde: %d", code)
	}
}

// İşlem geçmişi tutulmadan önce açılmış olaylarda başlangıç ve çözülme üretilir.
func TestCompleteEvents(t *testing.T) {
	inc := store.Incident{StartedAt: 100, ResolvedAt: 400, Cause: "502"}
	evs := completeEvents(inc, []store.IncidentEvent{})
	if len(evs) != 2 || evs[0].Kind != store.EventUp || evs[1].Kind != store.EventDown || evs[1].Message != "502" {
		t.Fatalf("%+v", evs)
	}
	var d map[string]int64
	json.Unmarshal(evs[0].Data, &d)
	if d["downtime"] != 300 {
		t.Errorf("süre: %v", d)
	}
	// Durdurmayla kapanan olaya ayrıca çözülme eklenmez.
	evs = completeEvents(inc, []store.IncidentEvent{{ID: 2, Time: 400, Kind: store.EventPaused}, {ID: 1, Time: 100, Kind: store.EventDown}})
	if len(evs) != 2 {
		t.Fatalf("%+v", evs)
	}
	// Süren olay: yalnızca başlangıç.
	evs = completeEvents(store.Incident{StartedAt: 100}, nil)
	if len(evs) != 1 || evs[0].Kind != store.EventDown {
		t.Fatalf("%+v", evs)
	}
}

func headerValue(hs []check.Header, name string) (string, bool) {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}
