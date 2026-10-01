package notify

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// sink gelen webhook gövdelerini biriktirir.
type sink struct {
	mu   sync.Mutex
	got  []WebhookPayload
	srv  *httptest.Server
	path string
}

func newSink(t *testing.T) *sink {
	s := &sink{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p WebhookPayload
		json.NewDecoder(r.Body).Decode(&p)
		s.mu.Lock()
		s.got = append(s.got, p)
		s.mu.Unlock()
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *sink) events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, p := range s.got {
		out = append(out, p.Event)
	}
	return out
}

func (s *sink) last() WebhookPayload {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.got) == 0 {
		return WebhookPayload{}
	}
	return s.got[len(s.got)-1]
}

// rulesFixture: bir monitör, verilen kurallarla bir webhook kanalı, sahte saat.
type rulesFixture struct {
	st    *store.Store
	d     *Dispatcher
	sink  *sink
	ch    store.Notification
	m     store.Monitor
	clock time.Time
}

func newRulesFixture(t *testing.T, mut func(*store.Notification), bind bool) *rulesFixture {
	t.Helper()
	st := storetest.Open(t, time.UTC)
	ctx := context.Background()
	f := &rulesFixture{st: st, sink: newSink(t), clock: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	f.ch = store.Notification{Name: "Kanal", Type: "webhook", Active: true, Config: json.RawMessage(`{"url":"` + f.sink.srv.URL + `"}`)}
	if mut != nil {
		mut(&f.ch)
	}
	if err := st.CreateNotification(ctx, &f.ch, false); err != nil {
		t.Fatal(err)
	}
	f.m = store.Monitor{Name: "site", Type: "http", Active: true, Interval: 60, RetryInterval: 60, Timeout: 30,
		Config: json.RawMessage(`{"url":"https://example.com"}`)}
	ids := []int64{f.ch.ID}
	if !bind {
		ids = nil
	}
	if err := st.CreateMonitor(ctx, &f.m, ids); err != nil {
		t.Fatal(err)
	}
	f.d = NewDispatcher(st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	f.d.retryDelays = nil
	f.d.now = func() time.Time { return f.clock }
	return f
}

func (f *rulesFixture) notify(kind string, inc int64) {
	f.d.Notify(Event{Kind: kind, MonitorID: f.m.ID, MonitorName: f.m.Name, MonitorType: "http", Time: f.clock, IncidentID: inc})
	f.d.Wait(5 * time.Second)
}

// deliveries olayın işlem geçmişindeki bildirim kayıtları (eskiden yeniye).
func (f *rulesFixture) deliveries(t *testing.T, inc int64) []deliveryData {
	t.Helper()
	evs, err := f.st.IncidentEvents(context.Background(), inc)
	if err != nil {
		t.Fatal(err)
	}
	var out []deliveryData
	for i := len(evs) - 1; i >= 0; i-- {
		if evs[i].Kind != store.EventNotify {
			continue
		}
		var dd deliveryData
		json.Unmarshal(evs[i].Data, &dd)
		out = append(out, dd)
	}
	return out
}

// Olay türü süzgeci: yalnızca "up" alan kanala kesinti gitmez, düzelme gider;
// atlanan bildirim işlem geçmişine nedeniyle yazılır.
func TestRulesEventFilter(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.Events = []string{KindUp} }, true)
	ctx := context.Background()
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("kesinti süzgeçten geçmemeliydi: %v", got)
	}
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix()+60)
	f.notify(KindUp, inc)
	if got := f.sink.events(); len(got) != 1 || got[0] != KindUp {
		t.Fatalf("yalnızca düzelme gitmeliydi: %v", got)
	}
	dd := f.deliveries(t, inc)
	if len(dd) != 2 || dd[0].Skipped != SkipFilter || dd[0].Event != KindDown || !dd[1].OK {
		t.Fatalf("kayıtlar: %+v", dd)
	}
}

