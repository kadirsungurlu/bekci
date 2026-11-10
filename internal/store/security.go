package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// 51: sunucu güvenlik durumu (iş 731, docs/GUVENLIK-EKLENTI-730.md §A.4).
//
//   - sec_state: son rapor (JSON, paket listesi hariç) ve filo sorguları için
//     düz sütunlar. upd_security -1: dağıtım güvenlik bilgisi vermiyor.
//     reboot_since: panelin "yeniden başlatma gerekli" durumunu ilk gördüğü an
//     (ajan saati değil). fw_active / av_ok / av_sig_age_h 732-734 içindir
//     (-1 bilinmiyor).
//   - sec_pkgs: bekleyen paketler; first_seen panelin ilk görme zamanı ("X
//     gündür bekliyor" bundan hesaplanır; paket sonradan güvenlik
//     güncellemesine dönüşürse o andan sayılır), listeden çıkan satır silinir.
//
// sec_updates_age kuralı (gün, 7, etkin) her sunucuya eklenir (disk_forecast kalıbı).
func init() {
	RegisterMigration(51, `
CREATE TABLE sec_state (
	server_id       INTEGER PRIMARY KEY REFERENCES probes(id) ON DELETE CASCADE,
	report          TEXT    NOT NULL DEFAULT '{}',
	hash            TEXT    NOT NULL DEFAULT '',
	received_at     INTEGER NOT NULL,
	upd_manager     TEXT    NOT NULL DEFAULT '',
	upd_total       INTEGER NOT NULL DEFAULT 0,
	upd_security    INTEGER NOT NULL DEFAULT 0,
	upd_checked     INTEGER NOT NULL DEFAULT 0,
	reboot_required INTEGER NOT NULL DEFAULT 0,
	reboot_since    INTEGER NOT NULL DEFAULT 0,
	fw_active       INTEGER NOT NULL DEFAULT -1,
	av_ok           INTEGER NOT NULL DEFAULT -1,
	av_sig_age_h    INTEGER NOT NULL DEFAULT -1
);
CREATE TABLE sec_pkgs (
	server_id   INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	name        TEXT    NOT NULL,
	current_ver TEXT    NOT NULL DEFAULT '',
	new_ver     TEXT    NOT NULL DEFAULT '',
	security    INTEGER NOT NULL DEFAULT 0,
	severity    TEXT    NOT NULL DEFAULT '',
	esm         INTEGER NOT NULL DEFAULT 0,
	first_seen  INTEGER NOT NULL,
	PRIMARY KEY (server_id, name)
);
INSERT INTO server_alerts (probe_id, metric, threshold, minutes, active, firing)
	SELECT DISTINCT probe_id, 'sec_updates_age', 7, 1, 1, 0 FROM server_alerts;
`)
}

// SecPkg panelde saklanan bekleyen paket.
type SecPkg struct {
	Name      string `json:"name"`
	Current   string `json:"current,omitempty"`
	New       string `json:"new"`
	Security  bool   `json:"security"`
	Severity  string `json:"severity,omitempty"`
	ESM       bool   `json:"esm,omitempty"`
	FirstSeen int64  `json:"first_seen"`
}

// SecSummary sunucu listesindeki güvenlik özeti.
type SecSummary struct {
	UpdTotal       int   `json:"upd_total"`
	UpdSecurity    int   `json:"upd_security"` // -1 bilinmiyor
	RebootRequired bool  `json:"reboot_required"`
	RebootSince    int64 `json:"reboot_since,omitempty"`
	OldestSecurity int64 `json:"oldest_security,omitempty"` // en eski bekleyen güvenlik paketinin first_seen'i
}

// ServerSecurity sunucunun saklanan güvenlik durumu (GET /api/servers/{id}/security).
type ServerSecurity struct {
	ReceivedAt  int64             `json:"received_at"` // 0: ajan hiç göndermedi
	Report      metrics.Security  `json:"report"`      // paket listesi Packages'ta
	RebootSince int64             `json:"reboot_since,omitempty"`
	Packages    []SecPkg          `json:"packages"`
	Notes       map[string]string `json:"notes"`
}

