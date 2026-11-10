package metrics

import (
	"os"
	"strings"
	"testing"
	"time"
)

func readSec(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/sec/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestParseWinEvent4625(t *testing.T) {
	ev, ok := parseWinEvent(readSec(t, "evt_4625.xml"))
	if !ok {
		t.Fatal("çözülemedi")
	}
	want := time.Date(2026, 11, 10, 8, 15, 30, 123456700, time.UTC)
	if ev.ID != 4625 || ev.RecordID != 90211 || !ev.Time.Equal(want) || ev.User != "yonetici" || ev.IP != "198.51.100.7" {
		t.Fatalf("olay: %+v", ev)
	}
	if id, ok := parseWinEventRecord(readSec(t, "evt_4625.xml")); !ok || id != 90211 {
		t.Fatalf("kayıt kimliği %d", id)
	}
}

func TestParseWinEvent4740(t *testing.T) {
	ev, ok := parseWinEvent(readSec(t, "evt_4740.xml"))
	if !ok || ev.ID != 4740 || ev.User != "muhasebe" || ev.Domain != "OFIS-PC12" || ev.IP != "" || ev.RecordID != 90214 {
		t.Fatalf("olay: %+v %v", ev, ok)
	}
}

func TestParseWinEventBad(t *testing.T) {
	for _, s := range []string{"", "<Event>", "<Event><System><EventID>x</EventID></System></Event>",
		"<Event><System><EventID>4625</EventID><TimeCreated SystemTime='dün'/></System></Event>"} {
		if _, ok := parseWinEvent(s); ok {
			t.Fatalf("kabul edilmemeli: %q", s)
		}
	}
	// IpAddress "-" ve yerel olmayan ad boş kalır.
	s := strings.Replace(readSec(t, "evt_4625.xml"), "::ffff:198.51.100.7", "-", 1)
	if ev, ok := parseWinEvent(s); !ok || ev.IP != "" {
		t.Fatalf("ip: %+v", ev)
	}
}

func TestCountWinEvents(t *testing.T) {
	a, _ := parseWinEvent(readSec(t, "evt_4625.xml"))
	l, _ := parseWinEvent(readSec(t, "evt_4740.xml"))
	agg := &authAgg{}
	countWinEvents(agg, []winEvent{a, a, l, {ID: 4740, Time: l.Time}, {ID: 4624, Time: a.Time}})
	if agg.Failed != 2 || agg.Accepted != 0 || agg.IPs["198.51.100.7"] != 2 || agg.Users["yonetici"] != 2 {
		t.Fatalf("birikim: %+v", agg)
	}
	if len(agg.Locked) != 1 || agg.Locked[0] != (SecLock{User: "muhasebe", Time: l.Time.Unix(), Source: "OFIS-PC12"}) {
		t.Fatalf("kilit: %+v", agg.Locked)
	}
	out := agg.ToAuth(AuthSourceEventLog)
	if out.Source != "eventlog" || len(out.Locked) != 1 || len(out.Hours) != 1 || out.TopIPs[0].Key != "198.51.100.7" {
		t.Fatalf("tel: %+v", out)
	}
	// Gönderilen kopya kabul edilince düşer; sonradan gelen kilit kalır.
	sent := agg.clone()
	agg.addLocked(SecLock{User: "x", Time: 5})
	agg.sub(sent)
	if agg.Failed != 0 || len(agg.Locked) != 1 || agg.Locked[0].User != "x" || agg.empty() {
		t.Fatalf("sub: %+v", agg)
	}
	agg.sub(agg.clone())
	if !agg.empty() {
		t.Fatalf("boş olmalı: %+v", agg)
	}
}

func TestWinEvtQuery(t *testing.T) {
	if q := winEvtQuery(42); q != "*[System[(EventID=4625 or EventID=4740) and EventRecordID > 42]]" {
		t.Fatal(q)
	}
	if q := winEvtQuery(0); !strings.Contains(q, "timediff(@SystemTime) <= 86400000") {
		t.Fatal(q)
	}
}

func TestWUPackages(t *testing.T) {
	u := wuPackages([]wuUpdate{
		{Title: "2026-11 Cumulative Update for Windows Server 2022 (KB5049000)", KBs: []string{"5049000"}, Severity: "Critical"},
		{Title: "Security Intelligence Update for Microsoft Defender Antivirus - KB2267602 (Version 1.421.1)", KBs: []string{"2267602"},
			Categories: []string{"{0FA1201D-4330-4FA8-8AE9-B877473B6441}"}},
		{Title: "Feature update to Windows 11, version 25H2 (KB5043178)"},
		{Title: "Some driver"},
		{},
	})
	if u.Manager != "wu" || u.Total != 4 || u.Security != 2 || len(u.Packages) != 4 {
		t.Fatalf("%+v", u)
	}
	p := u.Packages
	if p[0].Name != "KB5049000" || p[0].Severity != "Critical" || !p[0].Security || p[1].Name != "KB2267602" || !p[1].Security {
		t.Fatalf("%+v", p[:2])
	}
	if p[2].Name != "KB5043178" || p[2].Security || p[3].Name != "Some driver" || p[3].New != "Some driver" {
		t.Fatalf("%+v", p[2:])
	}
	s := Security{Updates: u}
	s.Sanitize()
	if s.Updates.Security != 2 || s.Updates.Total != 4 {
		t.Fatalf("sanitize: %+v", s.Updates)
	}
}

func TestWUFailures(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	w := wuFailures([]wuHistory{
		{Title: "Update (KB5049000)", Date: now.Add(-24 * time.Hour), Code: 4, Operation: 1},
		{Title: "Update (KB5048000)", Date: now.Add(-2 * time.Hour), Code: 5, Operation: 1},
		{Title: "Update (KB5047000)", Date: now.Add(-1 * time.Hour), Code: 2, Operation: 1},
		{Title: "Eski (KB5040000)", Date: now.AddDate(0, 0, -40), Code: 4, Operation: 1},
		{Title: "Kaldırma (KB5041000)", Date: now, Code: 4, Operation: 2},
	}, now)
	if w.Failed30d != 2 || len(w.Failed) != 2 || w.Failed[0].KB != "KB5048000" || w.Failed[0].Code != 5 || w.Failed[1].KB != "KB5049000" {
		t.Fatalf("%+v", w)
	}
}
