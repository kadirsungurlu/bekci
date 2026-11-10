package metrics

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// authNow testlerdeki "şimdi" (eski syslog satırları bu konumda yorumlanır).
var authNow = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

func hourOf(s string) int64 {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t.Unix()
}

func syslogAgg(t *testing.T, name string, cutoff time.Time) *authAgg {
	t.Helper()
	var evs []authEvent
	for line := range strings.Lines(secFixture(t, name)) {
		if ev, ok := parseSyslogSSH(line, authNow); ok && !ev.Time.Before(cutoff) {
			evs = append(evs, ev)
		}
	}
	agg := &authAgg{}
	countAuth(agg, evs)
	return agg
}

func TestAuthFixtures(t *testing.T) {
	journal := func() *authAgg {
		evs, cur := parseJournalSSH(secFixture(t, "journal_sshd_session.json"))
		if cur != "s=6a1f;i=12fa05;b=d0e3;m=2a6;t=65fe5;x=e8" {
			t.Fatalf("imleç %q", cur)
		}
		if len(evs) != 6 || evs[2].Msg != "Invalid user t\xe9st from 203.0.113.9 port 40002" || evs[2].PID != "4002" {
			t.Fatalf("journal olayları: %d %q", len(evs), evs[2].Msg)
		}
		agg := &authAgg{}
		countAuth(agg, evs)
		return agg
	}
	cases := []struct {
		name                      string
		agg                       *authAgg
		failed, accepted, invalid int
		ip, user                  string
		hours                     []SecAuthHour
	}{
		{"auth_debian.log (24 sa)", syslogAgg(t, "auth_debian.log", authNow.Add(-24*time.Hour)), 8, 1, 4, "203.0.113.5", "root",
			[]SecAuthHour{{hourOf("2026-11-10T09:00:00Z"), 2, 0}, {hourOf("2026-11-10T10:00:00Z"), 6, 1}}},
		{"auth_debian.log (tümü)", syslogAgg(t, "auth_debian.log", time.Time{}), 9, 1, 4, "203.0.113.5", "root",
			[]SecAuthHour{{hourOf("2026-11-09T08:00:00Z"), 1, 0}, {hourOf("2026-11-10T09:00:00Z"), 2, 0}, {hourOf("2026-11-10T10:00:00Z"), 6, 1}}},
		{"auth_debian12.log", syslogAgg(t, "auth_debian12.log", time.Time{}), 2, 1, 0, "2001:db8::5", "root",
			[]SecAuthHour{{hourOf("2026-11-10T06:00:00Z"), 2, 1}}},
		{"secure_rhel.log", syslogAgg(t, "secure_rhel.log", time.Time{}), 4, 1, 1, "192.0.2.200", "root",
			[]SecAuthHour{{hourOf("2026-11-10T08:00:00Z"), 4, 1}}},
		{"journal_sshd_session.json", journal(), 3, 1, 2, "203.0.113.9", "oracle",
			[]SecAuthHour{{hourOf("2026-11-10T06:00:00Z"), 3, 1}}},
	}
	for _, c := range cases {
		a := c.agg.ToAuth("x")
		if a.Failed != c.failed || a.Accepted != c.accepted || a.Invalid != c.invalid {
			t.Errorf("%s: failed/accepted/invalid = %d/%d/%d, beklenen %d/%d/%d", c.name, a.Failed, a.Accepted, a.Invalid, c.failed, c.accepted, c.invalid)
		}
		if len(a.TopIPs) == 0 || a.TopIPs[0].Key != c.ip {
			t.Errorf("%s: ilk IP %v, beklenen %s", c.name, a.TopIPs, c.ip)
		}
		if len(a.TopUsers) == 0 || a.TopUsers[0].Key != c.user {
			t.Errorf("%s: ilk kullanıcı %v, beklenen %s", c.name, a.TopUsers, c.user)
		}
		if !slices.Equal(a.Hours, c.hours) {
			t.Errorf("%s: saatler %v, beklenen %v", c.name, a.Hours, c.hours)
		}
	}
	// Ad çözülmüş uzak adres (scanner.example.net) IP sayımına girmez ama kullanıcı sayılır.
	a := cases[0].agg
	if _, ok := a.IPs["scanner.example.net"]; ok || a.Users["git"] != 1 {
		t.Fatalf("ad çözülmüş adres: %v %v", a.IPs, a.Users)
	}
}

