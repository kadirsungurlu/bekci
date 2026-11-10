package servers

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// secEnv yalnız verilen güvenlik kuralıyla (etkin) hazır ortam.
func secEnv(t *testing.T, r store.ServerAlert) *testEnv {
	t.Helper()
	e := newTestEnv(t)
	e.send(cpu(1))
	r.Active = true
	e.setRules([]store.ServerAlert{r})
	e.notif.take()
	return e
}

func (e *testEnv) ingestSec(rep metrics.Security) {
	e.t.Helper()
	if err := e.svc.IngestSecurity(context.Background(), e.getProbe(), rep); err != nil {
		e.t.Fatal(err)
	}
}

// expectFire tek uyarı bildirimi ve açık olay bekler; olay verisini döndürür.
func (e *testEnv) expectFire(metric, mount string) (notify.Event, store.ServerIncidentData) {
	e.t.Helper()
	n := e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerAlert || n[0].Metric != metric || n[0].Mount != mount ||
		n[0].Level != store.LevelWarning || n[0].IncidentID == 0 {
		e.t.Fatalf("%s açılış bildirimi: %+v", metric, n)
	}
	if !e.rules()[metric].Firing {
		e.t.Fatalf("%s tetiklenmedi", metric)
	}
	list := e.serverIncidents()
	if len(list) != 1 || list[0].ResolvedAt != 0 {
		e.t.Fatalf("%s olay: %+v", metric, list)
	}
	d := store.ParseServerIncidentData(list[0].Data)
	if d.Metric != metric || d.Mount != mount {
		e.t.Fatalf("%s olay verisi: %+v", metric, d)
	}
	for _, lang := range []string{"tr", "en"} {
		n[0].Lang = lang
		if ti := n[0].Title(); ti == "" || strings.Contains(ti, "notify.") || strings.Contains(ti, "%!") {
			e.t.Fatalf("%s başlık (%s): %q", metric, lang, ti)
		}
	}
	return n[0], d
}

func (e *testEnv) expectResolve(metric string) {
	e.t.Helper()
	n := e.notif.take()
	if len(n) != 1 || n[0].Kind != notify.KindServerResolved || n[0].Metric != metric {
		e.t.Fatalf("%s kapanış bildirimi: %+v", metric, n)
	}
	for _, lang := range []string{"tr", "en"} {
		n[0].Lang = lang
		if ti := n[0].Title(); ti == "" || strings.Contains(ti, "notify.") || strings.Contains(ti, "%!") {
			e.t.Fatalf("%s kapanış başlığı (%s): %q", metric, lang, ti)
		}
	}
	if e.rules()[metric].Firing || e.serverIncidents()[0].ResolvedAt == 0 {
		e.t.Fatalf("%s olay kapanmadı", metric)
	}
}

func (e *testEnv) expectQuiet(step string) {
	e.t.Helper()
	if n := e.notif.take(); len(n) != 0 {
		e.t.Fatalf("%s: beklenmeyen bildirim %+v", step, n)
	}
}

func (e *testEnv) timelineKinds(kinds ...string) []store.TimelineEvent {
	e.t.Helper()
	evs, err := e.st.TimelineEvents(context.Background(), store.TimelineFilter{ProbeIDs: []int64{e.probe.ID}, Kinds: kinds, Limit: 20})
	if err != nil {
		e.t.Fatal(err)
	}
	return evs
}

func TestSecRuleRebootRequired(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricRebootRequired, Threshold: 3, Minutes: 1})
	rb := func(req bool) { e.ingestSec(metrics.Security{Reboot: &metrics.SecReboot{Required: req}}) }
	rb(true)
	e.expectQuiet("0. gün")
	e.clock = e.clock.Add(2 * 24 * time.Hour)
	e.svc.CheckSecurity(context.Background())
	e.expectQuiet("2. gün")
	e.clock = e.clock.Add(24*time.Hour + time.Hour)
	e.svc.CheckSecurity(context.Background())
	ev, _ := e.expectFire(MetricRebootRequired, "")
	if ev.Value < 3 || ev.Value > 3.1 {
		t.Fatalf("değer: %v", ev.Value)
	}
	rb(false)
	e.expectResolve(MetricRebootRequired)
}

func TestSecRuleAuthFailed(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricAuthFailed, Threshold: 200, Minutes: 60})
	hour := e.clock.Unix() - e.clock.Unix()%3600
	auth := func(n int) metrics.Security {
		return metrics.Security{Auth: &metrics.SecAuth{Source: "journal", Failed: n,
			Hours: []metrics.SecAuthHour{{Hour: hour, Failed: n}}, TopIPs: []metrics.SecCount{{Key: "203.0.113.9", Count: n}}}}
	}
	e.ingestSec(auth(150))
	e.expectQuiet("150")
	e.ingestSec(auth(100)) // aynı saate eklenir: 250
	ev, d := e.expectFire(MetricAuthFailed, "203.0.113.9")
	if ev.Value != 250 || d.Minutes != 60 {
		t.Fatalf("değer: %v %+v", ev.Value, d)
	}
	// Pencere geçti: sayı düşer, olay kapanır.
	e.clock = e.clock.Add(3 * time.Hour)
	e.svc.CheckSecurity(context.Background())
	e.expectResolve(MetricAuthFailed)
}

