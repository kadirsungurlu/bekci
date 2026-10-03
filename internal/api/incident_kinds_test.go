package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// fireCPU sunucuya CPU kuralı ekleyip tetikler (sunucu olayı açar).
func fireCPU(t *testing.T, st *store.Store, probeID, at int64) {
	t.Helper()
	ctx := context.Background()
	rules, err := st.ReplaceServerAlerts(ctx, probeID, []store.ServerAlert{{Metric: "cpu", Threshold: 90, Minutes: 5, Active: true}}, at)
	if err != nil {
		t.Fatal(err)
	}
	rules[0].ProbeID = probeID
	if ok, err := st.FireServerAlert(ctx, rules[0], 96, "", at); !ok || err != nil {
		t.Fatalf("uyarı tetiklenmedi: %v %v", ok, err)
	}
}

// Olay türleri ve görünürlük: müşteri kısıtlı izleyici yalnızca izinli
// monitörlerin (kısmi dahil) ve atanmış sunucuların olaylarını görür; tür
// süzgeci; sunucu olayının ayrıntısı ve sunucu ayrıntısındaki liste.
func TestIncidentKindsVisibility(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	ctx := context.Background()
	mon := admin.push("Müşteri sitesi")
	hidden := admin.push("Başka site")
	var own, other serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Müşteri sunucusu"}, &own, 201)
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "Gizli sunucu"}, &other, 201)
	admin.mustDo("POST", "/api/users", map[string]any{"username": "musteri", "role": "viewer", "password": "gecici-sifre-1",
		"all_monitors": false, "monitor_ids": []int64{mon.ID}, "server_ids": []int64{own.Probe.ID}}, nil, 201)
	cust := admin.loginAs("musteri", "gecici-sifre-1")
	cust.mustDo("POST", "/api/auth/password", map[string]string{"current": "gecici-sifre-1", "new": "kalici-sifre-1"}, nil, 200)

	st := f.st
	st.OpenIncident(ctx, mon.ID, 100, "HTTP 503")
	st.OpenIncident(ctx, hidden.ID, 101, "gizli neden")
	st.StartPartialIncident(ctx, mon.ID, 102, "Berlin: Zaman aşımı", nil)
	fireCPU(t, st, own.Probe.ID, 103)
	fireCPU(t, st, other.Probe.ID, 104)

	var all []store.Incident
	admin.mustDo("GET", "/api/incidents", nil, &all, 200)
	if len(all) != 5 {
		t.Fatalf("yönetici tüm olayları görmeli: %d", len(all))
	}
	var srv []store.Incident
	admin.mustDo("GET", "/api/incidents?kind=server", nil, &srv, 200)
	if len(srv) != 2 || srv[0].Kind != store.IncidentServerAlert || srv[0].ServerName != "Gizli sunucu" ||
		srv[0].Cause != "CPU %96 (5 dk ortalama, eşik %90)" {
		t.Fatalf("sunucu süzgeci: %+v", srv)
	}
	admin.mustDo("GET", "/api/incidents?kind=kismi", nil, nil, 400)
	// open=1: yalnızca süren olaylar (monitör listesinin yan paneli).
	st.ResolveIncident(ctx, hidden.ID, 150)
	var open []store.Incident
	admin.mustDo("GET", "/api/incidents?open=1&kind=monitor", nil, &open, 200)
	if len(open) != 1 || open[0].MonitorID != mon.ID || open[0].ResolvedAt != 0 {
		t.Fatalf("süren olay süzgeci: %+v", open)
	}

	var mine []store.Incident
	cust.mustDo("GET", "/api/incidents", nil, &mine, 200)
	kinds := map[string]int{}
	for _, in := range mine {
		kinds[in.Kind]++
		if in.MonitorID == hidden.ID || in.ServerID == other.Probe.ID {
			t.Errorf("müşteri başkasının olayını gördü: %+v", in)
		}
	}
	if len(mine) != 3 || kinds[store.IncidentMonitor] != 1 || kinds[store.IncidentPartial] != 1 || kinds[store.IncidentServerAlert] != 1 {
		t.Fatalf("müşterinin olayları: %+v", mine)
	}
	var mp []store.Incident
	cust.mustDo("GET", "/api/incidents?kind=partial", nil, &mp, 200)
	if len(mp) != 1 || mp[0].Kind != store.IncidentPartial {
		t.Fatalf("kısmi süzgeç: %+v", mp)
	}
	// Monitör listesi ve ayrıntısı süren konum kesintisini gösterir (QA 2. tur O2).
	var mons []monitorView
	cust.mustDo("GET", "/api/monitors", nil, &mons, 200)
	partialID := mp[0].ID
	for _, m := range mons {
		if m.ID == mon.ID && (m.OpenPartialIncidentID == nil || *m.OpenPartialIncidentID != partialID) {
			t.Fatalf("listede open_partial_incident_id: %+v", m.OpenPartialIncidentID)
		}
	}
	var det struct {
		Monitor monitorView `json:"monitor"`
	}
	cust.mustDo("GET", fmt.Sprintf("/api/monitors/%d", mon.ID), nil, &det, 200)
	if det.Monitor.OpenPartialIncidentID == nil || *det.Monitor.OpenPartialIncidentID != partialID {
		t.Fatalf("ayrıntıda open_partial_incident_id: %+v", det.Monitor.OpenPartialIncidentID)
	}
	// Monitör ayrıntısındaki liste kısmi olayı da içerir.
	var ml []store.Incident
	cust.mustDo("GET", fmt.Sprintf("/api/monitors/%d/incidents", mon.ID), nil, &ml, 200)
	if len(ml) != 2 {
		t.Fatalf("monitör olayları: %+v", ml)
	}

	idOf := func(list []store.Incident, serverID int64) int64 {
		for _, in := range list {
			if in.ServerID == serverID {
				return in.ID
			}
		}
		t.Fatalf("sunucu %d olayı yok", serverID)
		return 0
	}
	ownID, otherID := idOf(all, own.Probe.ID), idOf(all, other.Probe.ID)
	var d struct {
		Incident store.Incident `json:"incident"`
		Server   *struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"server"`
		Events []store.IncidentEvent `json:"events"`
	}
	cust.mustDo("GET", fmt.Sprintf("/api/incidents/%d", ownID), nil, &d, 200)
	if d.Server == nil || d.Server.ID != own.Probe.ID || d.Server.Name != "Müşteri sunucusu" || d.Incident.Kind != store.IncidentServerAlert ||
		len(d.Events) != 1 || d.Events[0].Kind != store.EventDown || !strings.Contains(string(d.Incident.Data), `"metric":"cpu"`) {
		t.Fatalf("sunucu olayı ayrıntısı: %+v", d)
	}
	cust.mustDo("GET", fmt.Sprintf("/api/incidents/%d", otherID), nil, nil, 404)
	cust.mustDo("GET", fmt.Sprintf("/api/servers/%d/incidents", own.Probe.ID), nil, &srv, 200)
	if len(srv) != 1 || srv[0].ID != ownID {
		t.Fatalf("sunucu ayrıntısındaki olaylar: %+v", srv)
	}
	cust.mustDo("GET", fmt.Sprintf("/api/servers/%d/incidents", other.Probe.ID), nil, nil, 404)

	// Sunucu atanmamış müşteri sunucu olaylarını hiç görmez.
	nos, _ := admin.newUser("musteri2", store.RoleViewer, []int64{mon.ID})
	var l2 []store.Incident
	nos.mustDo("GET", "/api/incidents", nil, &l2, 200)
	for _, in := range l2 {
		if store.IsServerIncident(in.Kind) {
			t.Fatalf("sunucusuz müşteri sunucu olayı gördü: %+v", in)
		}
	}
	nos.mustDo("GET", fmt.Sprintf("/api/incidents/%d", ownID), nil, nil, 404)

	// Özet sayacı kısmi ve sunucu olaylarını saymaz.
	var sum map[string]any
	admin.mustDo("GET", "/api/summary", nil, &sum, 200)
	if n, _ := sum["incidents_24h"].(float64); n > 2 {
		t.Fatalf("özet sayacı: %v", sum["incidents_24h"])
	}
}

// Herkese açık durum sayfası kısmi kesintileri göstermez (sunucu olayları
// zaten bir monitöre bağlı değildir).
func TestPublicPageExcludesPartial(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	m := pe.seedMonitor("API", "https://api.example.com")
	now := pe.s.now().Unix()
	pe.beat(m.ID, now, store.StatusUp)
	st.OpenIncident(ctx, m.ID, now-7200, "HTTP 503")
	st.ResolveIncident(ctx, m.ID, now-7000)
	st.StartPartialIncident(ctx, m.ID, now-600, "Berlin: Zaman aşımı", nil)
	pe.mustDo("POST", "/api/status-pages", map[string]any{
		"slug": "genel", "title": "Genel",
		"sections": []map[string]any{{"title": "Servisler", "monitors": []map[string]any{{"id": m.ID}}}},
	}, nil, 201)
	resp, body := pe.anon().raw("GET", "/api/public/pages/genel", "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("sayfa: %d", resp.StatusCode)
	}
	var pub struct {
		Incidents []json.RawMessage `json:"incidents"`
	}
	json.Unmarshal(body, &pub)
	if len(pub.Incidents) != 1 {
		t.Fatalf("herkese açık olaylar yalnızca kesintiyi içermeli: %s", body)
	}
}

// Durdurma açık kısmi olayı da kapatır (durdurma kaydıyla).
func TestPauseClosesPartial(t *testing.T) {
	pe := setupPages(t)
	ctx := context.Background()
	st := pe.s.store
	m := pe.seedMonitor("Site", "https://site.example.com")
	id, err := st.StartPartialIncident(ctx, m.ID, pe.s.now().Unix()-60, "Berlin: Zaman aşımı", nil)
	if err != nil || id == 0 {
		t.Fatal(err)
	}
	pe.mustDo("POST", fmt.Sprintf("/api/monitors/%d/pause", m.ID), nil, nil, 200)
	inc, _ := st.GetIncident(ctx, id)
	evs, _ := st.IncidentEvents(ctx, id)
	if inc.ResolvedAt == 0 || len(evs) == 0 || evs[0].Kind != store.EventPaused {
		t.Fatalf("durdurulan monitörün kısmi olayı: %+v %+v", inc, evs)
	}
}

// Bulgu: olmayan veya yayında olmayan durum sayfası adresi (ve yayında
// olmayan sayfanın özel alan adı) arayüz HTML'iyle 200 dönüyordu. Artık aynı
// HTML 404 ile gelir (arayüz "bulunamadı" görünümünü gösterir).
func TestStatusPageSPA404(t *testing.T) {
	pe := setupPages(t)
	var draft, live pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "genel", "title": "Genel"}, &live, 201)
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "taslak", "title": "Taslak", "published": false,
		"custom_domain": "taslak.example.com"}, &draft, 201)
	anon := pe.anon()
	cases := []struct {
		path, host string
		code       int
	}{
		{"/durum/genel", "", 200},
		{"/durum/GENEL/", "", 200},
		{"/durum/yok", "", 404},
		{"/durum/taslak", "", 404},
		{"/durum/Geçersiz!", "", 404},
		{"/", "", 200},
		{"/", "taslak.example.com", 404},
		{"/", "baska.example.com", 200}, // eşleşmeyen alan adı: yönetim arayüzü
	}
	for _, c := range cases {
		resp, body := anon.raw("GET", c.path, c.host, "", nil)
		if resp.StatusCode != c.code || !strings.Contains(string(body), "<title>Uptime") ||
			!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			t.Errorf("%s (%s): %d %q, %d ve arayüz HTML'i bekleniyordu", c.path, c.host, resp.StatusCode, resp.Header.Get("Content-Type"), c.code)
		}
	}
	if resp, body := anon.raw("HEAD", "/durum/yok", "", "", nil); resp.StatusCode != 404 || len(body) != 0 {
		t.Errorf("HEAD: %d %d bayt", resp.StatusCode, len(body))
	}
	// Taslak yayına alınınca 200.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", draft.ID), map[string]any{"slug": "taslak", "title": "Taslak", "published": true}, nil, 200)
	if resp, _ := anon.raw("GET", "/durum/taslak", "", "", nil); resp.StatusCode != 200 {
		t.Errorf("yayındaki sayfa: %d", resp.StatusCode)
	}
}

// Bulgu: geçersiz metrik hatası geçerli "net" metriğini saymıyordu.
func TestValidateAlertsMessageListsNet(t *testing.T) {
	if _, err := validateAlerts([]alertInput{{Metric: "net", Threshold: 100, Minutes: 5}}); err != nil {
		t.Fatalf("net geçerli: %v", err)
	}
	_, err := validateAlerts([]alertInput{{Metric: "bogus", Minutes: 1}})
	if err == nil || !strings.Contains(err.Error(), "temp, net, offline, container veya reboot") {
		t.Fatalf("hata mesajı: %v", err)
	}
	if en := i18n.Error(i18n.EN, err.Error()); !strings.Contains(en, "temp, net, offline, container or reboot") {
		t.Fatalf("İngilizce hata: %q", en)
	}
}