func TestAuthDoubleCount(t *testing.T) {
	at := authNow
	ev := func(pid, msg string) authEvent { return authEvent{Time: at, PID: pid, Msg: msg} }
	cases := []struct {
		name    string
		evs     []authEvent
		failed  int
		invalid int
	}{
		{"failed varken ikincil sayılmaz", []authEvent{
			ev("1", "Invalid user bob from 192.0.2.1 port 1"),
			ev("1", "pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh ruser= rhost=192.0.2.1"),
			ev("1", "Failed password for invalid user bob from 192.0.2.1 port 1 ssh2"),
			ev("1", "error: maximum authentication attempts exceeded for invalid user bob from 192.0.2.1 port 1 ssh2 [preauth]"),
		}, 1, 1},
		{"yalnız ikincil: pid başına 1", []authEvent{
			ev("2", "Invalid user bob from 192.0.2.1 port 1"),
			ev("2", "pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh ruser= rhost=192.0.2.1"),
			ev("3", "pam_unix(sshd:auth): authentication failure; logname= uid=0 euid=0 tty=ssh ruser= rhost=192.0.2.2  user=root"),
		}, 2, 1},
		{"pid yok: her satır ayrı", []authEvent{
			ev("", "Invalid user bob from 192.0.2.1"),
			ev("", "Invalid user bob from 192.0.2.1"),
		}, 2, 2},
		{"tekrar eden ileti", []authEvent{
			ev("4", "message repeated 5 times: [ Failed password for root from 192.0.2.3 port 22 ssh2]"),
		}, 5, 0},
		{"geçersiz IP sayılmaz", []authEvent{
			ev("5", "Failed password for root from not-an-ip port 22 ssh2"),
		}, 1, 0},
	}
	for _, c := range cases {
		agg := &authAgg{}
		countAuth(agg, c.evs)
		if agg.Failed != c.failed || agg.Invalid != c.invalid {
			t.Errorf("%s: failed/invalid = %d/%d, beklenen %d/%d", c.name, agg.Failed, agg.Invalid, c.failed, c.invalid)
		}
	}
	agg := &authAgg{}
	countAuth(agg, cases[1].evs)
	if agg.Users["bob"] != 1 || agg.Users["root"] != 1 || agg.IPs["192.0.2.2"] != 1 {
		t.Fatalf("ikincil ayrıntı: %v %v", agg.Users, agg.IPs)
	}
}

func TestAuthAggLimits(t *testing.T) {
	agg := &authAgg{}
	for i := range maxAuthKeys + 10 {
		agg.addFailed(authNow, "10.0."+strconv.Itoa(i/256)+"."+strconv.Itoa(i%256), "", false, 1)
	}
	if len(agg.IPs) != maxAuthKeys || agg.Failed != maxAuthKeys+10 {
		t.Fatalf("IP sınırı: %d anahtar, %d deneme", len(agg.IPs), agg.Failed)
	}
	agg = &authAgg{}
	for i := range 60 {
		agg.addFailed(authNow.Add(time.Duration(i)*time.Hour), "", "", false, 1)
	}
	a := agg.ToAuth("journal")
	if len(a.Hours) != MaxSecAuthHours || a.Hours[0].Failed != 13 || a.Hours[0].Hour != authNow.Add(12*time.Hour).Unix() {
		t.Fatalf("saat birleştirme: %d kova, ilk %+v", len(a.Hours), a.Hours[0])
	}
	// Çıkarma: kopyadan sonra eklenen korunur.
	snap := agg.clone()
	agg.addFailed(authNow, "192.0.2.9", "x", true, 2)
	agg.sub(snap)
	if agg.Failed != 2 || agg.Invalid != 2 || len(agg.Hours) != 1 || agg.IPs["192.0.2.9"] != 2 {
		t.Fatalf("çıkarma: %+v", agg)
	}
}

// utmpRecord testte glibc utmp kaydı yazar.
func utmpRecord(typ int16, pid int32, line, user, host string, sec int32) []byte {
	b := make([]byte, utmpSize)
	bo := binary.NativeEndian
	bo.PutUint16(b[0:], uint16(typ))
	bo.PutUint32(b[4:], uint32(pid))
	copy(b[8:40], line)
	copy(b[44:76], user)
	copy(b[76:332], host)
	bo.PutUint32(b[340:], uint32(sec))
	return b
}

