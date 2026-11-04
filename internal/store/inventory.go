package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// 45: sunucu envanteri (docs/TIMESCALE-TASARIM.md §3.5). Sunucu kaydı probes
// tablosudur (kind = server): yabancı anahtarlar ve bütün ekranlar ona bağlı,
// ayrı bir servers tablosu yalnız çift kayıt üretirdi. Kayıt tabloları normal
// tablo: (server_id, anahtar) benzersiz, first_seen / last_seen; değişim
// olayları 660'ta server_events'e yazılır.
//
//   - inv_state: sunucunun son envanter zamanı ve toplanamayan bölümlerin notu.
//     Satır yoksa ajan envanter göndermemiş (eski ajan ya da henüz ilk tur).
//   - inv_certs.ignored: kullanıcı bu sertifika için uyarı istemiyor; yerine
//     başka sertifika gelirse sıfırlanır.
//   - inv_cert_alerts: bitişi yaklaşan sertifikanın tek açık uyarısı (parmak
//     izi başına: aynı sertifika hem dosyada hem portta görünse de tek olay).
//     Olayın alert_id'si -id'dir: kural kimlikleriyle çakışmaz, sunucu
//     olaylarının kapatma / seviye kodu aynen çalışır. stage son bildirilen
//     eşik (gün); -1 süresi doldu.
func init() {
	RegisterMigration(45, `
CREATE TABLE inv_state (
	server_id    INTEGER PRIMARY KEY REFERENCES probes(id) ON DELETE CASCADE,
	collected_at INTEGER NOT NULL,
	notes        TEXT    NOT NULL DEFAULT ''
);
CREATE TABLE inv_services (
	server_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	name        TEXT    NOT NULL,
	active      TEXT    NOT NULL DEFAULT '',
	sub         TEXT    NOT NULL DEFAULT '',
	enabled     TEXT    NOT NULL DEFAULT '',
	description TEXT    NOT NULL DEFAULT '',
	first_seen  INTEGER NOT NULL,
	last_seen   INTEGER NOT NULL,
	PRIMARY KEY (server_id, name)
);
CREATE TABLE inv_containers (
	server_id     INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	name          TEXT    NOT NULL,
	container_id  TEXT    NOT NULL DEFAULT '',
	image         TEXT    NOT NULL DEFAULT '',
	state         TEXT    NOT NULL DEFAULT '',
	health        TEXT    NOT NULL DEFAULT '',
	restart_count INTEGER NOT NULL DEFAULT 0,
	command       TEXT    NOT NULL DEFAULT '',
	started_at    TEXT    NOT NULL DEFAULT '',
	first_seen    INTEGER NOT NULL,
	last_seen     INTEGER NOT NULL,
	PRIMARY KEY (server_id, name)
);
CREATE TABLE inv_ports (
	server_id  INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	proto      TEXT    NOT NULL,
	addr       TEXT    NOT NULL,
	port       INTEGER NOT NULL,
	process    TEXT    NOT NULL DEFAULT '',
	cmd        TEXT    NOT NULL DEFAULT '',
	first_seen INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	PRIMARY KEY (server_id, proto, addr, port)
);
CREATE TABLE inv_certs (
	server_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	source      TEXT    NOT NULL,
	location    TEXT    NOT NULL,
	subject     TEXT    NOT NULL DEFAULT '',
	sans        TEXT    NOT NULL DEFAULT '',
	issuer      TEXT    NOT NULL DEFAULT '',
	not_before  INTEGER NOT NULL DEFAULT 0,
	not_after   INTEGER NOT NULL DEFAULT 0,
	fingerprint TEXT    NOT NULL DEFAULT '',
	ignored     INTEGER NOT NULL DEFAULT 0,
	first_seen  INTEGER NOT NULL,
	last_seen   INTEGER NOT NULL,
	PRIMARY KEY (server_id, source, location)
);
CREATE INDEX inv_certs_not_after ON inv_certs(not_after);
CREATE TABLE inv_disks (
	server_id  INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	mount      TEXT    NOT NULL,
	device     TEXT    NOT NULL DEFAULT '',
	fs         TEXT    NOT NULL DEFAULT '',
	total      INTEGER NOT NULL DEFAULT 0,
	first_seen INTEGER NOT NULL,
	last_seen  INTEGER NOT NULL,
	PRIMARY KEY (server_id, mount)
);
CREATE TABLE inv_cert_alerts (
	id          INTEGER PRIMARY KEY,
	server_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	fingerprint TEXT    NOT NULL,
	name        TEXT    NOT NULL DEFAULT '',
	not_after   INTEGER NOT NULL DEFAULT 0,
	stage       INTEGER NOT NULL DEFAULT 0,
	UNIQUE (server_id, fingerprint)
);
`)
}

