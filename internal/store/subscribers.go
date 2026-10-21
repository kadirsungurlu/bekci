package store

import (
	"context"
	"database/sql"
	"errors"
)

// 36: durum sayfası e-posta aboneleri.
//
//   - status_pages.subscriptions: sayfada "güncellemelere abone ol" açık mı
//     (varsayılan kapalı).
//   - page_subscribers: ziyaretçinin e-postası. confirmed_at = 0 onay bekliyor
//     (çift onay); confirm_hash onay belirtecinin SHA-256 özeti (tek
//     kullanımlık, onaydan sonra boşaltılır); unsub_key çıkış bağlantısının
//     HMAC anahtarı (abone silinince bağlantılar geçersiz olur). Sayfa
//     silinince aboneleri de silinir. Aboneler yedeğe girmez (kişisel veri;
//     web push abonelikleri gibi).
func init() {
	RegisterMigration(36, `
ALTER TABLE status_pages ADD COLUMN subscriptions INTEGER NOT NULL DEFAULT 0;
CREATE TABLE page_subscribers (
	id              INTEGER PRIMARY KEY,
	page_id         INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
	email           TEXT    NOT NULL,
	lang            TEXT    NOT NULL DEFAULT 'tr',
	base_url        TEXT    NOT NULL DEFAULT '',
	confirmed_at    INTEGER NOT NULL DEFAULT 0,
	created_at      INTEGER NOT NULL,
	confirm_hash    TEXT    NOT NULL DEFAULT '',
	confirm_expires INTEGER NOT NULL DEFAULT 0,
	confirm_sent_at INTEGER NOT NULL DEFAULT 0,
	unsub_key       TEXT    NOT NULL,
	UNIQUE (page_id, email)
);
CREATE INDEX page_subscribers_confirm ON page_subscribers(confirm_hash);
`)
}

// MaxPageSubscribers bir sayfadaki en fazla abone (onaylı + bekleyen).
const MaxPageSubscribers = 10000

// PageSubscriber durum sayfası abonesi.
type PageSubscriber struct {
	ID             int64
	PageID         int64
	Email          string
	Lang           string
	BaseURL        string
	ConfirmedAt    int64 // 0: onay bekliyor
	CreatedAt      int64
	ConfirmHash    string
	ConfirmExpires int64
	ConfirmSentAt  int64
	UnsubKey       string
}

const subCols = `id, page_id, email, lang, base_url, confirmed_at, created_at, confirm_hash, confirm_expires, confirm_sent_at, unsub_key`

func scanSub(sc scanner) (PageSubscriber, error) {
	var p PageSubscriber
	err := sc.Scan(&p.ID, &p.PageID, &p.Email, &p.Lang, &p.BaseURL, &p.ConfirmedAt, &p.CreatedAt,
		&p.ConfirmHash, &p.ConfirmExpires, &p.ConfirmSentAt, &p.UnsubKey)
	return p, err
}

