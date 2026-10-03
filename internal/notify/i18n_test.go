package notify

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

var trLetters = regexp.MustCompile(`[çğıöşüÇĞİÖŞÜ]`)

// TestEnglishEvents her olay türünün İngilizce metninde Türkçe kalıntı
// olmadığını ve beklenen ifadeleri içerdiğini denetler.
func TestEnglishEvents(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	evs := SampleEvents(SampleNames{}, now, "en")
	if len(evs) == 0 {
		t.Fatal("örnek yok")
	}
	for _, ev := range evs {
		txt := ev.Text()
		if trLetters.MatchString(txt) {
			t.Errorf("%s/%s: Türkçe kalıntı:\n%s", ev.Kind, ev.Metric, txt)
		}
		if !strings.Contains(txt, "(Sample notification — not a real event)") || !strings.Contains(txt, "Time: 2026-09-28") {
			t.Errorf("%s: örnek notu/zaman yok:\n%s", ev.Kind, txt)
		}
	}
	want := map[int]string{
		0:  "🔴 Example Site is down",
		1:  "🔴 Example Site is still down",
		2:  "🟢 Example Site is up again",
		3:  "🟡 Example Site is responding slowly",
		4:  "🟢 Example Site response time back to normal",
		5:  "🟡 Example Site: location outage",
		6:  "🟢 Example Site: all locations up",
		7:  "⚠️ Example Site: SSL certificate expires in 7 days",
		8:  "🟡 Example Site: domain example.com expires in 14 days",
		9:  "🟡 Example Server: CPU 82% (10 min average, warning threshold 80%)",
		10: "🔴 Example Server: CPU 94% (10 min average, threshold 90%)",
		12: "🔴 Example Server: Disk (/home) 91% (1 min average, threshold 85%)",
		13: "🔴 Example Server: container nginx is not running",
		14: "🔄 Example Server was rebooted",
		15: "🔴 Example Server: server unreachable",
		16: "🟢 Example Server: CPU back to normal",
		17: "🟢 Example Server: sending data again",
		18: "🔴 Frankfurt: check location unreachable",
		19: "🟢 Frankfurt: check location back online",
	}
	for i, w := range want {
		if got := evs[i].Title(); got != w {
			t.Errorf("başlık %d: %q, %q bekleniyordu", i, got, w)
		}
	}
	if txt := evs[2].Text(); !strings.Contains(txt, "Downtime: 1h 12m") {
		t.Errorf("kesinti süresi:\n%s", txt)
	}
	if txt := evs[3].Text(); !strings.Contains(txt, "Average response: 2840 ms (last 3 checks)") || !strings.Contains(txt, "Threshold: 2000 ms") {
		t.Errorf("yavaş yanıt ayrıntısı:\n%s", txt)
	}
	if txt := evs[15].Text(); !strings.Contains(txt, "Info: Last data: 2026-09-28") || !strings.Contains(txt, "Server: server01") {
		t.Errorf("çevrimdışı ayrıntısı:\n%s", txt)
	}
	if txt := evs[13].Text(); !strings.Contains(txt, "Container: nginx") || !strings.Contains(txt, "Info: exited") {
		t.Errorf("konteyner ayrıntısı:\n%s", txt)
	}
	if txt := evs[14].Text(); !strings.Contains(txt, "Boot time: 2026-09-28") {
		t.Errorf("yeniden başlatma ayrıntısı:\n%s", txt)
	}
	if txt := evs[18].Text(); !strings.Contains(txt, "Check location: ") || !strings.Contains(txt, "Info: Last data: 2026-09-28") {
		t.Errorf("kontrol noktası ayrıntısı:\n%s", txt)
	}

	test := Event{Kind: KindTest, MonitorName: "Test", Time: now, Lang: "en"}
	if got := test.Text(); !strings.HasPrefix(got, "✅ Test notification\nYour "+brand.Name+" notification channel is working.") {
		t.Errorf("test bildirimi: %q", got)
	}
	cert := Event{Kind: KindCert, MonitorName: "Site", CertDays: 1, Lang: "en", Time: now}
	if got := cert.Title(); got != "⚠️ Site: SSL certificate expires in 1 day" {
		t.Errorf("tekil gün: %q", got)
	}
	load := Event{Kind: KindServerAlert, MonitorName: "S", Metric: "load", Value: 1.254, Threshold: 1, Minutes: 5, Lang: "en"}
	if got := load.Title(); got != "🔴 S: Load 1.25 (5 min average, per core, threshold 1.00)" {
		t.Errorf("yük: %q", got)
	}
	gone := Event{Kind: KindServerResolved, MonitorName: "S", Metric: "temp", GoneMinutes: 3, Lang: "en", Time: now}
	if txt := gone.Text(); !strings.Contains(txt, "Info: No value for 3 minutes") {
		t.Errorf("değer gelmiyor:\n%s", txt)
	}
}

