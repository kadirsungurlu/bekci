package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Etiketler (Uptime Kuma'daki gibi): ad + renk; monitöre isteğe bağlı bir
// değerle bağlanır (ör. "ortam: canlı"). Ad büyük/küçük harf duyarsız tekildir;
// ifade indeksi kullanılmadığı için (PostgreSQL uyumu) küçük harfli hali
// name_key sütununda tutulur.
func init() {
	RegisterMigration(4, `
CREATE TABLE tags (
	id         INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	name_key   TEXT    NOT NULL UNIQUE,
	color      TEXT    NOT NULL DEFAULT '#64748b',
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE monitor_tags (
	monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	tag_id     INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	value      TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (monitor_id, tag_id, value)
) WITHOUT ROWID;
CREATE INDEX monitor_tags_tag ON monitor_tags(tag_id);
`)
}

// ErrTagExists aynı adda (büyük/küçük harf duyarsız) başka bir etiket var.
var ErrTagExists = errors.New("Bu adda bir etiket zaten var")

type Tag struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

// MonitorTag bir monitöre bağlı etiket (monitör görünümünde kullanılır).
type MonitorTag struct {
	ID    int64  `json:"id"` // etiket kimliği
	Name  string `json:"name"`
	Color string `json:"color"`
	Value string `json:"value"`
}

// MonitorTagInput monitöre bağlanacak etiket ve değeri.
type MonitorTagInput struct {
	TagID int64  `json:"tag_id"`
	Value string `json:"value"`
}

// TagKey etiket adının tekillik anahtarı.
func TagKey(name string) string { return strings.ToLower(strings.TrimSpace(name)) }

const tagCols = "id, name, color, created_at, updated_at"

func scanTag(sc scanner) (Tag, error) {
	var t Tag
	err := sc.Scan(&t.ID, &t.Name, &t.Color, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (s *Store) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+tagCols+" FROM tags ORDER BY name_key, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		t, err := scanTag(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) GetTag(ctx context.Context, id int64) (Tag, error) {
	t, err := scanTag(s.db.QueryRowContext(ctx, "SELECT "+tagCols+" FROM tags WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

func (s *Store) CreateTag(ctx context.Context, t *Tag) error {
	now := time.Now().Unix()
	t.CreatedAt, t.UpdatedAt = now, now
	id, err := insertID(ctx, s.db,
		"INSERT INTO tags (name, name_key, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		t.Name, TagKey(t.Name), t.Color, now, now)
	if IsUniqueViolation(err) {
		return ErrTagExists
	}
	t.ID = id
	return err
}

func (s *Store) UpdateTag(ctx context.Context, t *Tag) error {
	t.UpdatedAt = time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		"UPDATE tags SET name = ?, name_key = ?, color = ?, updated_at = ? WHERE id = ?",
		t.Name, TagKey(t.Name), t.Color, t.UpdatedAt, t.ID)
	if IsUniqueViolation(err) {
		return ErrTagExists
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteTag(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM tags WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// MonitorTags tüm monitörlerin etiketlerini döner (monitör kimliği → etiketler;
// etiket adına, sonra değere göre sıralı).
func (s *Store) MonitorTags(ctx context.Context) (map[int64][]MonitorTag, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT mt.monitor_id, t.id, t.name, t.color, mt.value
		FROM monitor_tags mt JOIN tags t ON t.id = mt.tag_id
		ORDER BY t.name_key, t.id, mt.value`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]MonitorTag{}
	for rows.Next() {
		var mid int64
		var t MonitorTag
		if err := rows.Scan(&mid, &t.ID, &t.Name, &t.Color, &t.Value); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], t)
	}
	return out, rows.Err()
}

// SetMonitorTags monitörün etiketlerini verilen listeyle değiştirir.
func (s *Store) SetMonitorTags(ctx context.Context, monitorID int64, tags []MonitorTagInput) error {
	return s.tx(ctx, func(tx *Tx) error { return setMonitorTagsTx(ctx, tx, monitorID, tags) })
}

func setMonitorTagsTx(ctx context.Context, tx *Tx, monitorID int64, tags []MonitorTagInput) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM monitor_tags WHERE monitor_id = ?", monitorID); err != nil {
		return err
	}
	for _, t := range tags {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO monitor_tags (monitor_id, tag_id, value) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			monitorID, t.TagID, t.Value); err != nil {
			return err
		}
	}
	return nil
}
