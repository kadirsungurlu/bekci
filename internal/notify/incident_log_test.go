package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// Gönderim sonuçları olayın işlem geçmişine yazılır: başarılı ve başarısız
// kanallar, gizli bilgisi temizlenmiş hata; sunucu uyarıları yazılmaz.
func TestDispatcherLogsDeliveries(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ok.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	defer bad.Close()

	good := store.Notification{Name: "Kanal A", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"` + ok.URL + `"}`)}
	failing := store.Notification{Name: "Kanal B", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"` + bad.URL + `/gizli-yol?token=abc"}`)}
	for _, n := range []*store.Notification{&good, &failing} {
		if err := st.CreateNotification(ctx, n, false); err != nil {
			t.Fatal(err)
		}
	}
	m := store.Monitor{Name: "site", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30,
		Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := st.CreateMonitor(ctx, &m, []int64{good.ID, failing.ID}); err != nil {
		t.Fatal(err)
	}
	lonely := store.Monitor{Name: "kanalsız", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30,
		Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := st.CreateMonitor(ctx, &lonely, []int64{}); err != nil {
		t.Fatal(err)
	}
	inc, _ := st.StartIncident(ctx, m.ID, 1000, "HTTP 500")
	inc2, _ := st.StartIncident(ctx, lonely.ID, 1000, "HTTP 500")

	d := NewDispatcher(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Notify(Event{Kind: KindDown, MonitorID: m.ID, MonitorName: "site", Time: time.Now(), IncidentID: inc})
	d.Notify(Event{Kind: KindDown, MonitorID: lonely.ID, MonitorName: "kanalsız", Time: time.Now(), IncidentID: inc2})
	d.Notify(Event{Kind: KindDown, MonitorID: m.ID, MonitorName: "site", Time: time.Now()}) // olaysız: yazılmaz
	d.Notify(Event{Kind: KindServerAlert, ProbeID: 5, MonitorID: m.ID, IncidentID: inc, Metric: "cpu", Time: time.Now()})
	d.Wait(5 * time.Second)

	evs, _ := st.IncidentEvents(ctx, inc)
	if len(evs) != 2 {
		t.Fatalf("%d kayıt, 2 bekleniyordu: %+v", len(evs), evs)
	}
	byName := map[string]deliveryData{}
	for _, ev := range evs {
		var dd deliveryData
		json.Unmarshal(ev.Data, &dd)
		if ev.Kind != store.EventNotify || dd.Event != KindDown {
			t.Fatalf("kayıt: %+v", ev)
		}
		byName[dd.Channel] = dd
	}
	if a := byName["Kanal A"]; !a.OK || a.Type != "webhook" || a.ChannelID != good.ID || a.Error != "" {
		t.Errorf("başarılı kanal: %+v", a)
	}
	b := byName["Kanal B"]
	if b.OK || !strings.Contains(b.Error, "500") || strings.Contains(b.Error, "gizli-yol") || strings.Contains(b.Error, "token=abc") {
		t.Errorf("başarısız kanal: %+v", b)
	}

	evs2, _ := st.IncidentEvents(ctx, inc2)
	var dd deliveryData
	if len(evs2) != 1 || json.Unmarshal(evs2[0].Data, &dd) != nil || !dd.None || evs2[0].Message != "Bağlı etkin bildirim kanalı yok" {
		t.Fatalf("kanalsız monitör: %+v", evs2)
	}
}

func TestSanitizeSendError(t *testing.T) {
	cfg := json.RawMessage(`{"bot_token":"123456:ABCDEF-gizli","chat_id":"42"}`)
	err := &url.Error{Op: "Post", URL: "https://api.telegram.org/bot123456:ABCDEF-gizli/sendMessage", Err: errors.New("bağlantı reddedildi")}
	got := SanitizeSendError("telegram", cfg, err)
	if strings.Contains(got, "ABCDEF") || !strings.Contains(got, "https://api.telegram.org/…") {
		t.Errorf("url hatası: %s", got)
	}
	got = SanitizeSendError("telegram", cfg, errors.New("HTTP 401: token 123456:ABCDEF-gizli geçersiz, bkz https://x.example/yol/gizli?k=v"))
	if strings.Contains(got, "ABCDEF") || strings.Contains(got, "/yol/gizli") || !strings.Contains(got, Mask) {
		t.Errorf("metin hatası: %s", got)
	}
	long := SanitizeSendError("webhook", nil, errors.New(strings.Repeat("x", 500)))
	if n := len([]rune(long)); n > maxSendError+1 {
		t.Errorf("kısaltılmadı: %d", n)
	}
}
