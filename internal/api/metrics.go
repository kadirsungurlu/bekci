package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Prometheus metrikleri -------------------------------------------------------------
//
//	GET /metrics   (metin biçimi 0.0.4)
//
// API anahtarı gerekir: "Authorization: Bearer upk_…" veya Basic kimlik
// doğrulama (kullanıcı adı "metrics", şifre API anahtarı). Anahtarın izleyici
// yetkisi yeterlidir; monitör kısıtlı kullanıcının anahtarı yalnızca izinli
// monitörleri görür.
//
// Örnek prometheus.yml:
//
//	scrape_configs:
//	  - job_name: uptime
//	    scheme: https
//	    static_configs: [{targets: ["uptime.kadir.app"]}]
//	    basic_auth: {username: metrics, password: upk_...}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.HandleFunc("GET /metrics", s.metrics)
	})
}

// metricsKey isteğin taşıdığı API anahtarı (Bearer veya Basic şifresi).
func metricsKey(r *http.Request) string {
	if k, ok := bearerAPIKey(r); ok {
		return k
	}
	if _, pw, ok := r.BasicAuth(); ok {
		return pw
	}
	return ""
}

// promLabel etiket değerini metin biçimine göre kaçırır: \ " ve satır sonu.
func promLabel(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(strings.ToValidUTF8(s, "�"))
}

func promFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	u, ok := s.userForAPIKey(r, metricsKey(r))
	if !ok || store.RoleRank(u.Role) == 0 {
		w.Header().Set("WWW-Authenticate", `Basic realm="`+brand.Name+` metrics", charset="UTF-8"`)
		http.Error(w, "API anahtarı gerekiyor", http.StatusUnauthorized)
		return
	}
	ctx := r.Context()
	monitors, err := s.store.ListMonitors(ctx)
	if err != nil {
		s.dbError(w, err)
		return
	}
	vis := visibleTo(u)
	visible := monitors[:0]
	for _, m := range monitors {
		if vis.can(m.ID) {
			visible = append(visible, m)
		}
	}
	now := s.now().Unix()
	windows := []struct {
		name string
		sec  int64
	}{{"24h", 86400}, {"7d", 7 * 86400}, {"30d", 30 * 86400}}
	counts := make([]map[int64]store.UpDown, len(windows))
	for i, win := range windows {
		if counts[i], err = s.store.UptimeCounts(ctx, now-win.sec); err != nil {
			s.dbError(w, err)
			return
		}
	}

	var b strings.Builder
	family := func(name, typ, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	labels := func(m store.Monitor) string {
		return fmt.Sprintf(`monitor_id="%d",monitor_name="%s",monitor_type="%s"`, m.ID, promLabel(m.Name), promLabel(m.Type))
	}

	family("uptime_app_info", "gauge", "Uygulama sürümü")
	fmt.Fprintf(&b, "uptime_app_info{version=\"%s\"} 1\n", promLabel(s.version))

	family("uptime_monitor_status", "gauge", "Monitör durumu (0 = çalışmıyor, 1 = çalışıyor, 2 = bekliyor)")
	for _, m := range visible {
		fmt.Fprintf(&b, "uptime_monitor_status{%s} %d\n", labels(m), m.Status)
	}
	family("uptime_monitor_active", "gauge", "Monitör etkin mi (0 = durduruldu, 1 = etkin)")
	for _, m := range visible {
		active := 0
		if m.Active {
			active = 1
		}
		fmt.Fprintf(&b, "uptime_monitor_active{%s} %d\n", labels(m), active)
	}
	family("uptime_monitor_response_time_ms", "gauge", "Son kontrolün yanıt süresi (milisaniye)")
	for _, m := range visible {
		if m.LastCheckAt > 0 && m.LastPingMs > 0 {
			fmt.Fprintf(&b, "uptime_monitor_response_time_ms{%s} %d\n", labels(m), m.LastPingMs)
		}
	}
	family("uptime_monitor_domain_days_remaining", "gauge", "Alan adının bitmesine kalan gün (geçmişse negatif)")
	for _, m := range visible {
		if m.DomainExpiresAt > 0 {
			fmt.Fprintf(&b, "uptime_monitor_domain_days_remaining{%s} %s\n", labels(m), promFloat(float64(m.DomainExpiresAt-now)/86400))
		}
	}
	family("uptime_monitor_cert_days_remaining", "gauge", "SSL sertifikasının bitmesine kalan gün (geçmişse negatif)")
	for _, m := range visible {
		if m.CertExpiresAt > 0 {
			fmt.Fprintf(&b, "uptime_monitor_cert_days_remaining{%s} %s\n", labels(m), promFloat(float64(m.CertExpiresAt-now)/86400))
		}
	}
	family("uptime_monitor_uptime_ratio", "gauge", "Çalışma oranı (0-1), saatlik özetlerden")
	for _, m := range visible {
		for i, win := range windows {
			c := counts[i][m.ID]
			if c.Up+c.Down == 0 {
				continue
			}
			fmt.Fprintf(&b, "uptime_monitor_uptime_ratio{%s,window=\"%s\"} %s\n", labels(m), win.name, promFloat(float64(c.Up)/float64(c.Up+c.Down)))
		}
	}
	family("uptime_monitor_slow", "gauge", "Yanıt süresi eşiği aşılmış mı (1 = yavaş; yalnızca eşik tanımlı monitörler)")
	for _, m := range visible {
		if m.SlowMs > 0 {
			fmt.Fprintf(&b, "uptime_monitor_slow{%s} %d\n", labels(m), boolMetric(m.Slow))
		}
	}

	if err := s.agentMetrics(r, &b, family, vis); err != nil {
		s.dbError(w, err)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.Write([]byte(b.String()))
}
