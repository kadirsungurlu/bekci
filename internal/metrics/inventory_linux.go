//go:build linux

package metrics

import (
	"bufio"
	"context"
	"encoding/hex"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var _ inventorySource = (*systemSource)(nil)

// services systemctl ile okur. Ajan konteynerde çalışıyorsa systemctl yoktur;
// host'un cgroup ağacından çalışan servisler ve etc altındaki etkin bağlantılar
// okunur (durmuş ve devre dışı servisler bu yolda görünmez).
func (s *systemSource) services(ctx context.Context) ([]InvService, string) {
	if bin, err := exec.LookPath("systemctl"); err == nil && s.root == "" {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, bin, "list-units", "--type=service", "--all", "--no-legend", "--no-pager", "--plain").Output()
		if err != nil {
			return []InvService{}, NoteNoSystemd // systemd çalışmıyor (init başka)
		}
		files, _ := exec.CommandContext(ctx, bin, "list-unit-files", "--type=service", "--no-legend", "--no-pager", "--plain").Output()
		return parseSystemctl(string(out), string(files)), ""
	}
	if list := cgroupServices(s.sys, s.root+"/etc/systemd/system"); len(list) > 0 {
		return list, ""
	}
	return []InvService{}, NoteNoSystemd
}

// parseSystemctl "list-units" ve "list-unit-files" çıktılarını birleştirir.
func parseSystemctl(units, files string) []InvService {
	enabled := map[string]string{}
	for _, l := range strings.Split(files, "\n") {
		if f := strings.Fields(l); len(f) >= 2 {
			enabled[f[0]] = f[1]
		}
	}
	out := []InvService{}
	for _, l := range strings.Split(units, "\n") {
		f := strings.Fields(strings.TrimLeft(l, "●× "))
		if len(f) < 4 || !strings.HasSuffix(f[0], ".service") || f[1] == "not-found" {
			continue
		}
		name := strings.TrimSuffix(f[0], ".service")
		out = append(out, InvService{Name: name, Active: f[2], Sub: f[3], Enabled: enabled[f[0]], Description: strings.Join(f[4:], " ")})
	}
	return out
}

// cgroupServices host cgroup ağacındaki *.service dizinleri (v2 ve v1).
func cgroupServices(sys, unitDir string) []InvService {
	if sys == "" {
		sys = "/sys"
	}
	out := []InvService{}
	seen := map[string]bool{}
	for _, dir := range []string{"/fs/cgroup/system.slice", "/fs/cgroup/systemd/system.slice"} {
		es, err := os.ReadDir(sys + dir)
		if err != nil {
			continue
		}
		for _, e := range es {
			if !e.IsDir() || !strings.HasSuffix(e.Name(), ".service") || seen[e.Name()] {
				continue
			}
			seen[e.Name()] = true
			st := "static"
			if _, err := os.Lstat(filepath.Join(unitDir, "multi-user.target.wants", e.Name())); err == nil {
				st = "enabled"
			}
			out = append(out, InvService{Name: strings.TrimSuffix(e.Name(), ".service"), Active: "active", Sub: "running", Enabled: st})
		}
	}
	return out
}

// ports HOST_PROC/1/net/{tcp,udp}{,6} dosyalarından dinleyen soketleri,
// süreç adını /proc/<pid>/fd bağlantılarından çözer. Yetki yoksa süreç adı boş
// kalır (systemd hizmetinde yalnız root'un süreçleri görünür).
func (s *systemSource) ports(ctx context.Context) ([]InvPort, string) {
	proc := s.proc
	if proc == "" {
		proc = "/proc"
	}
	base := proc + "/1/net"
	if _, err := os.Stat(base + "/tcp"); err != nil {
		base = proc + "/net"
	}
	out := []InvPort{}
	inodes := map[string]int{} // soket düğümü -> out dizini (ilk görülen)
	seen := map[string]bool{}
	read := false
	for _, f := range []struct{ file, proto string }{{"tcp", "tcp"}, {"tcp6", "tcp"}, {"udp", "udp"}, {"udp6", "udp"}} {
		data, err := os.ReadFile(base + "/" + f.file)
		if err != nil {
			continue
		}
		read = true
		for _, sock := range parseProcNet(string(data), f.proto) {
			key := sock.proto + sock.addr + strconv.Itoa(sock.port)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, InvPort{Proto: sock.proto, Addr: sock.addr, Port: sock.port})
			inodes[sock.inode] = len(out) - 1
		}
	}
	if !read {
		return out, NoteFailed
	}
	resolveProcesses(ctx, proc, inodes, out)
	return out, ""
}

type procSock struct {
	proto, addr, inode string
	port               int
}

// parseProcNet /proc/net/{tcp,udp}* satırlarını çözer: TCP'de LISTEN (0A), UDP'de
// karşı adresi boş soketler. Geçici aralıktaki UDP soketleri (istemci tarafı,
// sürekli değişir) atlanır.
func parseProcNet(data, proto string) []procSock {
	var out []procSock
	sc := bufio.NewScanner(strings.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 10 || f[0] == "sl" {
			continue
		}
		if proto == "tcp" && f[3] != "0A" {
			continue
		}
		if proto == "udp" && (f[3] != "07" || !strings.HasSuffix(f[2], ":0000")) {
			continue
		}
		addr, port, ok := parseHexAddr(f[1])
		if !ok || (proto == "udp" && port >= 32768) {
			continue
		}
		out = append(out, procSock{proto: proto, addr: addr, port: port, inode: f[9]})
	}
	return out
}

// parseHexAddr "0100007F:0050" biçimi (çekirdek baytları 32 bitlik gruplarda ters yazar).
func parseHexAddr(s string) (string, int, bool) {
	host, portHex, ok := strings.Cut(s, ":")
	if !ok {
		return "", 0, false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return "", 0, false
	}
	raw, err := hex.DecodeString(host)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return "", 0, false
	}
	for i := 0; i+4 <= len(raw); i += 4 {
		raw[i], raw[i+1], raw[i+2], raw[i+3] = raw[i+3], raw[i+2], raw[i+1], raw[i]
	}
	ip, ok := netip.AddrFromSlice(raw)
	if !ok {
		return "", 0, false
	}
	return ip.Unmap().String(), int(port), true
}

// resolveProcesses soket düğümlerini sahip süreçlere bağlar; ortam dosyası
// (environ) okunmaz, komut satırı maskelenir.
func resolveProcesses(ctx context.Context, proc string, inodes map[string]int, out []InvPort) {
	pids, err := os.ReadDir(proc)
	if err != nil {
		return
	}
	left := len(inodes)
	for _, e := range pids {
		if left == 0 || ctx.Err() != nil {
			return
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := proc + "/" + e.Name()
		fds, err := os.ReadDir(dir + "/fd")
		if err != nil {
			continue
		}
		var comm, cmd string
		for _, fd := range fds {
			link, err := os.Readlink(dir + "/fd/" + fd.Name())
			if err != nil || !strings.HasPrefix(link, "socket:[") {
				continue
			}
			i, ok := inodes[strings.TrimSuffix(strings.TrimPrefix(link, "socket:["), "]")]
			if !ok || out[i].Process != "" {
				continue
			}
			if comm == "" {
				b, _ := os.ReadFile(dir + "/comm")
				comm = strings.TrimSpace(string(b))
				c, _ := os.ReadFile(dir + "/cmdline")
				cmd = MaskSecrets(strings.TrimSpace(strings.ReplaceAll(string(c), "\x00", " ")))
			}
			out[i].Process, out[i].Cmd = comm, cmd
			left--
		}
	}
}
