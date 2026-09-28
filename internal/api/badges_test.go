package api

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/uptime-kadir-app/internal/store"
)

// wellFormed SVG'nin geçerli XML olduğunu doğrular.
func wellFormed(t *testing.T, svg []byte) {
	t.Helper()
	dec := xml.NewDecoder(strings.NewReader(string(svg)))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("SVG geçerli XML değil: %v\n%s", err, svg)
		}
	}
}

func (f *fenv) page(published bool, password string, ids ...int64) store.StatusPage {
	f.t.Helper()
	p := store.StatusPage{Slug: fmt.Sprintf("s%d", time.Now().UnixNano()), Title: "Durum", Published: published, PasswordHash: password}
	sec := store.PageSection{Title: "Grup"}
	for _, id := range ids {
		sec.Monitors = append(sec.Monitors, store.PageMonitor{ID: id})
	}
	p.Sections = []store.PageSection{sec}
	if err := f.st.CreatePage(context.Background(), &p); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func TestBadgeExposure(t *testing.T) {
	f := newFeatureEnv(t)
	secret := f.push("Gizli Müşteri Sunucusu")
	public := f.push("Açık Site")
	badge := func(id int64, kind string, hdr map[string]string) (int, http.Header, []byte) {
		return f.rawReq("GET", fmt.Sprintf("/api/badge/%d/%s", id, kind), hdr, nil)
	}

	// Hiçbir sayfada yok → 404.
	if code, _, _ := badge(secret.ID, "status.svg", nil); code != 404 {
		t.Errorf("sayfada olmayan monitör: %d, 404 bekleniyordu", code)
	}
	// Yayında olmayan ve şifreli sayfalar sayılmaz.
	f.page(false, "", secret.ID)
	f.page(true, "$2a$04$hash", secret.ID, public.ID)
	if code, _, _ := badge(secret.ID, "status.svg", nil); code != 404 {
		t.Errorf("yayında olmayan/şifreli sayfadaki monitör: %d, 404 bekleniyordu", code)
	}
	// Yayındaki şifresiz sayfa → herkese açık. Rozet "herkese açık monitör"
	// listesini kısa süre önbelleğe alır (badgeAllowTTL); sayfa doğrudan
	// veritabanına yazıldığı için (API'yi atlayarak) önbellek yenilensin diye
	// saat ileri alınır.
	f.page(true, "", public.ID)
	f.clk.advance(badgeAllowTTL + time.Second)
	for _, kind := range []string{"status.svg", "uptime.svg", "ping.svg", "cert-exp.svg"} {
		code, h, body := badge(public.ID, kind, nil)
		if code != 200 {
			t.Fatalf("%s: %d %s", kind, code, body)
		}
		if !strings.HasPrefix(h.Get("Content-Type"), "image/svg+xml") || h.Get("Cache-Control") != "public, max-age=60" {
			t.Errorf("%s başlıkları: %v", kind, h)
		}
		if strings.Contains(string(body), "Açık Site") {
			t.Errorf("%s monitör adını sızdırıyor", kind)
		}
		wellFormed(t, body)
	}
	// Olmayan monitör ve bilinmeyen rozet türü.
	if code, _, _ := badge(99999, "status.svg", nil); code != 404 {
		t.Errorf("olmayan monitör: %d", code)
	}
	if code, _, _ := badge(public.ID, "foo.svg", nil); code != 404 {
		t.Errorf("bilinmeyen rozet: %d", code)
	}

	// API anahtarı: sayfada olmayan ama kullanıcının görebildiği monitör.
	k := f.newKey("rozet", store.RoleViewer, 0)
	code, h, _ := badge(secret.ID, "status.svg", bearer(k.Secret))
	if code != 200 || h.Get("Cache-Control") != "private, max-age=60" {
		t.Errorf("anahtarla rozet: %d %v", code, h.Get("Cache-Control"))
	}
	// Kısıtlı müşterinin anahtarı başka monitörün rozetini alamaz.
	cust, _ := f.newUser("musteri", store.RoleViewer, []int64{public.ID})
	ck := cust.newKey("m", store.RoleViewer, 0)
	if code, _, _ := badge(secret.ID, "status.svg", bearer(ck.Secret)); code != 404 {
		t.Errorf("kısıtlı anahtarla başka monitörün rozeti: %d, 404 bekleniyordu", code)
	}
	// Oturum (arayüz önizlemesi).
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/badge/%d/status.svg", f.srv.URL, secret.ID), nil)
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("oturumla rozet: %d", resp.StatusCode)
	}
}

