package api

// Yedekle / geri yükle: tüm yapılandırmanın (monitörler, bildirim kanalları,
// etiketler, durum sayfaları, ayarlar) gizli bilgiler dahil JSON olarak dışa
// aktarılması ve geri yüklenmesi; Uptime Kuma ve UptimeRobot'tan içe aktarma.
// Hepsi yalnızca yönetici içindir ve işlem kaydına yazılır.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/kadirsungurlu/bekci/internal/backup"
	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/engine"
	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Boyut ve adet sınırları.
const (
	maxBackupBytes       = 20 << 20  // JSON yedek dosyası
	maxKumaUploadBytes   = 200 << 20 // kuma.db
	maxImportMonitors    = 5000
	maxImportNotifs      = 1000
	maxImportTags        = 1000
	maxImportPages       = 100
	maxImportPageMons    = 200
	maxImportSections    = 20
	maxImportLogoBytes   = 512 << 10
	maxImportAnnounces   = 200
	maxImportUsers       = 500
	importUploadDeadline = 15 * time.Minute
)

var (
	importSlugRe     = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,48}[a-z0-9])?$`)
	importPushTokRe  = regexp.MustCompile(`^[A-Za-z0-9_-]{4,128}$`)
	importLogoTypes  = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true}
	importSeverities = map[string]bool{"info": true, "warning": true, "danger": true, "success": true}
)

// importMu aynı anda tek içe aktarma: iki geri yükleme iç içe geçmesin.
var importMu sync.Mutex

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/export", s.admin(s.exportBackup))
		mux.Handle("POST /api/export", s.admin(s.exportBackupWith))
		mux.Handle("POST /api/import", s.admin(s.importBackup))
		mux.Handle("POST /api/import/uptime-kuma", s.admin(s.importKuma))
		mux.Handle("POST /api/import/uptimerobot", s.admin(s.importUptimeRobot))
	})
}

// Dışa aktarma --------------------------------------------------------------------

// exportOptions POST /api/export gövdesi: kullanıcılar (şifre özetleriyle),
// 2FA sırları ve dosya şifresi (boş: düz JSON).
type exportOptions struct {
	Users     bool   `json:"users"`
	TwoFactor bool   `json:"two_factor"`
	Password  string `json:"password"`
}

func (s *Server) buildExport(ctx context.Context) (*backup.Doc, error) {
	return s.buildExportWith(ctx, exportOptions{})
}

func (s *Server) buildExportWith(ctx context.Context, opt exportOptions) (*backup.Doc, error) {
	doc := &backup.Doc{
		Format: backup.Format, Version: backup.Version, ExportedAt: s.now().Unix(), AppVersion: s.version,
		Warning: backup.SecretsWarning, Tags: []backup.Tag{}, Notifications: []backup.Notification{},
		Monitors: []backup.Monitor{}, StatusPages: []backup.Page{},
	}
	settings, err := s.store.LoadSettings(ctx)
	if err != nil {
		return nil, err
	}
	doc.Settings = &settings

	tags, err := s.store.ListTags(ctx)
	if err != nil {
		return nil, err
	}
	for _, t := range tags {
		doc.Tags = append(doc.Tags, backup.Tag{Name: t.Name, Color: t.Color})
	}

	notifs, err := s.store.ListNotifications(ctx)
	if err != nil {
		return nil, err
	}
	// Monitörler kanallara adıyla bağlanır; aynı adı taşıyan kanallar dosyada ayrıştırılır.
	used := map[string]bool{}
	notifName := map[int64]string{}
	for _, n := range notifs {
		name := backup.UniqueName(n.Name, used)
		notifName[n.ID] = name
		bn := backup.Notification{
			Name: name, Type: n.Type, Config: n.Config, IsDefault: n.IsDefault, Active: n.Active,
			Events: n.Events, QuietHours: n.Quiet, DelayMin: n.DelayMin, EscalateMin: n.EscalateMin, Lang: n.Lang,
		}
		for _, r := range n.TagRules {
			bn.TagRules = append(bn.TagRules, backup.TagRule{Name: r.Name, Value: r.Value})
		}
		doc.Notifications = append(doc.Notifications, bn)
	}
	tagName := map[int64]string{}
	for _, t := range tags {
		tagName[t.ID] = t.Name
	}

	links, err := s.store.MonitorNotificationIDs(ctx)
	if err != nil {
		return nil, err
	}
	mtags, err := s.store.MonitorTags(ctx)
	if err != nil {
		return nil, err
	}
	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		return nil, err
	}
	locations, err := s.store.AllMonitorLocations(ctx)
	if err != nil {
		return nil, err
	}
	probeName := map[int64]string{}
	if probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindLocation); err == nil {
		for _, p := range probes {
			probeName[p.ID] = p.Name
		}
	}
	if n, ok := notifName[settings.SystemMailChannelID]; ok {
		doc.SystemMailChannel = n
	}
	for _, m := range monitors {
		bm := backup.Monitor{
			ID: m.ID, Name: m.Name, Type: m.Type, Description: m.Description, Active: m.Active,
			Interval: m.Interval, RetryInterval: m.RetryInterval, MaxRetries: m.MaxRetries, Timeout: m.Timeout,
			ResendEvery: m.ResendEvery, UpsideDown: m.UpsideDown, Config: m.Config, PushToken: m.PushToken,
			SlowMs: m.SlowMs, SlowChecks: m.SlowChecks,
			Notifications: []string{}, Tags: []backup.MonitorTag{},
		}
		if !m.DomainExpiry {
			off := false
			bm.DomainExpiry = &off
		}
		for _, nid := range links[m.ID] {
			if n, ok := notifName[nid]; ok {
				bm.Notifications = append(bm.Notifications, n)
			}
		}
		for _, t := range mtags[m.ID] {
			bm.Tags = append(bm.Tags, backup.MonitorTag{Name: t.Name, Value: t.Value})
		}
		if l, ok := locations[m.ID]; ok && l.Configured() {
			ml := &backup.MonitorLocations{IncludeLocal: l.IncludeLocal, Probes: []string{}, DownWhen: l.DownWhen, NotifyPartial: l.NotifyPartial}
			for _, pid := range l.ProbeIDs {
				if n, ok := probeName[pid]; ok {
					ml.Probes = append(ml.Probes, n)
				}
			}
			bm.Locations = ml
		}
		doc.Monitors = append(doc.Monitors, bm)
	}

	pages, err := s.store.ListPages(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		bp := backup.Page{
			Slug: p.Slug, Title: p.Title, Description: p.Description, Footer: p.Footer,
			CustomDomain: p.CustomDomain, PasswordHash: p.PasswordHash, ShowTargets: p.ShowTargets,
			BarRange: p.BarRange, ShowIncidents: &p.ShowIncidents, Collapsible: p.Collapsible, Lang: p.Lang, Published: p.Published, Sections: []backup.PageSection{}, Announcements: []backup.Announcement{},
			IncidentDays: p.IncidentDays, UptimeWindows: p.UptimeWindows,
		}
		showUptime := p.Layout.UptimeShown()
		bl := backup.PageLayout{Style: p.Layout.Style, Width: p.Layout.Width, Blocks: []backup.PageBlock{}, ShowUptime: &showUptime}
		for _, b := range p.Layout.Blocks {
			bl.Blocks = append(bl.Blocks, backup.PageBlock{ID: b.ID, Visible: b.Visible})
		}
		bp.Layout = &bl
		for _, sec := range p.Sections {
			bs := backup.PageSection{Title: sec.Title, Monitors: []backup.PageMonitor{}}
			if sec.TagID > 0 {
				if name, ok := tagName[sec.TagID]; ok {
					bs.Tag = &backup.TagRule{Name: name, Value: sec.TagValue}
				}
			}
			for _, pm := range sec.Monitors {
				if pm.Auto {
					continue // etiketle gelen; geri yüklenirken yeniden hesaplanır
				}
				bs.Monitors = append(bs.Monitors, backup.PageMonitor{MonitorID: pm.ID, Name: pm.Name})
			}
			bp.Sections = append(bp.Sections, bs)
		}
		if p.HasLogo {
			logo, typ, err := s.store.PageLogo(ctx, p.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			bp.Logo, bp.LogoType = logo, typ
		}
		anns, err := s.store.ListAnnouncements(ctx, p.ID)
		if err != nil {
			return nil, err
		}
		for _, a := range anns {
			bp.Announcements = append(bp.Announcements, backup.Announcement{
				Title: a.Title, Body: a.Body, Severity: a.Severity, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
			})
		}
		doc.StatusPages = append(doc.StatusPages, bp)
	}
	if opt.Users {
		users, err := s.store.UsersForBackup(ctx, opt.TwoFactor)
		if err != nil {
			return nil, err
		}
		doc.Users = []backup.User{}
		for _, ub := range users {
			u := ub.User
			bu := backup.User{Username: u.Username, DisplayName: u.DisplayName, Email: u.Email, Role: u.Role, Disabled: u.Disabled,
				MustChangePassword: u.MustChangePassword, AllMonitors: u.AllMonitors || u.Role != store.RoleViewer, Lang: u.Lang, Theme: u.Theme,
				PasswordHash: u.PasswordHash, TwoFactorEnabled: u.TwoFactorEnabled, TOTPSecret: ub.TOTPSecret, RecoveryCodes: ub.RecoveryCodes}
			if !bu.AllMonitors {
				bu.MonitorIDs = u.PickedMonitorIDs
				for _, r := range u.TagRules {
					bu.TagRules = append(bu.TagRules, backup.TagRule{Name: r.Name, Value: r.Value})
				}
			}
			doc.Users = append(doc.Users, bu)
		}
	}
	return doc, nil
}

// exportBackupWith: POST /api/export — seçeneklerle (kullanıcılar, 2FA
// sırları, dosya şifresi) yedek. Şifre verilirse dosya Argon2id + AES-256-GCM
// zarfıdır; geri yüklemede aynı şifre istenir.
func (s *Server) exportBackupWith(w http.ResponseWriter, r *http.Request) {
	if userFrom(r).APIKeyName != "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	var opt exportOptions
	if !readJSON(w, r, &opt) {
		return
	}
	if opt.Password != "" && utf8.RuneCountInString(opt.Password) < backup.MinPasswordLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("Yedek şifresi en az %d karakter olmalı", backup.MinPasswordLen))
		return
	}
	doc, err := s.buildExportWith(r.Context(), opt)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		s.dbError(w, err)
		return
	}
	data := buf.Bytes()
	name := "bekci-yedek-" + s.now().Format("20060102") + ".json"
	var notes []string
	if opt.Users {
		notes = append(notes, fmt.Sprintf("%d kullanıcı", len(doc.Users)))
		if opt.TwoFactor {
			notes = append(notes, "2FA sırları dahil")
		}
	}
	if opt.Password != "" {
		if data, err = backup.Encrypt(data, opt.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		name = "bekci-yedek-" + s.now().Format("20060102") + "-sifreli.json"
		notes = append(notes, "dosya şifreli")
	}
	detail := fmt.Sprintf("%d monitör, %d bildirim, %d etiket, %d durum sayfası", len(doc.Monitors), len(doc.Notifications), len(doc.Tags), len(doc.StatusPages))
	if len(notes) > 0 {
		detail += "; " + strings.Join(notes, ", ")
	}
	s.audit(r, store.User{}, "backup.export", "backup", 0, name, detail)
	s.log.Info("yedek dışa aktarıldı", "kullanıcı", userFrom(r).Username, "monitör", len(doc.Monitors), "kullanıcılar", opt.Users, "şifreli", opt.Password != "")
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}

// exportBackup: GET /api/export — gizli bilgiler dahil tam yedek (dosya olarak indirilir).
func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	doc, err := s.buildExport(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		s.dbError(w, err)
		return
	}
	name := "bekci-yedek-" + s.now().Format("20060102") + ".json"
	s.audit(r, store.User{}, "backup.export", "backup", 0, name, fmt.Sprintf("%d monitör, %d bildirim, %d etiket, %d durum sayfası",
		len(doc.Monitors), len(doc.Notifications), len(doc.Tags), len(doc.StatusPages)))
	s.log.Info("yedek dışa aktarıldı", "kullanıcı", userFrom(r).Username, "monitör", len(doc.Monitors))
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`)
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}

