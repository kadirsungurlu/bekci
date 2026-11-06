package api

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

func perfInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

// TestTopologyScale GET /api/topology süresini ve boyutunu ölçer; sonuçlar
// docs/TOPOLOJI-OLCEK.md. Yalnız elle: BEKCI_PERF_TOPO=1 UPTIME_TEST_PG=<timescale>.
func TestTopologyScale(t *testing.T) {
	if os.Getenv("BEKCI_PERF_TOPO") == "" {
		t.Skip("BEKCI_PERF_TOPO yok")
	}
	nMon, nSrv, nLoc := perfInt("BEKCI_PERF_MON", 1000), perfInt("BEKCI_PERF_SRV", 100), perfInt("BEKCI_PERF_LOC", 30)
	ctx := context.Background()
	st := storetest.Open(t, time.UTC)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer target.Close()
	now := time.Now().Unix()
	seedAt := time.Now()

	var locs, srvs []int64
	for i := range nLoc {
		p := store.Probe{Name: fmt.Sprintf("Nokta %02d", i), Kind: store.ProbeKindLocation, Active: true, Hash: fmt.Sprintf("n%d", i)}
		p.Region, p.Provider, p.Code = "Avrupa", "Hetzner", fmt.Sprintf("N%02d", i)
		if err := st.CreateProbe(ctx, &p); err != nil {
			t.Fatal(err)
		}
		if err := st.SetProbeLabels(ctx, p.ID, p.ProbeLabels); err != nil {
			t.Fatal(err)
		}
		locs = append(locs, p.ID)
	}
	for i := range nSrv {
		p := store.Probe{Name: fmt.Sprintf("sunucu-%03d", i), Kind: store.ProbeKindServer, Active: true, Hash: fmt.Sprintf("s%d", i)}
		p.Role, p.Region = "Web", "Frankfurt"
		if err := st.CreateProbe(ctx, &p); err != nil {
			t.Fatal(err)
		}
		if err := st.SetProbeLabels(ctx, p.ID, p.ProbeLabels); err != nil {
			t.Fatal(err)
		}
		srvs = append(srvs, p.ID)
		data, _ := json.Marshal(metrics.Stats{CPU: 31.5, MemTotal: 1 << 34, MemUsed: 1 << 33,
			Disks: []metrics.Disk{{Mount: "/", Device: "/dev/sda1", FS: "ext4", Total: 1 << 38, Used: 1 << 37}}})
		if _, err := st.SaveServerSample(ctx, store.ServerSample{ProbeID: p.ID, Time: now - now%60, At: now, Data: data}); err != nil {
			t.Fatal(err)
		}
	}
	var chans []int64
	for i := range 20 {
		n := store.Notification{Name: fmt.Sprintf("Kanal %d", i), Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"https://example.test/h"}`)}
		if err := st.CreateNotification(ctx, &n, false); err != nil {
			t.Fatal(err)
		}
		chans = append(chans, n.ID)
		if err := st.SetChannelSend(ctx, n.ID, now-60, ""); err != nil {
			t.Fatal(err)
		}
	}
	var tags []int64
	for i := range 30 {
		tg := store.Tag{Name: fmt.Sprintf("etiket-%d", i), Color: "#336699"}
		if err := st.CreateTag(ctx, &tg); err != nil {
			t.Fatal(err)
		}
		tags = append(tags, tg.ID)
	}
	mons := make([]int64, nMon)
	var wg sync.WaitGroup
	work := make(chan int)
	errs := make(chan error, 16)
	for range 12 {
		wg.Go(func() {
			for i := range work {
				cfg, _ := json.Marshal(map[string]any{"url": target.URL + "/" + strconv.Itoa(i)})
				m := store.Monitor{Name: fmt.Sprintf("Site %04d", i), Type: "http", Active: true, Interval: 300, Timeout: 10,
					Config: cfg, ServerID: srvs[i%len(srvs)]}
				err := st.CreateMonitor(ctx, &m, []int64{chans[i%len(chans)], chans[(i+7)%len(chans)]})
				if err == nil {
					mons[i] = m.ID
					err = st.SetMonitorLocations(ctx, m.ID, store.LocationSetup{IncludeLocal: true, DownWhen: "all",
						ProbeIDs: []int64{locs[i%len(locs)], locs[(i+1)%len(locs)], locs[(i+2)%len(locs)]}})
				}
				if err == nil {
					err = st.SetMonitorTags(ctx, m.ID, []store.MonitorTagInput{{TagID: tags[i%len(tags)]}})
				}
				for k := range 30 {
					if err != nil {
						break
					}
					status := store.StatusUp
					if i%33 == 0 && k == 29 {
						status = store.StatusDown
					}
					err = st.RecordBeat(ctx, store.BeatUpdate{Beat: store.Beat{MonitorID: m.ID, Time: now - int64(30-k)*300,
						Status: status, PingMs: int64(40 + (i+k)%200), Message: "200 - OK"}})
				}
				if err == nil && i%33 == 0 {
					err = st.OpenIncident(ctx, m.ID, now-600, "HTTP 503")
				}
				if err != nil {
					select {
					case errs <- err:
					default:
					}
				}
			}
		})
	}
	for i := range nMon {
		work <- i
	}
	close(work)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
	var links []store.ProbeLink
	for _, a := range locs {
		for _, b := range locs {
			if a != b {
				links = append(links, store.ProbeLink{Src: a, Dst: b, State: "up", RTTms: 12, LastOKAt: now, UpdatedAt: now})
			}
		}
	}
	if err := st.UpsertProbeLinks(ctx, links); err != nil {
		t.Fatal(err)
	}
	t.Logf("tohum: %s", time.Since(seedAt).Round(time.Second))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	hub := engine.NewHub()
	disp := notify.NewDispatcher(st, log)
	eng := engine.New(st, disp, hub, log, engine.Config{Unit: time.Second, BaseURL: "https://uptime.test"})
	runCtx, cancel := context.WithCancel(ctx)
	if err := eng.Start(runCtx); err != nil {
		t.Fatal(err)
	}
	s := New(st, eng, hub, disp, log, fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}}, "test")
	srv := httptest.NewServer(s.Handler())
	jar, _ := cookiejar.New(nil)
	defer func() { srv.CloseClientConnections(); srv.Close(); cancel(); eng.Wait(); disp.Wait(time.Second) }()
	e := &env{t: t, srv: srv, client: &http.Client{Jar: jar, Timeout: 30 * time.Second}}
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	for i := range 20 {
		e.newUser(fmt.Sprintf("musteri%d", i), "viewer", mons[i*20:i*20+25])
	}
	time.Sleep(3 * time.Second) // ping geçmişi tohumu ve motor açılışı

	raw := &http.Client{Jar: jar, Timeout: 30 * time.Second, Transport: &http.Transport{DisableCompression: true}}
	measure := func(label, path string, reps int) {
		var times []time.Duration
		var size, zsize int
		for i := range reps + 1 {
			req, _ := http.NewRequest("GET", srv.URL+path, nil)
			req.Header.Set("Accept-Encoding", "gzip")
			t0 := time.Now()
			resp, err := raw.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			d := time.Since(t0)
			if resp.StatusCode != 200 || resp.Header.Get("Content-Encoding") != "gzip" {
				t.Fatalf("%s: %d %q", path, resp.StatusCode, resp.Header.Get("Content-Encoding"))
			}
			if i == 0 {
				t.Logf("%s soğuk: %s", label, d.Round(time.Millisecond))
				continue
			}
			times = append(times, d)
			zsize = len(body)
			zr, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			plain, _ := io.ReadAll(zr)
			size = len(plain)
		}
		slices.Sort(times)
		t.Logf("%s: medyan %s, p95 %s, en çok %s, ham %d KB, gzip %d KB", label,
			times[len(times)/2].Round(100*time.Microsecond), times[len(times)*95/100].Round(100*time.Microsecond),
			times[len(times)-1].Round(100*time.Microsecond), size/1024, zsize/1024)
	}
	measure("tam", "/api/topology", perfInt("BEKCI_PERF_REPS", 40))
	measure("özet", "/api/topology?view=summary", perfInt("BEKCI_PERF_REPS", 40))
	if os.Getenv("BEKCI_PERF_PARTS") == "" {
		return
	}
	part := func(name string, f func() error) {
		best := time.Hour
		for range 5 {
			t0 := time.Now()
			if err := f(); err != nil {
				t.Fatal(name, err)
			}
			best = min(best, time.Since(t0))
		}
		t.Logf("parça %-24s %s", name, best.Round(100*time.Microsecond))
	}
	part("ListMonitors", func() error { _, err := st.ListMonitors(ctx); return err })
	part("ListProbesOfKind", func() error { _, err := st.ListProbesOfKind(ctx, store.ProbeKindServer); return err })
	part("OpenPartial", func() error { _, err := st.OpenPartialIncidentIDs(ctx); return err })
	part("MonitorTags", func() error { _, err := st.MonitorTags(ctx); return err })
	part("ListIncidents", func() error { _, err := st.ListIncidents(ctx, store.IncidentFilter{Open: true, Limit: 500}); return err })
	part("LastMonitorIncidents", func() error { _, err := st.LastMonitorIncidents(ctx); return err })
	part("MonitorNotificationIDs", func() error { _, err := st.MonitorNotificationIDs(ctx); return err })
	part("TagNotificationIDs", func() error { _, err := st.TagNotificationIDs(ctx); return err })
	part("AllProbeNotificationIDs", func() error { _, err := st.AllProbeNotificationIDs(ctx); return err })
	part("AllServerAlerts", func() error { _, err := st.AllServerAlerts(ctx); return err })
	part("ChannelSendCounts", func() error { _, err := st.ChannelSendCounts(ctx, now-86400); return err })
	part("ChannelSends", func() error { _, err := st.ChannelSends(ctx); return err })
	part("ListNotifications", func() error { _, err := st.ListNotifications(ctx); return err })
	part("ListUsers", func() error { _, err := st.ListUsers(ctx); return err })
	part("ListProbeLinks", func() error { _, err := st.ListProbeLinks(ctx); return err })
}
