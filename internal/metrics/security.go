package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

// Sunucu güvenlik durumu (iş 731, docs/GUVENLIK-EKLENTI-730.md §A.3): bekleyen
// paket güncellemeleri ve yeniden başlatma gereksinimi. Envanterden ayrı
// gönderilir: panel iş listesinde security_interval verirse (eski panel alanı
// bilmez, 0 sayılır ve ajan hiç toplamaz) ajan değişince ya da en geç 12 turda
// bir POST /api/probe/security ile yollar. SSH girişleri, oturumlar, güvenlik
// duvarı ve Defender sonraki işlerde (732-734) aynı gövdeye eklenir.

// Güvenlik raporu sınırları.
const (
	MaxSecurityBytes = 256 << 10
	MaxSecurityPkgs  = 500
)

// Güvenlik bölüm adları (Security.Notes anahtarı).
const (
	SecSectionUpdates  = "updates"
	SecSectionReboot   = "reboot"
	SecSectionAuth     = "auth"
	SecSectionSessions = "sessions"
	SecSectionFirewall = "firewall"
)

// SSH/oturum/güvenlik duvarı sınırları (iş 732).
const (
	MaxSecAuthIPs   = 20 // rapordaki IP sayısı (panel gün başına ilk 20'yi saklar)
	MaxSecAuthUsers = 5  // denenen kullanıcı adı
	MaxSecAuthHours = 48 // saat kovası (ajan uzun süre gönderemezse eskileri birleşir)
	MaxSecSessions  = 50
)

// Güvenlik bölümüne özgü not kodları (genel kodlar: NoteUnsupported,
// NoteNoHost, NoteFailed).
const (
	NoteNoManager    = "no_manager"    // desteklenen paket yöneticisi yok
	NoteCacheMissing = "cache_missing" // dnf/yum önbelleği yok (-C ile sorgu yapılamadı)
	NoteLocked       = "locked"        // paket yöneticisi kilitli (zypper), sonraki turda yeniden
	NoteNoErrata     = "no_errata"     // dağıtım güvenlik bilgisi vermiyor (CentOS 7): Security = -1
	NoteNoSource     = "no_source"     // SSH günlüğü yok (journal da auth.log/secure da okunamadı)
	NoteNoTool       = "no_tool"       // güvenlik duvarı aracı yok (firewalld/ufw/nft/iptables-legacy)
)

// Güvenlik duvarı arka uçları (SecFirewall.Backend).
const (
	FirewallFirewalld = "firewalld"
	FirewallUfw       = "ufw"
	FirewallNft       = "nftables"
	FirewallIptables  = "iptables-legacy"
)

// Oturum türleri (SecSession.Kind).
const (
	SessionSSH = "ssh"
	SessionTTY = "tty"
	SessionRDP = "rdp" // 733
)

// Paket yöneticileri (SecUpdates.Manager).
const (
	ManagerApt    = "apt"
	ManagerDnf    = "dnf"
	ManagerYum    = "yum"
	ManagerZypper = "zypper"
	ManagerWU     = "wu" // Windows Update (734)
)

// Yeniden başlatma gerekçeleri (SecReboot.Reason).
const (
	RebootFile     = "file"     // /var/run/reboot-required, /run/reboot-needed
	RebootTool     = "tool"     // needs-restarting / zypper needs-rebooting
	RebootKernel   = "kernel"   // kurulu en yeni çekirdek çalışan çekirdek değil
	RebootRegistry = "registry" // Windows: RebootRequired / RebootPending anahtarı
)

// SecPkg güncellemesi bekleyen bir paket.
type SecPkg struct {
	Name     string `json:"name"`
	Current  string `json:"current,omitempty"` // kurulu sürüm ("" bilinmiyor)
	New      string `json:"new"`
	Security bool   `json:"security,omitempty"`
	Severity string `json:"severity,omitempty"` // Critical | Important | Moderate | Low (dnf/zypper)
	ESM      bool   `json:"esm,omitempty"`      // Ubuntu Pro ESM kaynağından
}

// SecUpdates bekleyen güncellemeler. Security -1: dağıtım güvenlik bilgisi vermiyor.
type SecUpdates struct {
	Manager   string   `json:"manager"`
	Total     int      `json:"total"`
	Security  int      `json:"security"`
	IndexTime int64    `json:"index_time,omitempty"` // paket dizininin son tazelenmesi (unix sn; 0 bilinmiyor)
	CheckedAt int64    `json:"checked_at,omitempty"` // son gerçek sorgu (ajan saati, unix ms; önbellekli)
	Packages  []SecPkg `json:"packages"`
	Truncated bool     `json:"truncated,omitempty"`
}

