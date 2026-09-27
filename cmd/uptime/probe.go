package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os/signal"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/kadirsa1105/uptime-kadir-app/internal/probe"
)

// runProbe "uptime probe" komutu: uzak kontrol noktası / sunucu ajanı modu.
// Veritabanı açılmaz; atanan monitörler ana sunucudan alınır, sonuçlar oraya
// gönderilir. Ana sunucu isterse bulunduğu sunucunun metrikleri (CPU, RAM,
// disk, ağ, sıcaklık, Docker konteynerleri) de gönderilir.
//
// Ortam değişkenleri:
//
//	PROBE_SERVER           ana sunucunun adresi, ör. https://uptime.kadir.app
//	PROBE_TOKEN            arayüzde (Ayarlar → Kontrol noktaları) verilen token (upr_…)
//	MAX_CONCURRENT_CHECKS  aynı anda en fazla kontrol (varsayılan 20)
//	ADDR                   sağlık kontrolü (/healthz) adresi (varsayılan :8080; "-" ise kapalı)
//	METRICS                "0" ise sunucu metrikleri hiç toplanmaz, ana sunucu istese de
//	HOST_PROC, HOST_SYS,   Docker içinde çalışırken host'un /proc, /sys, /etc ve kök
//	HOST_ETC, HOST_ROOT    dizinlerinin bağlandığı yerler (ör. /host/proc … /host).
//	                       Konteynerde HOST_PROC yoksa metrik toplanmaz (konteynerin
//	                       kendi değerleri yanıltır), ana sunucuya neden bildirilir.
//	DOCKER_HOST            Docker API adresi (varsayılan unix:///var/run/docker.sock);
//	                       erişilebilirse konteyner istatistikleri de gönderilir
//
// Docker kurulumu (host ağı ve süreçleri, kök dizin salt okunur):
//
//	docker run -d --name uptime-agent --restart unless-stopped --network host --pid host \
//	  -v /:/host:ro -v /var/run/docker.sock:/var/run/docker.sock:ro \
//	  -e HOST_PROC=/host/proc -e HOST_SYS=/host/sys -e HOST_ETC=/host/etc -e HOST_ROOT=/host \
//	  -e ADDR=- -e PROBE_SERVER=… -e PROBE_TOKEN=upr_… …
func runProbe(log *slog.Logger) error {
	maxChecks, _ := strconv.Atoi(env("MAX_CONCURRENT_CHECKS", "20"))
	client, err := probe.New(probe.Config{
		Server:        env("PROBE_SERVER", ""),
		Token:         env("PROBE_TOKEN", ""),
		Version:       version,
		MaxConcurrent: maxChecks,
		NoMetrics:     slices.Contains([]string{"0", "false", "off", "no"}, strings.ToLower(env("METRICS", "1"))),
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
