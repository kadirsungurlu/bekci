//go:build linux

package metrics

import (
	"context"
	"os"
	"strings"
	"syscall"
)

// osSecUpdates bulunan paket yöneticisiyle bekleyen güncellemeleri sorgular.
// Sorgular yalnız yerel önbellekten yapılır (apt benzetimi, dnf/yum -C, zypper
// --no-refresh): ağa çıkmaz, dizini tazelemek dağıtımın kendi zamanlayıcısının işi.
func osSecUpdates(ctx context.Context, e secEnv) (*SecUpdates, string) {
	switch {
	case e.look("apt-get"):
		return aptUpdates(ctx, e)
	case e.look("dnf"):
		return dnfUpdates(ctx, e, ManagerDnf, "dnf")
	case e.look("dnf5"):
		return dnfUpdates(ctx, e, ManagerDnf, "dnf5")
	case e.look("yum"):
		return dnfUpdates(ctx, e, ManagerYum, "yum")
	case e.look("zypper"):
		return zypperUpdates(ctx, e)
	}
	return nil, NoteNoManager
}

func aptUpdates(ctx context.Context, e secEnv) (*SecUpdates, string) {
	r := e.run(ctx, secPkgTimeout, "apt-get", "-s", "-o", "Debug::NoLocking=true", "dist-upgrade")
	if r.Err != nil || r.Code != 0 {
		return nil, NoteFailed
	}
	pkgs := parseAptSimulate(r.Out)
	return &SecUpdates{Manager: ManagerApt, Total: len(pkgs), Packages: pkgs,
		IndexTime: newestMtime(e.root + "/var/lib/apt/lists/*InRelease")}, ""
}

// dnfUpdates dnf (4/5) ve yum: check-update çıkışı 100 = güncelleme var, 0 = yok.
// Güvenlik bilgisi updateinfo'dan; dağıtım hiç duyuru vermiyorsa (CentOS 7,
// CentOS Stream) güvenlik sayısı -1 = bilinmiyor.
func dnfUpdates(ctx context.Context, e secEnv, manager, bin string) (*SecUpdates, string) {
	r := e.run(ctx, secPkgTimeout, bin, "-C", "-q", "check-update")
	switch {
	case r.Err != nil:
		return nil, NoteFailed
	case r.Code != 0 && r.Code != 100:
		if strings.Contains(strings.ToLower(r.Out+r.Stderr), "cache") {
			return nil, NoteCacheMissing
		}
		return nil, NoteFailed
	}
	pkgs := parseDnfCheckUpdate(r.Out)
	u := &SecUpdates{Manager: manager, Total: len(pkgs), Packages: pkgs,
		IndexTime: newestMtime(e.root+"/var/cache/dnf/*/repodata/repomd.xml",
			e.root+"/var/cache/libdnf5/*/repodata/repomd.xml", e.root+"/var/cache/yum/*/*/*/repomd.xml")}
	if len(pkgs) == 0 {
		return u, ""
	}
	args := []string{"-C", "-q", "updateinfo", "list", "--available"}
	if manager == ManagerYum {
		args = []string{"-C", "-q", "updateinfo", "list"}
	}
	r = e.run(ctx, secPkgTimeout, bin, args...)
	if (r.Err != nil || r.Code != 0) && manager == ManagerDnf && !r.Missing {
		// dnf5: updateinfo takma adı çalışmazsa asıl komut.
		r = e.run(ctx, secPkgTimeout, bin, "-C", "-q", "advisory", "list")
	}
	if r.Err != nil || r.Code != 0 {
		u.Security = -1
		return u, NoteFailed
	}
	adv := parseDnfUpdateinfo(r.Out)
	if !adv.Any {
		u.Security = -1
		return u, NoteNoErrata
	}
	markSecurity(u.Packages, adv.Security)
	return u, ""
}

// zypperOK zypper çıkış kodu: 0 ya da 100+ (bilgi kodları) başarılı; 7 kilit.
func zypperOK(r cmdResult) (bool, string) {
	switch {
	case r.Err != nil:
		return false, NoteFailed
	case r.Code == 7:
		return false, NoteLocked
	case r.Code != 0 && r.Code < 100:
		return false, NoteFailed
	}
	return true, ""
}

