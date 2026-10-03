package store

import (
	"context"
	"database/sql"
	"errors"
)

// 32: Web Push abonelikleri (E-14). Tarayıcının PushSubscription kaydı;
// bir kullanıcının birden çok cihazı olabilir. endpoint push servisine
// özgü, tekil adrestir; p256dh/auth istemci anahtarlarıdır (base64url).
// Push servisi 404/410 verince kayıt silinir. VAPID anahtar çifti settings
// tablosunda "vapid" anahtarında (ilk açılışta üretilir; yedekte yok,
// yedek geri yüklenen kurulumda abonelikler yeniden alınır).
func init() {
	RegisterMigration(32, `
CREATE TABLE push_subscriptions (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	endpoint     TEXT    NOT NULL UNIQUE,
	p256dh       TEXT    NOT NULL,
	auth         TEXT    NOT NULL,
	user_agent   TEXT    NOT NULL DEFAULT '',
	created_at   INTEGER NOT NULL,
	last_used_at INTEGER,
	fail_count   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX push_subscriptions_user ON push_subscriptions(user_id);
`)
}

// PushSubscription bir cihazın aboneliği.
type PushSubscription struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Endpoint   string `json:"endpoint"`
	P256dh     string `json:"-"`
	Auth       string `json:"-"`
	UserAgent  string `json:"user_agent"`
	CreatedAt  int64  `json:"created_at"`
	LastUsedAt int64  `json:"last_used_at"`
	FailCount  int    `json:"fail_count"`
}

const pushCols = "id, user_id, endpoint, p256dh, auth, user_agent, created_at, last_used_at, fail_count"

func scanPush(sc scanner) (PushSubscription, error) {
	var p PushSubscription
	var last sql.NullInt64
	err := sc.Scan(&p.ID, &p.UserID, &p.Endpoint, &p.P256dh, &p.Auth, &p.UserAgent, &p.CreatedAt, &last, &p.FailCount)
	p.LastUsedAt = last.Int64
	return p, err
}

func (s *Store) queryPush(ctx context.Context, q string, args ...any) ([]PushSubscription, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PushSubscription{}
	for rows.Next() {
		p, err := scanPush(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPushSubscription aboneliği ekler; aynı endpoint varsa (başka
// kullanıcıya ait olsa da: aynı tarayıcıda hesap değişti) bu kullanıcıya
// taşır ve anahtarları günceller. Kaydın kimliğini döner.
func (s *Store) UpsertPushSubscription(ctx context.Context, p PushSubscription) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE push_subscriptions SET user_id = ?, p256dh = ?, auth = ?, user_agent = ?, fail_count = 0 WHERE endpoint = ?`,
			p.UserID, p.P256dh, p.Auth, p.UserAgent, p.Endpoint)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n > 0 {
			return tx.QueryRowContext(ctx, "SELECT id FROM push_subscriptions WHERE endpoint = ?", p.Endpoint).Scan(&id)
		}
		id, err = insertID(ctx, tx, `
			INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, user_agent, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			p.UserID, p.Endpoint, p.P256dh, p.Auth, p.UserAgent, p.CreatedAt)
		return err
	})
	return id, err
}

// PushSubscriptionsForUser kullanıcının cihazları (en yeni önce).
func (s *Store) PushSubscriptionsForUser(ctx context.Context, userID int64) ([]PushSubscription, error) {
	return s.queryPush(ctx, "SELECT "+pushCols+" FROM push_subscriptions WHERE user_id = ? ORDER BY id DESC", userID)
}

// PushSubscriptionsFor verilen kullanıcı adlarının (boş liste: herkesin)
// abonelikleri; yalnızca etkin kullanıcılar.
func (s *Store) PushSubscriptionsFor(ctx context.Context, usernames []string) ([]PushSubscription, error) {
	q := "SELECT " + pushCols + " FROM push_subscriptions p WHERE EXISTS (SELECT 1 FROM users u WHERE u.id = p.user_id AND u.disabled = 0"
	var args []any
	if len(usernames) > 0 {
		q += " AND LOWER(u.username) IN (" + placeholders(len(usernames)) + ")"
		for _, u := range usernames {
			args = append(args, u)
		}
	}
	q += ") ORDER BY p.user_id, p.id"
	return s.queryPush(ctx, q, args...)
}

// DeletePushSubscription kullanıcının kendi aboneliğini siler.
func (s *Store) DeletePushSubscription(ctx context.Context, userID, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE id = ? AND user_id = ?", id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePushByEndpoint push servisi "artık yok" dediğinde (404/410) kaydı siler.
func (s *Store) DeletePushByEndpoint(ctx context.Context, endpoint string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM push_subscriptions WHERE endpoint = ?", endpoint)
	return err
}

// TouchPush başarılı gönderimi yazar; failed ise hata sayacını artırır.
func (s *Store) TouchPush(ctx context.Context, id int64, now int64, failed bool) error {
	if failed {
		_, err := s.db.ExecContext(ctx, "UPDATE push_subscriptions SET fail_count = fail_count + 1 WHERE id = ?", id)
		return err
	}
	_, err := s.db.ExecContext(ctx, "UPDATE push_subscriptions SET last_used_at = ?, fail_count = 0 WHERE id = ?", now, id)
	return err
}

// PushSubscriptionCount abonelik sayısı (kanal formunda bilgi).
func (s *Store) PushSubscriptionCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM push_subscriptions").Scan(&n)
	return n, err
}

// vapidKey settings tablosundaki VAPID anahtar çiftinin anahtarı.
const vapidKey = "vapid"

// VAPIDKeys kayıtlı anahtar çiftini (JSON) döner; yoksa ok=false.
func (s *Store) VAPIDKeys(ctx context.Context) (string, bool, error) {
	return s.GetSetting(ctx, vapidKey)
}

// SaveVAPIDKeys anahtar çiftini yalnızca henüz yoksa yazar (iki örnek aynı
// anda açılırsa ilk yazan kazanır); kayıtlı değeri döner.
func (s *Store) SaveVAPIDKeys(ctx context.Context, v string) (string, error) {
	err := s.tx(ctx, func(tx *Tx) error {
		var cur string
		err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", vapidKey).Scan(&cur)
		if err == nil {
			v = cur
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES (?, ?)", vapidKey, v)
		return err
	})
	return v, err
}
