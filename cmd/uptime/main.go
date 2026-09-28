// Uptime: Go ile yazılmış, sade arayüzlü uptime izleme sistemi.
//
// Ortam değişkenleri:
//
//	ADDR                   dinlenecek adres (varsayılan :8080)
//	DATA_DIR               SQLite veritabanı ve yedeklerin klasörü (varsayılan ./data)
//	DATABASE_URL           verilirse PostgreSQL kullanılır (postgres://kullanıcı:şifre@sunucu:5432/vt)
//	BASE_URL               bildirimlerdeki bağlantılar için dış adres, ör. https://uptime.kadir.app
//	                       (durum sayfası özel alan adı bu adresle aynı olamaz)
//	LOG_LEVEL              debug | info | warn | error (varsayılan info)
//	MAX_CONCURRENT_CHECKS  aynı anda en fazla kontrol sayısı (varsayılan 50)
//	TZ                     saat dilimi (günlük özetler ve yedek saati için)
//	PROBE_IMAGE            kontrol noktası kurulum komutunda kullanılacak Docker imajı; boşsa (varsayılan)
//	                       komut herkese açık alpine imajıyla programı bu sunucudan indirir
//	AGENT_DIR              diğer platformların ajan programları (uptime-windows-amd64.exe …);
//	                       varsayılan /usr/local/share/uptime/agents (Docker imajında hazır)
//
// Uzak kontrol noktası modu (veritabanı kullanmaz; bkz. probe.go):
//
//	uptime probe            PROBE_SERVER ve PROBE_TOKEN ortam değişkenleriyle
//	uptime service install  (Windows, Yönetici) ajanı Windows hizmeti olarak kurar/günceller;
//	                        PROBE_SERVER ve PROBE_TOKEN ortamdan okunur (bkz. service_windows.go)
//	uptime service uninstall
//	uptime version          sürümü yazar
//
// Şifre sıfırlama (giriş yapılamadığında, container içinde):
//
//	uptime sifre-sifirla <kullanıcı-adı> [--2fa-kapat]
//	  yeni şifre standart girdiden okunur; --2fa-kapat iki adımlı doğrulamayı
//	  da kapatır (telefon ve kurtarma kodları kaybolduysa)
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // imajda tzdata olmasa da TZ çalışsın

	"golang.org/x/crypto/bcrypt"

	"github.com/kadirsa1105/uptime-kadir-app/internal/api"
	"github.com/kadirsa1105/uptime-kadir-app/internal/engine"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/stats"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
	"github.com/kadirsa1105/uptime-kadir-app/web"
)

// version derlemede -ldflags "-X main.version=..." ile verilir.
var version = "dev"

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
}

