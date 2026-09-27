package metrics

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fakeSource sahte sistem: testler alanları örnekler arasında değiştirir.
type fakeSource struct {
	host        Host
	cpuTotal    float64
	cpuBusy     float64
	cpuErr      error
	l1, l5, l15 float64
	mem         memStat
	mnts        []mount
	sizes       map[string][2]uint64 // bağlama noktası → toplam, kullanılan
	disk        map[string]ioCounter
	net         map[string]ioCounter
	tmp         []Temp
	tmpErr      error
}

func (f *fakeSource) hostInfo(context.Context) (Host, error) { return f.host, nil }
func (f *fakeSource) cpuTimes(context.Context) (float64, float64, error) {
	return f.cpuTotal, f.cpuBusy, f.cpuErr
}
func (f *fakeSource) load(context.Context) (float64, float64, float64, error) {
	return f.l1, f.l5, f.l15, nil
}
func (f *fakeSource) memory(context.Context) (memStat, error) { return f.mem, nil }
func (f *fakeSource) mounts(context.Context) ([]mount, error) { return f.mnts, nil }
func (f *fakeSource) usage(m mount) (uint64, uint64, bool) {
	s, ok := f.sizes[m.Point]
	return s[0], s[1], ok
}
func (f *fakeSource) diskIO(context.Context) (map[string]ioCounter, error) { return clone(f.disk), nil }
func (f *fakeSource) netIO(context.Context) (map[string]ioCounter, error)  { return clone(f.net), nil }
func (f *fakeSource) temps(context.Context) ([]Temp, error)                { return f.tmp, f.tmpErr }

