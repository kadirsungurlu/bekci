package store

import (
	"context"
	"database/sql"
	"errors"
)

// 25: elle açılan olaylar ve olay güncellemeleri (durum sayfası iletişimi).
//
//   - incidents.kind = 'manual': yönetici/editörün bir durum sayfası için
//     açtığı olay (monitor_id ve server_id boş). page_id sayfası, title
//     başlığı, severity önemi (minor | major | critical), state son
//     güncellemenin aşaması (investigating | identified | monitoring |
//     resolved), created_by açan kullanıcı. Otomatik olaylarda title/severity
//     boştur; state yalnızca güncelleme yazılmışsa doludur.
//   - incident_monitors: manuel olayın etkilediği monitörler (durum
//     sayfasında adlarıyla yazılır).
//   - incident_updates: insan eliyle yazılan güncellemeler (aşama + metin);
//     hem manuel hem otomatik olaylara eklenebilir. Herkese açık sayfada ve
//     RSS akışında görünür; işlem geçmişinden (incident_events) ayrıdır.
func init() {
	RegisterMigration(25, `
ALTER TABLE incidents ADD COLUMN page_id INTEGER REFERENCES status_pages(id) ON DELETE SET NULL;
ALTER TABLE incidents ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE incidents ADD COLUMN severity TEXT NOT NULL DEFAULT '';
ALTER TABLE incidents ADD COLUMN state TEXT NOT NULL DEFAULT '';
ALTER TABLE incidents ADD COLUMN created_by TEXT NOT NULL DEFAULT '';
CREATE INDEX incidents_page ON incidents(page_id, started_at);

CREATE TABLE incident_monitors (
	incident_id INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
	monitor_id  INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	PRIMARY KEY (incident_id, monitor_id)
) WITHOUT ROWID;

CREATE TABLE incident_updates (
	id          INTEGER PRIMARY KEY,
	incident_id INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
	time        INTEGER NOT NULL,
	state       TEXT    NOT NULL,
	body        TEXT    NOT NULL DEFAULT '',
	user_id     INTEGER,
	username    TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX incident_updates_incident ON incident_updates(incident_id, time);
`)
}

// IncidentManual elle açılan olay (durum sayfası iletişimi).
const IncidentManual = "manual"

// Manuel olay önem dereceleri.
const (
	SeverityMinor    = "minor"
	SeverityMajor    = "major"
	SeverityCritical = "critical"
)

// ValidSeverity önem derecesi geçerli mi?
func ValidSeverity(s string) bool {
	return s == SeverityMinor || s == SeverityMajor || s == SeverityCritical
}

// Güncelleme aşamaları (sırayla).
const (
	StateInvestigating = "investigating"
	StateIdentified    = "identified"
	StateMonitoring    = "monitoring"
	StateResolved      = "resolved"
)

// IncidentStates aşamalar (arayüz sırası).
var IncidentStates = []string{StateInvestigating, StateIdentified, StateMonitoring, StateResolved}

// ValidState aşama geçerli mi?
func ValidState(s string) bool {
	for _, x := range IncidentStates {
		if x == s {
			return true
		}
	}
	return false
}

// IncidentUpdate bir olay güncellemesi.
type IncidentUpdate struct {
	ID         int64  `json:"id"`
	IncidentID int64  `json:"incident_id"`
	Time       int64  `json:"time"`
	State      string `json:"state"`
	Body       string `json:"body"`
	UserID     int64  `json:"user_id,omitempty"`
	Username   string `json:"username,omitempty"`
}

// ManualIncidentInput elle olay açma girdisi (doğrulanmış).
type ManualIncidentInput struct {
	PageID     int64
	Title      string
	Severity   string
	MonitorIDs []int64
	StartedAt  int64
	CreatedBy  string
	UserID     int64
	// İlk güncelleme (aşama + metin); State boşsa investigating.
	State string
	Body  string
}

