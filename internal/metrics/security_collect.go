package metrics

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
)

// Güvenlik raporu toplayıcısı (iş 731, docs/GUVENLIK-EKLENTI-730.md §A.1, §A.3).
// İşletim sistemine bağlı bölümler security_linux.go, security_windows.go,
// security_other.go'da (osSecUpdates, osSecReboot); ayrıştırıcılar security_parse.go.
// SSH girişleri, oturumlar ve güvenlik duvarı (732): security_auth.go.

const (
	secUpdatesTTL    = time.Hour        // paket sorgusu en çok saatte bir (önbellekli)
	secUpdatesTTLWin = 6 * time.Hour    // Windows Update sorgusu (734)
	secCmdTimeout    = 20 * time.Second // komut başına
	secPkgTimeout    = 60 * time.Second // paket yöneticisi sorgusu
)

// secEnv işletim sistemine bağlı bölümlere verilen ortam (testte sahtesi).
type secEnv struct {
	root      string // HOST_ROOT ("" ajan doğrudan host'ta)
	container bool   // konteynerde: host'un paket yöneticisi ve araçları çalıştırılamaz
	run       func(ctx context.Context, timeout time.Duration, name string, args ...string) cmdResult
	look      func(name string) bool // komut PATH'te var mı
	alive     func(pid int) bool     // süreç yaşıyor mu (utmp süzgeci)
}

// cmdResult komut sonucu. Missing: komut bulunamadı; Err: başlatılamadı ya da
// zaman aşımı (Code anlamsız).
type cmdResult struct {
	Out, Stderr string
	Code        int
	Missing     bool
	Err         error
}

// secCache bekleyen güncellemeler önbelleği (Collector.sec).
type secCache struct {
	mu   sync.Mutex
	at   time.Time
	upd  *SecUpdates
	note string
}

// Security güvenlik raporunu toplar. Güncelleme sorgusu secUpdatesTTL boyunca
// önbellekten gelir (CheckedAt gerçek sorgunun zamanı); yeniden başlatma
// gereksinimi her çağrıda yeniden bakılır. Toplanamayan bölüm nil kalır,
// nedeni Notes'ta.
func (c *Collector) Security(ctx context.Context) Security {
	now := c.opts.Now()
	s := Security{Time: now.UnixMilli(), Notes: map[string]string{}}
	e := secEnv{
		root:      strings.TrimRight(c.opts.Getenv("HOST_ROOT"), "/"),
		container: c.opts.InContainer() || (c.src == nil && c.reason != ""),
		run:       runCmd,
		look:      func(n string) bool { _, err := exec.LookPath(n); return err == nil },
		alive:     procAlive,
	}
	var code string
	s.Updates, code = c.securityUpdates(ctx, e, now)
	if code != "" {
		s.Notes[SecSectionUpdates] = code
	}
	s.Reboot, code = osSecReboot(ctx, e)
	if code != "" {
		s.Notes[SecSectionReboot] = code
	}
	c.securityExtras(ctx, e, now, &s)
	s.Sanitize()
	if len(s.Notes) == 0 {
		s.Notes = nil
	}
	return s
}

// securityUpdates önbellekli paket sorgusu. Kilit, önbellek eksikliği ve hata
// önbelleğe alınmaz: sonraki turda yeniden denenir.
func (c *Collector) securityUpdates(ctx context.Context, e secEnv, now time.Time) (*SecUpdates, string) {
	if e.container {
		return nil, NoteNoHost
	}
	sc := &c.sec
	sc.mu.Lock()
	defer sc.mu.Unlock()
	ttl := secUpdatesTTL
	if runtime.GOOS == "windows" {
		ttl = secUpdatesTTLWin
	}
	if sc.at.IsZero() || now.Sub(sc.at) >= ttl || now.Before(sc.at) {
		upd, note := osSecUpdates(ctx, e)
		if ctx.Err() != nil && upd == nil {
			return nil, NoteFailed
		}
		if upd != nil {
			upd.CheckedAt = c.opts.Now().UnixMilli()
		}
		switch note {
		case NoteLocked, NoteFailed, NoteCacheMissing:
			return upd, note
		}
		sc.at, sc.upd, sc.note = now, upd, note
	}
	if sc.upd == nil {
		return nil, sc.note
	}
	u := *sc.upd
	u.Packages = slices.Clone(u.Packages)
	return &u, sc.note
}

// runCmd komutu kabuksuz, LC_ALL=C ile ve süre sınırıyla çalıştırır.
func runCmd(ctx context.Context, timeout time.Duration, name string, args ...string) cmdResult {
	path, err := exec.LookPath(name)
	if err != nil {
		return cmdResult{Missing: true, Err: err}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var out, serr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &serr
	cmd.WaitDelay = 2 * time.Second
	err = cmd.Run()
	r := cmdResult{Out: out.String(), Stderr: serr.String()}
	var ee *exec.ExitError
	switch {
	case ctx.Err() != nil:
		r.Err = ctx.Err()
	case errors.As(err, &ee):
		r.Code = ee.ExitCode()
	case err != nil:
		r.Err = err
	}
	return r
}

// newestMtime kalıplara uyan dosyaların en yeni değişiklik zamanı (unix sn; 0 yok).
func newestMtime(patterns ...string) int64 {
	var best int64
	for _, p := range patterns {
		files, _ := filepath.Glob(p)
		for _, f := range files {
			if st, err := os.Stat(f); err == nil {
				best = max(best, st.ModTime().Unix())
			}
		}
	}
	return best
}
