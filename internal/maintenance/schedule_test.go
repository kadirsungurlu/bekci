package maintenance

import (
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

var ist = mustLoc("Europe/Istanbul")

func mustLoc(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// at İstanbul yerel saatinde bir an.
func at(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, ist)
}

var now = at(2026, 9, 27, 12, 0)

func compile(t *testing.T, m store.Maintenance) *Schedule {
	t.Helper()
	m.Title = "Bakım"
	m.Active = true
	if m.MonitorIDs == nil {
		m.AllMonitors = true
	}
	if err := Normalize(&m, now); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	s, err := Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return s
}

type probe struct {
	t      time.Time
	active bool
}

func checkProbes(t *testing.T, s *Schedule, probes []probe) {
	t.Helper()
	for _, p := range probes {
		// Önbellekli ve önbelleksiz sonuç aynı olmalı.
		start, end, ok := s.Occurrence(p.t)
		raw := ok && !p.t.Before(start) && p.t.Before(end)
		if raw != p.active {
			t.Errorf("%s: Occurrence aktif=%v, %v bekleniyordu (%s - %s)", p.t, raw, p.active, start, end)
		}
		if got := s.ActiveAt(p.t); got != p.active {
			t.Errorf("%s: ActiveAt=%v, %v bekleniyordu", p.t, got, p.active)
		}
	}
}

func TestOnce(t *testing.T) {
	s := compile(t, store.Maintenance{Strategy: store.MaintOnce, Start: "2026-10-01T02:00", End: "2026-10-01T04:30"})
	checkProbes(t, s, []probe{
		{at(2026, 10, 1, 1, 59), false},
		{at(2026, 10, 1, 2, 0), true},
		{at(2026, 10, 1, 4, 29), true},
		{at(2026, 10, 1, 4, 30), false},
		{at(2026, 10, 2, 0, 0), false},
		{at(2026, 10, 1, 3, 0), true}, // geri giden saat de doğru (önbellek)
	})
	// İstanbul UTC+3, yaz saati yok: 02:00 yerel = 23:00 UTC önceki gün.
	start, _, _ := s.Occurrence(now)
	if want := time.Date(2026, 9, 30, 23, 0, 0, 0, time.UTC); !start.Equal(want) {
		t.Errorf("başlangıç %s, %s bekleniyordu", start.UTC(), want)
	}
}

func TestWeeklyAcrossMidnight(t *testing.T) {
	// Cuma (5) ve Pazar (0) 23:00 - 02:00.
	s := compile(t, store.Maintenance{Strategy: store.MaintWeekly, Weekdays: []int{5, 0, 5}, StartTime: "23:00", EndTime: "02:00"})
	checkProbes(t, s, []probe{
		{at(2026, 10, 1, 23, 30), false}, // Perşembe
		{at(2026, 10, 2, 22, 59), false}, // Cuma, başlamadan
		{at(2026, 10, 2, 23, 0), true},
		{at(2026, 10, 3, 0, 30), true}, // Cumartesi gece yarısından sonra: Cuma'nın penceresi
		{at(2026, 10, 3, 1, 59), true},
		{at(2026, 10, 3, 2, 0), false},
		{at(2026, 10, 3, 23, 30), false}, // Cumartesi seçili değil
		{at(2026, 10, 4, 23, 30), true},  // Pazar
		{at(2026, 10, 5, 1, 0), true},    // Pazartesi sabahı: Pazar'ın penceresi
		{at(2026, 10, 5, 3, 0), false},
	})
	start, end, ok := s.Occurrence(at(2026, 10, 5, 3, 0))
	if !ok || !start.Equal(at(2026, 10, 9, 23, 0)) || !end.Equal(at(2026, 10, 10, 2, 0)) {
		t.Errorf("sonraki tekrar yanlış: %s - %s %v", start, end, ok)
	}
}

func TestDailyWithDateRange(t *testing.T) {
	s := compile(t, store.Maintenance{Strategy: store.MaintDaily, StartTime: "22:00", EndTime: "01:00",
		DateFrom: "2026-10-01", DateTo: "2026-10-03"})
	checkProbes(t, s, []probe{
		{at(2026, 10, 1, 0, 30), false}, // 30 Eylül'ün penceresi aralık dışında
		{at(2026, 10, 1, 22, 0), true},
		{at(2026, 10, 2, 0, 59), true},
		{at(2026, 10, 2, 1, 0), false},
		{at(2026, 10, 3, 23, 0), true},
		{at(2026, 10, 4, 0, 30), true}, // son günün penceresi ertesi güne taşar
		{at(2026, 10, 4, 22, 30), false},
	})
	st, ns, ne := Status(store.Maintenance{Active: true, Strategy: store.MaintDaily, Timezone: "Europe/Istanbul",
		StartTime: "22:00", EndTime: "01:00", DateFrom: "2026-10-01", DateTo: "2026-10-03"}, now)
	if st != StatusScheduled || ns != at(2026, 10, 1, 22, 0).Unix() || ne != at(2026, 10, 2, 1, 0).Unix() {
		t.Errorf("durum yanlış: %s %d %d", st, ns, ne)
	}
	st, ns, _ = Status(store.Maintenance{Active: true, Strategy: store.MaintDaily, Timezone: "Europe/Istanbul",
		StartTime: "22:00", EndTime: "01:00", DateFrom: "2026-10-01", DateTo: "2026-10-03"}, at(2026, 10, 5, 0, 0))
	if st != StatusEnded || ns != 0 {
		t.Errorf("bitmiş olmalı: %s %d", st, ns)
	}
}

func TestDailyOtherTimezone(t *testing.T) {
	// UTC'de 00:00-01:00 = İstanbul'da 03:00-04:00.
	s := compile(t, store.Maintenance{Strategy: store.MaintDaily, Timezone: "UTC", StartTime: "00:00", EndTime: "01:00"})
	checkProbes(t, s, []probe{
		{at(2026, 10, 1, 2, 59), false},
		{at(2026, 10, 1, 3, 0), true},
		{at(2026, 10, 1, 3, 59), true},
		{at(2026, 10, 1, 4, 0), false},
	})
}

func TestCron(t *testing.T) {
	// Pazartesi 03:00, 90 dakika.
	s := compile(t, store.Maintenance{Strategy: store.MaintCron, Cron: " 0 3 * * 1 ", DurationMinutes: 90})
	checkProbes(t, s, []probe{
		{at(2026, 10, 5, 2, 59), false},
		{at(2026, 10, 5, 3, 0), true},
		{at(2026, 10, 5, 4, 29), true},
		{at(2026, 10, 5, 4, 30), false},
		{at(2026, 10, 6, 3, 30), false},
		{at(2026, 10, 12, 3, 45), true},
	})
	start, end, ok := s.Occurrence(at(2026, 10, 6, 0, 0))
	if !ok || !start.Equal(at(2026, 10, 12, 3, 0)) || !end.Equal(at(2026, 10, 12, 4, 30)) {
		t.Errorf("sonraki cron tekrarı yanlış: %s - %s", start, end)
	}

	// Süreden sık tekrarlar birleşir: 10 dakikada bir, 15 dakika.
	s = compile(t, store.Maintenance{Strategy: store.MaintCron, Cron: "*/10 * * * *", DurationMinutes: 15,
		DateFrom: "2026-10-01", DateTo: "2026-10-01"})
	checkProbes(t, s, []probe{
		{at(2026, 9, 30, 23, 59), false},
		{at(2026, 10, 1, 0, 0), true},
		{at(2026, 10, 1, 13, 7), true},
		{at(2026, 10, 1, 23, 59), true},
		{at(2026, 10, 2, 0, 4), true}, // 23:50 tekrarı 00:05'e kadar sürer
		{at(2026, 10, 2, 0, 5), false},
		{at(2026, 10, 2, 12, 0), false},
	})
	_, end, _ = s.Occurrence(at(2026, 10, 1, 0, 0))
	if !end.Equal(at(2026, 10, 2, 0, 5)) {
		t.Errorf("birleşik pencerenin sonu yanlış: %s", end)
	}
}

func TestManualAndStatus(t *testing.T) {
	m := store.Maintenance{Title: "Elle", Active: true, Strategy: store.MaintManual, AllMonitors: true}
	if err := Normalize(&m, now); err != nil {
		t.Fatal(err)
	}
	if m.Timezone != DefaultTimezone {
		t.Errorf("varsayılan saat dilimi atanmalı: %q", m.Timezone)
	}
	if st, _, _ := Status(m, now); st != StatusActive {
		t.Errorf("elle açık pencere aktif olmalı: %s", st)
	}
	m.Active = false
	if st, _, _ := Status(m, now); st != StatusInactive {
		t.Errorf("kapalı pencere inactive olmalı: %s", st)
	}
	once := store.Maintenance{Active: true, Strategy: store.MaintOnce, Timezone: "Europe/Istanbul", Start: "2026-09-27T11:00", End: "2026-09-27T13:00"}
	if st, ns, ne := Status(once, now); st != StatusActive || ns != at(2026, 9, 27, 11, 0).Unix() || ne != at(2026, 9, 27, 13, 0).Unix() {
		t.Errorf("tek seferlik aktif olmalı: %s %d %d", st, ns, ne)
	}
	if st, _, _ := Status(once, at(2026, 9, 27, 13, 0)); st != StatusEnded {
		t.Errorf("tek seferlik bitmiş olmalı: %s", st)
	}
}

func TestNormalize(t *testing.T) {
	base := func() store.Maintenance {
		return store.Maintenance{Title: "Bakım", Strategy: store.MaintDaily, StartTime: "02:00", EndTime: "03:00", AllMonitors: true}
	}
	cases := []struct {
		name string
		mut  func(*store.Maintenance)
		err  string
	}{
		{"başlık yok", func(m *store.Maintenance) { m.Title = "  " }, "Başlık"},
		{"uzun başlık", func(m *store.Maintenance) { m.Title = strings.Repeat("a", 201) }, "Başlık"},
		{"uzun açıklama", func(m *store.Maintenance) { m.Description = strings.Repeat("ç", 2001) }, "Açıklama"},
		{"strateji", func(m *store.Maintenance) { m.Strategy = "yearly" }, "strateji"},
		{"saat dilimi", func(m *store.Maintenance) { m.Timezone = "Mars/Olympus" }, "saat dilimi"},
		{"Local", func(m *store.Maintenance) { m.Timezone = "Local" }, "saat dilimi"},
		{"saat biçimi", func(m *store.Maintenance) { m.StartTime = "25:00" }, "Başlangıç saati"},
		{"aynı saat", func(m *store.Maintenance) { m.EndTime = "02:00" }, "aynı"},
		{"tarih biçimi", func(m *store.Maintenance) { m.DateFrom = "01.10.2026" }, "Başlangıç tarihi"},
		{"ters tarih", func(m *store.Maintenance) { m.DateFrom, m.DateTo = "2026-10-05", "2026-10-01" }, "önce olamaz"},
		{"monitör yok", func(m *store.Maintenance) { m.AllMonitors = false }, "En az bir monitör"},
		{"haftalık gün yok", func(m *store.Maintenance) { m.Strategy = store.MaintWeekly }, "gün"},
		{"haftalık gün 7", func(m *store.Maintenance) { m.Strategy, m.Weekdays = store.MaintWeekly, []int{7} }, "0 (Pazar)"},
		{"tek seferlik ters", func(m *store.Maintenance) {
			m.Strategy, m.Start, m.End = store.MaintOnce, "2026-10-01T05:00", "2026-10-01T04:00"
		}, "sonra"},
		{"tek seferlik boş", func(m *store.Maintenance) { m.Strategy = store.MaintOnce }, "Başlangıç zamanı"},
		{"cron bozuk", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "61 * * * *", 10 },
			"Cron ifadesinin dakika alanı geçersiz: 61 (izin verilen: 0-59)"},
		{"cron bozuk ay", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "0 3 * FOO *", 10 },
			"ay alanı geçersiz: FOO"},
		{"cron 6 alan", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "0 0 3 * * 1", 10 },
			"5 alandan oluşmalı (dakika saat ayın-günü ay haftanın-günü); 6 alan girildi"},
		{"cron 2 alan", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "bozuk cron", 10 }, "2 alan girildi"},
		{"cron kısaltma", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "@sometimes", 10 }, "@daily"},
		{"cron TZ", func(m *store.Maintenance) {
			m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "CRON_TZ=UTC 0 3 * * *", 10
		}, "saat dilimi"},
		{"cron hiç", func(m *store.Maintenance) { m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "0 0 30 2 *", 10 }, "hiçbir zaman"},
		{"cron süre", func(m *store.Maintenance) { m.Strategy, m.Cron = store.MaintCron, "0 3 * * *" }, "Süre"},
		{"cron uzun süre", func(m *store.Maintenance) {
			m.Strategy, m.Cron, m.DurationMinutes = store.MaintCron, "0 3 * * *", 10081
		}, "Süre"},
	}
	for _, c := range cases {
		m := base()
		c.mut(&m)
		err := Normalize(&m, now)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: hata %v, %q içermeliydi", c.name, err, c.err)
		}
		if _, ok := err.(ValidationError); err != nil && !ok {
			t.Errorf("%s: ValidationError olmalı: %T", c.name, err)
		}
	}

	// İlgisiz alanlar temizlenir, tekrarlar ayıklanır, saniyeli zaman kabul edilir.
	m := store.Maintenance{Title: " Gece ", Strategy: store.MaintOnce, Start: "2026-10-01T02:00:30", End: "2026-10-01T03:00",
		Weekdays: []int{1}, StartTime: "01:00", Cron: "* * * * *", DurationMinutes: 5, DateFrom: "2026-10-01",
		MonitorIDs: []int64{3, 1, 3}}
	if err := Normalize(&m, now); err != nil {
		t.Fatal(err)
	}
	if m.Title != "Gece" || m.Start != "2026-10-01T02:00" || len(m.Weekdays) != 0 || m.StartTime != "" || m.Cron != "" ||
		m.DurationMinutes != 0 || m.DateFrom != "" || len(m.MonitorIDs) != 2 {
		t.Errorf("normalize yanlış: %+v", m)
	}
	m = store.Maintenance{Title: "x", Strategy: store.MaintWeekly, Weekdays: []int{6, 1, 6}, StartTime: "1:05", EndTime: "02:00",
		AllMonitors: true, MonitorIDs: []int64{5}}
	if err := Normalize(&m, now); err != nil || m.StartTime != "01:05" || len(m.Weekdays) != 2 || m.Weekdays[0] != 1 || len(m.MonitorIDs) != 0 {
		t.Errorf("haftalık normalize yanlış: %v %+v", err, m)
	}
}

