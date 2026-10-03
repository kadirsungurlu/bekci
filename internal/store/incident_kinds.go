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
	// IncidentDegraded yavaş yanıt: monitör çalışıyor ama son N kontrolün
	// ortalama yanıt süresi eşiği aşıyor (monitor_id dolu; uptime etkilenmez,
	// herkese açık sayfalarda görünmez).
	IncidentDegraded = "degraded"
	// IncidentProbeOffline kontrol noktasından 90 sn'dir istek gelmiyor
	// (server_id kontrol noktasının kimliğidir; yalnızca "çevrimdışında
	// bildir" açık kontrol noktalarında açılır).
	IncidentProbeOffline = "probe_offline"
)

// IncidentKinds tüm olay türleri (sabit sırayla; /metrics sayaçları için).
var IncidentKinds = []string{IncidentMonitor, IncidentDegraded, IncidentPartial, IncidentServerOffline, IncidentServerAlert, IncidentProbeOffline, IncidentManual}

// İşlem geçmişinin olay türlerine özgü kayıtları.
const (
	// EventEscalated kısmi kesinti tam kesintiye dönüştü (kısmi olayı kapatır;
	// data.incident_id açılan normal olay).
	EventEscalated = "escalated"
	// EventFromPartial normal olay bir kısmi kesintiden dönüştü (data.incident_id kısmi olay).
	EventFromPartial = "from_partial"
	// EventResumed tam kesinti bitti ama konum hâlâ çalışmıyor: dönüştürülen
	// kısmi olay yeniden açıldı (data.incident_id kapanan normal olay).
	EventResumed = "resumed"
)