// SecurityChange kaydın önceki duruma göre farkı (zaman çizelgesi ve kurallar için).
type SecurityChange struct {
	NewSecurity     []string // ilk kez görülen güvenlik paketleri
	Installed       []string // listeden çıkan paketler
	RebootRequired  bool     // yeniden başlatma gerekli oldu
	RebootCleared   bool     // gereksinim bitti
	UpdatesReported bool     // bu raporda güncelleme bölümü vardı
}

// SaveSecurity raporu saklar. Bölüm raporda yoksa (toplanamadı) önceki değeri korunur.
func (s *Store) SaveSecurity(ctx context.Context, serverID, now int64, rep metrics.Security, hash string) (SecurityChange, error) {
	var ch SecurityChange
	stored := rep
	if rep.Updates != nil {
		u := *rep.Updates
		u.Packages = nil
		stored.Updates = &u
	}
	if rep.Auth != nil { // fark tablolara gider; raporda yalnız kaynak kalır
		stored.Auth = &metrics.SecAuth{Source: rep.Auth.Source}
	}
	err := s.tx(ctx, func(tx *Tx) error {
		var prevReport string
		var prevReboot, prevSince int64
		err := tx.QueryRowContext(ctx, `SELECT report, reboot_required, reboot_since FROM sec_state WHERE server_id = ?`, serverID).
			Scan(&prevReport, &prevReboot, &prevSince)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		var prev metrics.Security
		if prevReport != "" {
			_ = json.Unmarshal([]byte(prevReport), &prev)
		}
		if stored.Updates == nil {
			stored.Updates = prev.Updates
		}
		if stored.Reboot == nil {
			stored.Reboot = prev.Reboot
		}
		if stored.Auth == nil {
			stored.Auth = prev.Auth
		}
		if stored.Sessions == nil {
			stored.Sessions = prev.Sessions
		}
		if stored.Firewall == nil {
			stored.Firewall = prev.Firewall
		}
		// Defender kaldırıldıysa (no_defender) eski durum korunmaz.
		if stored.AV == nil && rep.Notes[metrics.SecSectionAV] != metrics.NoteNoDefender {
			stored.AV = prev.AV
		}
		avOK, avAge := -1, int64(-1)
		if a := stored.AV; a != nil {
			avOK = boolInt(a.OK())
			if a.SigUpdated > 0 {
				avAge = max(now-a.SigUpdated, 0) / 3600
			}
		}
		fw := -1
		if f := stored.Firewall; f != nil {
			fw = boolInt(f.Active)
		}
		reboot, since := int64(0), int64(0)
		if stored.Reboot != nil && stored.Reboot.Required {
			reboot, since = 1, prevSince
			if prevReboot == 0 || since == 0 {
				since = now
				ch.RebootRequired = rep.Reboot != nil
			}
		} else if prevReboot == 1 && rep.Reboot != nil {
			ch.RebootCleared = true
		}
		var total, secCount int
		var manager string
		var checked int64
		if u := stored.Updates; u != nil {
			total, secCount, manager, checked = u.Total, u.Security, u.Manager, u.CheckedAt
		}
		b, err := json.Marshal(stored)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sec_state (server_id, report, hash, received_at, upd_manager, upd_total, upd_security, upd_checked, reboot_required, reboot_since, fw_active, av_ok, av_sig_age_h)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (server_id) DO UPDATE SET report = excluded.report, hash = excluded.hash, received_at = excluded.received_at,
				upd_manager = excluded.upd_manager, upd_total = excluded.upd_total, upd_security = excluded.upd_security,
				upd_checked = excluded.upd_checked, reboot_required = excluded.reboot_required, reboot_since = excluded.reboot_since,
				fw_active = excluded.fw_active, av_ok = excluded.av_ok, av_sig_age_h = excluded.av_sig_age_h`,
			serverID, string(b), hash, now, manager, total, secCount, checked, reboot, since, fw, avOK, avAge); err != nil {
			return err
		}
		if rep.Auth != nil {
			if err := saveSecAuth(ctx, tx, serverID, now, rep.Auth); err != nil {
				return err
			}
		}
		if rep.Updates == nil {
			return nil
		}
		ch.UpdatesReported = true
		old := map[string]bool{} // ad -> güvenlik
		rows, err := tx.QueryContext(ctx, `SELECT name, security FROM sec_pkgs WHERE server_id = ?`, serverID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var n string
			var sec int
			if err := rows.Scan(&n, &sec); err != nil {
				rows.Close()
				return err
			}
			old[n] = sec == 1
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		seen := make(map[string]bool, len(rep.Updates.Packages))
		for _, p := range rep.Updates.Packages {
			seen[p.Name] = true
			if wasSec, ok := old[p.Name]; p.Security && (!ok || !wasSec) {
				ch.NewSecurity = append(ch.NewSecurity, p.Name)
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO sec_pkgs (server_id, name, current_ver, new_ver, security, severity, esm, first_seen)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT (server_id, name) DO UPDATE SET current_ver = excluded.current_ver, new_ver = excluded.new_ver,
					security = excluded.security, severity = excluded.severity, esm = excluded.esm,
					first_seen = CASE WHEN sec_pkgs.security = 0 AND excluded.security = 1 THEN excluded.first_seen ELSE sec_pkgs.first_seen END`,
				serverID, p.Name, p.Current, p.New, boolInt(p.Security), p.Severity, boolInt(p.ESM), now); err != nil {
				return err
			}
		}
		// Kesilmiş listede görünmeyen paket kurulmuş sayılmaz.
		if rep.Updates.Truncated {
			return nil
		}
		for n := range old {
			if seen[n] {
				continue
			}
			ch.Installed = append(ch.Installed, n)
			if _, err := tx.ExecContext(ctx, `DELETE FROM sec_pkgs WHERE server_id = ? AND name = ?`, serverID, n); err != nil {
				return err
			}
		}
		return nil
	})
	return ch, err
}

