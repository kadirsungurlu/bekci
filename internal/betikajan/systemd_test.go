//go:build linux

package betikajan

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

// Gerçek systemd testleri. Geçici birim açar, root ister; yalnız
// BEKCI_TEST_SYSTEMD=1 ile çalışır.
func needSystemd(t *testing.T) Platform {
	t.Helper()
	if os.Getenv("BEKCI_TEST_SYSTEMD") != "1" {
		t.Skip("BEKCI_TEST_SYSTEMD=1 yok")
	}
	p := DetectPlatform()
	if !p.OK {
		t.Skip(p.Detail)
	}
	return p
}

func unitID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	rand.Read(b)
	id := hex.EncodeToString(b)
	t.Cleanup(func() { (&Systemd{}).Stop(id) })
	return id
}

func realRun(t *testing.T, e *Systemd, body string, f func(*Spec)) Outcome {
	t.Helper()
	s := Spec{RunID: unitID(t), RunAs: "root", Body: []byte(body), Timeout: 30 * time.Second, OutputMax: 1 << 20, Env: map[string]string{}}
	if f != nil {
		f(&s)
	}
	o := e.Run(context.Background(), s)
	if o.Err != nil {
		t.Fatal(o.Err)
	}
	return o
}

func TestSystemdExitAndOutput(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	o := realRun(t, e, "echo cikti\necho hata >&2\nexit 7\n", nil)
	if o.ExitCode != 7 || !strings.Contains(string(o.Output), "cikti") || !strings.Contains(string(o.Output), "hata") {
		t.Fatalf("%+v %q", o, o.Output)
	}
	if o.OutputSHA256 != betik.Digest(o.Output) {
		t.Fatal("özet tutmuyor")
	}
}

// Parametre ortamdan gelir, argv'de görünmez; kabuk ve systemd açmaz.
func TestSystemdParamsLiteral(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	val := `$(touch /run/bekci-hat1-enj)${HOME}%n%i` + "`id`"
	o := realRun(t, e, `printf '[%s]' "$BEKCI_P_AD"`, func(s *Spec) { s.Env["BEKCI_P_AD"] = val })
	if string(o.Output) != "["+val+"]" {
		t.Fatalf("%q", o.Output)
	}
	if _, err := os.Stat("/run/bekci-hat1-enj"); err == nil {
		os.Remove("/run/bekci-hat1-enj")
		t.Fatal("parametre komut çalıştırdı")
	}
	args := e.Args(Spec{RunID: "x", Env: map[string]string{"BEKCI_P_AD": val}, Timeout: time.Second})
	if strings.Contains(strings.Join(args, " "), "touch") || !slices.Contains(args, "--setenv=BEKCI_P_AD") {
		t.Fatalf("%v", args)
	}
}

// Gövdeyi alt komut okuyamaz: sonraki satırlar yutulmaz.
func TestSystemdStdinClosed(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	o := realRun(t, e, "read x; echo \"[$x]\"\necho ikinci\n", nil)
	if string(o.Output) != "[]\nikinci\n" {
		t.Fatalf("%q", o.Output)
	}
}

// Betik ajanın ortamını ve gizli yolları göremez.
func TestSystemdSecretsHidden(t *testing.T) {
	p := needSystemd(t)
	t.Setenv("PROBE_TOKEN", "upr_gizli")
	dir := "/run/bekci-hat1-" + unitID(t)
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	secret := filepath.Join(dir, "anahtar")
	os.WriteFile(secret, []byte("OZEL"), 0o600)

	// Kontrol: gizleme yokken okunuyor, yoksa test anlamsız olur.
	open := realRun(t, &Systemd{PrivatePIDs: p.PrivatePIDs}, "env; cat "+secret, nil)
	if !strings.Contains(string(open.Output), "OZEL") {
		t.Fatalf("kontrol: %q", open.Output)
	}
	if strings.Contains(string(open.Output), "upr_gizli") {
		t.Fatal("token betik ortamında")
	}
	o := realRun(t, &Systemd{PrivatePIDs: p.PrivatePIDs, Hide: []string{dir}}, "cat "+secret, nil)
	if strings.Contains(string(o.Output), "OZEL") || o.ExitCode == 0 {
		t.Fatalf("%+v %q", o, o.Output)
	}
}

func TestSystemdLargeOutputBounded(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	o := realRun(t, e, "echo bas; head -c 5000000 /dev/zero | tr '\\0' x; echo; echo son\n", func(s *Spec) { s.OutputMax = 4096 })
	if !o.Truncated || len(o.Output) > 8192 || o.ExitCode != 0 {
		t.Fatalf("kesik=%v boy=%d rc=%d", o.Truncated, len(o.Output), o.ExitCode)
	}
	if !strings.HasPrefix(string(o.Output), "bas") || !strings.Contains(string(o.Output), "son") {
		t.Fatalf("baş+son yok")
	}
}

// Zaman aşımı arka plan çocukları da öldürür.
func TestSystemdTimeoutKillsChildren(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	mark := fmt.Sprintf("%d", 40000+time.Now().UnixNano()%20000)
	start := time.Now()
	o := realRun(t, e, "sleep "+mark+" &\nsleep "+mark+"\n", func(s *Spec) { s.Timeout = 2 * time.Second })
	if o.ExitCode == 0 {
		t.Fatalf("%+v", o)
	}
	if d := time.Since(start); d < 2*time.Second || d > 15*time.Second {
		t.Fatalf("süre %s", d)
	}
	if out, _ := exec.Command("pgrep", "-f", "sleep "+mark).Output(); len(out) != 0 {
		t.Fatalf("çocuk yaşıyor: %s", out)
	}
}