// Satır görünümleri: ajan alanları + ilk/son görülme.
type (
	InvService struct {
		metrics.InvService
		FirstSeen int64 `json:"first_seen"`
		LastSeen  int64 `json:"last_seen"`
	}
	InvContainer struct {
		metrics.InvContainer
		FirstSeen int64 `json:"first_seen"`
		LastSeen  int64 `json:"last_seen"`
	}
	InvPort struct {
		metrics.InvPort
		FirstSeen int64 `json:"first_seen"`
		LastSeen  int64 `json:"last_seen"`
	}
	InvCert struct {
		metrics.InvCert
		Ignored   bool  `json:"ignored"`
		FirstSeen int64 `json:"first_seen"`
		LastSeen  int64 `json:"last_seen"`
	}
	InvDisk struct {
		metrics.InvDisk
		FirstSeen int64 `json:"first_seen"`
		LastSeen  int64 `json:"last_seen"`
	}
)

// ServerInventory sunucunun envanteri. CollectedAt 0: ajan hiç göndermemiş.
type ServerInventory struct {
	CollectedAt int64             `json:"collected_at"`
	Notes       map[string]string `json:"notes"`
	Services    []InvService      `json:"services"`
	Containers  []InvContainer    `json:"containers"`
	Ports       []InvPort         `json:"ports"`
	Certs       []InvCert         `json:"certs"`
	Disks       []InvDisk         `json:"disks"`
}

// SaveInventory envanteri tek işlemde yazar. Notes'ta adı geçen bölüm
// toplanamamıştır, son bilinen hâli korunur (Docker kısa süre yanıt vermeyince
// konteynerler silinip "ilk görülme" sıfırlanmasın). Değişen satırların
// first_seen'i korunur.
func (s *Store) SaveInventory(ctx context.Context, serverID, now int64, inv metrics.Inventory) error {
	notes := ""
	if len(inv.Notes) > 0 {
		b, err := json.Marshal(inv.Notes)
		if err != nil {
			return err
		}
		notes = string(b)
	}
	return s.tx(ctx, func(tx *Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO inv_state (server_id, collected_at, notes) VALUES (?, ?, ?)
			ON CONFLICT (server_id) DO UPDATE SET collected_at = excluded.collected_at, notes = excluded.notes`,
			serverID, now, notes); err != nil {
			return err
		}
		skip := func(section string) bool { _, ok := inv.Notes[section]; return ok }
		prune := func(table string) error {
			_, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE server_id = ? AND last_seen < ?", serverID, now)
			return err
		}
		if !skip(metrics.InvServices) {
			for _, r := range inv.Services {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO inv_services (server_id, name, active, sub, enabled, description, first_seen, last_seen)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (server_id, name) DO UPDATE SET active = excluded.active, sub = excluded.sub,
						enabled = excluded.enabled, description = excluded.description, last_seen = excluded.last_seen`,
					serverID, r.Name, r.Active, r.Sub, r.Enabled, r.Description, now, now); err != nil {
					return err
				}
			}
			if err := prune("inv_services"); err != nil {
				return err
			}
		}
		if !skip(metrics.InvContainers) {
			for _, r := range inv.Containers {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO inv_containers (server_id, name, container_id, image, state, health, restart_count, command, started_at, first_seen, last_seen)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (server_id, name) DO UPDATE SET container_id = excluded.container_id, image = excluded.image,
						state = excluded.state, health = excluded.health, restart_count = excluded.restart_count,
						command = excluded.command, started_at = excluded.started_at, last_seen = excluded.last_seen`,
					serverID, r.Name, r.ID, r.Image, r.State, r.Health, r.RestartCount, r.Command, r.StartedAt, now, now); err != nil {
					return err
				}
			}
			if err := prune("inv_containers"); err != nil {
				return err
			}
		}
		if !skip(metrics.InvPorts) {
			for _, r := range inv.Ports {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO inv_ports (server_id, proto, addr, port, process, cmd, first_seen, last_seen)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (server_id, proto, addr, port) DO UPDATE SET process = excluded.process, cmd = excluded.cmd,
						last_seen = excluded.last_seen`,
					serverID, r.Proto, r.Addr, r.Port, r.Process, r.Cmd, now, now); err != nil {
					return err
				}
			}
			if err := prune("inv_ports"); err != nil {
				return err
			}
		}
		if !skip(metrics.InvCerts) {
			for _, r := range inv.Certs {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO inv_certs (server_id, source, location, subject, sans, issuer, not_before, not_after, fingerprint, first_seen, last_seen)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (server_id, source, location) DO UPDATE SET subject = excluded.subject, sans = excluded.sans,
						issuer = excluded.issuer, not_before = excluded.not_before, not_after = excluded.not_after,
						ignored = CASE WHEN inv_certs.fingerprint = excluded.fingerprint THEN inv_certs.ignored ELSE 0 END,
						fingerprint = excluded.fingerprint, last_seen = excluded.last_seen`,
					serverID, r.Source, r.Location, r.Subject, strings.Join(r.SANs, ","), r.Issuer, r.NotBefore, r.NotAfter, r.Fingerprint, now, now); err != nil {
					return err
				}
			}
			if err := prune("inv_certs"); err != nil {
				return err
			}
		}
		if !skip(metrics.InvDisks) {
			for _, r := range inv.Disks {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO inv_disks (server_id, mount, device, fs, total, first_seen, last_seen)
					VALUES (?, ?, ?, ?, ?, ?, ?)
					ON CONFLICT (server_id, mount) DO UPDATE SET device = excluded.device, fs = excluded.fs,
						total = excluded.total, last_seen = excluded.last_seen`,
					serverID, r.Mount, r.Device, r.FS, int64(r.Total), now, now); err != nil {
					return err
				}
			}
			if err := prune("inv_disks"); err != nil {
				return err
			}
		}
		return nil
	})
}