// ReopenPartialIncident tam kesintiye dönüşerek kapanmış kısmi olayı yeniden
// açar (tam kesinti bitti ama konum hâlâ çalışmıyor): geçmiş bölünmez, aynı
// olay sürer. Olay kapalı değilse, başka bir monitöre aitse ya da monitörün
// açık bir kısmi olayı zaten varsa açılmaz (false).
func (s *Store) ReopenPartialIncident(ctx context.Context, id, monitorID int64) (bool, error) {
	res, err := s.db.ExecContext(ctx, `
		UPDATE incidents SET resolved_at = NULL
		WHERE id = ? AND monitor_id = ? AND kind = ? AND resolved_at IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM incidents WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL)`,
		id, monitorID, IncidentPartial, monitorID, IncidentPartial)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// EscalatedPartialOf normal olayın dönüştüğü kısmi olayın kimliği
// ("Konum kesintisinden dönüştü" kaydının data.incident_id'si; yoksa 0).
func (s *Store) EscalatedPartialOf(ctx context.Context, incidentID int64) (int64, error) {
	var data string
	err := s.db.QueryRowContext(ctx, `
		SELECT data FROM incident_events WHERE incident_id = ? AND kind = ?
		ORDER BY time DESC, id DESC LIMIT 1`, incidentID, EventFromPartial).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var d struct {
		IncidentID int64 `json:"incident_id"`
	}
	if json.Unmarshal([]byte(data), &d) != nil {
		return 0, nil
	}
	return d.IncidentID, nil
}

// IsServerIncident ajan (sunucu veya kontrol noktası) olayı mı: server_id
// dolu, monitor_id boş; görünürlük sunucu atamasına göre.
func IsServerIncident(kind string) bool {
	return kind == IncidentServerOffline || kind == IncidentServerAlert || kind == IncidentProbeOffline
}

// Olay filtresinin tür grupları (arayüzdeki süzgeç çipleri).
const (
	KindGroupMonitor  = "monitor"
	KindGroupPartial  = "partial"
	KindGroupServer   = "server"
	KindGroupDegraded = "degraded"
	KindGroupManual   = "manual"
)

// kindsOf süzgeç grubunun olay türleri; bilinmeyen grup nil (süzgeç yok).
func kindsOf(group string) []string {
	switch group {
	case KindGroupMonitor:
		return []string{IncidentMonitor}
	case KindGroupPartial:
		return []string{IncidentPartial}
	case KindGroupDegraded:
		return []string{IncidentDegraded}
	case KindGroupManual:
		return []string{IncidentManual}
	case KindGroupServer:
		return []string{IncidentServerOffline, IncidentServerAlert, IncidentProbeOffline}
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

// Yavaş yanıt olayları ---------------------------------------------------------------

// DegradedIncidentData yavaş yanıt olayının verisi (ms).
type DegradedIncidentData struct {
	ThresholdMs int   `json:"threshold_ms"`
	Checks      int   `json:"checks"`  // ortalama penceresi (kontrol sayısı)
	AvgMs       int64 `json:"avg_ms"`  // başlangıçtaki ortalama
	PeakMs      int64 `json:"peak_ms"` // olay boyunca en yüksek ortalama
	LastMs      int64 `json:"last_ms"` // son (kapanışta: kapanış) ortalaması
}

// DegradedIncidentCause yavaş yanıt olayının neden metni (dilde).
func DegradedIncidentCause(lang string, d DegradedIncidentData) string {
	return i18n.T(lang, "incident.degraded.cause", d.AvgMs, d.Checks, d.ThresholdMs)
}

// ParseDegradedIncidentData olay verisini çözer (bozuksa boş).
func ParseDegradedIncidentData(raw json.RawMessage) DegradedIncidentData {
	var d DegradedIncidentData
	if len(raw) > 0 {
		json.Unmarshal(raw, &d)
	}
	return d
}

// StartDegradedIncident monitör için yavaş yanıt olayı açar (açık olan varsa
// onun kimliğini döner) ve başlangıç kaydını yazar.
func (s *Store) StartDegradedIncident(ctx context.Context, monitorID, t int64, d DegradedIncidentData) (int64, error) {
	cause := DegradedIncidentCause(i18n.TR, d)
	before, err := s.openKindIncidentID(ctx, IncidentDegraded, monitorID)
	if err != nil {
		return 0, err
	}
	id, err := s.startKindIncident(ctx, IncidentDegraded, monitorID, t, cause, EventData(d))
	if err != nil || id == 0 || id == before {
		return id, err
	}
	return id, s.AddIncidentEvents(ctx, id, IncidentEvent{Time: t, Kind: EventDown, Message: cause})
}

// OpenDegradedIncidentID monitörün açık yavaş yanıt olayı (yoksa 0).
func (s *Store) OpenDegradedIncidentID(ctx context.Context, monitorID int64) (int64, error) {
	return s.openKindIncidentID(ctx, IncidentDegraded, monitorID)
}

// UpdateDegradedIncident açık yavaş yanıt olayının son/en yüksek ortalamasını yazar.
func (s *Store) UpdateDegradedIncident(ctx context.Context, monitorID int64, avg int64) error {
	var id int64
	var raw string
	err := s.db.QueryRowContext(ctx, `
		SELECT id, data FROM incidents WHERE monitor_id = ? AND kind = ? AND resolved_at IS NULL
		ORDER BY id DESC LIMIT 1`, monitorID, IncidentDegraded).Scan(&id, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	d := ParseDegradedIncidentData(json.RawMessage(raw))
	if d.LastMs == avg && d.PeakMs >= avg {
		return nil
	}
	d.LastMs, d.PeakMs = avg, max(d.PeakMs, avg)
	return s.SetIncidentData(ctx, id, EventData(d))
}

// ResolveDegradedIncident monitörün açık yavaş yanıt olayını kapatır ve
// çözülme kaydını yazar (msg boşsa "normale döndü"); kimliğini ve
// başlangıcını döner (yoksa 0, 0).
func (s *Store) ResolveDegradedIncident(ctx context.Context, monitorID, t int64, msg string) (int64, int64, error) {
	id, started, err := s.resolveKindIncident(ctx, IncidentDegraded, monitorID, t)
	if err != nil || id == 0 {
		return id, started, err
	}
	return id, started, s.AddIncidentEvents(ctx, id, IncidentEvent{Time: t, Kind: EventUp, Message: msg,
		Data: EventData(map[string]int64{"downtime": max(0, t-started)})})
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
	Metric    string  `json:"metric"`              // cpu, mem, … , container veya offline
	Mount     string  `json:"mount,omitempty"`     // disk uyarısında bölüm; container'da konteyner adı
	Threshold float64 `json:"threshold,omitempty"` // eşik (offline: yok)
	Minutes   int     `json:"minutes"`             // ortalama penceresi / çevrimdışı süresi
	Value     float64 `json:"value"`               // başlangıçtaki değer (offline: veri gelmeyen dakika)
	Peak      float64 `json:"peak"`                // olay boyunca en yüksek ortalama
	Last      float64 `json:"last"`                // son (kapanışta: kapanış) ortalaması
	LastSeen  int64   `json:"last_seen,omitempty"` // offline: son verinin zamanı
	// Level uyarının seviyesi (warning | critical; eski olaylarda boş = kritik);
	// PeakLevel olay boyunca ulaşılan en yüksek seviye. State: container
	// metriğinde konteynerin durumu (exited, restarting…; boş: listede yok).
	Level     string `json:"level,omitempty"`
	PeakLevel string `json:"peak_level,omitempty"`
	State     string `json:"state,omitempty"`
}

// ServerIncidentCause sunucu olayının neden metni (dilde): "Sunucudan veri
// gelmiyor" veya "CPU %93,4 (10 dk ortalama, eşik %90)".
func ServerIncidentCause(lang, kind string, d ServerIncidentData) string {
	if kind == IncidentProbeOffline {
		return i18n.T(lang, "incident.probe.offline")
	}
	if kind == IncidentServerOffline || d.Metric == "offline" {
		return i18n.T(lang, "incident.server.offline")
	}
	if d.Metric == "container" {
		state := d.State
		if state == "" {
			state = i18n.T(lang, "incident.server.container_missing")
		}
		return i18n.T(lang, "incident.server.container", d.Mount, state)
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
	if a.Level != "" {
		d.Level, d.PeakLevel = a.Level, a.Level
		if a.Level == LevelWarning {
			d.Threshold = a.WarnThreshold
		}
	}
	if a.Metric == "container" {
		d.Threshold, d.Peak, d.Last, d.Value = 0, 0, 0, 0
		d.State = containerStateOf(value)
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

// Konteyner durumları değer olarak taşınır (FireServerAlertAt sayısal değer alır).
const (
	ContainerStateMissing    = 0 // listede yok (silinmiş ya da eski ajan)
	ContainerStateExited     = 1
	ContainerStateRestarting = 2
	ContainerStateOther      = 3 // paused, dead, created…
)

// ContainerStateCode durum metnini değere çevirir.
func ContainerStateCode(state string) float64 {
	switch state {
	case "":
		return ContainerStateMissing
	case "exited":
		return ContainerStateExited
	case "restarting":
		return ContainerStateRestarting
	}
	return ContainerStateOther
}

func containerStateOf(v float64) string {
	switch int(v) {
	case ContainerStateExited:
		return "exited"
	case ContainerStateRestarting:
		return "restarting"
	case ContainerStateOther:
		return "stopped"
	}
	return ""
}

// EventLevel işlem geçmişi: uyarının seviyesi değişti (data.level, data.value).
const EventLevel = "level"

// SetServerIncidentLevel tetiklenmiş kuralın açık olayının seviyesini değiştirir
// (uyarı ↔ kritik) ve işlem geçmişine yazar.
func (s *Store) SetServerIncidentLevel(ctx context.Context, alertID int64, level string, v float64, now int64) error {
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
	if d.Level == level {
		return nil
	}
	d.Level, d.Last = level, v
	d.Peak = max(d.Peak, v)
	if level == LevelCritical || d.PeakLevel == "" {
		d.PeakLevel = level
	}
	if err := s.SetIncidentData(ctx, id, EventData(d)); err != nil {
		return err
	}
	return s.AddIncidentEvents(ctx, id, IncidentEvent{Time: now, Kind: EventLevel,
		Message: i18n.T(i18n.TR, "incident.server.level_"+level), Data: EventData(map[string]any{"level": level, "value": v})})
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

// Kontrol noktası olayları ------------------------------------------------------------

// OpenProbeIncident kontrol noktası için probe_offline olayı açar (açık olan
// varsa onun kimliğini döner, yenisi açılmaz). lastSeen son isteğin zamanı.
func (s *Store) OpenProbeIncident(ctx context.Context, probeID, now, lastSeen int64) (int64, error) {
	d := ServerIncidentData{Metric: "offline", LastSeen: lastSeen}
	if lastSeen > 0 {
		d.Minutes = int(max(0, now-lastSeen) / 60)
		d.Value = float64(d.Minutes)
	}
	cause := ServerIncidentCause(i18n.TR, IncidentProbeOffline, d)
	err := s.tx(ctx, func(tx *Tx) error {
		var open int64
		err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(id), 0) FROM incidents WHERE server_id = ? AND kind = ? AND resolved_at IS NULL",
			probeID, IncidentProbeOffline).Scan(&open)
		if err != nil || open > 0 {
			return err
		}
		id, err := insertID(ctx, tx, `
			INSERT INTO incidents (kind, server_id, started_at, cause, data) VALUES (?, ?, ?, ?, ?)`,
			IncidentProbeOffline, probeID, now, cause, string(EventData(d)))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, '')",
			id, now, EventDown, cause)
		return err
	})
	if err != nil {
		return 0, err
	}
	return s.OpenProbeIncidentID(ctx, probeID)
}

// OpenProbeIncidentID kontrol noktasının açık probe_offline olayı (yoksa 0).
func (s *Store) OpenProbeIncidentID(ctx context.Context, probeID int64) (int64, error) {
	var id sql.NullInt64
	err := s.db.QueryRowContext(ctx, "SELECT MAX(id) FROM incidents WHERE server_id = ? AND kind = ? AND resolved_at IS NULL",
		probeID, IncidentProbeOffline).Scan(&id)
	return id.Int64, err
}

// ResolveProbeIncident kontrol noktasının açık probe_offline olayını kapatır;
// kimliğini ve başlangıcını döner (yoksa 0, 0).
func (s *Store) ResolveProbeIncident(ctx context.Context, probeID, now int64) (int64, int64, error) {
	var id, started int64
	err := s.tx(ctx, func(tx *Tx) error {
		err := tx.QueryRowContext(ctx, `
			SELECT id, started_at FROM incidents WHERE server_id = ? AND kind = ? AND resolved_at IS NULL
			ORDER BY id DESC LIMIT 1`, probeID, IncidentProbeOffline).Scan(&id, &started)
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
			"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', '', ?)",
			id, now, EventUp, `{"downtime":`+strconv.FormatInt(max(0, now-started), 10)+`}`)
		return err
	})
	return id, started, err
}

// rawString JSON veriyi saklama biçimine çevirir (yok: ”).
func rawString(data json.RawMessage) string {
	if len(data) == 0 || string(data) == "null" {
		return ""
	}
	return string(data)
}
