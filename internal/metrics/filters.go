package metrics

import (
	"cmp"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// mount /proc/<pid>/mountinfo'daki bir satır.
type mount struct {
	Dev    string // "8:1" (major:minor); aynı aygıtın tekrarlarını ayıklamak için
	Root   string // aygıt içindeki kök ("/" dışındaysa bir bind bağlamasıdır)
	Point  string // host'taki bağlama noktası
	FS     string // "ext4"
	Source string // "/dev/sda1"
}

// parseMountinfo mountinfo içeriğini çözer. hostRoot boş değilse satırlar
// ajanın kendi bağlama ad alanındandır (/proc/self/mountinfo): yalnızca
// hostRoot altındakiler alınır ve bağlama noktası önek atılarak host yoluna çevrilir.
func parseMountinfo(data, hostRoot string) []mount {
	hostRoot = strings.TrimRight(hostRoot, "/")
	var out []mount
	for line := range strings.SplitSeq(data, "\n") {
		pre, post, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		f, g := strings.Fields(pre), strings.Fields(post)
		if len(f) < 5 || len(g) < 2 {
			continue
		}
		m := mount{Dev: f[2], Root: unescapeMount(f[3]), Point: unescapeMount(f[4]), FS: g[0], Source: unescapeMount(g[1])}
		if hostRoot != "" {
			switch {
			case m.Point == hostRoot:
				m.Point = "/"
			case strings.HasPrefix(m.Point, hostRoot+"/"):
				m.Point = m.Point[len(hostRoot):]
			default:
				continue
			}
		}
		out = append(out, m)
	}
	return out
}

// unescapeMount mountinfo'daki sekizli kaçışları (\040 boşluk vb.) çözer.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// Sanal, geçici veya salt okunur imaj dosya sistemleri: disk listesinde yer almaz.
var virtualFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "devfs": true, "devpts": true, "ramfs": true, "rootfs": true,
	"overlay": true, "overlayfs": true, "aufs": true, "squashfs": true, "iso9660": true, "udf": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true, "nsfs": true, "autofs": true,
	"mqueue": true, "debugfs": true, "tracefs": true, "securityfs": true, "pstore": true, "bpf": true,
	"configfs": true, "fusectl": true, "hugetlbfs": true, "binfmt_misc": true, "efivarfs": true,
	"rpc_pipefs": true, "selinuxfs": true, "nfsd": true, "fuse": true, "shm": true, "none": true,
}

// Ağ dosya sistemleri de atlanır: statfs uzak taraf yanıt vermezse uzun süre
// takılabilir ve alan başka bir sunucuya aittir.
var networkFS = map[string]bool{
	"nfs": true, "nfs4": true, "cifs": true, "smb3": true, "smbfs": true, "ceph": true,
	"glusterfs": true, "9p": true, "afs": true, "lustre": true, "davfs": true,
}

// Konteyner çalışma zamanlarının iç bağlamaları ve Docker'ın dosya bind'ları.
var skipMountPrefixes = []string{"/proc", "/sys", "/dev", "/run", "/var/lib/docker", "/var/lib/containerd",
	"/var/lib/containers", "/var/lib/kubelet", "/snap", "/var/snap"}

var skipMountExact = map[string]bool{"/etc/hostname": true, "/etc/hosts": true, "/etc/resolv.conf": true}

func skipMount(m mount) bool {
	if virtualFS[m.FS] || networkFS[m.FS] || strings.HasPrefix(m.FS, "fuse.") || skipMountExact[m.Point] {
		return true
	}
	if strings.HasPrefix(m.Point, "/run/media/") {
		return false // masaüstünde takılan diskler
	}
	for _, p := range skipMountPrefixes {
		if m.Point == p || strings.HasPrefix(m.Point, p+"/") {
			return true
		}
	}
	return false
}

// selectDisks gerçek dosya sistemlerini seçer: sanal/ağ/konteyner bağlamaları
// atlanır, aynı aygıt birden çok yere bağlıysa (bind) en kısa yol kalır,
// boyutu 0 olanlar ve görünmeyenler düşer. "/" başta, gerisi yola göre sıralıdır.
func selectDisks(ms []mount, usage func(mount) (total, used uint64, ok bool)) []Disk {
	best := map[string]mount{}
	for _, m := range ms {
		if skipMount(m) {
			continue
		}
		key := m.Dev
		if key == "" || key == "0:0" {
			key = m.Source
		}
		cur, ok := best[key]
		if !ok || betterMount(m, cur) {
			best[key] = m
		}
	}
	var out []Disk
	for _, m := range best {
		total, used, ok := usage(m)
		if !ok || total == 0 {
			continue
		}
		out = append(out, Disk{Mount: m.Point, Device: m.Source, FS: m.FS, Total: total, Used: min(used, total)})
	}
	slices.SortFunc(out, func(a, b Disk) int {
		if (a.Mount == "/") != (b.Mount == "/") {
			if a.Mount == "/" {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Mount, b.Mount)
	})
	if len(out) > MaxDisks {
		out = out[:MaxDisks]
	}
	return out
}

// betterMount aynı aygıtın iki bağlamasından hangisinin gösterileceği:
// aygıtın kökünü bağlayan, sonra daha kısa yol.
func betterMount(a, b mount) bool {
	if (a.Root == "/") != (b.Root == "/") {
		return a.Root == "/"
	}
	if len(a.Point) != len(b.Point) {
		return len(a.Point) < len(b.Point)
	}
	return a.Point < b.Point
}

// Fiziksel disklerin tamamı (bölümler, loop, ram, dm-*, md* değil): G/Ç iki
// kez sayılmasın diye yalnızca bunlar toplanır.
var physicalDiskRe = regexp.MustCompile(`^(?:[shv]d[a-z]+|xvd[a-z]+|nvme\d+n\d+|mmcblk\d+)$`)

func isPhysicalDisk(name string) bool { return physicalDiskRe.MatchString(name) }

// Sanal ağ arayüzleri: konteyner/köprü trafiği host arayüzünden zaten geçtiği
// için sayılmaz. Tünel (tun, wg, tailscale) arayüzleri gerçek kabul edilir.
// tap: sanal makine NIC'leri (Proxmox/KVM), fw: Proxmox güvenlik duvarı
// köprüleri (fwbr, fwpr, fwln), vmbr: Proxmox köprüleri (sysfs görünmese de).
var virtualNetPrefixes = []string{"lo", "veth", "docker", "br-", "virbr", "cni", "flannel", "cali",
	"vxlan", "cilium", "lxc", "kube-", "weave", "dummy", "tap", "fw", "vmbr"}

func isVirtualNet(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range virtualNetPrefixes[1:] {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// netKeeper Linux'ta ağ trafiğinin iki kez sayılmaması için arayüzleri host'un
// sysfs'ine (sysDir: HOST_SYS veya /sys) ve procfs'ine (procDir: HOST_PROC/1
// gibi bir süreç dizini; VLAN listesi procDir/net/vlan) bakarak süzer:
//   - köprüler (sysDir/class/net/X/bridge var: br0, vmbr0, docker0): üye
//     arayüzlerin trafiğini tekrar sayarlar,
//   - bond üyeleri (sysDir/class/net/X/master bir bond'u gösteriyor): trafik
//     bond arayüzünde sayılır, üyeler atlanır,
//   - VLAN alt arayüzleri (procDir/net/vlan/X var veya adda nokta: eth0.100):
//     trafik ana arayüzde zaten sayılır,
//   - ada göre sanal arayüzler (isVirtualNet: veth, tap, fw… önekleri).
//
// Köprünün üyesi olan fiziksel arayüz (ör. Proxmox'ta vmbr0'ın eno1'i)
// sayılır; bir köprüye bağlı bond da. Dosyalar okunamazsa yalnızca ad
// kuralları uygulanır.
func netKeeper(sysDir, procDir string) func(string) bool {
	classNet := filepath.Join(sysDir, "class", "net")
	return func(name string) bool {
		if name == "" || isVirtualNet(name) || strings.ContainsAny(name, "./") {
			return false // adda nokta: VLAN (eth0.100); "/" yol kaçışına karşı
		}
		dev := filepath.Join(classNet, name)
		if fileExists(filepath.Join(dev, "bridge")) {
			return false
		}
		if procDir != "" && fileExists(filepath.Join(procDir, "net", "vlan", name)) {
			return false
		}
		if link, err := os.Readlink(filepath.Join(dev, "master")); err == nil {
			if master := filepath.Base(link); fileExists(filepath.Join(classNet, master, "bonding")) {
				return false
			}
		}
		return true
	}
}

// filterTemps anlamsız okumaları (≤0, ≥150 °C, NaN) atar, adları temizler,
// aynı ad ve değerde tekrarlananları birleştirir; aynı adlı farklı sensörlere
// sıra numarası eklenir. Ada göre sıralı, en fazla MaxTemps.
func filterTemps(in []Temp) []Temp {
	seen := map[string][]float64{}
	var out []Temp
	for _, t := range in {
		name := strings.Trim(strings.ToLower(strings.TrimSpace(t.Name)), "_")
		if name == "" || math.IsNaN(t.C) || t.C <= 0 || t.C >= 150 {
			continue
		}
		c := math.Round(t.C*10) / 10
		if slices.Contains(seen[name], c) {
			continue
		}
		seen[name] = append(seen[name], c)
		if n := len(seen[name]); n > 1 {
			name += "_" + strconv.Itoa(n)
		}
		out = append(out, Temp{Name: name, C: c})
	}
	slices.SortStableFunc(out, func(a, b Temp) int { return cmp.Compare(a.Name, b.Name) })
	if len(out) > MaxTemps {
		out = out[:MaxTemps]
	}
	return out
}
