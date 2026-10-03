package store

import (
	"context"
	"encoding/json"
	"time"
)

// İçe aktarma: doğrulanmış verinin tek işlemde (transaction) yazılması. Bir
// kayıt bile yazılamazsa hiçbir şey değişmez.

// ImportRef yeni veya mevcut bir kayda başvuru: New >= 0 ise ImportData'daki
// yeni kaydın sırası, aksi halde ID mevcut kaydın kimliği.
type ImportRef struct {
	New int
	ID  int64
}

// Existing mevcut kayda başvuru.
func Existing(id int64) ImportRef { return ImportRef{New: -1, ID: id} }

// NewRef bu içe aktarmada oluşturulan kayda başvuru.
func NewRef(i int) ImportRef { return ImportRef{New: i} }

type ImportTagRef struct {
	Tag   ImportRef
	Value string
}

type ImportMonitor struct {
	FileID        int64 // dosyadaki kimlik (durum sayfaları ve gruplar buna başvurur)
	Monitor       Monitor
	Notifications []ImportRef
	Tags          []ImportTagRef
}

// ImportNotification kanal ve etiket kuralları (etiketler ImportRef ile).
type ImportNotification struct {
	Notification Notification
	TagRules     []ImportTagRef
}

// ImportPage bölümlerindeki monitör kimlikleri dosya kimlikleridir.
// SectionTags: bölüm sırası → etiket kuralı (etikete bağlı gruplar).
type ImportPage struct {
	Page          StatusPage
	Logo          []byte
	LogoType      string
	Announcements []Announcement
	SectionTags   map[int]ImportTagRef
}

type ImportData struct {
	Replace       bool         // önce mevcut monitör, bildirim, etiket ve durum sayfalarını sil
	Settings      *AppSettings // nil değilse kaydedilir
	Tags          []Tag
	Notifications []Notification
	// NotificationTags Notifications ile aynı sırada etiket kuralları (yoksa nil).
	NotificationTags [][]ImportTagRef
	Monitors         []ImportMonitor
	Pages            []ImportPage
	// KnownMonitors dosya kimliği → zaten var olan monitör (birleştirmede
	// kopya olduğu için eklenmeyenler); durum sayfaları ve gruplar bunlara bağlanır.
	KnownMonitors map[int64]int64
	// RemapConfig başka monitörlere başvuran ayarları (ör. grup) yeni
	// kimliklere çevirir; değişiklik yoksa false döner.
	RemapConfig func(typ string, cfg json.RawMessage, ids map[int64]int64) (json.RawMessage, bool)
}

type ImportResult struct {
	MonitorIDs []int64         // oluşturulan monitörler (Monitors ile aynı sırada)
	IDMap      map[int64]int64 // dosya kimliği → veritabanı kimliği
	PageIDs    []int64
}

// ImportCounts mevcut kayıt sayıları (değiştir modunda silinecekler).
type ImportCounts struct {
	Monitors, Notifications, Tags, StatusPages int
}

func (s *Store) CountForImport(ctx context.Context) (ImportCounts, error) {
	var c ImportCounts
	for _, q := range []struct {
		table string
		dst   *int
	}{{"monitors", &c.Monitors}, {"notifications", &c.Notifications}, {"tags", &c.Tags}, {"status_pages", &c.StatusPages}} {
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+q.table).Scan(q.dst); err != nil {
			return c, err
		}
	}
	return c, nil
}

