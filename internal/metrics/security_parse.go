package metrics

import (
	"bufio"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"unicode"
)

// Güvenlik toplayıcısının saf ayrıştırıcıları (iş 731). Build etiketi yok:
// her ortamda testdata/sec/ altındaki örneklerle sınanır. Komut çalıştırma
// security_linux.go'da.

// aptInstRe `apt-get -s dist-upgrade` çıktısındaki
// "Inst ad [eski] (yeni köken1, köken2 [mimari]) [bağımlılık ]" satırı.
var aptInstRe = regexp.MustCompile(`^Inst (\S+)(?: \[([^\]]*)\])? \((\S+) (.*?)(?: \[[^\]]*\])?\)`)

// parseAptSimulate apt benzetim çıktısındaki kurulacak/yükseltilecek paketler.
// Güvenlik: kökenlerden biri "-security" ile biter ya da Debian-Security; Ubuntu
// Pro (UbuntuESM*) kökenleri ayrıca ESM işaretlenir.
func parseAptSimulate(out string) []SecPkg {
	var pkgs []SecPkg
	for line := range strings.Lines(out) {
		m := aptInstRe.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
		if m == nil {
			continue
		}
		p := SecPkg{Name: m[1], Current: m[2], New: m[3]}
		for o := range strings.SplitSeq(m[4], ",") {
			o = strings.TrimSpace(o)
			origin, suite, _ := strings.Cut(o, ":")
			if strings.HasPrefix(origin, "UbuntuESM") || strings.Contains(suite, "esm-apps") || strings.Contains(suite, "esm-infra") {
				p.ESM = true
			}
			if strings.HasSuffix(suite, "-security") || origin == "Debian-Security" {
				p.Security = true
			}
		}
		if p.ESM {
			p.Security = true
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

// rpmArches nevra ve "ad.mimari" sonlarında tanınan mimariler.
var rpmArches = map[string]bool{
	"x86_64": true, "noarch": true, "i686": true, "i386": true, "aarch64": true, "ppc64le": true,
	"s390x": true, "armv7hl": true, "riscv64": true, "src": true, "ppc64": true, "loongarch64": true,
}

// splitNameArch "ad.mimari" -> ad (mimari tanınmazsa ok=false).
func splitNameArch(s string) (string, bool) {
	i := strings.LastIndexByte(s, '.')
	if i <= 0 || !rpmArches[s[i+1:]] {
		return "", false
	}
	return s[:i], true
}

// nevraName "ad-[epok:]sürüm-sürüm2.mimari" -> ad.
func nevraName(s string) (string, bool) {
	base, ok := splitNameArch(s)
	if !ok {
		return "", false
	}
	i := strings.LastIndexByte(base, '-')
	if i <= 0 {
		return "", false
	}
	j := strings.LastIndexByte(base[:i], '-')
	if j <= 0 {
		return "", false
	}
	return base[:j], true
}

// parseDnfCheckUpdate `dnf/yum -q check-update` çıktısı: "ad.mimari sürüm depo"
// satırları. Uzun adlar iki satıra bölünebilir (ad tek başına, sonraki satırda
// sürüm ve depo). "Obsoleting Packages" bölümü sayılmaz. Aynı ad birden çok
// mimaride gelirse bir kez sayılır.
func parseDnfCheckUpdate(out string) []SecPkg {
	var pkgs []SecPkg
	seen := map[string]bool{}
	pending := ""
	for line := range strings.Lines(out) {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Obsoleting") {
			break
		}
		f := strings.Fields(line)
		if pending != "" {
			if len(f) == 2 {
				f = append([]string{pending}, f...)
			}
			pending = ""
		}
		if len(f) == 1 {
			if _, ok := splitNameArch(f[0]); ok {
				pending = f[0]
			}
			continue
		}
		if len(f) != 3 {
			continue
		}
		name, ok := splitNameArch(f[0])
		if !ok || !unicode.IsDigit(rune(f[1][0])) || seen[name] {
			continue
		}
		seen[name] = true
		pkgs = append(pkgs, SecPkg{Name: name, New: f[1]})
	}
	return pkgs
}

// secSeverities ciddiyet adları (büyük harfle başlayan biçim) ve sırası.
var secSeverities = map[string]int{"Low": 1, "Moderate": 2, "Important": 3, "Critical": 4}

func normSeverity(s string) string {
	s = strings.TrimSuffix(s, "/Sec.")
	if s == "" {
		return ""
	}
	s = strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
	if _, ok := secSeverities[s]; ok {
		return s
	}
	return ""
}

// secAdvisories updateinfo ayrıştırma sonucu.
type secAdvisories struct {
	Any      bool              // en az bir duyuru satırı (tür fark etmez): dağıtım errata veriyor
	Security map[string]string // güvenlik duyurusu olan paket adı -> en yüksek ciddiyet ("" bilinmiyor)
}

// parseDnfUpdateinfo `dnf updateinfo list --available` (dnf4, yum: "KOD tür nevra",
// tür "Moderate/Sec." ya da bugfix/enhancement) ve dnf5 `advisory list`
// ("KOD tür ciddiyet nevra tarih") çıktılarını okur.
func parseDnfUpdateinfo(out string) secAdvisories {
	adv := secAdvisories{Security: map[string]string{}}
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		name, sev, sec := "", "", false
		for _, x := range f[1:] {
			switch {
			case strings.HasSuffix(x, "/Sec."):
				sec, sev = true, normSeverity(x)
			case strings.EqualFold(x, "security"):
				sec = true
			case name == "":
				if n, ok := nevraName(x); ok {
					name = n
				} else if sev == "" {
					sev = normSeverity(x)
				}
			}
		}
		if name == "" {
			continue
		}
		adv.Any = true
		if !sec {
			continue
		}
		if old, ok := adv.Security[name]; !ok || secSeverities[sev] > secSeverities[old] {
			adv.Security[name] = sev
		}
	}
	return adv
}

// markSecurity güvenlik duyurusu olan paketleri işaretler.
func markSecurity(pkgs []SecPkg, sec map[string]string) {
	for i := range pkgs {
		if sev, ok := sec[pkgs[i].Name]; ok {
			pkgs[i].Security, pkgs[i].Severity = true, sev
		}
	}
}

// parseNeedsRestarting `needs-restarting -r` çıktısı: required yalnız "Reboot is
// required" cümlesi varsa (araç yoksa da çıkış 1 olur); known=false: çıktı
// tanınmadı (araç yok, eklenti eksik).
func parseNeedsRestarting(out string) (required, known bool, pkgs []string) {
	for line := range strings.Lines(out) {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "* "):
			pkgs = append(pkgs, strings.TrimSpace(t[2:]))
		case strings.Contains(t, "Reboot is required"):
			required, known = true, true
		case strings.Contains(t, "Reboot should not be necessary"), strings.Contains(t, "Reboot is probably not necessary"):
			known = true
		}
	}
	if !required {
		pkgs = nil
	}
	return required, known, pkgs
}