func TestSecRuleAVSignatureAge(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricAVSigAge, Threshold: 3, Minutes: 1})
	av := func(age time.Duration) metrics.Security {
		return metrics.Security{AV: &metrics.SecAV{Product: "defender", Service: true, Enabled: true, Realtime: true,
			SigUpdated: e.clock.Add(-age).Unix(), SigVersion: "1.401.1"}}
	}
	// AV'siz (Linux) sunucuda tetiklenmez.
	e.ingestSec(metrics.Security{Firewall: &metrics.SecFirewall{Active: true}})
	e.svc.CheckSecurity(context.Background())
	e.expectQuiet("AV yok")
	e.ingestSec(av(2 * 24 * time.Hour))
	e.expectQuiet("2 gün")
	e.ingestSec(av(5 * 24 * time.Hour))
	ev, _ := e.expectFire(MetricAVSigAge, "1.401.1")
	if ev.Value < 5 || ev.Value > 5.1 {
		t.Fatalf("değer: %v", ev.Value)
	}
	e.ingestSec(av(time.Hour))
	e.expectResolve(MetricAVSigAge)
}

func TestSecRuleAVOff(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricAVOff, Minutes: 1})
	av := func(rt bool) metrics.Security {
		return metrics.Security{AV: &metrics.SecAV{Product: "defender", Service: true, Enabled: true, Realtime: rt}}
	}
	e.ingestSec(av(true))
	e.expectQuiet("açık")
	e.ingestSec(av(false))
	e.expectFire(MetricAVOff, "defender")
	e.clock = e.clock.Add(15 * time.Minute)
	e.ingestSec(av(true))
	e.expectResolve(MetricAVOff)
	// Zaman çizelgesi: ilk rapor olay değil; kapandı + açıldı.
	evs := e.timelineKinds(EventAVChanged)
	if len(evs) != 2 || evs[0].Level != store.EventInfo || evs[1].Level != store.EventWarning || !strings.Contains(string(evs[1].Data), `"ok":false`) || evs[0].Subject != "defender" {
		t.Fatalf("av_changed: %+v", evs)
	}
}

func TestSecRuleFirewallOff(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricFirewallOff, Minutes: 1})
	fw := func(on bool) metrics.Security {
		return metrics.Security{Firewall: &metrics.SecFirewall{Active: on, Backend: "nftables"}}
	}
	e.ingestSec(fw(true))
	e.expectQuiet("açık")
	e.ingestSec(fw(false))
	e.expectFire(MetricFirewallOff, "nftables")
	e.ingestSec(metrics.Security{}) // bölüm yok: önceki durum korunur
	e.expectQuiet("bölüm yok")
	e.clock = e.clock.Add(15 * time.Minute)
	e.ingestSec(fw(true))
	e.expectResolve(MetricFirewallOff)
	evs := e.timelineKinds(EventFirewallChanged)
	if len(evs) != 2 || !strings.Contains(string(evs[0].Data), `"active":true`) || !strings.Contains(string(evs[1].Data), `"active":false`) || evs[1].Level != store.EventWarning {
		t.Fatalf("firewall_changed: %+v", evs)
	}
}

func TestSecRuleWUFailed(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricWUFailed, Minutes: 1})
	now := e.clock.Unix()
	e.ingestSec(metrics.Security{WU: &metrics.SecWU{}})
	e.expectQuiet("başarısız yok")
	e.ingestSec(metrics.Security{WU: &metrics.SecWU{Failed30d: 2, Failed: []metrics.SecWUFail{
		{KB: "KB5031000", Title: "Eski", Time: now - 5*86400, Code: 4},
		{KB: "KB5032000", Title: "Yeni", Time: now - 86400, Code: 4},
	}}})
	ev, _ := e.expectFire(MetricWUFailed, "KB5032000")
	if ev.Value != 2 {
		t.Fatalf("değer: %v", ev.Value)
	}
	// 30 günü geçince (yeni rapor gelmese de) saatlik denetim kapatır.
	e.clock = e.clock.Add(29*24*time.Hour + time.Hour)
	e.svc.CheckSecurity(context.Background())
	e.expectResolve(MetricWUFailed)
}

// Pasif kural tetiklenmez; etkinleşince saatlik denetim açar.
func TestSecRuleInactive(t *testing.T) {
	e := secEnv(t, store.ServerAlert{Metric: MetricAVOff, Minutes: 1})
	e.setRules([]store.ServerAlert{{Metric: MetricAVOff, Minutes: 1, Active: false}})
	e.ingestSec(metrics.Security{AV: &metrics.SecAV{Product: "defender"}})
	e.expectQuiet("pasif")
	e.setRules([]store.ServerAlert{{Metric: MetricAVOff, Minutes: 1, Active: true}})
	e.svc.CheckSecurity(context.Background())
	e.expectFire(MetricAVOff, "defender")
}