func utmpSample() []byte {
	var b []byte
	b = append(b, utmpRecord(2, 0, "~", "reboot", "6.12.0", 1794000000)...) // BOOT_TIME
	b = append(b, utmpRecord(7, 100, "pts/0", "root", "203.0.113.77", 1794100000)...)
	b = append(b, utmpRecord(7, 101, "tty1", "kadir", "", 1794100100)...)
	b = append(b, utmpRecord(7, 102, "pts/1", "olu", "192.0.2.1", 1794100200)...) // süreç yok
	b = append(b, utmpRecord(8, 103, "pts/2", "", "", 0)...)                      // DEAD_PROCESS
	b = append(b, utmpRecord(7, 104, "pts/3", "x11", ":0", 1794100300)...)
	return b
}

func TestParseUtmp(t *testing.T) {
	got := parseUtmp(utmpSample(), func(pid int) bool { return pid != 102 })
	want := []SecSession{
		{User: "root", TTY: "pts/0", From: "203.0.113.77", Since: 1794100000, Kind: SessionSSH},
		{User: "kadir", TTY: "tty1", Since: 1794100100, Kind: SessionTTY},
		{User: "x11", TTY: "pts/3", Since: 1794100300, Kind: SessionTTY},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("utmp:\n%v\n%v", got, want)
	}
	if s := parseUtmp(nil, nil); s == nil || len(s) != 0 {
		t.Fatal("boş utmp boş dizi olmalı")
	}
}

func TestParseLoginctl(t *testing.T) {
	if ids := parseLoginctlList(secFixture(t, "loginctl_list.txt")); !slices.Equal(ids, []string{"1", "2", "c3"}) {
		t.Fatalf("kimlikler %v", ids)
	}
	s, ok := parseLoginctlShow(secFixture(t, "loginctl_show_ssh.txt"))
	if !ok || s.User != "root" || s.Kind != SessionSSH || s.From != "203.0.113.77" || s.TTY != "pts/0" ||
		s.Since != hourOf("2026-10-08T06:24:43Z") {
		t.Fatalf("ssh oturumu %+v", s)
	}
	if _, ok := parseLoginctlShow(secFixture(t, "loginctl_show_manager.txt")); ok {
		t.Fatal("manager sınıfı alınmamalı")
	}
	s, ok = parseLoginctlShow(secFixture(t, "loginctl_show_tty.txt"))
	if !ok || s.Kind != SessionTTY || s.From != "" || s.Since != hourOf("2026-11-09T18:33:00Z") {
		t.Fatalf("tty oturumu %+v", s)
	}
}

func TestParseFirewall(t *testing.T) {
	nft := []struct {
		file   string
		active bool
		rules  int
		policy string
	}{
		{"nft_firewalld.json", true, 7, "accept"},
		{"nft_docker_only.json", false, 0, "accept"},
		{"nft_ufw_like.json", true, 5, "drop"},
	}
	for _, c := range nft {
		r, err := parseNftRuleset([]byte(secFixture(t, c.file)))
		if err != nil || !r.Found || r.Active != c.active || r.Rules != c.rules || r.Policy != c.policy {
			t.Errorf("%s: %+v %v", c.file, r, err)
		}
	}
	// Politika accept, drop yalnız atlanan zincirde (döngülü atlama da sonlanmalı).
	loop := `{"nftables":[{"chain":{"family":"inet","table":"t","name":"in","type":"filter","hook":"input","policy":"accept"}},
{"chain":{"family":"inet","table":"t","name":"a"}},{"chain":{"family":"inet","table":"t","name":"b"}},
{"rule":{"family":"inet","table":"t","chain":"in","expr":[{"jump":{"target":"a"}}]}},
{"rule":{"family":"inet","table":"t","chain":"a","expr":[{"goto":{"target":"b"}}]}},
{"rule":{"family":"inet","table":"t","chain":"b","expr":[{"jump":{"target":"a"}}]}},
{"rule":{"family":"inet","table":"t","chain":"b","expr":[{"vmap":{"key":{"meta":{"key":"l4proto"}},"data":{"set":[["tcp",{"drop":null}]]}}}]}}]}`
	if r, err := parseNftRuleset([]byte(loop)); err != nil || !r.Active || r.Rules != 4 {
		t.Fatalf("döngülü atlama: %+v %v", r, err)
	}
	if r, _ := parseNftRuleset([]byte(`{"nftables":[]}`)); r.Found {
		t.Fatal("boş kural kümesi")
	}
	ipt := []struct {
		file   string
		active bool
		rules  int
		policy string
	}{
		{"iptables_legacy_drop.txt", true, 4, "accept"},
		{"iptables_legacy_open.txt", false, 0, "accept"},
	}
	for _, c := range ipt {
		if r := parseIptablesS(secFixture(t, c.file)); !r.Found || r.Active != c.active || r.Rules != c.rules || r.Policy != c.policy {
			t.Errorf("%s: %+v", c.file, r)
		}
	}
	if r := parseIptablesS("-P INPUT DROP\n"); !r.Active || r.Policy != "drop" {
		t.Fatalf("politika drop: %+v", r)
	}
	if !parseUfwStatus("Status: active\n\nTo Action From\n") || parseUfwStatus("Status: inactive\n") {
		t.Fatal("ufw")
	}
	if !parseFirewalldState("running\n") || parseFirewalldState("not running\n") {
		t.Fatal("firewalld")
	}
}

