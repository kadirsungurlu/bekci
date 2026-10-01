package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Migration 18: eski (türsüz) olaylar işlem geçmişi ve yakalamalarıyla birlikte
// "monitor" türüne taşınır; monitor_id boş olabilir (sunucu olayları); yabancı
// anahtarlar (monitör silinince olay, olay silinince geçmiş) çalışmaya devam eder.
func TestIncidentKindsMigration(t *testing.T) {
	ctx := context.Background()
	target := testTarget(t)
	restore := withoutMigrationsFrom(t, 17)
	old, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := old.schemaVersion(ctx); v != 16 {
		t.Fatalf("eski sürüm %d, 16 bekleniyordu", v)
	}
	m := newMonitor(t, old, "eski-site")
	other := newMonitor(t, old, "diger")
	// Kimlikler RETURNING ile alınır (PostgreSQL'de kimlik sütunu dizisi ilerlesin).
	ins := func(q string, args ...any) int64 {
		t.Helper()
		id, err := insertID(ctx, old.db, q, args...)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	i7 := ins("INSERT INTO incidents (monitor_id, started_at, resolved_at, cause) VALUES (?, 100, 200, 'HTTP 503')", m.ID)
	i8 := ins("INSERT INTO incidents (monitor_id, started_at, cause) VALUES (?, 300, 'Zaman aşımı')", m.ID)
	i9 := ins("INSERT INTO incidents (monitor_id, started_at, cause) VALUES (?, 400, 'x')", other.ID)
	if err := old.AddIncidentEvents(ctx, i7, IncidentEvent{Time: 100, Kind: EventDown, Message: "HTTP 503"},
		IncidentEvent{Time: 200, Kind: EventUp, Data: EventData(map[string]int{"downtime": 100})}); err != nil {
		t.Fatal(err)
	}
	if err := old.AddIncidentEvents(ctx, i9, IncidentEvent{Time: 400, Kind: EventDown}); err != nil {
		t.Fatal(err)
	}
	if err := old.SaveIncidentCapture(ctx, i7, 100, "Ana sunucu", []byte(`{"kind":"http","status":503}`)); err != nil {
		t.Fatal(err)
	}
	old.Close()

	restore()
	s, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if v, _ := s.schemaVersion(ctx); v != latestMigration() {
		t.Fatalf("sürüm %d, %d bekleniyordu", v, latestMigration())
	}
	inc, err := s.GetIncident(ctx, i7)
	if err != nil || inc.Kind != IncidentMonitor || inc.MonitorID != m.ID || inc.MonitorName != "eski-site" ||
		inc.StartedAt != 100 || inc.ResolvedAt != 200 || inc.Cause != "HTTP 503" || inc.ServerID != 0 {
		t.Fatalf("taşınan olay: %+v %v", inc, err)
	}
	if evs, _ := s.IncidentEvents(ctx, i7); len(evs) != 2 || evs[0].Kind != EventUp || string(evs[0].Data) != `{"downtime":100}` {
		t.Fatalf("taşınan geçmiş: %+v", evs)
	}
	if c, ok, _ := s.GetIncidentCapture(ctx, i7); !ok || c.Location != "Ana sunucu" {
		t.Fatalf("taşınan yakalama: %+v %v", c, ok)
	}
	// Açık olay yeni kodla bulunur; yeni olay açılmaz, id dizisi sürer.
	if id, _ := s.OpenIncidentID(ctx, m.ID); id != i8 {
		t.Fatalf("açık olay: %d", id)
	}
	if id, _ := s.StartPartialIncident(ctx, m.ID, 500, "A: x", nil); id <= i9 {
		t.Fatalf("yeni olay kimliği: %d", id)
	}
	// Monitörsüz (sunucu) olay yazılabilir.
	p := Probe{Kind: ProbeKindServer, Name: "cp", Active: true, CreatedAt: 1, Hash: "h"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "INSERT INTO incidents (kind, server_id, started_at) VALUES ('server_offline', ?, 600)", p.ID); err != nil {
		t.Fatalf("monitörsüz olay: %v", err)
	}
	// Yabancı anahtarlar: monitör silinince olayı ve olayın geçmişi gider.
	if err := s.DeleteMonitor(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_events WHERE incident_id = ?", i9).Scan(&n)
	if _, err := s.GetIncident(ctx, i9); err != ErrNotFound || n != 0 {
		t.Fatalf("silinen monitörün olayı/geçmişi kaldı: %v %d", err, n)
	}
	// Sunucu silinince olayı gider.
	if _, err := s.DeleteProbe(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incidents WHERE server_id IS NOT NULL").Scan(&n)
	if n != 0 {
		t.Fatalf("silinen sunucunun olayı kaldı: %d", n)
	}
}

// Paralel dallarda eksik kalan migration numarası atlanır ve eklendiğinde
// (sürüm ileride olsa da) bir kez uygulanır.
func TestMigrationGapAppliedLater(t *testing.T) {
	ctx := context.Background()
	target := testTarget(t)
	ddl := migrations[16]
	delete(migrations, 16)
	put := func() {
		if _, ok := migrations[16]; !ok {
			migrations[16] = ddl
		}
	}
	t.Cleanup(put)
	s, err := Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := s.schemaVersion(ctx); v != latestMigration() {
		t.Fatalf("boşluklu sürüm %d", v)
	}
	if _, err := s.db.ExecContext(ctx, "SELECT lang FROM users"); err == nil {
		t.Fatal("eksik migration uygulanmamalıydı")
	}
	s.Close()

	put()
	s, err = Open(target, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.db.ExecContext(ctx, "SELECT lang FROM users"); err != nil {
		t.Fatalf("sonradan eklenen migration uygulanmadı: %v", err)
	}
	skipped, _ := s.skippedMigrations(ctx)
	for _, v := range skipped {
		if v == 16 {
			t.Fatalf("uygulanan migration atlanmış listesinde kaldı: %v", skipped)
		}
	}
	// Üçüncü açılış: tekrar uygulanmaz (hata olmaz).
	s2, err := Open(target, time.UTC)
	if err != nil {
		t.Fatalf("üçüncü açılış: %v", err)
	}
	s2.Close()
}

// Sunucu uyarısı başlayınca olay açılır, değerleri güncellenir, bitince (veya
// kural kaldırılınca) kapanır.
func TestServerIncidents(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	p := Probe{Kind: ProbeKindServer, Name: "CP", Active: true, CreatedAt: 1, Hash: "h"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	rules, err := s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{
		{Metric: "cpu", Threshold: 90, Minutes: 10, Active: true},
		{Metric: "disk", Mount: "/home", Threshold: 85, Minutes: 1, Active: true},
		{Metric: "offline", Minutes: 3, Active: true},
	}, 0)
	if err != nil {
		t.Fatal(err)
	}
	byMetric := map[string]ServerAlert{}
	for _, a := range rules {
		byMetric[a.Metric] = a
	}
	cpu, disk, off := byMetric["cpu"], byMetric["disk"], byMetric["offline"]

	if ok, err := s.FireServerAlert(ctx, cpu, 93.4, "", 1000); !ok || err != nil {
		t.Fatalf("tetikleme: %v %v", ok, err)
	}
	list, _ := s.ListIncidents(ctx, IncidentFilter{ServerID: p.ID})
	if len(list) != 1 || list[0].Kind != IncidentServerAlert || list[0].ServerName != "CP" || list[0].MonitorID != 0 ||
		list[0].Cause != "CPU %93 (10 dk ortalama, eşik %90)" {
		t.Fatalf("sunucu olayı: %+v", list)
	}
	if en := ServerIncidentCause("en", list[0].Kind, ParseServerIncidentData(list[0].Data)); en != "CPU 93% (10 min average, threshold 90%)" {
		t.Fatalf("İngilizce neden: %q", en)
	}
	for _, v := range []float64{97, 95} {
		if err := s.UpdateServerIncidentValue(ctx, cpu.ID, v); err != nil {
			t.Fatal(err)
		}
	}
	inc, _ := s.GetIncident(ctx, list[0].ID)
	d := ParseServerIncidentData(inc.Data)
	if d.Metric != "cpu" || d.Threshold != 90 || d.Minutes != 10 || d.Value != 93.4 || d.Peak != 97 || d.Last != 95 {
		t.Fatalf("olay verisi: %+v", d)
	}
	if ok, _ := s.ResolveServerAlert(ctx, cpu.ID, 1600); !ok {
		t.Fatal("kapatma")
	}
	inc, _ = s.GetIncident(ctx, list[0].ID)
	evs, _ := s.IncidentEvents(ctx, inc.ID)
	if inc.ResolvedAt != 1600 || len(evs) != 2 || evs[0].Kind != EventUp || string(evs[0].Data) != `{"downtime":600}` ||
		evs[1].Kind != EventDown {
		t.Fatalf("kapanan olay: %+v %+v", inc, evs)
	}

	// Çevrimdışı: son veri zamanı olay verisinde; disk kuralı silinince açık olayı kapanır.
	if ok, _ := s.FireServerAlertAt(ctx, off, 5, "", 2000, 1700); !ok {
		t.Fatal("çevrimdışı tetiklenmedi")
	}
	if ok, _ := s.FireServerAlert(ctx, disk, 91, "/home", 2000); !ok {
		t.Fatal("disk tetiklenmedi")
	}
	list, _ = s.ListIncidents(ctx, IncidentFilter{ServerID: p.ID, Kind: KindGroupServer})
	if len(list) != 3 || list[1].Kind != IncidentServerOffline || list[1].Cause != "Sunucuya ulaşılamıyor" ||
		!strings.Contains(string(list[1].Data), `"last_seen":1700`) || !strings.Contains(list[0].Cause, "Disk (/home) %91") {
		t.Fatalf("olaylar: %+v", list)
	}
	if _, err := s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{{Metric: "offline", Minutes: 3, Active: true}}, 2100); err != nil {
		t.Fatal(err)
	}
	inc, _ = s.GetIncident(ctx, list[0].ID)
	evs, _ = s.IncidentEvents(ctx, inc.ID)
	if inc.ResolvedAt != 2100 || evs[0].Message != ServerIncidentResolveRule {
		t.Fatalf("kural silinince: %+v %+v", inc, evs[0])
	}
	if inc, _ := s.GetIncident(ctx, list[1].ID); inc.ResolvedAt != 0 {
		t.Fatal("çevrimdışı olayı açık kalmalı")
	}
	// Kısmi/sunucu olayları kesinti sayacına ve herkese açık listeye girmez.
	if n, _ := s.CountIncidentsSince(ctx, 0); n != 0 {
		t.Fatalf("sayaç: %d", n)
	}
}

// Görünürlük süzgeci: kısıtlı kullanıcı yalnızca izinli monitörlerin ve
// atanmış sunucuların olaylarını görür; tür süzgeci gruplar.
func TestListIncidentsVisibilityAndKinds(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m1, m2 := newMonitor(t, s, "m1"), newMonitor(t, s, "m2")
	p1 := Probe{Kind: ProbeKindServer, Name: "s1", Active: true, CreatedAt: 1, Hash: "h1"}
	p2 := Probe{Kind: ProbeKindServer, Name: "s2", Active: true, CreatedAt: 1, Hash: "h2"}
	s.CreateProbe(ctx, &p1)
	s.CreateProbe(ctx, &p2)
	s.OpenIncident(ctx, m1.ID, 10, "a")
	s.OpenIncident(ctx, m2.ID, 11, "b")
	s.StartPartialIncident(ctx, m1.ID, 12, "c", nil)
	for _, p := range []Probe{p1, p2} {
		rules, _ := s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{{Metric: "cpu", Threshold: 90, Minutes: 1, Active: true}}, 0)
		rules[0].ProbeID = p.ID
		s.FireServerAlert(ctx, rules[0], 95, "", 13)
	}
	count := func(f IncidentFilter) int {
		t.Helper()
		list, err := s.ListIncidents(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}
	cases := []struct {
		name string
		f    IncidentFilter
		want int
	}{
		{"hepsi", IncidentFilter{}, 5},
		{"monitör", IncidentFilter{Kind: KindGroupMonitor}, 2},
		{"kısmi", IncidentFilter{Kind: KindGroupPartial}, 1},
		{"sunucu", IncidentFilter{Kind: KindGroupServer}, 2},
		{"müşteri: m1 + s2", IncidentFilter{MonitorIDs: []int64{m1.ID}, ServerIDs: []int64{p2.ID}}, 3},
		{"müşteri: yalnız m1", IncidentFilter{MonitorIDs: []int64{m1.ID}, ServerIDs: []int64{}}, 2},
		{"müşteri: yalnız s1", IncidentFilter{MonitorIDs: []int64{}, ServerIDs: []int64{p1.ID}}, 1},
		{"müşteri: hiçbiri", IncidentFilter{MonitorIDs: []int64{}, ServerIDs: []int64{}}, 0},
		{"müşteri: m1 sunucu süzgeci", IncidentFilter{MonitorIDs: []int64{m1.ID}, ServerIDs: []int64{}, Kind: KindGroupServer}, 0},
	}
	for _, c := range cases {
		if got := count(c.f); got != c.want {
			t.Errorf("%s: %d olay, %d bekleniyordu", c.name, got, c.want)
		}
	}
	if list, _ := s.IncidentsFor(ctx, []int64{m1.ID, m2.ID}, 0, 10); len(list) != 2 {
		t.Errorf("herkese açık liste yalnızca kesintileri içermeli: %+v", list)
	}
}

// Bakım kaydı idempotenttir: aynı durum ikinci kez yazılmaz, bitiş yalnızca
// kapanmamış başlangıçtan sonra yazılır.
func TestMarkIncidentMaint(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m := newMonitor(t, s, "m")
	s.OpenIncident(ctx, m.ID, 10, "x")
	id, _ := s.OpenIncidentID(ctx, m.ID)
	steps := []struct {
		start bool
		want  bool
	}{{false, false}, {true, true}, {true, false}, {false, true}, {false, false}, {true, true}}
	for i, st := range steps {
		if got, err := s.MarkIncidentMaint(ctx, id, int64(20+i), st.start); err != nil || got != st.want {
			t.Fatalf("adım %d: %v %v", i, got, err)
		}
	}
	if in, _ := s.InMaintLogged(ctx, id); !in {
		t.Fatal("son kayıt başlangıç")
	}
}

// Bulgu: durdurulan (hiç kontrol edilmemiş gibi görünen: son kontrol yok,
// durum bekliyor) monitörde eski kontrolün sertifika bilgisi kalıyordu.
func TestPauseClearsCert(t *testing.T) {
	ctx := context.Background()
	s := openTest(t)
	m := newMonitor(t, s, "m")
	if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: m.ID, Time: 100, Status: StatusUp, PingMs: 5}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateCert(ctx, m.ID, 9999999, "R3"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMonitorActive(ctx, m.ID, false); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetMonitor(ctx, m.ID)
	if got.LastCheckAt != 0 || got.Status != StatusPending || got.CertExpiresAt != 0 || got.CertIssuer != "" {
		t.Fatalf("durdurulan monitör: son kontrol %d durum %d sertifika %d %q", got.LastCheckAt, got.Status, got.CertExpiresAt, got.CertIssuer)
	}
}
