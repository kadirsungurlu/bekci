package store

import (
	"context"

	"github.com/kadirsungurlu/bekci/internal/metrics"
)

// 52: SSH giriş sayıları (iş 732, docs/GUVENLIK-EKLENTI-730.md §A.4).
//
//   - sec_auth_hourly: saat başına başarısız/başarılı giriş (olay saatine göre,
//     ajanın gönderdiği farklar toplanır). Saklama 90 gün.
//   - sec_auth_ip_daily: gün (UTC, panelin alış günü) başına IP deneme sayısı;
//     gün başına en çok deneyen 20 IP kalır. Saklama 30 gün.
//
// Timescale kipinde de düz tablo (küçük). Saklama Go bakımıyla (PruneSecAuth).
func init() {
	RegisterMigration(52, `
CREATE TABLE sec_auth_hourly (
	server_id INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	hour      INTEGER NOT NULL,
	failed    INTEGER NOT NULL DEFAULT 0,
	accepted  INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (server_id, hour)
);
CREATE TABLE sec_auth_ip_daily (
	server_id INTEGER NOT NULL REFERENCES probes(id) ON DELETE CASCADE,
	day       INTEGER NOT NULL,
	ip        TEXT    NOT NULL,
	count     INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (server_id, day, ip)
);
`)
}

const (
	secAuthHourlyKeepSec = 90 * 86400
	secAuthIPKeepSec     = 30 * 86400
	secAuthIPsPerDay     = 20
)

// SecAuthHour saklanan saatlik giriş sayısı.
type SecAuthHour struct {
	Hour     int64 `json:"hour"`
	Failed   int   `json:"failed"`
	Accepted int   `json:"accepted"`
}

// SecAuthIP IP başına deneme sayısı.
type SecAuthIP struct {
	IP    string `json:"ip"`
	Count int    `json:"count"`
}

// SecAuthView güvenlik sekmesindeki giriş özeti.
type SecAuthView struct {
	Hours       []SecAuthHour `json:"hours"`      // son 7 gün, yalnız boş olmayan saatler
	Failed24h   int           `json:"failed_24h"` // son 24 saat (saat kovasıyla)
	Accepted24h int           `json:"accepted_24h"`
	Failed7d    int           `json:"failed_7d"`
	TopIPs24h   []SecAuthIP   `json:"top_ips_24h"` // bugün + dün (UTC gün kovası)
	TopIPs7d    []SecAuthIP   `json:"top_ips_7d"`
}

// saveSecAuth giriş farkını tablolara ekler (SaveSecurity işlemi içinde).
func saveSecAuth(ctx context.Context, tx *Tx, serverID, now int64, a *metrics.SecAuth) error {
	for _, h := range a.Hours {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sec_auth_hourly (server_id, hour, failed, accepted) VALUES (?, ?, ?, ?)
			ON CONFLICT (server_id, hour) DO UPDATE SET failed = sec_auth_hourly.failed + excluded.failed,
				accepted = sec_auth_hourly.accepted + excluded.accepted`,
			serverID, h.Hour, h.Failed, h.Accepted); err != nil {
			return err
		}
	}
	if len(a.TopIPs) == 0 {
		return nil
	}
	day := now - now%86400
	for _, c := range a.TopIPs {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO sec_auth_ip_daily (server_id, day, ip, count) VALUES (?, ?, ?, ?)
			ON CONFLICT (server_id, day, ip) DO UPDATE SET count = sec_auth_ip_daily.count + excluded.count`,
			serverID, day, c.Key, c.Count); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM sec_auth_ip_daily WHERE server_id = ? AND day = ? AND ip NOT IN
		(SELECT ip FROM sec_auth_ip_daily WHERE server_id = ? AND day = ? ORDER BY count DESC, ip LIMIT ?)`,
		serverID, day, serverID, day, secAuthIPsPerDay)
	return err
}

// LoadSecAuth sekmedeki giriş özeti (now: unix sn).
func (s *Store) LoadSecAuth(ctx context.Context, serverID, now int64) (SecAuthView, error) {
	v := SecAuthView{Hours: []SecAuthHour{}, TopIPs24h: []SecAuthIP{}, TopIPs7d: []SecAuthIP{}}
	cur := now - now%3600
	rows, err := s.db.QueryContext(ctx, `SELECT hour, failed, accepted FROM sec_auth_hourly
		WHERE server_id = ? AND hour > ? AND hour <= ? ORDER BY hour`, serverID, cur-7*86400, cur)
	if err != nil {
		return v, err
	}
	for rows.Next() {
		var h SecAuthHour
		if err := rows.Scan(&h.Hour, &h.Failed, &h.Accepted); err != nil {
			rows.Close()
			return v, err
		}
		v.Hours = append(v.Hours, h)
		v.Failed7d += h.Failed
		if h.Hour > cur-86400 {
			v.Failed24h += h.Failed
			v.Accepted24h += h.Accepted
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return v, err
	}
	today := now - now%86400
	if v.TopIPs24h, err = s.secAuthTopIPs(ctx, serverID, today-86400); err != nil {
		return v, err
	}
	v.TopIPs7d, err = s.secAuthTopIPs(ctx, serverID, today-6*86400)
	return v, err
}

func (s *Store) secAuthTopIPs(ctx context.Context, serverID, fromDay int64) ([]SecAuthIP, error) {
	out := []SecAuthIP{}
	rows, err := s.db.QueryContext(ctx, `SELECT ip, SUM(count) AS n FROM sec_auth_ip_daily
		WHERE server_id = ? AND day >= ? GROUP BY ip ORDER BY n DESC, ip LIMIT 10`, serverID, fromDay)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var c SecAuthIP
		if err := rows.Scan(&c.IP, &c.Count); err != nil {
			return out, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PruneSecAuth saklama süresini aşan giriş sayılarını siler.
func (s *Store) PruneSecAuth(ctx context.Context, now int64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sec_auth_hourly WHERE hour < ?`, now-secAuthHourlyKeepSec); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sec_auth_ip_daily WHERE day < ?`, now-secAuthIPKeepSec)
	return err
}
