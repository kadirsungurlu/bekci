package api

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Manuel olay: editör açar, güncelleme yazar, çözer; izleyici yalnızca okur;
// herkese açık sayfa ve RSS başlık/önem/güncellemeleri taşır; planlı bakım
// bloğu sayfadaki monitörleri etkileyen pencereleri verir.
func TestManualIncidentsAndMaintenanceBlock(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://a.example.com")
	b := pe.seedMonitor("B", "https://b.example.com")
	now := pe.s.now().Unix()
	pe.beat(a.ID, now, store.StatusUp)
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "acme", "title": "Acme",
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID, "name": "Site"}}}}}, &p, 201)

	editor, _ := pe.newUser("editor1", store.RoleEditor, nil)
	viewer, _ := pe.newUser("viewer1", store.RoleViewer, nil)
	customer, _ := pe.newUser("musteri", store.RoleViewer, []int64{b.ID})

	// Doğrulamalar.
	viewer.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": "x"}, nil, 403)
	editor.mustDo("POST", "/api/incidents", map[string]any{"title": "Sayfasız"}, nil, 400)
	editor.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": ""}, nil, 400)
	editor.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": "x", "severity": "huge"}, nil, 400)
	editor.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": "x", "state": "done"}, nil, 400)
	editor.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": "x", "monitor_ids": []int64{9999}}, nil, 400)

	var inc store.Incident
	editor.mustDo("POST", "/api/incidents", map[string]any{"page_id": p.ID, "title": "Veritabanı yavaş", "severity": "major",
		"monitor_ids": []int64{a.ID}, "body": "İnceliyoruz."}, &inc, 201)
	if inc.Kind != store.IncidentManual || inc.Title != "Veritabanı yavaş" || inc.State != store.StateInvestigating || inc.ResolvedAt != 0 || inc.PageID != p.ID || inc.PageTitle != "Acme" || inc.CreatedBy != "editor1" {
		t.Fatalf("manuel olay: %+v", inc)
	}
	// Liste: manuel süzgeci ve müşteri kısıtlı izleyici de görür.
	var list []store.Incident
	customer.mustDo("GET", "/api/incidents?kind=manual", nil, &list, 200)
	if len(list) != 1 || list[0].ID != inc.ID {
		t.Fatalf("müşteri manuel olayı görmeli: %+v", list)
	}
	// Ayrıntı: güncellemeler ve etkilenen monitörler; izleyicide kullanıcı adı yok.
	var det struct {
		Incident store.Incident         `json:"incident"`
		Updates  []store.IncidentUpdate `json:"updates"`
		Affected []incidentAffected     `json:"affected"`
	}
	viewer.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &det, 200)
	if len(det.Updates) != 1 || det.Updates[0].Body != "İnceliyoruz." || det.Updates[0].Username != "" || len(det.Affected) != 1 || det.Affected[0].Name != "A" {
		t.Fatalf("izleyici ayrıntısı: %+v", det)
	}
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &det, 200)
	if det.Updates[0].Username != "editor1" {
		t.Fatalf("editör kullanıcı adını görmeli: %+v", det.Updates)
	}

	// Güncelleme: identified → monitoring → resolved (olay kapanır).
	pe.advance(5 * time.Minute)
	viewer.mustDo("POST", fmt.Sprintf("/api/incidents/%d/updates", inc.ID), map[string]any{"state": "identified"}, nil, 403)
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/updates", inc.ID), map[string]any{"state": "identified", "body": "Disk doldu."}, nil, 201)
	pe.advance(5 * time.Minute)
	var upd store.IncidentUpdate
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/updates", inc.ID), map[string]any{"state": "resolved", "body": "Temizlendi."}, &upd, 201)
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &det, 200)
	if det.Incident.ResolvedAt == 0 || det.Incident.State != store.StateResolved || len(det.Updates) != 3 {
		t.Fatalf("çözüldü aşaması olayı kapatmalı: %+v", det.Incident)
	}
	// Güncelleme silinince aşama bir öncekine döner ve olay yeniden açılır.
	editor.mustDo("DELETE", fmt.Sprintf("/api/incidents/%d/updates/%d", inc.ID, upd.ID), nil, nil, 200)
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &det, 200)
	if det.Incident.ResolvedAt != 0 || det.Incident.State != store.StateIdentified || len(det.Updates) != 2 {
		t.Fatalf("güncelleme silinince: %+v", det.Incident)
	}

	// Herkese açık sayfa: manuel olay başlık/önem/güncellemelerle; otomatik olay da gelir.
	auto, _ := pe.s.store.StartIncident(context.Background(), a.ID, now-3600, "HTTP 503")
	pe.s.store.ResolveIncident(context.Background(), a.ID, now-1800)
	pe.s.pagesChanged(context.Background())
	var pub struct {
		Incidents   []publicIncident    `json:"incidents"`
		Maintenance []publicMaintenance `json:"maintenance"`
	}
	pe.anon().mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	if len(pub.Incidents) != 2 {
		t.Fatalf("iki olay bekleniyordu: %+v", pub.Incidents)
	}
	var man, mon *publicIncident
	for i := range pub.Incidents {
		if pub.Incidents[i].Kind == store.IncidentManual {
			man = &pub.Incidents[i]
		} else {
			mon = &pub.Incidents[i]
		}
	}
	if man == nil || man.Title != "Veritabanı yavaş" || man.Severity != "major" || man.State != "identified" || len(man.Updates) != 2 || len(man.Monitors) != 1 || man.Monitors[0] != "Site" {
		t.Fatalf("herkese açık manuel olay: %+v", man)
	}
	if mon == nil || mon.ID != auto || mon.Monitor != "Site" || mon.Kind != store.IncidentMonitor {
		t.Fatalf("herkese açık otomatik olay: %+v", mon)
	}

	// Otomatik olaya güncelleme: açıkken "resolved" yazılamaz, diğer aşamalar yazılır ve sayfada görünür.
	open, _ := pe.s.store.StartIncident(context.Background(), a.ID, now-60, "HTTP 500")
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/updates", open), map[string]any{"state": "resolved"}, nil, 400)
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/updates", open), map[string]any{"state": "identified", "body": "Dağıtım geri alınıyor."}, nil, 201)
	var autoDet struct {
		Incident store.Incident         `json:"incident"`
		Updates  []store.IncidentUpdate `json:"updates"`
	}
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", open), nil, &autoDet, 200)
	if autoDet.Incident.ResolvedAt != 0 || autoDet.Incident.State != "identified" || len(autoDet.Updates) != 1 {
		t.Fatalf("otomatik olayın güncellemesi: %+v %+v", autoDet.Incident, autoDet.Updates)
	}
	pe.anon().mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	found := false
	for _, pi := range pub.Incidents {
		if pi.ID == open && len(pi.Updates) == 1 && pi.Updates[0].Body == "Dağıtım geri alınıyor." {
			found = true
		}
	}
	if !found {
		t.Fatalf("otomatik olayın güncellemesi sayfada yok: %+v", pub.Incidents)
	}

	// Manuel olayı düzenle ve sil; otomatik olay silinemez.
	editor.mustDo("PUT", fmt.Sprintf("/api/incidents/%d", inc.ID), map[string]any{"title": "Veritabanı", "severity": "critical", "monitor_ids": []int64{}}, &inc, 200)
	if inc.Title != "Veritabanı" || inc.Severity != "critical" {
		t.Fatalf("düzenleme: %+v", inc)
	}
	editor.mustDo("DELETE", fmt.Sprintf("/api/incidents/%d", open), nil, nil, 400)

	// RSS: manuel olay, güncelleme ve planlı bakım kayıtları.
	pe.mustDo("POST", "/api/maintenance", map[string]any{"title": "Disk değişimi", "strategy": "once", "timezone": "UTC",
		"start": time.Unix(now, 0).UTC().Add(2 * time.Hour).Format("2006-01-02T15:04"),
		"end":   time.Unix(now, 0).UTC().Add(3 * time.Hour).Format("2006-01-02T15:04"), "monitor_ids": []int64{a.ID}}, nil, 201)
	pe.mustDo("POST", "/api/maintenance", map[string]any{"title": "Başka sunucu", "strategy": "once", "timezone": "UTC",
		"start": time.Unix(now, 0).UTC().Add(2 * time.Hour).Format("2006-01-02T15:04"),
		"end":   time.Unix(now, 0).UTC().Add(3 * time.Hour).Format("2006-01-02T15:04"), "monitor_ids": []int64{b.ID}}, nil, 201)
	pe.mustDo("POST", "/api/maintenance", map[string]any{"title": "Uzak gelecek", "strategy": "once", "timezone": "UTC",
		"start": time.Unix(now, 0).UTC().Add(30 * 24 * time.Hour).Format("2006-01-02T15:04"),
		"end":   time.Unix(now, 0).UTC().Add(31 * 24 * time.Hour).Format("2006-01-02T15:04"), "all_monitors": true}, nil, 201)
	pe.anon().mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	if len(pub.Maintenance) != 1 || pub.Maintenance[0].Title != "Disk değişimi" || pub.Maintenance[0].Ongoing || len(pub.Maintenance[0].Monitors) != 1 || pub.Maintenance[0].Monitors[0] != "Site" {
		t.Fatalf("bakım bloğu: %+v", pub.Maintenance)
	}
	resp, body := pe.anon().raw("GET", "/durum/acme/feed.xml", "", "", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("akış: %d %s", resp.StatusCode, body)
	}
	feed := string(body)
	for _, want := range []string{"Veritabanı", "Olay güncellemesi", "Dağıtım geri alınıyor.", "Planlı bakım: Disk değişimi", "Önem: Kritik"} {
		if !strings.Contains(feed, want) {
			t.Errorf("akışta %q yok:\n%s", want, feed)
		}
	}
	if strings.Contains(feed, "Başka sunucu") || strings.Contains(feed, "Uzak gelecek") {
		t.Errorf("akışta sayfayla ilgisiz / uzak bakım olmamalı")
	}

	// Bakım bloğu gizlenince sayfa ve akış bakımı taşımaz.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "acme", "title": "Acme", "sections": p.Sections,
		"layout": map[string]any{"blocks": []map[string]any{{"id": "maintenance", "visible": false}}}}, nil, 200)
	pe.anon().mustDo("GET", "/api/public/pages/acme", nil, &pub, 200)
	if len(pub.Maintenance) != 0 {
		t.Fatalf("gizli bakım bloğu veri taşımamalı: %+v", pub.Maintenance)
	}

	// Silme ve süzgeçler.
	editor.mustDo("DELETE", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, nil, 200)
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, nil, 404)
	pe.mustDo("GET", fmt.Sprintf("/api/incidents?monitor_id=%d&q=500", a.ID), nil, &list, 200)
	if len(list) != 1 || list[0].ID != open {
		t.Fatalf("süzgeç monitor_id+q: %+v", list)
	}
	pe.mustDo("GET", fmt.Sprintf("/api/incidents?from=%d&to=%d", now-7200, now-3000), nil, &list, 200)
	if len(list) != 1 || list[0].ID != auto {
		t.Fatalf("süzgeç from/to: %+v", list)
	}
	pe.mustDo("GET", "/api/incidents?q=yok-boyle-bir-sey", nil, &list, 200)
	if len(list) != 0 {
		t.Fatalf("eşleşmeyen metin: %+v", list)
	}
}
