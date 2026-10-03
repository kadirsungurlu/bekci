package servers

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// sendHost belirtilen host bilgisiyle örnek gönderir (yeniden başlatma ve
// Docker durumu için).
func (e *testEnv) sendHost(host metrics.Host, st metrics.Stats) {
	e.t.Helper()
	smp := metrics.Sample{Time: 1, Host: &host, Stats: &st}
	if err := e.svc.Ingest(context.Background(), e.getProbe(), smp); err != nil {
		e.t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Minute)
}

func (e *testEnv) setRules(rules []store.ServerAlert) {
	e.t.Helper()
	if _, err := e.st.ReplaceServerAlerts(context.Background(), e.probe.ID, rules, e.clock.Unix()); err != nil {
		e.t.Fatal(err)
	}
}

func kinds(evs []notify.Event) []string {
	var out []string
	for _, ev := range evs {
		k := ev.Kind
		if ev.Level != "" {
			k += ":" + ev.Level
		}
		if ev.Downgraded {
			k += ":down"
		}
		out = append(out, k)
	}
	return out
}

// Uyarı + kritik eşik: CPU %82 → 🟡 uyarı; %95 → 🔴 kritiğe çıkış; %84 → 🟡
// kritikten iniş (histerezis: kritik eşiğin %5 altına inince); %10 → 🟢
// düzelme (en yüksek seviye kritik).
func TestAlertLevels(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(10)) // varsayılan kurallar ve kanal (yok)
	e.setRules([]store.ServerAlert{{Metric: MetricCPU, Threshold: 90, WarnThreshold: 80, Minutes: 1, Active: true}})
	e.send(cpu(82))
	e.send(cpu(82))
	got := e.notif.take()
	if k := kinds(got); len(k) != 1 || k[0] != "server_alert:warning" {
		t.Fatalf("uyarı seviyesi bekleniyordu: %v", k)
	}
	if got[0].Threshold != 80 {
		t.Errorf("uyarı bildiriminde uyarı eşiği yazılmalı: %+v", got[0].Threshold)
	}
	r := e.rules()["cpu"]
	if !r.Firing || r.Level != store.LevelWarning {
		t.Fatalf("kural uyarı seviyesinde olmalı: %+v", r)
	}
	inc, _ := e.st.OpenServerIncidentID(ctx, r.ID)
	if d := e.incidentData(inc); d.Level != store.LevelWarning || d.Threshold != 80 {
		t.Fatalf("olay verisi: %+v", d)
	}

	e.send(cpu(95))
	e.send(cpu(95))
	if k := kinds(e.notif.take()); len(k) != 1 || k[0] != "server_alert:critical" {
		t.Fatalf("kritiğe çıkış bekleniyordu: %v", k)
	}
	if d := e.incidentData(inc); d.Level != store.LevelCritical || d.PeakLevel != store.LevelCritical {
		t.Fatalf("olay seviyesi kritik olmalı: %+v", d)
	}
	// %89: kritik eşiğin histerezis payı (2 puan) içinde → seviye düşmez.
	e.send(cpu(89))
	e.send(cpu(89))
	if k := kinds(e.notif.take()); len(k) != 0 {
		t.Fatalf("histerezis içinde seviye değişmemeli: %v", k)
	}
	e.send(cpu(84))
	e.send(cpu(84))
	got = e.notif.take()
	if k := kinds(got); len(k) != 1 || k[0] != "server_alert:warning:down" {
		t.Fatalf("kritikten iniş bekleniyordu: %v", k)
	}
	if got[0].PeakLevel != store.LevelCritical {
		t.Errorf("iniş bildirimi en yüksek seviyeyi taşımalı: %+v", got[0])
	}
	e.send(cpu(10))
	e.send(cpu(10))
	got = e.notif.take()
	if k := kinds(got); len(k) != 1 || k[0] != "server_resolved:warning" || got[0].PeakLevel != store.LevelCritical {
		t.Fatalf("düzelme (en yüksek kritik) bekleniyordu: %v %+v", k, got)
	}
	if e.rules()["cpu"].Firing {
		t.Fatal("kural kapanmalıydı")
	}
	// Olayın işlem geçmişinde seviye değişimleri var.
	evs, _ := e.st.IncidentEvents(ctx, inc)
	levels := 0
	for _, ev := range evs {
		if ev.Kind == store.EventLevel {
			levels++
		}
	}
	if levels != 2 {
		t.Errorf("iki seviye kaydı bekleniyordu: %d", levels)
	}
}

