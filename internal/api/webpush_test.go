package api

import (
	"crypto/ecdh"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// fakePushService push servisi: aldığı istekleri sayar; endpoint yoluna göre
// 201 ya da 410 döner.
type fakePushService struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
	auth map[string]string // yol → Authorization başlığı
}

func newFakePushService(t *testing.T) *fakePushService {
	f := &fakePushService{hits: map[string]int{}, auth: map[string]string{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.hits[r.URL.Path]++
		f.auth[r.URL.Path] = r.Header.Get("Authorization")
		f.mu.Unlock()
		if r.Header.Get("Content-Encoding") != "aes128gcm" || len(body) < 86 {
			w.WriteHeader(400)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/gone") {
			w.WriteHeader(410)
			return
		}
		w.WriteHeader(201)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakePushService) authOf(path string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth[path]
}

func (f *fakePushService) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[path]
}

// subscription sahte tarayıcı aboneliği (rastgele P-256 anahtar).
func subscription(t *testing.T, endpoint string) map[string]any {
	t.Helper()
	ua, err := ecdh.P256().GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	b64 := base64.RawURLEncoding
	return map[string]any{"endpoint": endpoint, "keys": map[string]string{
		"p256dh": b64.EncodeToString(ua.PublicKey().Bytes()), "auth": b64.EncodeToString([]byte("0123456789abcdef"))}}
}

// Web Push uçtan uca: abonelik, VAPID anahtarı, test bildirimi, webpush
// kanalıyla kesinti bildirimi (kısıtlı kullanıcı görmediği monitörü almaz),
// 410 ile aboneliğin silinmesi.
func TestWebPush(t *testing.T) {
	ps := newFakePushService(t)
	admin := setupAdmin(t)
	var vapid map[string]string
	admin.mustDo("GET", "/api/webpush/vapid", nil, &vapid, 200)
	if len(vapid["public_key"]) < 80 {
		t.Fatalf("VAPID açık anahtarı: %v", vapid)
	}
	admin.mustDo("GET", "/api/webpush/vapid", nil, &vapid, 200) // ikinci çağrı aynı anahtar
	// Abonelik: http endpoint yalnızca yerel adreste; anahtarsız 400.
	admin.mustDo("POST", "/api/webpush/subscriptions", map[string]any{"endpoint": "http://push.example.com/x", "keys": map[string]string{"p256dh": "a", "auth": "b"}}, nil, 400)
	admin.mustDo("POST", "/api/webpush/subscriptions", map[string]any{"endpoint": ps.srv.URL + "/a", "keys": map[string]string{"p256dh": "a", "auth": "b"}}, nil, 400)
	admin.mustDo("POST", "/api/webpush/subscriptions", subscription(t, ps.srv.URL+"/admin-1"), nil, 200)
	admin.mustDo("POST", "/api/webpush/subscriptions", subscription(t, ps.srv.URL+"/admin-1"), nil, 200) // aynı endpoint: tekil
	var subs []store.PushSubscription
	admin.mustDo("GET", "/api/webpush/subscriptions", nil, &subs, 200)
	if len(subs) != 1 {
		t.Fatalf("abonelik tekil olmalı: %d", len(subs))
	}
	// Test bildirimi.
	var res map[string]int
	admin.mustDo("POST", "/api/webpush/test", nil, &res, 200)
	if res["sent"] != 1 || ps.count("/admin-1") != 1 || !strings.HasPrefix(ps.authOf("/admin-1"), "vapid t=") {
		t.Fatalf("test bildirimi: %v hits=%d auth=%q", res, ps.count("/admin-1"), ps.authOf("/admin-1"))
	}

	// Kanal: herkese (users boş); kısıtlı müşteri yalnızca kendi monitörünü alır.
	mine := admin.push("Müşteri sitesi")
	other := admin.push("Başka site")
	cust, _ := admin.newUser("musteri", store.RoleViewer, []int64{mine.ID})
	cust.mustDo("POST", "/api/webpush/subscriptions", subscription(t, ps.srv.URL+"/cust-1"), nil, 200)
	var ch store.Notification
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "Push", "type": "webpush", "config": map[string]any{"users": ""}, "apply_existing": true}, &ch, 201)
	admin.mustDo("POST", "/api/notifications", map[string]any{"name": "Bozuk", "type": "webpush", "config": map[string]any{"users": "ali, yok böyle"}}, nil, 400)

	admin.mustDo("GET", "/api/push/"+other.PushToken+"?status=down&msg=x", nil, nil, 200)
	waitFor(t, "başka siteye kesinti bildirimi yöneticiye gitti", func() bool { return ps.count("/admin-1") >= 2 })
	if ps.count("/cust-1") != 0 {
		t.Fatal("müşteri görmediği monitörün bildirimini almamalı")
	}
	admin.mustDo("GET", "/api/push/"+mine.PushToken+"?status=down&msg=x", nil, nil, 200)
	waitFor(t, "müşteri sitesine kesinti bildirimi müşteriye gitti", func() bool { return ps.count("/cust-1") >= 1 })
	// Olayın işlem geçmişinde kanal gönderimi başarılı.
	var list []store.Incident
	admin.mustDo("GET", "/api/incidents?open=1", nil, &list, 200)
	okNotify := false
	for _, inc := range list {
		var d incidentResp
		admin.mustDo("GET", fmt.Sprintf("/api/incidents/%d", inc.ID), nil, &d, 200)
		for _, ev := range d.Events {
			if ev.Kind == store.EventNotify && strings.Contains(string(ev.Data), `"type":"webpush"`) && strings.Contains(string(ev.Data), `"ok":true`) {
				okNotify = true
			}
		}
	}
	if !okNotify {
		t.Fatal("işlem geçmişinde başarılı webpush kaydı yok")
	}
	// Yalnızca belirli kullanıcılar; 410 veren abonelik silinir.
	admin.mustDo("PUT", fmt.Sprintf("/api/notifications/%d", ch.ID), map[string]any{"name": "Push", "type": "webpush", "config": map[string]any{"users": "KADIR"}}, &ch, 200)
	admin.mustDo("POST", "/api/webpush/subscriptions", subscription(t, ps.srv.URL+"/gone"), nil, 200)
	admin.mustDo("GET", "/api/push/"+other.PushToken+"?status=up", nil, nil, 200)
	waitFor(t, "düzelme bildirimi gitti", func() bool { return ps.count("/gone") >= 1 && ps.count("/admin-1") >= 3 })
	waitFor(t, "410 aboneliği silindi", func() bool {
		admin.mustDo("GET", "/api/webpush/subscriptions", nil, &subs, 200)
		return len(subs) == 1
	})
	before := ps.count("/cust-1")
	admin.mustDo("GET", "/api/push/"+mine.PushToken+"?status=up", nil, nil, 200)
	waitFor(t, "müşteri sitesi düzeldi", func() bool { return ps.count("/admin-1") >= 4 })
	if ps.count("/cust-1") != before {
		t.Fatal("kanal yalnızca kadir'e ayarlıyken müşteri almamalı")
	}
	// Cihaz silme ve başkasının cihazını silememe.
	cust.mustDo("GET", "/api/webpush/subscriptions", nil, &subs, 200)
	admin.mustDo("DELETE", fmt.Sprintf("/api/webpush/subscriptions/%d", subs[0].ID), nil, nil, 404)
	cust.mustDo("DELETE", fmt.Sprintf("/api/webpush/subscriptions/%d", subs[0].ID), nil, nil, 200)
	cust.mustDo("POST", "/api/webpush/test", nil, nil, 400)
}