func TestSecRuleDefaults(t *testing.T) {
	want := map[string]struct {
		th     float64
		min    int
		active bool
	}{
		MetricRebootRequired: {3, 1, false}, MetricAuthFailed: {200, 60, false}, MetricAVSigAge: {3, 1, true},
		MetricAVOff: {0, 1, true}, MetricFirewallOff: {0, 1, false}, MetricWUFailed: {0, 1, true},
	}
	got := map[string]store.ServerAlert{}
	for _, r := range DefaultRules() {
		got[r.Metric] = r
	}
	for m, w := range want {
		r, ok := got[m]
		if !ok || r.Threshold != w.th || r.Minutes != w.min || r.Active != w.active || !ValidMetric(m) || !SecurityMetric(m) {
			t.Errorf("%s: %+v", m, r)
		}
	}
}

func TestSecRuleCauseText(t *testing.T) {
	cases := []struct {
		d      store.ServerIncidentData
		tr, en string
	}{
		{store.ServerIncidentData{Metric: MetricRebootRequired, Last: 4.2, Threshold: 3},
			"Yeniden başlatma 4 gün bekliyor (eşik 3 gün)", "Reboot pending for 4 days (threshold 3 days)"},
		{store.ServerIncidentData{Metric: MetricAuthFailed, Mount: "203.0.113.9", Value: 250, Minutes: 60, Threshold: 200},
			"250 başarısız oturum açma, son 60 dakikada (eşik 200, en çok deneyen 203.0.113.9)",
			"250 failed logins in the last 60 minutes (threshold 200, top source 203.0.113.9)"},
		{store.ServerIncidentData{Metric: MetricAuthFailed, Value: 250, Minutes: 120, Threshold: 200},
			"250 başarısız oturum açma, son 120 dakikada (eşik 200)", "250 failed logins in the last 120 minutes (threshold 200)"},
		{store.ServerIncidentData{Metric: MetricAVSigAge, Mount: "1.401.1", Last: 5, Threshold: 3},
			"Antivirüs imzaları 5 gün önce güncellendi (eşik 3 gün)", "Antivirus signatures last updated 5 days ago (threshold 3 days)"},
		{store.ServerIncidentData{Metric: MetricAVOff, Mount: "defender"},
			"Antivirüs ya da gerçek zamanlı koruma kapalı", "Antivirus or real-time protection is off"},
		{store.ServerIncidentData{Metric: MetricFirewallOff},
			"Güvenlik duvarı kapalı", "Firewall is off"},
		{store.ServerIncidentData{Metric: MetricWUFailed, Mount: "KB5032000", Last: 2},
			"Son 30 günde 2 başarısız güncelleme (en yenisi KB5032000)", "2 failed updates in the last 30 days (latest KB5032000)"},
		{store.ServerIncidentData{Metric: MetricWUFailed, Last: 1},
			"Son 30 günde 1 başarısız güncelleme", "1 failed updates in the last 30 days"},
	}
	for _, c := range cases {
		if got := store.ServerIncidentCause("tr", store.IncidentServerAlert, c.d); got != c.tr {
			t.Errorf("%s tr: %q", c.d.Metric, got)
		}
		if got := store.ServerIncidentCause("en", store.IncidentServerAlert, c.d); got != c.en {
			t.Errorf("%s en: %q", c.d.Metric, got)
		}
	}
	// Bildirim başlıkları.
	ev := notify.Event{Kind: notify.KindServerAlert, MonitorName: "win-1", Metric: MetricAuthFailed, Mount: "203.0.113.9", Value: 250, Minutes: 60, Lang: "tr"}
	if got := ev.Title(); !strings.Contains(got, "win-1: 250 başarısız oturum açma (son 60 dakika)") {
		t.Errorf("auth tr: %q", got)
	}
	ev.Lang = "en"
	if got := ev.Title(); !strings.Contains(got, "win-1: 250 failed logins in the last 60 minutes") {
		t.Errorf("auth en: %q", got)
	}
	ev = notify.Event{Kind: notify.KindServerAlert, MonitorName: "win-1", Metric: MetricWUFailed, Mount: "KB5032000", Value: 1, Lang: "en"}
	if got := ev.Title(); !strings.Contains(got, "win-1: Windows update failed (KB5032000)") {
		t.Errorf("wu en: %q", got)
	}
	ev = notify.Event{Kind: notify.KindServerResolved, MonitorName: "web-1", Metric: MetricFirewallOff, Lang: "tr"}
	if got := ev.Title(); !strings.Contains(got, "web-1: güvenlik duvarı açıldı") {
		t.Errorf("fw ok tr: %q", got)
	}
}
