package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/check"
)

// FakeUptimeRobot getMonitors uç noktasını taklit eder (sayfalama dahil).
func fakeUptimeRobot(t *testing.T, key string, monitors []map[string]any) (*httptest.Server, *atomic.Int32) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		if r.Method != http.MethodPost || r.URL.Path != "/v2/getMonitors" || r.PostForm.Get("format") != "json" {
			w.WriteHeader(400)
			return
		}
		if r.PostForm.Get("api_key") != key {
			fmt.Fprint(w, `{"stat":"fail","error":{"type":"invalid_parameter","parameter_name":"api_key","passed_value":"x","message":"api_key is invalid."}}`)
			return
		}
		off, _ := strconv.Atoi(r.PostForm.Get("offset"))
		lim, _ := strconv.Atoi(r.PostForm.Get("limit"))
		end := min(off+lim, len(monitors))
		page := monitors[min(off, len(monitors)):end]
		json.NewEncoder(w).Encode(map[string]any{"stat": "ok",
			"pagination": map[string]any{"offset": off, "limit": lim, "total": len(monitors)}, "monitors": page})
	}))
	t.Cleanup(srv.Close)
	old := UptimeRobotAPI
	UptimeRobotAPI = srv.URL + "/v2"
	t.Cleanup(func() { UptimeRobotAPI = old })
	return srv, &calls
}

// URSample gerçek API yanıtındaki alan biçimleriyle örnek monitörler.
func URSample() []map[string]any {
	return []map[string]any{
		{"id": 101, "friendly_name": "Ana site", "url": "https://kadir.app", "type": 1, "sub_type": "", "keyword_type": nil,
			"keyword_case_type": nil, "keyword_value": "", "http_username": "", "http_password": "", "port": "", "interval": 300,
			"timeout": 30, "status": 2, "custom_http_headers": map[string]any{"X-Kaynak": "uptimerobot"}},
		{"id": 102, "friendly_name": "Durum sayfası", "url": "https://kadir.app/durum", "type": 2, "sub_type": "", "keyword_type": 1,
			"keyword_case_type": 0, "keyword_value": "Hata", "http_username": "u", "http_password": "p", "port": "", "interval": 60,
			"status": 9, "http_method": 2},
		{"id": 103, "friendly_name": "Olmalı", "url": "https://kadir.app", "type": 2, "keyword_type": "2", "keyword_case_type": "1",
			"keyword_value": "Hoş geldiniz", "interval": 10, "status": 0},
		{"id": 104, "friendly_name": "Sunucu ping", "url": "1.2.3.4", "type": 3, "interval": 300, "status": 2},
		{"id": 105, "friendly_name": "HTTPS portu", "url": "sunucu.kadir.app", "type": 4, "sub_type": 2, "port": "", "interval": 300, "status": 2},
		{"id": 106, "friendly_name": "Özel port", "url": "sunucu.kadir.app", "type": 4, "sub_type": 99, "port": 5432, "interval": 300, "status": 2},
		{"id": 107, "friendly_name": "Gece yedeği", "url": "https://heartbeat.uptimerobot.com/m107-abc", "type": 5, "interval": 86400, "status": 1},
		{"id": 108, "friendly_name": "Bilinmeyen", "url": "x", "type": 99, "interval": 300, "status": 2},
		{"id": 109, "friendly_name": "POST uç", "url": "https://api.kadir.app/ping", "type": 1, "http_method": 3, "post_value": "{\"a\":1}", "interval": 300, "status": 2},
	}
}

