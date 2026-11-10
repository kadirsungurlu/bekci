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
	SecSectionUpdates = "updates"
	SecSectionReboot  = "reboot"
)

// Güvenlik bölümüne özgü not kodları (genel kodlar: NoteUnsupported,
// NoteNoHost, NoteFailed).
const (
	NoteNoManager    = "no_manager"    // desteklenen paket yöneticisi yok
	NoteCacheMissing = "cache_missing" // dnf/yum önbelleği yok (-C ile sorgu yapılamadı)
	NoteLocked       = "locked"        // paket yöneticisi kilitli (zypper), sonraki turda yeniden
	NoteNoErrata     = "no_errata"     // dağıtım güvenlik bilgisi vermiyor (CentOS 7): Security = -1
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

// Security ajanın güvenlik raporu. Bölüm toplanamazsa nil kalır ve nedeni
// Notes'a yazılır; panel o bölümün önceki durumunu korur.
type Security struct {
	Time    int64             `json:"time"` // ajan saati (unix ms)
	Updates *SecUpdates       `json:"updates,omitempty"`
	Reboot  *SecReboot        `json:"reboot,omitempty"`
	Notes   map[string]string `json:"notes,omitempty"`
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
