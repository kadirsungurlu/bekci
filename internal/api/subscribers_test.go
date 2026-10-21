package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// fakeSubMail abone e-postalarını (başlıklarıyla) biriktirir.
type fakeSubMail struct {
	mu   sync.Mutex
	sent []sentSubMail
}

type sentSubMail struct {
	to, subject, text, html string
	headers                 map[string]string
}

func (f *fakeSubMail) send(_ context.Context, _ json.RawMessage, to, subject, text, html string, headers map[string]string) error {
	f.mu.Lock()
	f.sent = append(f.sent, sentSubMail{to, subject, text, html, headers})
	f.mu.Unlock()
	return nil
}

func (f *fakeSubMail) to(addr string) []sentSubMail {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sentSubMail
	for _, m := range f.sent {
		if m.to == addr {
			out = append(out, m)
		}
	}
	return out
}

type subEnv struct {
	*pageEnv
	mail   *fakeSubMail
	pageID int64
	mon    store.Monitor
}

const subWindow = 60 * time.Millisecond

// setupSubs yönetici oturumlu ortam, sahte posta, kısa birleştirme penceresi;
// ipLimit > 0 ise abone olma IP sınırı (testte değiştirilebilsin diye).
func setupSubs(t *testing.T, ipLimit int) *subEnv {
	t.Helper()
	se := &subEnv{mail: &fakeSubMail{}, pageEnv: &pageEnv{clock: &atomic.Int64{}}}
	pe := se.pageEnv
	pe.clock.Store(time.Now().UnixNano())
	pe.env = newEnv(t, func(s *Server) {
		pe.s = s
		s.BaseURL = "https://uptime.test/"
		s.now = func() time.Time { return time.Unix(0, pe.clock.Load()) }
		s.subMailer = se.mail.send
		s.subs.window = subWindow
		if ipLimit > 0 {
			s.subs.ipRL = newIPLimiterWindow(ipLimit, subIPWindow)
		}
	})
	pe.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	se.mon = pe.seedMonitor("Ic-Ad", "https://gizli.example/yol")
	var p store.StatusPage
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": "durum", "title": "Hizmetler",
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": se.mon.ID, "name": "Web Sitesi"}}}}}, &p, 201)
	se.pageID = p.ID
	return se
}

// enableMail sistem e-postasını ayarlar.
func (se *subEnv) enableMail() {
	var ch store.Notification
	se.mustDo("POST", "/api/notifications", map[string]any{"name": "Posta", "type": "email", "config": map[string]any{
		"host": "smtp.example.com", "from": "bekci@ornek.com", "to": "ops@ornek.com", "password": "x", "username": "u"}}, &ch, 201)
	var st store.AppSettings
	se.mustDo("GET", "/api/settings", nil, &st, 200)
	st.SystemMailChannelID = ch.ID
	se.mustDo("PUT", "/api/settings", st, nil, 200)
}

func (se *subEnv) setSubscriptions(on bool, want int) {
	se.t.Helper()
	var p store.StatusPage
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d", se.pageID), nil, &p, 200)
	se.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", se.pageID), map[string]any{"slug": p.Slug, "title": p.Title,
		"sections": p.Sections, "subscriptions": on}, nil, want)
}

func (se *subEnv) subscribe(email string, want int) {
	se.t.Helper()
	se.anon().mustDo("POST", "/api/public/pages/durum/subscribe", map[string]string{"email": email, "lang": "tr"}, nil, want)
}

var confirmRe = regexp.MustCompile(`abonelik=onay&t=([A-Za-z0-9_-]+)`)

// waitMails adrese n e-posta gidene kadar bekler.
func (se *subEnv) waitMails(addr string, n int) []sentSubMail {
	se.t.Helper()
	waitFor(se.t, fmt.Sprintf("%s adresine %d e-posta", addr, n), func() bool { return len(se.mail.to(addr)) >= n })
	return se.mail.to(addr)
}

