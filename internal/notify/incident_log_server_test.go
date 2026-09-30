package notify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// QA 2. tur D2: sunucu olayında da bildirim durumu işlem geçmişine yazılır
// (kanal yoksa "bağlı etkin bildirim kanalı yok").
func TestDispatcherLogsServerIncident(t *testing.T) {
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	p := store.Probe{Kind: store.ProbeKindServer, Name: "Sunucu", Active: true, CreatedAt: 1, Hash: "h"}
	if err := st.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	rules, err := st.ReplaceServerAlerts(ctx, p.ID, []store.ServerAlert{{Metric: "cpu", Threshold: 80, Minutes: 1, Active: true}}, 1000)
	if err != nil {
		t.Fatal(err)
	}
	a := rules[0]
	a.ProbeID = p.ID
	if _, err := st.FireServerAlertAt(ctx, a, 90, "", 1000, 0); err != nil {
		t.Fatal(err)
	}
	id, err := st.OpenServerIncidentID(ctx, a.ID)
	if err != nil || id == 0 {
		t.Fatalf("açık sunucu olayı: %d %v", id, err)
	}
	d := NewDispatcher(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.Notify(Event{Kind: KindServerAlert, ProbeID: p.ID, MonitorName: "Sunucu", IncidentID: id, Metric: "cpu", Time: time.Now()})
	d.Wait(5 * time.Second)
	evs, _ := st.IncidentEvents(ctx, id)
	found := false
	for _, ev := range evs {
		var dd deliveryData
		if ev.Kind == store.EventNotify && json.Unmarshal(ev.Data, &dd) == nil && dd.None && dd.Event == KindServerAlert {
			found = true
		}
	}
	if !found {
		t.Fatalf("sunucu olayına bildirim kaydı yazılmadı: %+v", evs)
	}
}