// İçe aktarma yanıtı -----------------------------------------------------------------

type importCounts struct {
	Monitors      int `json:"monitors"`
	Notifications int `json:"notifications"`
	Tags          int `json:"tags"`
	StatusPages   int `json:"status_pages"`
	Users         int `json:"users,omitempty"`
}

func (c *importCounts) add(kind string) {
	switch kind {
	case "monitor":
		c.Monitors++
	case "notification":
		c.Notifications++
	case "tag":
		c.Tags++
	case "status_page":
		c.StatusPages++
	case "user":
		c.Users++
	}
}

// importItem kayıt başına sonuç. Result: created (eklendi), existing (zaten
// vardı, eklenmedi), skipped (geçersiz/desteklenmiyor, atlandı).
type importItem struct {
	Kind     string   `json:"kind"` // monitor | notification | tag | status_page | settings
	Name     string   `json:"name"`
	Type     string   `json:"type,omitempty"`
	Result   string   `json:"result"`
	ID       int64    `json:"id,omitempty"` // oluşturulan monitörün kimliği
	Messages []string `json:"messages,omitempty"`
}

type importSummary struct {
	Source          string        `json:"source"` // bekci | uptime-kuma | uptimerobot
	Mode            string        `json:"mode"`   // merge | replace
	DryRun          bool          `json:"dry_run"`
	Created         importCounts  `json:"created"`
	Existing        importCounts  `json:"existing"`
	Skipped         importCounts  `json:"skipped"`
	Deleted         *importCounts `json:"deleted,omitempty"` // replace: silinen mevcut kayıtlar
	SettingsApplied bool          `json:"settings_applied"`
	Warnings        []string      `json:"warnings"`
	Items           []importItem  `json:"items"`
}

