package api

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testInstallToken = "upr_0123456789abcdefghijABCDEFGHIJ0123456789abc"

// installCmdServer Linux amd64/arm64 ve Windows programları olan bir sunucu.
// Kendi platformunun programı (os.Executable) da özet üretir; Linux'ta
// çalışan testte linux/<GOARCH> için o kullanılır.
func installCmdServer(t *testing.T, image string) *Server {
	t.Helper()
	dir := t.TempDir()
	for _, f := range []string{"uptime-linux-amd64", "uptime-linux-arm64", "uptime-windows-amd64.exe"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("program "+f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{AgentDir: dir, ProbeImage: image}
}

// linuxInstallCommands sunucu ajanının Docker ve systemd komutları ile
// kontrol noktasının Docker komutu (imajlı ve imajsız).
func linuxInstallCommands(t *testing.T, server string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, image := range []string{"", "ghcr.io/kadir/uptime:latest"} {
		s := installCmdServer(t, image)
		d, sd, _ := s.serverSetupCommands(server, testInstallToken)
		amd, arm := s.linuxSHAs()
		p := dockerInstallCommand(probeEnvPath, installEnvLines(server, testInstallToken),
			"docker run -d --name uptime-probe --restart unless-stopped "+probeHardenFlags, image, "uptime-probe-bin", amd, arm)
		out["docker_agent"+image], out["probe"+image] = d, p
		if image == "" {
			out["systemd"] = sd
		}
	}
	return out
}

// Token hiçbir sürecin argümanında görünmemeli: yalnızca yapıştırılan
// metnin içinde, 0600 oluşturulan env dosyasına yazan heredoc'ta durur.
// İndirme adımları token (Authorization başlığı) taşımaz.
func TestInstallCommandsKeepTokenOutOfArgv(t *testing.T) {
	for name, cmd := range linuxInstallCommands(t, "https://uptime.example.com") {
		lines := strings.Split(cmd, "\n")
		if lines[0] != "sh <<'UPTIME_KURULUM'" || lines[len(lines)-1] != "UPTIME_KURULUM" || lines[1] != "set -e" {
			t.Errorf("%s: komut stdin'den okunan bir sh betiği olmalı:\n%s", name, cmd)
		}
		for _, bad := range []string{"Authorization", "--header", " -H ", "-e PROBE_TOKEN", "printf"} {
			if strings.Contains(cmd, bad) {
				t.Errorf("%s: komutta %q olmamalı:\n%s", name, bad, cmd)
			}
		}
		n := 0
		for i, l := range lines {
			if !strings.Contains(l, testInstallToken) {
				continue
			}
			n++
			if l != "PROBE_TOKEN="+testInstallToken {
				t.Errorf("%s: token env satırı dışında geçiyor: %q", name, l)
				continue
			}
			// Heredoc başlangıcını geriye doğru bul: önce 0600 boş dosya oluşturulmalı.
			j := i - 1
			for j >= 0 && !strings.HasPrefix(lines[j], "cat > ") {
				j--
			}
			if j < 1 || !strings.HasSuffix(lines[j], " <<'UPTIME_ENV'") {
				t.Errorf("%s: token heredoc içinde değil", name)
				continue
			}
			path := strings.TrimSuffix(strings.TrimPrefix(lines[j], "cat > "), " <<'UPTIME_ENV'")
			if lines[j-1] != "install -m 600 /dev/null "+path {
				t.Errorf("%s: env dosyası önce 0600 oluşturulmalı: %q", name, lines[j-1])
			}
			if name != "systemd" && !strings.Contains(cmd, "--env-file "+path+" ") {
				t.Errorf("%s: docker --env-file %s kullanmalı", name, path)
			}
		}
		if n != 1 {
			t.Errorf("%s: token %d kez geçiyor, 1 bekleniyordu", name, n)
		}
	}
}

// Linux komutları mimariyi seçer ve iki mimarinin özetini gömer; indirme
// ?os=linux&arch=$A ister.
func TestInstallCommandsPickArch(t *testing.T) {
	s := installCmdServer(t, "")
	amd, arm := s.linuxSHAs()
	if amd == "" || arm == "" || amd == arm {
		t.Fatalf("özetler: %q %q", amd, arm)
	}
	for name, cmd := range linuxInstallCommands(t, "https://uptime.example.com") {
		if strings.Contains(name, "ghcr.io") {
			continue // imajlı kurulum program indirmez
		}
		for _, want := range []string{`case "$(uname -m)" in`, "x86_64|amd64) A=amd64; S=" + amd + ";;",
			"aarch64|arm64) A=arm64; S=" + arm + ";;", "/api/probe/binary?os=linux&arch=$A", `echo "$S  `, "sha256sum -c -"} {
			if !strings.Contains(cmd, want) {
				t.Errorf("%s: %q yok:\n%s", name, want, cmd)
			}
		}
	}
}

// Programı sunucuda olmayan mimari için özetsiz (doğrulamasız) kurulum
// yapılmaz; o mimari açık bir mesajla durur. Windows da aynı.
func TestInstallCommandsRefuseMissingSHA(t *testing.T) {
	c := linuxArchCase("aaa", "", "exit 1")
	if strings.Contains(c, "A=arm64") || !strings.Contains(c, "linux/arm64 ajan programı yok") || !strings.Contains(c, "A=amd64; S=aaa;;") {
		t.Errorf("arm64 özetsiz: %s", c)
	}
	c = linuxArchCase("", "", "exit 1")
	if strings.Contains(c, "; S=") {
		t.Errorf("hiç özet yokken S atanmamalı: %s", c)
	}
	if w := windowsAgentCommand("https://x.example", "upr_x", ""); !strings.HasPrefix(w, "throw ") || strings.Contains(w, "Invoke-WebRequest") || strings.Contains(w, "upr_x") {
		t.Errorf("özetsiz windows komutu: %s", w)
	}
}

// Üretilen betikler POSIX sh'ta sözdizimsel olarak geçerli; mimari seçimi
// uname -m'e göre doğru çalışır (sh yoksa atlanır).
func TestInstallScriptsShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh yok")
	}
	check := func(name, script string) {
		t.Helper()
		cmd := exec.Command(sh, "-n")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: sh -n: %v %s\n%s", name, err, out, script)
		}
	}
	for _, server := range []string{"https://uptime.example.com", "http://10.0.0.5:8080"} {
		for name, cmd := range linuxInstallCommands(t, server) {
			check(name, cmd)
			lines := strings.Split(cmd, "\n")
			check(name+" gövde", strings.Join(lines[1:len(lines)-1], "\n"))
			if i := strings.Index(cmd, "sh -c '"); i >= 0 {
				inner := cmd[i+len("sh -c '"):]
				inner = inner[:strings.Index(inner, "'")]
				check(name+" konteyner", inner)
			}
		}
	}

	arch := func(machine, amd, arm string) (string, int) {
		script := `uname() { echo "` + machine + `"; }; ` + linuxArchCase(amd, arm, "exit 3") + `; echo "$A $S"`
		out, err := exec.Command(sh, "-c", script).CombinedOutput()
		code := 0
		if e, ok := err.(*exec.ExitError); ok {
			code = e.ExitCode()
		}
		return strings.TrimSpace(string(out)), code
	}
	for _, c := range []struct {
		machine, amd, arm, want string
		code                    int
	}{
		{"x86_64", "a1", "b2", "amd64 a1", 0},
		{"aarch64", "a1", "b2", "arm64 b2", 0},
		{"arm64", "a1", "b2", "arm64 b2", 0},
		{"aarch64", "a1", "", "linux/arm64 ajan programı yok", 3},
		{"x86_64", "", "b2", "linux/amd64 ajan programı yok", 3},
		{"riscv64", "a1", "b2", "Desteklenmeyen mimari: riscv64", 3},
	} {
		out, code := arch(c.machine, c.amd, c.arm)
		if code != c.code || !strings.Contains(out, c.want) {
			t.Errorf("%s (%q,%q): çıkış %d %q; beklenen %d %q", c.machine, c.amd, c.arm, code, out, c.code, c.want)
		}
	}
}