func TestUptimeRobot(t *testing.T) {
	fakeUptimeRobot(t, "ur123-oku", URSample())
	list, err := FetchUptimeRobot(context.Background(), http.DefaultClient, "ur123-oku")
	if err != nil {
		t.Fatal(err)
	}
	res := FromUptimeRobot(list)
	d := res.Doc
	if len(d.Monitors) != 8 || len(res.Skipped) != 1 || res.Skipped[0].Name != "Bilinmeyen" {
		t.Fatalf("monitörler: %d, atlanan: %+v", len(d.Monitors), res.Skipped)
	}
	site := monitorByName(t, d, "Ana site")
	c := cfgOf(site)
	if site.Type != "http" || c["method"] != "GET" || c["headers"] != "X-Kaynak: uptimerobot" || site.Interval != 300 || !site.Active {
		t.Errorf("http: %+v %v", site, c)
	}
	if codes, _ := json.Marshal(c["accepted_codes"]); string(codes) != `["200-399"]` {
		t.Errorf("durum kodları: %s", codes)
	}
	kw := monitorByName(t, d, "Durum sayfası")
	c = cfgOf(kw)
	if c["keyword"] != "Hata" || c["keyword_invert"] != true || c["keyword_case"] != false || c["basic_user"] != "u" || !kw.Active {
		t.Errorf("keyword (var olursa uyar): %v", c)
	}
	must := monitorByName(t, d, "Olmalı")
	c = cfgOf(must)
	if c["keyword_invert"] != false || c["keyword_case"] != true || must.Active || must.Interval != 20 {
		t.Errorf("keyword (yoksa uyar), duraklatılmış: %+v %v", must, c)
	}
	if c := cfgOf(monitorByName(t, d, "Sunucu ping")); c["host"] != "1.2.3.4" {
		t.Errorf("ping: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "HTTPS portu")); c["port"] != float64(443) {
		t.Errorf("port 443: %v", c)
	}
	if c := cfgOf(monitorByName(t, d, "Özel port")); c["port"] != float64(5432) || c["host"] != "sunucu.kadir.app" {
		t.Errorf("özel port: %v", c)
	}
	hb := monitorByName(t, d, "Gece yedeği")
	if hb.Type != "push" || hb.PushToken != "" || len(hb.Notes) == 0 || hb.Interval != 86400 {
		t.Errorf("heartbeat: %+v", hb)
	}
	if c := cfgOf(monitorByName(t, d, "POST uç")); c["method"] != "POST" || c["body"] != `{"a":1}` {
		t.Errorf("post: %v", c)
	}
	for _, m := range d.Monitors {
		ch, _ := check.Get(m.Type)
		if _, err := ch.Normalize(m.Config); err != nil {
			t.Errorf("%s ayarı geçersiz: %v", m.Name, err)
		}
	}
	if !strings.Contains(strings.Join(res.Warnings, " "), "push adresleri") {
		t.Errorf("heartbeat uyarısı yok: %v", res.Warnings)
	}
}

func TestUptimeRobotPaginationAndErrors(t *testing.T) {
	var many []map[string]any
	for i := range 123 {
		many = append(many, map[string]any{"id": i + 1, "friendly_name": fmt.Sprint("m", i), "url": "https://x.example", "type": 1, "interval": 300, "status": 2})
	}
	_, calls := fakeUptimeRobot(t, "anahtar", many)
	list, err := FetchUptimeRobot(context.Background(), http.DefaultClient, "anahtar")
	if err != nil || len(list) != 123 || calls.Load() != 3 {
		t.Fatalf("sayfalama: %d monitör, %d çağrı, %v", len(list), calls.Load(), err)
	}
	_, err = FetchUptimeRobot(context.Background(), http.DefaultClient, "yanlis")
	var ue *ErrUptimeRobot
	if err == nil || !strings.Contains(err.Error(), "api_key is invalid") || !asURErr(err, &ue) {
		t.Errorf("geçersiz anahtar: %v", err)
	}
	UptimeRobotAPI = "http://127.0.0.1:1/v2"
	if _, err := FetchUptimeRobot(context.Background(), http.DefaultClient, "anahtar"); err == nil || asURErr(err, &ue) ||
		strings.Contains(err.Error(), "anahtar") {
		t.Errorf("bağlantı hatası (anahtar mesajda olmamalı): %v", err)
	}
}

func asURErr(err error, target **ErrUptimeRobot) bool {
	e, ok := err.(*ErrUptimeRobot)
	if ok {
		*target = e
	}
	return ok
}
