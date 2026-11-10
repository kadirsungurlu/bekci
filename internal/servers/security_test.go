package servers

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func (e *testEnv) security(pkgs ...metrics.SecPkg) {
	e.t.Helper()
	rep := metrics.Security{Updates: &metrics.SecUpdates{Manager: "apt", Packages: pkgs}}
	if err := e.svc.IngestSecurity(context.Background(), e.getProbe(), rep); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) secRule() store.ServerAlert {
	e.t.Helper()
	a, ok := e.rules()[MetricSecUpdatesAge]
	if !ok {
		e.t.Fatal("sec_updates_age kuralı yok")
	}
	return a
}

// Güvenlik paketi 0. gün olay açmaz; saatlik denetim 8. günde açar (konu en
// eski paket), paket kurulunca olay kapanır.
func TestSecUpdatesAgeLifecycle(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1))
	e.notif.take()
	if a := e.secRule(); a.Threshold != 7 || !a.Active {
		t.Fatalf("varsayılan kural: %+v", a)
	}

	ssl := metrics.SecPkg{Name: "openssl", New: "3.0.2", Security: true}
	vim := metrics.SecPkg{Name: "vim", New: "9.1"}
	e.security(ssl, vim)
	if n := e.notif.take(); len(n) != 0 || len(e.serverIncidents()) != 0 {
		t.Fatalf("0. gün: %+v", n)
	}
	// Ertesi gün yeni güvenlik paketi: en eski hâlâ openssl.
	e.clock = e.clock.Add(24 * time.Hour)
	curl := metrics.SecPkg{Name: "curl", New: "8", Security: true}
	e.security(ssl, vim, curl)
	e.clock = e.clock.Add(5 * 24 * time.Hour)
	e.svc.CheckSecurity(ctx)
	if n := e.notif.take(); len(n) != 0 {
		t.Fatalf("6. gün: %+v", n)
	}

	e.clock = e.clock.Add(2*24*time.Hour + time.Hour) // 8. gün
	e.svc.CheckSecurity(ctx)
	n := e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerAlert || n[0].Metric != MetricSecUpdatesAge || n[0].Mount != "openssl" ||
		n[0].Value < 8 || n[0].Value > 8.1 || n[0].Threshold != 7 || n[0].Level != store.LevelWarning || n[0].IncidentID == 0 {
		t.Fatalf("8. gün: %+v", n)
	}
	if !strings.Contains(n[0].Title(), "openssl") {
		t.Fatalf("başlık: %q", n[0].Title())
	}
	list := e.serverIncidents()
	if len(list) != 1 || list[0].ResolvedAt != 0 {
		t.Fatalf("olay: %+v", list)
	}
	d := store.ParseServerIncidentData(list[0].Data)
	if d.Metric != MetricSecUpdatesAge || d.Mount != "openssl" {
		t.Fatalf("olay verisi: %+v", d)
	}
	// Tekrar denetim: yeni bildirim yok, değer güncellenir.
	e.clock = e.clock.Add(24 * time.Hour)
	e.svc.CheckSecurity(ctx)
	if n := e.notif.take(); len(n) != 0 {
		t.Fatalf("tekrar: %+v", n)
	}
	// Metrik örneği (evaluate) sec_updates_age kuralını kapatmaz.
	e.send(cpu(1))
	if n := e.notif.take(); len(n) != 0 || !e.secRule().Firing {
		t.Fatalf("örnek kuralı etkiledi: %+v", n)
	}

	// openssl kuruldu, curl hâlâ bekliyor: olay açık kalır.
	e.clock = e.clock.Add(15 * time.Minute)
	e.security(vim, curl)
	if n := e.notif.take(); len(n) != 0 || !e.secRule().Firing {
		t.Fatalf("kısmi kurulum: %+v", n)
	}
	// Hepsi kuruldu: olay kapanır, tek "çözüldü" bildirimi (konu korunur).
	e.clock = e.clock.Add(15 * time.Minute)
	e.security(vim)
	n = e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerResolved || n[0].Metric != MetricSecUpdatesAge || n[0].Mount != "openssl" {
		t.Fatalf("kapanış: %+v", n)
	}
	if n[0].Title() == "" {
		t.Fatal("başlık")
	}
	if list = e.serverIncidents(); list[0].ResolvedAt == 0 || e.secRule().Firing {
		t.Fatal("olay kapanmadı")
	}

	// Zaman çizelgesi: yeni güvenlik paketleri, kurulumlar, uyarı.
	evs, err := e.st.TimelineEvents(ctx, store.TimelineFilter{ProbeIDs: []int64{e.probe.ID}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, ev := range evs {
		if ev.Kind == EventSecUpdatesNew || ev.Kind == EventSecUpdatesInstalled || ev.Subject == alertSubject(MetricSecUpdatesAge, "openssl") {
			got = append(got, ev.Kind)
		}
	}
	slices.Reverse(got)
	want := []string{EventSecUpdatesNew, EventSecUpdatesNew, EventAlert, EventSecUpdatesInstalled, EventSecUpdatesInstalled, EventAlertResolved}
	if !slices.Equal(got, want) {
		t.Fatalf("zaman çizelgesi: %v", got)
	}
}

// Kural pasifse olay açılmaz; sunucu devre dışı kalınca açık olay kapanır.
func TestSecUpdatesAgeInactiveAndDisabled(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1))
	e.setRules([]store.ServerAlert{{Metric: MetricSecUpdatesAge, Threshold: 3, Minutes: 1, Active: false}})
	e.notif.take()
	e.security(metrics.SecPkg{Name: "openssl", New: "2", Security: true}, metrics.SecPkg{Name: "zlib", New: "2"})
	e.clock = e.clock.Add(10 * 24 * time.Hour)
	e.svc.CheckSecurity(ctx)
	if n := e.notif.take(); len(n) != 0 || len(e.serverIncidents()) != 0 {
		t.Fatalf("pasif kural: %+v", n)
	}

	e.setRules([]store.ServerAlert{{Metric: MetricSecUpdatesAge, Threshold: 3, Minutes: 1, Active: true}})
	e.svc.CheckSecurity(ctx)
	if n := e.notif.take(); len(n) != 1 || n[0].Kind != notify.KindServerAlert || n[0].Value < 10 {
		t.Fatalf("etkin kural: %+v", n)
	}

	if err := e.st.UpdateProbe(ctx, e.probe.ID, e.probe.Name, false); err != nil {
		t.Fatal(err)
	}
	e.clock = e.clock.Add(time.Hour)
	e.svc.CheckSecurity(ctx)
	e.notif.take()
	if list := e.serverIncidents(); len(list) != 1 || list[0].ResolvedAt == 0 || e.secRule().Firing {
		t.Fatalf("devre dışı sunucuda olay kapanmadı: %+v", list)
	}
}