// LoadInventory sunucunun envanteri; boş bölümler boş dizi döner.
func (s *Store) LoadInventory(ctx context.Context, serverID int64) (ServerInventory, error) {
	out := ServerInventory{Notes: map[string]string{}, Services: []InvService{}, Containers: []InvContainer{},
		Ports: []InvPort{}, Certs: []InvCert{}, Disks: []InvDisk{}}
	var notes string
	err := s.db.QueryRowContext(ctx, "SELECT collected_at, notes FROM inv_state WHERE server_id = ?", serverID).Scan(&out.CollectedAt, &notes)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if notes != "" {
		_ = json.Unmarshal([]byte(notes), &out.Notes)
	}
	scan := func(q string, each func(*sql.Rows) error) error {
		rows, err := s.db.QueryContext(ctx, q, serverID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := each(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := scan(`SELECT name, active, sub, enabled, description, first_seen, last_seen FROM inv_services WHERE server_id = ? ORDER BY name`, func(r *sql.Rows) error {
		var v InvService
		err := r.Scan(&v.Name, &v.Active, &v.Sub, &v.Enabled, &v.Description, &v.FirstSeen, &v.LastSeen)
		out.Services = append(out.Services, v)
		return err
	}); err != nil {
		return out, err
	}
	if err := scan(`SELECT name, container_id, image, state, health, restart_count, command, started_at, first_seen, last_seen FROM inv_containers WHERE server_id = ? ORDER BY name`, func(r *sql.Rows) error {
		var v InvContainer
		err := r.Scan(&v.Name, &v.ID, &v.Image, &v.State, &v.Health, &v.RestartCount, &v.Command, &v.StartedAt, &v.FirstSeen, &v.LastSeen)
		out.Containers = append(out.Containers, v)
		return err
	}); err != nil {
		return out, err
	}
	if err := scan(`SELECT proto, addr, port, process, cmd, first_seen, last_seen FROM inv_ports WHERE server_id = ? ORDER BY port, proto, addr`, func(r *sql.Rows) error {
		var v InvPort
		err := r.Scan(&v.Proto, &v.Addr, &v.Port, &v.Process, &v.Cmd, &v.FirstSeen, &v.LastSeen)
		out.Ports = append(out.Ports, v)
		return err
	}); err != nil {
		return out, err
	}
	certs, err := s.ServerCerts(ctx, serverID)
	if err != nil {
		return out, err
	}
	out.Certs = certs
	if err := scan(`SELECT mount, device, fs, total, first_seen, last_seen FROM inv_disks WHERE server_id = ? ORDER BY mount`, func(r *sql.Rows) error {
		var v InvDisk
		var total int64
		err := r.Scan(&v.Mount, &v.Device, &v.FS, &total, &v.FirstSeen, &v.LastSeen)
		v.Total = uint64(total)
		out.Disks = append(out.Disks, v)
		return err
	}); err != nil {
		return out, err
	}
	return out, nil
}

// ServerCerts sunucunun sertifikaları, bitişi en yakın olan önce.
func (s *Store) ServerCerts(ctx context.Context, serverID int64) ([]InvCert, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT source, location, subject, sans, issuer, not_before, not_after, fingerprint, ignored, first_seen, last_seen
		FROM inv_certs WHERE server_id = ? ORDER BY not_after, location`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []InvCert{}
	for rows.Next() {
		var v InvCert
		var sans string
		var ignored int
		if err := rows.Scan(&v.Source, &v.Location, &v.Subject, &sans, &v.Issuer, &v.NotBefore, &v.NotAfter, &v.Fingerprint, &ignored, &v.FirstSeen, &v.LastSeen); err != nil {
			return nil, err
		}
		v.Ignored = ignored != 0
		if sans != "" {
			v.SANs = strings.Split(sans, ",")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetCertIgnored sertifikanın uyarısını kapatır ya da açar (parmak izi başına:
// aynı sertifikanın dosya ve port satırı birlikte). Satır yoksa ErrNotFound.
func (s *Store) SetCertIgnored(ctx context.Context, serverID int64, fingerprint string, ignored bool) error {
	v := 0
	if ignored {
		v = 1
	}
	res, err := s.db.ExecContext(ctx, "UPDATE inv_certs SET ignored = ? WHERE server_id = ? AND fingerprint = ?", v, serverID, fingerprint)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// CertAlert bitişi yaklaşan sertifikanın açık uyarısı.
type CertAlert struct {
	ID          int64
	ServerID    int64
	Fingerprint string
	Name        string
	NotAfter    int64
	Stage       int
}

// CertAlerts sunucunun açık sertifika uyarıları.
func (s *Store) CertAlerts(ctx context.Context, serverID int64) ([]CertAlert, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, server_id, fingerprint, name, not_after, stage FROM inv_cert_alerts WHERE server_id = ?`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CertAlert
	for rows.Next() {
		var a CertAlert
		if err := rows.Scan(&a.ID, &a.ServerID, &a.Fingerprint, &a.Name, &a.NotAfter, &a.Stage); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// OpenCertAlert uyarı kaydını ve sunucu olayını tek işlemde açar; a.ID dolar.
// Olay kuralı: Metric "cert", Mount sertifika adı, Value kalan gün.
func (s *Store) OpenCertAlert(ctx context.Context, a *CertAlert, rule ServerAlert, days float64, now int64) error {
	return s.tx(ctx, func(tx *Tx) error {
		id, err := insertID(ctx, tx, `
			INSERT INTO inv_cert_alerts (server_id, fingerprint, name, not_after, stage) VALUES (?, ?, ?, ?, ?)`,
			a.ServerID, a.Fingerprint, a.Name, a.NotAfter, a.Stage)
		if err != nil {
			return err
		}
		a.ID = id
		rule.ID = -id
		return openServerIncidentTx(ctx, tx, rule, days, a.Name, now, 0)
	})
}

// SetCertAlertStage uyarının bildirilen eşiğini günceller.
func (s *Store) SetCertAlertStage(ctx context.Context, id int64, stage int) error {
	_, err := s.db.ExecContext(ctx, "UPDATE inv_cert_alerts SET stage = ? WHERE id = ?", stage, id)
	return err
}

// CloseCertAlert uyarıyı siler, olayını note ile kapatır.
func (s *Store) CloseCertAlert(ctx context.Context, id, now int64, note string) error {
	return s.tx(ctx, func(tx *Tx) error {
		if err := closeServerIncidentTx(ctx, tx, -id, now, note); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM inv_cert_alerts WHERE id = ?", id)
		return err
	})
}

// ServerIncidentResolveCert sertifika yenilenince ya da kaldırılınca yazılan çözülme notu.
const ServerIncidentResolveCert = "Sertifika yenilendi veya artık görünmüyor; olay kapatıldı"
