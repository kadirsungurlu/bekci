package check

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDockerServer sahte Docker Engine API'sini bir unix soketi üzerinden
// sunar (gerçek docker.sock olmadan test edilir).
func fakeDockerServer(t *testing.T) *httptest.Server {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/containers/"), "/json")
		switch name {
		case "saglikli":
			fmt.Fprint(w, `{"State":{"Status":"running","Health":{"Status":"healthy"}}}`)
		case "hasta":
			fmt.Fprint(w, `{"State":{"Status":"running","Health":{"Status":"unhealthy"}}}`)
		case "kontrolsuz":
			fmt.Fprint(w, `{"State":{"Status":"running"}}`)
		case "durdu":
			fmt.Fprint(w, `{"State":{"Status":"exited"}}`)
		case "yeniden":
			fmt.Fprint(w, `{"State":{"Status":"restarting"}}`)
		case "sunucu-hatasi":
			// Yanıt gövdesinde iç veri: mesaja sızmamalı.
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"DAHILI-SIZINTI-DEGERI"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"message":"No such container"}`)
		}
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: mux}}
	srv.Start()
	t.Cleanup(srv.Close)
	// srv.URL bir http:// TCP adresi göstermez (unix listener); testte
	// endpoint'i sockPath'ten elle kuruyoruz.
	srv.URL = "unix://" + sockPath
	return srv
}

func TestDockerNormalize(t *testing.T) {
	c, _ := Get("docker")
	bad := []string{
		`{}`,
		`{"container":"x","endpoint":"ftp://x"}`,
		`{"container":"x","bilinmeyen":1}`,
	}
	for _, b := range bad {
		if _, err := c.Normalize(json.RawMessage(b)); err == nil {
			t.Errorf("%s: doğrulama hatası bekleniyordu", b)
		} else {
			var ve ValidationError
			if !errors.As(err, &ve) {
				t.Errorf("%s: ValidationError bekleniyordu, %v geldi", b, err)
			}
		}
	}
	norm, err := c.Normalize(json.RawMessage(`{"container":"web"}`))
	if err != nil {
		t.Fatal(err)
	}
	var cfg DockerConfig
	json.Unmarshal(norm, &cfg)
	if cfg.Endpoint != "unix:///var/run/docker.sock" {
		t.Errorf("varsayılan endpoint yanlış: %+v", cfg)
	}
}

func TestDockerHealthy(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "saglikli"})
	if !r.Up || r.Message != "Çalışıyor (sağlıklı)" {
		t.Errorf("sağlıklı konteyner UP olmalı: %+v", r)
	}
}

func TestDockerUnhealthy(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "hasta"})
	if r.Up || !strings.Contains(r.Message, "unhealthy") {
		t.Errorf("sağlıksız konteyner DOWN olmalı: %+v", r)
	}
}

func TestDockerRunningNoHealthcheck(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "kontrolsuz"})
	if !r.Up || r.Message != "Çalışıyor" {
		t.Errorf("sağlık kontrolü olmayan çalışan konteyner UP olmalı: %+v", r)
	}
}

func TestDockerExited(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "durdu"})
	if r.Up || !strings.Contains(r.Message, "sona erdi") {
		t.Errorf("sona ermiş konteyner DOWN olmalı: %+v", r)
	}
}

func TestDockerRestarting(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "yeniden"})
	if r.Up || !strings.Contains(r.Message, "yeniden başlatılıyor") {
		t.Errorf("yeniden başlayan konteyner DOWN olmalı: %+v", r)
	}
}

func TestDockerNotFound(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "yok-boyle-bir-sey"})
	if r.Up || !strings.Contains(r.Message, "bulunamadı") {
		t.Errorf("olmayan konteyner DOWN olmalı: %+v", r)
	}
}

func TestDockerServerErrorNoBodyLeak(t *testing.T) {
	srv := fakeDockerServer(t)
	r := run(t, "docker", map[string]any{"endpoint": srv.URL, "container": "sunucu-hatasi"})
	if r.Up || !strings.Contains(r.Message, "Docker API hatası: HTTP 500") {
		t.Errorf("500 hatası bekleniyordu: %+v", r)
	}
	if strings.Contains(r.Message, "DAHILI-SIZINTI-DEGERI") {
		t.Errorf("uzak yanıt gövdesi mesaja sızdı: %q", r.Message)
	}
}

// TestDockerLive gerçek Docker soketine karşı çalışır (salt okunur bağlanmış
// olması beklenir). UPTIME_IT_DOCKER_CONTAINER, bu soket üzerinde çalışan
// gerçek bir konteynerin adı/id'sidir. UPTIME_IT_DOCKER_ENDPOINT boşsa
// varsayılan unix:///var/run/docker.sock kullanılır.
func TestDockerLive(t *testing.T) {
	container := os.Getenv("UPTIME_IT_DOCKER_CONTAINER")
	if container == "" {
		t.Skip("UPTIME_IT_DOCKER_CONTAINER ayarlı değil, canlı test atlanıyor")
	}
	endpoint := os.Getenv("UPTIME_IT_DOCKER_ENDPOINT")
	cfg := map[string]any{"container": container}
	if endpoint != "" {
		cfg["endpoint"] = endpoint
	}
	r := run(t, "docker", cfg)
	if !r.Up {
		t.Errorf("canlı Docker kontrolü başarısız: %+v", r)
	}
}

func TestDockerSocketMissing(t *testing.T) {
	r := run(t, "docker", map[string]any{"endpoint": "unix:///tmp/uptime-test-olmayan-soket.sock", "container": "x"})
	if r.Up {
		t.Errorf("olmayan soket DOWN olmalı: %+v", r)
	}
}
