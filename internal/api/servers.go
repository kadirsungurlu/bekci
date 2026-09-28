package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/kadirsa1105/uptime-kadir-app/internal/metrics"
	"github.com/kadirsa1105/uptime-kadir-app/internal/notify"
	"github.com/kadirsa1105/uptime-kadir-app/internal/servers"
	"github.com/kadirsa1105/uptime-kadir-app/internal/store"
)

// Sunucu takibi (docs/PLAN.md §12) -----------------------------------------------------
//
// Her kontrol noktası aynı zamanda bir sunucu ajanıdır: host'u görebiliyorsa
// dakikada bir metrik örneği gönderir (POST /api/probe/metrics). Sunucu
// ekranları izleyicilere de açıktır; müşteri kısıtlı izleyiciler (yalnızca
// kendisine atanmış monitörleri gören) sunucuları hiç göremez.

const (
	serverEventsDays  = 90
	serverEventsLimit = 200
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/servers", s.auth(s.serversOnly(s.listServers)))
		mux.Handle("POST /api/servers", s.admin(s.createServer))
		mux.Handle("GET /api/servers/{id}", s.auth(s.serversOnly(s.getServer)))
		mux.Handle("GET /api/servers/{id}/stats", s.auth(s.serversOnly(s.serverStats)))
		mux.Handle("GET /api/servers/{id}/events", s.auth(s.serversOnly(s.serverEvents)))
		mux.Handle("PUT /api/servers/{id}/alerts", s.editor(s.putServerAlerts))
		mux.Handle("PUT /api/servers/{id}/notifications", s.editor(s.putServerNotifications))

		mux.Handle("POST /api/probe/metrics", s.probeOnly(s.probeMetrics))
	})
}

// serverProbe kimliği verilen sunucuyu okur. Kayıt bir kontrol noktasıysa
// (iki tür birbirinin ekranında görünmez) veya kısıtlı kullanıcıya atanmamışsa
// bulunamadı sayılır (varlığı da belli olmasın).
func (s *Server) serverProbe(r *http.Request, id int64) (store.Probe, error) {
	p, err := s.store.GetProbe(r.Context(), id)
	if err == nil && (p.Kind != store.ProbeKindServer || !visibleTo(userFrom(r)).canServer(p.ID)) {
		return store.Probe{}, store.ErrNotFound
	}
	return p, err
}

// serversOnly kendisine hiç sunucu atanmamış müşteri kısıtlı izleyiciyi
// sunucu ekranlarından uzak tutar; atanmışsa yalnızca onları görür.
func (s *Server) serversOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if u := userFrom(r); u.Restricted() && len(u.ServerIDs) == 0 {
			writeError(w, http.StatusForbidden, "Sunuculara erişim yetkiniz yok")
			return
		}
		h(w, r)
	}
}

