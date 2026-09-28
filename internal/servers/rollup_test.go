package servers

import (
	"math"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

func TestAggregate(t *testing.T) {
	if got := Aggregate(nil); got.CPU != 0 || got.Disks != nil {
		t.Fatalf("boş: %+v", got)
	}
	in := []metrics.Stats{
		{
			CPU: 10, Load1: 1, Load5: 2, Load15: 3, MemTotal: 1000, MemUsed: 100, MemCache: 10, SwapTotal: 50, SwapUsed: 5,
			DiskReadBps: 100, DiskWriteBps: 200, NetRxBps: 300, NetTxBps: 400, Uptime: 60,
			Disks:      []metrics.Disk{{Mount: "/", Total: 100, Used: 50}},
			Temps:      []metrics.Temp{{Name: "cpu", C: 40}, {Name: "nvme", C: 30}},
			Containers: []metrics.Container{{ID: "a1", Name: "web", CPU: 10, Mem: 100, NetRxBps: 10}, {Name: "db", CPU: 4, Mem: 400}},
		},
		{
			CPU: 30, Load1: 3, Load5: 2, Load15: 1, MemTotal: 1000, MemUsed: 301, MemCache: 30, SwapTotal: 50, SwapUsed: 15,
			DiskReadBps: 300, DiskWriteBps: 0, NetRxBps: 100, NetTxBps: 0, Uptime: 120,
			Disks:      []metrics.Disk{{Mount: "/", Total: 100, Used: 60}, {Mount: "/data", Total: 10, Used: 1}},
			Temps:      []metrics.Temp{{Name: "cpu", C: 60}},
			Containers: []metrics.Container{{ID: "a2", Name: "web", CPU: 20, Mem: 300, NetRxBps: 30, MemLimit: 999}, {Name: "job", CPU: 50, Mem: 1}},
		},
	}
	got := Aggregate(in)
	if got.CPU != 20 || got.CPUMax != 30 || got.Load1 != 2 || got.Load5 != 2 || got.Load15 != 2 {
		t.Fatalf("cpu/yük: %+v", got)
	}
	if got.MemTotal != 1000 || got.MemUsed != 201 || got.MemCache != 20 || got.SwapUsed != 10 || got.SwapTotal != 50 {
		t.Fatalf("bellek: %+v", got)
	}
	if got.DiskReadBps != 200 || got.DiskWriteBps != 100 || got.NetRxBps != 200 || got.NetTxBps != 200 || got.Uptime != 120 {
		t.Fatalf("hızlar: %+v", got)
	}
	// Diskler son örnekten.
	if len(got.Disks) != 2 || got.Disks[0].Used != 60 {
		t.Fatalf("diskler: %+v", got.Disks)
	}
	// Sıcaklık sensör başına ortalama (yalnızca göründüğü örneklerde).
	if len(got.Temps) != 2 || got.Temps[0] != (metrics.Temp{Name: "cpu", C: 50}) || got.Temps[1] != (metrics.Temp{Name: "nvme", C: 30}) {
		t.Fatalf("sıcaklıklar: %+v", got.Temps)
	}
	// Konteynerler: adların birleşimi, ada göre ortalama, son kimlik ve sınır.
	want := []metrics.Container{
		{ID: "a2", Name: "web", CPU: 15, Mem: 200, NetRxBps: 20, MemLimit: 999},
		{Name: "db", CPU: 4, Mem: 400},
		{Name: "job", CPU: 50, Mem: 1},
	}
	if len(got.Containers) != len(want) {
		t.Fatalf("konteynerler: %+v", got.Containers)
	}
	for i := range want {
		if got.Containers[i] != want[i] {
			t.Errorf("konteyner %d: %+v, %+v bekleniyordu", i, got.Containers[i], want[i])
		}
	}

	// Saatlik özet 10 dk özetlerinden: tepe CPU, özetlerin kendi tepelerinden.
	hour := Aggregate([]metrics.Stats{{CPU: 10, CPUMax: 70}, {CPU: 20, CPUMax: 40}})
	if hour.CPU != 15 || hour.CPUMax != 70 {
		t.Fatalf("saatlik: %+v", hour)
	}
}

func TestAggregateContainerLimit(t *testing.T) {
	var a, b metrics.Stats
	for i := range metrics.MaxContainers {
		a.Containers = append(a.Containers, metrics.Container{Name: "a" + string(rune('A'+i%26)) + string(rune('0'+i/26)), CPU: 1})
		b.Containers = append(b.Containers, metrics.Container{Name: "b" + string(rune('A'+i%26)) + string(rune('0'+i/26)), CPU: 2})
	}
	got := Aggregate([]metrics.Stats{a, b})
	if len(got.Containers) != metrics.MaxContainers {
		t.Fatalf("sınır: %d", len(got.Containers))
	}
	for _, c := range got.Containers {
		if c.CPU != 2 {
			t.Fatalf("en çok CPU kullananlar kalmalı: %+v", c)
		}
	}
}

func TestWindowAvg(t *testing.T) {
	var h []point
	for i := range 10 {
		h = append(h, point{t: int64(i * 60), cpu: float64(i * 10)})
	}
	// Son 3 dakika: 420, 480, 540 → 70, 80, 90.
	if v, ok := windowAvg(h, MetricCPU, "", 3, 540); !ok || v != 80 {
		t.Fatal(v, ok)
	}
	// 10 dk pencerede 10 örnek var: yeterli (en az 8).
	if v, ok := windowAvg(h, MetricCPU, "", 10, 540); !ok || v != 45 {
		t.Fatal(v, ok)
	}
	// 20 dk pencerede en az 16 örnek gerekir.
	if _, ok := windowAvg(h, MetricCPU, "", 20, 540); ok {
		t.Fatal("yetersiz örnekle değerlendirme yapılmamalı")
	}
	// Sıcaklık sensörü yoksa değerlendirilmez.
	if _, ok := windowAvg(h, MetricTemp, "", 1, 540); ok {
		t.Fatal("sensörsüz sıcaklık")
	}
	if minCoverage(1) != 1 || minCoverage(3) != 2 || minCoverage(10) != 8 || minCoverage(60) != 48 {
		t.Fatal("kapsama")
	}
	p := pointOf(0, &metrics.Stats{Load1: 4, MemTotal: 200, MemUsed: 50}, &metrics.Host{Threads: 8})
	if p.load != 0.5 || p.mem != 25 || math.Abs(p.disk) != 0 {
		t.Fatalf("nokta: %+v", p)
	}
}