func clone(m map[string]ioCounter) map[string]ioCounter {
	out := map[string]ioCounter{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

// fakeClock elle ilerletilen saat.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time      { return c.t }
func (c *fakeClock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *fakeClock               { return &fakeClock{t: time.Unix(1_800_000_000, 0)} }
func noEnv(string) string                { return "" }
func testOpts(clk *fakeClock) Options {
	return Options{Getenv: noEnv, InContainer: func() bool { return false }, DockerSocket: "-", Now: clk.now}
}

func TestCollectRates(t *testing.T) {
	clk := newClock()
	src := &fakeSource{
		host:     Host{Hostname: "h1", Threads: 4, BootTime: clk.t.Unix() - 3600},
		cpuTotal: 1000, cpuBusy: 100,
		l1: 0.456, l5: 0.3, l15: 0.2,
		mem:  memStat{Total: 8000, Available: 5000, Buffers: 100, Cached: 2000, SReclaimable: 300, SwapTotal: 1000, SwapFree: 400},
		disk: map[string]ioCounter{"sda": {A: 1000, B: 2000}, "sda1": {A: 1000, B: 2000}, "loop0": {A: 5, B: 5}},
		net:  map[string]ioCounter{"eth0": {A: 10_000, B: 20_000}, "lo": {A: 1, B: 1}, "veth1": {A: 9, B: 9}, "docker0": {A: 9, B: 9}},
	}
	c := newCollector(testOpts(clk), src, "")

	if _, ok := c.Collect(context.Background()); ok {
		t.Fatal("ilk örnek yalnızca hazırlık olmalı")
	}
	clk.add(10 * time.Second)
	src.cpuTotal, src.cpuBusy = 1400, 200 // 400 sürede 100 meşgul → %25
	src.disk = map[string]ioCounter{"sda": {A: 11_000, B: 52_000}, "sda1": {A: 999_999, B: 999_999}, "loop0": {A: 9e9, B: 9e9}}
	src.net = map[string]ioCounter{"eth0": {A: 30_000, B: 25_000}, "lo": {A: 9e9, B: 9e9}, "veth1": {A: 9e9, B: 9e9}, "docker0": {A: 9e9, B: 9e9}}
	s, ok := c.Collect(context.Background())
	if !ok || s.Stats == nil || s.Host == nil || s.Unavailable != "" {
		t.Fatalf("örnek bekleniyordu: %+v", s)
	}
	st := s.Stats
	if s.Time != clk.t.UnixMilli() {
		t.Errorf("zaman %d", s.Time)
	}
	if st.CPU != 25 {
		t.Errorf("cpu %v", st.CPU)
	}
	if st.DiskReadBps != 1000 || st.DiskWriteBps != 5000 {
		t.Errorf("disk %v %v (yalnızca sda sayılmalı)", st.DiskReadBps, st.DiskWriteBps)
	}
	if st.NetRxBps != 2000 || st.NetTxBps != 500 {
		t.Errorf("ağ %v %v (yalnızca eth0 sayılmalı)", st.NetRxBps, st.NetTxBps)
	}
	if st.Load1 != 0.46 || st.Uptime != 3610 || s.Host.Hostname != "h1" {
		t.Errorf("yük/çalışma süresi %+v", st)
	}
	// used = toplam - kullanılabilir; cache = 100+2000+300, toplam - used ile sınırlı değil
	if st.MemTotal != 8000 || st.MemUsed != 3000 || st.MemCache != 2400 || st.SwapTotal != 1000 || st.SwapUsed != 600 {
		t.Errorf("bellek %+v", st)
	}

	// Sayaç geriye giderse (aygıt yeniden oluştu) negatif hız çıkmaz.
	clk.add(10 * time.Second)
	src.net = map[string]ioCounter{"eth0": {A: 100, B: 35_000}}
	s, _ = c.Collect(context.Background())
	if s.Stats.NetRxBps != 0 || s.Stats.NetTxBps != 1000 {
		t.Errorf("geri giden sayaç %v %v", s.Stats.NetRxBps, s.Stats.NetTxBps)
	}

	// Reset sonrası yeniden hazırlık.
	c.Reset()
	if _, ok := c.Collect(context.Background()); ok {
		t.Fatal("Reset sonrası ilk örnek hazırlık olmalı")
	}
}

func TestCollectPartialFailure(t *testing.T) {
	clk := newClock()
	src := &fakeSource{cpuErr: errors.New("yok"), mem: memStat{Total: 100, Available: 150}, tmpErr: errors.New("uyarı"),
		tmp: []Temp{{Name: "acpitz", C: 40}}}
	c := newCollector(testOpts(clk), src, "")
	c.Collect(context.Background())
	clk.add(time.Second)
	s, ok := c.Collect(context.Background())
	if !ok || s.Stats.CPU != 0 || s.Stats.MemUsed != 0 {
		t.Fatalf("%+v", s.Stats)
	}
	if len(s.Stats.Temps) != 1 {
		t.Fatal("uyarıyla gelen kısmi sıcaklıklar kullanılmalı")
	}
}

func TestMemCacheClamped(t *testing.T) {
	var st Stats
	// used + cache + boş toplamı aşmaz (paylaşımlı bellek hem used'da hem Cached'da).
	fillMemory(&st, memStat{Total: 1000, Available: 300, Free: 100, Buffers: 200, Cached: 500, SReclaimable: 100})
	if st.MemUsed != 700 || st.MemCache != 200 {
		t.Fatalf("used %d cache %d", st.MemUsed, st.MemCache)
	}
	fillMemory(&st, memStat{Total: 1000, Available: 1200, Free: 1100})
	if st.MemUsed != 0 || st.MemCache != 0 {
		t.Fatalf("taşma: used %d cache %d", st.MemUsed, st.MemCache)
	}
}

func TestCPUPct(t *testing.T) {
	for _, c := range []struct{ pt, pb, t, b, want float64 }{
		{100, 50, 200, 100, 50},
		{100, 50, 100, 60, 0}, // süre ilerlemedi
		{100, 50, 200, 40, 0}, // meşgul geri gitti
		{0, 0, 100, 150, 100}, // taşma sınırlanır
	} {
		if got := cpuPct(c.pt, c.pb, c.t, c.b); got != c.want {
			t.Errorf("%+v → %v", c, got)
		}
	}
}

func TestUnavailable(t *testing.T) {
	clk := newClock()
	c := newCollector(testOpts(clk), nil, NoHostReason)
	s, ok := c.Collect(context.Background())
	if !ok || s.Unavailable != NoHostReason || s.Stats != nil || s.Host != nil || s.Time == 0 {
		t.Fatalf("%+v", s)
	}
}

func TestVisibility(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	all := func(string) bool { return true }
	none := func(string) bool { return false }
	if r := visibility(env(nil), false, none); r != "" {
		t.Errorf("doğrudan kurulum toplamalı: %q", r)
	}
	if r := visibility(env(nil), true, all); r != NoHostReason {
		t.Errorf("konteyner + HOST_PROC yok: %q", r)
	}
	if r := visibility(env(map[string]string{"HOST_PROC": "/host/proc"}), true, all); r != "" {
		t.Errorf("host bağlı: %q", r)
	}
	if r := visibility(env(map[string]string{"HOST_PROC": "/host/proc"}), true, none); !strings.Contains(r, "/host/proc") {
		t.Errorf("HOST_PROC yanlış: %q", r)
	}
	for s, want := range map[string]bool{
		"0::/":                                    false,
		"0::/../../init.scope":                    false,
		"0::/system.slice/uptime-agent.service":   false,
		"12:cpu:/docker/0123abcd":                 true,
		"0::/kubepods/besteffort/pod1/abc":        true,
		"1:name=systemd:/system.slice/containerd": true,
	} {
		if cgroupInContainer(s) != want {
			t.Errorf("cgroup %q → %v", s, !want)
		}
	}
}

func TestDockerEndpoint(t *testing.T) {
	env := func(v string) func(string) string { return func(string) string { return v } }
	for _, c := range []struct {
		o    Options
		want string
	}{
		{Options{Getenv: env("")}, "unix:///var/run/docker.sock"},
		{Options{Getenv: env("unix:///run/user/1000/docker.sock")}, "unix:///run/user/1000/docker.sock"},
		{Options{Getenv: env("tcp://1.2.3.4:2375"), DockerSocket: "/x.sock"}, "unix:///x.sock"},
		{Options{Getenv: env(""), DockerSocket: "-"}, "-"},
	} {
		if got := dockerEndpoint(c.o); got != c.want {
			t.Errorf("%q, beklenen %q", got, c.want)
		}
	}
	if newDockerClient("-") != nil || newDockerClient("ftp://x") != nil {
		t.Error("kapalı Docker istemci üretmemeli")
	}
}

func TestParseMountinfo(t *testing.T) {
	host := `22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw
23 22 0:5 / /dev rw - devtmpfs devtmpfs rw
24 22 8:15 / /boot/efi rw - vfat /dev/sda15 rw
25 22 8:1 /var/lib/docker/volumes/x /srv/x rw - ext4 /dev/sda1 rw
26 22 8:17 / /mnt/my\040disk rw - xfs /dev/sdb1 rw
bozuk satır`
	got := parseMountinfo(host, "")
	want := []mount{
		{"8:1", "/", "/", "ext4", "/dev/sda1"},
		{"0:5", "/", "/dev", "devtmpfs", "devtmpfs"},
		{"8:15", "/", "/boot/efi", "vfat", "/dev/sda15"},
		{"8:1", "/var/lib/docker/volumes/x", "/srv/x", "ext4", "/dev/sda1"},
		{"8:17", "/", "/mnt/my disk", "xfs", "/dev/sdb1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}

	// Ajanın kendi mountinfo'su: yalnızca HOST_ROOT altı, önek atılmış.
	self := `900 800 0:50 / / rw - overlay overlay rw
901 900 8:1 / /host ro master:1 - ext4 /dev/sda1 rw
902 901 8:15 / /host/boot/efi ro - vfat /dev/sda15 rw
903 900 8:1 /var/lib/docker/containers/abc/hostname /etc/hostname rw - ext4 /dev/sda1 rw
904 900 0:60 / /hostile rw - ext4 /dev/sdz rw`
	got = parseMountinfo(self, "/host/")
	if len(got) != 2 || got[0].Point != "/" || got[1].Point != "/boot/efi" {
		t.Fatalf("%+v", got)
	}
}

func TestSelectDisks(t *testing.T) {
	ms := []mount{
		{"8:1", "/", "/", "ext4", "/dev/sda1"},
		{"8:1", "/var/lib/docker/volumes/x", "/srv/x", "ext4", "/dev/sda1"}, // bind: aynı aygıt
		{"8:1", "/", "/var/lib/docker/overlay2/abc/merged", "ext4", "/dev/sda1"},
		{"8:1", "/containers/abc/hostname", "/etc/hostname", "ext4", "/dev/sda1"},
		{"0:5", "/", "/dev", "devtmpfs", "devtmpfs"},
		{"0:30", "/", "/run", "tmpfs", "tmpfs"},
		{"0:40", "/", "/var/lib/docker/overlay2/x/merged", "overlay", "overlay"},
		{"7:0", "/", "/snap/core/1", "squashfs", "/dev/loop0"},
		{"0:130", "/", "/data/backups", "cifs", "//box/share"},
		{"0:131", "/", "/home/u/g", "fuse.sshfs", "u@h:/"},
		{"8:15", "/", "/boot/efi", "vfat", "/dev/sda15"},
		{"8:17", "/", "/data", "xfs", "/dev/sdb1"},
		{"8:17", "/", "/data2", "xfs", "/dev/sdb1"},  // aynı aygıt iki kez: kısa olan kalır
		{"8:33", "/", "/empty", "ext4", "/dev/sdc1"}, // boyut 0
		{"8:49", "/", "/gone", "ext4", "/dev/sdd1"},  // görünmüyor
		{"0:77", "/", "/tank", "zfs", "tank"},
	}
	sizes := map[string][2]uint64{
		"/": {100, 40}, "/boot/efi": {10, 1}, "/data": {500, 600}, "/data2": {500, 1}, "/empty": {0, 0}, "/tank": {50, 5},
		"/srv/x": {100, 40}, "/etc/hostname": {100, 40}, "/data/backups": {1000, 1},
	}
	src := &fakeSource{sizes: sizes}
	got := selectDisks(ms, src.usage)
	want := []Disk{
		{Mount: "/", Device: "/dev/sda1", FS: "ext4", Total: 100, Used: 40},
		{Mount: "/boot/efi", Device: "/dev/sda15", FS: "vfat", Total: 10, Used: 1},
		{Mount: "/data", Device: "/dev/sdb1", FS: "xfs", Total: 500, Used: 500},
		{Mount: "/tank", Device: "tank", FS: "zfs", Total: 50, Used: 5},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n%+v\n%+v", got, want)
	}
}

func TestDiskAndNetFilters(t *testing.T) {
	for n, want := range map[string]bool{
		"sda": true, "sdab": true, "vda": true, "xvda": true, "hda": true, "nvme0n1": true, "nvme10n2": true, "mmcblk0": true,
		"sda1": false, "nvme0n1p1": false, "mmcblk0p1": false, "loop0": false, "ram0": false, "dm-0": false, "md0": false, "sr0": false, "zram0": false,
	} {
		if isPhysicalDisk(n) != want {
			t.Errorf("disk %s → %v", n, !want)
		}
	}
	for n, want := range map[string]bool{
		"lo": true, "veth0210710": true, "docker0": true, "br-2f77d6b09956": true, "virbr0": true, "cni0": true,
		"flannel.1": true, "cali123": true, "eth0": false, "ens3": false, "enp0s31f6": false, "wlan0": false,
		"tun0": false, "wg0": false, "tailscale0": false, "bond0": false, "lower": false,
	} {
		if isVirtualNet(n) != want {
			t.Errorf("ağ %s → %v", n, !want)
		}
	}
}

func TestFilterTemps(t *testing.T) {
	in := []Temp{
		{"coretemp_core_1", 45.04},
		{"coretemp_core_0", 44},
		{"coretemp_core_0", 44}, // tekrar
		{"nvme_composite", 38},
		{"nvme_composite", 41}, // ikinci NVMe
		{"acpitz", 0},          // anlamsız
		{"bad", 250},           // anlamsız
		{" Iwlwifi_1 ", -5},    // anlamsız
		{"", 30},               // adsız
		{"k10temp_tctl", 61.26},
	}
	got := filterTemps(in)
	want := []Temp{{"coretemp_core_0", 44}, {"coretemp_core_1", 45}, {"k10temp_tctl", 61.3}, {"nvme_composite", 38}, {"nvme_composite_2", 41}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%+v", got)
	}
	many := make([]Temp, 50)
	for i := range many {
		many[i] = Temp{Name: "s" + string(rune('a'+i%26)) + string(rune('a'+i/26)), C: 40}
	}
	if n := len(filterTemps(many)); n != MaxTemps {
		t.Fatalf("sınır: %d", n)
	}
}
