package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/check"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

func kindsOf(evs []store.IncidentEvent) []string {
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[len(evs)-1-i] = ev.Kind // eskiden yeniye
	}
	return out
}

func downWithDetail(msg string, status int) check.Result {
	r := down(msg)
	r.Detail = &check.Detail{Method: "GET", URL: "https://site.test", Status: status, Body: "hata gövdesi"}
	return r
}

// Tek konumlu monitörün olay geçmişi: tekrar denemeler, başlangıç, hata
// değişimi, hatırlatma, bakım, çözülme; olayı açan kontrolün istek/yanıtı.
func TestIncidentTimeline(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 2; m.ResendEvery = 2 })
	r := f.runnerFor(t, m.ID)
	step := func(res check.Result) {
		f.clock = f.clock.Add(time.Minute)
		r.process(res)
	}

	step(up())
	// Tekrar denemede düzelen hata olay açmaz, denemeleri de unutulur.
	step(down("eski hata"))
	step(up())
	if n := openIncidents(t, f, m.ID); n != 0 || len(r.preDown) != 0 {
		t.Fatalf("olay açılmamalı: %d, bekleyen %d", n, len(r.preDown))
	}

	step(downWithDetail("HTTP 400 Bad Request", 400))
	step(downWithDetail("HTTP 400 Bad Request", 400))
	startAt := f.clock.Add(time.Minute)
	step(downWithDetail("HTTP 400 Bad Request", 400)) // DOWN
	id, _ := f.st.OpenIncidentID(ctx, m.ID)
	if id == 0 || r.incidentID != id {
		t.Fatalf("olay kimliği: %d / %d", id, r.incidentID)
	}
	step(down("Zaman aşımı")) // hata değişti; hatırlatma sayacı 1
	step(down("Zaman aşımı")) // hatırlatma
	f.window(t, m.ID, f.clock.Add(time.Minute), f.clock.Add(3*time.Minute))
	step(down("Zaman aşımı")) // bakımda
	f.clock = f.clock.Add(4 * time.Minute)
	step(up()) // bakım bitti + çözüldü

	evs, err := f.st.IncidentEvents(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{store.EventRetry, store.EventRetry, store.EventDown, store.EventChange, store.EventReminder,
		store.EventMaintStart, store.EventMaintEnd, store.EventUp}
	if got := kindsOf(evs); !equal(got, want) {
		t.Fatalf("kayıtlar %v, %v bekleniyordu", got, want)
	}
	retry, start, change, upEv := evs[7], evs[5], evs[4], evs[0]
	var att map[string]int
	json.Unmarshal(retry.Data, &att)
	if att["attempt"] != 1 || att["max"] != 2 || retry.Location != LocalName || retry.Time >= start.Time {
		t.Errorf("tekrar deneme kaydı: %+v", retry)
	}
	var sd struct {
		Locations []incidentLocation `json:"locations"`
	}
	json.Unmarshal(start.Data, &sd)
	if start.Time != startAt.Unix() || start.Message != "HTTP 400 Bad Request" || start.Location != LocalName ||
		len(sd.Locations) != 1 || sd.Locations[0].Status != locDown {
		t.Errorf("başlangıç kaydı: %+v", start)
	}
	if change.Message != "Zaman aşımı" {
		t.Errorf("değişim kaydı: %+v", change)
	}
	var dt map[string]int64
	json.Unmarshal(upEv.Data, &dt)
	if upEv.Message != "200 OK" || dt["downtime"] != int64(f.clock.Sub(startAt)/time.Second) {
		t.Errorf("çözülme kaydı: %+v", upEv)
	}

	c, ok, _ := f.st.GetIncidentCapture(ctx, id)
	var d check.Detail
	json.Unmarshal(c.Detail, &d)
	if !ok || c.Location != LocalName || d.Status != 400 || d.Body != "hata gövdesi" || c.Time != startAt.Unix() {
		t.Fatalf("yakalama: %+v %s", c, c.Detail)
	}

	// Bildirimler olay kimliğini ve sayfasını taşır; olay kapandıktan sonra sıfırlanır.
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindReminder, notify.KindUp}) {
		t.Fatalf("bildirimler %v", got)
	}
	for _, ev := range f.n.events {
		if ev.IncidentID != id || ev.IncidentURL != "https://uptime.test/#/incidents/"+itoa(id) || ev.URL != "https://uptime.test/#/monitors/"+itoa(m.ID) {
			t.Errorf("bildirim olay bilgisi: %+v", ev)
		}
	}
	if r.incidentID != 0 {
		t.Error("çözülünce olay kimliği sıfırlanmalı")
	}

	// Yeniden başlatmada açık olay runner'a geri yüklenir.
	step(down("502"))
	step(down("502"))
	step(down("502"))
	id2, _ := f.st.OpenIncidentID(ctx, m.ID)
	r2 := f.runnerFor(t, m.ID)
	if id2 == 0 || id2 == id || r2.incidentID != id2 || r2.lastCause != "502" {
		t.Fatalf("yeniden başlatma: %d %d %q", id2, r2.incidentID, r2.lastCause)
	}
}

