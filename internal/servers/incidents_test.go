package servers

import (
	"context"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func (e *testEnv) serverIncidents() []store.Incident {
	e.t.Helper()
	list, err := e.st.ListIncidents(context.Background(), store.IncidentFilter{ServerID: e.probe.ID})
	if err != nil {
		e.t.Fatal(err)
	}
	return list
}

// Kaynak uyarısı başlayınca sunucu olayı açılır (en yüksek/son değer
// güncellenir), bitince kapanır; çevrimdışı uyarısı da olay açar ve veri
// gelince kapanır. Bildirimler eskisi gibi gider.
func TestServerAlertIncidents(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	e.send(cpu(1)) // varsayılan kurallar (offline 3 dk dahil)
	e.notif.take()
	if _, err := e.st.ReplaceServerAlerts(ctx, e.probe.ID, []store.ServerAlert{
		{Metric: MetricOffline, Minutes: 3, Active: true},
		{Metric: MetricCPU, Threshold: 80, Minutes: 1, Active: true},
	}, e.clock.Unix()); err != nil {
		t.Fatal(err)
	}

	e.send(cpu(85))
	list := e.serverIncidents()
	if len(list) != 1 || list[0].Kind != store.IncidentServerAlert || list[0].ResolvedAt != 0 || list[0].ServerName != "CP Server İstanbul" {
		t.Fatalf("uyarı olayı: %+v", list)
	}
	e.send(cpu(95))
	e.send(cpu(82))
	d := store.ParseServerIncidentData(e.serverIncidents()[0].Data)
	if d.Metric != MetricCPU || d.Threshold != 80 || d.Value != 85 || d.Peak != 95 || d.Last != 82 {
		t.Fatalf("olay değerleri: %+v", d)
	}
	e.send(cpu(10))
	list = e.serverIncidents()
	d = store.ParseServerIncidentData(list[0].Data)
	if list[0].ResolvedAt == 0 || d.Last != 10 || d.Peak != 95 {
		t.Fatalf("kapanış: %+v %+v", list[0], d)
	}
	if evs := e.notif.take(); len(evs) != 2 || evs[0].Kind != notify.KindServerAlert || evs[1].Kind != notify.KindServerResolved {
		t.Fatalf("bildirimler değişmemeli: %+v", evs)
	}

	// Çevrimdışı.
	last := e.getProbe().MetricsAt
	e.clock = e.clock.Add(5 * time.Minute)
	e.svc.CheckOffline(ctx)
	list = e.serverIncidents()
	if len(list) != 2 || list[0].Kind != store.IncidentServerOffline || list[0].ResolvedAt != 0 {
		t.Fatalf("çevrimdışı olayı: %+v", list)
	}
	if d := store.ParseServerIncidentData(list[0].Data); d.LastSeen != last || d.Metric != MetricOffline {
		t.Fatalf("çevrimdışı verisi: %+v (son veri %d)", d, last)
	}
	e.svc.CheckOffline(ctx) // ikinci tarama yeni olay açmaz
	if n := len(e.serverIncidents()); n != 2 {
		t.Fatalf("olay sayısı: %d", n)
	}
	e.send(cpu(5))
	if list = e.serverIncidents(); list[0].ResolvedAt == 0 {
		t.Fatal("veri gelince çevrimdışı olayı kapanmalı")
	}
	if evs := e.notif.take(); len(evs) != 2 {
		t.Fatalf("çevrimdışı bildirimleri: %+v", evs)
	}

	// Ajan devre dışı bırakılınca açık olay bildirimsiz kapanır.
	e.clock = e.clock.Add(5 * time.Minute)
	e.svc.CheckOffline(ctx)
	e.notif.take()
	if err := e.st.UpdateProbe(ctx, e.probe.ID, e.probe.Name, false); err != nil {
		t.Fatal(err)
	}
	e.svc.CheckOffline(ctx)
	list = e.serverIncidents()
	evs, _ := e.st.IncidentEvents(ctx, list[0].ID)
	if list[0].ResolvedAt == 0 || evs[0].Message != store.ServerIncidentResolveDisabled {
		t.Fatalf("devre dışı: %+v %+v", list[0], evs)
	}
}
