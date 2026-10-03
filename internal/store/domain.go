package store

import (
	"context"
	"time"
)

// 28: alan adı bitiş uyarısı (RDAP).
//
//   - monitors.domain_expiry: uyarı açık mı (varsayılan 1; eski monitörlerde
//     açık). domain_name: sorgulanan kayıt adı; domain_expires_at: bitiş
//     (unix; NULL = bilinmiyor); domain_registrar: kayıt operatörü;
//     domain_status: ok | unsupported | not_found | no_expiry | error | "";
//     domain_checked_at: son sorgu zamanı.
//   - domain_notices: eşik başına bir kez bildirim (cert_notices gibi).
func init() {
	RegisterMigration(28, `
ALTER TABLE monitors ADD COLUMN domain_expiry INTEGER NOT NULL DEFAULT 1;
ALTER TABLE monitors ADD COLUMN domain_name TEXT NOT NULL DEFAULT '';
ALTER TABLE monitors ADD COLUMN domain_expires_at INTEGER;
ALTER TABLE monitors ADD COLUMN domain_registrar TEXT NOT NULL DEFAULT '';
ALTER TABLE monitors ADD COLUMN domain_status TEXT NOT NULL DEFAULT '';
ALTER TABLE monitors ADD COLUMN domain_checked_at INTEGER NOT NULL DEFAULT 0;
CREATE TABLE domain_notices (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	expires_at INTEGER NOT NULL,
	days       INTEGER NOT NULL,
	sent_at    INTEGER NOT NULL,
	PRIMARY KEY (monitor_id, expires_at, days)
) WITHOUT ROWID;
`)
}

// Alan adı sorgu durumları (rdap paketiyle aynı değerler; "error" ek).
const (
	DomainOK          = "ok"
	DomainUnsupported = "unsupported"
	DomainNotFound    = "not_found"
	DomainNoExpiry    = "no_expiry"
	DomainError       = "error" // geçici hata: eski bilgi korunur
)

// DomainInfo bir sorgunun sonucu (UpdateDomain).
type DomainInfo struct {
	Name      string
	Status    string
	ExpiresAt int64 // 0 = bilinmiyor
	Registrar string
	CheckedAt int64
}

// UpdateDomain monitörün alan adı bilgisini yazar. DomainError'da bitiş ve
// kayıt operatörü korunur (yalnızca durum ve sorgu zamanı değişir).
func (s *Store) UpdateDomain(ctx context.Context, id int64, d DomainInfo) error {
	if d.CheckedAt == 0 {
		d.CheckedAt = time.Now().Unix()
	}
	if d.Status == DomainError {
		_, err := s.db.ExecContext(ctx,
			"UPDATE monitors SET domain_name = CASE WHEN ? = '' THEN domain_name ELSE ? END, domain_status = ?, domain_checked_at = ? WHERE id = ?",
			d.Name, d.Name, d.Status, d.CheckedAt, id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE monitors SET domain_name = ?, domain_status = ?, domain_expires_at = ?, domain_registrar = ?, domain_checked_at = ? WHERE id = ?",
		d.Name, d.Status, nullInt(d.ExpiresAt), d.Registrar, d.CheckedAt, id)
	return err
}

// MonitorsForDomainCheck alan adı uyarısı açık, etkin ve son sorgusu
// before'dan eski monitörler (sorgu sırası: en eski önce).
func (s *Store) MonitorsForDomainCheck(ctx context.Context, before int64) ([]Monitor, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+monitorCols+" FROM monitors WHERE active = 1 AND domain_expiry = 1 AND domain_checked_at < ? ORDER BY domain_checked_at, id",
		before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Monitor
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkDomainNotice bu bitiş tarihi + eşik için daha önce bildirim
// gönderilmediyse kaydeder ve true döner.
func (s *Store) MarkDomainNotice(ctx context.Context, monitorID, expiresAt int64, days int) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		"INSERT INTO domain_notices (monitor_id, expires_at, days, sent_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING",
		monitorID, expiresAt, days, time.Now().Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