// SecReboot yeniden başlatma gereksinimi.
type SecReboot struct {
	Required bool     `json:"required"`
	Reason   string   `json:"reason,omitempty"`
	Packages []string `json:"packages,omitempty"` // gerekçe paketleri (reboot-required.pkgs, needs-restarting)
}

// SecAuthHour bir saatlik giriş sayıları (Hour: saat başı, unix sn).
type SecAuthHour struct {
	Hour     int64 `json:"hour"`
	Failed   int   `json:"failed"`
	Accepted int   `json:"accepted"`
}

// SecCount ad (IP ya da kullanıcı) başına sayı.
type SecCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// SecAuth son başarılı gönderimden bu yana SSH giriş FARKI (birikimli değil):
// ajan panel kabul edene kadar biriktirir, kabulden sonra sıfırlar. Panel
// saatleri sec_auth_hourly'ye, IP'leri alış gününe göre sec_auth_ip_daily'ye ekler.
type SecAuth struct {
	Source   string        `json:"source"` // journal | file
	Failed   int           `json:"failed"`
	Accepted int           `json:"accepted"`
	Invalid  int           `json:"invalid"` // var olmayan kullanıcıyla denemeler (Failed'ın parçası)
	Hours    []SecAuthHour `json:"hours,omitempty"`
	TopIPs   []SecCount    `json:"top_ips,omitempty"`
	TopUsers []SecCount    `json:"top_users,omitempty"`
}

// SecSession oturum açmış kullanıcı.
type SecSession struct {
	User  string `json:"user"`
	TTY   string `json:"tty,omitempty"`
	From  string `json:"from,omitempty"`  // uzak adres (yerel oturumda boş)
	Since int64  `json:"since,omitempty"` // giriş zamanı (unix sn)
	Kind  string `json:"kind"`            // ssh | tty | rdp
	State string `json:"state,omitempty"` // Windows: Active | Disconnected
}

// SecFirewall güvenlik duvarı durumu (yalnız gelen yön: INPUT).
type SecFirewall struct {
	Active  bool   `json:"active"`
	Backend string `json:"backend,omitempty"` // firewalld | ufw | nftables | iptables-legacy
	Rules   int    `json:"rules"`             // INPUT zincirlerindeki kural sayısı (firewalld/ufw'de nft/iptables'tan; bilinmiyorsa 0)
	Policy  string `json:"policy,omitempty"`  // INPUT varsayılan politikası (accept | drop | reject)
}

// HasAuthDelta raporda gönderilmemiş SSH giriş farkı var mı (değişmese de gönderilir).
func (s Security) HasAuthDelta() bool {
	return s.Auth != nil && (s.Auth.Failed > 0 || s.Auth.Accepted > 0)
}

// Security ajanın güvenlik raporu. Bölüm toplanamazsa nil kalır ve nedeni
// Notes'a yazılır; panel o bölümün önceki durumunu korur.
type Security struct {
	Time     int64             `json:"time"` // ajan saati (unix ms)
	Updates  *SecUpdates       `json:"updates,omitempty"`
	Reboot   *SecReboot        `json:"reboot,omitempty"`
	Auth     *SecAuth          `json:"auth,omitempty"`     // 732: fark, bkz. SecAuth
	Sessions []SecSession      `json:"sessions"`           // 732: null toplanamadı, [] oturum yok
	Firewall *SecFirewall      `json:"firewall,omitempty"` // 732
	Notes    map[string]string `json:"notes,omitempty"`
}

