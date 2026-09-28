package main

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/kadirsungurlu/bekci/internal/brand"
)

// Windows hizmetinin yapılandırma ve günlük dosyaları. Platformdan bağımsız
// kısım burada (Linux'ta da test edilir); hizmetin kendisi service_windows.go.

// agentEnvKeys kurulumda ortam değişkenlerinden yapılandırma dosyasına
// aktarılan ayarlar (bkz. probe.go).
var agentEnvKeys = []string{"PROBE_SERVER", "PROBE_TOKEN", "PROBE_ALLOW_INSECURE", "ADDR", "METRICS", "MAX_CONCURRENT_CHECKS", "LOG_LEVEL"}

// parseEnvFile KEY=DEĞER satırlarını okur. Boş satırlar ve # ile başlayan
// yorumlar atlanır; değerin çevresindeki tek/çift tırnak kaldırılır.
func parseEnvFile(data string) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF")) // Not Defteri'nin BOM'u
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if !ok || k == "" {
			continue
		}
		if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
			v = v[1 : len(v)-1]
		}
		out[k] = v
	}
	return out
}

// formatEnvFile yapılandırma dosyasının içeriği (anahtar sırasıyla, CRLF).
func formatEnvFile(kv map[string]string) string {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString("# " + brand.Name + " ajanı ayarları (uptime service install yazar). Token içerir:\r\n")
	b.WriteString("# yalnızca SYSTEM ve Administrators okuyabilir. Değişiklikten sonra hizmeti yeniden başlatın.\r\n")
	for _, k := range keys {
		b.WriteString(k + "=" + kv[k] + "\r\n")
	}
	return b.String()
}

// mergeAgentEnv mevcut dosya ayarlarının üzerine ortam değişkenlerinde verilen
// ayarları yazar (boş olanlar mevcut değeri korur: token'sız yeniden kurulum
// eski token'la devam eder). Sağlık uç noktası varsayılan olarak kapalıdır
// (systemd kurulumundaki gibi ADDR=-). PROBE_SERVER ve PROBE_TOKEN zorunludur.
func mergeAgentEnv(cur map[string]string, getenv func(string) string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range cur {
		out[k] = v
	}
	for _, k := range agentEnvKeys {
		v := strings.TrimSpace(getenv(k))
		if strings.ContainsAny(v, "\r\n") {
			return nil, errors.New(k + " satır sonu içeremez")
		}
		if v != "" {
			out[k] = v
		}
	}
	if out["ADDR"] == "" {
		out["ADDR"] = "-"
	}
	if out["PROBE_SERVER"] == "" || out["PROBE_TOKEN"] == "" {
		return nil, errors.New("PROBE_SERVER ve PROBE_TOKEN ortam değişkenleri gerekli (kurulum komutunu panelden kopyalayın)")
	}
	return out, nil
}

// rotatingFile boyutu sınırlı günlük dosyası: limit baytı aşınca dosya
// ".1" uzantısıyla saklanır (öncekinin yerine) ve yenisine başlanır.
type rotatingFile struct {
	mu    sync.Mutex
	path  string
	limit int64
	f     *os.File
	size  int64
}

func openRotatingFile(path string, limit int64) (*rotatingFile, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return nil, err
	}
	r := &rotatingFile{path: path, limit: limit}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *rotatingFile) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	r.f, r.size = f, st.Size()
	return nil
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.size > 0 && r.size+int64(len(p)) > r.limit {
		r.f.Close()
		r.f = nil
		os.Remove(r.path + ".1")
		os.Rename(r.path, r.path+".1")
	}
	if r.f == nil {
		if err := r.open(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *rotatingFile) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}
