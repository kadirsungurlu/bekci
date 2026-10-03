package store

import (
	"context"
	"encoding/json"
)

// Değiştir modunda (yedekten geri yükleme) monitör, bildirim, etiket ve durum
// sayfaları silinip yeniden oluşturulur. Silinmeyen ama bunlara kimlikle bağlı
// kayıtlar (kısıtlı kullanıcıların monitör/etiket seçimi, konum ayarları,
// sunucu ve kontrol noktası bildirim bağları, bakım pencerelerinin monitörleri,
// elle açılan olayların durum sayfası, sistem e-posta kanalı) ON DELETE CASCADE ile sessizce kaybolurdu. Bu dosya
// silmeden önce bu bağları ADA göre saklar ve aynı adlı kayıtlar yeniden
// oluşturulunca geri yazar.

type replaceSnapshot struct {
	userMonitors map[int64][]string    // kullanıcı → monitör adları
	userTags     map[int64][]namedRule // kullanıcı → (etiket adı, değer)
	locations    map[string]locSnap    // monitör adı → konum ayarı
	probeNotifs  map[int64][]namedRule // ajan → (kanal adı, seviye)
	maintMons    map[int64][]string    // bakım → monitör adları
	incidentPage map[int64]string      // elle açılan olay → durum sayfası adresi (slug)
	systemMail   string                // sistem e-posta kanalının adı
}

type namedRule struct {
	Name, Value string
}

type locSnap struct {
	IncludeLocal, NotifyPartial bool
	DownWhen                    string
	ProbeIDs                    []int64
}