func (sum *importSummary) item(it importItem) int {
	switch it.Result {
	case "created":
		sum.Created.add(it.Kind)
	case "existing":
		sum.Existing.add(it.Kind)
	case "skipped":
		sum.Skipped.add(it.Kind)
	}
	sum.Items = append(sum.Items, it)
	return len(sum.Items) - 1
}

// importPlan doğrulanmış, yazılmaya hazır içe aktarma.
type importPlan struct {
	data         store.ImportData
	sum          importSummary
	monitorItems []int // data.Monitors[i] → sum.Items sırası
}

// importInputError kullanıcıya 400 olarak gösterilecek hata.
type importInputError string

func (e importInputError) Error() string { return string(e) }

// Planlama ---------------------------------------------------------------------------

// planImport dosyadaki her kaydı doğrular, mevcut kayıtlarla karşılaştırır ve
// yazılacak veriyi hazırlar. Veritabanına yazmaz.
func (s *Server) planImport(ctx context.Context, conv *backup.Result, replace bool, reqHost string) (*importPlan, error) {
	doc := conv.Doc
	switch {
	case len(doc.Monitors) > maxImportMonitors:
		return nil, importInputError(fmt.Sprintf("Dosyada en fazla %d monitör olabilir", maxImportMonitors))
	case len(doc.Notifications) > maxImportNotifs:
		return nil, importInputError(fmt.Sprintf("Dosyada en fazla %d bildirim kanalı olabilir", maxImportNotifs))
	case len(doc.Tags) > maxImportTags:
		return nil, importInputError(fmt.Sprintf("Dosyada en fazla %d etiket olabilir", maxImportTags))
	case len(doc.StatusPages) > maxImportPages:
		return nil, importInputError(fmt.Sprintf("Dosyada en fazla %d durum sayfası olabilir", maxImportPages))
	}
	pl := &importPlan{}
	pl.sum.Warnings = append([]string{}, conv.Warnings...)
	pl.sum.Items = []importItem{}
	pl.data.Replace = replace
	pl.data.KnownMonitors = map[int64]int64{}
	pl.data.RemapConfig = remapMonitorRefs

	for _, sk := range conv.Skipped {
		pl.sum.item(importItem{Kind: sk.Kind, Name: sk.Name, Type: sk.Type, Result: "skipped", Messages: []string{sk.Reason}})
	}

	// Mevcut kayıtlar (değiştir modunda hepsi silineceği için dikkate alınmaz).
	var (
		monitors []store.Monitor
		notifs   []store.Notification
		tags     []store.Tag
		pages    []store.StatusPage
		err      error
	)
	if !replace {
		if monitors, err = s.store.ListMonitors(ctx); err != nil {
			return nil, err
		}
		if notifs, err = s.store.ListNotifications(ctx); err != nil {
			return nil, err
		}
		if tags, err = s.store.ListTags(ctx); err != nil {
			return nil, err
		}
		if len(doc.StatusPages) > 0 {
			if pages, err = s.store.ListPages(ctx); err != nil {
				return nil, err
			}
		}
	}

	// Etiketler
	existingTags := map[string]int64{}
	for _, t := range tags {
		existingTags[store.TagKey(t.Name)] = t.ID
	}
	tagRefs := map[string]store.ImportRef{}
	addTag := func(name, color string, listed bool) (store.ImportRef, error) {
		n, c, err := normalizeTag(name, color)
		if err != nil {
			return store.ImportRef{}, err
		}
		key := store.TagKey(n)
		if ref, ok := tagRefs[key]; ok {
			return ref, nil
		}
		it := importItem{Kind: "tag", Name: n}
		var ref store.ImportRef
		if id, ok := existingTags[key]; ok {
			ref = store.Existing(id)
			it.Result = "existing"
			it.Messages = []string{"Aynı adda bir etiket zaten var; o kullanıldı"}
		} else {
			if len(tags)+len(pl.data.Tags) >= maxTagsTotal {
				return store.ImportRef{}, errors.New("Etiket sınırına (1000) ulaşıldı")
			}
			pl.data.Tags = append(pl.data.Tags, store.Tag{Name: n, Color: c})
			ref = store.NewRef(len(pl.data.Tags) - 1)
			it.Result = "created"
			if !listed {
				it.Messages = []string{"Etiket listesinde yoktu; varsayılan renkle oluşturuldu"}
			}
		}
		tagRefs[key] = ref
		pl.sum.item(it)
		return ref, nil
	}
	for _, t := range doc.Tags {
		if _, err := addTag(t.Name, backup.NormalizeColor(t.Color), true); err != nil {
			pl.sum.item(importItem{Kind: "tag", Name: t.Name, Result: "skipped", Messages: []string{err.Error()}})
		}
	}

	// Bildirim kanalları
	existingNotif := map[string]int64{}  // ad + tür
	existingByName := map[string]int64{} // ad (ilk)
	for _, n := range notifs {
		existingNotif[n.Name+"\x00"+n.Type] = n.ID
		if _, ok := existingByName[n.Name]; !ok {
			existingByName[n.Name] = n.ID
		}
	}
	notifRefs := map[string]store.ImportRef{}
	for _, bn := range doc.Notifications {
		it := importItem{Kind: "notification", Name: bn.Name, Type: bn.Type, Messages: bn.Notes}
		if _, ok := notify.Get(bn.Type); !ok {
			it.Result, it.Messages = "skipped", append(it.Messages, fmt.Sprintf("Bildirim türü “%s” bu sürümde yok", bn.Type))
			pl.sum.item(it)
			continue
		}
		active := bn.Active
		in := notificationInput{Name: bn.Name, Type: bn.Type, Config: bn.Config, IsDefault: bn.IsDefault, Active: &active,
			Events: bn.Events, QuietHours: bn.QuietHours, DelayMin: bn.DelayMin, EscalateMin: bn.EscalateMin, Lang: bn.Lang}
		n, err := in.toNotification()
		if err != nil {
			it.Result, it.Messages = "skipped", append(it.Messages, err.Error())
			pl.sum.item(it)
			continue
		}
		it.Name = n.Name
		var ref store.ImportRef
		if id, ok := existingNotif[n.Name+"\x00"+n.Type]; ok {
			ref = store.Existing(id)
			it.Result, it.Messages = "existing", append(it.Messages, "Aynı ad ve türde bir kanal zaten var; o kullanıldı")
		} else {
			var rules []store.ImportTagRef
			for _, tr := range bn.TagRules {
				tref, err := addTag(tr.Name, "", false)
				if err != nil {
					it.Messages = append(it.Messages, fmt.Sprintf("Etiket kuralı “%s” eklenemedi: %v", tr.Name, err))
					continue
				}
				v, err := normalizeTagValue(tr.Value)
				if err != nil {
					it.Messages = append(it.Messages, fmt.Sprintf("Etiket kuralı “%s”: %v", tr.Name, err))
					continue
				}
				rules = append(rules, store.ImportTagRef{Tag: tref, Value: v})
			}
			pl.data.Notifications = append(pl.data.Notifications, n)
			pl.data.NotificationTags = append(pl.data.NotificationTags, rules)
			ref = store.NewRef(len(pl.data.Notifications) - 1)
			it.Result = "created"
		}
		if _, dup := notifRefs[n.Name]; dup {
			it.Messages = append(it.Messages, "Aynı adda birden fazla kanal var; monitörler ilkine bağlanır")
		} else {
			notifRefs[n.Name] = ref
		}
		pl.sum.item(it)
	}

	// Monitörler
	// Konum ayarı: kontrol noktaları adla çözülür (büyük/küçük harf duyarsız).
	probeByName := map[string]int64{}
	if probes, err := s.store.ListProbesOfKind(ctx, store.ProbeKindLocation); err == nil {
		for _, p := range probes {
			if _, dup := probeByName[strings.ToLower(p.Name)]; !dup {
				probeByName[strings.ToLower(p.Name)] = p.ID
			}
		}
	}
	existingMon := map[string]int64{}
	usedTokens := map[string]bool{}
	for _, m := range monitors {
		existingMon[monitorDupKey(m)] = m.ID
		if m.PushToken != "" {
			usedTokens[m.PushToken] = true
		}
	}
	seenFileIDs := map[int64]bool{}
	for _, bm := range doc.Monitors {
		it := importItem{Kind: "monitor", Name: bm.Name, Type: bm.Type, Messages: append([]string{}, bm.Notes...)}
		skip := func(msg string) {
			it.Result, it.Messages = "skipped", append(it.Messages, msg)
			pl.sum.item(it)
		}
		if bm.Type == "browser" { // eski sürümlerin gerçek tarayıcı monitörü
			bm.Type, bm.Config = "http", browserAsHTTP(bm.Config)
			it.Type = bm.Type
			it.Messages = append(it.Messages, "Gerçek tarayıcı kontrolü kaldırıldı; HTTP monitörü olarak aktarıldı")
		}
		if _, ok := check.Get(bm.Type); !ok {
			skip(fmt.Sprintf("Monitör tipi “%s” bu sürümde yok", bm.Type))
			continue
		}
		in := monitorInput{
			Name: bm.Name, Type: bm.Type, Description: bm.Description, Interval: bm.Interval,
			RetryInterval: bm.RetryInterval, MaxRetries: bm.MaxRetries, Timeout: bm.Timeout,
			ResendEvery: bm.ResendEvery, UpsideDown: bm.UpsideDown, Config: bm.Config,
			SlowMs: bm.SlowMs, SlowChecks: bm.SlowChecks, DomainExpiry: bm.DomainExpiry,
		}
		if in.Type != check.TypePush && in.Type != check.TypeGroup && in.Timeout > 0 {
			// Eski sürümler zaman aşımının aralığı aşmasına izin veriyordu;
			// kayıt reddedilmez, zaman aşımı aralığın yarısına düşürülür.
			if lim := in.Interval; lim >= 20 && in.Timeout > lim {
				in.Timeout = max(1, lim/2)
				it.Messages = append(it.Messages, fmt.Sprintf("Zaman aşımı kontrol aralığından kısa olacak biçimde %d saniyeye düşürüldü", in.Timeout))
			}
		}
		m, err := in.toMonitor(false)
		if err != nil {
			skip(err.Error())
			continue
		}
		m.Active = bm.Active
		it.Name = m.Name
		fileID := bm.ID
		if fileID != 0 && seenFileIDs[fileID] {
			it.Messages = append(it.Messages, "Dosyada aynı kimlikte başka bir monitör var; durum sayfaları buna bağlanamaz")
			fileID = 0
		}
		if fileID != 0 {
			seenFileIDs[fileID] = true
		}
		if id, ok := existingMon[monitorDupKey(m)]; ok && !replace {
			if fileID != 0 {
				pl.data.KnownMonitors[fileID] = id
			}
			it.Result, it.ID = "existing", id
			it.Messages = append(it.Messages, "Aynı ad ve hedefle bir monitör zaten var; eklenmedi")
			pl.sum.item(it)
			continue
		}
		if m.Type == check.TypePush {
			tok := strings.TrimSpace(bm.PushToken)
			switch {
			case tok == "":
			case !importPushTokRe.MatchString(tok):
				it.Messages = append(it.Messages, "Push adresi bu uygulamada kullanılamıyor; yeni adres oluşturuldu, cron işini güncelleyin")
				tok = ""
			case usedTokens[tok]:
				it.Messages = append(it.Messages, "Push adresi başka bir monitörde kullanılıyor; yeni adres oluşturuldu, cron işini güncelleyin")
				tok = ""
			}
			if tok == "" {
				tok = randomToken(24)
			}
			usedTokens[tok] = true
			m.PushToken = tok
		}
		im := store.ImportMonitor{FileID: fileID, Monitor: m}
		seenRef := map[store.ImportRef]bool{}
		for _, name := range bm.Notifications {
			ref, ok := notifRefs[name]
			if !ok && !replace {
				if id, found := existingByName[name]; found {
					ref, ok = store.Existing(id), true
				}
			}
			if !ok {
				it.Messages = append(it.Messages, fmt.Sprintf("Bildirim kanalı “%s” bulunamadı; bağlanmadı", name))
				continue
			}
			if !seenRef[ref] {
				seenRef[ref] = true
				im.Notifications = append(im.Notifications, ref)
			}
		}
		seenTag := map[store.ImportTagRef]bool{}
		for _, t := range bm.Tags {
			ref, err := addTag(t.Name, "", false)
			if err != nil {
				it.Messages = append(it.Messages, fmt.Sprintf("Etiket “%s” eklenemedi: %v", t.Name, err))
				continue
			}
			v, err := normalizeTagValue(t.Value)
			if err != nil {
				it.Messages = append(it.Messages, fmt.Sprintf("Etiket “%s”: %v", t.Name, err))
				continue
			}
			tr := store.ImportTagRef{Tag: ref, Value: v}
			if !seenTag[tr] && len(im.Tags) < maxTagsPerMonitor {
				seenTag[tr] = true
				im.Tags = append(im.Tags, tr)
			}
		}
		if bl := bm.Locations; bl != nil && m.Type != check.TypeGroup && m.Type != check.TypePush {
			l := store.LocationSetup{IncludeLocal: bl.IncludeLocal, ProbeIDs: []int64{}, DownWhen: bl.DownWhen, NotifyPartial: bl.NotifyPartial}
			if !store.ValidDownWhen(l.DownWhen) {
				l.DownWhen = store.DownWhenAny
			}
			var missing []string
			for _, name := range bl.Probes {
				if pid, ok := probeByName[strings.ToLower(strings.TrimSpace(name))]; ok {
					if !slices.Contains(l.ProbeIDs, pid) {
						l.ProbeIDs = append(l.ProbeIDs, pid)
					}
				} else {
					missing = append(missing, name)
				}
			}
			if len(missing) > 0 {
				it.Messages = append(it.Messages, fmt.Sprintf("Kontrol noktası bulunamadı: %s; bu konum(lar) atlandı", strings.Join(missing, ", ")))
			}
			if !l.IncludeLocal && len(l.ProbeIDs) == 0 {
				l.IncludeLocal = true
				it.Messages = append(it.Messages, "Hiçbir kontrol noktası eşleşmedi; monitör ana sunucudan kontrol edilecek")
			}
			if l.Configured() {
				im.Locations = &l
			}
		}
		pl.data.Monitors = append(pl.data.Monitors, im)
		it.Result = "created"
		pl.monitorItems = append(pl.monitorItems, pl.sum.item(it))
	}

	// Durum sayfaları
	known := map[int64]bool{}
	for fid := range pl.data.KnownMonitors {
		known[fid] = true
	}
	for _, im := range pl.data.Monitors {
		if im.FileID != 0 {
			known[im.FileID] = true
		}
	}
	slugs, domains := map[string]bool{}, map[string]bool{}
	for _, p := range pages {
		slugs[p.Slug] = true
		if p.CustomDomain != "" {
			domains[p.CustomDomain] = true
		}
	}
	// Uygulamanın kendi adresi (BASE_URL) ve isteğin geldiği adres özel alan
	// adı olamaz (normalizePage ile aynı kural): aksi halde içe aktarma o
	// alan adında yönetim API'sini kapatıp herkesi dışarıda bırakırdı.
	selfHosts := map[string]bool{}
	if reqHost != "" {
		selfHosts[reqHost] = true
	}
	if b := s.baseHost(); b != "" {
		selfHosts[b] = true
	}
	tagRef := func(name, value string) (store.ImportTagRef, error) {
		ref, err := addTag(name, "", false)
		if err != nil {
			return store.ImportTagRef{}, err
		}
		v, err := normalizeTagValue(value)
		if err != nil {
			return store.ImportTagRef{}, err
		}
		return store.ImportTagRef{Tag: ref, Value: v}, nil
	}
	for _, bp := range doc.StatusPages {
		ip, it := planPage(bp, known, slugs, domains, selfHosts, tagRef)
		if it.Result == "created" {
			pl.data.Pages = append(pl.data.Pages, ip)
		}
		pl.sum.item(it)
	}

	// Kullanıcılar: var olan ad atlanır, yenisi şifre özetiyle eklenir
	// (değiştir modunda da kullanıcılar silinmez).
	if len(doc.Users) > maxImportUsers {
		return nil, importInputError(fmt.Sprintf("Dosyada en fazla %d kullanıcı olabilir", maxImportUsers))
	}
	if len(doc.Users) > 0 {
		existingUsers, err := s.store.ListUsers(ctx)
		if err != nil {
			return nil, err
		}
		taken := map[string]bool{}
		for _, u := range existingUsers {
			taken[strings.ToLower(u.Username)] = true
		}
		for _, bu := range doc.Users {
			it := importItem{Kind: "user", Name: bu.Username}
			switch {
			case !usernameRe.MatchString(bu.Username):
				it.Result, it.Messages = "skipped", []string{"Kullanıcı adı geçersiz"}
			case store.RoleRank(bu.Role) == 0:
				it.Result, it.Messages = "skipped", []string{"Rol geçersiz"}
			case !strings.HasPrefix(bu.PasswordHash, "$2") || len(bu.PasswordHash) < 59 || len(bu.PasswordHash) > 72:
				it.Result, it.Messages = "skipped", []string{"Şifre özeti okunamadı"}
			case taken[strings.ToLower(bu.Username)]:
				it.Result, it.Messages = "existing", []string{"Aynı adda bir kullanıcı zaten var; dokunulmadı"}
			default:
				iu := store.ImportUserData{Backup: store.UserBackup{User: store.User{Username: bu.Username, DisplayName: bu.DisplayName,
					Email: bu.Email, Role: bu.Role, Disabled: bu.Disabled, MustChangePassword: bu.MustChangePassword,
					AllMonitors: bu.AllMonitors || bu.Role != store.RoleViewer, Lang: bu.Lang, Theme: bu.Theme, PasswordHash: bu.PasswordHash}}}
				if email, err := normalizeEmail(bu.Email); err != nil {
					iu.Backup.User.Email = ""
					it.Messages = append(it.Messages, "E-posta geçersiz; alınmadı")
				} else {
					iu.Backup.User.Email = email
				}
				if bu.TOTPSecret != "" {
					iu.Backup.TOTPSecret, iu.Backup.RecoveryCodes = bu.TOTPSecret, bu.RecoveryCodes
					it.Messages = append(it.Messages, "İki adımlı doğrulama sırrıyla birlikte aktarıldı")
				} else if bu.TwoFactorEnabled {
					it.Messages = append(it.Messages, "Yedekte 2FA sırrı yok; iki adımlı doğrulama kapalı aktarıldı")
				}
				if !iu.Backup.User.AllMonitors {
					for _, fid := range bu.MonitorIDs {
						if known[fid] {
							iu.MonitorIDs = append(iu.MonitorIDs, fid)
						}
					}
					for _, tr := range bu.TagRules {
						if ref, err := tagRef(tr.Name, tr.Value); err == nil {
							iu.TagRules = append(iu.TagRules, ref)
						}
					}
				}
				taken[strings.ToLower(bu.Username)] = true
				pl.data.Users = append(pl.data.Users, iu)
				it.Result = "created"
			}
			pl.sum.item(it)
		}
	}

	// Ayarlar
	if doc.Settings != nil {
		it := importItem{Kind: "settings", Name: "Uygulama ayarları"}
		st := *doc.Settings
		switch err := st.Validate(); {
		case !replace:
			it.Result, it.Messages = "skipped", []string{"Ayarlar yalnızca “değiştir” modunda geri yüklenir"}
		case err != nil:
			it.Result, it.Messages = "skipped", []string{"Ayarlar geçersiz: " + err.Error()}
		default:
			pl.data.Settings = &st
			it.Result = "created"
			pl.sum.SettingsApplied = true
		}
		pl.sum.Items = append(pl.sum.Items, it)
	}
	pl.data.SystemMailChannel = doc.SystemMailChannel
	return pl, nil
}

