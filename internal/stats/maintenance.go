// Package stats periyodik bakım işlerini yürütür: eski kayıtların temizliği,
// süresi dolan oturumların silinmesi ve gece yedeği.
package stats

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// BackupHour yedeğin alınacağı yerel saat (03:xx, trafiğin en az olduğu zaman).
const BackupHour = 3

type Maintenance struct {
	store     *store.Store
	log       *slog.Logger
	backupDir string
	loc       *time.Location
	now       func() time.Time
}

func NewMaintenance(s *store.Store, log *slog.Logger, dataDir string, loc *time.Location) *Maintenance {
	return &Maintenance{store: s, log: log, backupDir: filepath.Join(dataDir, "backups"), loc: loc, now: time.Now}
}

// Run ctx iptal edilene kadar 10 dakikada bir bakım yapar.
func (m *Maintenance) Run(ctx context.Context) {
	// İlk temizlik açılıştan biraz sonra: başlangıçta motorla yarışmasın.
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.Tick(ctx)
			timer.Reset(10 * time.Minute)
		}
	}
}

// Tick tek bir bakım turu (testlerden de çağrılır).
func (m *Maintenance) Tick(ctx context.Context) {
	settings, err := m.store.LoadSettings(ctx)
	if err != nil {
		m.log.Error("ayarlar okunamadı", "hata", err)
		return
	}
	now := m.now()
	if n, err := m.store.DeleteBeatsBefore(ctx, now.AddDate(0, 0, -settings.RetentionRawDays).Unix()); err != nil {
		m.log.Error("eski kayıtlar silinemedi", "hata", err)
	} else if n > 0 {
		m.log.Info("eski kontrol kayıtları silindi", "adet", n)
	}
	if _, err := m.store.DeleteHourlyBefore(ctx, now.AddDate(0, 0, -settings.RetentionHourlyDays).Unix()); err != nil {
		m.log.Error("eski saatlik özetler silinemedi", "hata", err)
	}
	if err := m.store.DeleteExpiredSessions(ctx); err != nil {
		m.log.Error("süresi dolan oturumlar silinemedi", "hata", err)
	}

	local := now.In(m.loc)
	if settings.BackupKeep > 0 && local.Hour() >= BackupHour {
		if err := m.backup(ctx, local, settings.BackupKeep); err != nil {
			m.log.Error("yedek alınamadı", "hata", err)
		}
	}
}

// backup bugünün yedeği yoksa alır ve en yeni keep adet yedeği tutar.
func (m *Maintenance) backup(ctx context.Context, local time.Time, keep int) error {
	if err := os.MkdirAll(m.backupDir, 0o750); err != nil {
		return err
	}
	path := filepath.Join(m.backupDir, "uptime-"+local.Format("2006-01-02")+".db")
	if _, err := os.Stat(path); err == nil {
		return nil // bugünün yedeği zaten var
	}
	tmp := path + ".tmp"
	os.Remove(tmp)
	if err := m.store.Backup(ctx, tmp); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	m.log.Info("gece yedeği alındı", "dosya", path)
	return m.prune(keep)
}

func (m *Maintenance) prune(keep int) error {
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "uptime-") && strings.HasSuffix(e.Name(), ".db") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files) // tarih adın içinde: alfabetik = kronolojik
	for len(files) > keep {
		if err := os.Remove(filepath.Join(m.backupDir, files[0])); err != nil {
			return err
		}
		files = files[1:]
	}
	return nil
}
