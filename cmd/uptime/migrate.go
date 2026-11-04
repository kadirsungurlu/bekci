package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

// runMigrate: uptime migrate. SQLite veritabanını PostgreSQL + TimescaleDB'ye
// taşır (panel durdurulmuş olmalı). Kesilirse aynı komut kaldığı yerden sürer.
func runMigrate(log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	from := fs.String("from", filepath.Join(env("DATA_DIR", "./data"), "uptime.db"), "kaynak SQLite dosyası")
	to := fs.String("to", env("DATABASE_URL", ""), "hedef PostgreSQL adresi (varsayılan DATABASE_URL)")
	batch := fs.Int("batch", 10000, "parti başına satır")
	restart := fs.Bool("restart", false, "yarım kalan taşımayı silip baştan başlar")
	dry := fs.Bool("dry-run", false, "yalnız kaynağı okur, tablo ve satır sayılarını yazar")
	work := fs.String("work-dir", "", "geçici kopyanın klasörü (varsayılan kaynağın yanı; kaynak kadar boş alan ister)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New("kullanım: uptime migrate [--from dosya] [--to postgres://...] [--batch n] [--restart] [--dry-run] [--work-dir klasör]")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	start := time.Now()
	res, err := store.MigrateSQLite(ctx, store.SQLiteMigrateOptions{
		From: *from, To: *to, Batch: *batch, Restart: *restart, DryRun: *dry, WorkDir: *work,
		TimescaleOff: strings.EqualFold(env("DB_TIMESCALE", "auto"), "off"),
		Out:          os.Stdout, Log: log, Loc: time.Local,
	})
	if err != nil {
		if ctx.Err() != nil {
			return errors.New("taşıma kesildi; aynı komutla kaldığı yerden devam eder")
		}
		return err
	}
	if *dry {
		fmt.Printf("Plan: %d tablo, %d satır. Hedefe dokunulmadı.\n", res.Tables, res.Rows)
		return nil
	}
	fmt.Printf("Taşıma tamam: %d tablo, %d satır, kip %s, süre %s.\n", res.Tables, res.Rows, res.Mode, time.Since(start).Round(time.Second))
	if res.Sanitized > 0 {
		fmt.Printf("Not: %d metin değerinde PostgreSQL'in kabul etmediği karakterler temizlendi.\n", res.Sanitized)
	}
	if res.Mode != store.DBModeTimescale {
		fmt.Println("Uyarı: hedefte TimescaleDB yok ya da kapalı; panel düz PostgreSQL yolunda çalışır.")
	}
	fmt.Printf("SQLite dosyası değiştirilmedi: %s (geri dönüş için saklayın).\n", *from)
	return nil
}