// CreateManualIncident olayı, etkilenen monitörlerini ve ilk güncellemesini
// tek işlemde yazar; olayın kimliğini döner.
func (s *Store) CreateManualIncident(ctx context.Context, in ManualIncidentInput) (int64, error) {
	if in.State == "" {
		in.State = StateInvestigating
	}
	var id int64
	err := s.tx(ctx, func(tx *Tx) error {
		var err error
		var resolved any
		if in.State == StateResolved {
			resolved = in.StartedAt
		}
		id, err = insertID(ctx, tx, `
			INSERT INTO incidents (kind, page_id, title, severity, state, created_by, started_at, resolved_at, cause, data)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
			IncidentManual, in.PageID, in.Title, in.Severity, in.State, in.CreatedBy, in.StartedAt, resolved, in.Title)
		if err != nil {
			return err
		}
		for _, mid := range in.MonitorIDs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO incident_monitors (incident_id, monitor_id) VALUES (?, ?) ON CONFLICT DO NOTHING", id, mid); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO incident_updates (incident_id, time, state, body, user_id, username) VALUES (?, ?, ?, ?, ?, ?)`,
			id, in.StartedAt, in.State, in.Body, nullInt(in.UserID), in.CreatedBy); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, ?)",
			id, in.StartedAt, EventDown, in.Title, string(EventData(map[string]any{"manual": true, "user": in.CreatedBy, "severity": in.Severity})))
		return err
	})
	return id, err
}

// UpdateManualIncident başlık, önem ve etkilenen monitörleri değiştirir
// (yalnızca manuel olay).
func (s *Store) UpdateManualIncident(ctx context.Context, id int64, title, severity string, monitorIDs []int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, "UPDATE incidents SET title = ?, severity = ?, cause = ? WHERE id = ? AND kind = ?",
			title, severity, title, id, IncidentManual)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM incident_monitors WHERE incident_id = ?", id); err != nil {
			return err
		}
		for _, mid := range monitorIDs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO incident_monitors (incident_id, monitor_id) VALUES (?, ?) ON CONFLICT DO NOTHING", id, mid); err != nil {
				return err
			}
		}
		return nil
	})
}

