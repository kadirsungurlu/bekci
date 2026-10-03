package store

import (
	"context"
	"database/sql"
	"strings"
)

// 27: heartbeats.location — çok konumlu monitörde sonucu üreten konumların
// adı ("" = yalnızca ana sunucu). Monitör ayrıntısındaki "son kontroller"
// tablosunda gösterilir; eski kayıtlar boş kalır.
func init() {
	RegisterMigration(27, `
ALTER TABLE heartbeats ADD COLUMN location TEXT NOT NULL DEFAULT '';
`)
}

// LastBeats monitörün en son limit kontrolünü yeniden eskiye döner
// (ayrıntı sayfasındaki "son kontroller" tablosu).
func (s *Store) LastBeats(ctx context.Context, monitorID int64, limit int) ([]Beat, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT time, status, ping_ms, message, location FROM heartbeats WHERE monitor_id = ? ORDER BY time DESC, id DESC LIMIT ?",
		monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Beat{}
	for rows.Next() {
		b := Beat{MonitorID: monitorID}
		var ping sql.NullInt64
		if err := rows.Scan(&b.Time, &b.Status, &ping, &b.Message, &b.Location); err != nil {
			return nil, err
		}
		b.PingMs = -1
		if ping.Valid {
			b.PingMs = ping.Int64
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AuditFilter işlem kaydı süzgeçleri; sıfır değerler süzmez.
type AuditFilter struct {
	Before int64 // sayfalama: bu kimlikten küçükler
	Limit  int
	User   string // kullanıcı adı (tam eşleşme)
	// Action tam eylem kodu ("monitor.delete") ya da "monitor." gibi nokta ile
	// biten bir alan öneki (o alanın tüm eylemleri).
	Action string
	From   int64 // zaman aralığı (unix, dahil); 0 = sınırsız
	To     int64
	Query  string // hedef adı, ayrıntı, kullanıcı ve IP'de büyük/küçük harf duyarsız arama
}

// ListAuditFiltered süzgeçli işlem kaydı, en yeniden eskiye.
func (s *Store) ListAuditFiltered(ctx context.Context, f AuditFilter) ([]AuditEntry, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	q := "SELECT id, time, user_id, username, action, target_type, target_id, target_name, detail, ip FROM audit_log WHERE 1=1"
	var args []any
	if f.Before > 0 {
		q += " AND id < ?"
		args = append(args, f.Before)
	}
	if u := strings.TrimSpace(f.User); u != "" {
		q += " AND username = ?"
		args = append(args, u)
	}
	if a := strings.TrimSpace(f.Action); a != "" {
		if strings.HasSuffix(a, ".") {
			q += " AND action LIKE ?"
			args = append(args, a+"%")
		} else {
			q += " AND action = ?"
			args = append(args, a)
		}
	}
	if f.From > 0 {
		q += " AND time >= ?"
		args = append(args, f.From)
	}
	if f.To > 0 {
		q += " AND time <= ?"
		args = append(args, f.To)
	}
	if t := strings.TrimSpace(f.Query); t != "" {
		p := "%" + strings.ToLower(t) + "%"
		q += " AND (LOWER(target_name) LIKE ? OR LOWER(detail) LIKE ? OR LOWER(username) LIKE ? OR ip LIKE ?)"
		args = append(args, p, p, p, p)
	}
	q += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var uid, tid sql.NullInt64
		if err := rows.Scan(&e.ID, &e.Time, &uid, &e.Username, &e.Action, &e.TargetType, &tid, &e.TargetName, &e.Detail, &e.IP); err != nil {
			return nil, err
		}
		e.UserID, e.TargetID = uid.Int64, tid.Int64
		out = append(out, e)
	}
	return out, rows.Err()
}

// AuditFacets süzgeç kutuları için kayıtlarda geçen kullanıcı adları ve
// eylem kodları (sıralı, tekil).
func (s *Store) AuditFacets(ctx context.Context) (users, actions []string, err error) {
	users, err = s.distinct(ctx, "SELECT DISTINCT username FROM audit_log WHERE username <> '' ORDER BY username")
	if err != nil {
		return nil, nil, err
	}
	actions, err = s.distinct(ctx, "SELECT DISTINCT action FROM audit_log ORDER BY action")
	if err != nil {
		return nil, nil, err
	}
	return users, actions, nil
}

func (s *Store) distinct(ctx context.Context, q string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
