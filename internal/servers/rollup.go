package servers

import (
	"context"
	"encoding/json"
	"math"
	"sort"

	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Aggregate örnekleri tek bir özet kaydında birleştirir (10 dk'lık kova 1 dk
// örneklerinden, saatlik kova 10 dk kayıtlarından). Kurallar:
//
//   - sayısal alanlar ortalama; CPUMax örneklerdeki en yüksek CPU (özet
//     kayıtlarda kendi CPUMax'ları)
//   - Uptime ve diskler son örnekten (doluluk yavaş değişir; ortalama anlamsız)
//   - sıcaklıklar sensör adına göre ortalama
//   - konteynerler ada göre ortalama (yalnızca göründükleri örnekler
//     üzerinden; kısa süre çalışan konteyner sıfırlarla sulanmaz), adların
//     birleşimi; MaxContainers'ı aşarsa en çok CPU kullananlar kalır
//
// Örnekler zamana göre sıralı olmalı; boş liste için sıfır değer döner.
func Aggregate(in []metrics.Stats) metrics.Stats {
	var out metrics.Stats
	n := len(in)
	if n == 0 {
		return out
	}
	var memTotal, memUsed, memCache, swapTotal, swapUsed float64
	for _, s := range in {
		out.CPU += s.CPU
		out.CPUMax = max(out.CPUMax, s.CPU, s.CPUMax)
		out.Load1 += s.Load1
		out.Load5 += s.Load5
		out.Load15 += s.Load15
		memTotal += float64(s.MemTotal)
		memUsed += float64(s.MemUsed)
		memCache += float64(s.MemCache)
		swapTotal += float64(s.SwapTotal)
		swapUsed += float64(s.SwapUsed)
		out.DiskReadBps += s.DiskReadBps
		out.DiskWriteBps += s.DiskWriteBps
		out.NetRxBps += s.NetRxBps
		out.NetTxBps += s.NetTxBps
	}
	f := float64(n)
	out.CPU /= f
	out.Load1 /= f
	out.Load5 /= f
	out.Load15 /= f
	out.MemTotal = avgU(memTotal, f)
	out.MemUsed = avgU(memUsed, f)
	out.MemCache = avgU(memCache, f)
	out.SwapTotal = avgU(swapTotal, f)
	out.SwapUsed = avgU(swapUsed, f)
	out.DiskReadBps /= f
	out.DiskWriteBps /= f
	out.NetRxBps /= f
	out.NetTxBps /= f

	last := in[n-1]
	out.Uptime = last.Uptime
	if len(last.Disks) > 0 {
		out.Disks = append([]metrics.Disk(nil), last.Disks...)
	}
	out.Temps = aggregateTemps(in)
	out.Containers = aggregateContainers(in)
	return out
}

func avgU(sum, n float64) uint64 { return uint64(math.Round(sum / n)) }

func aggregateTemps(in []metrics.Stats) []metrics.Temp {
	type acc struct {
		sum float64
		n   int
	}
	var order []string
	m := map[string]*acc{}
	for _, s := range in {
		for _, t := range s.Temps {
			a := m[t.Name]
			if a == nil {
				a = &acc{}
				m[t.Name] = a
				order = append(order, t.Name)
			}
			a.sum += t.C
			a.n++
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]metrics.Temp, len(order))
	for i, name := range order {
		a := m[name]
		out[i] = metrics.Temp{Name: name, C: a.sum / float64(a.n)}
	}
	return out
}

func aggregateContainers(in []metrics.Stats) []metrics.Container {
	type acc struct {
		last         metrics.Container
		cpu, mem     float64
		netRx, netTx float64
		n            int
	}
	var order []string
	m := map[string]*acc{}
	for _, s := range in {
		for _, c := range s.Containers {
			a := m[c.Name]
			if a == nil {
				a = &acc{}
				m[c.Name] = a
				order = append(order, c.Name)
			}
			a.last = c
			a.cpu += c.CPU
			a.mem += float64(c.Mem)
			a.netRx += c.NetRxBps
			a.netTx += c.NetTxBps
			a.n++
		}
	}
	if len(order) == 0 {
		return nil
	}
	out := make([]metrics.Container, len(order))
	for i, name := range order {
		a := m[name]
		f := float64(a.n)
		out[i] = metrics.Container{
			ID: a.last.ID, Name: name, MemLimit: a.last.MemLimit,
			CPU: a.cpu / f, Mem: avgU(a.mem, f), NetRxBps: a.netRx / f, NetTxBps: a.netTx / f,
		}
	}
	if len(out) > metrics.MaxContainers {
		sort.SliceStable(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
		out = out[:metrics.MaxContainers]
	}
	return out
}

// rollup yeni 1 dk satırı (t) önceki satırdan (prev) sonra bir 10 dk
// sınırını geçtiyse prev'in 10 dk'lık kovasını veritabanındaki 1 dk
// satırlarından hesaplayıp yazar; saat sınırı geçildiyse saatlik kovayı da 10 dk
// satırlarından. Bellekte durum tutmaz: yeniden başlatmadan sonra da doğru
// çalışır, aynı kovayı tekrar yazmak zararsızdır (upsert).
func (s *Service) rollup(ctx context.Context, id, prev, t int64) error {
	if b := prev - prev%600; b != t-t%600 {
		if err := s.rollupBucket(ctx, id, store.ServerRes1, store.ServerRes10, b, 600); err != nil {
			return err
		}
	}
	if b := prev - prev%3600; b != t-t%3600 {
		return s.rollupBucket(ctx, id, store.ServerRes10, store.ServerRes60, b, 3600)
	}
	return nil
}

func (s *Service) rollupBucket(ctx context.Context, id int64, from, to int, start, span int64) error {
	rows, err := s.store.ServerStats(ctx, id, from, start, start+span)
	if err != nil {
		return err
	}
	list := make([]metrics.Stats, 0, len(rows))
	for _, r := range rows {
		var st metrics.Stats
		if json.Unmarshal(r.Data, &st) == nil {
			list = append(list, st)
		}
	}
	if len(list) == 0 {
		return nil
	}
	data, err := json.Marshal(Aggregate(list))
	if err != nil {
		return err
	}
	return s.store.UpsertServerStats(ctx, id, to, start, data)
}
