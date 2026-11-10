package metrics

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func secFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "sec", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func pkgMap(pkgs []SecPkg) map[string]SecPkg {
	m := map[string]SecPkg{}
	for _, p := range pkgs {
		m[p.Name] = p
	}
	return m
}

func countSec(pkgs []SecPkg) int {
	n := 0
	for _, p := range pkgs {
		if p.Security {
			n++
		}
	}
	return n
}

func TestParseAptUbuntu(t *testing.T) {
	pkgs := parseAptSimulate(secFixture(t, "apt_ubuntu.txt"))
	if len(pkgs) != 4 || countSec(pkgs) != 4 {
		t.Fatalf("toplam %d güvenlik %d: %+v", len(pkgs), countSec(pkgs), pkgs)
	}
	p := pkgMap(pkgs)["perl-base"]
	if p.Current != "5.38.2-3.2ubuntu0.6" || p.New != "5.38.2-3.2ubuntu0.7" || p.ESM {
		t.Fatalf("perl-base: %+v", p)
	}
}

func TestParseAptDebian(t *testing.T) {
	pkgs := parseAptSimulate(secFixture(t, "apt_debian.txt"))
	m := pkgMap(pkgs)
	if len(pkgs) != 10 {
		t.Fatalf("toplam %d: %+v", len(pkgs), pkgs)
	}
	// Bağımlılık eki "[libstdc++6:amd64 ]" satırı bozmaz.
	if g := m["gcc-12-base"]; g.New != "12.2.0-14+deb12u1" || g.Security {
		t.Fatalf("gcc-12-base: %+v", g)
	}
	for _, n := range []string{"libgcrypt20", "liblzma5", "libapt-pkg6.0"} {
		if !m[n].Security {
			t.Fatalf("%s güvenlik sayılmadı", n)
		}
	}
	if m["libstdc++6"].Security {
		t.Fatal("libstdc++6 güvenlik sayıldı")
	}
}

func TestParseAptESM(t *testing.T) {
	m := pkgMap(parseAptSimulate(secFixture(t, "apt_esm.txt")))
	if len(m) != 4 {
		t.Fatalf("toplam %d", len(m))
	}
	for _, n := range []string{"libxml2", "imagemagick"} {
		if p := m[n]; !p.ESM || !p.Security {
			t.Fatalf("%s: %+v", n, p)
		}
	}
	if k := m["linux-image-5.15.0-125-generic"]; !k.Security || k.Current != "" || k.ESM {
		t.Fatalf("yeni kurulum: %+v", k)
	}
	if m["tzdata"].Security {
		t.Fatal("tzdata güvenlik sayıldı")
	}
}

func TestParseDnf(t *testing.T) {
	pkgs := parseDnfCheckUpdate(secFixture(t, "dnf_check.txt"))
	m := pkgMap(pkgs)
	if len(pkgs) != 15 {
		t.Fatalf("toplam %d: %+v", len(pkgs), pkgs)
	}
	if m["python3-dateutil-but-with-a-very-long-package-name"].New != "2.8.1-7.el9_8" {
		t.Fatal("bölünmüş satır okunmadı")
	}
	if m["grub2-tools"].Name != "" {
		t.Fatal("Obsoleting bölümü sayıldı")
	}
	adv := parseDnfUpdateinfo(secFixture(t, "dnf_updateinfo.txt"))
	if !adv.Any {
		t.Fatal("duyuru yok sanıldı")
	}
	markSecurity(pkgs, adv.Security)
	m = pkgMap(pkgs)
	want := map[string]string{"binutils": "Important", "binutils-gold": "Moderate", "bzip2-libs": "Moderate",
		"ca-certificates": "Critical", "coreutils-single": "Moderate"}
	for n, sev := range want {
		if p := m[n]; !p.Security || p.Severity != sev {
			t.Fatalf("%s: %+v", n, p)
		}
	}
	if countSec(pkgs) != len(want) {
		t.Fatalf("güvenlik %d", countSec(pkgs))
	}
}

func TestParseDnf5Advisory(t *testing.T) {
	pkgs := parseDnfCheckUpdate(secFixture(t, "dnf5_check.txt"))
	adv := parseDnfUpdateinfo(secFixture(t, "dnf5_advisory.txt"))
	markSecurity(pkgs, adv.Security)
	m := pkgMap(pkgs)
	if len(pkgs) != 4 || countSec(pkgs) != 3 || m["openssl-libs"].Severity != "Important" || m["curl"].Severity != "Moderate" || m["tzdata"].Security {
		t.Fatalf("%+v", pkgs)
	}
}

func TestParseUpdateinfoEmpty(t *testing.T) {
	if adv := parseDnfUpdateinfo("\n"); adv.Any || len(adv.Security) != 0 {
		t.Fatalf("%+v", adv)
	}
}