// fakeSecEnv komut çıktılarını haritadan veren sahte ortam.
type fakeSecEnv struct {
	tools map[string]bool
	out   map[string]cmdResult // komut adı + " " + ilk argüman
	calls [][]string
	fn    func(name string, args []string) (cmdResult, bool)
}

func (f *fakeSecEnv) env(root string) secEnv {
	return secEnv{
		root:  root,
		look:  func(n string) bool { return f.tools[n] },
		alive: func(int) bool { return true },
		run: func(_ context.Context, _ time.Duration, name string, args ...string) cmdResult {
			f.calls = append(f.calls, append([]string{name}, args...))
			if f.fn != nil {
				if r, ok := f.fn(name, args); ok {
					return r
				}
			}
			key := name
			if len(args) > 0 {
				key += " " + args[0]
			}
			if r, ok := f.out[key]; ok {
				return r
			}
			return cmdResult{Code: 1}
		},
	}
}

func testCollector(t *testing.T, dir string, now *time.Time) *Collector {
	return newCollector(Options{StateDir: dir, DockerSocket: "-", Now: func() time.Time { return *now },
		InContainer: func() bool { return false }}, nil, "")
}

func argWith(args []string, prefix string) string {
	for _, a := range args {
		if strings.HasPrefix(a, prefix) {
			return a
		}
	}
	return ""
}

func TestSecurityAuthJournalFlow(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 10, 7, 0, 0, 0, time.UTC)
	journal := secFixture(t, "journal_sshd_session.json")
	lines := strings.SplitAfter(journal, "\n")
	f := &fakeSecEnv{tools: map[string]bool{"journalctl": true}}
	next := strings.Join(lines[:4], "") // ilk turda 4 kayıt
	f.fn = func(name string, args []string) (cmdResult, bool) {
		out := next
		next = ""
		return cmdResult{Out: out}, true
	}
	e := f.env("")
	c := testCollector(t, dir, &now)

	a, note := c.securityAuth(context.Background(), e, now, 1)
	if note != "" || a == nil || a.Failed != 2 || a.Accepted != 1 || a.Source != "journal" {
		t.Fatalf("1. tur: %+v %q", a, note)
	}
	if s := argWith(f.calls[0], "--since"); s == "" || !slices.Contains(f.calls[0], now.Add(-24*time.Hour).Local().Format("2006-01-02 15:04:05")) {
		t.Fatalf("ilk tur --since yok: %v", f.calls[0])
	}
	cur4 := "s=6a1f;i=12fa03;b=d0e3;m=2a6;t=65fe3;x=e8"
	// Ack'siz ikinci tur: yeni kayıt yok, birikim aynı (tekrar sayım yok), imleçten devam.
	a, _ = c.securityAuth(context.Background(), e, now, 2)
	if a.Failed != 2 || argWith(f.calls[1], "--after-cursor=") != "--after-cursor="+cur4 {
		t.Fatalf("2. tur: %+v %v", a, f.calls[1])
	}
	// Üçüncü turda yeni kayıtlar; sonra eski raporun (2) Ack'i gelmez, yalnız son raporunki.
	next = strings.Join(lines[4:], "")
	a, _ = c.securityAuth(context.Background(), e, now, 3)
	if a.Failed != 3 || a.Accepted != 1 {
		t.Fatalf("3. tur: %+v", a)
	}
	c.AckSecurity(Security{Time: 2, Auth: a}) // eski rapor: yok sayılır
	if c.auth.pending.Failed != 3 {
		t.Fatalf("eski rapor Ack'i düşmemeli: %+v", c.auth.pending)
	}
	// Durum dosyası: imleç + bekleyen fark; yeni toplayıcı geri yükler.
	var d secStateDisk
	b, err := os.ReadFile(filepath.Join(dir, secStateFile))
	if err != nil || json.Unmarshal(b, &d) != nil || d.JournalCursor != "s=6a1f;i=12fa05;b=d0e3;m=2a6;t=65fe5;x=e8" || d.Pending == nil || d.Pending.Failed != 3 {
		t.Fatalf("durum dosyası: %s %v", b, err)
	}
	if st, _ := os.Stat(filepath.Join(dir, secStateFile)); st.Mode().Perm() != 0o600 {
		t.Fatalf("izin %v", st.Mode())
	}
	c2 := testCollector(t, dir, &now)
	a2, _ := c2.securityAuth(context.Background(), e, now, 10)
	if a2.Failed != 3 || argWith(f.calls[len(f.calls)-1], "--after-cursor=") == "" {
		t.Fatalf("geri yükleme: %+v", a2)
	}
	c.AckSecurity(Security{Time: 3, Auth: a})
	a, _ = c.securityAuth(context.Background(), e, now, 4)
	if a == nil || a.Failed != 0 || a.Accepted != 0 || a.Source != "journal" {
		t.Fatalf("Ack sonrası: %+v", a)
	}
	b, _ = os.ReadFile(filepath.Join(dir, secStateFile))
	d = secStateDisk{}
	if json.Unmarshal(b, &d) != nil || d.Pending != nil {
		t.Fatalf("Ack sonrası durum: %s", b)
	}
}

