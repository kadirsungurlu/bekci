package store

// Sunucu grafikleri sütunlu tablolardan ve sürekli özetlerden (yalnız TimescaleDB
// tam kipi): res 1 ham, 10/60/1440 server_metrics_10m/_1h/_1d. Diğer kiplerde
// nil döner ve grafik server_stats (Go özetleri) üzerinden çizilir.

import (
	"context"
	"database/sql"
	"fmt"
)

// ServerSeriesDisk bir bölümün kovadaki durumu.
type ServerSeriesDisk struct {
	Mount       string
	Used, Total float64
}

// ServerSeriesContainer bir konteynerin kovadaki ortalaması.
type ServerSeriesContainer struct {
	Name     string
	CPU, Mem float64
}

// ServerSeriesRow grafik kovası.
type ServerSeriesRow struct {
	T                                 int64
	CPU, CPUMax, Load1, Load5, Load15 float64
	MemUsed, MemCache, MemTotal       float64
	SwapUsed, SwapTotal               float64
	DiskRead, DiskWrite, NetRx, NetTx float64
	Temp                              sql.NullFloat64
	Disks                             []ServerSeriesDisk
	Containers                        []ServerSeriesContainer
}

const dbl = " AS DOUBLE PRECISION)"

func metricsSQL(res int) string {
	if res == ServerRes1 {
		return `SELECT time, cpu, cpu, load1, load5, load15, mem_used, mem_cache, mem_total, swap_used, swap_total,
			disk_read_bps, disk_write_bps, net_rx_bps, net_tx_bps, temp_max
			FROM server_metrics WHERE probe_id = ? AND time >= ? AND time < ? ORDER BY time`
	}
	table := map[int]string{ServerRes10: "server_metrics_10m", ServerRes60: "server_metrics_1h", ServerRes1440: "server_metrics_1d"}[res]
	return `SELECT bucket, CAST(cpu_avg` + dbl + `, CAST(cpu_max` + dbl + `, CAST(load1_avg` + dbl + `, CAST(load5_avg` + dbl + `,
		CAST(load15_avg` + dbl + `, CAST(mem_used_avg` + dbl + `, CAST(mem_cache_avg` + dbl + `, CAST(mem_total` + dbl + `,
		CAST(swap_used_avg` + dbl + `, CAST(swap_total` + dbl + `, CAST(disk_read_avg` + dbl + `, CAST(disk_write_avg` + dbl + `,
		CAST(net_rx_avg` + dbl + `, CAST(net_tx_avg` + dbl + `, temp_max
		FROM ` + table + ` WHERE probe_id = ? AND bucket >= ? AND bucket < ? ORDER BY bucket`
}

func diskSQL(res int) string {
	const raw = `FROM server_disk_metrics WHERE probe_id = ? AND time >= ? AND time < ?`
	const hour = `FROM server_disk_1h WHERE probe_id = ? AND bucket >= ? AND bucket < ?`
	switch res {
	case ServerRes1:
		return `SELECT time, mount, CAST(used` + dbl + `, CAST(total` + dbl + ` ` + raw + ` ORDER BY 1, 2`
	case ServerRes10:
		return `SELECT time / 600 * 600, mount, CAST(max(used)` + dbl + `, CAST(max(total)` + dbl + ` ` + raw + ` GROUP BY 1, 2 ORDER BY 1, 2`
	case ServerRes60:
		return `SELECT bucket, mount, CAST(used_max` + dbl + `, CAST(total` + dbl + ` ` + hour + ` ORDER BY 1, 2`
	}
	return `SELECT bucket / 86400 * 86400, mount, CAST(max(used_max)` + dbl + `, CAST(max(total)` + dbl + ` ` + hour + ` GROUP BY 1, 2 ORDER BY 1, 2`
}

