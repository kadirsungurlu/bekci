package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// PageMonitor durum sayfasındaki bir monitör; Name boşsa monitörün kendi adı gösterilir.
type PageMonitor struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type PageSection struct {
	Title    string        `json:"title"`
	Monitors []PageMonitor `json:"monitors"`
}

type StatusPage struct {
	ID           int64         `json:"id"`
	Slug         string        `json:"slug"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	Footer       string        `json:"footer"`
	Sections     []PageSection `json:"sections"`
	CustomDomain string        `json:"custom_domain"`
	HasPassword  bool          `json:"has_password"`
	ShowTargets  bool          `json:"show_targets"`
	Published    bool          `json:"published"`
	HasLogo      bool          `json:"has_logo"`
	CreatedAt    int64         `json:"created_at"`
	UpdatedAt    int64         `json:"updated_at"`
	PasswordHash string        `json:"-"`
}

// MonitorIDs sayfadaki tüm monitörlerin kimlikleri.
func (p StatusPage) MonitorIDs() []int64 {
	var ids []int64
	for _, s := range p.Sections {
		for _, m := range s.Monitors {
			ids = append(ids, m.ID)
		}
	}
	return ids
}

const pageCols = `id, slug, title, description, footer, sections, custom_domain, password_hash,
	show_targets, published, logo IS NOT NULL, created_at, updated_at`

func scanPage(sc scanner) (StatusPage, error) {
	var (
		p        StatusPage
		sections string
		domain   sql.NullString
		pw       sql.NullString
	)
	err := sc.Scan(&p.ID, &p.Slug, &p.Title, &p.Description, &p.Footer, &sections, &domain, &pw,
		&p.ShowTargets, &p.Published, &p.HasLogo, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return p, err
	}
	p.CustomDomain = domain.String
	p.PasswordHash = pw.String
	p.HasPassword = pw.String != ""
	if json.Unmarshal([]byte(sections), &p.Sections) != nil || p.Sections == nil {
		p.Sections = []PageSection{}
	}
	return p, nil
}

func (s *Store) getPage(ctx context.Context, where string, arg any) (StatusPage, error) {
	p, err := scanPage(s.db.QueryRowContext(ctx, "SELECT "+pageCols+" FROM status_pages WHERE "+where, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) GetPage(ctx context.Context, id int64) (StatusPage, error) {
	return s.getPage(ctx, "id = ?", id)
}

func (s *Store) PageBySlug(ctx context.Context, slug string) (StatusPage, error) {
	return s.getPage(ctx, "slug = ?", slug)
}

func (s *Store) PageByDomain(ctx context.Context, domain string) (StatusPage, error) {
	return s.getPage(ctx, "custom_domain = ?", strings.ToLower(domain))
}

func (s *Store) ListPages(ctx context.Context) ([]StatusPage, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+pageCols+" FROM status_pages ORDER BY title COLLATE NOCASE, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StatusPage{}
	for rows.Next() {
		p, err := scanPage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CustomDomains özel alan adı → sayfa kimliği eşlemesi (istek yönlendirmesi için).
func (s *Store) CustomDomains(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT custom_domain, id FROM status_pages WHERE custom_domain IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var d string
		var id int64
		if err := rows.Scan(&d, &id); err != nil {
			return nil, err
		}
		out[d] = id
	}
	return out, rows.Err()
}

func (s *Store) CreatePage(ctx context.Context, p *StatusPage) error {
	now := time.Now().Unix()
	p.CreatedAt, p.UpdatedAt = now, now
	sections, _ := json.Marshal(p.Sections)
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO status_pages (slug, title, description, footer, sections, custom_domain,
			password_hash, show_targets, published, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.Slug, p.Title, p.Description, p.Footer, string(sections), nullStr(p.CustomDomain),
		nullStr(p.PasswordHash), boolInt(p.ShowTargets), boolInt(p.Published), now, now)
	if err != nil {
		return err
	}
	p.ID, err = res.LastInsertId()
	return err
}

// UpdatePage logo dışındaki alanları günceller (PasswordHash dahil; çağıran
// eskisini korumak istiyorsa aynı değeri verir).
func (s *Store) UpdatePage(ctx context.Context, p *StatusPage) error {
	p.UpdatedAt = time.Now().Unix()
	sections, _ := json.Marshal(p.Sections)
	res, err := s.db.ExecContext(ctx, `
		UPDATE status_pages SET slug = ?, title = ?, description = ?, footer = ?, sections = ?,
			custom_domain = ?, password_hash = ?, show_targets = ?, published = ?, updated_at = ?
		WHERE id = ?`,
		p.Slug, p.Title, p.Description, p.Footer, string(sections), nullStr(p.CustomDomain),
		nullStr(p.PasswordHash), boolInt(p.ShowTargets), boolInt(p.Published), p.UpdatedAt, p.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeletePage(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM status_pages WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetPageLogo logoyu yazar; data nil ise logo kaldırılır.
func (s *Store) SetPageLogo(ctx context.Context, id int64, data []byte, contentType string) error {
	var logo any
	if data != nil {
		logo = data
	}
	res, err := s.db.ExecContext(ctx,
		"UPDATE status_pages SET logo = ?, logo_type = ?, updated_at = ? WHERE id = ?",
		logo, contentType, time.Now().Unix(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) PageLogo(ctx context.Context, id int64) ([]byte, string, error) {
	var data []byte
	var typ string
	err := s.db.QueryRowContext(ctx, "SELECT logo, logo_type FROM status_pages WHERE id = ? AND logo IS NOT NULL", id).Scan(&data, &typ)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, typ, err
}

// Duyurular ------------------------------------------------------------------------

type Announcement struct {
	ID        int64  `json:"id"`
	PageID    int64  `json:"page_id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Severity  string `json:"severity"` // info | warning | danger | success
	StartsAt  int64  `json:"starts_at"`
	EndsAt    int64  `json:"ends_at"` // 0: süresiz
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

const announcementCols = "id, page_id, title, body, severity, starts_at, ends_at, created_at, updated_at"

func scanAnnouncement(sc scanner) (Announcement, error) {
	var a Announcement
	var ends sql.NullInt64
	err := sc.Scan(&a.ID, &a.PageID, &a.Title, &a.Body, &a.Severity, &a.StartsAt, &ends, &a.CreatedAt, &a.UpdatedAt)
	a.EndsAt = ends.Int64
	return a, err
}

func (s *Store) queryAnnouncements(ctx context.Context, q string, args ...any) ([]Announcement, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Announcement{}
	for rows.Next() {
		a, err := scanAnnouncement(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListAnnouncements(ctx context.Context, pageID int64) ([]Announcement, error) {
	return s.queryAnnouncements(ctx,
		"SELECT "+announcementCols+" FROM announcements WHERE page_id = ? ORDER BY starts_at DESC, id DESC", pageID)
}

// ActiveAnnouncements şu an yayında olan duyurular.
func (s *Store) ActiveAnnouncements(ctx context.Context, pageID, now int64) ([]Announcement, error) {
	return s.queryAnnouncements(ctx, `
		SELECT `+announcementCols+` FROM announcements
		WHERE page_id = ? AND starts_at <= ? AND (ends_at IS NULL OR ends_at > ?)
		ORDER BY starts_at DESC, id DESC`, pageID, now, now)
}

func (s *Store) GetAnnouncement(ctx context.Context, id int64) (Announcement, error) {
	a, err := scanAnnouncement(s.db.QueryRowContext(ctx, "SELECT "+announcementCols+" FROM announcements WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateAnnouncement(ctx context.Context, a *Announcement) error {
	now := time.Now().Unix()
	a.CreatedAt, a.UpdatedAt = now, now
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO announcements (page_id, title, body, severity, starts_at, ends_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.PageID, a.Title, a.Body, a.Severity, a.StartsAt, nullInt(a.EndsAt), now, now)
	if err != nil {
		return err
	}
	a.ID, err = res.LastInsertId()
	return err
}

func (s *Store) UpdateAnnouncement(ctx context.Context, a *Announcement) error {
	a.UpdatedAt = time.Now().Unix()
	res, err := s.db.ExecContext(ctx, `
		UPDATE announcements SET title = ?, body = ?, severity = ?, starts_at = ?, ends_at = ?, updated_at = ?
		WHERE id = ?`, a.Title, a.Body, a.Severity, a.StartsAt, nullInt(a.EndsAt), a.UpdatedAt, a.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteAnnouncement(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "DELETE FROM announcements WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Herkese açık sayfa verileri --------------------------------------------------------

// MonitorsByIDs verilen monitörleri kimliğe göre döner (olmayanlar atlanır).
func (s *Store) MonitorsByIDs(ctx context.Context, ids []int64) (map[int64]Monitor, error) {
	out := map[int64]Monitor{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, "SELECT "+monitorCols+" FROM monitors WHERE id IN ("+q+")", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out[m.ID] = m
	}
	return out, rows.Err()
}

// DailyFor verilen monitörlerin since'den itibaren günlük özetleri.
func (s *Store) DailyFor(ctx context.Context, ids []int64, since int64) (map[int64][]Bucket, error) {
	out := map[int64][]Bucket{}
	if len(ids) == 0 {
		return out, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+bucketCols+" FROM stats_daily WHERE monitor_id IN ("+q+") AND bucket >= ? ORDER BY monitor_id, bucket",
		append(args, since)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		mid, b, err := scanBucket(rows)
		if err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], b)
	}
	return out, rows.Err()
}

// IncidentsFor verilen monitörlerin since'den sonra başlamış veya hâlâ süren olayları.
func (s *Store) IncidentsFor(ctx context.Context, ids []int64, since int64, limit int) ([]Incident, error) {
	if len(ids) == 0 {
		return []Incident{}, nil
	}
	q, args := inClause(ids)
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause
		FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		WHERE i.monitor_id IN (`+q+`) AND (i.started_at >= ? OR i.resolved_at IS NULL)
		ORDER BY i.started_at DESC LIMIT ?`, append(args, since, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Incident{}
	for rows.Next() {
		var in Incident
		var resolved sql.NullInt64
		if err := rows.Scan(&in.ID, &in.MonitorID, &in.MonitorName, &in.StartedAt, &resolved, &in.Cause); err != nil {
			return nil, err
		}
		in.ResolvedAt = resolved.Int64
		out = append(out, in)
	}
	return out, rows.Err()
}

func inClause(ids []int64) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return strings.TrimSuffix(strings.Repeat("?,", len(ids)), ","), args
}
