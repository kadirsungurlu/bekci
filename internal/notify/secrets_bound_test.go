package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Webhook adresinin kendisi gizli olan kanallar (Discord, Slack, Teams...):
// arayüz adresi maskeli geri gönderir; ad değişikliği ve "Test gönder"
// hedef değişikliği sayılmamalı.
func TestMergeSecretsMaskedWebhookURL(t *testing.T) {
	for _, typ := range []string{"discord", "slack", "teams", "googlechat", "mattermost", "rocketchat"} {
		stored := json.RawMessage(`{"webhook_url":"https://hooks.example.com/1/gizli"}`)
		masked := MaskSecrets(typ, stored)
		if strings.Contains(string(masked), "gizli") {
			t.Fatalf("%s: maskelenmedi: %s", typ, masked)
		}
		merged, err := MergeSecrets(typ, masked, stored)
		if err != nil {
			t.Fatalf("%s: maskeli adresle kaydetme reddedildi: %v", typ, err)
		}
		var m map[string]any
		json.Unmarshal(merged, &m)
		if m["webhook_url"] != "https://hooks.example.com/1/gizli" {
			t.Errorf("%s: eski adres korunmadı: %s", typ, merged)
		}
		// Yeni adres açıkça girilirse kabul edilir (gizli bilgi tamamen yenilenir).
		if _, err := MergeSecrets(typ, json.RawMessage(`{"webhook_url":"https://hooks.example.com/2/yeni"}`), stored); err != nil {
			t.Errorf("%s: yeni adres reddedildi: %v", typ, err)
		}
	}
	// Adres gizli olsa da başka bir gizli alan maskeli kalırken adres
	// değiştirilirse ret sürer.
	stored2 := json.RawMessage(`{"webhook_url":"https://a.example/x","token":"t"}`)
	if _, err := MergeSecretsFor([]string{"webhook_url", "token"}, json.RawMessage(`{"webhook_url":"https://saldirgan.example/y","token":"`+Mask+`"}`), stored2); err != ErrSecretRebind {
		t.Errorf("adres değişip token maskeliyken ret bekleniyordu: %v", err)
	}
	// Gizli olmayan bir hedef alanına maske yazmak korumayı atlatmaz.
	if _, err := MergeSecretsFor([]string{"token"}, json.RawMessage(`{"url":"`+Mask+`","token":"`+Mask+`"}`), json.RawMessage(`{"url":"https://a.example","token":"t"}`)); err != ErrSecretRebind {
		t.Errorf("gizli olmayan hedefte maske kabul edilmemeli: %v", err)
	}
}

func TestMergeSecretsBound(t *testing.T) {
	stored := json.RawMessage(`{"host":"db","port":5432,"username":"izleme","password":"p","query":"SELECT 1","expected":"","db":0,"tls":false}`)
	bound := []string{"username", "query", "expected", "db", "tls"}
	m := Mask
	cases := []struct {
		cfg string
		err error
	}{
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1"}`, nil},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1","db":0,"tls":false,"expected":""}`, nil},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT secret FROM t"}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"postgres","password":"` + m + `","query":"SELECT 1"}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1","db":3}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1","tls":true}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"select 1"}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1","expected":"x"}`, ErrSecretBound},
		{`{"host":"db","port":5432,"username":"izleme","password":"yeni","query":"SELECT secret FROM t"}`, nil},
		{`{"host":"evil","port":5432,"username":"izleme","password":"` + m + `","query":"SELECT 1"}`, ErrSecretRebind},
	}
	for _, c := range cases {
		_, err := MergeSecretsBound([]string{"password"}, bound, nil, json.RawMessage(c.cfg), stored)
		if err != c.err {
			t.Errorf("%s: hata=%v, beklenen=%v", c.cfg, err, c.err)
		}
	}
	// normalize verilince eksik alan varsayılanıyla karşılaştırılır.
	norm := func(c json.RawMessage) (json.RawMessage, error) {
		var v map[string]any
		json.Unmarshal(c, &v)
		if v["method"] == nil {
			v["method"] = "GET"
		}
		return encode(v), nil
	}
	storedHTTP := json.RawMessage(`{"url":"https://a","method":"GET","basic_pass":"p"}`)
	if _, err := MergeSecretsBound([]string{"basic_pass"}, []string{"method"}, norm, json.RawMessage(`{"url":"https://a","basic_pass":"`+m+`"}`), storedHTTP); err != nil {
		t.Errorf("varsayılan yöntem değişiklik sayılmamalı: %v", err)
	}
	if _, err := MergeSecretsBound([]string{"basic_pass"}, []string{"method"}, norm, json.RawMessage(`{"url":"https://a","method":"DELETE","basic_pass":"`+m+`"}`), storedHTTP); err != ErrSecretBound {
		t.Errorf("yöntem değişikliği reddedilmeli: %v", err)
	}
}

func TestAlertKeySampleAndTest(t *testing.T) {
	real := Event{Kind: KindDown, MonitorID: 5}
	sample := Event{Kind: KindDown, MonitorID: 5, Sample: true}
	sampleUp := Event{Kind: KindUp, MonitorID: 5, Sample: true}
	sampleSrv := Event{Kind: KindServerAlert, ProbeID: 7, Metric: "cpu", Sample: true}
	test := Event{Kind: KindTest}
	if real.AlertKey() != "uptime-monitor-5" {
		t.Errorf("gerçek anahtar: %s", real.AlertKey())
	}
	if sample.AlertKey() != "uptime-sample-monitor-5" {
		t.Errorf("örnek anahtarı gerçek olaydan ayrılmalı: %s", sample.AlertKey())
	}
	if sampleUp.AlertKey() != sample.AlertKey() {
		t.Errorf("örnek başlangıç/bitiş eşleşmeli: %s %s", sampleUp.AlertKey(), sample.AlertKey())
	}
	if sampleSrv.AlertKey() != "uptime-sample-server-7-cpu" {
		t.Errorf("örnek sunucu anahtarı: %s", sampleSrv.AlertKey())
	}
	if test.AlertKey() != "uptime-test" {
		t.Errorf("test anahtarı: %s", test.AlertKey())
	}
	for _, ev := range SampleEvents(SampleNames{MonitorID: 5, ServerID: 7}, time.Now(), "tr") {
		if !strings.HasPrefix(ev.AlertKey(), "uptime-sample-") {
			t.Errorf("%s örneği gerçek anahtar taşıyor: %s", ev.Kind, ev.AlertKey())
		}
	}
}