// TestTurkishUnchanged Lang boşken metin eskisiyle birebir aynı kalır.
func TestTurkishUnchanged(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.Local)
	up := Event{Kind: KindUp, MonitorName: "API", Target: "https://x", Time: now, Downtime: 72 * time.Minute}
	want := "🟢 API tekrar çalışıyor\nHedef: https://x\nKesinti süresi: 1 sa 12 dk\nZaman: 28.09.2026 12:00:00"
	if got := up.Text(); got != want {
		t.Errorf("tr metin değişti:\n%q\n%q", got, want)
	}
	off := Event{Kind: KindServerAlert, ProbeID: 1, MonitorName: "S", Metric: "offline", Target: "h", Time: now, LastSeen: now.Add(-3 * time.Minute)}
	if txt := off.Text(); !strings.Contains(txt, "Ayrıntı: Son veri: 28.09.2026 11:57:00") {
		t.Errorf("tr çevrimdışı:\n%s", txt)
	}
}

// Çok konumlu kesintide her konum ayrı satırda; başlığı ayrı giden kanalların
// gövdesi başlıkla başlamaz.
func TestLocationLinesAndBody(t *testing.T) {
	ev := Event{Kind: KindDown, Lang: "tr", MonitorName: "Ayder", Target: "https://a.example",
		Message:   "Ana sunucu: HTTP 403 Forbidden (ulaşılamayan: CP Server IST)",
		Time:      time.Date(2026, 10, 1, 10, 58, 1, 0, time.Local),
		Locations: []LocationNote{{Name: "Ana sunucu", Message: "HTTP 403 Forbidden"}, {Name: "CP Server IST", NoData: true}}}
	body := ev.Body()
	want := "Hedef: https://a.example\nKonumlar:\n• Ana sunucu: HTTP 403 Forbidden\n• CP Server IST: Kontrol noktasına ulaşılamıyor\nZaman: "
	if !strings.HasPrefix(body, want) {
		t.Fatalf("gövde:\n%s", body)
	}
	if strings.Contains(body, "Neden") || strings.Contains(body, ev.Title()) {
		t.Fatalf("gövdede Neden satırı ya da başlık olmamalı:\n%s", body)
	}
	if ev.Text() != ev.Title()+"\n"+body {
		t.Fatalf("Text = başlık + gövde olmalı:\n%s", ev.Text())
	}
	ev.Lang = "en"
	if b := ev.Body(); !strings.Contains(b, "Locations:\n• Ana sunucu: HTTP 403 Forbidden\n• CP Server IST: Check location unreachable") {
		t.Fatalf("İngilizce gövde:\n%s", b)
	}
	// Konum bilgisi yoksa eskisi gibi "Neden".
	ev.Lang, ev.Locations = "tr", nil
	if b := ev.Body(); !strings.Contains(b, "\nNeden: Ana sunucu: HTTP 403") {
		t.Fatalf("konumsuz gövde:\n%s", b)
	}
}
