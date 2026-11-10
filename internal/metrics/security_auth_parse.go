package metrics

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"maps"
	"net"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SSH girişleri, oturumlar ve güvenlik duvarı için saf ayrıştırıcılar (iş 732,
// docs/GUVENLIK-EKLENTI-730.md §A.1). Build etiketi yok: her ortamda
// testdata/sec/ örnekleriyle sınanır. Düzenli ifadeler bu depoya özgüdür.

// authEvent sshd günlüğünden bir satır (journal ya da syslog dosyası).
type authEvent struct {
	Time time.Time
	PID  string // "" bilinmiyor: satır kendi başına bir grup sayılır
	Msg  string
}

// authKind satırın sınıfı.
type authKind int

const (
	authNone      authKind = iota
	authFailed             // "Failed <yöntem> for ..." (her satır bir deneme)
	authAccepted           // "Accepted <yöntem> for ..."
	authSecondary          // Invalid user / PAM authentication failure / maximum attempts (pid başına en çok 1)
)

// authLine sınıflandırılmış satır.
type authLine struct {
	Kind    authKind
	User    string
	IP      string // yalnız net.ParseIP geçerse dolu
	Invalid bool   // var olmayan kullanıcı
	N       int    // "message repeated N times" ile çoğaltma (en az 1)
}

var (
	sshRepeatRe   = regexp.MustCompile(`^message repeated (\d+) times: \[ ?(.*?) ?\]$`)
	sshFailedRe   = regexp.MustCompile(`^Failed \S+ for (invalid user )?(.*?) from (\S+) port \d+`)
	sshAcceptedRe = regexp.MustCompile(`^Accepted \S+ for (.*?) from (\S+) port \d+`)
	sshInvalidRe  = regexp.MustCompile(`^Invalid user (.*?) from (\S+)(?: port \d+)?\s*$`)
	sshMaxRe      = regexp.MustCompile(`maximum authentication attempts exceeded for (invalid user )?(.*?) from (\S+)(?: port \d+)?`)
	sshPamRe      = regexp.MustCompile(`authentication failure;.*\brhost=(\S*)`)
	sshPamUserRe  = regexp.MustCompile(`(?:^|\s)user=(\S+)`)
)

// validIP adres yalnız IP olarak çözülürse döner (UseDNS ile gelen ad sayılmaz).
func validIP(s string) string {
	if net.ParseIP(s) == nil {
		return ""
	}
	return s
}

// classifySSH bir sshd iletisini sınıflandırır.
func classifySSH(msg string) authLine {
	msg = strings.TrimSpace(msg)
	n := 1
	if m := sshRepeatRe.FindStringSubmatch(msg); m != nil {
		if k, err := strconv.Atoi(m[1]); err == nil && k > 0 {
			n = min(k, 10000)
		}
		msg = m[2]
	}
	if m := sshFailedRe.FindStringSubmatch(msg); m != nil {
		return authLine{Kind: authFailed, Invalid: m[1] != "", User: m[2], IP: validIP(m[3]), N: n}
	}
	if m := sshAcceptedRe.FindStringSubmatch(msg); m != nil {
		return authLine{Kind: authAccepted, User: m[1], IP: validIP(m[2]), N: n}
	}
	if m := sshInvalidRe.FindStringSubmatch(msg); m != nil {
		return authLine{Kind: authSecondary, Invalid: true, User: m[1], IP: validIP(m[2]), N: 1}
	}
	if m := sshMaxRe.FindStringSubmatch(msg); m != nil {
		return authLine{Kind: authSecondary, Invalid: m[1] != "", User: m[2], IP: validIP(m[3]), N: 1}
	}
	if m := sshPamRe.FindStringSubmatch(msg); m != nil {
		l := authLine{Kind: authSecondary, IP: validIP(m[1]), N: 1}
		if u := sshPamUserRe.FindStringSubmatch(msg); u != nil {
			l.User = u[1]
		}
		return l
	}
	return authLine{}
}

// countAuth bir partideki olayları sayar. Çift sayma kuralı: aynı pid'in her
// "Failed" satırı bir deneme; ikincil işaretler (Invalid user, PAM, maximum
// attempts) pid başına en çok bir deneme ve yalnız o pid'de hiç "Failed" yoksa.
func countAuth(agg *authAgg, events []authEvent) {
	type group struct {
		failed bool
		second *authLine
		at     time.Time
	}
	groups := map[string]*group{}
	var order []string
	for i, ev := range events {
		l := classifySSH(ev.Msg)
		if l.Kind == authNone {
			continue
		}
		key := ev.PID
		if key == "" {
			key = "#" + strconv.Itoa(i)
		}
		g := groups[key]
		if g == nil {
			g = &group{}
			groups[key] = g
			order = append(order, key)
		}
		switch l.Kind {
		case authFailed:
			g.failed = true
			agg.addFailed(ev.Time, l.IP, l.User, l.Invalid, l.N)
		case authAccepted:
			agg.addAccepted(ev.Time, l.N)
		case authSecondary:
			if g.second == nil {
				g.second, g.at = &l, ev.Time
			} else {
				// Bilgiyi tamamla (PAM satırında kullanıcı yoksa Invalid user satırından).
				if g.second.User == "" {
					g.second.User = l.User
				}
				if g.second.IP == "" {
					g.second.IP = l.IP
				}
				g.second.Invalid = g.second.Invalid || l.Invalid
			}
		}
	}
	for _, k := range order {
		if g := groups[k]; !g.failed && g.second != nil {
			agg.addFailed(g.at, g.second.IP, g.second.User, g.second.Invalid, 1)
		}
	}
}

// maxAuthKeys IP ve kullanıcı haritalarının ayrı anahtar sınırı.
const maxAuthKeys = 5000

// maxAggHours biriktiricinin tuttuğu saat kovası (uzun kesintide eskiler birleşir).
const maxAggHours = 24 * 14

// authHour bir saatteki sayılar.
type authHour struct {
	F int `json:"f"`
	A int `json:"a"`
}

// authAgg gönderilmemiş SSH giriş farkı (durum dosyasında "pending").
type authAgg struct {
	Failed   int                `json:"failed"`
	Accepted int                `json:"accepted"`
	Invalid  int                `json:"invalid"`
	Hours    map[int64]authHour `json:"hours,omitempty"`
	IPs      map[string]int     `json:"ips,omitempty"`
	Users    map[string]int     `json:"users,omitempty"`
	Locked   []SecLock          `json:"locked,omitempty"` // Windows 4740 (gönderilene dek, en çok MaxSecLocked)
}

func (a *authAgg) hour(t time.Time) int64 {
	h := t.Unix()
	h -= h % 3600
	if a.Hours == nil {
		a.Hours = map[int64]authHour{}
	}
	if _, ok := a.Hours[h]; !ok && len(a.Hours) >= maxAggHours {
		keys := slices.Sorted(maps.Keys(a.Hours))
		old, next := a.Hours[keys[0]], a.Hours[keys[1]]
		next.F += old.F
		next.A += old.A
		a.Hours[keys[1]] = next
		delete(a.Hours, keys[0])
	}
	return h
}

func addKey(m *map[string]int, k string, n int) {
	if k == "" {
		return
	}
	if *m == nil {
		*m = map[string]int{}
	}
	if _, ok := (*m)[k]; !ok && len(*m) >= maxAuthKeys {
		return
	}
	(*m)[k] += n
}

func (a *authAgg) addFailed(t time.Time, ip, user string, invalid bool, n int) {
	h := a.hour(t)
	x := a.Hours[h]
	x.F += n
	a.Hours[h] = x
	a.Failed += n
	if invalid {
		a.Invalid += n
	}
	addKey(&a.IPs, ip, n)
	addKey(&a.Users, user, n)
}

func (a *authAgg) addAccepted(t time.Time, n int) {
	h := a.hour(t)
	x := a.Hours[h]
	x.A += n
	a.Hours[h] = x
	a.Accepted += n
}

// addLocked kilitlenmeyi ekler; liste doluysa yenisi atılır (sub önden düşer).
func (a *authAgg) addLocked(l SecLock) {
	if len(a.Locked) < MaxSecLocked {
		a.Locked = append(a.Locked, l)
	}
}

func (a *authAgg) empty() bool {
	return a == nil || (a.Failed == 0 && a.Accepted == 0 && len(a.Locked) == 0)
}

func (a *authAgg) clone() *authAgg {
	if a == nil {
		return &authAgg{}
	}
	return &authAgg{Failed: a.Failed, Accepted: a.Accepted, Invalid: a.Invalid,
		Hours: maps.Clone(a.Hours), IPs: maps.Clone(a.IPs), Users: maps.Clone(a.Users), Locked: slices.Clone(a.Locked)}
}

// sub gönderilip kabul edilen kopyayı düşer (sıfıra iner, eksiye düşmez).
// Kopya alındıktan sonra birleşen eski saat kovası en eski kovadan düşülür.
func (a *authAgg) sub(o *authAgg) {
	if o == nil {
		return
	}
	a.Failed = max(a.Failed-o.Failed, 0)
	a.Accepted = max(a.Accepted-o.Accepted, 0)
	a.Locked = slices.Clone(a.Locked[min(len(o.Locked), len(a.Locked)):])
	if len(a.Locked) == 0 {
		a.Locked = nil
	}
	a.Invalid = max(a.Invalid-o.Invalid, 0)
	for h, x := range o.Hours {
		k := h
		if _, ok := a.Hours[k]; !ok && len(a.Hours) > 0 {
			k = slices.Min(slices.Collect(maps.Keys(a.Hours)))
		}
		y, ok := a.Hours[k]
		if !ok {
			continue
		}
		y.F, y.A = max(y.F-x.F, 0), max(y.A-x.A, 0)
		if y.F == 0 && y.A == 0 {
			delete(a.Hours, k)
		} else {
			a.Hours[k] = y
		}
	}
	for _, p := range []struct{ dst, src map[string]int }{{a.IPs, o.IPs}, {a.Users, o.Users}} {
		for k, n := range p.src {
			if v := p.dst[k] - n; v > 0 {
				p.dst[k] = v
			} else {
				delete(p.dst, k)
			}
		}
	}
}

// topOf haritayı sayıya göre azalan (eşitte anahtar) sıralar, ilk n'yi döndürür.
func topOf(m map[string]int, n int) []SecCount {
	out := make([]SecCount, 0, len(m))
	for k, v := range m {
		if v > 0 {
			out = append(out, SecCount{Key: k, Count: v})
		}
	}
	slices.SortFunc(out, func(a, b SecCount) int {
		if a.Count != b.Count {
			return b.Count - a.Count
		}
		return strings.Compare(a.Key, b.Key)
	})
	if len(out) > n {
		out = out[:n]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ToAuth tel biçimi: saatler sıralı, MaxSecAuthHours'tan eskiler en eski
// kovaya birleşir; IP ve kullanıcılar ilk MaxSecAuthIPs / MaxSecAuthUsers.
func (a *authAgg) ToAuth(source string) *SecAuth {
	out := &SecAuth{Source: source}
	if a == nil {
		return out
	}
	out.Failed, out.Accepted, out.Invalid = a.Failed, a.Accepted, min(a.Invalid, a.Failed)
	keys := slices.Sorted(maps.Keys(a.Hours))
	for _, h := range keys {
		x := a.Hours[h]
		if x.F == 0 && x.A == 0 {
			continue
		}
		out.Hours = append(out.Hours, SecAuthHour{Hour: h, Failed: x.F, Accepted: x.A})
	}
	if n := len(out.Hours) - MaxSecAuthHours; n > 0 {
		keep := &out.Hours[n]
		for _, x := range out.Hours[:n] {
			keep.Failed += x.Failed
			keep.Accepted += x.Accepted
		}
		out.Hours = out.Hours[n:]
	}
	out.TopIPs = topOf(a.IPs, MaxSecAuthIPs)
	out.TopUsers = topOf(a.Users, MaxSecAuthUsers)
	out.Locked = slices.Clone(a.Locked)
	return out
}

// parseJournalSSH `journalctl -o json` satırları; son imleci de döndürür.
// MESSAGE dize ya da (UTF-8 dışı içerikte) bayt dizisi olabilir.
func parseJournalSSH(out string) (events []authEvent, cursor string) {
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		var r struct {
			Cursor  string          `json:"__CURSOR"`
			RT      string          `json:"__REALTIME_TIMESTAMP"`
			Message json.RawMessage `json:"MESSAGE"`
			PID     string          `json:"_PID"`
		}
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		if r.Cursor != "" {
			cursor = r.Cursor
		}
		msg := journalString(r.Message)
		if msg == "" {
			continue
		}
		us, _ := strconv.ParseInt(r.RT, 10, 64)
		events = append(events, authEvent{Time: time.UnixMicro(us), PID: r.PID, Msg: msg})
	}
	return events, cursor
}

func journalString(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var nums []int
	if json.Unmarshal(raw, &nums) == nil {
		b := make([]byte, 0, len(nums))
		for _, n := range nums {
			b = append(b, byte(n))
		}
		return string(b)
	}
	return ""
}

var (
	syslogOldRe = regexp.MustCompile(`^([A-Z][a-z]{2} [ \d]\d \d\d:\d\d:\d\d) \S+ ([^\s\[:]+)(?:\[(\d+)\])?: (.*)$`)
	syslogNewRe = regexp.MustCompile(`^(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:?\d\d)) \S+ ([^\s\[:]+)(?:\[(\d+)\])?: (.*)$`)
)

// parseSyslogSSH syslog dosyası satırı (auth.log, secure). Yalnız sshd ve
// sshd-session programları. Eski biçimde yıl yoktur: now'ın yılı, sonuç
// gelecekteyse (1 sa pay) geçen yıl; saat now'ın konumunda.
func parseSyslogSSH(line string, now time.Time) (authEvent, bool) {
	line = strings.TrimRight(line, "\r\n")
	var ts, prog, pid, msg string
	var t time.Time
	if m := syslogNewRe.FindStringSubmatch(line); m != nil {
		ts, prog, pid, msg = m[1], m[2], m[3], m[4]
		var err error
		if t, err = time.Parse(time.RFC3339Nano, ts); err != nil {
			if t, err = time.Parse("2006-01-02T15:04:05.999999999-0700", ts); err != nil {
				return authEvent{}, false
			}
		}
	} else if m := syslogOldRe.FindStringSubmatch(line); m != nil {
		ts, prog, pid, msg = m[1], m[2], m[3], m[4]
		p, err := time.ParseInLocation("Jan _2 15:04:05", ts, now.Location())
		if err != nil {
			if p, err = time.ParseInLocation("Jan 2 15:04:05", ts, now.Location()); err != nil {
				return authEvent{}, false
			}
		}
		t = time.Date(now.Year(), p.Month(), p.Day(), p.Hour(), p.Minute(), p.Second(), 0, now.Location())
		if t.After(now.Add(time.Hour)) {
			t = t.AddDate(-1, 0, 0)
		}
	} else {
		return authEvent{}, false
	}
	if prog != "sshd" && prog != "sshd-session" {
		return authEvent{}, false
	}
	return authEvent{Time: t, PID: pid, Msg: msg}, true
}

// glibc Linux utmp kaydı (x86_64, arm64 vb.: 384 bayt).
const (
	utmpSize        = 384
	utmpUserProcess = 7
)

func cstr(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(strings.ToValidUTF8(string(b), ""))
}

// parseUtmp USER_PROCESS kayıtları; alive süreci yaşamayan (temizlenmemiş)
// kayıtları eler. Uzak adresi olan (X ekranı ":0" hariç) ssh sayılır.
func parseUtmp(b []byte, alive func(pid int) bool) []SecSession {
	out := []SecSession{}
	bo := binary.NativeEndian
	for off := 0; off+utmpSize <= len(b) && len(out) < MaxSecSessions; off += utmpSize {
		r := b[off : off+utmpSize]
		if int16(bo.Uint16(r[0:])) != utmpUserProcess {
			continue
		}
		pid := int(int32(bo.Uint32(r[4:])))
		if alive != nil && !alive(pid) {
			continue
		}
		s := SecSession{TTY: cstr(r[8:40]), User: cstr(r[44:76]), From: cstr(r[76:332]),
			Since: int64(int32(bo.Uint32(r[340:]))), Kind: SessionTTY}
		if s.User == "" {
			continue
		}
		if s.From != "" && !strings.HasPrefix(s.From, ":") {
			s.Kind = SessionSSH
		} else {
			s.From = ""
		}
		out = append(out, s)
	}
	return out
}

// parseLoginctlList `loginctl list-sessions --no-legend` oturum kimlikleri.
func parseLoginctlList(out string) []string {
	var ids []string
	for line := range strings.Lines(out) {
		if f := strings.Fields(line); len(f) > 0 {
			ids = append(ids, f[0])
		}
	}
	return ids
}

var loginctlTimeLayouts = []string{
	"Mon 2006-01-02 15:04:05 MST",
	"Mon 2006-01-02 15:04:05 -07",
	"Mon 2006-01-02 15:04:05 -0700",
	"Mon 2006-01-02 15:04:05 -07:00",
}

// parseLoginctlShow `loginctl show-session -p ...` Anahtar=Değer çıktısı;
// yalnız Class=user oturumlar.
func parseLoginctlShow(out string) (SecSession, bool) {
	kv := map[string]string{}
	for line := range strings.Lines(out) {
		if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			kv[k] = v
		}
	}
	if kv["Class"] != "user" || kv["Name"] == "" {
		return SecSession{}, false
	}
	s := SecSession{User: kv["Name"], TTY: kv["TTY"], From: kv["RemoteHost"], Kind: SessionTTY}
	if kv["Remote"] == "yes" || s.From != "" {
		s.Kind = SessionSSH
	}
	for _, l := range loginctlTimeLayouts {
		if t, err := time.Parse(l, kv["Timestamp"]); err == nil {
			s.Since = t.Unix()
			break
		}
	}
	return s, true
}

// fwResult güvenlik duvarı kural kümesi özeti (yalnız gelen yön).
type fwResult struct {
	Found  bool // input zinciri / INPUT bulundu
	Active bool
	Rules  int
	Policy string // accept | drop
}

// fwGraph zincir çizgesi: kurallar, inkâr (drop/reject) ve atlama hedefleri.
type fwGraph struct {
	rules map[string][]fwRule
}

type fwRule struct {
	deny    bool
	targets []string
}

// denies zincir doğrudan ya da atladığı zincirler üzerinden drop/reject içeriyor mu.
func (g fwGraph) denies(chain string, seen map[string]bool) bool {
	if seen[chain] {
		return false
	}
	seen[chain] = true
	for _, r := range g.rules[chain] {
		if r.deny {
			return true
		}
		for _, t := range r.targets {
			if g.denies(t, seen) {
				return true
			}
		}
	}
	return false
}

// count zincir ve ulaşılan zincirlerdeki kural sayısı.
func (g fwGraph) count(chain string, seen map[string]bool) int {
	if seen[chain] {
		return 0
	}
	seen[chain] = true
	n := len(g.rules[chain])
	for _, r := range g.rules[chain] {
		for _, t := range r.targets {
			n += g.count(t, seen)
		}
	}
	return n
}

// nftWalk JSON ifadesinde drop/reject anahtarı ve jump/goto hedeflerini arar.
func nftWalk(v any, r *fwRule) {
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			switch k {
			case "drop", "reject":
				r.deny = true
			case "jump", "goto":
				if m, ok := sub.(map[string]any); ok {
					if t, ok := m["target"].(string); ok {
						r.targets = append(r.targets, t)
					}
				}
			}
			nftWalk(sub, r)
		}
	case []any:
		for _, sub := range x {
			nftWalk(sub, r)
		}
	}
}