// Gecikme: kesinti N dk sürmezse ne 🔴 ne 🟢 gider; sürerse 🔴 "gecikmeli"
// notuyla gider, ardından 🟢 gider.
func TestRulesDelay(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.DelayMin = 5 }, true)
	ctx := context.Background()

	// Kısa kesinti: 2 dk sonra düzeldi.
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("gecikmeli kanala hemen gitmemeliydi: %v", got)
	}
	f.clock = f.clock.Add(2 * time.Minute)
	f.d.Sweep(ctx)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("vade dolmadan gitmemeliydi: %v", got)
	}
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix())
	f.notify(KindUp, inc)
	f.clock = f.clock.Add(10 * time.Minute)
	f.d.Sweep(ctx)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("kısa kesintide hiçbir bildirim gitmemeliydi: %v", got)
	}
	dd := f.deliveries(t, inc)
	var kinds []string
	for _, d := range dd {
		switch {
		case d.Deferred != 0:
			kinds = append(kinds, "deferred:"+d.Reason)
		case d.Skipped != "":
			kinds = append(kinds, "skip:"+d.Skipped)
		default:
			kinds = append(kinds, "sent:"+d.Event)
		}
	}
	if want := "deferred:delay,skip:cancelled,skip:unpaired"; strings.Join(kinds, ",") != want {
		t.Fatalf("işlem geçmişi %v, beklenen %s", kinds, want)
	}
	if n, _ := f.st.DueNotifications(ctx, f.clock.Unix()+1e6, 10); len(n) != 0 {
		t.Fatalf("kuyruk boş olmalıydı: %+v", n)
	}

	// Uzun kesinti: 5 dk sonra hâlâ açık.
	f.clock = f.clock.Add(time.Hour)
	inc2, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 502")
	f.notify(KindDown, inc2)
	f.notify(KindReminder, inc2) // 🔴 gitmeden hatırlatma da gitmez
	f.clock = f.clock.Add(5*time.Minute + time.Second)
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	got := f.sink.events()
	if len(got) != 1 || got[0] != KindDown {
		t.Fatalf("5 dk sonra kesinti gitmeliydi: %v", got)
	}
	if p := f.sink.last(); !p.Delayed || p.ElapsedSeconds < 300 || !strings.Contains(p.Text, "Gecikmeli bildirim") {
		t.Fatalf("gecikmeli not yok: %+v", p)
	}
	f.notify(KindReminder, inc2)
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix()+60)
	f.notify(KindUp, inc2)
	if got := f.sink.events(); len(got) != 3 || got[1] != KindReminder || got[2] != KindUp {
		t.Fatalf("hatırlatma ve düzelme gitmeliydi: %v", got)
	}
}

// Sessiz saatler: "hiçbiri" kipinde kesinti pencerenin bitimine ertelenir ve
// olay hâlâ açıksa gider; kritik kipte 🔴 geçer, 🟡 ertelenir, hatırlatma atılır.
func TestRulesQuietHours(t *testing.T) {
	// 22:00-07:00 UTC; saat 23:30.
	f := newRulesFixture(t, func(n *store.Notification) {
		n.Quiet = &store.QuietHours{Start: "22:00", End: "07:00", TZ: "UTC", Mode: store.QuietNone}
	}, true)
	ctx := context.Background()
	f.clock = time.Date(2026, 10, 1, 23, 30, 0, 0, time.UTC)
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc)
	f.notify(KindReminder, inc)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("sessiz saatte gitmemeliydi: %v", got)
	}
	dd := f.deliveries(t, inc)
	// Hatırlatma: 🔴 henüz gitmediği için eşleşmedi (sessiz saatten önce eşleştirme denetlenir).
	if len(dd) != 2 || dd[0].Deferred != time.Date(2026, 10, 2, 7, 0, 0, 0, time.UTC).Unix() || dd[0].Reason != store.QueueQuiet || dd[1].Skipped != SkipUnpaired {
		t.Fatalf("erteleme kaydı: %+v", dd)
	}
	f.clock = time.Date(2026, 10, 2, 6, 59, 0, 0, time.UTC)
	f.d.Sweep(ctx)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("pencere bitmeden gitmemeliydi: %v", got)
	}
	f.clock = time.Date(2026, 10, 2, 7, 0, 30, 0, time.UTC)
	f.d.Sweep(ctx)
	got := f.sink.events()
	if len(got) != 1 || got[0] != KindDown || !strings.Contains(f.sink.last().Text, "Sessiz saatler bitti") {
		t.Fatalf("pencere bitince kesinti gitmeliydi: %v %s", got, f.sink.last().Text)
	}

	// Kritik kip: başka bir kanal.
	g := newRulesFixture(t, func(n *store.Notification) {
		n.Quiet = &store.QuietHours{Start: "22:00", End: "07:00", TZ: "UTC", Mode: store.QuietCritical}
	}, true)
	g.clock = time.Date(2026, 10, 1, 23, 30, 0, 0, time.UTC)
	inc2, _ := g.st.StartIncident(ctx, g.m.ID, g.clock.Unix(), "HTTP 500")
	g.notify(KindDown, inc2)
	g.notify(KindSlow, inc2)
	if got := g.sink.events(); len(got) != 1 || got[0] != KindDown {
		t.Fatalf("kritik kipte yalnızca kesinti geçmeliydi: %v", got)
	}
	g.st.ResolveIncident(ctx, g.m.ID, g.clock.Unix()+60)
	g.notify(KindUp, inc2) // 🔴 gittiği için 🟢 de sessiz saatte geçer
	if got := g.sink.events(); len(got) != 2 || got[1] != KindUp {
		t.Fatalf("düzelme geçmeliydi: %v", got)
	}
}

