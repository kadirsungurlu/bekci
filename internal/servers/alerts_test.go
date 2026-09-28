package servers

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

func TestResolveBelow(t *testing.T) {
	cases := []struct {
		metric    string
		threshold float64
		want      float64
	}{
		{MetricCPU, 90, 85.5}, // %5 = 4,5 puan
		{MetricMem, 20, 18},   // %5 = 1 < en az 2 puan
		{MetricDisk, 85, 80.75},
		{MetricLoad, 2, 1.9},      // %5 = 0,1
		{MetricLoad, 0.5, 0.45},   // %5 = 0,025 < en az 0,05
		{MetricLoad, 0.01, 0.005}, // pay eşiğin yarısını geçmez
		{MetricTemp, 80, 76},      // %5 = 4 °C
		{MetricTemp, 30, 28},      // en az 2 °C
		{MetricNet, 100, 95},      // yalnızca %5
		{MetricNet, 1, 0.95},
	}
	for _, c := range cases {
		if got := resolveBelow(c.metric, c.threshold); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("resolveBelow(%s, %v) = %v, %v bekleniyordu", c.metric, c.threshold, got, c.want)
		}
	}
}

// Bulgu: arayüz "≥" gösterirken eşiğe tam eşit ortalama uyarı başlatmıyordu;
// eşik çevresinde gidip gelen değer uyarıyı her dakika başlatıp bitiriyordu.
func TestAlertAtThresholdAndHysteresis(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1)) // varsayılan kurallar
	e.notif.take()
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricCPU, Threshold: 80, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	step := func(v float64, want string) {
		t.Helper()
		e.send(cpu(v))
		evs := e.notif.take()
		switch {
		case want == "" && len(evs) != 0:
			t.Fatalf("%v: bildirim beklenmiyordu: %+v", v, evs)
		case want != "" && (len(evs) != 1 || evs[0].Kind != want):
			t.Fatalf("%v: %s bekleniyordu: %+v", v, want, evs)
		}
	}
	step(79.9, "")
	step(80, notify.KindServerAlert) // eşiğe eşit: başlar (≥)
	step(79, "")                     // eşiğin hemen altı: sürer (bitiş sınırı 76)
	step(81, "")
	step(76, "") // sınırın kendisi: hâlâ sürer
	step(75.9, notify.KindServerResolved)
	step(79, "") // yeniden başlaması için eşiğe ulaşmalı
	step(80, notify.KindServerAlert)
}

// Bulgu: bölüme bağlı disk kuralı tetiklenmişken bölüm ayrılırsa (veya
// sıcaklık sensörü kaybolursa) kural sonsuza dek tetiklenmiş kalıyordu.
func TestAlertResolvesWhenValueVanishes(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	withData := func(used uint64) metrics.Stats {
		st := cpu(10)
		st.Disks = append(st.Disks, metrics.Disk{Mount: "/data", Total: 100, Used: used})
		st.Temps = []metrics.Temp{{Name: "cpu", C: 95}}
		return st
	}
	e.send(withData(50))
	e.notif.take()
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricDisk, Mount: "/data", Threshold: 85, Minutes: 1, Active: true},
		{Metric: MetricTemp, Threshold: 90, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	e.send(withData(95))
	if evs := e.notif.take(); len(evs) != 2 {
		t.Fatalf("disk ve sıcaklık uyarısı bekleniyordu: %+v", evs)
	}
	// Bölüm ayrıldı, sensör kayboldu; ajan örnek göndermeyi sürdürüyor.
	for i := range 9 {
		e.send(cpu(10))
		if evs := e.notif.take(); len(evs) != 0 {
			t.Fatalf("%d. dk: 10 dk dolmadan kapanmamalı: %+v", i+1, evs)
		}
	}
	e.send(cpu(10))
	evs := e.notif.take()
	if len(evs) != 2 {
		t.Fatalf("iki kural da kapanmalı: %+v", evs)
	}
	for _, ev := range evs {
		if ev.Kind != notify.KindServerResolved || !strings.Contains(ev.Message, "gelmiyor") {
			t.Errorf("bitiş: %+v", ev)
		}
		if ev.Metric == MetricDisk && (ev.Mount != "/data" || ev.Value != 95) {
			t.Errorf("disk bitişi bölümü ve son değeri taşımalı: %+v", ev)
		}
	}
	r := e.rules()
	if r[MetricDisk].Firing || r[MetricTemp].Firing {
		t.Fatalf("kurallar kapanmalı: %+v", r)
	}
}

func TestValueGone(t *testing.T) {
	var h []point
	for i := range 10 {
		h = append(h, point{t: int64(60 * (i + 1)), disks: map[string]float64{"/": 10}})
	}
	if !valueGone(h, MetricDisk, "/data", 1, 600) {
		t.Error("10 dk boyunca /data yok: kaybolmuş sayılmalı")
	}
	if valueGone(h, MetricDisk, "/", 1, 600) {
		t.Error("/ raporlanıyor")
	}
	if valueGone(h[:5], MetricDisk, "/data", 1, 600) {
		t.Error("ajan az örnek gönderdiyse (çevrimdışı) kaybolmuş sayılmamalı")
	}
	h[9].disks["/data"] = 50
	if valueGone(h, MetricDisk, "/data", 1, 600) {
		t.Error("son örnekte /data var")
	}
}