// Çok konumlu monitör: yakalama çalışmayan konumdan, konum durum değişimleri
// işlem geçmişinde.
func TestIncidentLocations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fra := f.probe(t, "Frankfurt", true)
	m := f.monitor(t, nil)
	r := f.locRunner(t, m, store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAny})
	fake.set(up())
	defer fake.set(up())

	r.locationTick(ctx)
	f.clock = f.clock.Add(10 * time.Millisecond)
	if !r.remote(fra, f.clock, downWithDetail("HTTP 503 Service Unavailable", 503)) || r.m.Status != store.StatusDown {
		t.Fatalf("DOWN bekleniyordu: %d", r.m.Status)
	}
	id := r.incidentID
	c, ok, _ := f.st.GetIncidentCapture(ctx, id)
	if !ok || c.Location != "Frankfurt" {
		t.Fatalf("yakalama konumu: %+v", c)
	}
	// Frankfurt'ta hata değişir: konum adıyla "hata değişti".
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.remote(fra, f.clock, down("HTTP 502 Bad Gateway"))
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)

	// Ana sunucu da düşer, sonra Frankfurt düzelir, en son ana sunucu düzelir.
	fake.set(down("Zaman aşımı"))
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.remote(fra, f.clock, up())
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	fake.set(up())
	f.clock = f.clock.Add(10 * time.Millisecond)
	r.locationTick(ctx)
	if r.m.Status != store.StatusUp {
		t.Fatalf("UP bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}

	evs, _ := f.st.IncidentEvents(ctx, id)
	var locs []string
	for i := len(evs) - 1; i >= 0; i-- {
		switch evs[i].Kind {
		case store.EventLocation:
			locs = append(locs, evs[i].Location+"="+evs[i].Message)
		case store.EventChange:
			if evs[i].Location == "" {
				t.Errorf("çok konumluda birleşik mesaj değişimi yazılmamalı: %+v", evs[i])
			}
			locs = append(locs, "değişti:"+evs[i].Location+"="+evs[i].Message)
		}
	}
	want := []string{"değişti:Frankfurt=HTTP 502 Bad Gateway", "Ana sunucu=Çalışmıyor: Zaman aşımı", "Frankfurt=Çalışıyor", "Ana sunucu=Çalışıyor"}
	if !equal(locs, want) {
		t.Fatalf("konum kayıtları %v, %v bekleniyordu", locs, want)
	}
	start := evs[len(evs)-1]
	var sd struct {
		Locations []incidentLocation `json:"locations"`
	}
	json.Unmarshal(start.Data, &sd)
	if start.Kind != store.EventDown || start.Location != "Frankfurt" || len(sd.Locations) != 2 ||
		sd.Locations[0].Status != locUp || sd.Locations[1].Status != locDown {
		t.Fatalf("başlangıç kaydı: %+v %s", start, start.Data)
	}
	if evs[0].Kind != store.EventUp {
		t.Fatalf("son kayıt çözülme olmalı: %+v", evs[0])
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }
