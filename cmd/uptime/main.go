// Uptime: Go ile yazılmış, sade arayüzlü uptime izleme sistemi.
//
// Ortam değişkenleri:
//
//	ADDR                   dinlenecek adres (varsayılan :8080)
//	DATA_DIR               veritabanı ve yedeklerin klasörü (varsayılan ./data)
//	BASE_URL               bildirimlerdeki bağlantılar için dış adres, ör. https://uptime.kadir.app
//	LOG_LEVEL              debug | info | warn | error (varsayılan info)
//	MAX_CONCURRENT_CHECKS  aynı anda en fazla kontrol sayısı (varsayılan 50)
//	TZ                     saat dilimi (günlük özetler ve yedek saati için)
//
// Şifre sıfırlama (giriş yapılamadığında, container içinde):
//
//	uptime sifre-sifirla <kullanıcı-adı>   (yeni şifre standart girdiden okunur)
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
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

func run() error {
	var level slog.Level
	if err := level.UnmarshalText([]byte(env("LOG_LEVEL", "info"))); err != nil {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))

	dataDir := env("DATA_DIR", "./data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("veri klasörü oluşturulamadı: %w", err)
	}
	st, err := store.Open(filepath.Join(dataDir, "uptime.db"), time.Local)
	if err != nil {
		return fmt.Errorf("veritabanı açılamadı: %w", err)
	}
	defer st.Close()

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
	go stats.NewMaintenance(st, log, dataDir, time.Local).Run(ctx)

	srv := &http.Server{
		Addr:              env("ADDR", ":8080"),
		Handler:           api.New(st, eng, hub, dispatcher, log, web.Dist(), version).Handler(),
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
	dispatcher.Wait(10 * time.Second)
	log.Info("kapandı")
	return nil
}

func runCommand(st *store.Store, args []string) error {
	switch args[0] {
	case "sifre-sifirla":
		if len(args) != 2 {
			return errors.New("kullanım: uptime sifre-sifirla <kullanıcı-adı>")
		}
		ctx := context.Background()
		u, err := st.UserByName(ctx, args[1])
		if err != nil {
			return fmt.Errorf("kullanıcı bulunamadı: %s", args[1])
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
		return nil
	case "version", "surum":
		fmt.Println(version)
		return nil
	}
	return fmt.Errorf("bilinmeyen komut: %s", args[0])
}
