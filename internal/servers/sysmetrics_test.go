package servers

import (
	"context"
	"strings"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// 730 §D: inode ve zombi kuralları. Eski ajan (alan yok) için kural hiç
// değerlendirilmez; yeni ajanda eşik aşılınca olay açılır, bölüm olayda yer alır.
func TestInodeZombieRules(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1)) // varsayılan kurallar
	e.notif.take()
	r := e.rules()
	if a, ok := r[MetricInode]; !ok || !a.Active || a.Threshold != 90 {
		t.Fatalf("inode varsayılanı (90, etkin) yok: %+v", a)
	}
	if a, ok := r[MetricZombies]; !ok || a.Active || a.Threshold != 50 {
		t.Fatalf("zombies varsayılanı (50, pasif) yok: %+v", a)
	}
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricInode, Threshold: 90, Minutes: 1, Active: true},
		{Metric: MetricZombies, Threshold: 50, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}

	// Eski ajan: disk var ama inode yok, zombi alanı yok -> değerlendirilmez.
	old := func() metrics.Stats {
		st := cpu(10)
		st.Disks = []metrics.Disk{{Mount: "/", Total: 100, Used: 99}}
		return st
	}
	for range 3 {
		e.send(old())
		if evs := e.notif.take(); len(evs) != 0 {
			t.Fatalf("eski ajanda bildirim olmamalı: %+v", evs)
		}
	}
	h := e.svc.history[e.probe.ID]
	if _, ok := h[len(h)-1].value(MetricInode, ""); ok {
		t.Fatal("eski örnekte inode değeri olmamalı")
	}
	if _, ok := h[len(h)-1].value(MetricZombies, ""); ok {
		t.Fatal("eski örnekte zombi değeri olmamalı")
	}

	// Yeni ajan: /data inode %95, 60 zombi.
	fresh := func(inodeUsed uint64, z int) metrics.Stats {
		st := cpu(10)
		st.Disks = []metrics.Disk{
			{Mount: "/", Total: 100, Used: 10, Inodes: 1000, InodesUsed: 100},
			{Mount: "/data", Total: 100, Used: 10, Inodes: 1000, InodesUsed: inodeUsed},
		}
		st.Zombies = &z
		return st
	}
	e.send(fresh(950, 60))
	evs := e.notif.take()
	if len(evs) != 2 {
		t.Fatalf("inode + zombi uyarısı bekleniyordu: %+v", evs)
	}
	for _, ev := range evs {
		if ev.Kind != notify.KindServerAlert {
			t.Fatalf("tür: %+v", ev)
		}
		switch ev.Metric {
		case MetricInode:
			if ev.Mount != "/data" || ev.Value != 95 || !strings.Contains(ev.Text(), "Inode") {
				t.Errorf("inode olayı: %+v %q", ev, ev.Text())
			}
		case MetricZombies:
			if ev.Value != 60 || !strings.Contains(ev.Text(), "60") || strings.Contains(ev.Text(), "60%") {
				t.Errorf("zombi olayı: %+v %q", ev, ev.Text())
			}
		}
	}
	e.send(fresh(100, 0))
	evs = e.notif.take()
	if len(evs) != 2 || evs[0].Kind != notify.KindServerResolved || evs[1].Kind != notify.KindServerResolved {
		t.Fatalf("iki kural da kapanmalı: %+v", evs)
	}
	for _, ev := range evs {
		if ev.Metric == MetricInode && ev.Mount != "/data" {
			t.Errorf("inode bitişi bölümü taşımalı: %+v", ev)
		}
	}
}
