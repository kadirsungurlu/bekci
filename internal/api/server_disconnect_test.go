package api

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/servers"
)

// Sunucu ajanı uzun yoklama bağlantısını kopardığında (durduruldu, silindi)
// sunucu 3 dakikayı beklemeden çevrimdışı görünür; yeniden bağlanınca çevrimiçi.
func TestServerAgentDisconnectShowsOffline(t *testing.T) {
	f := newServersEnv(t)
	admin := f.env
	f.s.probeHold = 10 * time.Second
	f.s.probeReconnect = 300 * time.Millisecond
	var cp serverSetup
	admin.mustDo("POST", "/api/servers", map[string]any{"name": "CP Server"}, &cp, 201)
	if code, _, body := admin.rawReq("POST", "/api/probe/metrics", bearer(cp.Token), testSample(5)); code != 204 {
		t.Fatalf("örnek: %d %s", code, body)
	}
	state := func() string {
		var list struct {
			Servers []servers.View `json:"servers"`
		}
		admin.mustDo("GET", "/api/servers", nil, &list, 200)
		return list.Servers[0].State
	}
	if s := state(); s != servers.StateOnline {
		t.Fatalf("örnekten sonra çevrimiçi olmalı: %s", s)
	}
	first := admin.pollJobs(cp.Token, "?since=0")
	since := fmt.Sprintf("?since=%d", *first.resp.Version)

	// Bekleyen istek ajan tarafından koparılır (konteyner silindi).
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan polled, 1)
	go func() { done <- admin.pollJobsAsync(ctx, cp.Token, since) }()
	time.Sleep(200 * time.Millisecond)
	if s := state(); s != servers.StateOnline {
		t.Fatalf("bağlıyken çevrimiçi olmalı: %s", s)
	}
	cancel()
	<-done
	deadline := time.Now().Add(2 * time.Second)
	for state() != servers.StateOffline {
		if time.Now().After(deadline) {
			t.Fatalf("bağlantı kopunca çevrimdışı olmalı: %s", state())
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Yeniden bağlanınca çevrimiçi.
	ch := admin.goPoll(cp.Token, since)
	deadline = time.Now().Add(2 * time.Second)
	for state() != servers.StateOnline {
		if time.Now().After(deadline) {
			t.Fatalf("yeniden bağlanınca çevrimiçi olmalı: %s", state())
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = ch
}
