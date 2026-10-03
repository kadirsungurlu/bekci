package store

import (
	"context"
	"errors"
	"strings"
)

// 30: etiket tabanlı kurallar (E-12).
//
//   - notification_tags: bildirim kanalı "şu etiketi (= değeri) taşıyan her
//     monitör için" gönderir. Monitörün açık bağlantılarına (monitor_notifications)
//     EK olarak uygulanır (birleşim): etiketi alan yeni monitör kanalı
//     kendiliğinden kazanır, açık bağlantı hiçbir zaman etiketle silinmez.
//   - user_tags: kısıtlı (müşteri) izleyici, açıkça seçilen monitörlere EK
//     olarak bu etiketi taşıyan monitörleri de görür.
//   - Durum sayfası grupları: sections JSON'undaki tag_id/tag_value alanı;
//     grup o etiketi taşıyan monitörlerle kendiliğinden dolar (bkz. resolveTagSections).
//
// value ” = etiketin her değeri. Etiket silinince kural da silinir (CASCADE).
func init() {
	RegisterMigration(30, `
CREATE TABLE notification_tags (
	notification_id INTEGER NOT NULL REFERENCES notifications(id) ON DELETE CASCADE,
	tag_id          INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	value           TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (notification_id, tag_id, value)
) WITHOUT ROWID;
CREATE INDEX notification_tags_tag ON notification_tags(tag_id);

CREATE TABLE user_tags (
	user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	tag_id  INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	value   TEXT    NOT NULL DEFAULT '',
	PRIMARY KEY (user_id, tag_id, value)
) WITHOUT ROWID;
CREATE INDEX user_tags_tag ON user_tags(tag_id);
`)
}

// TagRule "etiket (= değer)" kuralı. Name ve Color yalnızca okumada doldurulur.
type TagRule struct {
	TagID int64  `json:"tag_id"`
	Value string `json:"value"`
	Name  string `json:"name,omitempty"`
	Color string `json:"color,omitempty"`
}

// tagRuleCond monitor_tags mt satırının kural tablosu r ile eşleşme koşulu.
const tagRuleCond = "mt.tag_id = r.tag_id AND (r.value = '' OR r.value = mt.value)"

// loadTagRules verilen tablodaki (notification_tags | user_tags) kuralları
// sahip kimliğine göre gruplar (etiket adına göre sıralı).
func (s *Store) loadTagRules(ctx context.Context, table, ownerCol string, owners []int64) (map[int64][]TagRule, error) {
	out := map[int64][]TagRule{}
	if len(owners) == 0 {
		return out, nil
	}
	in, args := inClause(owners)
	rows, err := s.db.QueryContext(ctx, `
		SELECT r.`+ownerCol+`, r.tag_id, r.value, t.name, t.color FROM `+table+` r JOIN tags t ON t.id = r.tag_id
		WHERE r.`+ownerCol+` IN (`+in+`) ORDER BY t.name_key, t.id, r.value`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var owner int64
		var r TagRule
		if err := rows.Scan(&owner, &r.TagID, &r.Value, &r.Name, &r.Color); err != nil {
			return nil, err
		}
		out[owner] = append(out[owner], r)
	}
	return out, rows.Err()
}

// setTagRulesTx sahibin kurallarını verilen listeyle değiştirir.
func setTagRulesTx(ctx context.Context, tx *Tx, table, ownerCol string, owner int64, rules []TagRule) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE "+ownerCol+" = ?", owner); err != nil {
		return err
	}
	for _, r := range rules {
		if r.TagID <= 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO "+table+" ("+ownerCol+", tag_id, value) VALUES (?, ?, ?) ON CONFLICT DO NOTHING",
			owner, r.TagID, strings.TrimSpace(r.Value)); err != nil {
			return err
		}
	}
	return nil
}

// MonitorsMatchingTags verilen etiket kurallarından en az birine uyan
// monitörlerin kimlikleri (ada göre sıralı). Boş kural listesi → boş sonuç.
func (s *Store) MonitorsMatchingTags(ctx context.Context, rules []TagRule) ([]int64, error) {
	out := []int64{}
	if len(rules) == 0 {
		return out, nil
	}
	var or []string
	var args []any
	for _, r := range rules {
		if r.TagID <= 0 {
			continue
		}
		if v := strings.TrimSpace(r.Value); v != "" {
			or = append(or, "(mt.tag_id = ? AND mt.value = ?)")
			args = append(args, r.TagID, v)
		} else {
			or = append(or, "mt.tag_id = ?")
			args = append(args, r.TagID)
		}
	}
	if len(or) == 0 {
		return out, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT m.id, LOWER(m.name) FROM monitor_tags mt JOIN monitors m ON m.id = mt.monitor_id
		WHERE `+strings.Join(or, " OR ")+` ORDER BY LOWER(m.name), m.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// Bildirim kanalları --------------------------------------------------------------

// attachNotificationTags kanal listesine etiket kurallarını ekler.
func (s *Store) attachNotificationTags(ctx context.Context, list []Notification) error {
	ids := make([]int64, len(list))
	for i, n := range list {
		ids[i] = n.ID
	}
	rules, err := s.loadTagRules(ctx, "notification_tags", "notification_id", ids)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].TagRules = rules[list[i].ID]
		if list[i].TagRules == nil {
			list[i].TagRules = []TagRule{}
		}
	}
	return nil
}