// quiet pencere geçtikten sonra adrese giden e-posta sayısının değişmediğini doğrular.
func (se *subEnv) quiet(addr string, n int) {
	se.t.Helper()
	time.Sleep(subWindow*3 + 50*time.Millisecond)
	if got := len(se.mail.to(addr)); got != n {
		se.t.Fatalf("%s: %d e-posta bekleniyordu, %d gitti", addr, n, got)
	}
}

func (se *subEnv) confirmToken(m sentSubMail) string {
	se.t.Helper()
	mm := confirmRe.FindStringSubmatch(m.text)
	if mm == nil {
		se.t.Fatalf("onay bağlantısı yok: %s", m.text)
	}
	return mm[1]
}

// unsubToken e-postanın List-Unsubscribe başlığındaki belirteç.
func (se *subEnv) unsubToken(m sentSubMail) string {
	se.t.Helper()
	h := m.headers["List-Unsubscribe"]
	_, q, ok := strings.Cut(strings.Trim(h, "<>"), "?t=")
	if !ok {
		se.t.Fatalf("List-Unsubscribe yok: %v", m.headers)
	}
	tok, _ := url.QueryUnescape(q)
	return tok
}

func (se *subEnv) event(kind string, incident int64) {
	se.s.notifier.Notify(notify.Event{Kind: kind, MonitorID: se.mon.ID, MonitorName: "Ic-Ad", Target: "https://gizli.example/yol",
		Message: "connection refused 10.0.0.5", Time: se.s.now(), IncidentID: incident})
}

func (se *subEnv) publicSubscribeFlag() any {
	var pub map[string]any
	se.anon().mustDo("GET", "/api/public/pages/durum", nil, &pub, 200)
	return pub["subscribe"]
}

// Kapalıyken (ya da sistem e-postası yokken) genel uçlar 404, açma isteği 400.
func TestSubscriptionsDisabled(t *testing.T) {
	se := setupSubs(t, 0)
	se.subscribe("a@ornek.com", 404)
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": "x"}, nil, 404)
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe?t=1.2.3", nil, nil, 404)
	if f := se.publicSubscribeFlag(); f != false {
		t.Fatalf("subscribe false olmalı: %v", f)
	}
	// Sistem e-postası yokken açılamaz.
	se.setSubscriptions(true, 400)
	var list map[string]any
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/subscribers", se.pageID), nil, &list, 200)
	if list["available"] != false {
		t.Fatalf("available false olmalı: %v", list)
	}

	se.enableMail()
	se.subscribe("a@ornek.com", 404) // sayfada kapalı
	se.setSubscriptions(true, 200)
	if f := se.publicSubscribeFlag(); f != true {
		t.Fatalf("subscribe true olmalı: %v", f)
	}
	se.subscribe("a@ornek.com", 202)
	tok := se.confirmToken(se.waitMails("a@ornek.com", 1)[0])
	// Sayfada kapatılınca: abone olma ve onay 404.
	se.setSubscriptions(false, 200)
	se.subscribe("b@ornek.com", 404)
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": tok}, nil, 404)
	if f := se.publicSubscribeFlag(); f != false {
		t.Fatalf("kapatınca subscribe false olmalı: %v", f)
	}
}