// monitorDupKey birleştirmede kopya kontrolü: ad (büyük/küçük harf duyarsız) + hedef.
// Grup ve push'un hedefi ("N monitör", "Push") başka kayıtlara ya da rastgele
// token'a bağlı olduğundan bunlarda ad + tip yeterlidir; aksi halde grup, alt
// monitör kimlikleri yeniden eşlenmeden karşılaştırılıp yanlışlıkla yeni sayılır.
func monitorDupKey(m store.Monitor) string {
	name := strings.ToLower(strings.TrimSpace(m.Name))
	if m.Type == "group" || m.Type == "push" {
		return name + "\x00" + m.Type
	}
	return name + "\x00" + engine.Target(m)
}

// remapMonitorRefs grup monitörünün alt monitör kimliklerini dosya
// kimliklerinden yeni kimliklere çevirir; karşılığı olmayanlar çıkarılır.
func remapMonitorRefs(typ string, cfg json.RawMessage, ids map[int64]int64) (json.RawMessage, bool) {
	if typ != "group" {
		return nil, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(cfg, &m) != nil {
		return nil, false
	}
	var old []int64
	if json.Unmarshal(m["monitor_ids"], &old) != nil {
		return nil, false
	}
	out := make([]int64, 0, len(old))
	for _, id := range old {
		if n, ok := ids[id]; ok {
			out = append(out, n)
		}
	}
	m["monitor_ids"], _ = json.Marshal(out)
	b, err := json.Marshal(m)
	return b, err == nil
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// importHostnameOK küçük harfli, noktalı bir alan adı mı (IP adresi değil).
func importHostnameOK(h string) bool {
	if len(h) > 253 || !strings.Contains(h, ".") {
		return false
	}
	labels := strings.Split(h, ".")
	for _, l := range labels {
		if l == "" || len(l) > 63 || l[0] == '-' || l[len(l)-1] == '-' {
			return false
		}
		for _, c := range l {
			if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
				return false
			}
		}
	}
	return strings.Trim(labels[len(labels)-1], "0123456789") != ""
}

// planPage durum sayfasını doğrular; çakışmaları ve bulunamayan monitörleri not eder.
// known: içe aktarılan veya zaten var olan monitörlerin dosya kimlikleri.
func planPage(bp backup.Page, known map[int64]bool, slugs, domains, selfHosts map[string]bool,
	tagRef func(name, value string) (store.ImportTagRef, error)) (store.ImportPage, importItem) {
	it := importItem{Kind: "status_page", Name: strings.TrimSpace(bp.Title)}
	skip := func(msg string) (store.ImportPage, importItem) {
		it.Result, it.Messages = "skipped", append(it.Messages, msg)
		return store.ImportPage{}, it
	}
	p := store.StatusPage{
		Slug: strings.ToLower(strings.TrimSpace(bp.Slug)), Title: strings.TrimSpace(bp.Title),
		Description: strings.TrimSpace(bp.Description), Footer: strings.TrimSpace(bp.Footer),
		ShowTargets: bp.ShowTargets, Published: bp.Published, BarRange: bp.BarRange,
		ShowIncidents: bp.ShowIncidents == nil || *bp.ShowIncidents, Collapsible: bp.Collapsible,
		Lang: i18n.Or(bp.Lang), IncidentDays: bp.IncidentDays, UptimeWindows: store.NormalizeUptimeWindows(bp.UptimeWindows),
	}
	if bp.Layout != nil {
		l := store.PageLayout{Style: bp.Layout.Style, Width: bp.Layout.Width, ShowUptime: bp.Layout.ShowUptime}
		for _, b := range bp.Layout.Blocks {
			l.Blocks = append(l.Blocks, store.PageBlock{ID: b.ID, Visible: b.Visible})
			// show_incidents olmayan (elle yazılmış) yedekte olaylar dizilimden alınır.
			if b.ID == store.BlockIncidents && bp.ShowIncidents == nil {
				p.ShowIncidents = b.Visible
			}
		}
		p.Layout = l
	}
	p.Layout = store.NormalizeLayout(p.Layout, p.ShowIncidents)
	switch {
	case !importSlugRe.MatchString(p.Slug):
		return skip("Geçersiz sayfa adresi: " + bp.Slug)
	case runeLen(p.Title) < 1 || runeLen(p.Title) > 100:
		return skip("Başlık 1-100 karakter olmalı")
	case runeLen(p.Description) > 1000 || runeLen(p.Footer) > 500:
		return skip("Açıklama en fazla 1000, alt bilgi en fazla 500 karakter olabilir")
	case slugs[p.Slug]:
		it.Result, it.Messages = "existing", []string{"Bu adreste (" + p.Slug + ") bir durum sayfası zaten var; eklenmedi"}
		return store.ImportPage{}, it
	}
	if h := bp.PasswordHash; h != "" {
		if strings.HasPrefix(h, "$2") && len(h) >= 59 && len(h) <= 72 {
			p.PasswordHash = h
		} else {
			// Şifre korumalı sayfa yanlışlıkla herkese açılmasın.
			p.Published = false
			it.Messages = append(it.Messages, "Sayfa şifresi okunamadı; sayfa yayından kaldırılmış olarak eklendi, yeni şifre belirleyin")
		}
	}
	if d := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(bp.CustomDomain)), "."); d != "" {
		switch {
		case !importHostnameOK(d):
			it.Messages = append(it.Messages, "Özel alan adı geçersiz; kaldırıldı")
		case selfHosts[d]:
			it.Messages = append(it.Messages, "Özel alan adı ("+d+") uygulamanın kendi adresi olamaz; kaldırıldı")
		case domains[d]:
			it.Messages = append(it.Messages, "Özel alan adı ("+d+") başka bir sayfada kullanılıyor; kaldırıldı")
		default:
			p.CustomDomain = d
			domains[d] = true
		}
	}
	missing, total := 0, 0
	seen := map[int64]bool{}
	sectionTags := map[int]store.ImportTagRef{}
	for i, sec := range bp.Sections {
		if i >= maxImportSections {
			it.Messages = append(it.Messages, fmt.Sprintf("En fazla %d grup aktarıldı", maxImportSections))
			break
		}
		ps := store.PageSection{Title: strings.TrimSpace(sec.Title), Monitors: []store.PageMonitor{}}
		if sec.Tag != nil && tagRef != nil {
			if ref, err := tagRef(sec.Tag.Name, sec.Tag.Value); err == nil {
				sectionTags[len(p.Sections)] = ref
			} else {
				it.Messages = append(it.Messages, fmt.Sprintf("Grup etiketi “%s” eklenemedi: %v", sec.Tag.Name, err))
			}
		}
		if runeLen(ps.Title) > 100 {
			ps.Title = string([]rune(ps.Title)[:100])
		}
		for _, pm := range sec.Monitors {
			if !known[pm.MonitorID] {
				missing++
				continue
			}
			if seen[pm.MonitorID] || total >= maxImportPageMons {
				continue
			}
			seen[pm.MonitorID] = true
			total++
			name := strings.TrimSpace(pm.Name)
			if runeLen(name) > 100 {
				name = string([]rune(name)[:100])
			}
			ps.Monitors = append(ps.Monitors, store.PageMonitor{ID: pm.MonitorID, Name: name})
		}
		p.Sections = append(p.Sections, ps)
	}
	if missing > 0 {
		it.Messages = append(it.Messages, fmt.Sprintf("%d monitör içe aktarılmadığı için sayfadan çıkarıldı", missing))
	}
	ip := store.ImportPage{Page: p, SectionTags: sectionTags}
	if len(bp.Logo) > 0 {
		if importLogoTypes[bp.LogoType] && len(bp.Logo) <= maxImportLogoBytes {
			ip.Logo, ip.LogoType = bp.Logo, bp.LogoType
		} else {
			it.Messages = append(it.Messages, "Logo geçersiz veya çok büyük; aktarılmadı")
		}
	}
	for i, a := range bp.Announcements {
		if i >= maxImportAnnounces {
			break
		}
		a.Title, a.Body, a.Severity = strings.TrimSpace(a.Title), strings.TrimSpace(a.Body), strings.TrimSpace(a.Severity)
		if a.Severity == "" {
			a.Severity = "info"
		}
		if a.Title == "" || runeLen(a.Title) > 200 || runeLen(a.Body) > 5000 || !importSeverities[a.Severity] || a.StartsAt <= 0 {
			it.Messages = append(it.Messages, "Geçersiz bir duyuru atlandı")
			continue
		}
		ip.Announcements = append(ip.Announcements, store.Announcement{
			Title: a.Title, Body: a.Body, Severity: a.Severity, StartsAt: a.StartsAt, EndsAt: a.EndsAt,
		})
	}
	slugs[p.Slug] = true
	it.Result = "created"
	return ip, it
}

// browserAsHTTP kaldırılan tarayıcı monitörünün ayarından HTTP kontrolüyle
// ortak alanları (adres, kelime, TLS) alır.
func browserAsHTTP(raw json.RawMessage) json.RawMessage {
	var c struct {
		URL       string `json:"url"`
		Keyword   string `json:"keyword,omitempty"`
		IgnoreTLS bool   `json:"ignore_tls,omitempty"`
	}
	json.Unmarshal(raw, &c)
	out, _ := json.Marshal(c)
	return out
}