// newLogger LOG_LEVEL düzeyinde metin günlüğü.
func newLogger(w io.Writer) *slog.Logger {
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

func run() error {
	log := newLogger(os.Stdout)
	// Veritabanı gerektirmeyen komutlar.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "probe":
			return runProbe(log)
		case "service":
			return runServiceCommand(os.Args[2:])
		case "version", "surum":
			fmt.Println(version)
			return nil
		}
	}

	dataDir := env("DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("veri klasörü oluşturulamadı: %w", err)
	}
	target, dbDesc := filepath.Join(dataDir, "uptime.db"), "SQLite"
	if u := env("DATABASE_URL", ""); u != "" {
		if !store.IsPostgresDSN(u) {
			return errors.New("DATABASE_URL postgres:// veya postgresql:// ile başlamalı")
		}
		target, dbDesc = u, "PostgreSQL "+redactDSN(u)
	}
	// Sunucu modunda SQLite dosyasına tek örnek kilidi alınır (Coolify yeni
	// konteyneri eskisi kapanmadan başlatır). Komutlar (sifre-sifirla vb.)
	// çalışan sunucunun yanında kullanıldığı için kilit almaz.
	serverMode := len(os.Args) <= 1
	lockWait, _ := strconv.Atoi(env("UPTIME_LOCK_WAIT", "600"))
	st, err := store.OpenWith(target, time.Local, store.Options{
		Log:          log,
		BackupDir:    filepath.Join(dataDir, "backups"),
		InstanceLock: serverMode,
		LockWait:     time.Duration(lockWait) * time.Second,
		StandbyFile:  filepath.Join(os.TempDir(), "uptime-bekliyor"),
	})
	if err != nil {
		return fmt.Errorf("veritabanı açılamadı: %w", err)
	}
	defer st.Close()
	log.Info("veritabanı", "tür", dbDesc)

	if len(os.Args) > 1 {
		return runCommand(st, os.Args[1:])
	}

	maxChecks, _ := strconv.Atoi(env("MAX_CONCURRENT_CHECKS", "50"))
	baseURL := strings.TrimRight(env("BASE_URL", ""), "/")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	hub := engine.NewHub()
	dispatcher := notify.NewDispatcher(st, log)
	eng := engine.New(st, dispatcher, hub, log, engine.Config{MaxConcurrent: maxChecks, BaseURL: baseURL})
	if err := eng.Start(ctx); err != nil {
		return fmt.Errorf("kontrol motoru başlatılamadı: %w", err)
	}
	maint := stats.NewMaintenance(st, log, dataDir, time.Local)
	if st.Postgres() {
		maint.SetPostgresDump(target)
	}
	go maint.Run(ctx)

	apiServer := api.New(st, eng, hub, dispatcher, log, web.Dist(), version)
	apiServer.BaseURL = baseURL
	apiServer.ProbeImage = env("PROBE_IMAGE", "")
	if d := env("AGENT_DIR", ""); d != "" {
		apiServer.AgentDir = d
	}
	// Sunucu takibi: çevrimdışı ajan taraması (30 sn'de bir) ve bildirim bağlantıları.
	serverMon := apiServer.Servers()
	serverMon.SetBaseURL(baseURL)
	serverMon.Start(ctx)
	srv := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           apiServer.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // SSE bağlantısı kendi süresini kaldırır
		IdleTimeout:       120 * time.Second,
		// Kapanışta açık SSE bağlantıları da sonlansın.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	errCh := make(chan error, 1)
	go func() {
		log.Info("Uptime başladı", "sürüm", version, "adres", srv.Addr, "veri", dataDir, "saat_dilimi", time.Local.String())
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-errCh:
		return err
	}
	log.Info("kapanıyor")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(shutdownCtx)
	eng.Wait()
	serverMon.Wait()
	dispatcher.Wait(10 * time.Second)
	log.Info("kapandı")
	return nil
}

func runCommand(st *store.Store, args []string) error {
	switch args[0] {
	case "sifre-sifirla":
		usage := errors.New("kullanım: uptime sifre-sifirla <kullanıcı-adı> [--2fa-kapat]")
		var names []string
		disable2FA := false
		for _, a := range args[1:] {
			switch {
			case a == "--2fa-kapat":
				disable2FA = true
			case strings.HasPrefix(a, "-"):
				return usage
			default:
				names = append(names, a)
			}
		}
		if len(names) != 1 {
			return usage
		}
		name := names[0]
		ctx := context.Background()
		u, err := st.UserByName(ctx, name)
		if err != nil {
			return fmt.Errorf("kullanıcı bulunamadı: %s", name)
		}
		fmt.Fprint(os.Stderr, "Yeni şifre: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pw := strings.TrimRight(line, "\r\n")
		if len([]rune(pw)) < 8 || len(pw) > 72 {
			return errors.New("şifre 8-72 karakter olmalı")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
		if err != nil {
			return err
		}
		if err := st.UpdatePassword(ctx, u.ID, string(hash)); err != nil {
			return err
		}
		// Acil sıfırlama genelde erişim şüphesiyle yapılır: açık oturumlar kapanır.
		if err := st.DeleteUserSessions(ctx, u.ID); err != nil {
			return err
		}
		fmt.Fprintln(os.Stderr, "Şifre değiştirildi, tüm oturumlar kapatıldı.")
		switch {
		case disable2FA && u.TwoFactorEnabled:
			if err := st.DisableTwoFactor(ctx, u.ID); err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, "İki adımlı doğrulama kapatıldı.")
		case u.TwoFactorEnabled:
			fmt.Fprintln(os.Stderr, "Not: iki adımlı doğrulama açık; kapatmak için komutu --2fa-kapat ile çalıştırın.")
		}
		return nil
	}
	return fmt.Errorf("bilinmeyen komut: %s", args[0])
}

// redactDSN loglarda bağlantı adresindeki şifreyi gizler.
func redactDSN(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "(adres okunamadı)"
	}
	if _, ok := u.User.Password(); ok {
		u.User = url.UserPassword(u.User.Username(), "***")
	}
	return u.Redacted()
}
