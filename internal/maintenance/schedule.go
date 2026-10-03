// Package maintenance bakım pencerelerini doğrular ve zamanlamasını hesaplar.
//
// Motor her kontrolde "bu monitör şu an bakımda mı?" diye sorar; bu yüzden
// pencereler bir kez derlenir (Compile) ve bellekte bir dizinde (Index)
// tutulur. Veritabanına kontrol başına sorgu gitmez; pencere eklenince,
// düzenlenince veya silinince dizin yeniden kurulur.
package maintenance

import (
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"time"
	_ "time/tzdata" // imajda/test ortamında tzdata olmasa da saat dilimleri çalışsın
	"unicode/utf8"

	"github.com/robfig/cron/v3"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// DefaultTimezone saat dilimi boş bırakılırsa kullanılır.
const DefaultTimezone = "Europe/Istanbul"

// Pencere durumları (API'de "status").
const (
	StatusActive    = "active"    // şu an bakımda
	StatusScheduled = "scheduled" // ileride başlayacak
	StatusEnded     = "ended"     // bir daha başlamayacak
	StatusInactive  = "inactive"  // elle kapatılmış
)

const (
	dateTimeLayout = "2006-01-02T15:04"
	dateLayout     = "2006-01-02"
	timeLayout     = "15:04"

	maxCronMinutes = 7 * 24 * 60 // cron süresi en fazla bir hafta
	maxMonitors    = 10000
)

// ValidationError kullanıcı girdisindeki hatadır (API'de 400).
type ValidationError string

func (e ValidationError) Error() string { return string(e) }

func invalid(format string, args ...any) error { return ValidationError(fmt.Sprintf(format, args...)) }

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)

// Cron alanlarının adları ve izin verilen değerleri (hata mesajı için).
var (
	cronFieldNames  = [5]string{"dakika", "saat", "ayın günü", "ay", "haftanın günü"}
	cronFieldRanges = [5]string{"0-59", "0-23", "1-31", "1-12 veya JAN-DEC", "0-6 veya SUN-SAT"}
)

// cronProblem geçersiz cron ifadesinin kullanıcıya gösterilecek açıklaması.
// Kütüphanenin İngilizce hatası (ör. "expected exactly 5 fields, found 2")
// gösterilmez; alan sayısı yanlışsa beklenen biçim, değilse hatalı alan ve
// izin verilen değerler söylenir. Metinler i18n hata kataloğunda çevrilir.
func cronProblem(expr string) string {
	if strings.HasPrefix(expr, "@") {
		return "Bilinmeyen cron kısaltması: @yearly, @monthly, @weekly, @daily veya @hourly kullanılabilir"
	}
	f := strings.Fields(expr)
	if len(f) != 5 {
		return fmt.Sprintf("Cron ifadesi 5 alandan oluşmalı (dakika saat ayın-günü ay haftanın-günü); %d alan girildi", len(f))
	}
	for i := range f {
		probe := []string{"*", "*", "*", "*", "*"}
		probe[i] = f[i]
		if _, err := cronParser.Parse(strings.Join(probe, " ")); err != nil {
			return fmt.Sprintf("Cron ifadesinin %s alanı geçersiz: %s (izin verilen: %s)", cronFieldNames[i], f[i], cronFieldRanges[i])
		}
	}
	return "Cron ifadesi geçersiz"
}

