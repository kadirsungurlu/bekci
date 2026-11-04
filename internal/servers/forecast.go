package servers

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"time"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Disk dolma tahmini (iş 660): saatlik doluluk noktalarına en küçük kareler
// doğrusu. Tüm veritabanı kiplerinde aynı Go kodu çalışır (saf, sınanabilir);
// kaynak TimescaleDB'de server_disk_1h, diğerlerinde server_stats saatlik satırı.
const (
	ForecastGrowing  = "growing"  // doluyor; Days dolu
	ForecastFlat     = "flat"     // anlamlı büyüme yok
	ForecastLearning = "learning" // yeterli geçmiş yok

	forecastWindow    = 7 * 86400
	forecastLongWin   = 30 * 86400
	forecastMinSpan   = 12 * 3600
	forecastMinPoints = 6
	forecastMaxDays   = 3650
	// Günde toplamın on binde birinden yavaş büyüme "düz" sayılır.
	forecastMinGrowth = 0.0001
	// Bu kadar düşüş (temizlik) ya da boyut değişimi eğimi böler.
	forecastBreak = 0.01

	forecastRefresh = 600 // sn; bölüm %1+ oynarsa beklemez
	// Güven: uyarı için doğrunun noktaları en az bu kadar açıklaması gerekir.
	forecastMinR2 = 0.5
)

// DiskPoint bir bölümün bir andaki doluluğu.
type DiskPoint struct {
	T           int64
	Used, Total uint64
}

// DiskForecast bir bölümün tahmini.
type DiskForecast struct {
	Mount    string   `json:"mount"`
	Status   string   `json:"status"`
	Days     *float64 `json:"days"`       // growing: dolmasına kalan gün
	PerDay   float64  `json:"per_day"`    // bayt/gün (7 gün)
	PerDay30 float64  `json:"per_day_30"` // bayt/gün (30 gün; yetersizse 0)
	Used     uint64   `json:"used"`
	Total    uint64   `json:"total"`
	R2       float64  `json:"r2"`
}

// regress en küçük kareler eğimi (bayt/gün) ve belirlilik katsayısı.
func regress(pts []DiskPoint) (slope, r2 float64) {
	var sx, sy, sxx, sxy, syy float64
	n := float64(len(pts))
	t0 := pts[0].T
	for _, p := range pts {
		x, y := float64(p.T-t0)/86400, float64(p.Used)
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
		syy += y * y
	}
	dx := n*sxx - sx*sx
	dy := n*syy - sy*sy
	if dx <= 0 {
		return 0, 0
	}
	num := n*sxy - sx*sy
	slope = num / dx
	if dy > 0 {
		r2 = num * num / (dx * dy)
	}
	return slope, r2
}

// segment son düz parça: boyut değişimi ya da temizlikten sonrası.
func segment(pts []DiskPoint) []DiskPoint {
	last := pts[len(pts)-1]
	limit := float64(last.Total) * forecastBreak
	for i := len(pts) - 2; i >= 0; i-- {
		a, b := pts[i], pts[i+1]
		if math.Abs(float64(a.Total)-float64(last.Total)) > limit || float64(a.Used)-float64(b.Used) > limit {
			return pts[i+1:]
		}
	}
	return pts
}

// Forecast zamana göre sıralı noktalardan tahmin üretir.
func Forecast(mount string, pts []DiskPoint, now int64) DiskForecast {
	out := DiskForecast{Mount: mount, Status: ForecastLearning}
	if len(pts) == 0 {
		return out
	}
	last := pts[len(pts)-1]
	out.Used, out.Total = last.Used, last.Total
	seg := segment(pts)
	week := seg
	if i := sort.Search(len(seg), func(i int) bool { return seg[i].T >= now-forecastWindow }); i > 0 {
		week = seg[i:]
	}
	if len(week) < forecastMinPoints || last.T-week[0].T < forecastMinSpan || last.Total == 0 {
		return out
	}
	out.PerDay, out.R2 = regress(week)
	out.PerDay, out.R2 = math.Round(out.PerDay), math.Round(out.R2*1000)/1000
	if len(seg) > len(week) && last.T-seg[0].T > forecastWindow {
		slope, _ := regress(seg)
		out.PerDay30 = math.Round(slope)
	}
	if out.PerDay <= float64(last.Total)*forecastMinGrowth {
		out.Status = ForecastFlat
		return out
	}
	free := float64(last.Total) - float64(last.Used)
	days := max(free, 0) / out.PerDay
	if days > forecastMaxDays {
		out.Status = ForecastFlat
		return out
	}
	days = math.Round(days*10) / 10
	out.Status, out.Days = ForecastGrowing, &days
	return out
}

type forecastEntry struct {
	at   int64
	pct  map[string]float64
	list []DiskForecast
}

// diskHistory bölüm başına son 30 günün saatlik noktaları. Sütunlu kaynak
// (dönüşümden sonrası) eski dönemi server_stats'tan tamamlar.
func (s *Service) diskHistory(ctx context.Context, id, now int64) (map[string][]DiskPoint, error) {
	from := now - forecastLongWin
	hours, err := s.store.ServerDiskHourly(ctx, id, from)
	if err != nil {
		return nil, err
	}
	out := map[string][]DiskPoint{}
	oldest := now + 1
	for _, d := range hours {
		oldest = min(oldest, d.T)
	}
	rows, err := s.store.ServerStats(ctx, id, store.ServerRes60, from, oldest)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		var st metrics.Stats
		if json.Unmarshal(row.Data, &st) != nil {
			continue
		}
		for _, d := range st.Disks {
			out[d.Mount] = append(out[d.Mount], DiskPoint{T: row.Time, Used: d.Used, Total: d.Total})
		}
	}
	for _, d := range hours {
		out[d.Mount] = append(out[d.Mount], DiskPoint{T: d.T, Used: d.Used, Total: d.Total})
	}
	return out, nil
}

