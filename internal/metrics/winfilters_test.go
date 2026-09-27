package metrics

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestKeepWinVolume(t *testing.T) {
	cases := []struct {
		v    winVolume
		want bool
	}{
		{winVolume{Path: `C:\`, FS: "NTFS", Type: winDriveFixed}, true},
		{winVolume{Path: `D:\`, FS: "ReFS", Type: winDriveFixed}, true},
		{winVolume{Path: `C:\mnt\veri\`, FS: "NTFS", Type: winDriveFixed}, true},
		{winVolume{Path: `E:\`, FS: "FAT32", Type: winDriveRemovable}, false}, // USB bellek
		{winVolume{Path: `Z:\`, FS: "NTFS", Type: winDriveRemote}, false},     // ağ sürücüsü
		{winVolume{Path: `F:\`, FS: "CDFS", Type: winDriveCDROM}, false},
		{winVolume{Path: `R:\`, FS: "NTFS", Type: winDriveRAMDisk}, false},
		{winVolume{Path: `G:\`, FS: "", Type: winDriveFixed}, false}, // biçimlenmemiş / kilitli BitLocker
		{winVolume{Path: `H:\`, FS: "NTFS", Type: winDriveUnknown}, false},
		{winVolume{Path: ``, FS: "NTFS", Type: winDriveFixed}, false},
	}
	for _, c := range cases {
		if got := keepWinVolume(c.v); got != c.want {
			t.Errorf("keepWinVolume(%+v) = %v, beklenen %v", c.v, got, c.want)
		}
	}
}

func TestWinMountPoint(t *testing.T) {
	for in, want := range map[string]string{
		`C:\`: "C:", `c:\`: "C:", `C:`: "C:", `D:\`: "D:", `C:\mnt\veri\`: `C:\mnt\veri`, ` E:\ `: "E:", ``: "",
	} {
		if got := winMountPoint(in); got != want {
			t.Errorf("winMountPoint(%q) = %q, beklenen %q", in, got, want)
		}
	}
}

// Aynı birim hem sürücü harfine hem klasöre bağlıysa bir kez (sürücü
// harfiyle) sayılır; diskler bağlama noktasına göre sıralanır.
func TestSelectDisksWindows(t *testing.T) {
	vols := []winVolume{
		{Path: `D:\`, Volume: `\\?\Volume{d}\`, Label: "Veri", FS: "NTFS", Type: winDriveFixed},
		{Path: `C:\`, Volume: `\\?\Volume{c}\`, Label: "", FS: "NTFS", Type: winDriveFixed},
		{Path: `C:\mnt\d\`, Volume: `\\?\Volume{d}\`, Label: "Veri", FS: "NTFS", Type: winDriveFixed},
		{Path: `C:\mnt\yedek\`, Volume: `\\?\Volume{y}\`, Label: "Yedek", FS: "ReFS", Type: winDriveFixed},
		{Path: `E:\`, Volume: `\\?\Volume{e}\`, FS: "FAT32", Type: winDriveRemovable},
	}
	var ms []mount
	for _, v := range vols {
		if keepWinVolume(v) {
			ms = append(ms, winMount(v))
		}
	}
	sizes := map[string][2]uint64{"C:": {100, 60}, "D:": {200, 50}, `C:\mnt\d`: {200, 50}, `C:\mnt\yedek`: {300, 30}}
	got := selectDisks(ms, func(m mount) (uint64, uint64, bool) { s, ok := sizes[m.Point]; return s[0], s[1], ok })
	want := []Disk{
		{Mount: "C:", Device: "", FS: "NTFS", Total: 100, Used: 60},
		{Mount: `C:\mnt\yedek`, Device: "Yedek", FS: "ReFS", Total: 300, Used: 30},
		{Mount: "D:", Device: "Veri", FS: "NTFS", Total: 200, Used: 50},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("diskler\n%+v\nbeklenen\n%+v", got, want)
	}
	// GUID okunamazsa bağlama noktası kimlik olur.
	if m := winMount(winVolume{Path: `F:\`, FS: "NTFS", Type: winDriveFixed}); m.Dev != "F:" || m.Point != "F:" || m.Root != "/" {
		t.Errorf("GUID'siz birim: %+v", m)
	}
}

func TestIsWinDriveKey(t *testing.T) {
	for name, want := range map[string]bool{"C:": true, "d:": true, "C": false, `C:\`: false, "sda": false, "PhysicalDrive0": false, "": false} {
		if isWinDriveKey(name) != want {
			t.Errorf("isWinDriveKey(%q) != %v", name, want)
		}
	}
}

func TestIsVirtualWinNet(t *testing.T) {
	const ethernet, wifi, ppp, propVirtual = 6, 71, 23, 53
	cases := []struct {
		name, desc string
		ifType     uint32
		want       bool
	}{
		{"Ethernet", "Intel(R) Ethernet Connection (7) I219-LM", ethernet, false},
		{"Ethernet 2", "Red Hat VirtIO Ethernet Adapter", ethernet, false}, // KVM/VPS'teki gerçek kart
		{"Ethernet0", "vmxnet3 Ethernet Adapter", ethernet, false},         // VMware konuğundaki gerçek kart
		{"Ethernet", "Microsoft Hyper-V Network Adapter", ethernet, false}, // Hyper-V/Azure konuğundaki gerçek kart
		{"Wi-Fi", "Intel(R) Wi-Fi 6 AX201 160MHz", wifi, false},
		{"wg0", "WireGuard Tunnel", propVirtual, false}, // VPN gerçek sayılır (Linux'la aynı)
		{"Loopback Pseudo-Interface 1", "Software Loopback Interface 1", 24, true},
		{"vEthernet (Default Switch)", "Hyper-V Virtual Ethernet Adapter", ethernet, true},
		{"vEthernet (WSL)", "Hyper-V Virtual Ethernet Adapter #2", ethernet, true},
		{"vEthernet (External)", "Hyper-V Virtual Ethernet Adapter #3", ethernet, true},
		{"isatap.{1234}", "Microsoft ISATAP Adapter", 131, true},
		{"Teredo Tunneling Pseudo-Interface", "Microsoft Teredo Tunneling Adapter", 131, true},
		{"Local Area Connection* 1", "WAN Miniport (IP)", ppp, true},
		{"Bluetooth Network Connection", "Bluetooth Device (Personal Area Network)", ethernet, true},
		{"Local Area Connection* 2", "Microsoft Wi-Fi Direct Virtual Adapter", wifi, true},
		{"VirtualBox Host-Only Network", "VirtualBox Host-Only Ethernet Adapter", ethernet, true},
		{"VMware Network Adapter VMnet8", "VMware Virtual Ethernet Adapter for VMnet8", ethernet, true},
		{"Npcap Loopback Adapter", "Npcap Loopback Adapter", ethernet, true},
		{"vEthernet (nat)", "", 0, true}, // açıklama okunamazsa yalnızca ad
	}
	for _, c := range cases {
		if got := isVirtualWinNet(c.name, c.desc, c.ifType); got != c.want {
			t.Errorf("isVirtualWinNet(%q, %q, %d) = %v, beklenen %v", c.name, c.desc, c.ifType, got, c.want)
		}
	}
}

func TestWinHostText(t *testing.T) {
	if got := winKernel("10.0.20348.2340 Build 20348.2340"); got != "10.0.20348.2340" {
		t.Errorf("çekirdek %q", got)
	}
	if got := winKernel("10.0.17763.1"); got != "10.0.17763.1" {
		t.Errorf("çekirdek %q", got)
	}
	for _, c := range [][3]string{
		{"Microsoft Windows Server 2022 Datacenter", "21H2", "Microsoft Windows Server 2022 Datacenter 21H2"},
		{"Microsoft Windows Server 2019 Standard", "", "Microsoft Windows Server 2019 Standard"},
		{"Microsoft Windows 11 Pro 23H2", "23H2", "Microsoft Windows 11 Pro 23H2"},
	} {
		if got := winPlatform(c[0], c[1]); got != c[2] {
			t.Errorf("winPlatform(%q, %q) = %q", c[0], c[1], got)
		}
	}
}

// winFakeSource G/Ç anahtarlarını kendisi süzen (Windows gibi) sahte kaynak.
type winFakeSource struct{ fakeSource }

func (f *winFakeSource) keepDisk(name string) bool { return isWinDriveKey(name) }
func (f *winFakeSource) keepNet(string) bool       { return true }

// Windows kaynağında sürücü harfleri disk G/Ç'ye sayılır (Linux'taki aygıt adı
// süzgeci uygulanmaz); bellek önbelleği bekleme listesidir.
func TestCollectWindowsSource(t *testing.T) {
	clk := newClock()
	src := &winFakeSource{fakeSource{
		host:     Host{Hostname: "WIN-SRV", OS: "windows", Threads: 8},
		cpuTotal: 1000, cpuBusy: 100,
		// Windows: toplam 16000, kullanılabilir 10000 (bunun 6000'i bekleme listesi).
		mem:   memStat{Total: 16000, Available: 10000, Free: 4000, Cached: 6000, SwapTotal: 2000, SwapFree: 1500},
		mnts:  []mount{winMount(winVolume{Path: `C:\`, Volume: "c", FS: "NTFS", Type: winDriveFixed})},
		sizes: map[string][2]uint64{"C:": {500, 200}},
		disk:  map[string]ioCounter{"C:": {A: 1000, B: 1000}, "D:": {A: 0, B: 0}},
		net:   map[string]ioCounter{"Ethernet": {A: 100, B: 100}},
	}}
	c := newCollector(testOpts(clk), src, "")
	c.Collect(context.Background())
	clk.add(10 * time.Second)
	src.disk = map[string]ioCounter{"C:": {A: 11_000, B: 21_000}, "D:": {A: 10_000, B: 0}}
	src.net = map[string]ioCounter{"Ethernet": {A: 10_100, B: 5_100}}
	s, ok := c.Collect(context.Background())
	if !ok || s.Stats == nil {
		t.Fatalf("örnek bekleniyordu: %+v", s)
	}
	st := s.Stats
	if st.DiskReadBps != 2000 || st.DiskWriteBps != 2000 {
		t.Errorf("disk G/Ç %v %v", st.DiskReadBps, st.DiskWriteBps)
	}
	if st.NetRxBps != 1000 || st.NetTxBps != 500 {
		t.Errorf("ağ %v %v", st.NetRxBps, st.NetTxBps)
	}
	if st.MemUsed != 6000 || st.MemCache != 6000 || st.SwapUsed != 500 {
		t.Errorf("bellek %+v", st)
	}
	if len(st.Disks) != 1 || st.Disks[0].Mount != "C:" || st.Disks[0].Used != 200 {
		t.Errorf("diskler %+v", st.Disks)
	}
}