// snapshotForReplace silinecek kayıtlara bağlı verileri ada göre okur.
func snapshotForReplace(ctx context.Context, tx *Tx) (*replaceSnapshot, error) {
	rs := &replaceSnapshot{
		userMonitors: map[int64][]string{}, userTags: map[int64][]namedRule{},
		locations: map[string]locSnap{}, probeNotifs: map[int64][]namedRule{}, maintMons: map[int64][]string{},
		incidentPage: map[int64]string{},
	}
	collect := func(query string, fn func(scan func(dest ...any) error) error) error {
		rows, err := tx.QueryContext(ctx, query)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows.Scan); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := collect(`SELECT um.user_id, m.name FROM user_monitors um JOIN monitors m ON m.id = um.monitor_id`,
		func(scan func(...any) error) error {
			var uid int64
			var name string
			if err := scan(&uid, &name); err != nil {
				return err
			}
			rs.userMonitors[uid] = append(rs.userMonitors[uid], name)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT ut.user_id, t.name, ut.value FROM user_tags ut JOIN tags t ON t.id = ut.tag_id`,
		func(scan func(...any) error) error {
			var uid int64
			var r namedRule
			if err := scan(&uid, &r.Name, &r.Value); err != nil {
				return err
			}
			rs.userTags[uid] = append(rs.userTags[uid], r)
			return nil
		}); err != nil {
		return nil, err
	}
	// Aynı adlı monitörlerden ilki (en küçük kimlik) esas alınır.
	if err := collect(`SELECT m.name, s.include_local, s.down_when, s.notify_partial, s.monitor_id
		FROM monitor_location_settings s JOIN monitors m ON m.id = s.monitor_id ORDER BY s.monitor_id`,
		func(scan func(...any) error) error {
			var name string
			var l locSnap
			var id int64
			if err := scan(&name, &l.IncludeLocal, &l.DownWhen, &l.NotifyPartial, &id); err != nil {
				return err
			}
			if _, dup := rs.locations[name]; !dup {
				l.ProbeIDs = []int64{}
				rs.locations[name] = l
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT m.name, l.probe_id, l.monitor_id FROM monitor_locations l JOIN monitors m ON m.id = l.monitor_id ORDER BY l.monitor_id, l.probe_id`,
		func(scan func(...any) error) error {
			var name string
			var pid, mid int64
			if err := scan(&name, &pid, &mid); err != nil {
				return err
			}
			if l, ok := rs.locations[name]; ok {
				l.ProbeIDs = append(l.ProbeIDs, pid)
				rs.locations[name] = l
			}
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT pn.probe_id, n.name, pn.level FROM probe_notifications pn JOIN notifications n ON n.id = pn.notification_id`,
		func(scan func(...any) error) error {
			var pid int64
			var r namedRule
			if err := scan(&pid, &r.Name, &r.Value); err != nil {
				return err
			}
			rs.probeNotifs[pid] = append(rs.probeNotifs[pid], r)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT mm.maintenance_id, m.name FROM maintenance_monitors mm JOIN monitors m ON m.id = mm.monitor_id`,
		func(scan func(...any) error) error {
			var mid int64
			var name string
			if err := scan(&mid, &name); err != nil {
				return err
			}
			rs.maintMons[mid] = append(rs.maintMons[mid], name)
			return nil
		}); err != nil {
		return nil, err
	}
	if err := collect(`SELECT i.id, p.slug FROM incidents i JOIN status_pages p ON p.id = i.page_id`,
		func(scan func(...any) error) error {
			var id int64
			var slug string
			if err := scan(&id, &slug); err != nil {
				return err
			}
			rs.incidentPage[id] = slug
			return nil
		}); err != nil {
		return nil, err
	}
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", appSettingsKey).Scan(&raw); err == nil {
		var a AppSettings
		if json.Unmarshal([]byte(raw), &a) == nil && a.SystemMailChannelID > 0 {
			tx.QueryRowContext(ctx, "SELECT name FROM notifications WHERE id = ?", a.SystemMailChannelID).Scan(&rs.systemMail)
		}
	}
	return rs, nil
}

// restore bağları yeni kimliklere yazar. monIDs / tagIDs / notifIDs ad → yeni
// kimlik (aynı addan birkaç kayıt varsa ilki). Dosyadan konum ayarı gelen
// monitörlere (zaten satırı olanlara) dokunulmaz.
func (rs *replaceSnapshot) restore(ctx context.Context, tx *Tx, monIDs, tagIDs, notifIDs, pageIDs map[string]int64) error {
	for incID, slug := range rs.incidentPage {
		if id, ok := pageIDs[slug]; ok {
			if _, err := tx.ExecContext(ctx, "UPDATE incidents SET page_id = ? WHERE id = ?", id, incID); err != nil {
				return err
			}
		}
	}
	for uid, names := range rs.userMonitors {
		for _, n := range names {
			if id, ok := monIDs[n]; ok {
				if _, err := tx.ExecContext(ctx, "INSERT INTO user_monitors (user_id, monitor_id) VALUES (?, ?) ON CONFLICT DO NOTHING", uid, id); err != nil {
					return err
				}
			}
		}
	}
	for uid, rules := range rs.userTags {
		for _, r := range rules {
			if id, ok := tagIDs[r.Name]; ok {
				if _, err := tx.ExecContext(ctx, "INSERT INTO user_tags (user_id, tag_id, value) VALUES (?, ?, ?) ON CONFLICT DO NOTHING", uid, id, r.Value); err != nil {
					return err
				}
			}
		}
	}
	for name, l := range rs.locations {
		id, ok := monIDs[name]
		if !ok {
			continue
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM monitor_location_settings WHERE monitor_id = ?", id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			continue
		}
		if err := setMonitorLocationsTx(ctx, tx, id, LocationSetup{IncludeLocal: l.IncludeLocal, ProbeIDs: l.ProbeIDs, DownWhen: l.DownWhen, NotifyPartial: l.NotifyPartial}); err != nil {
			return err
		}
	}
	for pid, rules := range rs.probeNotifs {
		for _, r := range rules {
			if id, ok := notifIDs[r.Name]; ok {
				if _, err := tx.ExecContext(ctx, "INSERT INTO probe_notifications (probe_id, notification_id, level) VALUES (?, ?, ?) ON CONFLICT DO NOTHING", pid, id, r.Value); err != nil {
					return err
				}
			}
		}
	}
	for mid, names := range rs.maintMons {
		for _, n := range names {
			if id, ok := monIDs[n]; ok {
				if _, err := tx.ExecContext(ctx, "INSERT INTO maintenance_monitors (maintenance_id, monitor_id) VALUES (?, ?) ON CONFLICT DO NOTHING", mid, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// setSystemMailTx ayarlardaki sistem e-posta kanalını yazar (0: yok).
func setSystemMailTx(ctx context.Context, tx *Tx, id int64) error {
	var raw string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", appSettingsKey).Scan(&raw); err != nil {
		return nil // ayar satırı yok: varsayılanlar geçerli, kanal zaten 0
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return nil
	}
	m["system_mail_channel_id"] = id
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE settings SET value = ? WHERE key = ?", string(b), appSettingsKey)
	return err
}
