package api

import (
	"encoding/json"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Kontrol ve tekrar deneme aralığı alt sınırı 5 sn; zaman aşımı aralığı aşamaz.
func TestIntervalLimits(t *testing.T) {
	cfg := json.RawMessage(`{"host":"example.com","port":443}`)
	in := func(iv, ri, to int) *monitorInput {
		return &monitorInput{Name: "x", Type: "tcp", Interval: iv, RetryInterval: ri, Timeout: to, Config: cfg}
	}
	for _, iv := range []int{5, 10, 15} {
		m, err := in(iv, iv, 0).toMonitor(true)
		if err != nil {
			t.Fatalf("aralık %d reddedildi: %v", iv, err)
		}
		if m.Interval != iv || m.RetryInterval != iv || m.Timeout < 1 || m.Timeout > iv {
			t.Errorf("aralık %d: %+v", iv, m)
		}
	}
	if m, _ := in(5, 0, 0).toMonitor(true); m.Timeout != 2 || m.RetryInterval != 5 {
		t.Errorf("varsayılan zaman aşımı/tekrar: %+v", m)
	}
	for _, c := range []struct{ iv, ri, to int }{{4, 5, 0}, {5, 4, 0}, {5, 5, 6}, {store.MaxIntervalSec + 1, 60, 0}} {
		if _, err := in(c.iv, c.ri, c.to).toMonitor(true); err == nil {
			t.Errorf("%+v kabul edilmemeliydi", c)
		}
	}
}