// stopRun betik gerçekten başladıktan sonra durdurur (işaret dosyası): birim
// listede göründüğü anda kabuk daha trap kurmamış olabilir.
func stopRun(t *testing.T, body string) (Outcome, time.Duration) {
	t.Helper()
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	id := unitID(t)
	mark := "/run/bekci-hat1-" + id
	defer os.Remove(mark)
	done := make(chan Outcome, 1)
	go func() {
		done <- e.Run(context.Background(), Spec{RunID: id, RunAs: "root", Body: []byte(body + "\ntouch " + mark + "\nsleep 300\n"), Timeout: 60 * time.Second, OutputMax: 1024})
	}()
	for end := time.Now().Add(15 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(mark); err == nil {
			break
		}
		if time.Now().After(end) {
			t.Fatal("betik başlamadı")
		}
	}
	st := time.Now()
	if err := e.Stop(id); err != nil {
		t.Logf("stop: %v", err)
	}
	took := time.Since(st)
	select {
	case o := <-done:
		if u, _ := e.Units(); slices.Contains(u, id) {
			t.Fatal("birim kaldı")
		}
		return o, took
	case <-time.After(30 * time.Second):
		t.Fatal("durmadı")
	}
	return Outcome{}, 0
}

// Durdurulan betik hızlı biter, başarılı görünmez.
func TestSystemdStopNotSuccess(t *testing.T) {
	o, took := stopRun(t, ":")
	if o.ExitCode != 143 || took > 5*time.Second {
		t.Fatalf("rc=%d stop=%s", o.ExitCode, took)
	}
}

// TERM'i yok sayan betik TimeoutStopSec sonra SIGKILL ile biter, başarılı görünmez.
func TestSystemdStopIgnoredTerm(t *testing.T) {
	o, took := stopRun(t, "trap '' TERM")
	if o.ExitCode == 0 || took < (stopSec-1)*time.Second || took > (stopSec+5)*time.Second {
		t.Fatalf("rc=%d stop=%s", o.ExitCode, took)
	}
}

// Açılış anında durdurma: trap henüz kurulmamış olabilir, rc güvenilmez (0 da
// olabilir; bu yüzden ajan iptali rc'den değil bayraktan bilir). Burada aranan:
// birim her durumda sınırlı sürede biter, betik çalışmaya devam etmez.
func TestSystemdStopDuringStartup(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	for range 3 {
		id := unitID(t)
		done := make(chan Outcome, 1)
		go func() {
			done <- e.Run(context.Background(), Spec{RunID: id, RunAs: "root", Body: []byte("sleep 300\n"), Timeout: 60 * time.Second, OutputMax: 1024})
		}()
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
			if u, _ := e.Units(); slices.Contains(u, id) {
				break
			}
		}
		e.Stop(id)
		select {
		case <-done:
		case <-time.After((stopSec + 15) * time.Second):
			t.Fatal("durmadı")
		}
		if u, _ := e.Units(); slices.Contains(u, id) {
			t.Fatal("birim kaldı")
		}
	}
}

func TestSystemdRunAsNobody(t *testing.T) {
	p := needSystemd(t)
	e := &Systemd{PrivatePIDs: p.PrivatePIDs}
	o := realRun(t, e, "id -un\n", func(s *Spec) { s.RunAs = "nobody" })
	if strings.TrimSpace(string(o.Output)) != "nobody" {
		t.Fatalf("%q", o.Output)
	}
}

// Uçtan uca: gerçek ajan + gerçek birim; durum/anahtar betiğe görünmez.
func TestSystemdAgentEndToEnd(t *testing.T) {
	p := needSystemd(t)
	h := newHarness(t, Allow{ID: "temizle", MinApproval: "signed"})
	dir := "/run/bekci-hat1-" + unitID(t)
	os.MkdirAll(dir, 0o700)
	defer os.RemoveAll(dir)
	// PrivateTmp /tmp'yi zaten gizler; durum dizinini gerçek yola taşı.
	state := filepath.Join(dir, "state")
	if out, err := exec.Command("cp", "-a", h.paths.StateDir, state).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	h.paths.StateDir = state
	plat := Platform{OK: true, PrivatePIDs: p.PrivatePIDs}
	h.agent = New(Options{Paths: h.paths, Platform: &plat, Now: func() time.Time { return h.clock }})
	h.agent.capTTL = 0
	h.agent.SetPanelSecure(true)

	body := "cat " + filepath.Join(state, keyFile) + " && echo OKUDU\necho $BEKCI_RUN_ID\n"
	h.manifestBase.SHA256 = betik.Digest([]byte(body))
	h.manifestBase.Approval = "signed"
	j := h.job(nil)
	j.Body = []byte(body)
	j.Ticket = h.ticket(&j, nil)
	h.handle(j)
	names := h.agent.queue.names()
	if len(names) != 1 {
		t.Fatalf("kuyruk %v, olay %+v", names, h.lastEvent())
	}
	ev, _ := h.agent.queue.get(names[0])
	env, _ := betik.ParseEnvelope(ev.Receipt)
	r, err := betik.ParseReceipt(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(ev.Output), "OKUDU") || !strings.Contains(string(ev.Output), j.RunID) {
		t.Fatalf("makbuz %+v çıktı %q", r, ev.Output)
	}
}