// zypperUpdates paket listesi + güvenlik yamaları; yamaların hangi paketleri
// yükselttiği `patch --dry-run` özetinden okunur (yalnız gereken yama varsa).
func zypperUpdates(ctx context.Context, e secEnv) (*SecUpdates, string) {
	r := e.run(ctx, secPkgTimeout, "zypper", "-n", "--no-refresh", "-x", "list-updates")
	if ok, note := zypperOK(r); !ok {
		return nil, note
	}
	pkgs, err := parseZypperUpdates(strings.NewReader(r.Out))
	if err != nil {
		return nil, NoteFailed
	}
	u := &SecUpdates{Manager: ManagerZypper, Total: len(pkgs), Packages: pkgs,
		IndexTime: newestMtime(e.root + "/var/cache/zypp/raw/*/repodata/repomd.xml")}
	if len(pkgs) == 0 {
		return u, ""
	}
	r = e.run(ctx, secPkgTimeout, "zypper", "-n", "--no-refresh", "-x", "list-patches", "--category", "security")
	ok, note := zypperOK(r)
	if !ok {
		u.Security = -1
		return u, note
	}
	needed, _, err := parseZypperPatches(strings.NewReader(r.Out))
	if err != nil {
		u.Security = -1
		return u, NoteFailed
	}
	if needed == 0 {
		return u, ""
	}
	r = e.run(ctx, secPkgTimeout, "zypper", "-n", "--no-refresh", "-x", "patch", "--dry-run", "--category", "security")
	if ok, note = zypperOK(r); !ok {
		u.Security = -1
		return u, note
	}
	sec, err := parseZypperDryRun(strings.NewReader(r.Out))
	if err != nil {
		u.Security = -1
		return u, NoteFailed
	}
	markSecurity(u.Packages, sec)
	return u, ""
}

// osSecReboot sırayla: işaret dosyaları (Debian/Ubuntu, SUSE), dağıtım aracı
// (needs-restarting, zypper needs-rebooting), son çare çekirdek karşılaştırması.
// Konteynerde araçlar çalıştırılmaz; HOST_ROOT varsa dosyalar oradan okunur.
func osSecReboot(ctx context.Context, e secEnv) (*SecReboot, string) {
	root := e.root
	if e.container && root == "" {
		return nil, NoteNoHost
	}
	for _, f := range []string{"/run/reboot-required", "/var/run/reboot-required"} {
		if fileExists(root + f) {
			b, _ := os.ReadFile(root + f + ".pkgs")
			return &SecReboot{Required: true, Reason: RebootFile, Packages: parseRebootPkgs(string(b))}, ""
		}
	}
	if fileExists(root + "/run/reboot-needed") {
		return &SecReboot{Required: true, Reason: RebootFile}, ""
	}
	if !e.container {
		for _, cmd := range [][]string{{"dnf", "needs-restarting", "-r"}, {"needs-restarting", "-r"}} {
			if !e.look(cmd[0]) {
				continue
			}
			r := e.run(ctx, secCmdTimeout, cmd[0], cmd[1:]...)
			if r.Err != nil {
				continue
			}
			if req, known, pkgs := parseNeedsRestarting(r.Out + r.Stderr); known {
				if req {
					return &SecReboot{Required: true, Reason: RebootTool, Packages: pkgs}, ""
				}
				return &SecReboot{}, ""
			}
		}
		if e.look("zypper") {
			r := e.run(ctx, secCmdTimeout, "zypper", "needs-rebooting")
			switch {
			case r.Err != nil:
			case r.Code == 102:
				return &SecReboot{Required: true, Reason: RebootTool}, ""
			case r.Code == 0:
				return &SecReboot{}, ""
			}
		}
	}
	if running := kernelRelease(); running != "" {
		var names []string
		if ents, err := os.ReadDir(root + "/boot"); err == nil {
			for _, d := range ents {
				names = append(names, d.Name())
			}
		}
		if newest := newestKernel(names); newest != "" && compareVersions(newest, running) > 0 {
			return &SecReboot{Required: true, Reason: RebootKernel, Packages: []string{"kernel-" + newest}}, ""
		}
	}
	return &SecReboot{}, ""
}

// kernelRelease çalışan çekirdek (uname -r); konteynerde de host'unkidir.
func kernelRelease() string {
	b, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// secExtrasSupported SSH girişleri, oturumlar ve güvenlik duvarı (732) toplanır.
const secExtrasSupported = true

// fileInode günlük dosyasının inode'u (döndürme algılaması).
func fileInode(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ino
	}
	return 0
}

// osSecExtras Linux'ta kullanılmaz (secExtrasSupported).
func osSecExtras(context.Context, secEnv, *Security) bool { return false }
