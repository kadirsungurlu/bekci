package api

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/probe"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Uçtan uca: gerçek bir kontrol noktası istemcisi (internal/probe) test
// sunucusundaki API'ye bağlanır, işleri alır, siteyi kontrol eder ve
// sonuçları gönderir. Aralıklar milisaniye cinsinden.
func TestProbeEndToEnd(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	ctx := context.Background()

	var failing atomic.Bool
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() {
			w.WriteHeader(503)
			return
		}
		io.WriteString(w, "merhaba")
	}))
	defer site.Close()

	cp := admin.newProbe("Frankfurt")
	var mon monitorView
	admin.mustDo("POST", "/api/monitors", map[string]any{
		"name": "Site", "type": "http", "interval": 50, "retry_interval": 50, "max_retries": 1, "timeout": 300,
		"config": map[string]any{"url": site.URL},
	}, &mon, 201)
	admin.setLocations(mon.ID, map[string]any{"include_local": false, "probe_ids": []int64{cp.Probe.ID}}, 200)
	get := func() store.Monitor {
		m, err := f.st.GetMonitor(ctx, mon.ID)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}

	startProbe := func() (*probe.Client, func()) {
		c, err := probe.New(probe.Config{
			Server: admin.srv.URL, Token: cp.Token, Version: "e2e", Unit: time.Millisecond, AllowInsecure: true,
			FlushEvery: 10 * time.Millisecond, MaxBackoff: 50 * time.Millisecond,
			Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		})
		if err != nil {
			t.Fatal(err)
		}
		pctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { c.Run(pctx); close(done) }()
		return c, func() { cancel(); <-done }
	}
	client, stop := startProbe()
	defer func() { stop() }()

	waitFor(t, "UP", func() bool { m := get(); return m.Status == store.StatusUp })
	// UP ana sunucunun kendi kontrolünden, kontrol noktası ilk isteğini atmadan
	// gelebilir; çevrimiçi olmasını ayrıca bekle.
	var probes []probeAdminView
	waitFor(t, "kontrol noktası çevrimiçi", func() bool {
		admin.mustDo("GET", "/api/probes", nil, &probes, 200)
		return probes[0].Online
	})
	if !probes[0].Online || probes[0].Version != "e2e" || probes[0].MonitorCount != 1 {
		t.Fatalf("kontrol noktası: %+v", probes[0])
	}

	// Site çöker: tekrar denemeden sonra DOWN, mesajda konum adı.
	failing.Store(true)
	waitFor(t, "DOWN", func() bool {
		m := get()
		return m.Status == store.StatusDown && strings.HasPrefix(m.LastMessage, "Frankfurt: ") && strings.Contains(m.LastMessage, "503")
	})
	failing.Store(false)
	waitFor(t, "tekrar UP", func() bool { return get().Status == store.StatusUp })
	incidents, _ := f.st.ListIncidents(ctx, store.IncidentFilter{MonitorID: mon.ID})
	if len(incidents) != 1 || incidents[0].ResolvedAt == 0 || !strings.HasPrefix(incidents[0].Cause, "Frankfurt: ") {
		t.Fatalf("olay: %+v", incidents)
	}

	// Kontrol noktası kapanır: sonuçlar bayatlayınca monitör PENDING olur (DOWN değil).
	stop()
	waitFor(t, "sonuç gelmiyor", func() bool {
		m := get()
		return m.Status == store.StatusPending && m.LastMessage == engine.NoLocationData
	})
	if n, _ := f.st.ListIncidents(ctx, store.IncidentFilter{MonitorID: mon.ID}); len(n) != 1 {
		t.Fatal("kontrol noktasının kapanması kesinti açmamalı")
	}

	// Yeniden başlar; sonra yönetici devre dışı bırakır: kontroller hemen durur.
	client, stop = startProbe()
	waitFor(t, "yeniden UP", func() bool { return get().Status == store.StatusUp })
	if len(client.Jobs()) != 1 {
		t.Fatalf("işler: %v", client.Jobs())
	}
	admin.mustDo("PUT", fmt.Sprintf("/api/probes/%d", cp.Probe.ID), map[string]any{"name": "Frankfurt", "active": false}, nil, 200)
	waitFor(t, "işler durdu", func() bool { return len(client.Jobs()) == 0 })
	waitFor(t, "devre dışı → PENDING", func() bool {
		m := get()
		return m.Status == store.StatusPending && m.LastMessage == engine.NoLocationData
	})
}
