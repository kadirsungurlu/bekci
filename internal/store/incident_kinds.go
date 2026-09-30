package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// Olay türleri (migration 18) -------------------------------------------------------
//
//   - monitor: monitörün kesintisi (eski olayların hepsi bu türdür).
//   - partial: kısmi kesinti. Çok konumlu monitörde en az bir konum (tekrar
//     denemelerden sonra) çalışmıyor ama kesinti kuralı sağlanmadığı için
//     monitör çalışıyor. Bildirim gönderilmez, uptime'a etki etmez, herkese açık
//     durum sayfalarında görünmez. Monitör tamamen çalışmaz olursa kısmi olay
//     "tam kesintiye dönüştü" kaydıyla kapanır ve normal olay açılır.
//   - server_offline: sunucudan (ajandan) veri gelmiyor; çevrimdışı uyarısı
//     başlayınca açılır, veri gelince kapanır.
//   - server_alert: sunucu kaynak uyarısı (cpu, mem, disk…); uyarı başına bir olay.
//
// monitor_id sunucu olaylarında boştur (NULL); server_id yalnızca sunucu
// olaylarında doludur. alert_id sunucu uyarı kuralının kimliğidir (yabancı
// anahtar değildir: kural silinse de olay kalır). data türe özgü JSON'dur (” = yok).
//
// SQLite NOT NULL kısıtını ALTER ile kaldıramaz: incidents (ve ona bağlı
// incident_events / incident_captures) yeni tabloya kopyalanıp yeniden
// adlandırılır. Önce bağlı tablolar kopyalanıp eski halleri silinir; böylece
// eski incidents silinirken ON DELETE CASCADE hiçbir kaydı götürmez.
// Yeniden adlandırma diğer tabloların REFERENCES ifadelerini de günceller.
func init() {
	RegisterMigrationDialect(18, `
CREATE TABLE incidents_v18 (
	id          INTEGER PRIMARY KEY,
	kind        TEXT    NOT NULL DEFAULT 'monitor',
	monitor_id  INTEGER REFERENCES monitors(id) ON DELETE CASCADE,
	server_id   INTEGER REFERENCES probes(id) ON DELETE CASCADE,
	alert_id    INTEGER,
	started_at  INTEGER NOT NULL,
	resolved_at INTEGER,
	cause       TEXT    NOT NULL DEFAULT '',
	data        TEXT    NOT NULL DEFAULT ''
);
INSERT INTO incidents_v18 (id, kind, monitor_id, started_at, resolved_at, cause)
	SELECT id, 'monitor', monitor_id, started_at, resolved_at, cause FROM incidents;

CREATE TABLE incident_events_v18 (
	id          INTEGER PRIMARY KEY,
	incident_id INTEGER NOT NULL REFERENCES incidents_v18(id) ON DELETE CASCADE,
	time        INTEGER NOT NULL,
	kind        TEXT    NOT NULL,
	location    TEXT    NOT NULL DEFAULT '',
	message     TEXT    NOT NULL DEFAULT '',
	data        TEXT    NOT NULL DEFAULT ''
);
INSERT INTO incident_events_v18 (id, incident_id, time, kind, location, message, data)
	SELECT id, incident_id, time, kind, location, message, data FROM incident_events;

CREATE TABLE incident_captures_v18 (
	incident_id INTEGER NOT NULL PRIMARY KEY REFERENCES incidents_v18(id) ON DELETE CASCADE,
	time        INTEGER NOT NULL,
	location    TEXT    NOT NULL DEFAULT '',
	data        TEXT    NOT NULL
);
INSERT INTO incident_captures_v18 (incident_id, time, location, data)
	SELECT incident_id, time, location, data FROM incident_captures;

DROP TABLE incident_events;
DROP TABLE incident_captures;
DROP TABLE incidents;
ALTER TABLE incidents_v18 RENAME TO incidents;
ALTER TABLE incident_events_v18 RENAME TO incident_events;
ALTER TABLE incident_captures_v18 RENAME TO incident_captures;

CREATE INDEX incidents_monitor ON incidents(monitor_id, started_at);
CREATE INDEX incidents_started ON incidents(started_at);
CREATE INDEX incidents_server ON incidents(server_id, started_at);
CREATE INDEX incident_events_incident ON incident_events(incident_id, time);
`, `
ALTER TABLE incidents ADD COLUMN kind TEXT NOT NULL DEFAULT 'monitor';
ALTER TABLE incidents ALTER COLUMN monitor_id DROP NOT NULL;
ALTER TABLE incidents ADD COLUMN server_id INTEGER REFERENCES probes(id) ON DELETE CASCADE;
ALTER TABLE incidents ADD COLUMN alert_id INTEGER;
ALTER TABLE incidents ADD COLUMN data TEXT NOT NULL DEFAULT '';
CREATE INDEX incidents_server ON incidents(server_id, started_at);
`)
}