func TestSecurityAuthFileFlow(t *testing.T) {
	if !secExtrasSupported {
		t.Skip("inode yalnız Linux'ta")
	}
	root := t.TempDir()
	logDir := filepath.Join(root, "var", "log")
	os.MkdirAll(logDir, 0o755)
	logf := filepath.Join(logDir, "auth.log")
	os.WriteFile(logf, []byte(secFixture(t, "auth_debian.log")), 0o644)
	now := authNow
	f := &fakeSecEnv{tools: map[string]bool{"journalctl": true}} // journal hata verir, çıktı yok -> dosya
	e := f.env(root)
	c := testCollector(t, "", &now)

	a, note := c.securityAuth(context.Background(), e, now, 1)
	if note != "" || a.Source != "file" || a.Failed != 8 || a.Accepted != 1 { // 24 saatten eski satır sayılmaz
		t.Fatalf("1. tur: %+v %q", a, note)
	}
	c.AckSecurity(Security{Time: 1, Auth: a})
	// Yarım satır bir sonraki tura kalır.
	fh, _ := os.OpenFile(logf, os.O_APPEND|os.O_WRONLY, 0)
	fh.WriteString("Nov 10 11:00:00 web1 sshd[1200]: Failed password for root from 192.0.2.33 port 1 ssh2\nNov 10 11:00:01 web1 sshd[1201]: Failed password for ")
	a, _ = c.securityAuth(context.Background(), e, now, 2)
	if a.Failed != 1 {
		t.Fatalf("2. tur: %+v", a)
	}
	c.AckSecurity(Security{Time: 2, Auth: a})
	fh.WriteString("admin from 192.0.2.34 port 2 ssh2\n")
	fh.Close()
	a, _ = c.securityAuth(context.Background(), e, now, 3)
	if a.Failed != 1 || a.TopIPs[0].Key != "192.0.2.34" || a.TopUsers[0].Key != "admin" {
		t.Fatalf("3. tur (satır tamamlandı): %+v", a)
	}
	c.AckSecurity(Security{Time: 3, Auth: a})
	// Döndürme: yeni inode baştan okunur.
	os.Rename(logf, logf+".1")
	os.WriteFile(logf, []byte("Nov 10 11:30:00 web1 sshd-session[1300]: Accepted password for ali from 192.0.2.40 port 3 ssh2\n"), 0o644)
	a, _ = c.securityAuth(context.Background(), e, now, 4)
	if a.Failed != 0 || a.Accepted != 1 {
		t.Fatalf("döndürme: %+v", a)
	}
	c.AckSecurity(Security{Time: 4, Auth: a})
	// Kesme (boyut < konum): baştan.
	os.WriteFile(logf, []byte("Nov 10 11:40:00 web1 sshd[1400]: Invalid user z from 192.0.2.41 port 4\n"), 0o644)
	if a, _ = c.securityAuth(context.Background(), e, now, 5); a.Failed != 1 || a.Invalid != 1 {
		t.Fatalf("kesme: %+v", a)
	}
	// Kaynak yok.
	c3 := testCollector(t, "", &now)
	if a, note := c3.securityAuth(context.Background(), (&fakeSecEnv{}).env(t.TempDir()), now, 1); a != nil || note != NoteNoSource {
		t.Fatalf("kaynak yok: %+v %q", a, note)
	}
}

