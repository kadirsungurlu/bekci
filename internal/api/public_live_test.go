package api

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Genel canlı akış: sayfadaki monitörün kontrol sonucu sinyal üretir; akışta
// yalnızca "update" sinyali vardır (iç olay verisi sızmaz); ilgisiz monitör
// sinyal üretmez; şifreli sayfa çerezsiz açılmaz.
func TestPublicLiveStream(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var a, b monitorView
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Sayfadaki", "type": "push", "interval": 60000, "config": map[string]any{}}, &a, 201)
	e.mustDo("POST", "/api/monitors", map[string]any{"name": "Disarida", "type": "push", "interval": 60000, "config": map[string]any{}}, &b, 201)
	e.mustDo("POST", "/api/status-pages", map[string]any{"slug": "canli", "title": "Canli", "published": true,
		"sections": []map[string]any{{"title": "Web", "monitors": []map[string]any{{"id": a.ID}}}}}, nil, 201)
	e.mustDo("POST", "/api/status-pages", map[string]any{"slug": "kilitli", "title": "Kilitli", "published": true, "password": "gizli",
		"sections": []map[string]any{}}, nil, 201)

	if code := e.do("GET", "/api/public/pages/yok/events", nil, nil); code != 404 {
		t.Fatalf("bilinmeyen sayfa: %d", code)
	}
	resp, err := http.Get(e.srv.URL + "/api/public/pages/kilitli/events")
	if err != nil || resp.StatusCode != 401 {
		t.Fatalf("şifreli sayfa çerezsiz 401 olmalı: %v %v", resp.StatusCode, err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", e.srv.URL+"/api/public/pages/canli/events", nil)
	resp, err = http.DefaultClient.Do(req) // oturumsuz istemci
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("akış açılmadı: %v %v", resp.StatusCode, err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("içerik türü %q", ct)
	}
	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if l := sc.Text(); strings.HasPrefix(l, "data:") {
				lines <- l
			}
		}
		close(lines)
	}()
	push := func(m monitorView, q string) {
		r, err := http.Get(e.srv.URL + "/api/push/" + m.PushToken + q)
		if err != nil || r.StatusCode != 200 {
			t.Fatalf("push: %v", err)
		}
		r.Body.Close()
	}

	// İlgisiz monitör: sinyal yok.
	push(b, "?status=down&msg=ic-ayrinti-10.0.0.5")
	select {
	case l := <-lines:
		t.Fatalf("ilgisiz monitör sinyal üretti: %s", l)
	case <-time.After(publicLiveFlush + 700*time.Millisecond):
	}

	// Sayfadaki monitör: tek "update" sinyali, iç ayrıntı yok.
	push(a, "?status=down&msg=ic-ayrinti-10.0.0.5&ping=7")
	select {
	case l := <-lines:
		if l != `data: {"type":"update"}` {
			t.Fatalf("akışta beklenmeyen veri: %s", l)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("sayfadaki monitör için sinyal gelmedi")
	}
	// Sinyalden sonra genel uç önbellekten eski veriyi vermemeli.
	var pg struct {
		Status string `json:"status"`
	}
	e.mustDo("GET", "/api/public/pages/canli", nil, &pg, 200)
	if pg.Status != "down" {
		t.Fatalf("sinyalden sonra sayfa durumu %q, down bekleniyordu", pg.Status)
	}
}

// Genel akış sayısı IP başına sınırlıdır.
func TestPublicLiveLimits(t *testing.T) {
	l := newPublicLive()
	var chans []chan struct{}
	for i := 0; i < publicLiveMaxPerIP; i++ {
		ch, ok := l.add("s", "1.2.3.4")
		if !ok {
			t.Fatalf("%d. akış reddedildi", i+1)
		}
		chans = append(chans, ch)
	}
	if _, ok := l.add("s", "1.2.3.4"); ok {
		t.Fatal("IP sınırı aşıldı")
	}
	if _, ok := l.add("s", "5.6.7.8"); !ok {
		t.Fatal("başka IP reddedildi")
	}
	l.remove("s", "1.2.3.4", chans[0])
	if _, ok := l.add("s", "1.2.3.4"); !ok {
		t.Fatal("kapanan akışın yeri açılmadı")
	}
	if got := fmt.Sprint(l.total); got != fmt.Sprint(publicLiveMaxPerIP+1) {
		t.Fatalf("toplam %s", got)
	}
}