func (e *testEnv) incidentData(id int64) store.ServerIncidentData {
	e.t.Helper()
	inc, err := e.st.GetIncident(context.Background(), id)
	if err != nil {
		e.t.Fatal(err)
	}
	return store.ParseServerIncidentData(inc.Data)
}

func withContainers(st metrics.Stats, cs ...metrics.Container) metrics.Stats {
	st.Containers = cs
	return st
}

// Konteyner kuralı: adlı kural konteyner durunca (2 dk) uyarır, çalışınca
// düzelir; adsız kural bilinen herhangi bir konteyner durunca; eski ajan
// (State yok) listeden düşen konteyneri de yakalar; Docker yoksa değerlendirilmez.
func TestContainerAlert(t *testing.T) {
	e := newTestEnv(t)
	host := metrics.Host{Hostname: "cp1", Threads: 4, Docker: true}
	run := func(names ...string) metrics.Stats {
		var cs []metrics.Container
		for _, n := range names {
			cs = append(cs, metrics.Container{ID: n, Name: n, State: "running"})
		}
		return withContainers(cpu(10), cs...)
	}
	e.sendHost(host, run("web", "db"))
	e.setRules([]store.ServerAlert{
		{Metric: MetricContainer, Mount: "db", Minutes: 2, Active: true},
		{Metric: MetricContainer, Mount: "", Minutes: 2, Active: true},
	})
	e.sendHost(host, run("web", "db"))
	// db durdu (exited).
	for i := 0; i < 2; i++ {
		e.sendHost(host, withContainers(cpu(10), metrics.Container{ID: "web", Name: "web", State: "running"}, metrics.Container{ID: "db", Name: "db", State: "exited"}))
	}
	got := e.notif.take()
	if len(got) != 2 {
		t.Fatalf("adlı ve adsız kural birer bildirim göndermeliydi: %v", kinds(got))
	}
	for _, ev := range got {
		if ev.Kind != notify.KindServerAlert || ev.Metric != MetricContainer || ev.Mount != "db" || ev.Message != "exited" {
			t.Errorf("konteyner bildirimi: %+v", ev)
		}
	}
	rules := e.rules()
	for _, r := range e.allRules() {
		if r.Metric == MetricContainer && !r.Firing {
			t.Errorf("konteyner kuralı tetiklenmeli: %+v", r)
		}
	}
	_ = rules
	// db tekrar çalışıyor → iki düzelme.
	e.sendHost(host, run("web", "db"))
	got = e.notif.take()
	if len(got) != 2 || got[0].Kind != notify.KindServerResolved || got[0].Mount != "db" {
		t.Fatalf("düzelme bekleniyordu: %v %+v", kinds(got), got)
	}
	// Eski ajan: State yok, durmuş konteyner listede değil → adsız kural yakalar, adlı kural da.
	e.sendHost(host, withContainers(cpu(10), metrics.Container{ID: "web", Name: "web"}))
	e.sendHost(host, withContainers(cpu(10), metrics.Container{ID: "web", Name: "web"}))
	got = e.notif.take()
	if len(got) != 2 || got[0].Mount != "db" || got[0].Message != "Konteyner listede yok" {
		t.Fatalf("listeden düşen konteyner yakalanmalı: %v %+v", kinds(got), got)
	}
	// Docker erişimi yok: hiçbir şey değişmez (uyarı kapanmaz, yenisi açılmaz).
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4}, cpu(10))
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4}, cpu(10))
	if got := e.notif.take(); len(got) != 0 {
		t.Fatalf("Docker yokken bildirim gitmemeli: %v", kinds(got))
	}
}

