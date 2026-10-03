package api

import (
	"fmt"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Onay / susturma API'si: editör onaylar, izleyici yapamaz, kapanmış olay 409,
// onay işlem geçmişine ve işlem kaydına yazılır.
func TestIncidentAckAPI(t *testing.T) {
	admin := setupAdmin(t)
	m := admin.push("Yedek işi")
	admin.mustDo("GET", "/api/push/"+m.PushToken+"?status=down&msg=hata", nil, nil, 200)
	var list []store.Incident
	waitFor(t, "olay açıldı", func() bool {
		admin.mustDo("GET", "/api/incidents?open=1", nil, &list, 200)
		return len(list) == 1
	})
	inc := list[0]
	if inc.AckedAt != 0 || inc.SnoozedUntil != 0 {
		t.Fatalf("yeni olay onaysız olmalı: %+v", inc)
	}

	viewer, _ := admin.newUser("izleyici", "viewer", nil)
	viewer.mustDo("POST", fmt.Sprintf("/api/incidents/%d/ack", inc.ID), map[string]string{"note": ""}, nil, 403)

	editor, _ := admin.newUser("editor", "editor", nil)
	var out store.Incident
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/ack", inc.ID), map[string]string{"note": "bakıyoruz"}, &out, 200)
	if out.AckedAt == 0 || out.AckedBy != "editor" || out.AckNote != "bakıyoruz" {
		t.Fatalf("onay alanları: %+v", out)
	}
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/snooze", inc.ID), map[string]int{"minutes": 0}, nil, 400)
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/snooze", inc.ID), map[string]int{"minutes": 60}, &out, 200)
	if out.SnoozedUntil == 0 {
		t.Fatalf("susturma yazılmadı: %+v", out)
	}
	// Ayrıntı: işlem geçmişinde ack ve snooze kayıtları; liste de alanları taşır.
	var detail incidentResp
	editor.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &detail, 200)
	if !detail.has(store.EventAck) || !detail.has(store.EventSnooze) || detail.Incident.AckedBy != "editor" {
		t.Fatalf("işlem geçmişi: %v", detail.kinds())
	}
	// omitempty alanlar JSON'da gelmez: her adımda temiz değişken.
	var afterSnooze store.Incident
	editor.mustDo("DELETE", fmt.Sprintf("/api/incidents/%d/snooze", inc.ID), nil, &afterSnooze, 200)
	if afterSnooze.SnoozedUntil != 0 {
		t.Fatalf("susturma kalkmalıydı: %+v", afterSnooze)
	}
	var afterUnack store.Incident
	editor.mustDo("DELETE", fmt.Sprintf("/api/incidents/%d/ack", inc.ID), nil, &afterUnack, 200)
	if afterUnack.AckedAt != 0 || afterUnack.AckedBy != "" {
		t.Fatalf("onay kalkmalıydı: %+v", afterUnack)
	}
	// İzleyici onay bilgisini okur (düğme yok ama alanlar görünür).
	viewer.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &detail, 200)

	// Olay kapanınca onaylanamaz.
	admin.mustDo("GET", "/api/push/"+m.PushToken+"?status=up", nil, nil, 200)
	waitFor(t, "olay kapandı", func() bool {
		var d incidentResp
		admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &d, 200)
		return d.Incident.ResolvedAt != 0
	})
	editor.mustDo("POST", fmt.Sprintf("/api/incidents/%d/ack", inc.ID), map[string]string{"note": ""}, nil, 409)
	editor.mustDo("POST", "/api/incidents/999999/ack", map[string]string{"note": ""}, nil, 404)

	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit?action=incident.", nil, &audit, 200)
	seen := map[string]bool{}
	for _, a := range audit {
		seen[a.Action] = true
	}
	for _, want := range []string{"incident.ack", "incident.unack", "incident.snooze", "incident.unsnooze"} {
		if !seen[want] {
			t.Fatalf("işlem kaydında %s yok: %v", want, seen)
		}
	}
}
