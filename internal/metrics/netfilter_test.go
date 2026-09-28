package metrics

import (
	"os"
	"path/filepath"
	"testing"
)

// fakeNetTree sahte bir sysfs/procfs ağacı kurar: arayüz dizinleri,
// köprü/bond işaretleri, master bağlantıları ve VLAN listesi.
func fakeNetTree(t *testing.T) (sysDir, procDir string) {
	t.Helper()
	root := t.TempDir()
	sysDir, procDir = filepath.Join(root, "sys"), filepath.Join(root, "proc", "1")
	net := filepath.Join(sysDir, "class", "net")
	mk := func(p ...string) {
		if err := os.MkdirAll(filepath.Join(p...), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := func(dev, master string) {
		if err := os.Symlink(filepath.Join("..", master), filepath.Join(net, dev, "master")); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"eth0", "eth1", "eno1", "eno2", "enp3s0", "wg0", "vlan5", "team0"} {
		mk(net, d)
	}
	mk(net, "bond0", "bonding")   // bond: sayılır
	mk(net, "bond1", "bonding")   // köprüye bağlı bond: sayılır
	mk(net, "vmbr0", "bridge")    // Proxmox köprüsü
	mk(net, "br0", "bridge")      // Linux köprüsü
	mk(net, "mybridge", "bridge") // öneki olmayan köprü de sysfs'ten tanınır
	link("eno1", "bond0")         // bond üyeleri atlanır
	link("eno2", "bond0")
	link("eth1", "vmbr0")   // köprü üyesi fiziksel arayüz sayılır
	link("bond1", "br0")    // köprüye bağlı bond sayılır
	link("enp3s0", "team0") // bond olmayan master: sayılır
	mk(procDir, "net", "vlan")
	if err := os.WriteFile(filepath.Join(procDir, "net", "vlan", "vlan5"), []byte("vlan5 VID: 5\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sysDir, procDir
}

func TestNetKeeper(t *testing.T) {
	sysDir, procDir := fakeNetTree(t)
	keep := netKeeper(sysDir, procDir)
	for n, want := range map[string]bool{
		"eth0": true, "eth1": true, "bond0": true, "bond1": true, "enp3s0": true, "wg0": true, "tun0": true,
		"eno1": false, "eno2": false, // bond üyeleri
		"vmbr0": false, "br0": false, "mybridge": false, // köprüler
		"vlan5": false, "eth0.100": false, "bond0.20": false, // VLAN
		"tap100i0": false, "fwbr100i0": false, "fwpr100p0": false, "fwln100i0": false, "vmbr1": false, // Proxmox, sysfs'te yok
		"veth12": false, "docker0": false, "lo": false, "": false, "../eth0": false,
	} {
		if got := keep(n); got != want {
			t.Errorf("%q → %v, %v bekleniyordu", n, got, want)
		}
	}
	// sysfs/procfs okunamazsa yalnızca ad kuralları uygulanır.
	keep = netKeeper(filepath.Join(t.TempDir(), "yok"), "")
	for n, want := range map[string]bool{"eth0": true, "eno1": true, "eth0.100": false, "tap0": false} {
		if got := keep(n); got != want {
			t.Errorf("sysfs yokken %q → %v", n, got)
		}
	}
}

// Köprü, bond üyesi ve VLAN trafiği toplam hıza bir kez girer.
func TestNetKeeperRates(t *testing.T) {
	sysDir, procDir := fakeNetTree(t)
	prev := map[string]ioCounter{"bond0": {0, 0}, "eno1": {0, 0}, "eno2": {0, 0}, "vmbr0": {0, 0}, "vlan5": {0, 0}, "eth0.100": {0, 0}}
	cur := map[string]ioCounter{"bond0": {1000, 500}, "eno1": {600, 300}, "eno2": {400, 200}, "vmbr0": {900, 450}, "vlan5": {100, 50}, "eth0.100": {10, 5}}
	keep := netKeeper(sysDir, procDir)
	rx, tx := rates(filterKeys(prev, keep), filterKeys(cur, keep), 1)
	if rx != 1000 || tx != 500 {
		t.Fatalf("yalnızca bond0 sayılmalı: rx=%v tx=%v", rx, tx)
	}
}