// updateForecasts önbelleği gerekirse yeniler ve güncel listeyi döner.
// evalMu altında çağrılır.
func (s *Service) updateForecasts(ctx context.Context, id int64, st *metrics.Stats, now time.Time) []DiskForecast {
	t := now.Unix()
	pct := make(map[string]float64, len(st.Disks))
	for _, d := range st.Disks {
		if d.Total > 0 {
			pct[d.Mount] = 100 * float64(d.Used) / float64(d.Total)
		}
	}
	s.mu.RLock()
	old := s.forecasts[id]
	s.mu.RUnlock()
	if old != nil && t-old.at < forecastRefresh && !pctMoved(old.pct, pct) {
		return old.list
	}
	hist, err := s.diskHistory(ctx, id, t)
	if err != nil {
		s.log.Error("disk geçmişi okunamadı", "sunucu", id, "hata", err)
		if old != nil {
			return old.list
		}
		return nil
	}
	list := make([]DiskForecast, 0, len(st.Disks))
	for _, d := range st.Disks {
		// Son nokta her zaman güncel değer: saatlik özet saatin başında kalır.
		list = append(list, Forecast(d.Mount, append(hist[d.Mount], DiskPoint{T: t, Used: d.Used, Total: d.Total}), t))
	}
	s.mu.Lock()
	s.forecasts[id] = &forecastEntry{at: t, pct: pct, list: list}
	s.mu.Unlock()
	return list
}

func pctMoved(a, b map[string]float64) bool {
	if len(a) != len(b) {
		return true
	}
	for k, v := range b {
		if o, ok := a[k]; !ok || math.Abs(o-v) >= 1 {
			return true
		}
	}
	return false
}

// DiskForecasts son hesaplanan tahminler.
func (s *Service) DiskForecasts(id int64) []DiskForecast {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e := s.forecasts[id]; e != nil {
		return e.list
	}
	return nil
}

// soonest en erken dolacak bölüm; onlyConfident ise doğrusu güvenilir olanlardan.
func soonest(list []DiskForecast, onlyConfident bool) (DiskForecast, bool) {
	var best DiskForecast
	found := false
	for _, f := range list {
		if f.Status != ForecastGrowing || f.Days == nil || (onlyConfident && f.R2 < forecastMinR2) {
			continue
		}
		if !found || *f.Days < *best.Days {
			best, found = f, true
		}
	}
	return best, found
}

// fastRise pencerede en hızlı dolan bölüm: şimdiki doluluk eksi penceredeki en
// düşük (puan). Geçmişte yeterli örnek yoksa ok=false.
func fastRise(h []point, minutes int, now int64) (mount string, rise float64, ok bool) {
	from := now - int64(minutes)*60
	need := max(2, min(5, minutes))
	lo, cur, n := map[string]float64{}, map[string]float64{}, map[string]int{}
	for _, p := range h {
		if p.t <= from || p.t > now {
			continue
		}
		for m, v := range p.disks {
			if c, seen := lo[m]; !seen || v < c {
				lo[m] = v
			}
			cur[m] = v
			n[m]++
		}
	}
	for m, c := range cur {
		if r := c - lo[m]; n[m] >= need && (!ok || r > rise || (r == rise && m < mount)) {
			mount, rise, ok = m, r, true
		}
	}
	return mount, rise, ok
}

// trendRule kuralın olaya ve bildirime giden kopyası: seviye hep "uyarı".
func trendCopy(a *store.ServerAlert) *store.ServerAlert {
	b := *a
	b.Level, b.WarnThreshold = store.LevelWarning, a.Threshold
	return &b
}

// evaluateDiskTrend disk_forecast ve disk_fast kuralları. Kural bir bölüme
// bağlı değildir: en kötü bölüm değerlendirilir, olay o bölümle açılır.
func (s *Service) evaluateDiskTrend(ctx context.Context, p store.Probe, host *metrics.Host, a *store.ServerAlert, h []point, list []DiskForecast, t int64, now time.Time) {
	var v float64
	var mount string
	var over, clear bool
	if a.Metric == MetricDiskForecast {
		if len(list) == 0 {
			return
		}
		if f, ok := soonest(list, true); ok && *f.Days <= a.Threshold {
			mount, v, over = f.Mount, *f.Days, true
		}
		if f, ok := soonest(list, false); !ok {
			clear = true
		} else {
			if !over {
				v = *f.Days
			}
			clear = *f.Days > a.Threshold*1.5
		}
	} else {
		m, rise, ok := fastRise(h, a.Minutes, t)
		if !ok {
			return
		}
		mount, v, over, clear = m, rise, rise >= a.Threshold, rise < a.Threshold*0.6
	}
	switch {
	case !a.Firing && over:
		b := trendCopy(a)
		s.fire(ctx, p, host, b, v, now, nil, mount)
		a.Firing, a.FiredAt, a.Level = b.Firing, b.FiredAt, b.Level
	case a.Firing && clear:
		b := trendCopy(a)
		s.resolve(ctx, p, host, b, v, now, nil)
		a.Firing, a.FiredAt, a.Level = b.Firing, b.FiredAt, b.Level
	case a.Firing:
		if err := s.store.UpdateServerIncidentValue(ctx, a.ID, v); err != nil {
			s.log.Error("sunucu olayının değeri yazılamadı", "sunucu", p.Name, "hata", err)
		}
	}
}
