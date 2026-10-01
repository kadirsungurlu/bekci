package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/metrics"
	"github.com/kadirsungurlu/bekci/internal/servers"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func boolMetric(v bool) int {
	if v {
		return 1
	}
	return 0
}

// agentMetrics /metrics çıktısının sunucu (ajan) bölümü: son örnekteki
// ölçümler, kontrol noktalarının durumu ve açık olay sayıları. Etiket sayısı
// sabittir: sunucu başına kimlik + ad (+ disk bölümü); konteyner ve sıcaklık
// sensörü listeleri etiketlenmez (en sıcak sensör tek değerdir). Kısıtlı
// kullanıcı yalnızca atanmış sunucuları ve kendi olaylarının sayısını görür;
// kontrol noktaları yalnızca kısıtsız kullanıcıya verilir.
func (s *Server) agentMetrics(r *http.Request, b *strings.Builder, family func(name, typ, help string), vis visibility) error {
	ctx := r.Context()
	now := s.now()
	probes, err := s.store.ListProbes(ctx)
	if err != nil {
		return err
	}
	type serverSample struct {
		p    store.Probe
		st   *metrics.Stats
		host *metrics.Host
	}
	var srvs []serverSample
	var locations []store.Probe
	for _, p := range probes {
		switch {
		case p.Kind == store.ProbeKindServer && vis.canServer(p.ID):
			x := serverSample{p: p, st: s.servers.Latest(ctx, p)}
			if p.HostInfo != "" {
				var h metrics.Host
				if json.Unmarshal([]byte(p.HostInfo), &h) == nil {
					x.host = &h
				}
			}
			srvs = append(srvs, x)
		case p.Kind == store.ProbeKindLocation && vis.all:
			locations = append(locations, p)
		}
	}
	slabels := func(p store.Probe) string {
		return fmt.Sprintf(`server_id="%d",server_name="%s"`, p.ID, promLabel(p.Name))
	}
	family("uptime_server_online", "gauge", "Sunucu ajanı veri gönderiyor mu (1 = çevrimiçi, 0 = çevrimdışı/bekliyor/kapalı)")
	for _, x := range srvs {
		fmt.Fprintf(b, "uptime_server_online{%s} %d\n", slabels(x.p), boolMetric(servers.State(x.p, now) == servers.StateOnline))
	}
	family("uptime_server_last_sample_timestamp_seconds", "gauge", "Sunucudan gelen son örneğin zamanı (unix saniye)")
	for _, x := range srvs {
		if x.p.MetricsAt > 0 {
			fmt.Fprintf(b, "uptime_server_last_sample_timestamp_seconds{%s} %d\n", slabels(x.p), x.p.MetricsAt)
		}
	}
	type gauge struct {
		name, help string
		value      func(st *metrics.Stats, host *metrics.Host) (float64, bool)
	}
	always := func(f func(st *metrics.Stats) float64) func(*metrics.Stats, *metrics.Host) (float64, bool) {
		return func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return f(st), true }
	}
	gauges := []gauge{
		{"uptime_server_cpu_percent", "CPU kullanımı (%, tüm çekirdekler)", always(func(st *metrics.Stats) float64 { return st.CPU })},
		{"uptime_server_memory_percent", "RAM kullanımı (%, önbellek hariç)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return st.MemPct(), st.MemTotal > 0 }},
		{"uptime_server_memory_used_bytes", "Kullanılan RAM (bayt, önbellek hariç)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return float64(st.MemUsed), st.MemTotal > 0 }},
		{"uptime_server_memory_total_bytes", "Toplam RAM (bayt)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return float64(st.MemTotal), st.MemTotal > 0 }},
		{"uptime_server_swap_percent", "Swap kullanımı (%)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return st.SwapPct(), st.SwapTotal > 0 }},
		{"uptime_server_load1", "1 dakikalık yük ortalaması", always(func(st *metrics.Stats) float64 { return st.Load1 })},
		{"uptime_server_load5", "5 dakikalık yük ortalaması", always(func(st *metrics.Stats) float64 { return st.Load5 })},
		{"uptime_server_load15", "15 dakikalık yük ortalaması", always(func(st *metrics.Stats) float64 { return st.Load15 })},
		{"uptime_server_load1_per_core", "1 dakikalık yük / mantıksal çekirdek", func(st *metrics.Stats, h *metrics.Host) (float64, bool) {
			if h == nil || h.Threads <= 0 {
				return 0, false
			}
			return st.Load1 / float64(h.Threads), true
		}},
		{"uptime_server_net_rx_bytes_per_second", "Gelen ağ trafiği (bayt/sn)", always(func(st *metrics.Stats) float64 { return st.NetRxBps })},
		{"uptime_server_net_tx_bytes_per_second", "Giden ağ trafiği (bayt/sn)", always(func(st *metrics.Stats) float64 { return st.NetTxBps })},
		{"uptime_server_disk_read_bytes_per_second", "Disk okuma (bayt/sn)", always(func(st *metrics.Stats) float64 { return st.DiskReadBps })},
		{"uptime_server_disk_write_bytes_per_second", "Disk yazma (bayt/sn)", always(func(st *metrics.Stats) float64 { return st.DiskWriteBps })},
		{"uptime_server_temperature_celsius", "En sıcak sensör (°C)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return st.TempMax() }},
		{"uptime_server_uptime_seconds", "Sunucunun açık kalma süresi (saniye)", func(st *metrics.Stats, _ *metrics.Host) (float64, bool) { return float64(st.Uptime), st.Uptime > 0 }},
		{"uptime_server_containers", "Son örnekteki Docker konteyneri sayısı", always(func(st *metrics.Stats) float64 { return float64(len(st.Containers)) })},
	}
	for _, g := range gauges {
		family(g.name, "gauge", g.help)
		for _, x := range srvs {
			if x.st == nil {
				continue
			}
			if v, ok := g.value(x.st, x.host); ok {
				fmt.Fprintf(b, "%s{%s} %s\n", g.name, slabels(x.p), promFloat(v))
			}
		}
	}
	family("uptime_server_disk_percent", "gauge", "Bölüm doluluğu (%)")
	for _, x := range srvs {
		if x.st == nil {
			continue
		}
		for _, d := range x.st.Disks {
			if d.Total > 0 {
				fmt.Fprintf(b, "uptime_server_disk_percent{%s,mount=\"%s\"} %s\n", slabels(x.p), promLabel(d.Mount), promFloat(100*float64(d.Used)/float64(d.Total)))
			}
		}
	}

	family("uptime_probe_online", "gauge", "Kontrol noktası çevrimiçi mi (1 = çevrimiçi, 0 = çevrimdışı veya devre dışı)")
	for _, p := range locations {
		online := engine.ProbeOnline(p, now) && !s.engine.ProbeDisconnected(p.ID)
		fmt.Fprintf(b, "uptime_probe_online{probe_id=\"%d\",probe_name=\"%s\"} %d\n", p.ID, promLabel(p.Name), boolMetric(online))
	}

	var monitorIDs, serverIDs []int64
	if !vis.all {
		monitorIDs, serverIDs = vis.list(), vis.serverList()
	}
	open, err := s.store.OpenIncidentCounts(ctx, monitorIDs, serverIDs)
	if err != nil {
		return err
	}
	family("uptime_incidents_open", "gauge", "Süren (çözülmemiş) olay sayısı, türe göre")
	for _, kind := range store.IncidentKinds {
		fmt.Fprintf(b, "uptime_incidents_open{kind=\"%s\"} %d\n", kind, open[kind])
	}
	return nil
}
