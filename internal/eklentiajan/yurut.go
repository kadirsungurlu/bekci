package eklentiajan

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

// UnitPrefix geçici birimlerin adı: bekci-eklenti-<id>-<rastgele>.
const UnitPrefix = "bekci-eklenti-"

// wrapper gövdeyi stdin'den okur ve stdin'i kapatır (680 §6.2), sonra
// eklentiyi timeout(1) altında çalıştırır. Süre kafesin İÇİNDE sayılır:
// CPUQuota=20% altında birim kurulumu (ad alanları) saniyeler sürebilir ve
// RuntimeMaxSec bu kurulumu da sayar; RuntimeMaxSec yalnız geniş bir yedektir.
// timeout 124 (TERM) ya da 137 (KILL, -k sonrası) döner.
const wrapper = `b=$(cat); exec </dev/null; exec timeout -k %d %d /bin/sh -c "$b"`

// setupSlack RuntimeMaxSec yedeğinin kurulum payı.
const setupSlack = 60

// Kafes sınırları (730 §B.1). Eklenti kısa ve hafif olmalı; fazlası hata.
const (
	memoryMax   = "128M"
	cpuQuota    = "20%"
	tasksMax    = "32"
	stopSec     = 5
	maxStderr   = 1 << 10
	minSystemd  = 254 // D5
	pidsSystemd = 257 // PrivatePIDs
)

// Gizlenen yollar: ajan belirteci, yerel güven kökü, kayıt, betik durumu.
var hidden = []string{"/etc/uptime-agent.env", "/etc/bekci", "/var/lib/bekci-betik", "/var/log/bekci-eklenti", "/var/log/bekci-betik"}

// Spec tek çalıştırma.
type Spec struct {
	Unit    string
	RunAs   string // nobody | root
	Network bool
	Body    []byte
	Timeout time.Duration
}

// Outcome çalıştırma sonucu.
type Outcome struct {
	ExitCode  int // bilinmiyorsa -1
	Stdout    []byte
	Truncated bool
	Stderr    []byte // ilk 1 KiB
	TimedOut  bool
	Elapsed   time.Duration
	Err       error // başlatılamadı
}

// Executor yürütme katmanı; testler sahtesini verir.
type Executor interface {
	Run(ctx context.Context, s Spec) Outcome
}

// Systemd gerçek yürütücü: eklenti ajanın alt süreci değildir, PID 1 başlatır.
type Systemd struct {
	PrivatePIDs bool
}

// NewUnit benzersiz birim adı.
func NewUnit(id string) string {
	var b [4]byte
	rand.Read(b[:])
	return UnitPrefix + id + "-" + hex.EncodeToString(b[:]) + ".service"
}

// Args systemd-run argümanları. Ortam aktarılmaz: birimin ortamı PID 1'den.
func (e *Systemd) Args(s Spec) []string {
	a := []string{"--quiet", "--collect", "--wait", "--pipe", "--expand-environment=no", "--unit=" + s.Unit,
		"--setenv=LC_ALL=C", "--setenv=PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	prop := func(v string) { a = append(a, "-p", v) }
	sec := max(int(s.Timeout/time.Second), 1)
	prop(fmt.Sprintf("RuntimeMaxSec=%d", sec+stopSec+setupSlack))
	prop(fmt.Sprintf("TimeoutStopSec=%d", stopSec))
	prop("MemoryMax=" + memoryMax)
	prop("MemorySwapMax=0")
	prop("CPUQuota=" + cpuQuota)
	prop("TasksMax=" + tasksMax)
	prop("ProtectSystem=strict")
	prop("ProtectHome=read-only")
	prop("PrivateTmp=yes")
	prop("NoNewPrivileges=yes")
	prop("ProtectKernelTunables=yes")
	prop("ProtectKernelModules=yes")
	prop("ProtectControlGroups=yes")
	if !s.Network {
		prop("PrivateNetwork=yes")
	}
	if e.PrivatePIDs {
		prop("PrivatePIDs=yes")
	}
	for _, p := range hidden {
		prop("InaccessiblePaths=-" + p)
	}
	if s.RunAs != "root" {
		prop("User=nobody")
	}
	return append(a, "/bin/sh", "-c", fmt.Sprintf(wrapper, stopSec, sec))
}

// Run birimi başlatır, bitene dek bekler. RuntimeMaxSec süreyi birim içinde
// keser; istemci takılırsa güvenlik sayacı birimi durdurur.
func (e *Systemd) Run(ctx context.Context, s Spec) Outcome {
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return Outcome{ExitCode: -1, Err: err}
	}
	out := &headBuf{limit: eklenti.MaxOutput + 1} // fazlası: ayrıştırıcı kesildiğini görür
	errb := &headBuf{limit: maxStderr}
	cmd := exec.Command(path, e.Args(s)...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	cmd.Stdin = bytes.NewReader(s.Body)
	cmd.Stdout, cmd.Stderr = out, errb
	cmd.WaitDelay = 5 * time.Second
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Outcome{ExitCode: -1, Err: err}
	}
	stop := func() {
		exec.Command("systemctl", "stop", s.Unit).Run()
		cmd.Process.Kill()
	}
	guard := time.AfterFunc(s.Timeout+(2*stopSec+setupSlack)*time.Second, stop)
	defer guard.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-done:
		}
	}()
	err = cmd.Wait()
	o := Outcome{Stdout: out.b, Truncated: out.over, Stderr: errb.b, Elapsed: time.Since(start)}
	var xe *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &xe):
		o.ExitCode = xe.ExitCode()
	default:
		o.ExitCode = -1
	}
	o.TimedOut = o.ExitCode == 124 || o.ExitCode == 137 || o.Elapsed >= s.Timeout+(stopSec+setupSlack)*time.Second
	return o
}

// headBuf ilk limit baytı tutar, kalanı boşaltır (eklenti tıkanmaz).
type headBuf struct {
	b     []byte
	limit int
	n     int
	over  bool
}

func (h *headBuf) Write(p []byte) (int, error) {
	h.n += len(p)
	if room := h.limit - len(h.b); room > 0 {
		h.b = append(h.b, p[:min(room, len(p))]...)
	}
	h.over = h.n > h.limit
	return len(p), nil
}

var systemdVerRe = regexp.MustCompile(`^systemd (\d+)`)

// SystemdVersion yerel systemd sürümü; Linux değilse ya da yoksa 0.
func SystemdVersion() int {
	if runtime.GOOS != "linux" {
		return 0
	}
	out, err := exec.Command("systemctl", "--version").Output()
	if err != nil {
		return 0
	}
	m := systemdVerRe.FindSubmatch(out)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n
}

// NewSystemd platform uygunsa yürütücü, değilse nil (D5: Linux + systemd >= 254).
func NewSystemd() *Systemd {
	v := SystemdVersion()
	if v < minSystemd {
		return nil
	}
	return &Systemd{PrivatePIDs: v >= pidsSystemd}
}
