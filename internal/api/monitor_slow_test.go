package api

import (
	"fmt"
	"testing"
)

// TestMonitorSlowFields: yavaş yanıt eşiği ve penceresi POST/PUT ile yazılır,
// GET'te geri gelir (GET→PUT döngüsü değeri korur); pencere verilmezse
// varsayılan 3; sınır dışı değerler reddedilir; eski arayüz göndermezse kapalı.
func TestMonitorSlowFields(t *testing.T) {
	admin := setupAdmin(t)
	var m monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "push", "interval": 5000, "config": map[string]any{}, "slow_ms": 2000,
	}, &m, 201)
	if m.SlowMs != 2000 || m.SlowChecks != 3 || m.Slow || m.OpenDegradedIncidentID != nil {
		t.Fatalf("eşik kaydedilmeli, pencere varsayılan 3: %+v", m.Monitor)
	}
	var list []monitorView
	admin.mustDo("GET", "/api/monitors", nil, &list, 200)
	if len(list) != 1 || list[0].SlowMs != 2000 || list[0].SlowChecks != 3 {
		t.Fatalf("listede eşik yok: %+v", list)
	}
	// GET → PUT döngüsü: değerler korunur, pencere değişir.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{
		"name": "Site", "type": "push", "interval": 5000, "config": map[string]any{}, "slow_ms": list[0].SlowMs, "slow_checks": 5,
	}, &m, 200)
	if m.SlowMs != 2000 || m.SlowChecks != 5 {
		t.Fatalf("PUT sonrası: %+v", m.Monitor)
	}
	// Eski arayüz alanları göndermez: eşik kapanır (0), pencere varsayılana döner.
	admin.mustDo("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{
		"name": "Site", "type": "push", "interval": 5000, "config": map[string]any{},
	}, &m, 200)
	if m.SlowMs != 0 || m.SlowChecks != 3 {
		t.Fatalf("alan gönderilmeyince kapalı olmalı: %+v", m.Monitor)
	}
	var errResp struct {
		Error string `json:"error"`
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{
		"name": "Site", "type": "push", "interval": 5000, "config": map[string]any{}, "slow_ms": 700000,
	}, &errResp); code != 400 {
		t.Errorf("sınır dışı eşik reddedilmeli: %d %q", code, errResp.Error)
	}
	if code := admin.do("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{
		"name": "Site", "type": "push", "interval": 5000, "config": map[string]any{}, "slow_ms": 1000, "slow_checks": 500,
	}, &errResp); code != 400 {
		t.Errorf("sınır dışı pencere reddedilmeli: %d %q", code, errResp.Error)
	}
}