func TestParseZypper(t *testing.T) {
	pkgs, err := parseZypperUpdates(strings.NewReader(secFixture(t, "zypper_updates.xml")))
	if err != nil || len(pkgs) != 4 {
		t.Fatalf("%v %+v", err, pkgs)
	}
	if c := pkgMap(pkgs)["curl"]; c.Current != "8.0.1-150400.5.50.1" || c.New != "8.0.1-150400.5.59.1" {
		t.Fatalf("curl: %+v", c)
	}
	n, sev, err := parseZypperPatches(strings.NewReader(secFixture(t, "zypper_patches.xml")))
	if err != nil || n != 2 || sev != "Important" {
		t.Fatalf("yamalar: %d %q %v", n, sev, err)
	}
	sec, err := parseZypperDryRun(strings.NewReader(secFixture(t, "zypper_dryrun.xml")))
	if err != nil {
		t.Fatal(err)
	}
	markSecurity(pkgs, sec)
	if m := pkgMap(pkgs); countSec(pkgs) != 3 || m["aaa_base"].Security {
		t.Fatalf("%+v", pkgs)
	}
}

func TestParseNeedsRestarting(t *testing.T) {
	req, known, pkgs := parseNeedsRestarting(secFixture(t, "needs_restarting_yes.txt"))
	if !req || !known || strings.Join(pkgs, ",") != "kernel,systemd" {
		t.Fatalf("%v %v %v", req, known, pkgs)
	}
	if req, known, _ = parseNeedsRestarting(secFixture(t, "needs_restarting_no.txt")); req || !known {
		t.Fatal("gerekmiyor okunmadı")
	}
	// Eklenti yok (çıkış 1): bilinmiyor, yedek kurala geçilir.
	if _, known, _ = parseNeedsRestarting("No such command: needs-restarting. Please use /usr/bin/dnf --help\n"); known {
		t.Fatal("eksik eklenti tanındı sanıldı")
	}
	if p := parseRebootPkgs(secFixture(t, "reboot-required.pkgs")); len(p) != 2 || p[0] != "linux-image-6.8.0-48-generic" {
		t.Fatalf("%v", p)
	}
}

func TestNewestKernel(t *testing.T) {
	names := []string{"vmlinuz-0-rescue-b2ec7b5d", "vmlinuz-6.12.0-211.56.1.el10_2.x86_64", "vmlinuz-6.12.0-211.61.1.el10_2.x86_64",
		"vmlinuz-6.12.0-211.9.1.el10_2.x86_64", "config-6.12.0-211.99.1.el10_2.x86_64", "initramfs-x.img"}
	if k := newestKernel(names); k != "6.12.0-211.61.1.el10_2.x86_64" {
		t.Fatalf("%q", k)
	}
	if k := newestKernel([]string{"vmlinuz-6.8.0-45-generic", "vmlinuz-6.8.0-48-generic", "vmlinuz"}); k != "6.8.0-48-generic" {
		t.Fatalf("%q", k)
	}
	cases := []struct {
		a, b string
		want int
	}{
		{"1.10", "1.9", 1}, {"1.0", "1.0", 0}, {"1.0~rc1", "1.0", -1}, {"1.0a", "1.0", 1}, {"2", "10", -1},
		{"6.1.0-18-amd64", "6.1.0-9-amd64", 1}, {"1.0.1", "1.0a", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compare(%q,%q)=%d istenen %d", c.a, c.b, got, c.want)
		}
	}
}

// Konteynerde (host görünmüyor) güncellemeler no_host; reboot HOST_ROOT yoksa no_host.
func TestSecurityContainerNoHost(t *testing.T) {
	c := newCollector(Options{Getenv: func(string) string { return "" }, InContainer: func() bool { return true }, DockerSocket: "-"}, nil, NoHostReason)
	s := c.Security(context.Background())
	if s.Updates != nil || s.Notes[SecSectionUpdates] != NoteNoHost {
		t.Fatalf("%+v", s)
	}
}

// TestSecurityLive gerçek sistemde toplar ve JSON basar (BEKCI_SEC_LIVE=1).
// Konteynerde denemek için BEKCI_SEC_HOST=1 konteyneri host sayar.
func TestSecurityLive(t *testing.T) {
	if os.Getenv("BEKCI_SEC_LIVE") != "1" {
		t.Skip("BEKCI_SEC_LIVE=1 değil")
	}
	o := Options{DockerSocket: "-"}
	if os.Getenv("BEKCI_SEC_HOST") == "1" {
		o.InContainer = func() bool { return false }
	}
	c := NewCollector(o)
	start := time.Now()
	s := c.Security(context.Background())
	first := time.Since(start)
	start = time.Now()
	s2 := c.Security(context.Background())
	if s.Hash() != s2.Hash() {
		t.Error("ikinci çağrı farklı (önbellek)")
	}
	if s.Updates != nil && s2.Updates.CheckedAt != s.Updates.CheckedAt {
		t.Error("önbellekten gelmedi")
	}
	pk := s.Updates
	if pk != nil && len(pk.Packages) > 5 {
		cp := *pk
		cp.Packages = cp.Packages[:5]
		s.Updates = &cp
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	t.Logf("ilk %v, ikinci %v\n%s", first.Round(time.Millisecond), time.Since(start).Round(time.Millisecond), b)
}
