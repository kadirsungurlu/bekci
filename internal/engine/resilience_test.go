package engine

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/store/storetest"
)

// rawDB testin veritabanına store dışından ikinci bir bağlantı açar.
func (f *fixture) rawDB(t *testing.T) *sql.DB {
	t.Helper()
	driver := "sqlite"
	if storetest.PG() {
		driver = "pgx"
	}
	db, err := sql.Open(driver, f.target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// failResolves olay kapatmayı (resolved_at yazımı) veritabanı tetikleyicisiyle
// başarısız kılar; dönen fonksiyon tetikleyiciyi kaldırır.
func (f *fixture) failResolves(t *testing.T) (restore func()) {
	t.Helper()
	db := f.rawDB(t)
	stmts := []string{`CREATE TRIGGER fail_resolve BEFORE UPDATE ON incidents
		FOR EACH ROW WHEN (NEW.resolved_at IS NOT NULL) BEGIN SELECT RAISE(ABORT, 'test: kapatma başarısız'); END`}
	drop := []string{"DROP TRIGGER fail_resolve"}
	if storetest.PG() {
		stmts = []string{
			`CREATE FUNCTION fail_resolve() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'test: kapatma başarısız'; END $$ LANGUAGE plpgsql`,
			`CREATE TRIGGER fail_resolve BEFORE UPDATE ON incidents
				FOR EACH ROW WHEN (NEW.resolved_at IS NOT NULL) EXECUTE FUNCTION fail_resolve()`,
		}
		drop = []string{"DROP TRIGGER fail_resolve ON incidents", "DROP FUNCTION fail_resolve()"}
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		for _, q := range drop {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func incidentsOf(t *testing.T, f *fixture, id int64) []store.Incident {
	t.Helper()
	list, err := f.st.ListIncidents(context.Background(), store.IncidentFilter{MonitorID: id})
	if err != nil {
		t.Fatal(err)
	}
	return list // yeniden eskiye
}

// Bulgu: UP kaydedildikten sonra olay kapatılamadan uygulama çökerse olay
// sonsuza dek açık kalıyor, sonraki kesinti yeni olay açamıyordu.
func TestRestartUpWithOpenIncident(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	m := f.monitor(t, nil)
	downAt := f.clock.Unix()
	if _, err := f.st.StartIncident(ctx, m.ID, downAt, "502"); err != nil {
		t.Fatal(err)
	}
	upAt := f.clock.Add(5 * time.Minute).Unix()
	if err := f.st.RecordBeat(ctx, store.BeatUpdate{
		Beat:         store.Beat{MonitorID: m.ID, Time: upAt, Status: store.StatusUp, PingMs: 10, Message: "200 OK"},
		LastChangeAt: upAt,
	}); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Hour) // yeniden başlatma çok sonra

	r := f.runnerFor(t, m.ID)
	if r.confirmed != store.StatusUp || r.incidentID != 0 {
		t.Fatalf("UP ve olaysız başlamalı: %d %d", r.confirmed, r.incidentID)
	}
	list := incidentsOf(t, f, m.ID)
	if len(list) != 1 || list[0].ResolvedAt != upAt {
		t.Fatalf("olay UP anında kapanmalı: %+v", list)
	}
	evs, _ := f.st.IncidentEvents(ctx, list[0].ID)
	if k := kindsOf(evs); len(k) == 0 || k[len(k)-1] != store.EventUp {
		t.Fatalf("çözülme kaydı yazılmalı: %v", k)
	}

	// Sonraki kesinti YENİ bir olay açar.
	f.clock = f.clock.Add(time.Minute)
	r.process(down("zaman aşımı"))
	list = incidentsOf(t, f, m.ID)
	if len(list) != 2 || list[0].ResolvedAt != 0 || list[0].Cause != "zaman aşımı" || list[1].ResolvedAt != upAt {
		t.Fatalf("yeni olay açılmalı: %+v", list)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("yalnızca yeni DOWN bildirilmeli: %v", got)
	}
}

// Bulgu: UP'ta olay kapatılamazsa bir daha denenmiyordu.
func TestResolveRetriedAfterFailure(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("502"))
	if len(incidentsOf(t, f, m.ID)) != 1 {
		t.Fatal("olay açılmalı")
	}

	restore := f.failResolves(t)
	f.clock = f.clock.Add(time.Minute)
	upAt := f.clock.Unix()
	r.process(up())
	f.clock = f.clock.Add(time.Minute)
	r.process(up()) // hâlâ başarısız
	if list := incidentsOf(t, f, m.ID); list[0].ResolvedAt != 0 {
		t.Fatalf("tetikleyici varken kapanmamalı: %+v", list)
	}
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindUp}) {
		t.Fatalf("UP bildirimi yine gitmeli: %v", got)
	}

	restore()
	f.clock = f.clock.Add(time.Minute)
	r.process(up())
	list := incidentsOf(t, f, m.ID)
	if len(list) != 1 || list[0].ResolvedAt != upAt {
		t.Fatalf("olay ilk UP anıyla kapanmalı: %+v", list)
	}

	f.clock = f.clock.Add(time.Minute)
	r.process(down("503"))
	if list := incidentsOf(t, f, m.ID); len(list) != 2 || list[0].ResolvedAt != 0 || list[0].Cause != "503" {
		t.Fatalf("yeni kesinti yeni olay açmalı: %+v", list)
	}
}

// Kapatma hâlâ başarısızken yeni kesinti başlarsa eski olay devam eder ve
// sonradan (monitör DOWN iken) kapatılmaz.
func TestUnresolvedIncidentContinuesOnNewDown(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, nil)
	r := f.runnerFor(t, m.ID)
	r.process(down("502"))
	restore := f.failResolves(t)
	f.clock = f.clock.Add(time.Minute)
	r.process(up())
	f.clock = f.clock.Add(time.Minute)
	r.process(down("503"))
	restore()
	f.clock = f.clock.Add(time.Minute)
	r.process(down("503"))
	list := incidentsOf(t, f, m.ID)
	if len(list) != 1 || list[0].ResolvedAt != 0 || r.unresolved != nil || r.incidentID != list[0].ID {
		t.Fatalf("eski olay açık kalmalı ve sürmeli: %+v %+v", list, r.unresolved)
	}
}

