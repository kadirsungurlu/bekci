package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// incidentsOf monitörün verilen türdeki olayları (yeniden eskiye).
func kindIncidents(t *testing.T, f *fixture, monitorID int64, group string) []store.Incident {
	t.Helper()
	list, err := f.st.ListIncidents(context.Background(), store.IncidentFilter{MonitorID: monitorID, Kind: group})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func eventKinds(t *testing.T, f *fixture, id int64) []string {
	t.Helper()
	evs, err := f.st.IncidentEvents(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(evs))
	for i, ev := range evs {
		out[len(evs)-1-i] = ev.Kind // eskiden yeniye
	}
	return out
}

func countKind(kinds []string, k string) int {
	n := 0
	for _, x := range kinds {
		if x == k {
			n++
		}
	}
	return n
}

func withDetail(res check.Result, loc string) check.Result {
	res.Detail = &check.Detail{Kind: "tcp", Error: res.Message + " @" + loc}
	return res
}

// Kısmi kesinti: bir konum (tekrar denemeden sonra) çalışmıyorken monitör
// çalışıyorsa bildirimsiz kısmi olay açılır; tam kesintide normal olaya
// dönüşür; konumlar düzelince kapanır.
func TestPartialIncidentLifecycle(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b, c := f.probe(t, "Ankara", true), f.probe(t, "Berlin", true), f.probe(t, "Chicago", true)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 1 })
	r := f.locRunner(t, m, store.LocationSetup{ProbeIDs: []int64{a.ID, b.ID, c.ID}, DownWhen: store.DownWhenMajority})
	step := func() { f.clock = f.clock.Add(10 * time.Millisecond) }

	for _, p := range []store.Probe{a, b, c} {
		step()
		r.remote(p, f.clock, up())
	}
	if r.m.Status != store.StatusUp {
		t.Fatalf("UP bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}

	// İlk hata: konum tekrar deneniyor → kısmi olay açılmaz.
	step()
	r.remote(a, f.clock, withDetail(down("Bağlantı reddedildi"), "A"))
	if n := len(kindIncidents(t, f, m.ID, store.KindGroupPartial)); n != 0 {
		t.Fatalf("tekrar denenen konum kısmi olay açmamalı: %d", n)
	}
	// Tekrar deneme hakkı bitti: konum çalışmıyor, monitör (çoğunluk) çalışıyor.
	step()
	r.remote(a, f.clock, withDetail(down("Bağlantı reddedildi"), "A"))
	if r.m.Status != store.StatusUp {
		t.Fatalf("monitör UP kalmalı: %d", r.m.Status)
	}
	parts := kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if len(parts) != 1 || parts[0].ResolvedAt != 0 || parts[0].Kind != store.IncidentPartial ||
		parts[0].Cause != "Ankara: Bağlantı reddedildi" {
		t.Fatalf("kısmi olay: %+v", parts)
	}
	var pd store.PartialIncidentData
	if json.Unmarshal(parts[0].Data, &pd); len(pd.Locations) != 1 || pd.Locations[0] != "Ankara" {
		t.Fatalf("kısmi olay verisi: %s", parts[0].Data)
	}
	if cap, ok, _ := f.st.GetIncidentCapture(ctx, parts[0].ID); !ok || cap.Location != "Ankara" ||
		!strings.Contains(string(cap.Detail), "@A") {
		t.Fatalf("kısmi olayın yakalaması: %+v %v", cap, ok)
	}
	if n := len(kindIncidents(t, f, m.ID, store.KindGroupMonitor)); n != 0 {
		t.Fatalf("normal olay açılmamalı: %d", n)
	}
	if got := f.n.kinds(); len(got) != 0 {
		t.Fatalf("kısmi kesinti bildirim göndermemeli: %v", got)
	}
	// Özet sayacında ve herkese açık listede görünmez.
	if n, _ := f.st.CountIncidentsSince(ctx, 0); n != 0 {
		t.Fatalf("kısmi olay kesinti sayılmamalı: %d", n)
	}
	if list, _ := f.st.IncidentsFor(ctx, []int64{m.ID}, 0, 10); len(list) != 0 {
		t.Fatalf("kısmi olay herkese açık listede: %+v", list)
	}
	// Uptime etkilenmez: tüm kayıtlar UP.
	if pct, ok, _ := f.st.Uptime(ctx, m.ID, 0); !ok || pct != 100 {
		t.Fatalf("uptime: %v %v", pct, ok)
	}

	// İkinci konum da çalışmaz: çoğunluk → tam kesinti. Kısmi olay kapanır,
	// normal olay açılır, bildirim gider.
	step()
	r.remote(b, f.clock, down("Zaman aşımı"))
	step()
	r.remote(b, f.clock, down("Zaman aşımı"))
	if r.m.Status != store.StatusDown {
		t.Fatalf("DOWN bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
	}
	parts = kindIncidents(t, f, m.ID, store.KindGroupPartial)
	mons := kindIncidents(t, f, m.ID, store.KindGroupMonitor)
	if len(parts) != 1 || parts[0].ResolvedAt == 0 || len(mons) != 1 || mons[0].ResolvedAt != 0 {
		t.Fatalf("dönüşüm: kısmi %+v normal %+v", parts, mons)
	}
	pk := eventKinds(t, f, parts[0].ID)
	if pk[len(pk)-1] != store.EventEscalated || countKind(pk, store.EventUp) != 0 {
		t.Fatalf("kısmi olayın geçmişi: %v", pk)
	}
	evs, _ := f.st.IncidentEvents(ctx, parts[0].ID)
	if !strings.Contains(string(evs[0].Data), `"incident_id":`) || evs[0].Message != "Tam kesintiye dönüştü" {
		t.Fatalf("dönüşüm kaydı: %+v", evs[0])
	}
	if mk := eventKinds(t, f, mons[0].ID); countKind(mk, store.EventFromPartial) != 1 {
		t.Fatalf("normal olayın geçmişi: %v", mk)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("bildirimler: %v", got)
	}

	// Bir konum düzelir: monitör çalışır, normal olay kapanır; Berlin hâlâ
	// çalışmadığı için dönüştürülen kısmi olay yeniden açılır (yeni olay
	// açılmaz, geçmiş bölünmez). Berlin de düzelince kapanır.
	step()
	r.remote(a, f.clock, up())
	parts = kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if r.m.Status != store.StatusUp || len(parts) != 1 || parts[0].ResolvedAt != 0 ||
		kindIncidents(t, f, m.ID, store.KindGroupMonitor)[0].ResolvedAt == 0 {
		t.Fatalf("kısmi düzelme: %d %+v", r.m.Status, kindIncidents(t, f, m.ID, ""))
	}
	if pk := eventKinds(t, f, parts[0].ID); pk[len(pk)-1] != store.EventResumed || countKind(pk, store.EventEscalated) != 1 {
		t.Fatalf("yeniden açılan kısmi olayın geçmişi: %v", pk)
	}
	if upEv := f.n.events[len(f.n.events)-1]; upEv.Kind != notify.KindUp || len(upEv.Locations) != 1 || upEv.Locations[0].Name != "Berlin" {
		t.Fatalf("düzelme bildirimi hâlâ çalışmayan konumu listelemeli: %+v", upEv)
	}
	json.Unmarshal(parts[0].Data, &pd)
	if !equal(pd.Locations, []string{"Ankara", "Berlin"}) {
		t.Fatalf("etkilenen konumlar: %v", pd.Locations)
	}
	step()
	r.remote(b, f.clock, up())
	if parts = kindIncidents(t, f, m.ID, store.KindGroupPartial); parts[0].ResolvedAt == 0 {
		t.Fatalf("tüm konumlar düzelince kısmi olay kapanmalı: %+v", parts)
	}

	// Yeni kısmi kesinti ve kapanışı.
	step()
	r.remote(c, f.clock, down("HTTP 503"))
	step()
	r.remote(c, f.clock, down("HTTP 503"))
	parts = kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if len(parts) != 2 || parts[0].ResolvedAt != 0 {
		t.Fatalf("ikinci kısmi olay: %+v", parts)
	}
	step()
	r.remote(c, f.clock, up())
	parts = kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if parts[0].ResolvedAt == 0 {
		t.Fatal("konum düzelince kısmi olay kapanmalı")
	}
	evs, _ = f.st.IncidentEvents(ctx, parts[0].ID)
	if evs[0].Kind != store.EventUp || evs[0].Message != "Tüm konumlar çalışıyor" || i18n.Message(i18n.EN, evs[0].Message) != "All locations are up" {
		t.Fatalf("kapanış kaydı: %+v", evs[0])
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("kısmi kesinti bildirim göndermemeli: %v", got)
	}
}

// Yeniden başlatmada açık kısmi olay kaldığı yerden sürer (aynı konum
// değişimi ikinci kez yazılmaz); kaldırılan konum olayı kapatır; konum ayarı
// tümden kaldırılınca olay kapanır.
func TestPartialIncidentRestartAndRemoval(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	a, b, c := f.probe(t, "Ankara", true), f.probe(t, "Berlin", true), f.probe(t, "Chicago", true)
	m := f.monitor(t, nil)
	setup := store.LocationSetup{ProbeIDs: []int64{a.ID, b.ID, c.ID}, DownWhen: store.DownWhenAll}
	r := f.locRunner(t, m, setup)
	step := func() { f.clock = f.clock.Add(10 * time.Millisecond) }
	for _, p := range []store.Probe{a, b} {
		step()
		r.remote(p, f.clock, up())
	}
	step()
	r.remote(c, f.clock, down("Bağlantı reddedildi"))
	parts := kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if len(parts) != 1 || parts[0].ResolvedAt != 0 {
		t.Fatalf("kısmi olay açılmalı (tekrar deneme yok): %+v", parts)
	}
	id := parts[0].ID
	before := len(eventKinds(t, f, id))

	// Yeniden başlatma: yeni runner konumları baştan ("ilk sonuç bekleniyor") kurar.
	restart := func(s store.LocationSetup) *runner {
		t.Helper()
		if err := f.st.SetMonitorLocations(ctx, m.ID, s); err != nil {
			t.Fatal(err)
		}
		got, _ := f.st.GetMonitor(ctx, m.ID)
		nr := &runner{e: f.e, m: got, checker: fake}
		nr.locs = f.e.loadLocations(got)
		nr.confirmed = nr.initialConfirmed(ctx)
		return nr
	}
	r = restart(setup)
	if r.partialID != id {
		t.Fatalf("açık kısmi olay yüklenmedi: %d", r.partialID)
	}
	step()
	r.remote(c, f.clock, down("Bağlantı reddedildi"))
	for _, p := range []store.Probe{a, b} {
		step()
		r.remote(p, f.clock, up())
	}
	if got := len(eventKinds(t, f, id)); got != before {
		t.Fatalf("yeniden başlatmadan sonra aynı konum durumu tekrar yazıldı: %v", eventKinds(t, f, id))
	}
	if parts = kindIncidents(t, f, m.ID, store.KindGroupPartial); parts[0].ResolvedAt != 0 {
		t.Fatal("olay açık kalmalı")
	}

	// Çalışmayan konum kaldırıldı: kalanlar çalışıyor → olay kapanır.
	r = restart(store.LocationSetup{ProbeIDs: []int64{a.ID, b.ID}, DownWhen: store.DownWhenAll})
	for _, p := range []store.Probe{a, b} {
		step()
		r.remote(p, f.clock, up())
	}
	if parts = kindIncidents(t, f, m.ID, store.KindGroupPartial); parts[0].ResolvedAt == 0 {
		t.Fatalf("konum kaldırılınca olay kapanmalı: %v", eventKinds(t, f, id))
	}

	// Yeni kısmi olay; konum ayarı tümden kaldırılınca (tek konumlu) kapanır.
	r = restart(setup)
	for _, p := range []store.Probe{a, b} {
		step()
		r.remote(p, f.clock, up())
	}
	step()
	r.remote(c, f.clock, down("Bağlantı reddedildi"))
	parts = kindIncidents(t, f, m.ID, store.KindGroupPartial)
	if len(parts) != 2 || parts[0].ResolvedAt != 0 {
		t.Fatalf("ikinci kısmi olay: %+v", parts)
	}
	if err := f.st.SetMonitorLocations(ctx, m.ID, store.DefaultLocations()); err != nil {
		t.Fatal(err)
	}
	got, _ := f.st.GetMonitor(ctx, m.ID)
	nr := &runner{e: f.e, m: got, checker: fake}
	nr.locs = f.e.loadLocations(got)
	if nr.locs != nil {
		t.Fatal("konum ayarı kaldırılmalıydı")
	}
	nr.initialConfirmed(ctx)
	if parts = kindIncidents(t, f, m.ID, store.KindGroupPartial); parts[0].ResolvedAt == 0 {
		t.Fatal("tek konumluya dönen monitörün kısmi olayı kapanmalı")
	}
	if got := f.n.kinds(); len(got) != 0 {
		t.Fatalf("bildirim gitmemeli: %v", got)
	}
}

// Bulgu: ters modda hedefe ulaşılamadığı için UP sayılan sonuç yalnızca
// "Bağlantı reddedildi" diyordu (yanıltıcı). Artık ters mod açıklaması eklenir
// (DOWN tarafındaki "hedef erişilebilir" açıklamasıyla aynı biçimde).
func TestUpsideDownUpMessage(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.UpsideDown = true })
	r := f.runnerFor(t, m.ID)
	r.process(down("Bağlantı reddedildi"))
	if r.m.Status != store.StatusUp || r.m.LastMessage != "Ters mod: hedef erişilemiyor (Bağlantı reddedildi)" {
		t.Fatalf("ters mod UP: %d %q", r.m.Status, r.m.LastMessage)
	}
	if en := i18n.Message(i18n.EN, r.m.LastMessage); en != "Upside down mode: target is unreachable (Connection refused)" {
		t.Fatalf("çeviri: %q", en)
	}
	r.process(check.Result{PingMs: -1})
	if r.m.LastMessage != "Ters mod: hedef erişilemiyor" {
		t.Fatalf("mesajsız hata: %q", r.m.LastMessage)
	}
	r.process(up())
	if r.m.Status != store.StatusDown || r.m.LastMessage != "Ters mod: hedef erişilebilir (200 OK)" {
		t.Fatalf("ters mod DOWN: %d %q", r.m.Status, r.m.LastMessage)
	}

	// Çok konumlu: konum başına aynı açıklama.
	p := f.probe(t, "Paris", true)
	m2 := f.monitor(t, func(m *store.Monitor) { m.UpsideDown = true })
	r2 := f.locRunner(t, m2, store.LocationSetup{ProbeIDs: []int64{p.ID}, DownWhen: store.DownWhenAny})
	f.clock = f.clock.Add(time.Second)
	r2.remote(p, f.clock, down("Bağlantı reddedildi"))
	if r2.m.Status != store.StatusUp || r2.m.LastMessage != "Ters mod: hedef erişilemiyor (Bağlantı reddedildi)" {
		t.Fatalf("çok konumlu ters mod UP: %d %q", r2.m.Status, r2.m.LastMessage)
	}
}