// TagNotificationIDs monitör → etiket kuralıyla bağlanan kanallar (açık
// bağlantılar hariç; arayüzde "etiketle bağlı" gösterimi için).
func (s *Store) TagNotificationIDs(ctx context.Context) (map[int64][]int64, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT mt.monitor_id, r.notification_id FROM notification_tags r JOIN monitor_tags mt ON `+tagRuleCond+`
		ORDER BY mt.monitor_id, r.notification_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var mid, nid int64
		if err := rows.Scan(&mid, &nid); err != nil {
			return nil, err
		}
		out[mid] = append(out[mid], nid)
	}
	return out, rows.Err()
}

// Kullanıcılar -----------------------------------------------------------------------

// userTagRules kısıtlı kullanıcının etiket kuralları.
func (s *Store) userTagRules(ctx context.Context, userID int64) ([]TagRule, error) {
	rules, err := s.loadTagRules(ctx, "user_tags", "user_id", []int64{userID})
	if err != nil {
		return nil, err
	}
	if rules[userID] == nil {
		return []TagRule{}, nil
	}
	return rules[userID], nil
}

// effectiveMonitorIDs açık seçim + etiket kurallarının birleşimi (sıralı, tekil).
func (s *Store) effectiveMonitorIDs(ctx context.Context, picked []int64, rules []TagRule) ([]int64, error) {
	byTag, err := s.MonitorsMatchingTags(ctx, rules)
	if err != nil {
		return nil, err
	}
	seen := make(map[int64]bool, len(picked)+len(byTag))
	out := make([]int64, 0, len(picked)+len(byTag))
	for _, id := range picked {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, id := range byTag {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// Durum sayfaları --------------------------------------------------------------------

// resolveTagSections etikete bağlı grupları (TagID > 0) o etiketi taşıyan
// monitörlerle doldurur: açıkça eklenen monitörler (özel adlarıyla) önce,
// ardından etiketle gelenler (Auto) ada göre. Sayfanın başka bir grubunda
// açıkça listelenen monitör etiketle ikinci kez eklenmez.
func (s *Store) resolveTagSections(ctx context.Context, p *StatusPage) error {
	hasTag := false
	explicit := map[int64]bool{}
	for _, sec := range p.Sections {
		if sec.TagID > 0 {
			hasTag = true
		}
		for _, m := range sec.Monitors {
			if !m.Auto {
				explicit[m.ID] = true
			}
		}
	}
	if !hasTag {
		return nil
	}
	for i := range p.Sections {
		sec := &p.Sections[i]
		if sec.TagID <= 0 {
			continue
		}
		mons := make([]PageMonitor, 0, len(sec.Monitors))
		for _, m := range sec.Monitors {
			if !m.Auto {
				mons = append(mons, m)
			}
		}
		if _, err := s.GetTag(ctx, sec.TagID); err != nil {
			if !errors.Is(err, ErrNotFound) {
				return err
			}
			// Etiket silinmiş: grup sıradan gruba döner (bağ arayüzde görünmez).
			sec.TagID, sec.TagValue, sec.Monitors = 0, "", mons
			continue
		}
		ids, err := s.MonitorsMatchingTags(ctx, []TagRule{{TagID: sec.TagID, Value: sec.TagValue}})
		if err != nil {
			return err
		}
		for _, id := range ids {
			if !explicit[id] {
				explicit[id] = true // aynı monitör iki etiket grubuna da girmesin
				mons = append(mons, PageMonitor{ID: id, Auto: true})
			}
		}
		sec.Monitors = mons
	}
	return nil
}

// ExplicitSections etiketle gelen (Auto) monitörleri çıkarır: kayıt ve
// yedek için saklanan hal (etiket kuralı kalır, monitörler yeniden hesaplanır).
func ExplicitSections(sections []PageSection) []PageSection {
	out := make([]PageSection, len(sections))
	for i, sec := range sections {
		mons := make([]PageMonitor, 0, len(sec.Monitors))
		for _, m := range sec.Monitors {
			if !m.Auto {
				mons = append(mons, m)
			}
		}
		out[i] = PageSection{Title: sec.Title, Monitors: mons, TagID: sec.TagID, TagValue: sec.TagValue}
	}
	return out
}
