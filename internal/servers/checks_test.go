package servers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func (e *testEnv) check(name string, st eklenti.Status, ms ...eklenti.Metric) {
	e.t.Helper()
	r := CheckReport{Name: name, Source: "plugin", T: e.clock.UnixMilli(), Result: eklenti.Result{Status: st, Message: "çıktı", Metrics: ms}}
	if err := e.svc.IngestChecks(context.Background(), e.getProbe(), []CheckReport{r}); err != nil {
		e.t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Minute)
}

func (e *testEnv) checkRule(mount string) store.ServerAlert {
	e.t.Helper()
	for _, a := range e.allRules() {
		if a.Metric == MetricCheck && a.Mount == mount {
			return a
		}
	}
	e.t.Fatalf("kural yok: %s", mount)
	return store.ServerAlert{}
}

func (e *testEnv) takeOne(kind, mount, level string) notify.Event {
	e.t.Helper()
	n := e.notif.take()
	if len(n) != 1 || n[0].Kind != kind || n[0].Metric != MetricCheck || n[0].Mount != mount || (level != "" && n[0].Level != level) {
		e.t.Fatalf("bildirim beklendi (%s %s %s): %+v", kind, mount, level, n)
	}
	for _, lang := range []string{"tr", "en"} {
		n[0].Lang = lang
		if ti := n[0].Title(); ti == "" || strings.Contains(ti, "notify.") || strings.Contains(ti, "%!") {
			e.t.Fatalf("başlık (%s): %q", lang, ti)
		}
	}
	return n[0]
}

// Durum kuralı: ilk sonuçta varsayılan kural (yalnız critical) oluşur, olay
// açılır, seviye değişmez, düzelince kapanır; 3 unknown çizelgeye düşer.
func TestCheckStatusIncident(t *testing.T) {
	e := newTestEnv(t)
	e.check("mysql", eklenti.OK)
	if a := e.checkRule("mysql"); a.Threshold != store.CheckStatusCritical || a.Firing || !a.Active {
		t.Fatalf("varsayılan kural: %+v", a)
	}
	e.expectQuiet("ok")
	e.check("mysql", eklenti.Warning)
	e.expectQuiet("warning maskede yok")
	e.check("mysql", eklenti.Critical)
	ev := e.takeOne(notify.KindServerAlert, "mysql", store.LevelCritical)
	if ev.Message != "çıktı" {
		t.Fatalf("ileti: %q", ev.Message)
	}
	list := e.serverIncidents()
	if len(list) != 1 || list[0].ResolvedAt != 0 {
		t.Fatalf("olay: %+v", list)
	}
	for _, lang := range []string{"tr", "en"} {
		c := store.ServerIncidentCause(lang, store.IncidentServerAlert, store.ParseServerIncidentData(list[0].Data))
		if c == "" || strings.Contains(c, "incident.") || strings.Contains(c, "%!") {
			t.Fatalf("neden (%s): %q", lang, c)
		}
	}
	e.check("mysql", eklenti.Critical)
	e.expectQuiet("sürüyor")
	e.check("mysql", eklenti.OK)
	e.takeOne(notify.KindServerResolved, "mysql", "")
	if e.serverIncidents()[0].ResolvedAt == 0 {
		t.Fatal("olay kapanmadı")
	}
	for i := 0; i < 4; i++ {
		e.check("mysql", eklenti.Unknown)
	}
	e.expectQuiet("unknown maskede yok")
	if evs := e.timelineKinds(EventCheckUnknown); len(evs) != 1 || evs[0].Subject != "mysql" {
		t.Fatalf("check_unknown: %+v", evs)
	}
}

// Süreli durum kuralı ve kapatma: süre dolmadan olay yok; kapatılan denetimin
// olayı kapanır.
func TestCheckStatusMinutesAndDisable(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.check("nginx", eklenti.OK)
	if _, err := e.st.ReplaceCheckAlerts(ctx, e.probe.ID, "nginx", []store.ServerAlert{
		{Metric: MetricCheck, Mount: "nginx", Threshold: store.CheckStatusWarning | store.CheckStatusCritical, Minutes: 3, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	e.check("nginx", eklenti.Warning)
	e.check("nginx", eklenti.Warning)
	e.check("nginx", eklenti.Warning)
	e.expectQuiet("süre dolmadı")
	e.check("nginx", eklenti.Warning)
	e.takeOne(notify.KindServerAlert, "nginx", store.LevelWarning)
	e.check("nginx", eklenti.Critical)
	if n := e.notif.take(); len(n) == 0 || e.checkRule("nginx").Level != store.LevelCritical {
		t.Fatalf("seviye yükselmedi: %+v %+v", n, e.checkRule("nginx"))
	}
	off := false
	if _, err := e.st.SetCheckConfig(ctx, e.probe.ID, "nginx", store.CheckConfig{Enabled: &off}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.ResolveCheck(ctx, e.getProbe(), "nginx"); err != nil {
		t.Fatal(err)
	}
	e.takeOne(notify.KindServerResolved, "nginx", "")
	e.check("nginx", eklenti.Critical) // kapalı denetimin geç sonucu olay açmaz
	e.expectQuiet("kapalı")
}

// Metrik eşik kuralı, below yönü: küçük değer kötü; uyarı -> kritik -> düzelme.
func TestCheckMetricBelow(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.check("disk", eklenti.OK, eklenti.Metric{Name: "free_gb", Value: 80})
	if _, err := e.st.ReplaceCheckAlerts(ctx, e.probe.ID, "disk", []store.ServerAlert{
		{Metric: MetricCheck, Mount: "disk", Threshold: store.CheckStatusCritical, Minutes: 1, Active: true},
		{Metric: MetricCheck, Mount: "disk/free_gb", Threshold: 10, WarnThreshold: 20, Below: true, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	if !e.checkRule("disk/free_gb").Below {
		t.Fatal("below saklanmadı")
	}
	e.check("disk", eklenti.OK, eklenti.Metric{Name: "free_gb", Value: 15})
	e.takeOne(notify.KindServerAlert, "disk/free_gb", store.LevelWarning)
	e.check("disk", eklenti.OK) // metrik yok: durum korunur
	e.expectQuiet("metrik yok")
	e.check("disk", eklenti.OK, eklenti.Metric{Name: "free_gb", Value: 5})
	if e.checkRule("disk/free_gb").Level != store.LevelCritical {
		t.Fatal("kritik olmadı")
	}
	e.notif.take()
	e.check("disk", eklenti.OK, eklenti.Metric{Name: "free_gb", Value: 50})
	e.takeOne(notify.KindServerResolved, "disk/free_gb", "")
	// Genel uyarı kaydı denetim kurallarını silmez.
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, append([]store.ServerAlert{}, e.allRules()...), e.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.CheckAlerts(ctx, e.probe.ID, "disk"); len(got) != 2 {
		t.Fatalf("kurallar: %+v", got)
	}
}
