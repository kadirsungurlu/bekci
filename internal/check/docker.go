package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DockerConfig konteyner sağlığını Docker Engine API üzerinden kontrol eder.
//
// Güvenlik notu: docker.sock'a doğrudan erişim, konteynerdeki her şeye tam
// kök erişimiyle eşdeğerdir. Soketi salt okunur bağlayın (":ro") veya
// tercihen tecnativa/docker-socket-proxy gibi bir proxy'yi sadece
// CONTAINERS=1 ile açığa çıkarıp bu monitörü ona yönlendirin.
type DockerConfig struct {
	Endpoint  string `json:"endpoint"`  // unix:///var/run/docker.sock | tcp://host:2375 | http(s)://…
	Container string `json:"container"` // ad veya id
}

type dockerChecker struct{}

func init() { Register("docker", dockerChecker{}) }

func (dockerChecker) Normalize(raw json.RawMessage) (json.RawMessage, error) {
	var c DockerConfig
	if err := decode(raw, &c); err != nil {
		return nil, err
	}
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	if c.Endpoint == "" {
		c.Endpoint = "unix:///var/run/docker.sock"
	}
	if _, _, err := dockerHTTPClient(c.Endpoint); err != nil {
		return nil, invalid("Geçersiz endpoint: %v", err)
	}
	c.Container = strings.TrimSpace(c.Container)
	if c.Container == "" {
		return nil, invalid("Konteyner adı veya id gerekli")
	}
	return encode(c), nil
}

func (dockerChecker) Target(raw json.RawMessage) string {
	var c DockerConfig
	json.Unmarshal(raw, &c)
	return c.Container + " (" + c.Endpoint + ")"
}

// dockerHTTPClient endpoint'e göre bir http.Client ve istekler için taban
// URL üretir. unix:// bir soket üzerinden bağlanır; tcp:// düz HTTP'ye
// çevrilir; http(s):// (ör. docker-socket-proxy) olduğu gibi kullanılır.
func dockerHTTPClient(endpoint string) (*http.Client, string, error) {
	switch {
	case strings.HasPrefix(endpoint, "unix://"):
		sockPath := strings.TrimPrefix(endpoint, "unix://")
		if sockPath == "" {
			return nil, "", fmt.Errorf("soket yolu boş")
		}
		// Her kontrol kendi Transport'unu kurar; bağlantı havuzda bekletilmez
		// (DisableKeepAlives). Aksi halde her kontrolden kalan boşta bağlantı
		// ve okuma/yazma goroutine'leri hiç kapanmadan birikir.
		transport := &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", sockPath)
			},
			DisableKeepAlives: true,
		}
		return &http.Client{Transport: transport}, "http://docker", nil
	case strings.HasPrefix(endpoint, "tcp://"):
		host := strings.TrimPrefix(endpoint, "tcp://")
		return &http.Client{}, "http://" + host, nil
	case strings.HasPrefix(endpoint, "http://"), strings.HasPrefix(endpoint, "https://"):
		return &http.Client{}, strings.TrimRight(endpoint, "/"), nil
	default:
		return nil, "", fmt.Errorf("bilinmeyen endpoint biçimi (unix://, tcp:// veya http(s):// olmalı)")
	}
}

type dockerInspect struct {
	State struct {
		Status string `json:"Status"` // created, running, paused, restarting, removing, exited, dead
		Health *struct {
			Status string `json:"Status"` // starting, healthy, unhealthy
		} `json:"Health"`
	} `json:"State"`
}

var dockerStatusTR = map[string]string{
	"created":    "oluşturuldu",
	"running":    "çalışıyor",
	"paused":     "duraklatıldı",
	"restarting": "yeniden başlatılıyor",
	"removing":   "kaldırılıyor",
	"exited":     "sona erdi",
	"dead":       "öldü",
}

func (dockerChecker) Check(ctx context.Context, raw json.RawMessage) Result {
	var c DockerConfig
	if err := json.Unmarshal(raw, &c); err != nil {
		return down("Ayar okunamadı: " + err.Error())
	}
	client, base, err := dockerHTTPClient(c.Endpoint)
	if err != nil {
		return down("Endpoint geçersiz: " + err.Error())
	}
	if t, ok := client.Transport.(*http.Transport); ok {
		// Kontrole özel Transport: kalan bağlantılar kontrol bitince kapanır.
		defer t.CloseIdleConnections()
	}

	reqURL := base + "/containers/" + url.PathEscape(c.Container) + "/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return down("İstek oluşturulamadı: " + err.Error())
	}

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return down(describeErr(ctx, err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	ping := msSince(start)

	if resp.StatusCode == http.StatusNotFound {
		return down("Konteyner bulunamadı: " + c.Container)
	}
	if resp.StatusCode != http.StatusOK {
		// Uzak yanıt gövdesi mesaja EKLENMEZ: iç bir Docker soketine/servise
		// yönlendirilen istekte gövde, hedef verisini sızdırabilir. Yalnızca
		// durum kodu bırakılır.
		return down(fmt.Sprintf("Docker API hatası: HTTP %d", resp.StatusCode))
	}

	var info dockerInspect
	if err := json.Unmarshal(body, &info); err != nil {
		return down("Docker API yanıtı okunamadı: " + err.Error())
	}

	if info.State.Status != "running" {
		label := dockerStatusTR[info.State.Status]
		if label == "" {
			label = info.State.Status
		}
		return down("Konteyner çalışmıyor (durum: " + label + ")")
	}
	if info.State.Health != nil && info.State.Health.Status != "healthy" {
		return down("Konteyner çalışıyor ama sağlık durumu: " + info.State.Health.Status)
	}
	if info.State.Health != nil {
		return Result{Up: true, PingMs: ping, Message: "Çalışıyor (sağlıklı)"}
	}
	return Result{Up: true, PingMs: ping, Message: "Çalışıyor"}
}
