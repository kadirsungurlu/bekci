package api

import (
	"fmt"
	"strings"
	"testing"
)

// strictEnv zaman aşımı kuralı açık test sunucusu (üretim davranışı).
func strictEnv(t *testing.T) *env {
	e := newEnv(t, func(s *Server) { s.relaxTimeoutRule = false })
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return e
}

// Bulgu (D-3): zaman aşımı kontrol aralığına eşit veya uzun kabul ediliyordu
// (kontrol bir sonrakine kadar bitmezdi). Bulgu (D-2): push monitöründe ters
// mod kabul ediliyordu (alarm hiç gelmezdi).
func TestMonitorTimeoutAndUpsideDownRules(t *testing.T) {
	e := strictEnv(t)
	var errResp struct {
		Error string `json:"error"`
	}
	code := e.do("POST", "/api/monitors", map[string]any{
		"name": "Yavaş", "type": "http", "interval": 20, "timeout": 300, "config": map[string]any{"url": "https://example.com"},
	}, &errResp)
	if code != 400 || !strings.Contains(errResp.Error, "Zaman aşımı kontrol aralığından") {
		t.Fatalf("zaman aşımı > aralık reddedilmeli: %d %q", code, errResp.Error)
	}
	// Tekrar deneme aralığından uzun zaman aşımı serbest (kontroller sırayla).
	code = e.do("POST", "/api/monitors", map[string]any{
		"name": "Yavaş", "type": "http", "interval": 120, "retry_interval": 20, "timeout": 30, "config": map[string]any{"url": "https://example.com"},
	}, nil)
	if code != 201 {
		t.Fatalf("zaman aşımı > tekrar deneme aralığı kabul edilmeli: %d", code)
	}
	// Aralığa eşit zaman aşımı serbest.
	code = e.do("POST", "/api/monitors", map[string]any{
		"name": "Eşit", "type": "http", "interval": 30, "timeout": 30, "config": map[string]any{"url": "https://example.com"},
	}, nil)
	if code != 201 {
		t.Fatalf("zaman aşımı = aralık kabul edilmeli: %d", code)
	}
	// Varsayılan zaman aşımı aralığın yarısını geçmez.
	var m monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Hızlı", "type": "http", "interval": 20, "config": map[string]any{"url": "https://example.com"},
	}, &m, 201)
	if m.Timeout != 10 {
		t.Fatalf("varsayılan zaman aşımı min(30, aralık/2) olmalı: %d", m.Timeout)
	}
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Normal", "type": "http", "interval": 300, "config": map[string]any{"url": "https://example.com"},
	}, &m, 201)
	if m.Timeout != 30 {
		t.Fatalf("varsayılan zaman aşımı 30 olmalı: %d", m.Timeout)
	}
	// Push ve grup zaman aşımı kullanmaz: kural uygulanmaz.
	e.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Yedek", "type": "push", "interval": 20, "timeout": 300, "config": map[string]any{},
	}, &m, 201)
	code = e.do("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), map[string]any{
		"name": "Yedek", "type": "push", "interval": 20, "upside_down": true, "config": map[string]any{},
	}, &errResp)
	if code != 400 || !strings.Contains(errResp.Error, "ters mod") {
		t.Fatalf("push + ters mod reddedilmeli: %d %q", code, errResp.Error)
	}
}
