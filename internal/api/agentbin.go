package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"time"
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

// binaryRatePerMin GET /api/probe/binary için IP başına dakikada en fazla
// indirme. Uç nokta token istemediği için büyük dosyanın tekrar tekrar
// indirilmesiyle bant genişliği tüketimine karşı sınırlanır; gerçek kurulumlar
// dakikada bir iki indirme yapar.
const binaryRatePerMin = 10

// ipRateLimiter IP başına dakikalık istek sınırı (sabit pencere; probeLimiter
// ve rozet sınırıyla aynı desen).
type ipRateLimiter struct {
	mu    sync.Mutex
	limit int
	m     map[string]*probeWindow
}

func newIPRateLimiter(limit int) *ipRateLimiter {
	return &ipRateLimiter{limit: limit, m: map[string]*probeWindow{}}
}

// allow isteğe izin verilip verilmediğini ve verilmediyse kaç saniye
// beklenmesi gerektiğini döner.
func (l *ipRateLimiter) allow(ip string, now time.Time) (bool, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	minute := now.Unix() / 60
	w := l.m[ip]
	if w == nil || w.start != minute {
		if len(l.m) > 10000 { // bellek şişmesin: eski pencereleri at
			for k, v := range l.m {
				if v.start != minute {
					delete(l.m, k)
				}
			}
		}
		w = &probeWindow{start: minute}
		l.m[ip] = w
	}
	w.count++
	if w.count > l.limit {
		return false, int(60 - now.Unix()%60)
	}
	return true, 0
}

// binaryRateLimit ajan programı indirmesini IP başına sınırlar. Sınırlayıcı
// rota kaydında (sunucu başına bir kez) oluşur.
func (s *Server) binaryRateLimit(h http.HandlerFunc) http.Handler {
	rl := newIPRateLimiter(binaryRatePerMin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok, wait := rl.allow(clientIP(r), s.now()); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(wait))
			writeError(w, http.StatusTooManyRequests, "Çok fazla indirme isteği; biraz sonra tekrar deneyin")
			return
		}
		h(w, r)
	})
}