// Olay türleri.
const (
	IncidentMonitor       = "monitor"
	IncidentPartial       = "partial"
	IncidentServerOffline = "server_offline"
	IncidentServerAlert   = "server_alert"
)

// İşlem geçmişinin olay türlerine özgü kayıtları.
const (
	// EventEscalated kısmi kesinti tam kesintiye dönüştü (kısmi olayı kapatır;
	// data.incident_id açılan normal olay).
	EventEscalated = "escalated"
	// EventFromPartial normal olay bir kısmi kesintiden dönüştü (data.incident_id kısmi olay).
	EventFromPartial = "from_partial"
)

// IsServerIncident sunucu olayı mı?
func IsServerIncident(kind string) bool {
	return kind == IncidentServerOffline || kind == IncidentServerAlert
}

// Olay filtresinin tür grupları (arayüzdeki süzgeç çipleri).
const (
	KindGroupMonitor = "monitor"
	KindGroupPartial = "partial"
	KindGroupServer  = "server"
)

// kindsOf süzgeç grubunun olay türleri; bilinmeyen grup nil (süzgeç yok).
func kindsOf(group string) []string {
	switch group {
	case KindGroupMonitor:
		return []string{IncidentMonitor}
	case KindGroupPartial:
		return []string{IncidentPartial}
	case KindGroupServer:
		return []string{IncidentServerOffline, IncidentServerAlert}
	}
	return nil
}

// ValidKindGroup süzgeç grubu geçerli mi ("" = hepsi)?
func ValidKindGroup(g string) bool { return g == "" || kindsOf(g) != nil }

// Monitöre bağlı olaylar (normal ve kısmi) ---------------------------------------

// startKindIncident monitör için verilen türde olay açar ve açık olayın
// kimliğini döner; o türde açık olay varsa yenisi açılmaz.
func (s *Store) startKindIncident(ctx context.Context, kind string, monitorID, t int64, cause string, data json.RawMessage) (int64, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO incidents (kind, monitor_id, started_at, cause, data)
		SELECT CAST(? AS TEXT), CAST(? AS BIGINT), CAST(? AS BIGINT), CAST(? AS TEXT), CAST(? AS TEXT)
		WHERE NOT EXISTS (SELECT 1 FROM incidents WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL)`,
		kind, monitorID, t, cause, rawString(data), monitorID, kind)
	if err != nil {
		return 0, err
	}
	return s.openKindIncidentID(ctx, kind, monitorID)
}

func (s *Store) openKindIncidentID(ctx context.Context, kind string, monitorID int64) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx,
		"SELECT MAX(id) FROM incidents WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL", monitorID, kind).Scan(&id)
	return id.Int64, err
}

// resolveKindIncident monitörün verilen türdeki açık olayını kapatır;
// kapatılan olayın kimliğini ve başlangıcını döner (yoksa 0, 0).
func (s *Store) resolveKindIncident(ctx context.Context, kind string, monitorID, t int64) (int64, int64, error) {
	var id, started sql.NullInt64
	err := s.db.QueryRowContext(ctx, `
		UPDATE incidents SET resolved_at = ?
		WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL
		RETURNING id, started_at`, t, monitorID, kind).Scan(&id, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	return id.Int64, started.Int64, err
}

// StartPartialIncident monitör için kısmi kesinti olayı açar (açık olan varsa
// onun kimliğini döner).
func (s *Store) StartPartialIncident(ctx context.Context, monitorID, t int64, cause string, data json.RawMessage) (int64, error) {
	return s.startKindIncident(ctx, IncidentPartial, monitorID, t, cause, data)
}

// OpenPartialIncidentID monitörün açık kısmi kesinti olayı (yoksa 0).
func (s *Store) OpenPartialIncidentID(ctx context.Context, monitorID int64) (int64, error) {
	return s.openKindIncidentID(ctx, IncidentPartial, monitorID)
}

// ResolvePartialIncident monitörün açık kısmi kesinti olayını kapatır;
// kimliğini ve başlangıcını döner (yoksa 0, 0).
func (s *Store) ResolvePartialIncident(ctx context.Context, monitorID, t int64) (int64, int64, error) {
	return s.resolveKindIncident(ctx, IncidentPartial, monitorID, t)
}

// ClosePartialIncident monitörün açık kısmi olayı varsa işlem geçmişine ev'i
// yazar ve olayı kapatır (durdurma, hedef değişikliği). Olay yoksa bir şey yapmaz.
func (s *Store) ClosePartialIncident(ctx context.Context, monitorID, t int64, ev IncidentEvent) error {
	id, err := s.OpenPartialIncidentID(ctx, monitorID)
	if err != nil || id == 0 {
		return err
	}
	if err := s.AddIncidentEvents(ctx, id, ev); err != nil {
		return err
	}
	_, _, err = s.ResolvePartialIncident(ctx, monitorID, t)
	return err
}

// SetIncidentData olayın türe özgü verisini değiştirir.
func (s *Store) SetIncidentData(ctx context.Context, id int64, data json.RawMessage) error {
	_, err := s.db.ExecContext(ctx, "UPDATE incidents SET data = ? WHERE id = ?", rawString(data), id)
	return err
}

// PartialIncidentData kısmi kesinti olayının verisi: olay boyunca çalışmayan
// konumların adları (ilk çalışmama sırasıyla).
type PartialIncidentData struct {
	Locations []string `json:"locations"`
}

// Bakım kayıtları ---------------------------------------------------------------

// lastMaintEvent olayın son bakım kaydının türü (maint_start / maint_end; yoksa "").
func (s *Store) lastMaintEvent(ctx context.Context, incidentID int64) (string, error) {
	var kind string
	err := s.db.QueryRowContext(ctx, `
		SELECT kind FROM incident_events WHERE incident_id = ? AND kind IN (?, ?)
		ORDER BY time DESC, id DESC LIMIT 1`, incidentID, EventMaintStart, EventMaintEnd).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return kind, err
}

// InMaintLogged olayın son bakım kaydı kapanmamış bir "bakım başladı" mı?
// (Yeniden başlatmadan sonra aynı bakım için ikinci kez yazılmasın.)
func (s *Store) InMaintLogged(ctx context.Context, incidentID int64) (bool, error) {
	k, err := s.lastMaintEvent(ctx, incidentID)
	return k == EventMaintStart, err
}

// MarkIncidentMaint olayın işlem geçmişine bakım başlangıcını (start) veya
// bitişini yazar; son bakım kaydı zaten aynı durumu gösteriyorsa yazmaz
// (idempotent: yeniden başlatma, bakım dizininin yenilenmesi ve runner aynı
// bakımı ayrı ayrı bildirse de tek kayıt olur). Bitiş yalnızca kapanmamış bir
// başlangıçtan sonra yazılır. Denetim ve ekleme tek ifadedir. Yazıldıysa true döner.
func (s *Store) MarkIncidentMaint(ctx context.Context, incidentID, t int64, start bool) (bool, error) {
	if incidentID <= 0 {
		return false, nil
	}
	kind, msg, cond := EventMaintStart, MaintStartMessage, "<>"
	if !start {
		kind, msg, cond = EventMaintEnd, MaintEndMessage, "="
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO incident_events (incident_id, time, kind, location, message, data)
		SELECT CAST(? AS BIGINT), CAST(? AS BIGINT), CAST(? AS TEXT), '', CAST(? AS TEXT), ''
		WHERE COALESCE((SELECT kind FROM incident_events WHERE incident_id = ? AND kind IN (?, ?)
			ORDER BY time DESC, id DESC LIMIT 1), '') `+cond+` ?`,
		incidentID, t, kind, msg, incidentID, EventMaintStart, EventMaintEnd, EventMaintStart)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Bakım kayıtlarının (Türkçe saklanan) metinleri.