func TestSecuritySessionsAndFirewallFlow(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "run"), 0o755)
	os.WriteFile(filepath.Join(root, "run", "utmp"), utmpSample(), 0o644)
	f := &fakeSecEnv{}
	if s, note := secSessions(context.Background(), f.env(root)); note != "" || len(s) != 4 {
		t.Fatalf("utmp: %v %q", s, note)
	}
	// utmp yok: loginctl.
	f = &fakeSecEnv{tools: map[string]bool{"loginctl": true}}
	f.fn = func(name string, args []string) (cmdResult, bool) {
		switch {
		case args[0] == "list-sessions":
			return cmdResult{Out: secFixture(t, "loginctl_list.txt")}, true
		case args[1] == "1":
			return cmdResult{Out: secFixture(t, "loginctl_show_ssh.txt")}, true
		case args[1] == "2":
			return cmdResult{Out: secFixture(t, "loginctl_show_manager.txt")}, true
		}
		return cmdResult{Out: secFixture(t, "loginctl_show_tty.txt")}, true
	}
	if s, note := secSessions(context.Background(), f.env(t.TempDir())); note != "" || len(s) != 2 || s[0].Kind != SessionSSH {
		t.Fatalf("loginctl: %v %q", s, note)
	}
	if s, note := secSessions(context.Background(), (&fakeSecEnv{}).env(t.TempDir())); s != nil || note != NoteNoSource {
		t.Fatalf("kaynak yok: %v %q", s, note)
	}

	fw := func(tools map[string]bool, out map[string]cmdResult) (*SecFirewall, string) {
		return secFirewall(context.Background(), (&fakeSecEnv{tools: tools, out: out}).env(""))
	}
	all := map[string]bool{"firewall-cmd": true, "nft": true}
	got, note := fw(all, map[string]cmdResult{"firewall-cmd --state": {Out: "running\n"}, "nft -j": {Out: secFixture(t, "nft_firewalld.json")}})
	if note != "" || *got != (SecFirewall{Active: true, Backend: FirewallFirewalld, Rules: 7, Policy: "accept"}) {
		t.Fatalf("firewalld: %+v %q", got, note)
	}
	got, _ = fw(all, map[string]cmdResult{"firewall-cmd --state": {Out: "not running\n", Code: 252}, "nft -j": {Out: secFixture(t, "nft_docker_only.json")}})
	if *got != (SecFirewall{Active: false, Backend: FirewallNft, Policy: "accept"}) {
		t.Fatalf("yalnız docker: %+v", got)
	}
	got, _ = fw(map[string]bool{"iptables-legacy": true}, map[string]cmdResult{"iptables-legacy -S": {Out: secFixture(t, "iptables_legacy_drop.txt")}})
	if *got != (SecFirewall{Active: true, Backend: FirewallIptables, Rules: 4, Policy: "accept"}) {
		t.Fatalf("iptables-legacy: %+v", got)
	}
	if got, note = fw(nil, nil); got != nil || note != NoteNoTool {
		t.Fatalf("araç yok: %+v %q", got, note)
	}
	if got, note = fw(map[string]bool{"nft": true}, map[string]cmdResult{"nft -j": {Code: 1, Stderr: "Operation not permitted"}}); got != nil || note != NoteFailed {
		t.Fatalf("nft hatası: %+v %q", got, note)
	}
}

func TestSecurityExtrasContainer(t *testing.T) {
	now := authNow
	c := testCollector(t, "", &now)
	s := Security{Notes: map[string]string{}}
	c.securityExtras(context.Background(), secEnv{container: true}, now, &s)
	want := NoteNoHost
	if !secExtrasSupported {
		want = NoteUnsupported
	}
	if s.Auth != nil || s.Sessions != nil || s.Firewall != nil || s.Notes[SecSectionAuth] != want ||
		s.Notes[SecSectionSessions] != want || s.Notes[SecSectionFirewall] != want {
		t.Fatalf("konteyner: %+v", s)
	}
}
