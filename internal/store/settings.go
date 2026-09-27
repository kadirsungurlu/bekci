package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

// AppSettings arayüzden değiştirilebilen uygulama ayarları.
type AppSettings struct {
	RetentionRawDays    int   `json:"retention_raw_days"`    // ham kontrol kayıtları
	RetentionHourlyDays int   `json:"retention_hourly_days"` // saatlik özetler (günlükler süresiz)
	CertDays            []int `json:"cert_days"`             // SSL uyarı eşikleri (gün)
	BackupKeep          int   `json:"backup_keep"`           // saklanacak gece yedeği sayısı
}

func DefaultSettings() AppSettings {
	return AppSettings{RetentionRawDays: 14, RetentionHourlyDays: 365, CertDays: []int{21, 14, 7, 3, 1}, BackupKeep: 7}
}

// Validate hatalıysa Türkçe açıklama döner; CertDays'i sıralar ve tekilleştirir.
func (a *AppSettings) Validate() error {
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