const (
	MaintStartMessage = "Bakım penceresi başladı; kontroller sürüyor, bildirim gönderilmiyor"
	MaintEndMessage   = "Bakım penceresi bitti"
)

// OpenMonitorIncidents açık monitör olayları: monitör kimliği → olay kimliği
// (bakım dizini yenilenince bakım kaydı yazmak için).
func (s *Store) OpenMonitorIncidents(ctx context.Context) (map[int64]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT monitor_id, MAX(id) FROM incidents WHERE kind = ? AND resolved_at IS NULL GROUP BY monitor_id", IncidentMonitor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var mid, id int64
		if err := rows.Scan(&mid, &id); err != nil {
			return nil, err
		}
		out[mid] = id
	}
	return out, rows.Err()
}

// Sunucu olayları -----------------------------------------------------------------

// ServerIncidentData sunucu olayının verisi.
type ServerIncidentData struct {
	Metric    string  `json:"metric"`              // cpu, mem, … veya offline
	Mount     string  `json:"mount,omitempty"`     // disk uyarısında bölüm
	Threshold float64 `json:"threshold,omitempty"` // eşik (offline: yok)
	Minutes   int     `json:"minutes"`             // ortalama penceresi / çevrimdışı süresi
	Value     float64 `json:"value"`               // başlangıçtaki değer (offline: veri gelmeyen dakika)
	Peak      float64 `json:"peak"`                // olay boyunca en yüksek ortalama
	Last      float64 `json:"last"`                // son (kapanışta: kapanış) ortalaması
	LastSeen  int64   `json:"last_seen,omitempty"` // offline: son verinin zamanı
}