// parseNftRuleset `nft -j list ruleset`: ip/ip6/inet ailesinde hook input,
// filter türü zincirler. Docker'ın forward/nat zincirleri sayılmaz.
func parseNftRuleset(b []byte) (fwResult, error) {
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return fwResult{}, err
	}
	type chain struct {
		Family, Table, Name, Type, Hook, Policy string
	}
	g := fwGraph{rules: map[string][]fwRule{}}
	var inputs []string
	policy := ""
	for _, obj := range doc.Nftables {
		if raw, ok := obj["chain"]; ok {
			var c chain
			if json.Unmarshal(raw, &c) != nil {
				continue
			}
			key := c.Family + "\x00" + c.Table + "\x00" + c.Name
			switch c.Family {
			case "ip", "ip6", "inet":
			default:
				continue
			}
			if c.Hook == "input" && (c.Type == "" || c.Type == "filter") {
				inputs = append(inputs, key)
				p := strings.ToLower(c.Policy)
				if p == "" {
					p = "accept"
				}
				if p == "drop" || policy == "" {
					policy = p
				}
			}
			if _, ok := g.rules[key]; !ok {
				g.rules[key] = nil
			}
		}
		if raw, ok := obj["rule"]; ok {
			var r struct {
				Family, Table, Chain string
				Expr                 []any
			}
			if json.Unmarshal(raw, &r) != nil {
				continue
			}
			var fr fwRule
			nftWalk(r.Expr, &fr)
			for i, t := range fr.targets {
				fr.targets[i] = r.Family + "\x00" + r.Table + "\x00" + t
			}
			key := r.Family + "\x00" + r.Table + "\x00" + r.Chain
			g.rules[key] = append(g.rules[key], fr)
		}
	}
	if len(inputs) == 0 {
		return fwResult{}, nil
	}
	res := fwResult{Found: true, Policy: policy}
	seenD, seenC := map[string]bool{}, map[string]bool{}
	for _, k := range inputs {
		res.Rules += g.count(k, seenC)
		if g.denies(k, seenD) {
			res.Active = true
		}
	}
	if policy == "drop" || policy == "reject" {
		res.Active = true
	}
	return res, nil
}