// LoadSecurity sunucunun güvenlik durumu; ajan hiç göndermediyse ReceivedAt 0.
func (s *Store) LoadSecurity(ctx context.Context, serverID int64) (ServerSecurity, error) {
	out := ServerSecurity{Packages: []SecPkg{}, Notes: map[string]string{}}
	var report string
	err := s.db.QueryRowContext(ctx, `SELECT received_at, report, reboot_since FROM sec_state WHERE server_id = ?`, serverID).
		Scan(&out.ReceivedAt, &report, &out.RebootSince)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	_ = json.Unmarshal([]byte(report), &out.Report)
	if out.Report.Notes != nil {
		out.Notes = out.Report.Notes
	}
	rows, err := s.db.QueryContext(ctx, `SELECT name, current_ver, new_ver, security, severity, esm, first_seen
		FROM sec_pkgs WHERE server_id = ? ORDER BY security DESC, first_seen, name`, serverID)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var p SecPkg
		var sec, esm int
		if err := rows.Scan(&p.Name, &p.Current, &p.New, &sec, &p.Severity, &esm, &p.FirstSeen); err != nil {
			return out, err
		}
		p.Security, p.ESM = sec == 1, esm == 1
		out.Packages = append(out.Packages, p)
	}
	return out, rows.Err()
}

// OldestSecurityPkg en eski bekleyen güvenlik paketi ve güvenlik paketi sayısı
// (yoksa count 0).
func (s *Store) OldestSecurityPkg(ctx context.Context, serverID int64) (name string, firstSeen int64, count int, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MIN(first_seen), 0) FROM sec_pkgs WHERE server_id = ? AND security = 1`, serverID).
		Scan(&count, &firstSeen)
	if err != nil || count == 0 {
		return "", 0, count, err
	}
	err = s.db.QueryRowContext(ctx, `SELECT name FROM sec_pkgs WHERE server_id = ? AND security = 1 AND first_seen = ? ORDER BY name LIMIT 1`, serverID, firstSeen).
		Scan(&name)
	return name, firstSeen, count, err
}

// SecSummaries tüm sunucuların güvenlik özeti (sunucu kimliği -> özet).
func (s *Store) SecSummaries(ctx context.Context) (map[int64]SecSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT st.server_id, st.upd_total, st.upd_security, st.reboot_required, st.reboot_since,
			COALESCE((SELECT MIN(p.first_seen) FROM sec_pkgs p WHERE p.server_id = st.server_id AND p.security = 1), 0)
		FROM sec_state st`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]SecSummary{}
	for rows.Next() {
		var id int64
		var v SecSummary
		var reboot int
		if err := rows.Scan(&id, &v.UpdTotal, &v.UpdSecurity, &reboot, &v.RebootSince, &v.OldestSecurity); err != nil {
			return nil, err
		}
		v.RebootRequired = reboot == 1
		out[id] = v
	}
	return out, rows.Err()
}
