package metrics

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SSH girişleri (fark + onay), oturumlar ve güvenlik duvarı toplama akışı (iş
// 732). Kodun kendisi her platformda derlenir; yalnız secExtrasSupported olan
// sistemde (Linux) çağrılır. Ayrıştırıcılar security_auth_parse.go'da.

const (
	secStateFile    = "uptime.sec.json"
	authFirstWindow = 24 * time.Hour
	authFirstTail   = 4 << 20  // ilk turda dosyanın son 4 MiB'ı
	authMaxRead     = 64 << 20 // tur başına en çok okunan bayt
	authMaxLines    = 200000   // tur başına en çok satır
	authJournalMax  = 50000    // journalctl -n (tur başına en çok kayıt; bellek sınırı)
)

// authFilePos dosya yedeğinde kalınan yer.
type authFilePos struct {
	Path   string `json:"path"`
	Inode  uint64 `json:"inode"`
	Offset int64  `json:"offset"`
}

// secStateDisk durum dosyası (<StateDir>/uptime.sec.json).
type secStateDisk struct {
	JournalCursor string      `json:"journal_cursor"`
	File          authFilePos `json:"file"`
	EventRecord   uint64      `json:"event_record,omitempty"` // Windows Security günlüğü: son okunan EventRecordID (734)
	Pending       *authAgg    `json:"pending,omitempty"`
}

// secAuthState Collector içindeki SSH durumu.
type secAuthState struct {
	mu        sync.Mutex
	loaded    bool
	cursor    string
	since     time.Time // imleç yokken bir önceki boş turun zamanı
	file      authFilePos
	evtRecord uint64 // Windows: son okunan EventRecordID
	source    string
	pending   *authAgg // onaylanmamış toplam fark
	sending   *authAgg // son Security() anındaki kopya (AckSecurity düşer)
	sendingAt int64    // o raporun Time değeri
}

// securityExtras SSH, oturum ve güvenlik duvarı bölümlerini doldurur.
func (c *Collector) securityExtras(ctx context.Context, e secEnv, now time.Time, s *Security) {
	if !secExtrasSupported {
		if osSecExtras(ctx, e, s) {
			if winEventLog {
				var code string
				if s.Auth, code = c.securityAuth(ctx, e, now, s.Time); code != "" {
					s.Notes[SecSectionAuth] = code
				}
			}
			return
		}
		s.Notes[SecSectionAuth] = NoteUnsupported
		s.Notes[SecSectionSessions] = NoteUnsupported
		s.Notes[SecSectionFirewall] = NoteUnsupported
		return
	}
	if e.container {
		s.Notes[SecSectionAuth] = NoteNoHost
		s.Notes[SecSectionSessions] = NoteNoHost
		s.Notes[SecSectionFirewall] = NoteNoHost
		return
	}
	var code string
	if s.Auth, code = c.securityAuth(ctx, e, now, s.Time); code != "" {
		s.Notes[SecSectionAuth] = code
	}
	if s.Sessions, code = secSessions(ctx, e); code != "" {
		s.Notes[SecSectionSessions] = code
	}
	if s.Firewall, code = secFirewall(ctx, e); code != "" {
		s.Notes[SecSectionFirewall] = code
	}
}

// AckSecurity panel raporu kabul edince çağrılır: rapordaki SSH farkını
// birikimden düşer (rapordan sonra okunan olaylar kalır) ve durumu yazar.
func (c *Collector) AckSecurity(rep Security) {
	st := &c.auth
	st.mu.Lock()
	defer st.mu.Unlock()
	if rep.Auth == nil || st.sending == nil || st.sendingAt != rep.Time {
		return
	}
	if st.pending != nil {
		st.pending.sub(st.sending)
	}
	st.sending = nil
	c.saveSecState()
}

// securityAuth yeni sshd kayıtlarını okur, birikime ekler ve birikimi döndürür.
func (c *Collector) securityAuth(ctx context.Context, e secEnv, now time.Time, repTime int64) (*SecAuth, string) {
	st := &c.auth
	st.mu.Lock()
	defer st.mu.Unlock()
	if !st.loaded {
		st.loaded = true
		c.loadSecState()
	}
	if st.pending == nil {
		st.pending = &authAgg{}
	}
	pos := func() string {
		return st.cursor + "|" + strconv.FormatInt(st.file.Offset, 10) + "|" + strconv.FormatUint(st.file.Inode, 10) +
			"|" + strconv.FormatUint(st.evtRecord, 10)
	}
	before := pos()
	added := st.pending.Failed + st.pending.Accepted + len(st.pending.Locked)
	var source, note string
	if winEventLog {
		source, note = c.readEventLog(ctx, now)
	} else {
		source, note = c.readAuth(ctx, e, now)
	}
	if before != pos() || added != st.pending.Failed+st.pending.Accepted+len(st.pending.Locked) {
		c.saveSecState()
	}
	if source != "" {
		st.source = source
	}
	if source == "" && st.pending.empty() {
		return nil, note
	}
	st.sending, st.sendingAt = st.pending.clone(), repTime
	return st.sending.ToAuth(st.source), note
}

// readAuth önce journal, olmazsa auth.log / secure. Kaynak adı ya da not döner.
func (c *Collector) readAuth(ctx context.Context, e secEnv, now time.Time) (string, string) {
	st := &c.auth
	if e.look("journalctl") {
		args := []string{"-q", "-o", "json", "--output-fields=MESSAGE,_PID,__REALTIME_TIMESTAMP",
			"-n", strconv.Itoa(authJournalMax), "-t", "sshd", "-t", "sshd-session"}
		switch {
		case st.cursor != "":
			args = append(args, "--after-cursor="+st.cursor)
		case !st.since.IsZero():
			args = append(args, "--since", st.since.Local().Format("2006-01-02 15:04:05"))
		default:
			args = append(args, "--since", now.Add(-authFirstWindow).Local().Format("2006-01-02 15:04:05"))
		}
		r := e.run(ctx, secCmdTimeout, "journalctl", args...)
		if r.Err == nil && (r.Code == 0 || strings.TrimSpace(r.Out) != "") {
			events, cur := parseJournalSSH(r.Out)
			countAuth(st.pending, events)
			if cur != "" {
				st.cursor, st.since = cur, time.Time{}
			} else if st.cursor == "" {
				st.since = now
			}
			return "journal", ""
		}
	}
	for _, p := range []string{"/var/log/auth.log", "/var/log/secure"} {
		ok, err := c.readAuthFile(e.root+p, now)
		if ok {
			return "file", ""
		}
		if err != nil {
			c.opts.Log.Debug("ssh günlüğü okunamadı", "dosya", p, "hata", err)
		}
	}
	return "", NoteNoSource
}

// readAuthFile dosyayı kaldığı yerden okur. Dosya yoksa (false, nil).
// İnode değiştiyse ya da dosya kısaldıysa baştan; ilk turda (konum yok) son
// authFirstTail bayt ve yalnız son 24 saat. Yarım satır bir sonraki tura kalır.
func (c *Collector) readAuthFile(path string, now time.Time) (bool, error) {
	st := &c.auth
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return false, err
	}
	ino, size := fileInode(fi), fi.Size()
	pos := st.file
	first := pos.Path != path
	var start int64
	var cutoff time.Time
	switch {
	case first:
		start = max(size-authFirstTail, 0)
		cutoff = now.Add(-authFirstWindow)
	case pos.Inode != ino || size < pos.Offset:
		start = 0 // döndürüldü ya da kesildi
	default:
		start = pos.Offset
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return false, err
	}
	br := bufio.NewReaderSize(io.LimitReader(f, authMaxRead), 256<<10)
	off := start
	if first && start > 0 {
		// Ortadan başlandı: ilk (yarım) satırı atla.
		skip, err := br.ReadBytes('\n')
		if err != nil {
			st.file = authFilePos{Path: path, Inode: ino, Offset: start}
			return true, nil
		}
		off += int64(len(skip))
	}
	var events []authEvent
	for lines := 0; lines < authMaxLines; lines++ {
		line, err := br.ReadBytes('\n')
		if err != nil {
			break // yarım satır ya da dosya sonu: konum satır başında kalır
		}
		off += int64(len(line))
		if !bytes.Contains(line, []byte("sshd")) {
			continue
		}
		ev, ok := parseSyslogSSH(string(line), now)
		if !ok || (!cutoff.IsZero() && ev.Time.Before(cutoff)) {
			continue
		}
		events = append(events, ev)
	}
	countAuth(st.pending, events)
	st.file = authFilePos{Path: path, Inode: ino, Offset: off}
	return true, nil
}