// DeleteManualIncident manuel olayı siler (güncellemeleri ve geçmişiyle).
func (s *Store) DeleteManualIncident(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM incidents WHERE id = ? AND kind = ?", id, IncidentManual)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// IncidentMonitorIDs manuel olayın etkilediği monitörler.
func (s *Store) IncidentMonitorIDs(ctx context.Context, incidentID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT monitor_id FROM incident_monitors WHERE incident_id = ? ORDER BY monitor_id", incidentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AddIncidentUpdate güncellemeyi yazar ve olayın aşamasını günceller. Manuel
// olayda "resolved" aşaması olayı kapatır (resolved_at = zaman); kapanmış
// manuel olaya başka aşamada güncelleme yazılırsa olay yeniden açılır.
// Otomatik olaylarda resolved_at'e dokunulmaz.
func (s *Store) AddIncidentUpdate(ctx context.Context, u *IncidentUpdate) error {
	return s.tx(ctx, func(tx *Tx) error {
		var kind string
		if err := tx.QueryRowContext(ctx, "SELECT kind FROM incidents WHERE id = ?", u.IncidentID).Scan(&kind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		var err error
		u.ID, err = insertID(ctx, tx, `
			INSERT INTO incident_updates (incident_id, time, state, body, user_id, username) VALUES (?, ?, ?, ?, ?, ?)`,
			u.IncidentID, u.Time, u.State, u.Body, nullInt(u.UserID), u.Username)
		if err != nil {
			return err
		}
		if kind == IncidentManual {
			var resolved any
			if u.State == StateResolved {
				resolved = u.Time
			}
			_, err = tx.ExecContext(ctx, "UPDATE incidents SET state = ?, resolved_at = ? WHERE id = ?", u.State, resolved, u.IncidentID)
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE incidents SET state = ? WHERE id = ?", u.State, u.IncidentID)
		return err
	})
}

// DeleteIncidentUpdate güncellemeyi siler; olayın aşaması kalan son
// güncellemeden yeniden kurulur (manuel olayda kapanış da).
func (s *Store) DeleteIncidentUpdate(ctx context.Context, incidentID, updateID int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, "DELETE FROM incident_updates WHERE id = ? AND incident_id = ?", updateID, incidentID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ErrNotFound
		}
		var kind string
		if err := tx.QueryRowContext(ctx, "SELECT kind FROM incidents WHERE id = ?", incidentID).Scan(&kind); err != nil {
			return err
		}
		var state string
		var at int64
		err = tx.QueryRowContext(ctx, "SELECT state, time FROM incident_updates WHERE incident_id = ? ORDER BY time DESC, id DESC LIMIT 1", incidentID).Scan(&state, &at)
		if errors.Is(err, sql.ErrNoRows) {
			state, err = "", nil
		}
		if err != nil {
			return err
		}
		if kind != IncidentManual {
			_, err = tx.ExecContext(ctx, "UPDATE incidents SET state = ? WHERE id = ?", state, incidentID)
			return err
		}
		var resolved any
		if state == StateResolved {
			resolved = at
		}
		_, err = tx.ExecContext(ctx, "UPDATE incidents SET state = ?, resolved_at = ? WHERE id = ?", state, resolved, incidentID)
		return err
	})
}

// IncidentUpdates olayın güncellemeleri, yeniden eskiye.
func (s *Store) IncidentUpdates(ctx context.Context, incidentID int64) ([]IncidentUpdate, error) {
	m, err := s.UpdatesForIncidents(ctx, []int64{incidentID})
	if err != nil {
		return nil, err
	}
	if out, ok := m[incidentID]; ok {
		return out, nil
	}
	return []IncidentUpdate{}, nil
}

// UpdatesForIncidents verilen olayların güncellemeleri (olay → yeniden eskiye).
func (s *Store) UpdatesForIncidents(ctx context.Context, ids []int64) (map[int64][]IncidentUpdate, error) {
	out := map[int64][]IncidentUpdate{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, `SELECT id, incident_id, time, state, body, user_id, username FROM incident_updates
		WHERE incident_id IN (`+q+`) ORDER BY time DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var u IncidentUpdate
		var uid sql.NullInt64
		if err := rows.Scan(&u.ID, &u.IncidentID, &u.Time, &u.State, &u.Body, &uid, &u.Username); err != nil {
			return nil, err
		}
		u.UserID = uid.Int64
		out[u.IncidentID] = append(out[u.IncidentID], u)
	}
	return out, rows.Err()
}

// ManualIncidentsForPage sayfanın since'den sonra başlamış veya hâlâ süren
// manuel olayları (yeniden eskiye).
func (s *Store) ManualIncidentsForPage(ctx context.Context, pageID, since int64, limit int) ([]Incident, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+incidentCols+incidentFrom+`
		WHERE i.kind = ? AND i.page_id = ? AND (i.started_at >= ? OR i.resolved_at IS NULL)
		ORDER BY i.started_at DESC LIMIT ?`, IncidentManual, pageID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		in, err := scanIncident(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// IncidentMonitorNames verilen olayların etkilediği monitörlerin kimlikleri
// (olay → monitör kimlikleri; sayfada görünen adlarla eşlemek için).
func (s *Store) IncidentMonitorsFor(ctx context.Context, ids []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, "SELECT incident_id, monitor_id FROM incident_monitors WHERE incident_id IN ("+q+") ORDER BY monitor_id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var iid, mid int64
		if err := rows.Scan(&iid, &mid); err != nil {
			return nil, err
		}
		out[iid] = append(out[iid], mid)
	}
	return out, rows.Err()
}