// Sanitize alanları sınırlar ve sıralar; panel güvenilmeyen girdide, ajan da
// özetten önce çağırır.
func (s *Security) Sanitize() {
	if u := s.Updates; u != nil {
		u.Manager = invClip(u.Manager)
		if len(u.Packages) > MaxSecurityPkgs {
			u.Packages, u.Truncated = u.Packages[:MaxSecurityPkgs], true
		}
		for i := range u.Packages {
			p := &u.Packages[i]
			p.Name, p.Current, p.New, p.Severity = invClip(p.Name), invClip(p.Current), invClip(p.New), invClip(p.Severity)
		}
		u.Packages = slices.DeleteFunc(u.Packages, func(p SecPkg) bool { return p.Name == "" })
		slices.SortFunc(u.Packages, func(a, b SecPkg) int { return strings.Compare(a.Name, b.Name) })
		u.Packages = slices.CompactFunc(u.Packages, func(a, b SecPkg) bool { return a.Name == b.Name })
		sec := 0
		for _, p := range u.Packages {
			if p.Security {
				sec++
			}
		}
		u.Total = max(u.Total, len(u.Packages))
		switch {
		case u.Security < 0:
			u.Security = -1
		case !u.Truncated:
			u.Security = sec
		default:
			u.Security = max(u.Security, sec)
		}
		u.Security = min(u.Security, u.Total)
		u.IndexTime, u.CheckedAt = max(u.IndexTime, 0), max(u.CheckedAt, 0)
		if u.Packages == nil {
			u.Packages = []SecPkg{}
		}
	}
	if r := s.Reboot; r != nil {
		r.Reason = invClip(r.Reason)
		if len(r.Packages) > 100 {
			r.Packages = r.Packages[:100]
		}
		for i := range r.Packages {
			r.Packages[i] = invClip(r.Packages[i])
		}
		r.Packages = slices.DeleteFunc(r.Packages, func(p string) bool { return p == "" })
		slices.Sort(r.Packages)
		r.Packages = slices.Compact(r.Packages)
		if !r.Required {
			r.Reason, r.Packages = "", nil
		}
	}
	s.sanitize732()
	if len(s.Notes) > 20 {
		s.Notes = nil
	}
	for k, v := range s.Notes {
		delete(s.Notes, k)
		if k, v = invClip(k), invClip(v); k != "" {
			s.Notes[k] = v
		}
	}
}

// Hash içeriğin özeti (ajan saati ve sorgu zamanı hariç): ajan yalnız değişince gönderir.
func (s Security) Hash() string {
	s.Time = 0
	if s.Updates != nil {
		u := *s.Updates
		u.CheckedAt = 0
		s.Updates = &u
	}
	b, _ := json.Marshal(s)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// sanitize732 SSH, oturum ve güvenlik duvarı bölümlerini sınırlar.
func (s *Security) sanitize732() {
	if a := s.Auth; a != nil {
		a.Source = invClip(a.Source)
		a.Failed, a.Accepted = max(a.Failed, 0), max(a.Accepted, 0)
		a.Invalid = min(max(a.Invalid, 0), a.Failed)
		a.Hours = slices.DeleteFunc(a.Hours, func(h SecAuthHour) bool {
			return h.Hour <= 0 || h.Failed < 0 || h.Accepted < 0 || (h.Failed == 0 && h.Accepted == 0)
		})
		for i := range a.Hours {
			a.Hours[i].Hour -= a.Hours[i].Hour % 3600
		}
		slices.SortFunc(a.Hours, func(x, y SecAuthHour) int { return int(x.Hour - y.Hour) })
		if len(a.Hours) > MaxSecAuthHours {
			a.Hours = a.Hours[len(a.Hours)-MaxSecAuthHours:]
		}
		a.TopIPs = topCounts(a.TopIPs, MaxSecAuthIPs)
		a.TopUsers = topCounts(a.TopUsers, MaxSecAuthUsers)
	}
	if s.Sessions != nil {
		if len(s.Sessions) > MaxSecSessions {
			s.Sessions = s.Sessions[:MaxSecSessions]
		}
		for i := range s.Sessions {
			x := &s.Sessions[i]
			x.User, x.TTY, x.From, x.Kind, x.State = invClip(x.User), invClip(x.TTY), invClip(x.From), invClip(x.Kind), invClip(x.State)
			x.Since = max(x.Since, 0)
		}
		s.Sessions = slices.DeleteFunc(s.Sessions, func(x SecSession) bool { return x.User == "" })
		slices.SortFunc(s.Sessions, func(a, b SecSession) int {
			if c := strings.Compare(a.User, b.User); c != 0 {
				return c
			}
			return strings.Compare(a.TTY, b.TTY)
		})
	}
	if f := s.Firewall; f != nil {
		f.Backend, f.Policy = invClip(f.Backend), invClip(f.Policy)
		f.Rules = max(f.Rules, 0)
	}
}

// topCounts boş/sıfır kayıtları atar, aynı anahtarları birleştirir, sayıya göre
// azalan (eşitte anahtar) sıralar ve ilk n'yi bırakır.
func topCounts(in []SecCount, n int) []SecCount {
	m := map[string]int{}
	for _, c := range in {
		if k := invClip(c.Key); k != "" && c.Count > 0 {
			m[k] += c.Count
		}
	}
	out := make([]SecCount, 0, len(m))
	for k, v := range m {
		out = append(out, SecCount{Key: k, Count: v})
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
