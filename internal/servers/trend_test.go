package servers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func disk(used uint64) metrics.Stats {
	return metrics.Stats{MemTotal: 100, MemUsed: 10, Disks: []metrics.Disk{{Mount: "/data", Total: 100 * gb, Used: used}}}
}

func onlyRule(t *testing.T, e *testEnv, r store.ServerAlert) {
	t.Helper()
	if _, err := e.st.ReplaceServerAlerts(context.Background(), e.probe.ID, []store.ServerAlert{r}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
}

func TestDiskFastAlert(t *testing.T) {
	e := newTestEnv(t)
	e.send(disk(40 * gb))
	onlyRule(t, e, store.ServerAlert{Metric: MetricDiskFast, Threshold: 5, Minutes: 60, Active: true})
	e.notif.take()
	for range 5 {
		e.send(disk(40 * gb))
	}
	if ev := e.notif.take(); len(ev) != 0 {
		t.Fatalf("erken: %+v", ev)
	}
	e.send(disk(46 * gb))
	ev := e.notif.take()
	if len(ev) != 1 || ev[0].Kind != notify.KindServerAlert || ev[0].Metric != MetricDiskFast || ev[0].Mount != "/data" || ev[0].Level != store.LevelWarning || ev[0].Threshold != 5 {
		t.Fatalf("%+v", ev)
	}
	inc, err := e.st.TimelineEvents(context.Background(), store.TimelineFilter{ProbeIDs: []int64{e.probe.ID}, Kinds: []string{EventAlert}, Limit: 5})
	if err != nil || len(inc) != 1 || inc[0].Subject != "disk_fast:/data" || inc[0].Level != store.EventWarning {
		t.Fatalf("%+v %v", inc, err)
	}
}

func TestDiskForecastAlert(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(disk(80 * gb))
	onlyRule(t, e, store.ServerAlert{Metric: MetricDiskForecast, Threshold: 7, Minutes: 1, Active: true})
	e.notif.take()
	// Geçmiş: 3 gündür saatte 0,25 GB (kalan 20 GB, yaklaşık 3,3 gün).
	base := e.clock.Unix() - e.clock.Unix()%3600
	for i := 72; i > 0; i-- {
		data, _ := json.Marshal(disk(80*gb - uint64(i)*gb/4))
		if err := e.st.UpsertServerStats(ctx, e.probe.ID, store.ServerRes60, base-int64(i)*3600, data); err != nil {
			t.Fatal(err)
		}
	}
	e.clock = e.clock.Add(time.Duration(forecastRefresh) * time.Second)
	e.send(disk(80 * gb))
	ev := e.notif.take()
	if len(ev) != 1 || ev[0].Metric != MetricDiskForecast || ev[0].Mount != "/data" || ev[0].Value > 4 || ev[0].Value < 2.5 || ev[0].Threshold != 7 {
		t.Fatalf("%+v", ev)
	}
	list := e.svc.DiskForecasts(e.probe.ID)
	if len(list) != 1 || list[0].Status != ForecastGrowing {
		t.Fatalf("%+v", list)
	}
	if title := ev[0].Title(); title == "" {
		t.Fatal("başlık yok")
	}
	// Temizlik: tahmin bitti, uyarı kapanır.
	e.clock = e.clock.Add(time.Duration(forecastRefresh) * time.Second)
	e.send(disk(30 * gb))
	ev = e.notif.take()
	if len(ev) != 1 || ev[0].Kind != notify.KindServerResolved || ev[0].Mount != "/data" {
		t.Fatalf("%+v", ev)
	}
}

func TestOOMEventStored(t *testing.T) {
	e := newTestEnv(t)
	smp := metrics.Sample{Time: 1, Host: &metrics.Host{Hostname: "cp1"}, Stats: &metrics.Stats{}, Events: []metrics.HostEvent{{Time: e.clock.Unix(), Kind: "oom", Subject: "java", PID: 9, Count: 1}}}
	if err := e.svc.Ingest(context.Background(), e.getProbe(), smp); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ingest(context.Background(), e.getProbe(), smp); err != nil { // ajanın yeniden gönderimi
		t.Fatal(err)
	}
	l, err := e.st.TimelineEvents(context.Background(), store.TimelineFilter{ProbeIDs: []int64{e.probe.ID}, Limit: 5})
	if err != nil || len(l) != 1 || l[0].Kind != EventOOM || l[0].Subject != "java" {
		t.Fatalf("%+v %v", l, err)
	}
}