func TestIndex(t *testing.T) {
	windows := []store.Maintenance{
		{ID: 1, Active: true, Strategy: store.MaintManual, Timezone: "UTC", MonitorIDs: []int64{10}},
		{ID: 2, Active: false, Strategy: store.MaintManual, Timezone: "UTC", MonitorIDs: []int64{20}},
		{ID: 3, Active: true, Strategy: store.MaintOnce, Timezone: "Europe/Istanbul", Start: "2026-10-01T02:00", End: "2026-10-01T03:00", AllMonitors: true},
		{ID: 4, Active: true, Strategy: "bozuk", Timezone: "UTC", MonitorIDs: []int64{30}},
	}
	var bad []int64
	ix := NewIndex(windows, func(w store.Maintenance, err error) { bad = append(bad, w.ID) })
	if len(bad) != 1 || bad[0] != 4 {
		t.Errorf("derlenemeyen pencere bildirilmeli: %v", bad)
	}
	if !ix.InMaintenance(10, now) || ix.InMaintenance(20, now) || ix.InMaintenance(30, now) {
		t.Error("elle pencere dizini yanlış")
	}
	if !ix.InMaintenance(20, at(2026, 10, 1, 2, 30)) || ix.InMaintenance(20, at(2026, 10, 1, 3, 0)) {
		t.Error("tüm monitörleri etkileyen pencere yanlış")
	}
	var nilIx *Index
	if nilIx.InMaintenance(1, now) {
		t.Error("boş dizin bakım demez")
	}
}