func (c *Collector) statePath() string {
	if c.opts.StateDir == "" {
		return ""
	}
	return filepath.Join(c.opts.StateDir, secStateFile)
}

// loadSecState durum dosyasını okur (yoksa ya da bozuksa sıfırdan).
func (c *Collector) loadSecState() {
	p := c.statePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var d secStateDisk
	if json.Unmarshal(b, &d) != nil {
		c.opts.Log.Warn("güvenlik durum dosyası bozuk, yok sayıldı", "dosya", p)
		return
	}
	st := &c.auth
	st.cursor, st.file, st.pending, st.evtRecord = d.JournalCursor, d.File, d.Pending, d.EventRecord
	if p := st.pending; p != nil && (p.Failed < 0 || p.Accepted < 0 || p.Invalid < 0) {
		st.pending = nil
	}
}

// saveSecState durum dosyasını atomik yazar (geçici dosya + rename, 0600).
func (c *Collector) saveSecState() {
	p := c.statePath()
	if p == "" {
		return
	}
	st := &c.auth
	d := secStateDisk{JournalCursor: st.cursor, File: st.file, EventRecord: st.evtRecord}
	if !st.pending.empty() {
		d.Pending = st.pending
	}
	b, err := json.Marshal(d)
	if err != nil {
		return
	}
	if err := writeFileAtomic(p, b); err != nil {
		c.opts.Log.Debug("güvenlik durum dosyası yazılamadı", "dosya", p, "hata", err)
	}
}