func TestSecurityChanges(t *testing.T) {
	if l := securityChanges(1, store.SecurityChange{UpdatesReported: true}, 5); len(l) != 0 {
		t.Fatalf("boş fark: %+v", l)
	}
	many := make([]string, maxSecEventPkgs+5)
	for i := range many {
		many[i] = string(rune('z' - i))
	}
	l := securityChanges(7, store.SecurityChange{NewSecurity: many, Installed: []string{"b", "a"}, RebootRequired: true, RebootCleared: true}, 9)
	m := map[string]string{}
	for _, ev := range l {
		m[ev.Kind] = ev.Level
	}
	if len(l) != 4 || m[EventSecUpdatesNew] != store.EventWarning || m[EventSecUpdatesInstalled] != store.EventInfo ||
		m[EventRebootRequired] != store.EventWarning || m[EventRebootCleared] != store.EventInfo {
		t.Fatalf("%+v", m)
	}
	for _, ev := range l {
		if ev.ProbeID != 7 || ev.Time != 9 {
			t.Fatalf("%+v", ev)
		}
		var d struct {
			Count    int      `json:"count"`
			Packages []string `json:"packages"`
		}
		switch ev.Kind {
		case EventSecUpdatesNew:
			_ = json.Unmarshal(ev.Data, &d)
			if d.Count != maxSecEventPkgs+5 || len(d.Packages) != maxSecEventPkgs || !slices.IsSorted(d.Packages) {
				t.Fatalf("yeni: %s", ev.Data)
			}
		case EventSecUpdatesInstalled:
			_ = json.Unmarshal(ev.Data, &d)
			if d.Count != 2 || !slices.Equal(d.Packages, []string{"a", "b"}) {
				t.Fatalf("kurulan: %s", ev.Data)
			}
		}
	}
}

// Yeniden başlatma raporu zaman çizelgesine yazılır (geçişlerde bir kez).
func TestIngestSecurityReboot(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	rb := func(req bool) {
		if err := e.svc.IngestSecurity(ctx, e.getProbe(), metrics.Security{Reboot: &metrics.SecReboot{Required: req, Reason: metrics.RebootKernel}}); err != nil {
			t.Fatal(err)
		}
	}
	rb(true)
	rb(true)
	rb(false)
	evs, _ := e.st.TimelineEvents(ctx, store.TimelineFilter{ProbeIDs: []int64{e.probe.ID}, Kinds: []string{EventRebootRequired, EventRebootCleared}, Limit: 10})
	if len(evs) != 2 || evs[0].Kind != EventRebootCleared || evs[1].Kind != EventRebootRequired {
		t.Fatalf("%+v", evs)
	}
}

func TestSecUpdatesAgeText(t *testing.T) {
	d := store.ServerIncidentData{Metric: MetricSecUpdatesAge, Mount: "openssl", Last: 8.2, Threshold: 7}
	if got := store.ServerIncidentCause("tr", store.IncidentServerAlert, d); got != "Güvenlik güncellemesi 8 gün bekliyor (en eskisi openssl, eşik 7 gün)" {
		t.Fatalf("tr: %q", got)
	}
	if got := store.ServerIncidentCause("en", store.IncidentServerAlert, d); got != "Security update pending for 8 days (oldest openssl, threshold 7 days)" {
		t.Fatalf("en: %q", got)
	}
	ev := notify.Event{Kind: notify.KindServerAlert, MonitorName: "web-1", Metric: MetricSecUpdatesAge, Mount: "openssl", Value: 8.2, Threshold: 7}
	for lang, want := range map[string]string{
		"tr": "web-1: güvenlik güncellemesi 8 gün bekliyor (en eskisi openssl)",
		"en": "web-1: security update pending for 8 days (oldest openssl)",
	} {
		ev.Lang = lang
		if got := ev.Title(); !strings.Contains(got, want) {
			t.Errorf("%s alarm: %q", lang, got)
		}
	}
	ev.Kind = notify.KindServerResolved
	for lang, want := range map[string]string{
		"tr": "web-1: bekleyen güvenlik güncellemesi kalmadı",
		"en": "web-1: no pending security updates left",
	} {
		ev.Lang = lang
		if got := ev.Title(); !strings.Contains(got, want) {
			t.Errorf("%s çözüldü: %q", lang, got)
		}
	}
}