func (s *Store) Import(ctx context.Context, d *ImportData) (ImportResult, error) {
	res := ImportResult{IDMap: map[int64]int64{}}
	now := time.Now().Unix()
	err := s.tx(ctx, func(tx *Tx) error {
		if d.Replace {
			// Sıra önemli değil (ON DELETE CASCADE), ama sayfalar monitörlere
			// JSON içinde başvurduğu için önce silinir.
			for _, q := range []string{"DELETE FROM status_pages", "DELETE FROM monitors", "DELETE FROM notifications", "DELETE FROM tags"} {
				if _, err := tx.ExecContext(ctx, q); err != nil {
					return err
				}
			}
		}
		if d.Settings != nil {
			b, err := json.Marshal(d.Settings)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
				appSettingsKey, string(b)); err != nil {
				return err
			}
		}

		tagIDs := make([]int64, len(d.Tags))
		for i, t := range d.Tags {
			id, err := insertID(ctx, tx,
				"INSERT INTO tags (name, name_key, color, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
				t.Name, TagKey(t.Name), t.Color, now, now)
			if err != nil {
				return err
			}
			tagIDs[i] = id
		}
		notifIDs := make([]int64, len(d.Notifications))
		for i, n := range d.Notifications {
			id, err := insertID(ctx, tx, `
				INSERT INTO notifications (name, type, config, is_default, active, created_at, updated_at,
					events, quiet_hours, delay_min, escalate_min, lang)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				n.Name, n.Type, string(n.Config), boolInt(n.IsDefault), boolInt(n.Active), now, now,
				encodeEvents(n.Events), encodeQuiet(n.Quiet), n.DelayMin, n.EscalateMin, n.Lang)
			if err != nil {
				return err
			}
			notifIDs[i] = id
		}
		resolve := func(r ImportRef, ids []int64) int64 {
			if r.New >= 0 {
				return ids[r.New]
			}
			return r.ID
		}
		for i, rules := range d.NotificationTags {
			if i >= len(notifIDs) || len(rules) == 0 {
				continue
			}
			tr := make([]TagRule, 0, len(rules))
			for _, r := range rules {
				tr = append(tr, TagRule{TagID: resolve(r.Tag, tagIDs), Value: r.Value})
			}
			if err := setTagRulesTx(ctx, tx, "notification_tags", "notification_id", notifIDs[i], tr); err != nil {
				return err
			}
		}

		for fileID, id := range d.KnownMonitors {
			res.IDMap[fileID] = id
		}
		res.MonitorIDs = make([]int64, len(d.Monitors))
		for i, im := range d.Monitors {
			m := im.Monitor
			id, err := insertID(ctx, tx, `
				INSERT INTO monitors (name, type, description, active, interval_sec, retry_interval_sec,
					max_retries, timeout_sec, resend_every, upside_down, config, push_token, status,
					created_at, updated_at, slow_ms, slow_checks, domain_expiry)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				m.Name, m.Type, m.Description, boolInt(m.Active), m.Interval, m.RetryInterval,
				m.MaxRetries, m.Timeout, m.ResendEvery, boolInt(m.UpsideDown), string(m.Config),
				nullStr(m.PushToken), StatusPending, now, now, m.SlowMs, slowChecksOr(m.SlowChecks), boolInt(m.DomainExpiry))
			if err != nil {
				return err
			}
			res.MonitorIDs[i] = id
			if im.FileID != 0 {
				res.IDMap[im.FileID] = id
			}
			links := make([]int64, 0, len(im.Notifications))
			for _, r := range im.Notifications {
				links = append(links, resolve(r, notifIDs))
			}
			if err := setMonitorNotifications(ctx, tx, id, links); err != nil {
				return err
			}
			tags := make([]MonitorTagInput, 0, len(im.Tags))
			for _, t := range im.Tags {
				tags = append(tags, MonitorTagInput{TagID: resolve(t.Tag, tagIDs), Value: t.Value})
			}
			if err := setMonitorTagsTx(ctx, tx, id, tags); err != nil {
				return err
			}
		}
		if d.RemapConfig != nil {
			for i, im := range d.Monitors {
				cfg, changed := d.RemapConfig(im.Monitor.Type, im.Monitor.Config, res.IDMap)
				if !changed {
					continue
				}
				if _, err := tx.ExecContext(ctx, "UPDATE monitors SET config = ? WHERE id = ?", string(cfg), res.MonitorIDs[i]); err != nil {
					return err
				}
			}
		}

		for _, ip := range d.Pages {
			p := ip.Page
			sections := make([]PageSection, 0, len(p.Sections))
			for si, sec := range p.Sections {
				ms := make([]PageMonitor, 0, len(sec.Monitors))
				for _, pm := range sec.Monitors {
					if id, ok := res.IDMap[pm.ID]; ok {
						ms = append(ms, PageMonitor{ID: id, Name: pm.Name})
					}
				}
				ns := PageSection{Title: sec.Title, Monitors: ms}
				if ref, ok := ip.SectionTags[si]; ok {
					ns.TagID, ns.TagValue = resolve(ref.Tag, tagIDs), ref.Value
				}
				sections = append(sections, ns)
			}
			sj, _ := json.Marshal(sections)
			var logo any
			if len(ip.Logo) > 0 {
				logo = ip.Logo
			}
			id, err := insertID(ctx, tx, `
				INSERT INTO status_pages (slug, title, description, footer, sections, custom_domain,
					password_hash, show_targets, published, logo, logo_type, created_at, updated_at, bar_range,
					show_incidents, collapsible, lang, layout, incident_days, uptime_windows)
				VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
				p.Slug, p.Title, p.Description, p.Footer, string(sj), nullStr(p.CustomDomain),
				nullStr(p.PasswordHash), boolInt(p.ShowTargets), boolInt(p.Published), logo, ip.LogoType, now, now,
				barRangeOr(p.BarRange), boolInt(p.ShowIncidents), boolInt(p.Collapsible), pageLangOr(p.Lang), encodeLayout(p.Layout),
				incidentDaysOr(p.IncidentDays), encodeWindows(p.UptimeWindows))
			if err != nil {
				return err
			}
			res.PageIDs = append(res.PageIDs, id)
			for _, a := range ip.Announcements {
				if _, err := tx.ExecContext(ctx, `
					INSERT INTO announcements (page_id, title, body, severity, starts_at, ends_at, created_at, updated_at)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
					id, a.Title, a.Body, a.Severity, a.StartsAt, nullInt(a.EndsAt), now, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
	return res, err
}
