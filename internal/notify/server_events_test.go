package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var serverAlert = Event{
	Kind: KindServerAlert, ProbeID: 7, MonitorName: "CP Server İstanbul", MonitorType: "server", Target: "cp1",
	Metric: "cpu", Value: 94.4, Threshold: 90, Minutes: 10,
	Time: time.Date(2026, 9, 27, 2, 20, 0, 0, time.UTC), URL: "https://uptime.test/#/servers/7",
}

func TestServerEventTexts(t *testing.T) {
	resolved := serverAlert
	resolved.Kind, resolved.Value = KindServerResolved, 40
	offline := serverAlert
	offline.Metric, offline.Message = "offline", "Son veri: 27.09.2026 05:10:00"
	back := offline
	back.Kind = KindServerResolved
	load := serverAlert
	load.Metric, load.Value, load.Threshold, load.Minutes = "load", 2.347, 1.5, 5
	temp := serverAlert
	temp.Metric, temp.Value, temp.Threshold = "temp", 81.6, 80

	cases := map[string]Event{
		"🔴 CP Server İstanbul: CPU %94 (10 dk ortalama, eşik %90)":                   serverAlert,
		"🟢 CP Server İstanbul: CPU normale döndü":                                    resolved,
		"🔴 CP Server İstanbul: sunucuya ulaşılamıyor":                                offline,
		"🟢 CP Server İstanbul: tekrar veri gönderiyor":                               back,
		"🔴 CP Server İstanbul: Yük 2,35 (5 dk ortalama, çekirdek başına, eşik 1,50)": load,
		"🔴 CP Server İstanbul: Sıcaklık 82 °C (10 dk ortalama, eşik 80 °C)":          temp,
	}
	for want, ev := range cases {
		if got := ev.Title(); got != want {
			t.Errorf("başlık: %q, %q bekleniyordu", got, want)
		}
	}
	if !serverAlert.IsProblem() || resolved.IsProblem() || !resolved.IsRecovery() || serverAlert.IsRecovery() {
		t.Error("sorun/düzelme ayrımı")
	}
	if txt := offline.Text(); !strings.Contains(txt, "Sunucu: cp1") || !strings.Contains(txt, "Ayrıntı: Son veri") || strings.Contains(txt, "Hedef") {
		t.Errorf("metin: %s", txt)
	}
	if txt := resolved.Text(); !strings.Contains(txt, "Son ortalama: %40") {
		t.Errorf("bitiş metni: %s", txt)
	}
	if serverAlert.AlertKey() != "uptime-server-7-cpu" || offline.AlertKey() != "uptime-server-7-offline" {
		t.Errorf("anahtar: %s %s", serverAlert.AlertKey(), offline.AlertKey())
	}
}

func TestServerEventProviders(t *testing.T) {
	c, srv := newCapture(t)
	if err := send(t, "webhook", map[string]any{"url": srv.URL}, serverAlert); err != nil {
		t.Fatal(err)
	}
	var p WebhookPayload
	json.Unmarshal([]byte(c.body), &p)
	if p.Event != "server_alert" || p.Server == nil || p.Server.ID != 7 || p.Server.Metric != "cpu" || p.Server.Value != 94.4 ||
		p.Monitor.Name != "CP Server İstanbul" || p.Monitor.Type != "server" {
		t.Errorf("webhook: %s", c.body)
	}

	old := pagerdutyAPI
	pagerdutyAPI = srv.URL
	defer func() { pagerdutyAPI = old }()
	resolved := serverAlert
	resolved.Kind = KindServerResolved
	for _, ev := range []Event{serverAlert, resolved} {
		if err := send(t, "pagerduty", map[string]any{"routing_key": "rk_x"}, ev); err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		json.Unmarshal([]byte(c.body), &body)
		want := map[string]string{KindServerAlert: "trigger", KindServerResolved: "resolve"}[ev.Kind]
		if body["event_action"] != want || body["dedup_key"] != "uptime-server-7-cpu" {
			t.Errorf("pagerduty %s: %v", ev.Kind, body)
		}
	}

	oldOG := opsgenieAPIUS
	opsgenieAPIUS = srv.URL
	defer func() { opsgenieAPIUS = oldOG }()
	if err := send(t, "opsgenie", map[string]any{"api_key": "key_x"}, resolved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(c.path, "/v2/alerts/uptime-server-7-cpu/close") {
		t.Errorf("opsgenie kapatma: %s", c.path)
	}
	if appriseType(serverAlert) != "failure" || appriseType(resolved) != "success" {
		t.Error("apprise türü")
	}
}
