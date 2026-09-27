package notify

import "time"

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
// hatırlatma, tekrar çalışıyor, SSL sertifikası, sunucu CPU/RAM/disk ve
// çevrimdışı uyarıları ile düzelme bildirimleri. Hepsi Sample işaretlidir.
func SampleEvents(n SampleNames, now time.Time) []Event {
	if n.Monitor == "" {
		n.Monitor, n.Target = "Örnek Site", "https://ornek.com"
	}
	if n.Server == "" {
		n.Server, n.Host = "Örnek Sunucu", "sunucu01"
	}
	if n.CertIssuer == "" {
		n.CertIssuer = "Let's Encrypt"
	}
	mount := n.DiskMount
	if mount == "" {
		mount = "/home"
	}
	mon := func(kind string) Event {
		return Event{Kind: kind, Sample: true, MonitorID: n.MonitorID, MonitorName: n.Monitor, MonitorType: "http",
			Target: n.Target, Time: now, URL: n.MonitorURL, IncidentURL: n.IncidentURL}
	}
	srv := func(kind, metric string, value, threshold float64, minutes int) Event {
		return Event{Kind: kind, Sample: true, ProbeID: max(n.ServerID, 1), MonitorName: n.Server, MonitorType: "server",
			Target: n.Host, Time: now, URL: n.ServerURL, Metric: metric, Value: value, Threshold: threshold, Minutes: minutes}
	}

	down := mon(KindDown)
	down.Message = "HTTP 503 Service Unavailable"
	reminder := mon(KindReminder)
	reminder.Message, reminder.Downtime = "HTTP 503 Service Unavailable", time.Hour
	up := mon(KindUp)
	up.Downtime = 1*time.Hour + 12*time.Minute
	cert := mon(KindCert)
	cert.IncidentURL = ""
	cert.CertDays, cert.CertExpires, cert.CertIssuer = 7, now.AddDate(0, 0, 7), n.CertIssuer

	cpu := srv(KindServerAlert, "cpu", 94, 90, 10)
	mem := srv(KindServerAlert, "mem", 92, 90, 10)
	disk := srv(KindServerAlert, "disk", 91, 85, 1)
	disk.Mount = mount
	offline := srv(KindServerAlert, "offline", 3, 0, 3)
	offline.Message = "Son veri: " + now.Add(-3*time.Minute).Local().Format("02.01.2006 15:04:05")
	cpuOK := srv(KindServerResolved, "cpu", 41, 90, 10)
	offlineOK := srv(KindServerResolved, "offline", 0, 0, 3)

	return []Event{down, reminder, up, cert, cpu, mem, disk, offline, cpuOK, offlineOK}
}
