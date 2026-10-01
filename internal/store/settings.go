package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/i18n"
)

// AppSettings arayüzden değiştirilebilen uygulama ayarları.
type AppSettings struct {
	RetentionRawDays    int   `json:"retention_raw_days"`    // ham kontrol kayıtları
	RetentionHourlyDays int   `json:"retention_hourly_days"` // saatlik özetler (günlükler süresiz)
	CertDays            []int `json:"cert_days"`             // SSL uyarı eşikleri (gün)
	BackupKeep          int   `json:"backup_keep"`           // saklanacak gece yedeği sayısı
	// NotifyLang bildirim metinlerinin dili (tr | en). Boş (eski kayıt) tr sayılır.
	NotifyLang string `json:"notify_lang"`
	// CheckUserAgent kontrol isteklerinin User-Agent'ı; boş: varsayılan
	// (check.DefaultUserAgent, "Bekci" içerir). Kontrol noktalarına da gider.
	CheckUserAgent string `json:"check_user_agent"`

	// Saklama süreleri (gün; 0 = süresiz). Eski kayıtlarda alan yoksa
	// varsayılanlar (365 / 90 / 365) geçerlidir: davranış değişmez.
	RetentionIncidentDays *int `json:"retention_incident_days"` // çözülmüş olaylar (işlem geçmişiyle)
	RetentionCaptureDays  *int `json:"retention_capture_days"`  // olayı açan istek/yanıt kaydı
	RetentionAuditDays    *int `json:"retention_audit_days"`    // işlem kaydı
}

// Saklama varsayılanları (gün).
const (
	DefaultIncidentKeepDays = IncidentKeepDays
	DefaultCaptureKeepDays  = CaptureKeepDays
	DefaultAuditKeepDays    = 365
)

func intPtr(n int) *int { return &n }

func DefaultSettings() AppSettings {
	return AppSettings{RetentionRawDays: 14, RetentionHourlyDays: 365, CertDays: []int{21, 14, 7, 3, 1}, BackupKeep: 7, NotifyLang: i18n.Default,
		RetentionIncidentDays: intPtr(DefaultIncidentKeepDays), RetentionCaptureDays: intPtr(DefaultCaptureKeepDays), RetentionAuditDays: intPtr(DefaultAuditKeepDays)}
}

// IncidentKeep / CaptureKeep / AuditKeep saklama süreleri (gün; 0 = süresiz;
// alan yoksa varsayılan).
func (a AppSettings) IncidentKeep() int {
	return orDefault(a.RetentionIncidentDays, DefaultIncidentKeepDays)
}
func (a AppSettings) CaptureKeep() int {
	return orDefault(a.RetentionCaptureDays, DefaultCaptureKeepDays)
}
func (a AppSettings) AuditKeep() int { return orDefault(a.RetentionAuditDays, DefaultAuditKeepDays) }

func orDefault(p *int, def int) int {
	if p == nil {
		return def
	}
	return *p
}

// Validate hatalıysa Türkçe açıklama döner; CertDays'i sıralar ve tekilleştirir.
func (a *AppSettings) Validate() error {
	a.CheckUserAgent = strings.TrimSpace(a.CheckUserAgent)
	if len(a.CheckUserAgent) > 300 {
		return fmt.Errorf("User-Agent en fazla 300 karakter olabilir")
	}
	for _, r := range a.CheckUserAgent {
		// HTTP başlık değeri: yazdırılabilir ASCII (satır sonu ile başlık eklenemez).
		if r < 0x20 || r > 0x7e {
			return fmt.Errorf("User-Agent yalnızca yazdırılabilir ASCII karakterler içerebilir")
		}
	}
	if a.RetentionRawDays < 1 || a.RetentionRawDays > 90 {
		return fmt.Errorf("Ham kayıt saklama süresi 1-90 gün olmalı")
	}
	// 30 ve 90 günlük uptime ile grafikler saatlik özetlerden okunur.
	if a.RetentionHourlyDays < 90 || a.RetentionHourlyDays > 3650 {
		return fmt.Errorf("Saatlik özet saklama süresi 90-3650 gün olmalı")
	}
	if a.RetentionHourlyDays < a.RetentionRawDays {
		return fmt.Errorf("Saatlik özetler ham kayıtlardan daha kısa saklanamaz")
	}
	if len(a.CertDays) > 10 {
		return fmt.Errorf("En fazla 10 SSL uyarı eşiği girilebilir")
	}
	seen := map[int]bool{}
	days := []int{}
	for _, d := range a.CertDays {
		if d < 0 || d > 90 {
			return fmt.Errorf("SSL uyarı eşikleri 0-90 gün olmalı")
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(days)))
	a.CertDays = days
	if a.BackupKeep < 0 || a.BackupKeep > 60 {
		return fmt.Errorf("Saklanacak yedek sayısı 0-60 olmalı (0: yedek alma)")
	}
	if a.RetentionIncidentDays == nil {
		a.RetentionIncidentDays = intPtr(DefaultIncidentKeepDays)
	}
	if a.RetentionCaptureDays == nil {
		a.RetentionCaptureDays = intPtr(DefaultCaptureKeepDays)
	}
	if a.RetentionAuditDays == nil {
		a.RetentionAuditDays = intPtr(DefaultAuditKeepDays)
	}
	if d := *a.RetentionIncidentDays; d != 0 && (d < 7 || d > 3650) {
		return fmt.Errorf("Olay saklama süresi 7-3650 gün ya da 0 (süresiz) olmalı")
	}
	if d := *a.RetentionCaptureDays; d != 0 && (d < 1 || d > 3650) {
		return fmt.Errorf("İstek/yanıt kaydı saklama süresi 1-3650 gün ya da 0 (süresiz) olmalı")
	}
	if d := *a.RetentionAuditDays; d != 0 && (d < 7 || d > 3650) {
		return fmt.Errorf("İşlem kaydı saklama süresi 7-3650 gün ya da 0 (süresiz) olmalı")
	}
	if a.NotifyLang == "" {
		a.NotifyLang = i18n.Default
	}
	if !i18n.Valid(a.NotifyLang) {
		return fmt.Errorf("Bildirim dili tr veya en olmalı")
	}
	return nil
}

const appSettingsKey = "app"

// LoadSettings kayıtlı ayarları varsayılanların üzerine uygular.
func (s *Store) LoadSettings(ctx context.Context) (AppSettings, error) {
	a := DefaultSettings()
	v, ok, err := s.GetSetting(ctx, appSettingsKey)
	if err != nil || !ok {
		return a, err
	}
	if err := json.Unmarshal([]byte(v), &a); err != nil {
		return DefaultSettings(), nil
	}
	if a.Validate() != nil {
		return DefaultSettings(), nil
	}
	return a, nil
}

func (s *Store) SaveSettings(ctx context.Context, a AppSettings) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	return s.SetSettings(ctx, map[string]string{appSettingsKey: string(b)})
}

func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	return s.tx(ctx, func(tx *Tx) error {
		for k, v := range kv {
			if _, err := tx.ExecContext(ctx,
				"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value",
				k, v); err != nil {
				return err
			}
		}
		return nil
	})
}
