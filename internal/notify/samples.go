package notify

import (
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// SampleNames örnek bildirimlerde kullanılacak adlar (paneldeki gerçek bir
// monitör ve sunucu varsa onlar, yoksa yer tutucular).
type SampleNames struct {
	Monitor, Target string
	Server, Host    string
	MonitorURL      string
	IncidentURL     string
	ServerURL       string
	MonitorID       int64
	ServerID        int64
	CertIssuer      string
	DiskMount       string // disk örneği için bölüm; boşsa "/home"
}

// SampleEvents her bildirim türünden birer örnek: monitör çalışmıyor,
// hatırlatma, tekrar çalışıyor, konum kesintisi ve düzelmesi, SSL
// sertifikası, sunucu CPU/RAM/disk ve çevrimdışı uyarıları ile düzelme
// bildirimleri. Hepsi Sample işaretlidir ve lang dilindedir (bildirim dili;
// yer tutucu adlar da bu dilde).
func SampleEvents(n SampleNames, now time.Time, lang string) []Event {
	if n.Monitor == "" {
		n.Monitor, n.Target = i18n.T(lang, "notify.sample.monitor"), i18n.T(lang, "notify.sample.target")
	}
	if n.Server == "" {
		n.Server, n.Host = i18n.T(lang, "notify.sample.server"), i18n.T(lang, "notify.sample.host")
	}
	if n.CertIssuer == "" {
		n.CertIssuer = "Let's Encrypt"
	}
	mount := n.DiskMount
	if mount == "" {
		mount = "/home"
	}
	mon := func(kind string) Event {
		return Event{Kind: kind, Sample: true, Lang: lang, MonitorID: n.MonitorID, MonitorName: n.Monitor, MonitorType: "http",
			Target: n.Target, Time: now, URL: n.MonitorURL, IncidentURL: n.IncidentURL}
	}
	srv := func(kind, metric string, value, threshold float64, minutes int) Event {
		return Event{Kind: kind, Sample: true, Lang: lang, ProbeID: max(n.ServerID, 1), MonitorName: n.Server, MonitorType: "server",
			Target: n.Host, Time: now, URL: n.ServerURL, Metric: metric, Value: value, Threshold: threshold, Minutes: minutes}
	}

	down := mon(KindDown)
	down.Message = "HTTP 503 Service Unavailable"
	reminder := mon(KindReminder)
	reminder.Message, reminder.Downtime = "HTTP 503 Service Unavailable", time.Hour
	up := mon(KindUp)
	up.Downtime = 1*time.Hour + 12*time.Minute
	locDown := mon(KindLocationDown)
	locDown.Message = "Frankfurt: " + i18n.T(lang, "notify.sample.loc_error")
	locDown.Locations = []LocationNote{{Name: "Frankfurt", Message: i18n.T(lang, "notify.sample.loc_error")}}
	locUp := mon(KindLocationUp)
	locUp.Downtime = 23 * time.Minute
	slow := mon(KindSlow)
	slow.Value, slow.Threshold, slow.Checks = 2840, 2000, 3
	slowOK := mon(KindSlowResolved)
	slowOK.Value, slowOK.Threshold, slowOK.Checks, slowOK.Downtime = 640, 2000, 3, 18*time.Minute
	cert := mon(KindCert)
	cert.IncidentURL = ""
	cert.CertDays, cert.CertExpires, cert.CertIssuer = 7, now.AddDate(0, 0, 7), n.CertIssuer
	dom := mon(KindDomain)
	dom.IncidentURL = ""
	dom.Domain, dom.DomainDays, dom.DomainExpires, dom.DomainRegistrar, dom.DomainCritical = sampleDomain(n.Target), 14, now.AddDate(0, 0, 14), "Example Registrar Inc.", 7

	cpu := srv(KindServerAlert, "cpu", 94, 90, 10)
	cpu.Level = LevelCritical
	cpuWarn := srv(KindServerAlert, "cpu", 82, 80, 10)
	cpuWarn.Level = LevelWarning
	cont := srv(KindServerAlert, "container", 0, 0, 1)
	cont.Mount, cont.Message = "nginx", "exited"
	reboot := srv(KindServerReboot, "reboot", 0, 0, 0)
	reboot.BootTime = now.Add(-90 * time.Second)
	mem := srv(KindServerAlert, "mem", 92, 90, 10)
	disk := srv(KindServerAlert, "disk", 91, 85, 1)
	disk.Mount = mount
	offline := srv(KindServerAlert, "offline", 3, 0, 3)
	offline.LastSeen = now.Add(-3 * time.Minute)
	cpuOK := srv(KindServerResolved, "cpu", 41, 90, 10)
	offlineOK := srv(KindServerResolved, "offline", 0, 0, 3)
	probeOff := Event{Kind: KindProbeOffline, Sample: true, Lang: lang, ProbeID: max(n.ServerID, 1), MonitorName: i18n.T(lang, "notify.sample.probe"),
		MonitorType: "probe", Target: "203.0.113.10", Metric: "offline", Time: now, LastSeen: now.Add(-2 * time.Minute)}
	probeOn := probeOff
	probeOn.Kind, probeOn.LastSeen, probeOn.Downtime = KindProbeOnline, time.Time{}, 7*time.Minute

	return []Event{down, reminder, up, slow, slowOK, locDown, locUp, cert, dom, cpuWarn, cpu, mem, disk, cont, reboot, offline, cpuOK, offlineOK, probeOff, probeOn}
}

// sampleDomain örnek hedefin ana makine adı (URL ise host'u; boşsa ornek.com).
func sampleDomain(target string) string {
	host := target
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/:"); i >= 0 {
		host = host[:i]
	}
	host = strings.TrimPrefix(host, "www.")
	if host == "" {
		return "ornek.com"
	}
	return host
}
