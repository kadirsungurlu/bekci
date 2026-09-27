package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Ajan program dosyasının yolu ve SHA-256'sı. Kurulum komutu indirmeyi bu
// özetle doğrular (sürüm sabitleme): ajan yeniden başlatmada programı yeniden
// indirmez; yalnızca ilk kurulumda indirir ve özetle karşılaştırır. Böylece
// sunucu sonradan ele geçirilse bile filoya kendiliğinden zararlı program inmez.

// agentBinaryPath verilen platformun ajan programının bu sunucudaki yolu.
// Sunucunun kendi platformu için çalışan program (os.Executable), diğerleri
// için AgentDir'deki uptime-<os>-<arch>[.exe] dosyası. Yoksa "" döner.
func (s *Server) agentBinaryPath(goos, goarch string) string {
	if goos == runtime.GOOS && goarch == runtime.GOARCH {
		exe, err := os.Executable()
		if err != nil {
			return ""
		}
		return exe
	}
	if s.AgentDir == "" {
		return ""
	}
	file := "uptime-" + goos + "-" + goarch
	if goos == "windows" {
		file += ".exe"
	}
	return filepath.Join(s.AgentDir, file)
}

type shaEntry struct {
	sum     string
	size    int64
	modUnix int64
}

var (
	shaMu    sync.Mutex
	shaCache = map[string]shaEntry{}
)

// agentBinarySHA256 platformun ajan programının SHA-256'sını (hex) döner.
// Dosya değişmediyse (boyut+değişim zamanı) önbellekten verir. Dosya yoksa "".
func (s *Server) agentBinarySHA256(goos, goarch string) string {
	path := s.agentBinaryPath(goos, goarch)
	if path == "" {
		return ""
	}
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	shaMu.Lock()
	if e, ok := shaCache[path]; ok && e.size == st.Size() && e.modUnix == st.ModTime().Unix() {
		shaMu.Unlock()
		return e.sum
	}
	shaMu.Unlock()

	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	sum := hex.EncodeToString(h.Sum(nil))
	shaMu.Lock()
	shaCache[path] = shaEntry{sum: sum, size: st.Size(), modUnix: st.ModTime().Unix()}
	shaMu.Unlock()
	return sum
}
