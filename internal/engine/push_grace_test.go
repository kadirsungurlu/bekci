package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Push toleransı: sinyal beklenen aralık + tolerans geçmeden DOWN olmaz;
// geçince olur. Tolerans aralık biriminde (testte ms) sayılır.
func TestPushGracePeriod(t *testing.T) {
	f := newFixture(t)
	f.e.now = time.Now
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := f.monitor(t, func(m *store.Monitor) {
		m.Type, m.Interval, m.PushToken = check.TypePush, 100, "tok-tolerans"
		m.Config = json.RawMessage(`{"grace_sec":300}`)
	})
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.e.Push(ctx, "tok-tolerans", true, "", 5); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "push UP", func() bool {
		got, _ := f.st.GetMonitor(ctx, m.ID)
		return got.Status == store.StatusUp
	})
	// Aralık (100 ms) geçti, tolerans (300 ms) henüz dolmadı: hâlâ UP.
	time.Sleep(200 * time.Millisecond)
	if got, _ := f.st.GetMonitor(ctx, m.ID); got.Status != store.StatusUp {
		t.Fatalf("tolerans dolmadan DOWN olmamalı: %d", got.Status)
	}
	waitFor(t, "tolerans sonrası DOWN", func() bool {
		got, _ := f.st.GetMonitor(ctx, m.ID)
		return got.Status == store.StatusDown
	})
	cancel()
	f.e.Wait()
}

// Push ayarı: tolerans doğrulanır ve eski boş ayar değişmeden kalır.
func TestPushConfigNormalize(t *testing.T) {
	c, _ := check.Get(check.TypePush)
	if out, err := c.Normalize(json.RawMessage(`{}`)); err != nil || string(out) != "{}" {
		t.Errorf("boş ayar: %s %v", out, err)
	}
	if out, err := c.Normalize(json.RawMessage(`{"grace_sec":900}`)); err != nil || check.PushGrace(out) != 900 {
		t.Errorf("tolerans korunmalı: %s %v", out, err)
	}
	if _, err := c.Normalize(json.RawMessage(`{"grace_sec":-1}`)); err == nil {
		t.Error("negatif tolerans reddedilmeli")
	}
	if _, err := c.Normalize(json.RawMessage(`{"grace_sec":90000}`)); err == nil {
		t.Error("24 saatten uzun tolerans reddedilmeli")
	}
}
