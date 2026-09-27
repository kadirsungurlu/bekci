package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/probe"
)

// runProbe "uptime probe" komutu: uzak kontrol noktası modu. Veritabanı
// açılmaz; atanan monitörler ana sunucudan alınır, sonuçlar oraya gönderilir.
//
// Ortam değişkenleri:
//
//	PROBE_SERVER           ana sunucunun adresi, ör. https://uptime.kadir.app
//	PROBE_TOKEN            arayüzde (Ayarlar → Kontrol noktaları) verilen token (upr_…)
//	MAX_CONCURRENT_CHECKS  aynı anda en fazla kontrol (varsayılan 20)
//	ADDR                   sağlık kontrolü (/healthz) adresi (varsayılan :8080; "-" ise kapalı)
func runProbe(log *slog.Logger) error {
	maxChecks, _ := strconv.Atoi(env("MAX_CONCURRENT_CHECKS", "20"))
	client, err := probe.New(probe.Config{
		Server:        env("PROBE_SERVER", ""),
		Token:         env("PROBE_TOKEN", ""),
		Version:       version,
		MaxConcurrent: maxChecks,
		Log:           log,
	})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// İmajdaki HEALTHCHECK /healthz'i yoklar; kontrol noktası modunda da yanıt verilir.
	if addr := env("ADDR", ":8080"); addr != "-" {
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			io.WriteString(w, "ok")
		})
		srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Warn("sağlık kontrolü adresi dinlenemedi", "adres", addr, "hata", err)
			}
		}()
		defer srv.Close()
	}
	return client.Run(ctx)
}