// parseRebootPkgs /run/reboot-required.pkgs: satır başına paket adı.
func parseRebootPkgs(s string) []string {
	var out []string
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(line); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// zypper XML ----------------------------------------------------------------------------------

type zypUpdate struct {
	Kind       string `xml:"kind,attr"`
	Name       string `xml:"name,attr"`
	Edition    string `xml:"edition,attr"`
	EditionOld string `xml:"edition-old,attr"`
	Status     string `xml:"status,attr"`
	Category   string `xml:"category,attr"`
	Severity   string `xml:"severity,attr"`
}

type zypSolvable struct {
	Type       string `xml:"type,attr"`
	Name       string `xml:"name,attr"`
	Edition    string `xml:"edition,attr"`
	EditionOld string `xml:"edition-old,attr"`
}

// zypStream zypper -x çıktısında gereken öğeler (iletiler atlanır).
type zypStream struct {
	Updates []zypUpdate `xml:"update-status>update-list>update"`
	// Paket yöneticisi yaması önce kurulmalıysa diğer yamalar bu listede
	// görünür; yine de gereklidir.
	Blocked []zypUpdate `xml:"update-status>blocked-update-list>update"`
	Summary struct {
		Upgrade []zypSolvable `xml:"to-upgrade>solvable"`
		Install []zypSolvable `xml:"to-install>solvable"`
	} `xml:"install-summary"`
}

func decodeZypper(r io.Reader) (zypStream, error) {
	var s zypStream
	d := xml.NewDecoder(bufio.NewReader(r))
	d.Strict = false
	err := d.Decode(&s)
	return s, err
}

// parseZypperUpdates `zypper -x list-updates`: bekleyen paketler.
func parseZypperUpdates(r io.Reader) ([]SecPkg, error) {
	s, err := decodeZypper(r)
	if err != nil {
		return nil, err
	}
	var pkgs []SecPkg
	for _, u := range s.Updates {
		if u.Kind == "package" && u.Name != "" {
			pkgs = append(pkgs, SecPkg{Name: u.Name, Current: u.EditionOld, New: u.Edition})
		}
	}
	return pkgs, nil
}

// parseZypperPatches `zypper -x list-patches --category security`: gereken
// güvenlik yamalarının sayısı ve en yüksek ciddiyet.
func parseZypperPatches(r io.Reader) (needed int, severity string, err error) {
	s, err := decodeZypper(r)
	if err != nil {
		return 0, "", err
	}
	for _, u := range append(s.Updates, s.Blocked...) {
		if u.Kind != "patch" || u.Category != "security" || (u.Status != "" && u.Status != "needed") {
			continue
		}
		needed++
		if sev := normSeverity(u.Severity); secSeverities[sev] > secSeverities[severity] {
			severity = sev
		}
	}
	return needed, severity, nil
}

// parseZypperDryRun `zypper -x patch --dry-run --category security`: güvenlik
// yamalarının kuracağı/yükselteceği paket adları.
func parseZypperDryRun(r io.Reader) (map[string]string, error) {
	s, err := decodeZypper(r)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, p := range append(s.Summary.Upgrade, s.Summary.Install...) {
		if p.Type == "package" && p.Name != "" {
			out[p.Name] = ""
		}
	}
	return out, nil
}

// Çekirdek sürümü ---------------------------------------------------------------------------

// newestKernel /boot/vmlinuz-* adlarından en yeni çekirdek sürümü (kurtarma
// imajları atlanır).
func newestKernel(names []string) string {
	best := ""
	for _, n := range names {
		v, ok := strings.CutPrefix(n, "vmlinuz-")
		if !ok || v == "" || strings.Contains(v, "rescue") || !unicode.IsDigit(rune(v[0])) {
			continue
		}
		if best == "" || compareVersions(v, best) > 0 {
			best = v
		}
	}
	return best
}

// compareVersions rpmvercmp benzeri karşılaştırma: sayısal ve alfabetik
// parçalar ayrı ayrı, sayı harften büyük, "~" her şeyden küçük.
func compareVersions(a, b string) int {
	for a != "" || b != "" {
		a = strings.TrimLeftFunc(a, func(r rune) bool { return !isAlnum(r) && r != '~' })
		b = strings.TrimLeftFunc(b, func(r rune) bool { return !isAlnum(r) && r != '~' })
		if strings.HasPrefix(a, "~") || strings.HasPrefix(b, "~") {
			if !strings.HasPrefix(a, "~") {
				return 1
			}
			if !strings.HasPrefix(b, "~") {
				return -1
			}
			a, b = a[1:], b[1:]
			continue
		}
		if a == "" || b == "" {
			break
		}
		num := unicode.IsDigit(rune(a[0]))
		sa, ra := cutSegment(a, num)
		sb, rb := cutSegment(b, num)
		if sb == "" { // farklı tür parçalar: sayı kazanır
			if num {
				return 1
			}
			return -1
		}
		if num {
			sa, sb = strings.TrimLeft(sa, "0"), strings.TrimLeft(sb, "0")
			if len(sa) != len(sb) {
				if len(sa) > len(sb) {
					return 1
				}
				return -1
			}
		}
		if c := strings.Compare(sa, sb); c != 0 {
			return c
		}
		a, b = ra, rb
	}
	switch {
	case a == "" && b == "":
		return 0
	case a == "":
		return -1
	default:
		return 1
	}
}

func isAlnum(r rune) bool { return r < 128 && (unicode.IsDigit(r) || unicode.IsLetter(r)) }

func cutSegment(s string, num bool) (seg, rest string) {
	i := 0
	for i < len(s) {
		r := rune(s[i])
		if num && !unicode.IsDigit(r) || !num && !(r < 128 && unicode.IsLetter(r)) {
			break
		}
		i++
	}
	return s[:i], s[i:]
}