// Normalize pencereyi doğrular, varsayılanları doldurur ve stratejiyle ilgisiz
// alanları temizler (arayüz formun tüm alanlarını gönderebilsin diye).
// Monitörlerin var olup olmadığını çağıran (API) kontrol eder.
func Normalize(m *store.Maintenance, now time.Time) error {
	m.Title = strings.TrimSpace(m.Title)
	m.Description = strings.TrimSpace(m.Description)
	if m.Title == "" || utf8.RuneCountInString(m.Title) > 200 {
		return invalid("Başlık 1-200 karakter olmalı")
	}
	if utf8.RuneCountInString(m.Description) > 2000 {
		return invalid("Açıklama en fazla 2000 karakter olabilir")
	}
	m.Timezone = strings.TrimSpace(m.Timezone)
	if m.Timezone == "" {
		m.Timezone = DefaultTimezone
	}
	loc, err := loadLocation(m.Timezone)
	if err != nil {
		return err
	}

	keep := struct{ once, times, weekdays, dates, cron bool }{}
	switch m.Strategy {
	case store.MaintManual:
	case store.MaintOnce:
		keep.once = true
	case store.MaintWeekly:
		keep.times, keep.weekdays, keep.dates = true, true, true
	case store.MaintDaily:
		keep.times, keep.dates = true, true
	case store.MaintCron:
		keep.cron, keep.dates = true, true
	default:
		return invalid("Geçersiz strateji: manual, once, recurring_weekly, recurring_daily veya cron olmalı")
	}
	if !keep.once {
		m.Start, m.End = "", ""
	}
	if !keep.times {
		m.StartTime, m.EndTime = "", ""
	}
	if !keep.weekdays {
		m.Weekdays = []int{}
	}
	if !keep.dates {
		m.DateFrom, m.DateTo = "", ""
	}
	if !keep.cron {
		m.Cron, m.DurationMinutes = "", 0
	}

	if keep.once {
		start, err := parseLocal(dateTimeLayout, m.Start, loc)
		if err != nil {
			return invalid("Başlangıç zamanı geçersiz (YYYY-AA-GGTSS:DD olmalı)")
		}
		end, err := parseLocal(dateTimeLayout, m.End, loc)
		if err != nil {
			return invalid("Bitiş zamanı geçersiz (YYYY-AA-GGTSS:DD olmalı)")
		}
		if !end.After(start) {
			return invalid("Bitiş zamanı başlangıçtan sonra olmalı")
		}
		m.Start, m.End = start.Format(dateTimeLayout), end.Format(dateTimeLayout)
	}
	if keep.times {
		st, err := time.Parse(timeLayout, m.StartTime)
		if err != nil {
			return invalid("Başlangıç saati geçersiz (SS:DD olmalı)")
		}
		et, err := time.Parse(timeLayout, m.EndTime)
		if err != nil {
			return invalid("Bitiş saati geçersiz (SS:DD olmalı)")
		}
		if st.Equal(et) {
			return invalid("Başlangıç ve bitiş saati aynı olamaz")
		}
		m.StartTime, m.EndTime = st.Format(timeLayout), et.Format(timeLayout)
	}
	if keep.weekdays {
		days := make([]int, 0, 7)
		for _, d := range m.Weekdays {
			if d < 0 || d > 6 {
				return invalid("Haftanın günleri 0 (Pazar) ile 6 (Cumartesi) arasında olmalı")
			}
			if !slices.Contains(days, d) {
				days = append(days, d)
			}
		}
		if len(days) == 0 {
			return invalid("En az bir gün seçin")
		}
		slices.Sort(days)
		m.Weekdays = days
	}
	if m.Weekdays == nil {
		m.Weekdays = []int{}
	}
	if keep.dates {
		var from, to time.Time
		if m.DateFrom = strings.TrimSpace(m.DateFrom); m.DateFrom != "" {
			if from, err = time.Parse(dateLayout, m.DateFrom); err != nil {
				return invalid("Başlangıç tarihi geçersiz (YYYY-AA-GG olmalı)")
			}
		}
		if m.DateTo = strings.TrimSpace(m.DateTo); m.DateTo != "" {
			if to, err = time.Parse(dateLayout, m.DateTo); err != nil {
				return invalid("Bitiş tarihi geçersiz (YYYY-AA-GG olmalı)")
			}
		}
		if m.DateFrom != "" && m.DateTo != "" && to.Before(from) {
			return invalid("Bitiş tarihi başlangıç tarihinden önce olamaz")
		}
	}
	if keep.cron {
		m.Cron = strings.Join(strings.Fields(m.Cron), " ")
		if m.Cron == "" || len(m.Cron) > 100 {
			return invalid("Cron ifadesi 1-100 karakter olmalı")
		}
		if strings.Contains(strings.ToUpper(m.Cron), "TZ=") {
			return invalid("Cron ifadesinde saat dilimi belirtilmez; pencerenin saat dilimi kullanılır")
		}
		sched, err := cronParser.Parse(m.Cron)
		if err != nil {
			return ValidationError(cronProblem(m.Cron))
		}
		if sched.Next(now.In(loc)).IsZero() {
			return invalid("Cron ifadesi hiçbir zaman çalışmıyor")
		}
		if m.DurationMinutes < 1 || m.DurationMinutes > maxCronMinutes {
			return invalid("Süre 1 dakika ile 7 gün (10080 dakika) arasında olmalı")
		}
	}

	uniq := func(in []int64) []int64 {
		out := make([]int64, 0, len(in))
		seen := map[int64]bool{}
		for _, id := range in {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		return out
	}
	if m.AllMonitors {
		m.MonitorIDs = []int64{}
	} else {
		m.MonitorIDs = uniq(m.MonitorIDs)
		if len(m.MonitorIDs) > maxMonitors {
			return invalid("En fazla %d monitör seçilebilir", maxMonitors)
		}
	}
	// Sunucular (migration 26): pencere monitörleri, sunucuları ya da ikisini kapsar.
	if m.AllServers {
		m.ServerIDs = []int64{}
	} else {
		m.ServerIDs = uniq(m.ServerIDs)
		if len(m.ServerIDs) > maxMonitors {
			return invalid("En fazla %d sunucu seçilebilir", maxMonitors)
		}
	}
	if !m.AllMonitors && len(m.MonitorIDs) == 0 && !m.AllServers && len(m.ServerIDs) == 0 {
		return invalid("En az bir monitör ya da sunucu seçin veya tüm monitörleri seçin")
	}
	return nil
}

func loadLocation(name string) (*time.Location, error) {
	if name == "Local" {
		return nil, invalid("Geçersiz saat dilimi: %s", name)
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, invalid("Geçersiz saat dilimi: %s", name)
	}
	return loc, nil
}

// parseLocal "2006-01-02T15:04" (saniyeli biçim de kabul edilir) değerini loc'ta yorumlar.
func parseLocal(layout, v string, loc *time.Location) (time.Time, error) {
	v = strings.TrimSpace(v)
	t, err := time.ParseInLocation(layout, v, loc)
	if err != nil && layout == dateTimeLayout {
		t, err = time.ParseInLocation(layout+":05", v, loc)
		t = t.Truncate(time.Minute)
	}
	return t, err
}

// Schedule derlenmiş bir pencere. Eşzamanlı kullanıma uygundur.
type Schedule struct {
	ID       int64
	manual   bool
	loc      *time.Location
	strategy string

	start, end   time.Time // once
	weekdays     [7]bool   // weekly
	startMinutes int       // günlük başlangıç (dakika)
	endMinutes   int       // günlük bitiş (dakika); <= başlangıç ise ertesi gün
	dateFrom     time.Time // tekrarlayan: ilk gün (yerel gece yarısı); sıfırsa sınır yok
	dateTo       time.Time // tekrarlayan: son günün ertesi (yerel gece yarısı); sıfırsa sınır yok
	cron         cron.Schedule
	duration     time.Duration
	endedAt      time.Time // "şimdi bitir": bu anda süren tekrar, bu andan itibaren bitmiş sayılır

	cache atomic.Pointer[span]
}

// span bir Occurrence sonucunun önbelleği: from anından itibaren, end'e kadar
// (ok=false ise sonsuza kadar) geçerlidir.
type span struct {
	from, start, end time.Time
	ok               bool
}

// Compile normalize edilmiş pencereyi derler.
func Compile(m store.Maintenance) (*Schedule, error) {
	loc, err := loadLocation(m.Timezone)
	if err != nil {
		return nil, err
	}
	s := &Schedule{ID: m.ID, loc: loc, strategy: m.Strategy}
	if m.EndedAt > 0 {
		s.endedAt = time.Unix(m.EndedAt, 0)
	}
	switch m.Strategy {
	case store.MaintManual:
		s.manual = true
	case store.MaintOnce:
		if s.start, err = parseLocal(dateTimeLayout, m.Start, loc); err != nil {
			return nil, err
		}
		if s.end, err = parseLocal(dateTimeLayout, m.End, loc); err != nil {
			return nil, err
		}
	case store.MaintWeekly, store.MaintDaily:
		st, err := time.Parse(timeLayout, m.StartTime)
		if err != nil {
			return nil, err
		}
		et, err := time.Parse(timeLayout, m.EndTime)
		if err != nil {
			return nil, err
		}
		s.startMinutes = st.Hour()*60 + st.Minute()
		s.endMinutes = et.Hour()*60 + et.Minute()
		for d := range s.weekdays {
			s.weekdays[d] = m.Strategy == store.MaintDaily || slices.Contains(m.Weekdays, d)
		}
	case store.MaintCron:
		if s.cron, err = cronParser.Parse(m.Cron); err != nil {
			return nil, err
		}
		s.duration = time.Duration(m.DurationMinutes) * time.Minute
		if s.duration <= 0 {
			return nil, invalid("Süre geçersiz")
		}
	default:
		return nil, invalid("Geçersiz strateji")
	}
	if m.Strategy == store.MaintWeekly || m.Strategy == store.MaintDaily || m.Strategy == store.MaintCron {
		if m.DateFrom != "" {
			if s.dateFrom, err = time.ParseInLocation(dateLayout, m.DateFrom, loc); err != nil {
				return nil, err
			}
		}
		if m.DateTo != "" {
			to, err := time.ParseInLocation(dateLayout, m.DateTo, loc)
			if err != nil {
				return nil, err
			}
			s.dateTo = addDays(to, 1, loc)
		}
	}
	return s, nil
}

// addDays yerel takvimde n gün ekler (yaz saati geçişlerinde de gece yarısında kalır).
func addDays(t time.Time, n int, loc *time.Location) time.Time {
	y, mo, d := t.In(loc).Date()
	return time.Date(y, mo, d+n, 0, 0, 0, 0, loc)
}

// inDates bir tekrarın başlangıcı tarih aralığında mı?
func (s *Schedule) inDates(start time.Time) bool {
	if !s.dateFrom.IsZero() && start.Before(s.dateFrom) {
		return false
	}
	return s.dateTo.IsZero() || start.Before(s.dateTo)
}

// Occurrence t anında süren tekrarı, yoksa t'den sonraki ilk tekrarı döner
// (başlangıç, bitiş; bitiş hariç). Bir daha tekrar yoksa ok=false.
// Elle (manual) pencerelerde anlamsızdır: ok=false döner.
func (s *Schedule) Occurrence(t time.Time) (start, end time.Time, ok bool) {
	switch s.strategy {
	case store.MaintOnce:
		if t.Before(s.end) {
			return s.start, s.end, true
		}
	case store.MaintWeekly, store.MaintDaily:
		return s.dailyOccurrence(t)
	case store.MaintCron:
		return s.cronOccurrence(t)
	}
	return time.Time{}, time.Time{}, false
}

func (s *Schedule) dailyOccurrence(t time.Time) (time.Time, time.Time, bool) {
	// Gece yarısını aşan pencere dünden başlamış olabilir: bir gün önceden başla.
	day := addDays(t, -1, s.loc)
	if !s.dateFrom.IsZero() && day.Before(s.dateFrom) {
		day = s.dateFrom
	}
	for range 9 { // haftalıkta en fazla 7 gün sonra bir eşleşme vardır
		if !s.dateTo.IsZero() && !day.Before(s.dateTo) {
			break
		}
		if s.weekdays[day.Weekday()] {
			y, mo, d := day.Date()
			start := time.Date(y, mo, d, s.startMinutes/60, s.startMinutes%60, 0, 0, s.loc)
			endDay := d
			if s.endMinutes <= s.startMinutes {
				endDay++ // gece yarısını aşıyor: ertesi gün biter
			}
			end := time.Date(y, mo, endDay, s.endMinutes/60, s.endMinutes%60, 0, 0, s.loc)
			if t.Before(end) && end.After(start) {
				return start, end, true
			}
		}
		day = addDays(day, 1, s.loc)
	}
	return time.Time{}, time.Time{}, false
}

// maxCronMerge üst üste binen cron tekrarlarını birleştirirken en fazla bakılacak tekrar.
const maxCronMerge = 1000

func (s *Schedule) cronOccurrence(t time.Time) (time.Time, time.Time, bool) {
	// t'yi kapsayabilecek ilk tekrar (t - süre, t] aralığında başlar; cron.Next
	// verilen andan kesin sonrasını döner.
	from := t.In(s.loc).Add(-s.duration)
	if !s.dateFrom.IsZero() && from.Before(s.dateFrom) {
		from = s.dateFrom.Add(-time.Second)
	}
	start := s.cron.Next(from)
	if start.IsZero() || !s.inDates(start) {
		return time.Time{}, time.Time{}, false
	}
	end := start.Add(s.duration)
	// Tekrarlar süreden sık ise pencereler birleşir: kesintisiz bakım süresi.
	next := start
	for range maxCronMerge {
		next = s.cron.Next(next)
		if next.IsZero() || next.After(end) || !s.inDates(next) {
			break
		}
		end = next.Add(s.duration)
	}
	return start, end, true
}

// ActiveAt pencere t anında bakımda mı? (Pencerenin "aktif" bayrağı çağıran
// tarafından kontrol edilir; dizinde yalnızca aktif pencereler bulunur.)
// Sonuç önbelleğe alınır: sıradan bir çağrı hesaplama yapmaz.
func (s *Schedule) ActiveAt(t time.Time) bool {
	if s.manual {
		return true
	}
	c := s.cache.Load()
	if c == nil || t.Before(c.from) || (c.ok && !t.Before(c.end)) {
		start, end, ok := s.Occurrence(t)
		c = &span{from: t, start: start, end: end, ok: ok}
		s.cache.Store(c)
	}
	return c.ok && !t.Before(c.start) && !s.endedEarly(t, c.start)
}

// endedEarly start'ta başlayan tekrar "şimdi bitir" ile bitirilmiş ve t o
// andan sonra mı: tekrar endedAt'ten önce (ya da o anda) başlamış ve t ≥ endedAt.
func (s *Schedule) endedEarly(t, start time.Time) bool {
	return !s.endedAt.IsZero() && !t.Before(s.endedAt) && !start.After(s.endedAt)
}

// Status pencerenin t anındaki durumu ve şu anki/sonraki tekrarın sınırları
// (unix saniye; yoksa 0). Elle pencerede sınır yoktur.
func Status(m store.Maintenance, t time.Time) (status string, nextStart, nextEnd int64) {
	if !m.Active {
		status = StatusInactive
	}
	s, err := Compile(m)
	if err != nil {
		if status == "" {
			status = StatusEnded
		}
		return status, 0, 0
	}
	if s.manual {
		if status == "" {
			status = StatusActive
		}
		return status, 0, 0
	}
	start, end, ok := s.Occurrence(t)
	if ok && !t.Before(start) && s.endedEarly(t, start) {
		// Süren tekrar erken bitirildi: sıradaki tekrar (varsa) gösterilir.
		start, end, ok = s.Occurrence(end)
	}
	if ok {
		nextStart, nextEnd = start.Unix(), end.Unix()
	}
	if status == "" {
		switch {
		case !ok:
			status = StatusEnded
		case !t.Before(start):
			status = StatusActive
		default:
			status = StatusScheduled
		}
	}
	return status, nextStart, nextEnd
}