// Gece yarısını aşan ve aşmayan pencereler, saat dilimi.
func TestQuietHoursActive(t *testing.T) {
	q := store.QuietHours{Start: "22:00", End: "07:00", TZ: "Europe/Istanbul"}
	loc, _ := time.LoadLocation("Europe/Istanbul")
	cases := []struct {
		at   time.Time
		want bool
		end  time.Time
	}{
		{time.Date(2026, 10, 1, 23, 0, 0, 0, loc), true, time.Date(2026, 10, 2, 7, 0, 0, 0, loc)},
		{time.Date(2026, 10, 2, 3, 0, 0, 0, loc), true, time.Date(2026, 10, 2, 7, 0, 0, 0, loc)},
		{time.Date(2026, 10, 2, 7, 0, 0, 0, loc), false, time.Time{}},
		{time.Date(2026, 10, 2, 12, 0, 0, 0, loc), false, time.Time{}},
		{time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC), true, time.Date(2026, 10, 2, 7, 0, 0, 0, loc)}, // 23:00 İstanbul
	}
	for _, c := range cases {
		got, end := q.Active(c.at)
		if got != c.want || (got && !end.Equal(c.end)) {
			t.Errorf("%s: %v %v (beklenen %v %v)", c.at, got, end, c.want, c.end)
		}
	}
	day := store.QuietHours{Start: "09:00", End: "17:00", TZ: "UTC"}
	if ok, _ := day.Active(time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)); !ok {
		t.Error("gündüz penceresi aktif olmalıydı")
	}
	if ok, _ := day.Active(time.Date(2026, 10, 1, 18, 0, 0, 0, time.UTC)); ok {
		t.Error("gündüz penceresi dışında olmalıydı")
	}
	bad := store.QuietHours{Start: "25:00", End: "07:00"}
	if err := bad.Validate(); err == nil {
		t.Error("geçersiz saat kabul edilmemeli")
	}
}

// Eskalasyon: monitöre bağlı olmayan kanal N dk sonra 🔴 alır, olay kapanınca 🟢 de alır;
// aynı olay için ikinci kez bildirilmez.
func TestRulesEscalation(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.EscalateMin = 10 }, false)
	ctx := context.Background()
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc) // bağlı kanal yok
	f.clock = f.clock.Add(9 * time.Minute)
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 0 {
		t.Fatalf("10 dk dolmadan eskalasyon gitmemeliydi: %v", got)
	}
	f.clock = f.clock.Add(2 * time.Minute)
	f.d.Sweep(ctx)
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	got := f.sink.events()
	if len(got) != 1 || got[0] != KindDown {
		t.Fatalf("eskalasyon bir kez gitmeliydi: %v", got)
	}
	p := f.sink.last()
	if !p.Escalated || p.Monitor.Name != "site" || p.Monitor.Target != "https://example.com" || !strings.Contains(p.Text, "Eskalasyon") || p.Incident == nil || p.Incident.ID != inc {
		t.Fatalf("eskalasyon gövdesi: %+v", p)
	}
	// Yeniden başlatma: bellek sıfırlansa da teslimat kaydı tekrarlamayı önler.
	f.d.escDone = map[[2]int64]bool{}
	f.d.Sweep(ctx)
	f.d.Wait(5 * time.Second)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("eskalasyon tekrarlanmamalı: %v", got)
	}
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix())
	f.notify(KindUp, inc)
	if got := f.sink.events(); len(got) != 2 || got[1] != KindUp {
		t.Fatalf("düzelme eskalasyon kanalına gitmeliydi: %v", got)
	}
}

// Kanal dili: ayarlardaki dil tr olsa da kanal en ise metin İngilizcedir.
func TestRulesChannelLang(t *testing.T) {
	f := newRulesFixture(t, func(n *store.Notification) { n.Lang = "en" }, true)
	inc, _ := f.st.StartIncident(context.Background(), f.m.ID, f.clock.Unix(), "HTTP 500")
	f.notify(KindDown, inc)
	if got := f.sink.events(); len(got) != 1 {
		t.Fatalf("gitmeliydi: %v", got)
	}
	if title := f.sink.last().Title; !strings.Contains(title, "is down") {
		t.Fatalf("İngilizce başlık bekleniyordu: %q", title)
	}
}

// Kuralsız kanalda eski davranış korunur: 🔴 gönderilemese de 🟢 gider.
func TestRulesNoneKeepsLegacy(t *testing.T) {
	f := newRulesFixture(t, nil, true)
	ctx := context.Background()
	inc, _ := f.st.StartIncident(ctx, f.m.ID, f.clock.Unix(), "HTTP 500")
	f.st.ResolveIncident(ctx, f.m.ID, f.clock.Unix()+60)
	f.notify(KindUp, inc) // 🔴 hiç gönderilmedi
	if got := f.sink.events(); len(got) != 1 || got[0] != KindUp {
		t.Fatalf("kuralsız kanalda düzelme gitmeliydi: %v", got)
	}
}
