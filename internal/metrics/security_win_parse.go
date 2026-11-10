package metrics

import (
	"encoding/xml"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Windows olay günlüğü (4625/4740) ve Windows Update sonuçları için saf
// ayrıştırıcılar (iş 734, docs/GUVENLIK-EKLENTI-730.md §A.2). Build etiketi
// yok: testdata/sec/ altındaki kendi örneklerimizle her ortamda sınanır.

// Olay kimlikleri (Security günlüğü).
const (
	evtLogonFailed   = 4625
	evtAccountLocked = 4740
)

// winEvent EvtRender(EvtRenderEventXml) çıktısından gereken alanlar.
type winEvent struct {
	ID       int
	RecordID uint64
	Time     time.Time
	User     string // TargetUserName
	Domain   string // TargetDomainName (4740'ta kilidi tetikleyen bilgisayar)
	IP       string // IpAddress (4625; "-" ya da geçersizse boş)
}

type evtXML struct {
	System struct {
		EventID     string `xml:"EventID"`
		RecordID    string `xml:"EventRecordID"`
		TimeCreated struct {
			SystemTime string `xml:"SystemTime,attr"`
		} `xml:"TimeCreated"`
	} `xml:"System"`
	Data []struct {
		Name  string `xml:"Name,attr"`
		Value string `xml:",chardata"`
	} `xml:"EventData>Data"`
}

// parseWinEvent tek bir olay XML'ini çözer.
func parseWinEvent(s string) (winEvent, bool) {
	var x evtXML
	if err := xml.Unmarshal([]byte(s), &x); err != nil {
		return winEvent{}, false
	}
	var ev winEvent
	var err error
	if ev.ID, err = strconv.Atoi(strings.TrimSpace(x.System.EventID)); err != nil {
		return winEvent{}, false
	}
	ev.RecordID, _ = strconv.ParseUint(strings.TrimSpace(x.System.RecordID), 10, 64)
	ev.Time, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(x.System.TimeCreated.SystemTime))
	if err != nil {
		return winEvent{}, false
	}
	for _, d := range x.Data {
		v := strings.TrimSpace(d.Value)
		if v == "-" {
			v = ""
		}
		switch d.Name {
		case "TargetUserName":
			ev.User = v
		case "TargetDomainName":
			ev.Domain = v
		case "IpAddress":
			ev.IP = winIP(v)
		}
	}
	return ev, true
}

// winIP IPv4 eşlemeli IPv6'yı (::ffff:a.b.c.d) düz IPv4'e çevirir; IP değilse boş.
func winIP(s string) string {
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.String()
}

// countWinEvents 4625'leri Linux'taki SSH özetiyle aynı biçimde (saat, IP,
// kullanıcı) birikime ekler, 4740'ları kilitlenme listesine yazar.
func countWinEvents(agg *authAgg, events []winEvent) {
	for _, ev := range events {
		switch ev.ID {
		case evtLogonFailed:
			agg.addFailed(ev.Time, ev.IP, ev.User, false, 1)
		case evtAccountLocked:
			if ev.User != "" {
				agg.addLocked(SecLock{User: ev.User, Time: ev.Time.Unix(), Source: ev.Domain})
			}
		}
	}
}

// winEvtQuery Security günlüğü XPath sorgusu: imleç (EventRecordID) varsa
// ondan sonrakiler, yoksa son 24 saat.
func winEvtQuery(after uint64) string {
	cond := "(EventID=4625 or EventID=4740)"
	if after > 0 {
		return "*[System[" + cond + " and EventRecordID > " + strconv.FormatUint(after, 10) + "]]"
	}
	return "*[System[" + cond + " and TimeCreated[timediff(@SystemTime) <= " +
		strconv.FormatInt(authFirstWindow.Milliseconds(), 10) + "]]]"
}

// Windows Update.
const (
	wuSecurityCategory = "0fa1201d-4330-4fa8-8ae9-b877473b6441" // "Security Updates" kategori kimliği (dilden bağımsız)
	wuMaxPkgs          = 200
	wuHistoryDays      = 30
)

// wuUpdate IUpdate'ten okunan alanlar.
type wuUpdate struct {
	Title      string
	KBs        []string // KBArticleIDs (yalnız rakam ya da "KB..." biçimi)
	Severity   string   // MsrcSeverity
	Categories []string // CategoryID
}

// wuHistory IUpdateHistoryEntry.
type wuHistory struct {
	Title     string
	Date      time.Time
	Code      int // OperationResultCode: 2 başarılı, 3 hatalı başarılı, 4 başarısız, 5 yarıda kaldı
	Operation int // 1 kurulum, 2 kaldırma
}

var kbRe = regexp.MustCompile(`(?i)\bKB ?(\d{5,8})\b`)

// wuKB ilk KB numarası ("KB5031234"); yoksa başlıktan aranır.
func wuKB(kbs []string, title string) string {
	for _, k := range kbs {
		k = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(k)), "KB")
		if k != "" && strings.Trim(k, "0123456789") == "" {
			return "KB" + k
		}
	}
	if m := kbRe.FindStringSubmatch(title); m != nil {
		return "KB" + m[1]
	}
	return ""
}

// wuPackages IUpdate listesini paket listesine çevirir (Name = KB, New = başlık).
func wuPackages(list []wuUpdate) *SecUpdates {
	u := &SecUpdates{Manager: ManagerWU, Packages: []SecPkg{}}
	for _, x := range list {
		title := strings.TrimSpace(x.Title)
		name := wuKB(x.KBs, title)
		if name == "" {
			name = title
		}
		if name == "" {
			continue
		}
		sev := strings.TrimSpace(x.Severity)
		sec := sev != "" || slices.ContainsFunc(x.Categories, func(c string) bool {
			return strings.EqualFold(strings.Trim(strings.TrimSpace(c), "{}"), wuSecurityCategory)
		})
		u.Total++
		if sec {
			u.Security++
		}
		if len(u.Packages) < wuMaxPkgs {
			u.Packages = append(u.Packages, SecPkg{Name: name, New: title, Security: sec, Severity: sev})
		} else {
			u.Truncated = true
		}
	}
	return u
}

// wuFailures son 30 gündeki başarısız/yarıda kalan kurulumlar.
func wuFailures(hist []wuHistory, now time.Time) *SecWU {
	w := &SecWU{}
	from := now.AddDate(0, 0, -wuHistoryDays)
	for _, h := range hist {
		if h.Operation == 2 || (h.Code != 4 && h.Code != 5) || h.Date.Before(from) {
			continue
		}
		w.Failed30d++
		w.Failed = append(w.Failed, SecWUFail{KB: wuKB(nil, h.Title), Title: strings.TrimSpace(h.Title), Time: h.Date.Unix(), Code: h.Code})
	}
	slices.SortFunc(w.Failed, func(a, b SecWUFail) int { return int(b.Time - a.Time) })
	if len(w.Failed) > MaxSecWUFailed {
		w.Failed = w.Failed[:MaxSecWUFailed]
	}
	return w
}

// parseWinEventRecord yalnız EventRecordID (en yeni kayıt sorgusu; olay türü önemsiz).
func parseWinEventRecord(s string) (uint64, bool) {
	var x evtXML
	if xml.Unmarshal([]byte(s), &x) != nil {
		return 0, false
	}
	n, err := strconv.ParseUint(strings.TrimSpace(x.System.RecordID), 10, 64)
	return n, err == nil
}
