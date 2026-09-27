package metrics

import (
	"regexp"
	"strings"
)

// Windows seçimleri. Saf fonksiyonlardır ve derleme etiketi yoktur (dosya
// adı _windows ile bitmez): Linux'ta da derlenip test edilir; ham değerleri
// source_windows.go okur.

// GetDriveType sonuçları (winbase.h DRIVE_*).
const (
	winDriveUnknown   uint32 = 0
	winDriveNoRootDir uint32 = 1
	winDriveRemovable uint32 = 2
	winDriveFixed     uint32 = 3
	winDriveRemote    uint32 = 4
	winDriveCDROM     uint32 = 5
	winDriveRAMDisk   uint32 = 6
)

// winVolume Windows'ta bir birimin bağlama noktası (sürücü harfi veya
// klasöre bağlanmış birim).
type winVolume struct {
	Path   string // "C:\" veya "C:\mnt\veri\" (ters bölü ile biter)
	Volume string // `\\?\Volume{GUID}\`; aynı birimin tekrarlarını ayıklamak için (boş olabilir)
	Label  string // birim etiketi ("Windows", "Veri"); boş olabilir
	FS     string // "NTFS", "ReFS"
	Type   uint32 // GetDriveType
}

// keepWinVolume yalnızca yerel sabit diskleri alır: çıkarılabilir (USB),
// ağ sürücüsü, CD/DVD ve RAM diski ile dosya sistemi okunamayan (biçimlenmemiş,
// kilitli BitLocker) birimler atlanır.
func keepWinVolume(v winVolume) bool {
	return v.Type == winDriveFixed && strings.TrimSpace(v.FS) != "" && winMountPoint(v.Path) != ""
}

// winMountPoint gösterim için bağlama noktası: "C:\" → "C:",
// "C:\mnt\veri\" → "C:\mnt\veri". Sürücü harfi büyük yazılır.
func winMountPoint(p string) string {
	p = strings.TrimRight(strings.TrimSpace(p), `\/`)
	if len(p) >= 2 && p[1] == ':' {
		p = strings.ToUpper(p[:1]) + p[1:]
	}
	return p
}

// winMount birimi toplayıcının ortak bağlama biçimine çevirir. Dev birimin
// kimliğidir: aynı birim hem sürücü harfine hem klasöre bağlıysa selectDisks
// daha kısa yolu (sürücü harfini) tutar. Aygıt sütununda etiket gösterilir.
func winMount(v winVolume) mount {
	point := winMountPoint(v.Path)
	dev := v.Volume
	if dev == "" {
		dev = point
	}
	return mount{Dev: dev, Root: "/", Point: point, FS: v.FS, Source: strings.TrimSpace(v.Label)}
}

// winDriveKeyRe disk G/Ç sayaçlarının anahtarı: sabit sürücü harfleri ("C:").
var winDriveKeyRe = regexp.MustCompile(`^[A-Za-z]:$`)

func isWinDriveKey(name string) bool { return winDriveKeyRe.MatchString(name) }

// Windows arayüz türleri (ipifcons.h IF_TYPE_*).
const (
	winIfLoopback uint32 = 24
	winIfTunnel   uint32 = 131
)

// winVirtualNetMarkers arayüz adında veya açıklamasında geçince sanal sayılan
// ifadeler (küçük harf). Hyper-V sanal anahtarına bağlı fiziksel kart zaten
// sayıldığı için "vEthernet" arayüzleri (host'un ve sanal makinelerin trafiği
// fiziksel karttan da geçer) sayılmaz; Linux'taki köprü/veth mantığıyla aynı.
// VPN'ler (WireGuard, OpenVPN, Tailscale) Linux'taki gibi gerçek sayılır.
var winVirtualNetMarkers = []string{
	"loopback", "vethernet", "hyper-v virtual", "isatap", "teredo", "6to4", "ip-https",
	"wan miniport", "bluetooth", "kernel debug", "wi-fi direct virtual", "virtual wifi",
	"virtualbox host-only", "vmware virtual ethernet", "vmnet", "npcap", "container",
	"docker", "wsl", "pseudo-interface",
}

// isVirtualWinNet sanal Windows ağ arayüzü mü (ad: "Ethernet", "vEthernet (WSL)";
// açıklama: "Intel(R) Ethernet Connection", "Hyper-V Virtual Ethernet Adapter").
func isVirtualWinNet(name, desc string, ifType uint32) bool {
	if ifType == winIfLoopback || ifType == winIfTunnel {
		return true
	}
	s := strings.ToLower(name + "\n" + desc)
	for _, m := range winVirtualNetMarkers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}

// winKernel gopsutil'in "10.0.20348.2340 Build 20348.2340" biçimindeki
// sürümünü kısaltır: "10.0.20348.2340".
func winKernel(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.Index(v, " Build "); i > 0 {
		v = v[:i]
	}
	return v
}

// winPlatform "Microsoft Windows Server 2022 Datacenter" + görünen sürüm ("21H2").
func winPlatform(product, display string) string {
	product, display = strings.TrimSpace(product), strings.TrimSpace(display)
	if display == "" || strings.Contains(product, display) {
		return product
	}
	return strings.TrimSpace(product + " " + display)
}