// parseIptablesS `iptables-legacy -S` (filter tablosu): INPUT politikası,
// kuralları ve -j/-g ile ulaşılan kullanıcı zincirleri.
func parseIptablesS(out string) fwResult {
	g := fwGraph{rules: map[string][]fwRule{}}
	res := fwResult{}
	for line := range strings.Lines(out) {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "-P":
			if f[1] == "INPUT" && len(f) >= 3 {
				res.Found = true
				res.Policy = strings.ToLower(f[2])
			}
		case "-A":
			var r fwRule
			for i := 2; i+1 < len(f); i++ {
				if f[i] != "-j" && f[i] != "-g" {
					continue
				}
				switch t := f[i+1]; t {
				case "DROP", "REJECT":
					r.deny = true
				case "ACCEPT", "RETURN", "LOG", "MARK", "CONNMARK", "NFLOG":
				default:
					r.targets = append(r.targets, t)
				}
			}
			if f[1] == "INPUT" {
				res.Found = true
			}
			g.rules[f[1]] = append(g.rules[f[1]], r)
		}
	}
	if !res.Found {
		return res
	}
	res.Rules = g.count("INPUT", map[string]bool{})
	res.Active = res.Policy == "drop" || res.Policy == "reject" || g.denies("INPUT", map[string]bool{})
	return res
}

// parseUfwStatus `ufw status`: "Status: active".
func parseUfwStatus(out string) bool {
	for line := range strings.Lines(out) {
		if strings.EqualFold(strings.TrimSpace(line), "Status: active") {
			return true
		}
	}
	return false
}

// parseFirewalldState `firewall-cmd --state`: "running".
func parseFirewalldState(out string) bool { return strings.TrimSpace(out) == "running" }