func containerSQL(res int) string {
	const raw = `FROM server_container_metrics WHERE probe_id = ? AND time >= ? AND time < ?`
	switch res {
	case ServerRes1:
		return `SELECT time, name, cpu, CAST(mem` + dbl + ` ` + raw + ` ORDER BY 1, 2`
	case ServerRes10:
		return `SELECT time / 600 * 600, name, avg(cpu), CAST(avg(mem)` + dbl + ` ` + raw + ` GROUP BY 1, 2 ORDER BY 1, 2`
	case ServerRes60:
		return `SELECT bucket, name, cpu_avg, CAST(mem_avg` + dbl + ` FROM server_container_1h WHERE probe_id = ? AND bucket >= ? AND bucket < ? ORDER BY 1, 2`
	}
	return "" // günlük çözünürlükte konteyner dökümü yok (saklama 90 gün)
}

// ServerSeries [from, to) aralığının grafik kovaları; sütunlu kaynak yoksa nil.
func (s *Store) ServerSeries(ctx context.Context, probeID int64, res int, from, to int64) ([]ServerSeriesRow, error) {
	if !s.tsFull() {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, metricsSQL(res), probeID, from, to)
	if err != nil {
		return nil, fmt.Errorf("sunucu serisi: %w", err)
	}
	defer rows.Close()
	var out []ServerSeriesRow
	at := map[int64]int{}
	for rows.Next() {
		var r ServerSeriesRow
		if err := rows.Scan(&r.T, &r.CPU, &r.CPUMax, &r.Load1, &r.Load5, &r.Load15, &r.MemUsed, &r.MemCache, &r.MemTotal,
			&r.SwapUsed, &r.SwapTotal, &r.DiskRead, &r.DiskWrite, &r.NetRx, &r.NetTx, &r.Temp); err != nil {
			return nil, err
		}
		at[r.T] = len(out)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	dr, err := s.db.QueryContext(ctx, diskSQL(res), probeID, from, to)
	if err != nil {
		return nil, fmt.Errorf("sunucu disk serisi: %w", err)
	}
	defer dr.Close()
	for dr.Next() {
		var t int64
		var d ServerSeriesDisk
		if err := dr.Scan(&t, &d.Mount, &d.Used, &d.Total); err != nil {
			return nil, err
		}
		if i, ok := at[t]; ok {
			out[i].Disks = append(out[i].Disks, d)
		}
	}
	if err := dr.Err(); err != nil {
		return nil, err
	}
	q := containerSQL(res)
	if q == "" {
		return out, nil
	}
	cr, err := s.db.QueryContext(ctx, q, probeID, from, to)
	if err != nil {
		return nil, fmt.Errorf("sunucu konteyner serisi: %w", err)
	}
	defer cr.Close()
	for cr.Next() {
		var t int64
		var c ServerSeriesContainer
		if err := cr.Scan(&t, &c.Name, &c.CPU, &c.Mem); err != nil {
			return nil, err
		}
		if i, ok := at[t]; ok {
			out[i].Containers = append(out[i].Containers, c)
		}
	}
	return out, cr.Err()
}

// DiskHour bir bölümün saatlik en yüksek doluluğu (tahmin girdisi).
type DiskHour struct {
	T           int64
	Mount       string
	Used, Total uint64
}

// ServerDiskHourly from'dan sonraki saatlik disk noktaları (yalnız TimescaleDB
// tam kipi; aksi halde nil).
func (s *Store) ServerDiskHourly(ctx context.Context, probeID, from int64) ([]DiskHour, error) {
	if !s.tsFull() {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT bucket, mount, used_max, total FROM server_disk_1h
		WHERE probe_id = ? AND bucket >= ? ORDER BY bucket`, probeID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DiskHour
	for rows.Next() {
		var d DiskHour
		var used, total int64
		if err := rows.Scan(&d.T, &d.Mount, &used, &total); err != nil {
			return nil, err
		}
		d.Used, d.Total = uint64(used), uint64(total)
		out = append(out, d)
	}
	return out, rows.Err()
}