// Bulgu: bakım penceresi sürerken her yeniden başlatma açık olaya yeni bir
// "bakım başladı" kaydı ekliyordu (4 kez, bitişsiz); ilk kayıt da pencere
// açık olayın üstüne eklendiğinde değil, sonraki yeniden başlatmada yazılıyordu.
func TestMaintenanceEventsIdempotent(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("502"))
	id, _ := f.st.OpenIncidentID(ctx, m.ID)
	if id == 0 {
		t.Fatal("olay açılmalı")
	}
	count := func() (int, int) {
		k := eventKinds(t, f, id)
		return countKind(k, store.EventMaintStart), countKind(k, store.EventMaintEnd)
	}

	// Pencere eklenir eklenmez (kontrol beklenmeden) kayıt yazılır.
	f.clock = f.clock.Add(time.Minute)
	f.window(t, m.ID, f.clock, f.clock.Add(30*time.Minute))
	if s, e := count(); s != 1 || e != 0 {
		t.Fatalf("pencere eklenince: başlangıç %d bitiş %d", s, e)
	}
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	// Üç kez yeniden başlatma (bakım dizini açılışta yeniden yüklenir).
	for range 3 {
		if err := f.e.ReloadMaintenance(ctx); err != nil {
			t.Fatal(err)
		}
		r = f.runnerFor(t, m.ID)
		if !r.maintLogged {
			t.Fatal("yeniden başlatmada yazılmış bakım kaydı tanınmalı")
		}
		f.clock = f.clock.Add(time.Minute)
		r.process(down("502"))
	}
	if s, e := count(); s != 1 || e != 0 {
		t.Fatalf("yeniden başlatmalardan sonra: başlangıç %d bitiş %d", s, e)
	}
	// Pencere biter: bitiş bir kez yazılır.
	f.clock = f.clock.Add(time.Hour)
	r.process(down("502"))
	r.process(down("502"))
	if err := f.e.ReloadMaintenance(ctx); err != nil {
		t.Fatal(err)
	}
	if s, e := count(); s != 1 || e != 1 {
		t.Fatalf("bakım bitince: başlangıç %d bitiş %d", s, e)
	}
}