// Abone ol -> onay -> bildirim -> çıkış.
func TestSubscribeFlow(t *testing.T) {
	se := setupSubs(t, 1000)
	se.enableMail()
	se.setSubscriptions(true, 200)

	se.subscribe("bad-address", 400)
	se.subscribe("A@Ornek.com", 202)
	first := se.waitMails("a@ornek.com", 1)[0]
	if first.headers != nil || !strings.Contains(first.subject, "Hizmetler") ||
		!strings.Contains(first.text, "https://uptime.test/durum/durum?abonelik=onay&t=") {
		t.Fatalf("onay e-postası yanlış: %+v", first)
	}
	oldTok := se.confirmToken(first)
	// 10 dakika içinde ikinci istek: yeni posta yok.
	se.subscribe("a@ornek.com", 202)
	time.Sleep(100 * time.Millisecond)
	if n := len(se.mail.to("a@ornek.com")); n != 1 {
		t.Fatalf("10 dk içinde ikinci onay postası gitmemeli: %d", n)
	}

	// Onaysız aboneye olay bildirimi gitmez.
	se.event(notify.KindDown, 1)
	se.quiet("a@ornek.com", 1)

	// 11 dakika sonra yeniden: yeni belirteç, eskisi geçersiz.
	se.advance(11 * time.Minute)
	se.subscribe("a@ornek.com", 202)
	tok := se.confirmToken(se.waitMails("a@ornek.com", 2)[1])
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": oldTok}, nil, 400)
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": "yanlis"}, nil, 400)
	var conf map[string]any
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": tok}, &conf, 200)
	if conf["page"] != "Hizmetler" {
		t.Fatalf("onay yanıtı: %v", conf)
	}
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": tok}, nil, 400) // tek kullanımlık
	se.subscribe("a@ornek.com", 202)                                                                         // zaten onaylı: posta yok
	se.quiet("a@ornek.com", 2)

	// Süresi dolmuş onay belirteci.
	se.subscribe("b@ornek.com", 202)
	bTok := se.confirmToken(se.waitMails("b@ornek.com", 1)[0])
	se.advance(25 * time.Hour)
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": bTok}, nil, 400)

	// Onaylı aboneye bildirim: sayfadaki ad, hedef/hata yok, çıkış başlıkları.
	se.event(notify.KindDown, 2)
	m := se.waitMails("a@ornek.com", 3)[2]
	if !strings.Contains(m.subject, "Hizmet kesintisi: Web Sitesi") || !strings.HasPrefix(m.subject, "[Hizmetler]") {
		t.Fatalf("konu yanlış: %q", m.subject)
	}
	for _, leak := range []string{"Ic-Ad", "gizli.example", "10.0.0.5"} {
		if strings.Contains(m.text+m.html+m.subject, leak) {
			t.Fatalf("e-postada iç bilgi var (%s)", leak)
		}
	}
	if m.headers["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" ||
		!strings.HasPrefix(m.headers["List-Unsubscribe"], "<https://uptime.test/api/public/subscriptions/unsubscribe?t=") {
		t.Fatalf("çıkış başlıkları yanlış: %v", m.headers)
	}
	if !strings.Contains(m.text, "/durum/durum?abonelik=cik&t=") || !strings.Contains(m.html, "Abonelikten çık") {
		t.Fatalf("gövdede çıkış bağlantısı yok: %s", m.text)
	}

	// Dalgalanma: aynı pencerede başlangıç + çözüm -> e-posta yok.
	se.event(notify.KindDown, 3)
	se.event(notify.KindUp, 3)
	se.quiet("a@ornek.com", 3)
	// İki ayrı güncelleme tek özet e-postada.
	se.event(notify.KindUp, 2)
	se.event(notify.KindDown, 4)
	m = se.waitMails("a@ornek.com", 4)[3]
	if !strings.Contains(m.subject, "2 güncelleme") {
		t.Fatalf("özet konusu yanlış: %q", m.subject)
	}

	// Abone başına saatlik sınır: bu saatte 2 gitti, 4 daha gider, sonra durur.
	for i := int64(10); i < 14; i++ {
		se.event(notify.KindDown, i)
		se.waitMails("a@ornek.com", int(i-10)+5)
	}
	se.event(notify.KindDown, 20)
	se.quiet("a@ornek.com", 8)
	se.advance(61 * time.Minute)
	se.event(notify.KindDown, 21)
	m = se.waitMails("a@ornek.com", 9)[8]

	// Çıkış: bozuk ve süresi dolmuş belirteç 400, geçerlisi satırı siler.
	unsub := se.unsubToken(m)
	parts := strings.Split(unsub, ".")
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": "bozuk"}, nil, 400)
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": parts[0] + "." + parts[1] + ".AAAA"}, nil, 400)
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": parts[0] + ".1." + parts[2]}, nil, 400)
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": unsub}, nil, 200)
	se.anon().mustDo("POST", "/api/public/subscriptions/unsubscribe", map[string]string{"token": unsub}, nil, 200) // zaten yok
	var list struct {
		Count, Pending int
	}
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/subscribers", se.pageID), nil, &list, 200)
	if list.Count != 0 {
		t.Fatalf("çıkıştan sonra onaylı abone kalmamalı: %+v", list)
	}
	se.event(notify.KindDown, 30)
	se.quiet("a@ornek.com", 9)

	// One-click: X-Uptime başlığı olmadan form POST.
	se.advance(time.Hour)
	se.subscribe("c@ornek.com", 202)
	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": se.confirmToken(se.waitMails("c@ornek.com", 1)[0])}, nil, 200)
	se.event(notify.KindDown, 31)
	m = se.waitMails("c@ornek.com", 2)[1]
	link := strings.Trim(m.headers["List-Unsubscribe"], "<>")
	link = strings.Replace(link, "https://uptime.test", se.srv.URL, 1)
	resp, err := http.Post(link, "application/x-www-form-urlencoded", strings.NewReader("List-Unsubscribe=One-Click"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("one-click çıkış: %d %s", resp.StatusCode, body)
	}
	se.event(notify.KindDown, 32)
	se.quiet("c@ornek.com", 2)
	// Diğer genel POST'lar CSRF başlığı ister.
	resp, err = http.Post(se.srv.URL+"/api/public/pages/durum/subscribe", "application/json", strings.NewReader(`{"email":"d@ornek.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("X-Uptime'sız abone olma 403 olmalı: %d", resp.StatusCode)
	}
}

// IP başına sınır; yönetim listesi, CSV ve silme.
func TestSubscribersAdmin(t *testing.T) {
	se := setupSubs(t, 0)
	se.enableMail()
	se.setSubscriptions(true, 200)
	se.subscribe("=hack@ornek.com", 202)
	se.subscribe("b@ornek.com", 202)
	se.subscribe("c@ornek.com", 202)
	se.subscribe("d@ornek.com", 202)
	se.subscribe("e@ornek.com", 202)
	se.subscribe("f@ornek.com", 429)

	se.anon().mustDo("POST", "/api/public/subscriptions/confirm", map[string]string{"token": se.confirmToken(se.waitMails("b@ornek.com", 1)[0])}, nil, 200)
	var list struct {
		Available bool `json:"available"`
		Count     int  `json:"count"`
		Pending   int  `json:"pending"`
		Items     []struct {
			ID        int64  `json:"id"`
			Email     string `json:"email"`
			Confirmed bool   `json:"confirmed"`
		} `json:"items"`
	}
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/subscribers", se.pageID), nil, &list, 200)
	if !list.Available || list.Count != 1 || list.Pending != 4 || len(list.Items) != 5 {
		t.Fatalf("liste yanlış: %+v", list)
	}

	resp, body := se.raw("GET", fmt.Sprintf("/api/status-pages/%d/subscribers.csv", se.pageID), "", "", nil)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("CSV: %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	csv := string(body)
	if !strings.HasPrefix(csv, "email,status,created_at,confirmed_at\n") || !strings.Contains(csv, "'=hack@ornek.com,pending,") ||
		!regexp.MustCompile(`b@ornek\.com,confirmed,\d{4}-\d\d-\d\dT[^,]+Z,\d{4}-`).MatchString(csv) {
		t.Fatalf("CSV içeriği yanlış:\n%s", csv)
	}

	var victim int64
	for _, it := range list.Items {
		if it.Email == "b@ornek.com" {
			victim = it.ID
		}
	}
	se.mustDo("DELETE", fmt.Sprintf("/api/status-pages/%d/subscribers/%d", se.pageID+100, victim), nil, nil, 404)
	if code := se.do("DELETE", fmt.Sprintf("/api/status-pages/%d/subscribers/%d", se.pageID, victim), nil, nil); code != 204 {
		t.Fatalf("silme: %d", code)
	}
	se.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/subscribers", se.pageID), nil, &list, 200)
	if list.Count != 0 || len(list.Items) != 4 {
		t.Fatalf("silmeden sonra liste yanlış: %+v", list)
	}
	// Onaylı abone yokken bildirim gitmez.
	se.event(notify.KindDown, 1)
	se.quiet("b@ornek.com", 1)
}
