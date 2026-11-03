package store

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"encoding/json"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/mesh"
)

// IncidentMesh kontrol noktaları arası bağlantı sorunu (Main kararı). monitor_id
// ve server_id boştur; data mesh.Report JSON'udur (son yorum). Yalnız yöneticiler
// görür (kısıtlı kullanıcı süzgecinde yer almaz).
const IncidentMesh = "mesh"

// EventMeshChange açık mesh olayında yorum değişti (message: yeni neden, data: rapor).
const EventMeshChange = "mesh_change"

// OpenMeshIncident açık mesh olayı yoksa açar, varsa yorumunu günceller
// (işlem geçmişine "mesh_change"). Olayın kimliğini ve yeni açılıp açılmadığını döner.
func (s *Store) OpenMeshIncident(ctx context.Context, now int64, cause, data string) (int64, bool, error) {
	var id int64
	opened := false
	err := s.tx(ctx, func(tx *Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT id FROM incidents WHERE kind = ? AND resolved_at IS NULL ORDER BY id DESC LIMIT 1",
			IncidentMesh).Scan(&id)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id > 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE incidents SET cause = ?, data = ? WHERE id = ?", cause, data, id); err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx,
				"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, ?)",
				id, now, EventMeshChange, cause, data)
			return err
		}
		id, err = insertID(ctx, tx, "INSERT INTO incidents (kind, started_at, cause, data) VALUES (?, ?, ?, ?)",
			IncidentMesh, now, cause, data)
		if err != nil {
			return err
		}
		opened = true
		_, err = tx.ExecContext(ctx,
			"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', ?, ?)",
			id, now, EventDown, cause, data)
		return err
	})
	return id, opened, err
}

// OpenMeshIncidentData açık mesh olayının kimliği ve verisi (yoksa 0, "").
func (s *Store) OpenMeshIncidentData(ctx context.Context) (int64, string, error) {
	var id int64
	var data string
	err := s.db.QueryRowContext(ctx, "SELECT id, data FROM incidents WHERE kind = ? AND resolved_at IS NULL ORDER BY id DESC LIMIT 1",
		IncidentMesh).Scan(&id, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return id, data, err
}

// ResolveMeshIncident açık mesh olayını kapatır; kimliğini ve başlangıcını döner (yoksa 0, 0).
func (s *Store) ResolveMeshIncident(ctx context.Context, now int64) (int64, int64, error) {
	var id, started int64
	err := s.tx(ctx, func(tx *Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT id, started_at FROM incidents WHERE kind = ? AND resolved_at IS NULL ORDER BY id DESC LIMIT 1",
			IncidentMesh).Scan(&id, &started)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE incidents SET resolved_at = ? WHERE kind = ? AND resolved_at IS NULL", now, IncidentMesh); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx,
			"INSERT INTO incident_events (incident_id, time, kind, location, message, data) VALUES (?, ?, ?, '', '', ?)",
			id, now, EventUp, `{"downtime":`+strconv.FormatInt(max(0, now-started), 10)+`}`)
		return err
	})
	return id, started, err
}

// MeshIncidentCause mesh olayının nedeni dilde (data: mesh.Report JSON):
// "Kontrol noktası bağlantı sorunu: <ilk yorum>".
func MeshIncidentCause(lang, data string) string {
	var r mesh.Report
	if json.Unmarshal([]byte(data), &r) != nil || r.Verdict.Healthy() {
		return i18n.T(lang, "mesh.title")
	}
	return i18n.T(lang, "mesh.cause", mesh.FindingText(lang, r.Verdict.Findings[0], r.Names))
}
