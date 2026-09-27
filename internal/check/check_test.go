package check

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/miekg/dns"
)

func run(t *testing.T, typ string, cfg any) Result {
	t.Helper()
	c, ok := Get(typ)
	if !ok {
		t.Fatalf("tip kayıtlı değil: %s", typ)
	}
	raw, _ := json.Marshal(cfg)
	norm, err := c.Normalize(raw)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Check(ctx, norm)
}

func TestHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, `<html><title>Home Assistant</title></html>`)
		case "/500":
			w.WriteHeader(500)
		case "/head-yok":
			if r.Method == http.MethodHead {
				w.WriteHeader(405)
				return
			}
			fmt.Fprint(w, "ok")
		case "/yonlen":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/json":
			fmt.Fprint(w, `{"status":"ok","count":7,"items":[{"name":"a"}]}`)
		case "/yavas":
			time.Sleep(2 * time.Second)
		case "/auth":
			u, p, _ := r.BasicAuth()
			if u != "kadir" || p != "gizli" || r.Header.Get("X-Test") != "evet" {
				w.WriteHeader(401)
			}
		case "/post":
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(400)
			}
		}
	}))
	defer srv.Close()
	u := srv.URL

	cases := []struct {
		name   string
		cfg    map[string]any
		up     bool
		msgHas string
	}{
		{"basit", map[string]any{"url": u + "/ok"}, true, "200 OK"},
		{"500 hata", map[string]any{"url": u + "/500"}, false, "HTTP 500"},
		{"varsayılan GET, HEAD 405 sorunu yok", map[string]any{"url": u + "/head-yok"}, true, ""},
		{"HEAD seçilirse 405", map[string]any{"url": u + "/head-yok", "method": "HEAD"}, false, "405"},
		{"yönlendirme takip edilir", map[string]any{"url": u + "/yonlen", "keyword": "Home Assistant"}, true, ""},
		{"yönlendirme takip edilmez, 302 kabul", map[string]any{"url": u + "/yonlen", "max_redirects": 0}, true, "302"},
		{"yönlendirme takip edilmez, sadece 200", map[string]any{"url": u + "/yonlen", "max_redirects": 0, "accepted_codes": []string{"200"}}, false, "302"},
		{"keyword var", map[string]any{"url": u + "/ok", "keyword": "home assistant"}, true, ""},
		{"keyword harf duyarlı", map[string]any{"url": u + "/ok", "keyword": "home assistant", "keyword_case": true}, false, "bulunamadı"},
		{"keyword yok", map[string]any{"url": u + "/ok", "keyword": "hata"}, false, "bulunamadı"},
		{"ters keyword", map[string]any{"url": u + "/ok", "keyword": "Home", "keyword_invert": true}, false, "olmaması"},
		{"json eşit", map[string]any{"url": u + "/json", "json_path": "status", "json_expected": "ok"}, true, ""},
		{"json sayısal >", map[string]any{"url": u + "/json", "json_path": "count", "json_op": ">", "json_expected": "5"}, true, ""},
		{"json sayısal < başarısız", map[string]any{"url": u + "/json", "json_path": "count", "json_op": "<", "json_expected": "5"}, false, "beklenen değerle eşleşmedi"},
		{"json dizi yolu", map[string]any{"url": u + "/json", "json_path": "items.0.name", "json_expected": "a"}, true, ""},
		{"json alan yok", map[string]any{"url": u + "/json", "json_path": "yok", "json_op": "exists"}, false, "alanı yok"},
		{"json değil", map[string]any{"url": u + "/ok", "json_path": "a"}, false, "JSON değil"},
		{"basic auth + başlık", map[string]any{"url": u + "/auth", "basic_user": "kadir", "basic_pass": "gizli", "headers": "X-Test: evet"}, true, ""},
		{"basic auth yanlış", map[string]any{"url": u + "/auth", "basic_user": "kadir", "basic_pass": "x"}, false, "401"},
		{"post json gövde", map[string]any{"url": u + "/post", "method": "POST", "body": `{"a":1}`}, true, ""},
		{"kod aralığı 2xx", map[string]any{"url": u + "/500", "accepted_codes": []string{"2xx", "500"}}, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := run(t, "http", tc.cfg)
			if r.Up != tc.up || !strings.Contains(r.Message, tc.msgHas) {
				t.Errorf("up=%v msg=%q; beklenen up=%v, mesajda %q", r.Up, r.Message, tc.up, tc.msgHas)
			}
			if r.Up && r.PingMs < 1 {
				t.Errorf("ping ölçülmedi: %d", r.PingMs)
			}
		})
	}
}

