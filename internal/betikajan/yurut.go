package betikajan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// UnitPrefix geçici birimlerin adı; ardından yalnız run_id gelir.
const UnitPrefix = "bekci-run-"

// wrapper gövdeyi stdin'den okur, stdin'i kapatır: alt komutlar kalan gövdeyi
// yiyemez. trap: systemd SIGTERM ile durdurulan süreci "temiz" sayar ve
// systemd-run 0 döner; durdurulan bir betik başarılı görünmesin diye 143.
// Sınır: durdurma kabuk trap'i kurmadan gelirse rc yine 0 olabilir ya da TERM
// boşa gidip SIGKILL'e kalabilir. Bu yüzden iptal ve kapanış rc'den çıkarılmaz,
// ajanın kendi bayrağından gelir (bkz. Agent.finish).
const wrapper = `trap 'exit 143' TERM; b=$(cat); exec </dev/null; eval "$b"`

const (
	stopSec    = 10
	memoryMax  = "512M"
	cpuQuota   = "100%"
	tasksMax   = "512"
	agentEnv   = "/etc/uptime-agent.env"
	spareAfter = 60 * time.Second // RuntimeMaxSec'ten sonra istemciye tanınan pay
)

// Spec tek yürütme.
type Spec struct {
	RunID     string
	RunAs     string
	Body      []byte
	Env       map[string]string // BEKCI_* (değerleri argv'ye girmez)
	Timeout   time.Duration
	OutputMax int
}

// Outcome yürütme sonucu.
type Outcome struct {
	ExitCode     int // bilinmiyorsa -1
	Output       []byte
	Truncated    bool
	OutputSHA256 string
	Elapsed      time.Duration
	Err          error // süreç başlatılamadı
}

// Executor yürütme katmanı. Testler sahtesini verir.
type Executor interface {
	Run(ctx context.Context, s Spec) Outcome
	Stop(runID string) error
	Units() ([]string, error) // çalışan bekci-run-* birimlerinin run_id'leri
}

// Systemd gerçek yürütücü: betik ajanın alt süreci olamaz (ajanın birimi
// NoNewPrivileges ve dar yetenek kümesiyle çalışır), PID 1 başlatır.
type Systemd struct {
	PrivatePIDs bool
	Hide        []string // betiğe görünmeyecek ek yollar
}

// Args systemd-run argümanları. Parametre değerleri burada yoktur: yalnız
// --setenv=AD, değer çağıranın ortamından alınır.
func (e *Systemd) Args(s Spec) []string {
	a := []string{
		"--quiet", "--collect", "--wait", "--pipe", "--expand-environment=no",
		"--unit=" + UnitPrefix + s.RunID,
	}
	keys := make([]string, 0, len(s.Env))
	for k := range s.Env {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		a = append(a, "--setenv="+k)
	}
	prop := func(v string) { a = append(a, "-p", v) }
	prop(fmt.Sprintf("RuntimeMaxSec=%d", int(s.Timeout/time.Second)))
	prop(fmt.Sprintf("TimeoutStopSec=%d", stopSec))
	prop("MemoryMax=" + memoryMax)
	prop("CPUQuota=" + cpuQuota)
	prop("TasksMax=" + tasksMax)
	prop("PrivateTmp=yes")
	prop("ProtectHome=read-only")
	if e.PrivatePIDs {
		prop("PrivatePIDs=yes")
	}
	prop("InaccessiblePaths=-" + agentEnv)
	for _, p := range e.Hide {
		prop("InaccessiblePaths=-" + p)
	}
	if s.RunAs != "" && s.RunAs != "root" {
		prop("User=" + s.RunAs)
	}
	return append(a, "/bin/sh", "-c", wrapper)
}

// Run birimi başlatır, bitene dek bekler. Birim PID 1'de olduğundan ctx iptali
// onu durdurmaz: durdurma Stop ile yapılır. ctx yalnız istemci takılırsa kurtarır.
func (e *Systemd) Run(ctx context.Context, s Spec) Outcome {
	path, err := exec.LookPath("systemd-run")
	if err != nil {
		return Outcome{ExitCode: -1, Err: err}
	}
	col := newCollector(s.OutputMax)
	cmd := exec.Command(path, e.Args(s)...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin"}
	for k, v := range s.Env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stdin = bytes.NewReader(s.Body)
	cmd.Stdout, cmd.Stderr = col, col
	cmd.WaitDelay = 5 * time.Second
	start := time.Now()
	if err := cmd.Start(); err != nil {
		return Outcome{ExitCode: -1, Err: err}
	}
	guard := time.AfterFunc(s.Timeout+stopSec*time.Second+spareAfter, func() {
		e.Stop(s.RunID)
		cmd.Process.Kill()
	})
	defer guard.Stop()
	err = cmd.Wait()
	out := col.outcome()
	out.Elapsed = time.Since(start)
	var xe *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &xe):
		out.ExitCode = xe.ExitCode()
	default:
		out.ExitCode = -1
	}
	return out
}

func unitName(runID string) string { return UnitPrefix + runID + ".service" }

// Stop birimi durdurur (SIGTERM, TimeoutStopSec sonra SIGKILL).
func (e *Systemd) Stop(runID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), (stopSec+20)*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "stop", unitName(runID)).Run()
}

// Units çalışan bekci-run-* birimlerini listeler.
func (e *Systemd) Units() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "list-units", "--plain", "--no-legend", "--all",
		UnitPrefix+"*.service").Output()
	if err != nil {
		return nil, err
	}
	return parseUnits(string(out)), nil
}

func parseUnits(out string) []string {
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		id, ok := strings.CutPrefix(f[0], UnitPrefix)
		if !ok {
			continue
		}
		if id, ok = strings.CutSuffix(id, ".service"); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// collector çıktının başını ve sonunu tutar, tamamının özetini alır, boru hep
// boşaltılır: betik çıktı yüzünden tıkanmaz.
type collector struct {
	head, tail []byte
	tailPos    int
	tailFull   bool
	total      int64
	sum        hash.Hash
}

func newCollector(limit int) *collector {
	h := limit / 2
	return &collector{head: make([]byte, 0, h), tail: make([]byte, limit-h), sum: sha256.New()}
}

func (c *collector) Write(p []byte) (int, error) {
	c.sum.Write(p)
	c.total += int64(len(p))
	n := len(p)
	if room := cap(c.head) - len(c.head); room > 0 {
		k := min(room, len(p))
		c.head = append(c.head, p[:k]...)
		p = p[k:]
	}
	for len(p) > 0 && len(c.tail) > 0 {
		k := copy(c.tail[c.tailPos:], p)
		p = p[k:]
		c.tailPos += k
		if c.tailPos == len(c.tail) {
			c.tailPos, c.tailFull = 0, true
		}
	}
	return n, nil
}

func (c *collector) outcome() Outcome {
	var tail []byte
	if c.tailFull {
		tail = append(append(tail, c.tail[c.tailPos:]...), c.tail[:c.tailPos]...)
	} else {
		tail = c.tail[:c.tailPos]
	}
	dropped := c.total - int64(len(c.head)) - int64(len(tail))
	o := Outcome{OutputSHA256: hex.EncodeToString(c.sum.Sum(nil))}
	o.Output = append(o.Output, c.head...)
	if dropped > 0 {
		o.Truncated = true
		o.Output = append(o.Output, fmt.Sprintf("\n[... %d bayt kısaltıldı ...]\n", dropped)...)
	}
	o.Output = append(o.Output, tail...)
	return o
}