// ServerIncidentCause sunucu olayının neden metni (dilde): "Sunucudan veri
// gelmiyor" veya "CPU %93,4 (10 dk ortalama, eşik %90)".
func ServerIncidentCause(lang, kind string, d ServerIncidentData) string {
	if kind == IncidentServerOffline || d.Metric == "offline" {
		return i18n.T(lang, "incident.server.offline")
	}
	name := d.Metric
	if i18n.Has("metric." + d.Metric) {
		name = i18n.T(lang, "metric."+d.Metric)
	}
	if d.Mount != "" {
		name += " (" + d.Mount + ")"
	}
	key := "incident.server.alert"
	if d.Metric == "load" {
		key = "incident.server.alert_per_core"
	}
	return i18n.T(lang, key, name, i18n.MetricValue(lang, d.Metric, d.Value), d.Minutes,
		i18n.MetricValue(lang, d.Metric, d.Threshold))
}

// ParseServerIncidentData olay verisini çözer (bozuksa boş).
func ParseServerIncidentData(raw json.RawMessage) ServerIncidentData {
	var d ServerIncidentData
	if len(raw) > 0 {
		json.Unmarshal(raw, &d)
	}
	return d
}

// openServerIncidentTx sunucu uyarısı başlayınca olay açar (FireServerAlert'in işleminde).
func openServerIncidentTx(ctx context.Context, tx *Tx, a ServerAlert, value float64, mount string, now, lastSeen int64) error {
	kind := IncidentServerAlert
	if a.Metric == "offline" {
		kind = IncidentServerOffline
	}
	d := ServerIncidentData{Metric: a.Metric, Mount: mount, Threshold: a.Threshold, Minutes: a.Minutes,
		Value: value, Peak: value, Last: value, LastSeen: lastSeen}
	if kind == IncidentServerOffline {
		d.Threshold, d.Peak, d.Last = 0, 0, 0
	}
	data := EventData(d)
	cause := ServerIncidentCause(i18n.TR, kind, d)
	id, err := insertID(ctx, tx, `
		INSERT INTO incidents (kind, server_id, alert_id, started_at, cause, data)
		VALUES (?, ?, ?, ?, ?, ?)`, kind, a.ProbeID, a.ID, now, cause, string(data))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, '')",
		id, now, EventDown, cause)
	return err
}

// ServerIncidentResolveRule kural silinince veya kapatılınca yazılan çözülme notu.
const ServerIncidentResolveRule = "Uyarı kuralı kaldırıldı veya kapatıldı; olay kapatıldı"

// ServerIncidentResolveDisabled sunucu devre dışı bırakılınca veya metrik
// toplama kapatılınca yazılan çözülme notu.
const ServerIncidentResolveDisabled = "Sunucu devre dışı bırakıldı veya metrik toplama kapatıldı; olay kapatıldı"

// closeServerIncidentTx uyarı bitince kuralın açık olayını kapatır ve çözülme
// kaydını yazar. note: çözülme kaydının mesajı ("" = normale döndü).
func closeServerIncidentTx(ctx context.Context, tx *Tx, alertID, now int64, note string) error {
	var id, started int64
	err := tx.QueryRowContext(ctx, `
		SELECT id, started_at FROM incidents WHERE alert_id = ? AND server_id IS NOT NULL AND resolved_at IS NULL
		ORDER BY id DESC LIMIT 1`, alertID).Scan(&id, &started)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE incidents SET resolved_at = ? WHERE id = ?", now, id); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx,
		"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, ?)",
		id, now, EventUp, note, `{"downtime":`+strconv.FormatInt(max(0, now-started), 10)+`}`)
	return err
}

// OpenServerIncidentID uyarı kuralının açık sunucu olayı (yoksa 0).
func (s *Store) OpenServerIncidentID(ctx context.Context, alertID int64) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT id FROM incidents WHERE alert_id = ? AND server_id IS NOT NULL AND resolved_at IS NULL
		ORDER BY id DESC LIMIT 1`, alertID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// UpdateServerIncidentValue tetiklenmiş kuralın açık olayının son ve en
// yüksek değerini günceller (değer değişmediyse yazmaz).
func (s *Store) UpdateServerIncidentValue(ctx context.Context, alertID int64, v float64) error {
	var id int64
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, data FROM incidents WHERE alert_id = ? AND kind = ? AND resolved_at IS NULL
		ORDER BY id DESC LIMIT 1`, alertID, IncidentServerAlert).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d := ParseServerIncidentData(json.RawMessage(raw))
	if d.Last == v && d.Peak >= v {
		return nil
	}
	d.Last = v
	d.Peak = max(d.Peak, v)
	return s.SetIncidentData(ctx, id, EventData(d))
}

// rawString JSON veriyi saklama biçimine çevirir (yok: ”).
func rawString(data json.RawMessage) string {
	if len(data) == 0 || string(data) == "null" {
		return ""
	}
	return string(data)
}
