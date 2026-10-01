package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// Bulgu (O-5): geçici hatada (5xx/429/ağ) bildirim bir kez denenip
// kayboluyordu. İki ek deneme yapılır; deneme sayısı olayın geçmişine yazılır.
// Kalıcı hata (401) yeniden denenmez.
func TestDispatcherRetriesTransientErrors(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	var flaky, denied atomic.Int32
	flakySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flaky.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer flakySrv.Close()
	deniedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		denied.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer deniedSrv.Close()

	a := store.Notification{Name: "Kararsız", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"` + flakySrv.URL + `"}`)}
	b := store.Notification{Name: "Yetkisiz", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"` + deniedSrv.URL + `"}`)}
	for _, n := range []*store.Notification{&a, &b} {
		if err := st.CreateNotification(ctx, n, false); err != nil {
			t.Fatal(err)
		}
	}
	m := store.Monitor{Name: "site", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30,
		Config: json.RawMessage(`{"url":"https://example.com"}`)}
	if err := st.CreateMonitor(ctx, &m, []int64{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	inc, _ := st.StartIncident(ctx, m.ID, 1000, "HTTP 500")

	d := NewDispatcher(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.retryDelays = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond}
	d.Notify(Event{Kind: KindDown, MonitorID: m.ID, MonitorName: "site", Time: time.Now(), IncidentID: inc})
	d.Wait(5 * time.Second)

	if n := flaky.Load(); n != 2 {
		t.Errorf("kararsız kanal 2 kez denenmeli (503 → 200): %d", n)
	}
	if n := denied.Load(); n != 1 {
		t.Errorf("401 yeniden denenmemeli: %d", n)
	}
	evs, _ := st.IncidentEvents(ctx, inc)
	byName := map[string]deliveryData{}
	for _, ev := range evs {
		var dd deliveryData
		json.Unmarshal(ev.Data, &dd)
		byName[dd.Channel] = dd
	}
	if dd := byName["Kararsız"]; !dd.OK || dd.Attempts != 2 {
		t.Errorf("kararsız kanal kaydı: %+v", dd)
	}
	if dd := byName["Yetkisiz"]; dd.OK || dd.Attempts != 0 {
		t.Errorf("yetkisiz kanal kaydı: %+v", dd)
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("HTTP 503 (hedef sunucu yanıt vermiyor)"), true},
		{errors.New("HTTP 429"), true},
		{errors.New("HTTP 500"), true},
		{errors.New("HTTP 401 (yetki hatası)"), false},
		{errors.New("HTTP 404"), false},
		{errors.New("HTTP 400"), false},
		{context.DeadlineExceeded, true},
		{errors.New("bilinmeyen bildirim tipi: x"), false},
	}
	for _, c := range cases {
		if got := retryable(c.err); got != c.want {
			t.Errorf("retryable(%v) = %v, %v bekleniyordu", c.err, got, c.want)
		}
	}
}

// Kapanışta bekleyen yeniden deneme beklemez.
func TestRetryStopsOnShutdown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer srv.Close()
	d := NewDispatcher(nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.retryDelays = []time.Duration{10 * time.Second}
	done := make(chan int, 1)
	go func() {
		n, _ := d.sendRetry("webhook", json.RawMessage(`{"url":"`+srv.URL+`"}`), Event{Kind: KindDown, MonitorName: "x", Time: time.Now()})
		done <- n
	}()
	time.Sleep(50 * time.Millisecond)
	d.Stop()
	d.Wait(time.Second)
	select {
	case n := <-done:
		if n != 1 {
			t.Fatalf("kapanışta ek deneme yapılmamalı: %d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("kapanış yeniden deneme beklemesini kesmeli")
	}
}
