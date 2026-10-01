package api

import (
	"strconv"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func allKinds() []string { return notify.Kinds }

// Kanal kuralları API'de gidiş-dönüş yapar; eski istemcinin kuralsız
// gövdesi tüm türleri alan, kuralsız kanal üretir; geçersiz değerler 400.
func TestNotificationRulesAPI(t *testing.T) {
	e := setupAdmin(t)
	// Eski istemci: kural alanları yok.
	var legacy store.Notification
	e.mustDo("POST", "/api/notifications", map[string]any{"name": "Eski", "type": "webhook",
		"config": map[string]any{"url": "https://example.com/hook"}}, &legacy, 201)
	if len(legacy.Events) != 0 || legacy.Quiet != nil || legacy.DelayMin != 0 || legacy.EscalateMin != 0 || legacy.Lang != "" {
		t.Fatalf("kuralsız kanal bekleniyordu: %+v", legacy)
	}

	body := map[string]any{"name": "Nöbet", "type": "webhook", "config": map[string]any{"url": "https://example.com/hook"},
		"events": []string{"down", "up", "down"}, "quiet_hours": map[string]any{"start": "22:00", "end": "07:00", "tz": "Europe/Istanbul", "mode": "none"},
		"delay_min": 5, "escalate_min": 30, "lang": "en"}
	var ch store.Notification
	e.mustDo("POST", "/api/notifications", body, &ch, 201)
	if len(ch.Events) != 2 || ch.Quiet == nil || ch.Quiet.Mode != store.QuietNone || ch.DelayMin != 5 || ch.EscalateMin != 30 || ch.Lang != "en" {
		t.Fatalf("kurallar kaydedilmedi: %+v", ch)
	}
	var list []store.Notification
	e.mustDo("GET", "/api/notifications", nil, &list, 200)
	var got store.Notification
	for _, n := range list {
		if n.ID == ch.ID {
			got = n
		}
	}
	if got.Quiet == nil || got.Quiet.Start != "22:00" || len(got.Events) != 2 {
		t.Fatalf("liste kuralları taşımıyor: %+v", got)
	}
	// GET → PUT gidiş-dönüş: aynı gövde değişiklik yaratmaz.
	body["events"], body["delay_min"] = got.Events, got.DelayMin
	body["apply_existing"] = false
	e.mustDo("PUT", "/api/notifications/"+itoa(ch.ID), body, &ch, 200)
	if ch.DelayMin != 5 || ch.Quiet == nil {
		t.Fatalf("güncelleme kuralları bozdu: %+v", ch)
	}
	// Tüm türler seçiliyse süzgeç kapanır (yeni türler de gelsin).
	all := append([]string{}, allKinds()...)
	body["events"] = all
	e.mustDo("PUT", "/api/notifications/"+itoa(ch.ID), body, &ch, 200)
	if len(ch.Events) != 0 {
		t.Fatalf("tüm türler seçiliyken süzgeç boş olmalı: %v", ch.Events)
	}
	// Geçersizler.
	bad := []map[string]any{
		{"events": []string{"yok"}},
		{"quiet_hours": map[string]any{"start": "25:00", "end": "07:00"}},
		{"quiet_hours": map[string]any{"start": "22:00", "end": "22:00"}},
		{"quiet_hours": map[string]any{"start": "22:00", "end": "07:00", "tz": "Mars/Olympus"}},
		{"quiet_hours": map[string]any{"start": "22:00", "end": "07:00", "mode": "maybe"}},
		{"delay_min": 2000},
		{"escalate_min": -1},
		{"lang": "de"},
	}
	for _, b := range bad {
		req := map[string]any{"name": "x", "type": "webhook", "config": map[string]any{"url": "https://example.com/hook"}}
		for k, v := range b {
			req[k] = v
		}
		if code := e.do("POST", "/api/notifications", req, nil); code != 400 {
			t.Errorf("%v: %d, 400 bekleniyordu", b, code)
		}
	}
	// Test bildirimi kanal dilini kabul eder (gönderim başarısız olsa da dil doğrulanır).
	if code := e.do("POST", "/api/notifications/test", map[string]any{"type": "webhook", "config": map[string]any{"url": "https://example.com/hook"}, "lang": "xx"}, nil); code != 400 {
		t.Errorf("geçersiz test dili: %d", code)
	}
}
