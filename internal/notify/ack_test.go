package notify

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// Onaylı olayda hatırlatma kanala gitmez (işlem geçmişine "acked" nedeniyle
// yazılır), eskalasyon yapılmaz; onay geri alınınca eskalasyon gider.
func TestAckStopsReminderAndEscalation(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.EscalateMin = 10 }, true)
	ctx := context.Background()
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("kesinti gitmeliydi: %v", got)
	}
	if err := f.st.AckIncident(ctx, inc, "kadir", "bakıyoruz", f.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	f.notify(KindReminder, inc)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("onaylı olayda hatırlatma gitmemeliydi: %v", got)
	}
	dd := f.deliveries(t, inc)
	if len(dd) != 2 || dd[1].Event != KindReminder || dd[1].Skipped != SkipAcked {
		t.Fatalf("hatırlatma 'acked' nedeniyle atlanmalıydı: %+v", dd)
	}
	// Eskalasyon: 10 dk doldu ama olay onaylı.
	f.clock = f.clock.Add(11 * time.Minute)
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("onaylı olay eskalasyona gitmemeliydi: %v", got)
	}
	// Onay geri alındı: bir sonraki taramada eskalasyon... kanal bağlı olduğu
	// için zaten kesintiyi aldı (duplicate değil, Delivered). Bağlı olmayan
	// eskalasyon kanalıyla da denenir:
	esc := store.Notification{Name: "Nöbet", Type: "webhook", Active: true, EscalateMin: 10,
		Config: json.RawMessage(`{"url":"` + f.sink.srv.URL + `"}`)}
	if err := f.st.CreateNotification(ctx, &esc, false); err != nil {
		t.Fatal(err)
	}
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("onaylı olay yeni eskalasyon kanalına da gitmemeliydi: %v", got)
	}
	if err := f.st.UnackIncident(ctx, inc, "kadir", f.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	got := f.sink.events()
	if len(got) != 2 || got[1] != KindDown || !f.sink.last().Escalated {
		t.Fatalf("onay kalkınca eskalasyon gitmeliydi: %v", got)
	}
}

// Susturma süresi dolunca eskalasyon kendiliğinden devam eder.
func TestSnoozeExpires(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.EscalateMin = 10 }, false)
	ctx := context.Background()
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	until := f.clock.Add(30 * time.Minute).Unix()
	if err := f.st.SnoozeIncident(ctx, inc, until, "kadir", f.clock.Unix()); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(11 * time.Minute)
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("susturulmuş olay eskalasyona gitmemeliydi: %v", got)
	}
	f.notify(KindReminder, inc)
	if dd := f.deliveries(t, inc); len(dd) != 1 || dd[0].Skipped != SkipSnoozed {
		t.Fatalf("hatırlatma 'snoozed' nedeniyle atlanmalıydı: %+v", dd)
	}
	f.clock = f.clock.Add(20 * time.Minute) // susturma bitti
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 1 || got[0] != KindDown {
		t.Fatalf("susturma bitince eskalasyon gitmeliydi: %v", got)
	}
	// Onaylı/susturulmuş olay kapanırken düzelme bildirimi yine gider.
	f.st.AckIncident(ctx, inc, "kadir", "", f.clock.Unix())
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix())
	f.notify(KindUp, inc)
	if got := f.sink.events(); len(got) != 2 || got[1] != KindUp {
		t.Fatalf("düzelme gitmeliydi: %v", got)
	}
}

// "acked" türü yalnızca açıkça seçen kanallara gider; süzgeçsiz kanal almaz.
func TestAckedEventOptIn(t *testing.T) {
	f := newRulesFixture(t, nil, true) // süzgeçsiz kanal
	ctx := context.Background()
	opt := newSink(t)
	ch := store.Notification{Name: "Onaylar", Type: "webhook", Active: true, Events: []string{KindAcked},
		Config: json.RawMessage(`{"url":"` + opt.srv.URL + `"}`)}
	if err := f.st.CreateNotification(ctx, &ch, true); err != nil {
		t.Fatal(err)
	}
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.d.Notify(Event{Kind: KindAcked, MonitorID: f.m.ID, MonitorName: f.m.Name, MonitorType: "http", Time: f.clock,
		IncidentID: inc, AckedBy: "kadir", AckNote: "bakıyoruz"})
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("süzgeçsiz kanal onay bildirimi almamalıydı: %v", got)
	}
	got := opt.events()
	if len(got) != 1 || got[0] != KindAcked {
		t.Fatalf("opt-in kanal onay bildirimini almalıydı: %v", got)
	}
	p := opt.last()
	if p.Ack == nil || p.Ack.By != "kadir" || p.Ack.Note != "bakıyoruz" || p.Incident == nil {
		t.Fatalf("ack gövdesi: %+v", p)
	}
	// Almayan kanal için işlem geçmişine "atlandı" satırı yazılmaz.
	for _, dd := range f.deliveries(t, inc) {
		if dd.Event == KindAcked && dd.Skipped != "" {
			t.Fatalf("onay için atlama kaydı yazılmamalıydı: %+v", dd)
		}
	}
	if !ch.Accepts(KindAcked) || f.ch.Accepts(KindAcked) || !f.ch.Accepts(KindDown) {
		t.Fatal("Accepts opt-in kuralı yanlış")
	}
}