// http sunucuda env dosyasına ve Windows komutuna PROBE_ALLOW_INSECURE eklenir.
func TestInstallCommandsInsecure(t *testing.T) {
	s := installCmdServer(t, "")
	d, sd, w := s.serverSetupCommands("http://10.0.0.5:8080", testInstallToken)
	for _, c := range []string{d, sd} {
		if !strings.Contains(c, "\nPROBE_ALLOW_INSECURE=1\n") {
			t.Errorf("http'de PROBE_ALLOW_INSECURE yok:\n%s", c)
		}
	}
	if !strings.Contains(w, "$env:PROBE_ALLOW_INSECURE='1'") || !strings.Contains(w, "Remove-Item Env:PROBE_ALLOW_INSECURE") || strings.Contains(w, "Authorization") {
		t.Errorf("windows http: %s", w)
	}
	d, sd, w = s.serverSetupCommands("https://uptime.example.com", testInstallToken)
	for _, c := range []string{d, sd, w} {
		if strings.Contains(c, "PROBE_ALLOW_INSECURE") {
			t.Errorf("https'te PROBE_ALLOW_INSECURE olmamalı:\n%s", c)
		}
	}
}

// Program indirmesi IP başına dakikada binaryRatePerMin ile sınırlı.
func TestProbeBinaryRateLimit(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Date(2026, 9, 27, 12, 0, 5, 0, time.UTC).Unix())
	e := newEnv(t, func(s *Server) {
		s.now = func() time.Time { return time.Unix(now.Load(), 0) }
		s.AgentDir = t.TempDir()
		os.WriteFile(filepath.Join(s.AgentDir, "uptime-linux-arm64"), []byte("arm"), 0o755)
	})
	const url = "/api/probe/binary?os=linux&arch=arm64"
	for i := range binaryRatePerMin {
		if code, _, body := e.anon().rawReq("GET", url, nil, nil); code != 200 || string(body) != "arm" {
			t.Fatalf("%d. indirme: %d %q", i+1, code, body)
		}
	}
	code, hdr, _ := e.anon().rawReq("GET", url, nil, nil)
	if code != 429 || hdr.Get("Retry-After") != "55" {
		t.Fatalf("sınır aşımı: %d Retry-After=%q", code, hdr.Get("Retry-After"))
	}
	// Geçersiz istekler de sayılır (tarama yavaşlar).
	if code, _, _ := e.anon().rawReq("GET", "/api/probe/binary?os=../x", nil, nil); code != 429 {
		t.Fatalf("sınırdaki geçersiz istek: %d", code)
	}
	// Sonraki dakika yeni pencere.
	now.Add(60)
	if code, _, _ := e.anon().rawReq("GET", url, nil, nil); code != 200 {
		t.Fatalf("yeni pencere: %d", code)
	}
}

func TestIPRateLimiter(t *testing.T) {
	l := newIPRateLimiter(2)
	t0 := time.Unix(600, 0)
	for i, want := range []bool{true, true, false} {
		if ok, _ := l.allow("1.1.1.1", t0); ok != want {
			t.Fatalf("%d: %v", i, ok)
		}
	}
	if ok, _ := l.allow("2.2.2.2", t0); !ok {
		t.Fatal("başka IP ayrı sayılmalı")
	}
	if ok, _ := l.allow("1.1.1.1", t0.Add(time.Minute)); !ok {
		t.Fatal("yeni dakika")
	}
}