func (e *testEnv) allRules() []store.ServerAlert {
	list, err := e.st.ServerAlerts(context.Background(), e.probe.ID)
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

// Yeniden başlatma: açılış zamanı ileri kayınca uyarı geçmişine yazılır ve
// "reboot" kuralı açıksa bildirim gider; saat kayması (< 60 sn) sayılmaz;
// kural kapalıysa yalnızca kayıt.
func TestRebootDetection(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	boot := e.clock.Add(-10 * 24 * time.Hour).Unix()
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4, BootTime: boot}, cpu(10))
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4, BootTime: boot + 30}, cpu(10)) // kayma
	if got := e.notif.take(); len(got) != 0 {
		t.Fatalf("saat kayması yeniden başlatma sayılmamalı: %v", kinds(got))
	}
	newBoot := e.clock.Unix() - 20
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4, BootTime: newBoot}, cpu(10))
	got := e.notif.take()
	if len(got) != 1 || got[0].Kind != notify.KindServerReboot || got[0].BootTime.Unix() != newBoot || got[0].Target != "cp1" {
		t.Fatalf("yeniden başlatma bildirimi bekleniyordu: %v %+v", kinds(got), got)
	}
	evs, _ := e.st.ServerAlertEvents(ctx, e.probe.ID, 0, 10)
	if len(evs) != 1 || evs[0].Metric != MetricReboot || evs[0].EndedAt == 0 || int64(evs[0].Value) != newBoot {
		t.Fatalf("uyarı geçmişinde yeniden başlatma kaydı yok: %+v", evs)
	}
	// Kural kapalı: kayıt var, bildirim yok.
	rules := e.allRules()
	for i := range rules {
		if rules[i].Metric == MetricReboot {
			rules[i].Active = false
		}
	}
	e.setRules(rules)
	e.sendHost(metrics.Host{Hostname: "cp1", Threads: 4, BootTime: newBoot + 600}, cpu(10))
	if got := e.notif.take(); len(got) != 0 {
		t.Fatalf("kural kapalıyken bildirim gitmemeli: %v", kinds(got))
	}
	if evs, _ := e.st.ServerAlertEvents(ctx, e.probe.ID, 0, 10); len(evs) != 2 {
		t.Fatalf("ikinci yeniden başlatma kaydı yok: %+v", evs)
	}
}

// Sunucu bakım penceresi: pencere içinde yeni uyarı açılmaz ve çevrimdışı
// bildirimi gitmez; pencere bitince normal.
func TestServerMaintenanceSuppresses(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(10))
	e.setRules([]store.ServerAlert{{Metric: MetricCPU, Threshold: 90, Minutes: 1, Active: true}, {Metric: MetricOffline, Minutes: 3, Active: true}})
	// Elle (süresiz) bakım penceresi yalnızca bu sunucu için.
	w := store.Maintenance{Title: "Bakım", Active: true, Strategy: store.MaintManual, Timezone: "UTC", ServerIDs: []int64{e.probe.ID}}
	if err := e.st.CreateMaintenance(ctx, &w); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ReloadMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	e.send(cpu(95))
	e.send(cpu(95))
	if got := e.notif.take(); len(got) != 0 {
		t.Fatalf("bakımda uyarı açılmamalı: %v", kinds(got))
	}
	if e.rules()["cpu"].Firing {
		t.Fatal("bakımda kural tetiklenmemeli")
	}
	if v := e.hub.last(); !v.InMaintenance {
		t.Fatal("görünüm bakımda olmalı")
	}
	// Veri kesildi: çevrimdışı uyarısı da açılmaz.
	e.clock = e.clock.Add(10 * time.Minute)
	e.svc.CheckOffline(ctx)
	if got := e.notif.take(); len(got) != 0 || e.rules()["offline"].Firing {
		t.Fatalf("bakımda çevrimdışı uyarısı açılmamalı: %v", kinds(got))
	}
	// Pencere kapandı: uyarı açılır.
	if err := e.st.SetMaintenanceActive(ctx, w.ID, false); err != nil {
		t.Fatal(err)
	}
	e.svc.ReloadMaintenance(ctx)
	e.send(cpu(95))
	e.send(cpu(95))
	if got := e.notif.take(); len(got) != 1 || got[0].Kind != notify.KindServerAlert {
		t.Fatalf("bakım bitince uyarı gitmeliydi: %v", kinds(got))
	}
}
