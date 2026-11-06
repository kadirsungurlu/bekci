package store

import (
	"context"
	"database/sql"
	"time"
)

// 50: ilişkisel topoloji verisi.
//   - monitors.server_id: monitörün çalıştığı sunucu (kullanıcı seçer; sunucu
//     silinirse bağ düşer, monitör kalır).
//   - probes.role/region/provider/code: görünen üst bilgi. Sunucu: rol + konum
//     (region); kontrol noktası: bölge, sağlayıcı, kısa kod. Boş = belirtilmemiş.
//   - channel_last_send.error: son başarısız gönderimin (adressiz) hata metni.
//   - channel_send_hourly: kanal başına saatlik gönderim sayacı (son 24 saat).
func init() {
	RegisterMigration(50, `
ALTER TABLE monitors ADD COLUMN server_id BIGINT REFERENCES probes(id) ON DELETE SET NULL;
CREATE INDEX monitors_server ON monitors(server_id);
ALTER TABLE probes ADD COLUMN role TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN region TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN provider TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN code TEXT NOT NULL DEFAULT '';
ALTER TABLE channel_last_send ADD COLUMN error TEXT NOT NULL DEFAULT '';
CREATE TABLE channel_send_hourly (
	channel_id BIGINT NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	hour BIGINT NOT NULL,
	sent INTEGER NOT NULL DEFAULT 0,
	failed INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (channel_id, hour)
);`)
}

// ProbeLabels sunucu ajanı ve kontrol noktasının görünen üst bilgisi.
type ProbeLabels struct {
	Role     string `json:"role"`     // sunucu rolü (Web, Veritabanı...)
	Region   string `json:"region"`   // sunucuda konum, noktada bölge
	Provider string `json:"provider"` // barındırma sağlayıcısı
	Code     string `json:"code"`     // kısa kod (IST1)
}

// SetProbeLabels üst bilgiyi yazar.
func (s *Store) SetProbeLabels(ctx context.Context, id int64, l ProbeLabels) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET role = ?, region = ?, provider = ?, code = ? WHERE id = ?",
		l.Role, l.Region, l.Provider, l.Code, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ChannelCount kanalın son 24 saatlik gönderim sayısı; Failed, Sent'in alt kümesi.
type ChannelCount struct {
	Sent   int `json:"sent"`
	Failed int `json:"failed"`
}

// ChannelSendCounts since (unix) sonrasını kapsayan saatlerin toplamı.
func (s *Store) ChannelSendCounts(ctx context.Context, since int64) (map[int64]ChannelCount, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT channel_id, SUM(sent), SUM(failed) FROM channel_send_hourly
		WHERE hour >= ? GROUP BY channel_id`, since-since%3600)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]ChannelCount{}
	for rows.Next() {
		var id int64
		var c ChannelCount
		if err := rows.Scan(&id, &c.Sent, &c.Failed); err != nil {
			return nil, err
		}
		out[id] = c
	}
	return out, rows.Err()
}

// LastMonitorIncidents her monitörün en yeni kesinti olayı (monitor, partial,
// degraded; manuel duyurular kesinti özeti değildir).
func (s *Store) LastMonitorIncidents(ctx context.Context) (map[int64]Incident, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, kind, monitor_id, started_at, resolved_at, cause, data, acked_at FROM incidents
		WHERE id IN (SELECT MAX(id) FROM incidents WHERE monitor_id IS NOT NULL AND kind IN (?, ?, ?) GROUP BY monitor_id)`,
		IncidentMonitor, IncidentPartial, IncidentDegraded)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Incident{}
	for rows.Next() {
		var in Incident
		var resolved, acked sql.NullInt64
		var data string
		if err := rows.Scan(&in.ID, &in.Kind, &in.MonitorID, &in.StartedAt, &resolved, &in.Cause, &data, &acked); err != nil {
			return nil, err
		}
		in.ResolvedAt, in.AckedAt = resolved.Int64, acked.Int64
		if data != "" {
			in.Data = []byte(data)
		}
		out[in.MonitorID] = in
	}
	return out, rows.Err()
}

// channelHourlyKeep saatlik sayaç satırlarının ömrü (24 saatlik pencere + pay).
const channelHourlyKeep = 48 * time.Hour