func (s *Server) listServers(w http.ResponseWriter, r *http.Request) {
	probes, err := s.store.ListProbesOfKind(r.Context(), store.ProbeKindServer)
	if err != nil {
		s.dbError(w, err)
		return
	}
	vis := visibleTo(userFrom(r))
	probes = slices.DeleteFunc(probes, func(p store.Probe) bool { return !vis.canServer(p.ID) })
	rules, err := s.store.AllServerAlerts(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := make([]servers.View, len(probes))
	for i, p := range probes {
		out[i] = s.servers.View(r.Context(), p, rules[p.ID], false)
	}
	writeJSON(w, http.StatusOK, map[string]any{"servers": out})
}

// serverDetail sunucu detayı: görünüm + kurallar + kanallar.
type serverDetail struct {
	servers.View
	Alerts          []store.ServerAlert `json:"alerts"`
	NotificationIDs []int64             `json:"notification_ids"`
}

func (s *Server) getServer(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	rules, err := s.store.ServerAlerts(r.Context(), id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	ids := []int64{}
	// Bildirim bağlantıları monitörlerde olduğu gibi yalnızca editör ve yöneticiye.
	if canSeeConfig(userFrom(r)) {
		if ids, err = s.store.ProbeNotificationIDs(r.Context(), id); err != nil {
			s.dbError(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, serverDetail{View: s.servers.View(r.Context(), p, rules, true), Alerts: rules, NotificationIDs: ids})
}

func (s *Server) serverStats(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "24h"
	}
	series, ok, err := s.servers.Series(r.Context(), id, rng)
	if !ok {
		writeError(w, http.StatusBadRequest, "Aralık 1h, 24h, 7d veya 30d olmalı")
		return
	}
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, series)
}

// serverEventView uyarı geçmişi kaydı; ended_at sürüyorsa null.
type serverEventView struct {
	ID        int64   `json:"id"`
	Metric    string  `json:"metric"`
	Mount     string  `json:"mount"`
	Value     float64 `json:"value"`
	Threshold float64 `json:"threshold"`
	StartedAt int64   `json:"started_at"`
	EndedAt   *int64  `json:"ended_at"`
}

func (s *Server) serverEvents(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if _, err := s.serverProbe(r, id); err != nil {
		s.dbError(w, err)
		return
	}
	since := s.now().AddDate(0, 0, -serverEventsDays).Unix()
	list, err := s.store.ServerAlertEvents(r.Context(), id, since, serverEventsLimit)
	if err != nil {
		s.dbError(w, err)
		return
	}
	out := make([]serverEventView, len(list))
	for i, e := range list {
		out[i] = serverEventView{ID: e.ID, Metric: e.Metric, Mount: e.Mount, Value: e.Value, Threshold: e.Threshold, StartedAt: e.StartedAt}
		if e.EndedAt != 0 {
			out[i].EndedAt = &e.EndedAt
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": out})
}

type alertInput struct {
	Metric    string  `json:"metric"`
	Mount     string  `json:"mount"` // yalnızca disk: bölüm ("" = en dolu bölüm)
	Threshold float64 `json:"threshold"`
	Minutes   int     `json:"minutes"`
	Active    *bool   `json:"active"`
}

// diskMountRe disk kuralındaki bölüm: Linux bağlama noktası (/home) veya
// Windows sürücüsü/klasöre bağlı birim (D:, C:\Veri); denetim karakteri yok.
var diskMountRe = regexp.MustCompile(`^(/|[A-Za-z]:(\\|$))[^\x00-\x1f]*$`)

// maxAlertRules bir sunucudaki en fazla kural (disk kuralları bölüm başına ayrıdır).
const maxAlertRules = 40

// validateAlerts kuralları doğrular ve kayıt biçimine çevirir.
func validateAlerts(in []alertInput) ([]store.ServerAlert, error) {
	if len(in) > maxAlertRules {
		return nil, fmt.Errorf("En fazla %d kural olabilir", maxAlertRules)
	}
	seen := map[string]bool{}
	out := make([]store.ServerAlert, 0, len(in))
	for _, a := range in {
		a.Metric = strings.TrimSpace(a.Metric)
		if !servers.ValidMetric(a.Metric) {
			return nil, errors.New("Metrik cpu, mem, swap, disk, load, temp veya offline olmalı")
		}
		a.Mount = strings.TrimSpace(a.Mount)
		switch {
		case a.Metric != servers.MetricDisk:
			a.Mount = "" // bölüm yalnızca disk kuralında anlamlı
		case a.Mount != "" && (!diskMountRe.MatchString(a.Mount) || len([]rune(a.Mount)) > metrics.MaxText):
			return nil, errors.New("Disk bölümü / ile ya da sürücü harfiyle başlamalı, ör. /home veya D:")
		}
		if key := a.Metric + "\x00" + a.Mount; seen[key] {
			if a.Mount != "" {
				return nil, fmt.Errorf("%s bölümü için birden fazla disk kuralı olamaz", a.Mount)
			}
			return nil, fmt.Errorf("%s için birden fazla kural olamaz", a.Metric)
		} else {
			seen[key] = true
		}
		if a.Minutes < 1 || a.Minutes > 60 {
			return nil, errors.New("Süre 1-60 dakika olmalı")
		}
		if a.Metric == servers.MetricOffline {
			a.Threshold = 0 // kullanılmaz
		} else if lo, hi := servers.ThresholdRange(a.Metric); a.Threshold != a.Threshold || a.Threshold < lo || a.Threshold > hi {
			return nil, fmt.Errorf("%s eşiği %s ile %s arasında olmalı", a.Metric,
				strconv.FormatFloat(lo, 'f', -1, 64), strconv.FormatFloat(hi, 'f', -1, 64))
		}
		out = append(out, store.ServerAlert{Metric: a.Metric, Mount: a.Mount, Threshold: a.Threshold, Minutes: a.Minutes, Active: a.Active == nil || *a.Active})
	}
	return out, nil
}

// alertSummary işlem kaydı için kısa özet: "cpu %90/10 dk, offline 3 dk (kapalı)".
func alertSummary(rules []store.ServerAlert) string {
	if len(rules) == 0 {
		return "kural yok"
	}
	parts := make([]string, len(rules))
	for i, a := range rules {
		p := a.Metric + " "
		if a.Mount != "" {
			p += a.Mount + " "
		}
		if a.Metric != servers.MetricOffline {
			p += notify.FormatMetric(a.Metric, a.Threshold) + "/"
		}
		p += strconv.Itoa(a.Minutes) + " dk"
		if !a.Active {
			p += " (kapalı)"
		}
		parts[i] = p
	}
	return strings.Join(parts, ", ")
}

func (s *Server) putServerAlerts(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in struct {
		Alerts []alertInput `json:"alerts"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	rules, err := validateAlerts(in.Alerts)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.store.ReplaceServerAlerts(r.Context(), id, rules, s.now().Unix())
	if err != nil {
		s.dbError(w, err)
		return
	}
	s.audit(r, store.User{}, "server.alerts", "probe", id, p.Name, alertSummary(saved))
	s.servers.Publish(r.Context(), id)
	writeJSON(w, http.StatusOK, map[string]any{"alerts": saved})
}

func (s *Server) putServerNotifications(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	p, err := s.serverProbe(r, id)
	if err != nil {
		s.dbError(w, err)
		return
	}
	var in struct {
		NotificationIDs []int64 `json:"notification_ids"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	ids := []int64{}
	for _, nid := range in.NotificationIDs {
		if !slices.Contains(ids, nid) {
			ids = append(ids, nid)
		}
	}
	slices.Sort(ids)
	names, err := s.store.NotificationNames(r.Context(), ids)
	if err != nil {
		s.dbError(w, err)
		return
	}
	list := make([]string, 0, len(ids))
	for _, nid := range ids {
		name, ok := names[nid]
		if !ok {
			writeError(w, http.StatusBadRequest, "Seçilen bildirim kanalı bulunamadı")
			return
		}
		list = append(list, name)
	}
	if err := s.store.SetProbeNotifications(r.Context(), id, ids); err != nil {
		s.dbError(w, err)
		return
	}
	detail := "kanal yok"
	if len(list) > 0 {
		detail = "kanallar: " + strings.Join(list, ", ")
	}
	s.audit(r, store.User{}, "server.notifications", "probe", id, p.Name, detail)
	writeJSON(w, http.StatusOK, map[string]any{"notification_ids": ids})
}

// probeMetrics ajanın metrik örneği. Bilinmeyen alanlar kabul edilir (yeni
// sürüm ajan eski sunucuya da gönderebilsin). Örneğin zamanı olarak sunucunun
// alış zamanı kullanılır. Metrik toplama kapalıysa örnek yok sayılır (ajan iş
// listesinde metrics_interval=0 görünce göndermeyi bırakır).
func (s *Server) probeMetrics(w http.ResponseWriter, r *http.Request) {
	p := probeFrom(r)
	var in metrics.Sample
	body := http.MaxBytesReader(w, r.Body, metrics.MaxBodyBytes)
	if err := json.NewDecoder(body).Decode(&in); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
			return
		}
		writeError(w, http.StatusBadRequest, "Geçersiz istek gövdesi: "+err.Error())
		return
	}
	if _, err := io.Copy(io.Discard, body); err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "İstek gövdesi çok büyük")
		return
	}
	err := s.servers.Ingest(r.Context(), p, in)
	switch {
	case errors.Is(err, servers.ErrEmptySample):
		writeError(w, http.StatusBadRequest, "Örnekte stats veya unavailable alanı olmalı")
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusUnauthorized, "Geçersiz kontrol noktası token'ı")
	case err != nil:
		s.dbError(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// serverSetupCommands ajan kurulum komutları (§12.2): host'u gören Docker
// konteyneri, doğrudan kurulum (systemd) ve Windows hizmeti (PowerShell).
// Program token'sız indirilir ve komuta gömülü SHA-256 ile doğrulanır; token
// yalnızca 0600 izinli env dosyasına heredoc ile yazılır (hiçbir sürecin
// argümanında görünmez, bkz. installcmd.go). Aynı komut tekrar çalıştırılınca
// ajan güncellenir.
func (s *Server) serverSetupCommands(server, token string) (dockerAgent, systemd, windows string) {
	const hostFlags = "--network host --pid host -v /:/host:ro,rslave -v /var/run/docker.sock:/var/run/docker.sock:ro " +
		"-e HOST_PROC=/host/proc -e HOST_SYS=/host/sys -e HOST_ETC=/host/etc -e HOST_ROOT=/host -e ADDR=- "
	amd, arm := s.linuxSHAs()
	dockerAgent = dockerInstallCommand(agentEnvPath, installEnvLines(server, token),
		"docker run -d --name uptime-agent --restart unless-stopped "+serverHardenFlags+hostFlags, s.ProbeImage, "uptime-agent-bin", amd, arm)

	// systemd: sunucunun mimarisine uygun program /usr/local/bin/uptime'a iner
	// ve SHA-256 ile doğrulanır; yalnızca bu komut tekrar çalıştırıldığında
	// yenilenir (hizmet yeniden başlarken indirmez). Token yalnızca root'un
	// okuyabildiği env dosyasında. Sertleştirme: ayrıcalık yükseltme kapalı,
	// bellek sınırlı, geçici dizin özel.
	body := []string{
		linuxArchCase(amd, arm, "exit 1"),
		`U="` + server + `/api/probe/binary?os=linux&arch=$A"`,
		"F=/usr/local/bin/uptime",
		`if command -v curl >/dev/null 2>&1; then curl -fsSL -o "$F.new" "$U"; else wget -qO "$F.new" "$U"; fi`,
		`if ! echo "$S  $F.new" | sha256sum -c - >/dev/null; then rm -f "$F.new"; echo "Program özeti (SHA-256) uyuşmuyor: kurulum komutunu panelden yenileyin" >&2; exit 1; fi`,
		`chmod 755 "$F.new"`,
		`mv -f "$F.new" "$F"`,
	}
	body = append(body, envFileSteps(agentEnvPath, installEnvLines(server, token, "ADDR=-"))...)
	body = append(body,
		"cat > /etc/systemd/system/uptime-agent.service <<'UPTIME_UNIT'",
		"[Unit]", "Description=Uptime agent", "After=network-online.target", "Wants=network-online.target", "",
		"[Service]", "EnvironmentFile="+agentEnvPath,
		"ExecStart=/usr/local/bin/uptime probe", "Restart=always", "RestartSec=10",
		"NoNewPrivileges=yes", "PrivateTmp=yes", "MemoryMax=256M", "CapabilityBoundingSet=CAP_NET_RAW", "",
		"[Install]", "WantedBy=multi-user.target",
		"UPTIME_UNIT",
		"systemctl daemon-reload",
		"systemctl enable uptime-agent",
		"systemctl restart uptime-agent",
		`echo "Uptime ajanı kuruldu ve başlatıldı (durum: systemctl status uptime-agent)"`,
	)
	systemd = rootScript(body...)
	// Windows komutu Windows programının kendi özetiyle doğrular (sunucunun
	// Linux programının özeti değil).
	return dockerAgent, systemd, windowsAgentCommand(server, token, s.agentBinarySHA256("windows", "amd64"))
}

// windowsAgentCommand Yönetici PowerShell'de çalıştırılan tek satırlık kurulum
// (Windows PowerShell 5.1 ve PowerShell 7). Program %ProgramFiles%\Uptime'a
// geçici adla token'sız indirilir ve SHA-256 ile doğrulanır; "uptime service
// install" ayarları yalnızca yöneticilerin okuyabildiği
// %ProgramFiles%\Uptime\agent.env'e yazar, çalışan hizmeti durdurup programı
// değiştirir, hizmeti kurar/günceller (hata olursa yeniden başlat) ve başlatır.
// Token komut satırı argümanı olarak değil, oturumun ortam değişkeniyle
// verilir (süreç listesinde görünmez) ve sonunda silinir. http sunucuda
// PROBE_ALLOW_INSECURE=1 de aynı yolla verilir. PSReadLine 2.2+ "token" geçen
// satırı geçmiş dosyasına yazmaz; eski sürümler için arayüzde not vardır.
// İlerleme çubuğu 5.1'de indirmeyi çok yavaşlattığı için kapatılır. Windows
// programı sunucuda yoksa (özet boş) doğrulamasız kurulum yapılmaz: komut
// açıklamalı bir hatayla durur.
func windowsAgentCommand(server, token, sha string) string {
	if sha == "" {
		return `throw "Ana sunucuda Windows (amd64) ajan programı yok; programı AGENT_DIR klasörüne uptime-windows-amd64.exe adıyla koyun (resmi Docker imajı içerir) ve komutu panelden yenileyin"`
	}
	// İndirilen program, indirmeden önce hesaplanan SHA-256 ile doğrulanır;
	// uymuyorsa kurulum durur. İndirme veya kurulum hata verse de (finally)
	// geçici program ve token oturum ortamından silinir.
	inner := []string{
		"$d=Join-Path $env:ProgramFiles 'Uptime'",
		"New-Item -ItemType Directory -Force -Path $d | Out-Null",
		"$f=Join-Path $d 'uptime-setup.exe'",
		`Invoke-WebRequest -UseBasicParsing -Uri "$env:PROBE_SERVER/api/probe/binary?os=windows&arch=amd64" -OutFile $f`,
		fmt.Sprintf(`if ((Get-FileHash $f -Algorithm SHA256).Hash -ne '%s') { throw "Program bütünlük doğrulaması başarısız (SHA-256 uyuşmuyor)" }`, strings.ToUpper(sha)),
		"& $f service install", "$c=$LASTEXITCODE",
	}
	parts := []string{
		"$ErrorActionPreference='Stop'",
		"$ProgressPreference='SilentlyContinue'",
		"[Net.ServicePointManager]::SecurityProtocol=[Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12",
		"$env:PROBE_SERVER=" + psQuote(server),
		"$env:PROBE_TOKEN=" + psQuote(token),
	}
	cleanup := "Remove-Item Env:PROBE_TOKEN -ErrorAction SilentlyContinue"
	if strings.HasPrefix(server, "http://") {
		parts = append(parts, "$env:PROBE_ALLOW_INSECURE='1'")
		cleanup += "; Remove-Item Env:PROBE_ALLOW_INSECURE -ErrorAction SilentlyContinue"
	}
	return strings.Join(append(parts,
		"$f=$null; $c=1",
		"try { "+strings.Join(inner, "; ")+" } finally { if ($f) { Remove-Item $f -Force -ErrorAction SilentlyContinue }; "+cleanup+" }",
		`if ($c -ne 0) { throw "Kurulum tamamlanamadı (çıkış kodu $c)" }`,
	), "; ")
}

// psQuote PowerShell tek tırnaklı metni (içindeki ' iki kez yazılır; $ ve `
// tek tırnakta yorumlanmaz).
func psQuote(v string) string { return "'" + strings.ReplaceAll(v, "'", "''") + "'" }
