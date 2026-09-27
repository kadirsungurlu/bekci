package notify

import (
	"strings"
	"testing"
	"time"
)

func TestSampleEvents(t *testing.T) {
	evs := SampleEvents(SampleNames{Monitor: "Ayder Tesisat", Target: "https://aydertesisat.com.tr", Server: "CP Server IST", Host: "cp", DiskMount: "/"}, time.Unix(1_790_000_000, 0))
	kinds := map[string]int{}
	for _, ev := range evs {
		kinds[ev.Kind]++
		if !strings.Contains(ev.Text(), "Örnek bildirim") {
			t.Errorf("%s: örnek notu yok:\n%s", ev.Kind, ev.Text())
		}
	}
	for _, k := range []string{KindDown, KindReminder, KindUp, KindCert, KindServerAlert, KindServerResolved} {
		if kinds[k] == 0 {
			t.Errorf("%s örneği yok", k)
		}
	}
	if got := evs[6].Title(); got != "🔴 CP Server IST: Disk (/) %91 (1 dk ortalama, eşik %85)" {
		t.Errorf("disk başlığı: %s", got)
	}
}