// Bulgu: aynı monitör için eşzamanlı Reload'lar eski ayarlı runner bırakabiliyor
// veya "zaten çalışıyor" hatası veriyordu.
func TestConcurrentReload(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := f.monitor(t, func(m *store.Monitor) { m.Interval = 100000 })
	if err := f.e.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := range 40 {
		wg.Go(func() {
			cur, err := f.st.GetMonitor(ctx, m.ID)
			if err != nil {
				errs <- err
				return
			}
			cur.Name = fmt.Sprintf("site-%d", i)
			if err := f.st.UpdateMonitor(ctx, &cur, nil, false); err != nil {
				errs <- err
				return
			}
			if err := f.e.Reload(ctx, m.ID); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("Reload: %v", err)
	}
	final, _ := f.st.GetMonitor(ctx, m.ID)
	f.e.mu.Lock()
	r, n := f.e.runners[m.ID], len(f.e.runners)
	f.e.mu.Unlock()
	if r == nil || n != 1 || r.m.Name != final.Name {
		t.Fatalf("runner son ayarla çalışmalı: runner=%v adet=%d ad=%q, veritabanı %q", r != nil, n, func() string {
			if r == nil {
				return ""
			}
			return r.m.Name
		}(), final.Name)
	}
	cancel()
	f.e.Wait()
}

// failBeats kontrol kaydı yazımını (heartbeats INSERT) veritabanı
// tetikleyicisiyle başarısız kılar; dönen fonksiyon tetikleyiciyi kaldırır.
func (f *fixture) failBeats(t *testing.T) (restore func()) {
	t.Helper()
	db := f.rawDB(t)
	stmts := []string{`CREATE TRIGGER fail_beat BEFORE INSERT ON heartbeats
		FOR EACH ROW BEGIN SELECT RAISE(ABORT, 'test: kayıt başarısız'); END`}
	drop := []string{"DROP TRIGGER fail_beat"}
	if storetest.PG() {
		stmts = []string{
			`CREATE FUNCTION fail_beat() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'test: kayıt başarısız'; END $$ LANGUAGE plpgsql`,
			`CREATE TRIGGER fail_beat BEFORE INSERT ON heartbeats FOR EACH ROW EXECUTE FUNCTION fail_beat()`,
		}
		drop = []string{"DROP TRIGGER fail_beat ON heartbeats", "DROP FUNCTION fail_beat()"}
	}
	for _, q := range stmts {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return func() {
		for _, q := range drop {
			if _, err := db.Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
}

// Bulgu (D-10): kontrol sonucu kaydedilemeyince tekrar deneme sayacı geri
// alınmıyordu; bir deneme hakkı boşa gidip monitör erken DOWN oluyordu.
func TestRetriesRolledBackWhenBeatFails(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.MaxRetries = 2 })
	r := f.runnerFor(t, m.ID)
	restore := f.failBeats(t)
	r.process(down("502"))
	if r.retries != 0 || r.confirmed != unknown {
		t.Fatalf("kaydedilmeyen deneme hak düşürmemeli: retries=%d confirmed=%d", r.retries, r.confirmed)
	}
	restore()
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	if r.m.Status != store.StatusPending || r.retries != 2 {
		t.Fatalf("iki deneme hakkı sonra hâlâ PENDING olmalı: %d retries=%d", r.m.Status, r.retries)
	}
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502"))
	if r.m.Status != store.StatusDown {
		t.Fatalf("üçüncü hatada DOWN: %d", r.m.Status)
	}
}

// Bulgu (D-11): hatırlatma sayacı bellekteydi; yeniden başlatma hatırlatma
// sıklığını sıfırlıyordu. Sayaç olayın DOWN kayıtlarından kurulur.
func TestReminderCounterSurvivesRestart(t *testing.T) {
	f := newFixture(t)
	m := f.monitor(t, func(m *store.Monitor) { m.ResendEvery = 3 })
	r := f.runnerFor(t, m.ID)
	r.process(down("502")) // olay açıldı, 🔴
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502")) // 1. ek kontrol
	if r.downBeats != 1 {
		t.Fatalf("downBeats=%d", r.downBeats)
	}
	f.clock = f.clock.Add(time.Minute)
	r = f.runnerFor(t, m.ID) // yeniden başlatma
	if r.downBeats != 1 {
		t.Fatalf("yeniden başlatmada sayaç korunmalı: %d", r.downBeats)
	}
	r.process(down("502")) // 2. ek kontrol
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown}) {
		t.Fatalf("henüz hatırlatma gitmemeli: %v", got)
	}
	f.clock = f.clock.Add(time.Minute)
	r.process(down("502")) // 3. ek kontrol → hatırlatma
	if got := f.n.kinds(); !equal(got, []string{notify.KindDown, notify.KindReminder}) {
		t.Fatalf("üçüncü ek kontrolde hatırlatma: %v", got)
	}
}