func writeFileAtomic(p string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(p), ".uptime.sec.*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// secSessions utmp, yoksa loginctl. Oturum yoksa boş dizi.
func secSessions(ctx context.Context, e secEnv) ([]SecSession, string) {
	for _, p := range []string{"/var/run/utmp", "/run/utmp"} {
		b, err := os.ReadFile(e.root + p)
		if err == nil {
			return parseUtmp(b, e.alive), ""
		}
		if !os.IsNotExist(err) {
			return nil, NoteFailed
		}
	}
	if !e.look("loginctl") {
		return nil, NoteNoSource
	}
	r := e.run(ctx, secCmdTimeout, "loginctl", "list-sessions", "--no-legend")
	if r.Err != nil || r.Code != 0 {
		return nil, NoteFailed
	}
	out := []SecSession{}
	for _, id := range parseLoginctlList(r.Out) {
		if len(out) >= MaxSecSessions {
			break
		}
		r := e.run(ctx, secCmdTimeout, "loginctl", "show-session", id,
			"-p", "Name", "-p", "TTY", "-p", "RemoteHost", "-p", "Remote", "-p", "Timestamp", "-p", "Class", "-p", "Type")
		if r.Err != nil || r.Code != 0 {
			continue
		}
		if s, ok := parseLoginctlShow(r.Out); ok {
			out = append(out, s)
		}
	}
	return out, ""
}

// secFirewall sırayla firewalld, ufw, nft, iptables-legacy. firewalld/ufw
// açıksa kural sayısı ve politika nft'den tamamlanmaya çalışılır.
func secFirewall(ctx context.Context, e secEnv) (*SecFirewall, string) {
	var fw *SecFirewall
	tool, failed := false, false
	lastBackend := ""
	if e.look("firewall-cmd") {
		tool = true
		r := e.run(ctx, secCmdTimeout, "firewall-cmd", "--state")
		switch {
		case r.Err != nil:
			failed = true
		case parseFirewalldState(r.Out):
			fw = &SecFirewall{Active: true, Backend: FirewallFirewalld}
		default:
			lastBackend = FirewallFirewalld
		}
	}
	if fw == nil && e.look("ufw") {
		tool = true
		r := e.run(ctx, secCmdTimeout, "ufw", "status")
		switch {
		case r.Err != nil || (r.Code != 0 && !strings.Contains(r.Out, "Status:")):
			failed = true
		case parseUfwStatus(r.Out):
			fw = &SecFirewall{Active: true, Backend: FirewallUfw}
		default:
			lastBackend = FirewallUfw
		}
	}
	if e.look("nft") {
		tool = true
		r := e.run(ctx, secCmdTimeout, "nft", "-j", "list", "ruleset")
		res, err := parseNftRuleset([]byte(r.Out))
		switch {
		case r.Err != nil || r.Code != 0 || err != nil:
			failed = true
		case fw != nil:
			fw.Rules, fw.Policy = res.Rules, res.Policy
			return fw, ""
		case res.Found:
			return &SecFirewall{Active: res.Active, Backend: FirewallNft, Rules: res.Rules, Policy: res.Policy}, ""
		default:
			lastBackend = FirewallNft
		}
	}
	if fw != nil {
		return fw, ""
	}
	if e.look("iptables-legacy") {
		tool = true
		r := e.run(ctx, secCmdTimeout, "iptables-legacy", "-S")
		switch res := parseIptablesS(r.Out); {
		case r.Err != nil || r.Code != 0:
			failed = true
		case res.Found:
			if res.Active || lastBackend == "" {
				return &SecFirewall{Active: res.Active, Backend: FirewallIptables, Rules: res.Rules, Policy: res.Policy}, ""
			}
		}
	}
	switch {
	case lastBackend != "":
		return &SecFirewall{Backend: lastBackend}, ""
	case !tool:
		return nil, NoteNoTool
	case failed:
		return nil, NoteFailed
	}
	return nil, NoteNoTool
}

// procAlive /proc/<pid> var mı (utmp'de temizlenmemiş kayıtları eler).
func procAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	_, err := os.Stat("/proc/" + strconv.Itoa(pid))
	return err == nil
}
