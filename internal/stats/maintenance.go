// Package stats periyodik bakım işlerini yürütür: eski kayıtların temizliği,
// süresi dolan oturumların silinmesi ve gece yedeği.
package stats

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// BackupHour yedeğin alınacağı yerel saat (03:xx, trafiğin en az olduğu zaman).
const BackupHour = 3

type Maintenance struct {
	pgDumpDSN string // PostgreSQL'de gece yedeği için pg_dump bağlantısı (boşsa yedek alınmaz)
	store     *store.Store
	log       *slog.Logger
	backupDir string
	loc       *time.Location
	now       func() time.Time
}

// SetPostgresDump PostgreSQL kullanılırken gece yedeğinin pg_dump ile
// alınmasını sağlar (pg_dump PATH'te yoksa yedek atlanır).
func (m *Maintenance) SetPostgresDump(dsn string) { m.pgDumpDSN = dsn }

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
	if _, err := m.store.DeleteAuditBefore(ctx, now.AddDate(-1, 0, 0).Unix()); err != nil {
		m.log.Error("eski işlem kayıtları silinemedi", "hata", err)
	}
	m.pruneServerStats(ctx, now)
	if _, err := m.store.DeleteIncidentCapturesBefore(ctx, now.AddDate(0, 0, -store.CaptureKeepDays).Unix()); err != nil {
		m.log.Error("eski olay istek/yanıt kayıtları silinemedi", "hata", err)
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

// Sunucu metriklerinin saklama süreleri (docs/PLAN.md §12.4); ayarlardan değişmez.
var serverStatsKeep = map[int]time.Duration{
	store.ServerRes1:  24 * time.Hour,
	store.ServerRes10: 7 * 24 * time.Hour,
	store.ServerRes60: 90 * 24 * time.Hour,
}

// serverEventsKeep bitmiş sunucu uyarılarının saklama süresi.
const serverEventsKeep = 90 * 24 * time.Hour

// pruneServerStats eski sunucu örneklerini, özetlerini ve uyarı geçmişini siler.
func (m *Maintenance) pruneServerStats(ctx context.Context, now time.Time) {
	for _, res := range []int{store.ServerRes1, store.ServerRes10, store.ServerRes60} {
		if _, err := m.store.DeleteServerStatsBefore(ctx, res, now.Add(-serverStatsKeep[res]).Unix()); err != nil {
			m.log.Error("eski sunucu metrikleri silinemedi", "çözünürlük", res, "hata", err)
		}
	}
	if _, err := m.store.DeleteServerAlertEventsBefore(ctx, now.Add(-serverEventsKeep).Unix()); err != nil {
		m.log.Error("eski sunucu uyarıları silinemedi", "hata", err)
	}
}

// backup bugünün yedeği yoksa alır ve en yeni keep adet yedeği tutar.
func (m *Maintenance) backup(ctx context.Context, local time.Time, keep int) error {
	if err := os.MkdirAll(m.backupDir, 0o750); err != nil {
		return err
	}
	ext, dump := ".db", m.store.Backup
	if m.store.Postgres() {
		// PostgreSQL: gömülü imajda pg_dump var; dış veritabanında yedek
		// veritabanı tarafında (Coolify yedekleri vb.) alınmalı.
		if m.pgDumpDSN == "" {
			return nil
		}
		if _, err := exec.LookPath("pg_dump"); err != nil {
			return nil
		}
		ext, dump = ".dump", m.pgDump
	}
	path := filepath.Join(m.backupDir, "uptime-"+local.Format("2006-01-02")+ext)
	if _, err := os.Stat(path); err == nil {
		return nil // bugünün yedeği zaten var
	}
	tmp := path + ".tmp"
	os.Remove(tmp)
	if err := dump(ctx, tmp); err != nil {
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
		name := e.Name()
		if !e.IsDir() && strings.HasPrefix(name, "uptime-") && (strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".dump")) {
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

// pgDump PostgreSQL veritabanını pg_dump'ın sıkıştırılmış özel biçiminde
// yazar (geri yükleme: pg_restore --clean -d <veritabanı> dosya.dump).
func (m *Maintenance) pgDump(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pg_dump", "--format=custom", "--no-owner", "--file", path, "--dbname", m.pgDumpDSN)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pg_dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