// Yükseltilen kısmi olay: tam kesinti bitince uzak konum hâlâ çalışmıyorsa
// aynı kısmi olay yeniden açılır; ikinci bir konum kesintisi (🟡) bildirimi
// gitmez, düzelme (🟢) bildirimi hâlâ çalışmayan konumu listeler. Yeniden
// başlatma bu bağı unutturmaz.
func TestEscalatedPartialResumesWithoutSecondNotification(t *testing.T) {
	for _, restart := range []bool{false, true} {
		f := newFixture(t)
		ctx := context.Background()
		fra := f.probe(t, "Frankfurt", true)
		m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 0 })
		setup := store.LocationSetup{IncludeLocal: true, ProbeIDs: []int64{fra.ID}, DownWhen: store.DownWhenAll, NotifyPartial: true}
		r := f.locRunner(t, m, setup)
		step := func() { f.clock = f.clock.Add(10 * time.Millisecond) }
		defer fake.set(up())

		fake.set(up())
		r.locationTick(ctx)
		step()
		r.remote(fra, f.clock, down("DNS hatası"))
		pid := r.partialID
		if pid == 0 {
			t.Fatal("konum kesintisi açılmalıydı")
		}
		// Ana sunucu da düşer: tam kesinti, kısmi olay dönüşür.
		fake.set(down("HTTP 503"))
		step()
		r.locationTick(ctx)
		if r.m.Status != store.StatusDown || r.partialID != 0 || r.escalatedPartial != pid {
			t.Fatalf("dönüşüm: durum %d kısmi %d dönüşen %d", r.m.Status, r.partialID, r.escalatedPartial)
		}
		if restart {
			r = f.runnerFor(t, m.ID)
			r.locs = f.e.loadLocations(r.m)
			if r.escalatedPartial != pid || r.incidentID == 0 {
				t.Fatalf("yeniden başlatmada dönüşüm bağı yüklenmeli: %d (olay %d)", r.escalatedPartial, r.incidentID)
			}
			step()
			r.remote(fra, f.clock, down("DNS hatası"))
		}
		// Ana sunucu düzelir, Frankfurt hâlâ çalışmıyor.
		fake.set(up())
		step()
		r.locationTick(ctx)
		if r.m.Status != store.StatusUp {
			t.Fatalf("UP bekleniyordu: %d %q", r.m.Status, r.m.LastMessage)
		}
		if r.partialID != pid {
			t.Fatalf("aynı kısmi olay yeniden açılmalı: %d → %d", pid, r.partialID)
		}
		parts := kindIncidents(t, f, m.ID, store.KindGroupPartial)
		if len(parts) != 1 || parts[0].ResolvedAt != 0 {
			t.Fatalf("kısmi olaylar: %+v", parts)
		}
		evs, _ := f.st.IncidentEvents(ctx, pid)
		if evs[0].Kind != store.EventResumed || !strings.Contains(string(evs[0].Data), `"incident_id":`) {
			t.Fatalf("yeniden açılma kaydı: %+v", evs[0])
		}
		got := f.n.kinds()
		if !equal(got, []string{notify.KindLocationDown, notify.KindDown, notify.KindUp}) {
			t.Fatalf("bildirimler (restart=%v): %v", restart, got)
		}
		upEv := f.n.events[2]
		if len(upEv.Locations) != 1 || upEv.Locations[0].Name != "Frankfurt" || upEv.Locations[0].Message != "DNS hatası" {
			t.Fatalf("düzelme bildirimi hâlâ çalışmayan konumu listelemeli: %+v", upEv.Locations)
		}
		if body := upEv.Body(); !strings.Contains(body, "Frankfurt: DNS hatası") {
			t.Fatalf("bildirim gövdesi: %q", body)
		}
		// Frankfurt düzelir: kısmi olay kapanır, 🟢 aynı olaya bağlı.
		step()
		r.remote(fra, f.clock, up())
		if r.partialID != 0 {
			t.Fatal("konum düzelince kısmi olay kapanmalıydı")
		}
		if got := f.n.kinds(); !equal(got, []string{notify.KindLocationDown, notify.KindDown, notify.KindUp, notify.KindLocationUp}) {
			t.Fatalf("bildirimler: %v", got)
		}
		if f.n.events[3].IncidentID != pid {
			t.Fatalf("konum düzelme bildirimi ilk kısmi olaya bağlı olmalı: %d", f.n.events[3].IncidentID)
		}
	}
}
