package notify

import (
	"strings"
	"testing"
	"time"
)

func TestSampleEvents(t *testing.T) {
	evs := SampleEvents(SampleNames{Monitor: "Ayder Tesisat", Target: "https://aydertesisat.com.tr", Server: "CP Server IST", Host: "cp", DiskMount: "/"}, time.Unix(1_790_000_000, 0), "tr")
	kinds := map[string]int{}
	for _, ev := range evs {
		kinds[ev.Kind]++
		if !strings.Contains(ev.Text(), "Örnek bildirim") {
			t.Errorf("%s: örnek notu yok:\n%s", ev.Kind, ev.Text())
		}
	}
	for _, k := range []string{KindDown, KindReminder, KindUp, KindLocationDown, KindLocationUp, KindCert, KindServerAlert, KindServerResolved} {
		if kinds[k] == 0 {
			t.Errorf("%s örneği yok", k)
		}
	}
	for _, ev := range evs {
		if ev.Metric == "disk" {
			if got := ev.Title(); got != "🔴 CP Server IST: Disk (/) %91 (1 dk ortalama, eşik %85)" {
				t.Errorf("disk başlığı: %s", got)
			}
		}
		if ev.Kind == KindLocationDown && (len(ev.Locations) != 1 || !strings.Contains(ev.Body(), "Frankfurt")) {
			t.Errorf("konum kesintisi örneği konum satırı taşımalı: %q", ev.Body())
		}
	}
}