func TestBadgeRendering(t *testing.T) {
	f := newFeatureEnv(t)
	m := f.push("Site")
	f.page(true, "", m.ID)
	get := func(kind, query string) (int, string) {
		code, _, body := f.rawReq("GET", fmt.Sprintf("/api/badge/%d/%s?%s", m.ID, kind, query), nil, nil)
		return code, string(body)
	}

	// Kaçırma: etiket SVG/HTML olarak yorumlanamaz.
	evil := `<script>alert(1)</script>"&'><svg onload=x>`
	code, body := get("status.svg", "label="+url.QueryEscape(evil))
	if code != 200 {
		t.Fatalf("etiketli rozet: %d", code)
	}
	if strings.Contains(body, "<script") || strings.Contains(body, "<svg onload") || strings.Contains(body, `"&'`) {
		t.Errorf("etiket kaçırılmamış: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("kaçırılmış etiket bulunamadı: %s", body)
	}
	wellFormed(t, []byte(body))
	// Kontrol karakterleri atılır, XML bozulmaz.
	code, body = get("status.svg", "label=a%00b%1bc")
	if code != 200 {
		t.Fatal(code)
	}
	wellFormed(t, []byte(body))

	// Renk ve parametre doğrulaması.
	for _, q := range []string{
		"upColor=" + url.QueryEscape(`red" onload="x`), "downColor=url(javascript:x)", "labelColor=12345",
		"style=plastic", "duration=1y", "color=%23ggg",
	} {
		if code, _ := get("uptime.svg", q); code != 400 {
			t.Errorf("geçersiz parametre %q: %d, 400 bekleniyordu", q, code)
		}
	}
	code, body = get("status.svg", "pendingColor=00FF00&labelColor=blue")
	if code != 200 || !strings.Contains(body, `fill="#00ff00"`) || !strings.Contains(body, `fill="#007ec6"`) {
		t.Errorf("özel renkler uygulanmadı: %d %s", code, body)
	}

	// Varsayılan durum: bekliyor. for-the-badge Türkçe büyük harf kullanır.
	_, body = get("status.svg", "style=for-the-badge")
	if !strings.Contains(body, "BEKLİYOR") || !strings.Contains(body, "DURUM") || !strings.Contains(body, `height="28"`) {
		t.Errorf("for-the-badge: %s", body)
	}
	_, body = get("status.svg", "style=flat-square")
	if strings.Contains(body, "linearGradient") || strings.Contains(body, `rx="3"`) {
		t.Errorf("flat-square düz olmalı: %s", body)
	}
	_, body = get("uptime.svg", "")
	if !strings.Contains(body, "veri yok") {
		t.Errorf("veri yokken uptime: %s", body)
	}
	_, body = get("cert-exp.svg", "")
	if !strings.Contains(body, ">yok<") {
		t.Errorf("sertifikasız monitör: %s", body)
	}

	// Veri gelince.
	resp, _ := http.Get(f.srv.URL + "/api/push/" + m.PushToken + "?status=up&ping=1234")
	resp.Body.Close()
	waitFor(t, "çalışıyor", func() bool {
		_, body := get("status.svg", "")
		return strings.Contains(body, "çalışıyor")
	})
	_, body = get("uptime.svg", "duration=7d")
	if !strings.Contains(body, "%100") || !strings.Contains(body, "uptime (7 gün)") {
		t.Errorf("uptime rozeti: %s", body)
	}
	_, body = get("ping.svg", "")
	if !strings.Contains(body, "1.234 ms") {
		t.Errorf("ping rozeti: %s", body)
	}
}

func TestBadgeHelpers(t *testing.T) {
	for in, want := range map[float64]string{100: "%100", 99.999: "%99,99", 95.5: "%95,50", 0: "%0,00"} {
		if got := fmtPercent("tr", in); got != want {
			t.Errorf("fmtPercent(%v) = %q, %q bekleniyordu", in, got, want)
		}
	}
	for in, want := range map[int64]string{5: "5", 1234: "1.234", 1234567: "1.234.567"} {
		if got := fmtThousands("tr", in); got != want {
			t.Errorf("fmtThousands(%d) = %q", in, got)
		}
	}
	for _, c := range []string{"#fff", "FFF", "a1b2c3", "brightgreen", "RED"} {
		if _, ok := parseColor(c); !ok {
			t.Errorf("geçerli renk reddedildi: %q", c)
		}
	}
	for _, c := range []string{"", "#ffff", "rgb(0,0,0)", "red;", "#12345g", "javascript"} {
		if _, ok := parseColor(c); ok {
			t.Errorf("geçersiz renk kabul edildi: %q", c)
		}
	}
}

var promLine = regexp.MustCompile(`^[a-z_]+(\{([a-z_]+="([^"\\]|\\.)*",?)*\})? -?[0-9.e+-]+$`)

func TestMetrics(t *testing.T) {
	f := newFeatureEnv(t)
	a := f.push(`Ana "site" \ yedek`)
	b := f.push("B")

	// Kimlik doğrulama.
	code, h, _ := f.rawReq("GET", "/metrics", nil, nil)
	if code != 401 || !strings.Contains(h.Get("WWW-Authenticate"), "Basic") {
		t.Fatalf("kimliksiz metrik: %d %v", code, h)
	}
	if code, _, _ := f.rawReq("GET", "/metrics", bearer("upk_yanlis"), nil); code != 401 {
		t.Errorf("yanlış anahtar: %d", code)
	}
	// Oturum çerezi tek başına yetmez.
	resp, err := f.client.Get(f.srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("oturumla metrik: %d, 401 bekleniyordu", resp.StatusCode)
	}

	k := f.newKey("prometheus", store.RoleViewer, 0)
	resp, _ = http.Get(f.srv.URL + "/api/push/" + a.PushToken + "?status=up&ping=42")
	resp.Body.Close()
	waitFor(t, "a çalışıyor", func() bool {
		m, _ := f.st.GetMonitor(context.Background(), a.ID)
		return m.Status == store.StatusUp
	})

	code, h, body := f.rawReq("GET", "/metrics", bearer(k.Secret), nil)
	if code != 200 || h.Get("Content-Type") != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("metrik: %d %v %s", code, h, body)
	}
	text := string(body)
	for _, want := range []string{
		`uptime_app_info{version="test"} 1`,
		"# TYPE uptime_monitor_status gauge",
		fmt.Sprintf(`uptime_monitor_status{monitor_id="%d",monitor_name="Ana \"site\" \\ yedek",monitor_type="push"} 1`, a.ID),
		fmt.Sprintf(`uptime_monitor_status{monitor_id="%d",monitor_name="B",monitor_type="push"} 2`, b.ID),
		fmt.Sprintf(`uptime_monitor_response_time_ms{monitor_id="%d",monitor_name="Ana \"site\" \\ yedek",monitor_type="push"} 42`, a.ID),
		fmt.Sprintf(`uptime_monitor_uptime_ratio{monitor_id="%d",monitor_name="Ana \"site\" \\ yedek",monitor_type="push",window="24h"} 1`, a.ID),
		fmt.Sprintf(`uptime_monitor_active{monitor_id="%d",monitor_name="B",monitor_type="push"} 1`, b.ID),
	} {
		if !strings.Contains(text, want+"\n") {
			t.Errorf("metrikte yok: %s\n---\n%s", want, text)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if strings.HasPrefix(line, "# HELP ") || strings.HasPrefix(line, "# TYPE ") {
			continue
		}
		if !promLine.MatchString(line) {
			t.Errorf("geçersiz metrik satırı: %q", line)
		}
	}

	// Basic kimlik doğrulama: metrics / anahtar.
	req, _ := http.NewRequest("GET", f.srv.URL+"/metrics", nil)
	req.SetBasicAuth("metrics", k.Secret)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("Basic ile metrik: %d", resp.StatusCode)
	}

	// Kısıtlı kullanıcının anahtarı sadece izinli monitörü görür.
	cust, _ := f.newUser("musteri", store.RoleViewer, []int64{b.ID})
	ck := cust.newKey("m", store.RoleViewer, 0)
	_, _, body = f.rawReq("GET", "/metrics", bearer(ck.Secret), nil)
	if strings.Contains(string(body), fmt.Sprintf(`monitor_id="%d"`, a.ID)) || !strings.Contains(string(body), fmt.Sprintf(`monitor_id="%d"`, b.ID)) {
		t.Errorf("kısıtlı metrik: %s", body)
	}
}