// TestHTTPJSONMismatchNoLeak: JSON yolu uyuşmadığında mesaj, hedeften okunan
// GERÇEK değeri yankılamaz (izleyiciye kadar ulaşan bu mesaj hedef verisini
// sızdırabilir). Kullanıcının kendi girdiği beklenen değer görülebilir.
func TestHTTPJSONMismatchNoLeak(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"secret":"S3CR3T-DAHILI-DEGER","sayi":"metin-degeri"}`)
	}))
	defer srv.Close()

	// Eşitlik uyuşmazlığı: gerçek değer mesaja yazılmamalı.
	res := run(t, "http", map[string]any{"url": srv.URL, "json_path": "secret", "json_op": "==", "json_expected": "BEKLENEN"})
	if res.Up || strings.Contains(res.Message, "S3CR3T-DAHILI-DEGER") {
		t.Errorf("gerçek değer sızdı: %q", res.Message)
	}
	if !strings.Contains(res.Message, "BEKLENEN") || !strings.Contains(res.Message, "secret") {
		t.Errorf("beklenen değer ve yol mesajda olmalı: %q", res.Message)
	}

	// Sayısal olmayan değer: yine gerçek değer yazılmamalı.
	res = run(t, "http", map[string]any{"url": srv.URL, "json_path": "sayi", "json_op": ">", "json_expected": "5"})
	if res.Up || strings.Contains(res.Message, "metin-degeri") {
		t.Errorf("sayısal olmayan değer sızdı: %q", res.Message)
	}
}

func TestHTTPTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
	}))
	defer srv.Close()
	c, _ := Get("http")
	cfg, _ := c.Normalize(json.RawMessage(`{"url":"` + srv.URL + `"}`))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	r := c.Check(ctx, cfg)
	if r.Up || r.Message != "Zaman aşımı" {
		t.Errorf("zaman aşımı bekleniyordu: %+v", r)
	}
}

func TestHTTPSCertInfo(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	// Kendinden imzalı sertifika: doğrulama hatası beklenir.
	r := run(t, "http", map[string]any{"url": srv.URL})
	if r.Up || !strings.Contains(r.Message, "SSL") {
		t.Errorf("SSL hatası bekleniyordu: %+v", r)
	}
	// TLS hatası yok sayılınca çalışır ve sertifika bilgisi gelir.
	r = run(t, "http", map[string]any{"url": srv.URL, "ignore_tls": true})
	if !r.Up || r.Cert == nil || r.Cert.NotAfter.IsZero() {
		t.Errorf("sertifika bilgisi bekleniyordu: %+v", r)
	}
}

func TestHTTPNormalize(t *testing.T) {
	c, _ := Get("http")
	bad := []string{
		`{"url":"ftp://x"}`,
		`{"url":"https://x","method":"FOO"}`,
		`{"url":"https://x","accepted_codes":["abc"]}`,
		`{"url":"https://x","headers":"bozuk satır"}`,
		`{"url":"https://x","method":"HEAD","keyword":"a"}`,
		`{"url":"https://x","bilinmeyen":1}`,
	}
	for _, b := range bad {
		_, err := c.Normalize(json.RawMessage(b))
		var ve ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: doğrulama hatası bekleniyordu, %v geldi", b, err)
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"url":" https://kadir.app "}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := HTTPConfigOf(norm)
	if cfg.URL != "https://kadir.app" || cfg.Method != "GET" || *cfg.MaxRedirects != 10 || !*cfg.CertExpiry || cfg.AcceptedCodes[0] != "200-399" {
		t.Errorf("varsayılanlar yanlış: %+v", cfg)
	}
}

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	if r := run(t, "tcp", map[string]any{"host": "127.0.0.1", "port": port}); !r.Up {
		t.Errorf("açık port: %+v", r)
	}
	ln.Close()
	if r := run(t, "tcp", map[string]any{"host": "127.0.0.1", "port": port}); r.Up || r.Message != "Bağlantı reddedildi" {
		t.Errorf("kapalı port: %+v", r)
	}
}

func TestDNS(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := dns.NewServeMux()
	mux.HandleFunc("kadir.app.", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetReply(r)
		switch r.Question[0].Qtype {
		case dns.TypeA:
			rr, _ := dns.NewRR("kadir.app. 60 IN A 94.130.174.47")
			m.Answer = append(m.Answer, rr)
		case dns.TypeMX:
			rr, _ := dns.NewRR("kadir.app. 60 IN MX 10 mx.kadir.app.")
			m.Answer = append(m.Answer, rr)
		}
		w.WriteMsg(m)
	})
	mux.HandleFunc(".", func(w dns.ResponseWriter, r *dns.Msg) {
		m := new(dns.Msg)
		m.SetRcode(r, dns.RcodeNameError)
		w.WriteMsg(m)
	})
	srv := &dns.Server{PacketConn: pc, Handler: mux}
	go srv.ActivateAndServe()
	defer srv.Shutdown()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	base := map[string]any{"server": "127.0.0.1", "port": port}
	with := func(kv ...any) map[string]any {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for i := 0; i < len(kv); i += 2 {
			m[kv[i].(string)] = kv[i+1]
		}
		return m
	}

	if r := run(t, "dns", with("host", "kadir.app")); !r.Up || r.Message != "94.130.174.47" {
		t.Errorf("A kaydı: %+v", r)
	}
	if r := run(t, "dns", with("host", "kadir.app", "record_type", "MX", "expected", "mx.kadir")); !r.Up || r.Message != "10 mx.kadir.app" {
		t.Errorf("MX kaydı: %+v", r)
	}
	if r := run(t, "dns", with("host", "kadir.app", "expected", "1.2.3.4")); r.Up || !strings.Contains(r.Message, "Beklenen") {
		t.Errorf("beklenen değer yok: %+v", r)
	}
	if r := run(t, "dns", with("host", "yok.example")); r.Up || !strings.Contains(r.Message, "NXDOMAIN") {
		t.Errorf("NXDOMAIN: %+v", r)
	}
	if r := run(t, "dns", with("host", "kadir.app", "record_type", "TXT")); r.Up || !strings.Contains(r.Message, "bulunamadı") {
		t.Errorf("kayıt yok: %+v", r)
	}
}

func TestPingLocalhost(t *testing.T) {
	r := run(t, "ping", map[string]any{"host": "127.0.0.1", "count": 2})
	if strings.Contains(r.Message, "permission denied") || strings.Contains(r.Message, "operation not permitted") {
		t.Skipf("bu ortamda yetkisiz ping kapalı: %s", r.Message)
	}
	if !r.Up || r.Message != "2/2 paket" {
		t.Errorf("localhost ping: %+v", r)
	}
}

func TestCodeRanges(t *testing.T) {
	cases := map[string][2]int{"200": {200, 200}, "200-299": {200, 299}, "3xx": {300, 399}}
	for in, want := range cases {
		lo, hi, err := parseCodeRange(in)
		if err != nil || lo != want[0] || hi != want[1] {
			t.Errorf("%s → %d-%d %v", in, lo, hi, err)
		}
	}
	for _, in := range []string{"", "99", "600", "300-200", "6xx", "a-b"} {
		if _, _, err := parseCodeRange(in); err == nil {
			t.Errorf("%q reddedilmeliydi", in)
		}
	}
}

// Bulgu: yönlendirme sınırı aşılınca 3xx "çalışıyor" sayılmamalı.
func TestHTTPTooManyRedirects(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/dongu", http.StatusFound)
	}))
	defer srv.Close()
	r := run(t, "http", map[string]any{"url": srv.URL, "max_redirects": 2})
	if r.Up || !strings.Contains(r.Message, "Çok fazla yönlendirme") {
		t.Errorf("yönlendirme döngüsü DOWN olmalı: %+v", r)
	}
}

// Bulgu: DNS kontrolü kütüphanenin 2 saniyesi yerine monitörün süresini kullanmalı.
func TestDNSUsesMonitorTimeout(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &dns.Server{PacketConn: pc, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, r *dns.Msg) {
		time.Sleep(2500 * time.Millisecond) // yavaş yetkili sunucu
		m := new(dns.Msg)
		m.SetReply(r)
		rr, _ := dns.NewRR("yavas.test. 60 IN A 10.0.0.1")
		m.Answer = append(m.Answer, rr)
		w.WriteMsg(m)
	})}
	go srv.ActivateAndServe()
	defer srv.Shutdown()
	c, _ := Get("dns")
	cfg, _ := c.Normalize(json.RawMessage(fmt.Sprintf(`{"host":"yavas.test","server":"127.0.0.1","port":%d}`, pc.LocalAddr().(*net.UDPAddr).Port)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if r := c.Check(ctx, cfg); !r.Up {
		t.Errorf("5 sn zaman aşımında 2,5 sn'lik yanıt beklenmeli: %+v", r)
	}
}