func (s *Store) oneSub(ctx context.Context, where string, args ...any) (PageSubscriber, error) {
	p, err := scanSub(s.db.QueryRowContext(ctx, "SELECT "+subCols+" FROM page_subscribers WHERE "+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) querySubs(ctx context.Context, q string, args ...any) ([]PageSubscriber, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PageSubscriber{}
	for rows.Next() {
		p, err := scanSub(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPageSubscriber kimliğe göre abone.
func (s *Store) GetPageSubscriber(ctx context.Context, id int64) (PageSubscriber, error) {
	return s.oneSub(ctx, "id = ?", id)
}

// PageSubscriberByEmail sayfadaki adres (küçük harfe çevrilmiş olmalı).
func (s *Store) PageSubscriberByEmail(ctx context.Context, pageID int64, email string) (PageSubscriber, error) {
	return s.oneSub(ctx, "page_id = ? AND email = ?", pageID, email)
}

// PageSubscriberByConfirmHash bekleyen onay belirtecinin sahibi (süre çağıranda denetlenir).
func (s *Store) PageSubscriberByConfirmHash(ctx context.Context, hash string) (PageSubscriber, error) {
	if hash == "" {
		return PageSubscriber{}, ErrNotFound
	}
	return s.oneSub(ctx, "confirm_hash = ?", hash)
}

// CountPageSubscribers sayfadaki abone sayısı (onaylı + bekleyen).
func (s *Store) CountPageSubscribers(ctx context.Context, pageID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM page_subscribers WHERE page_id = ?", pageID).Scan(&n)
	return n, err
}

// CreatePageSubscriber yeni (onay bekleyen) abone ekler.
func (s *Store) CreatePageSubscriber(ctx context.Context, p *PageSubscriber) error {
	id, err := insertID(ctx, s.db, `
		INSERT INTO page_subscribers (page_id, email, lang, base_url, confirmed_at, created_at, confirm_hash,
			confirm_expires, confirm_sent_at, unsub_key)
		VALUES (?, ?, ?, ?, 0, ?, ?, ?, ?, ?)`,
		p.PageID, p.Email, p.Lang, p.BaseURL, p.CreatedAt, p.ConfirmHash, p.ConfirmExpires, p.ConfirmSentAt, p.UnsubKey)
	p.ID = id
	return err
}

// RenewSubscriberConfirm bekleyen aboneye yeni onay belirteci (eskisi geçersiz olur).
func (s *Store) RenewSubscriberConfirm(ctx context.Context, id int64, hash string, expires, sentAt int64, lang, baseURL string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE page_subscribers SET confirm_hash = ?, confirm_expires = ?, confirm_sent_at = ?,
		lang = ?, base_url = ? WHERE id = ? AND confirmed_at = 0`, hash, expires, sentAt, lang, baseURL, id)
	return err
}

// ConfirmPageSubscriber onay belirtecini tüketir: hash hâlâ kayıtlıysa abone
// onaylanır ve belirteç silinir (tek kullanımlık). Tüketilemediyse ErrNotFound.
func (s *Store) ConfirmPageSubscriber(ctx context.Context, id int64, hash string, now int64) error {
	res, err := s.db.ExecContext(ctx, `UPDATE page_subscribers SET confirmed_at = ?, confirm_hash = '', confirm_expires = 0
		WHERE id = ? AND confirm_hash = ? AND confirm_hash <> ''`, now, id, hash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ListPageSubscribers sayfanın tüm aboneleri (en yeni önce).
func (s *Store) ListPageSubscribers(ctx context.Context, pageID int64) ([]PageSubscriber, error) {
	return s.querySubs(ctx, "SELECT "+subCols+" FROM page_subscribers WHERE page_id = ? ORDER BY created_at DESC, id DESC", pageID)
}

// ConfirmedPageSubscribers sayfanın onaylı aboneleri (bildirim alıcıları).
func (s *Store) ConfirmedPageSubscribers(ctx context.Context, pageID int64) ([]PageSubscriber, error) {
	return s.querySubs(ctx, "SELECT "+subCols+" FROM page_subscribers WHERE page_id = ? AND confirmed_at > 0 ORDER BY id", pageID)
}

// DeletePageSubscriber aboneyi siler; pageID > 0 ise yalnızca o sayfadaysa.
func (s *Store) DeletePageSubscriber(ctx context.Context, pageID, id int64) error {
	q, args := "DELETE FROM page_subscribers WHERE id = ?", []any{id}
	if pageID > 0 {
		q, args = q+" AND page_id = ?", append(args, pageID)
	}
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePendingSubscribersBefore t'den önce oluşturulmuş, hâlâ onaylanmamış
// aboneleri siler (günlük bakım; 7 gün).
func (s *Store) DeletePendingSubscribersBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM page_subscribers WHERE id IN
		(SELECT id FROM page_subscribers WHERE confirmed_at = 0 AND created_at < ? LIMIT ?)`, t)
}
