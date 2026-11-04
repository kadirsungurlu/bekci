package metrics

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// Sunucu envanteri: ajanın yavaş değişen bilgileri (servisler, konteynerler,
// dinleyen portlar, SSL sertifikaları, disk bölümleri). Metrik örneğinden ayrı
// gönderilir: panel iş listesinde inventory_interval verirse (eski panel alanı
// bilmez, 0 sayılır ve ajan hiç toplamaz), ajan değişince ya da en geç saatte
// bir POST /api/probe/inventory ile yollar.
//
// Gizli bilgi toplanmaz: ortam değişkenleri hiç okunmaz, komut satırları
// MaskSecrets'tan geçer (panel de aldığında aynı maskeyi yeniden uygular).

// Envanter sınırları: gövde ve bölüm başına satır.
const (
	MaxInventoryBytes = 2 << 20
	MaxInventoryItems = 500
	MaxInventoryCerts = 200
	maxInvText        = 300
)

// Bölüm adları (Notes anahtarı).
const (
	InvServices   = "services"
	InvContainers = "containers"
	InvPorts      = "ports"
	InvCerts      = "certs"
	InvDisks      = "disks"
)

// Bölümün toplanamama kodları (Notes değeri; metni arayüz çevirir).
const (
	NoteUnsupported = "unsupported" // işletim sistemi desteklenmiyor
	NoteNoSystemd   = "no_systemd"
	NoteNoDocker    = "no_docker"
	NoteNoHost      = "no_host" // host dizinleri bağlanmamış konteyner
	NoteFailed      = "failed"
)

// InvService bir systemd servisi.
type InvService struct {
	Name        string `json:"name"`
	Active      string `json:"active"`  // active | inactive | failed…
	Sub         string `json:"sub"`     // running | exited | dead…
	Enabled     string `json:"enabled"` // enabled | disabled | static… ("" bilinmiyor)
	Description string `json:"description,omitempty"`
}

// InvContainer bir Docker konteyneri. Ortam değişkenleri okunmaz; Command maskelenmiştir.
type InvContainer struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Image        string `json:"image"`
	State        string `json:"state"`
	Health       string `json:"health,omitempty"`
	RestartCount int    `json:"restart_count"`
	Command      string `json:"command,omitempty"`
	StartedAt    string `json:"started_at,omitempty"`
}

// InvPort dinleyen bir port. Cmd maskelenmiş komut satırıdır.
type InvPort struct {
	Proto   string `json:"proto"` // tcp | udp
	Addr    string `json:"addr"`
	Port    int    `json:"port"`
	Process string `json:"process,omitempty"`
	Cmd     string `json:"cmd,omitempty"`
}

// InvCert bir SSL sertifikası: Source "file" ise Location dosya yolu, "port" ise adres:port.
type InvCert struct {
	Source      string   `json:"source"`
	Location    string   `json:"location"`
	Subject     string   `json:"subject"`
	SANs        []string `json:"sans,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	NotBefore   int64    `json:"not_before"`
	NotAfter    int64    `json:"not_after"`
	Fingerprint string   `json:"fingerprint"` // SHA-256, onaltılık
}

// InvDisk bir disk bölümü (doluluk metrik örneğindedir, burada kapasite).
type InvDisk struct {
	Mount  string `json:"mount"`
	Device string `json:"device"`
	FS     string `json:"fs"`
	Total  uint64 `json:"total"`
}

// Inventory ajanın gönderdiği tam envanter. Notes'ta adı geçen bölüm
// toplanamamıştır: panel o bölümün son bilinen hâlini korur.
type Inventory struct {
	Time       int64             `json:"time"` // ajan saati (unix ms)
	Services   []InvService      `json:"services"`
	Containers []InvContainer    `json:"containers"`
	Ports      []InvPort         `json:"ports"`
	Certs      []InvCert         `json:"certs"`
	Disks      []InvDisk         `json:"disks"`
	Notes      map[string]string `json:"notes,omitempty"`
}

// Sanitize alanları sınırlar, komut satırlarını yeniden maskeler ve sıralar;
// panel bunu güvenilmeyen girdide, ajan da özetten önce çağırır.
func (inv *Inventory) Sanitize() {
	inv.Services = capList(inv.Services, MaxInventoryItems)
	inv.Containers = capList(inv.Containers, MaxInventoryItems)
	inv.Ports = capList(inv.Ports, MaxInventoryItems)
	inv.Certs = capList(inv.Certs, MaxInventoryCerts)
	inv.Disks = capList(inv.Disks, MaxInventoryItems)
	for i := range inv.Services {
		s := &inv.Services[i]
		s.Name, s.Active, s.Sub, s.Enabled = invClip(s.Name), invClip(s.Active), invClip(s.Sub), invClip(s.Enabled)
		s.Description = invClip(s.Description)
	}
	for i := range inv.Containers {
		c := &inv.Containers[i]
		c.ID, c.Name, c.State, c.Health, c.StartedAt = invClip(c.ID), invClip(c.Name), invClip(c.State), invClip(c.Health), invClip(c.StartedAt)
		c.Image = invClip(MaskSecrets(c.Image))
		c.Command = invClip(MaskSecrets(c.Command))
	}
	for i := range inv.Ports {
		p := &inv.Ports[i]
		p.Proto, p.Addr, p.Process = invClip(p.Proto), invClip(p.Addr), invClip(p.Process)
		p.Cmd = invClip(MaskSecrets(p.Cmd))
	}
	for i := range inv.Certs {
		c := &inv.Certs[i]
		c.Source, c.Location, c.Subject, c.Issuer, c.Fingerprint = invClip(c.Source), invClip(c.Location), invClip(c.Subject), invClip(c.Issuer), invClip(c.Fingerprint)
		c.SANs = capList(c.SANs, 50)
		for j := range c.SANs {
			c.SANs[j] = invClip(c.SANs[j])
		}
	}
	for i := range inv.Disks {
		d := &inv.Disks[i]
		d.Mount, d.Device, d.FS = invClip(d.Mount), invClip(d.Device), invClip(d.FS)
	}
	slices.SortFunc(inv.Services, func(a, b InvService) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(inv.Containers, func(a, b InvContainer) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(inv.Ports, func(a, b InvPort) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Proto+a.Addr, b.Proto+b.Addr)
	})
	slices.SortFunc(inv.Certs, func(a, b InvCert) int { return strings.Compare(a.Source+a.Location, b.Source+b.Location) })
	slices.SortFunc(inv.Disks, func(a, b InvDisk) int { return strings.Compare(a.Mount, b.Mount) })
}

// Hash içeriğin özeti (zaman hariç): ajan yalnız değişince gönderir.
func (inv Inventory) Hash() string {
	inv.Time = 0
	b, _ := json.Marshal(inv)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func capList[T any](l []T, n int) []T {
	if len(l) > n {
		return l[:n]
	}
	return l
}

// invClip denetim karakterlerini atar ve metni rune sınırında kısaltır.
func invClip(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > maxInvText {
		cut := maxInvText
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut]
	}
	return s
}

const maskValue = "***"

var (
	secretWord = `(?:pass(?:word|wd|phrase)?|pwd|secret|token|api[_-]?key|apikey|access[_-]?key|private[_-]?key|credentials?|auth)`
	quotedOr   = `(?:"[^"]*"|'[^']*'|\S+)`
	// şema://kullanıcı:parola@sunucu
	maskURL = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.-]*://[^\s:/@]*:)[^\s@/]+@`)
	// PAROLA=değer, --password=değer, token:değer
	maskKV = regexp.MustCompile(`(?i)([\w.-]*` + secretWord + `[\w.-]*[=:])` + quotedOr)
	// --password değer, -password değer
	maskFlag = regexp.MustCompile(`(?i)(^|\s)(--?[\w-]*` + secretWord + `[\w-]*\s+)` + quotedOr)
	// Authorization: Bearer xxx, basic xxx
	maskBearer = regexp.MustCompile(`(?i)\b(bearer|basic)\s+\S+`)
	// mysql -pGIZLI (bitişik parola; -p 8080 gibi boşluklu kullanım dokunulmaz)
	maskMySQL     = regexp.MustCompile(`(^|\s)-p\S+`)
	maskMySQLTool = regexp.MustCompile(`(?i)mysql|mariadb`)
	maskAttached  = regexp.MustCompile(`(^|\s)-p[^\s\d-]\S*`)
)

// MaskSecrets komut satırındaki parola, belirteç ve bağlantı adresi
// kimliklerini *** ile değiştirir. Hedef: görüntülenen metinde gizli bilgi
// kalmaması; yanlış pozitif zararsızdır.
func MaskSecrets(s string) string {
	if s == "" {
		return s
	}
	s = maskURL.ReplaceAllString(s, "${1}"+maskValue+"@")
	s = maskBearer.ReplaceAllString(s, "${1} "+maskValue)
	s = maskFlag.ReplaceAllString(s, "${1}${2}"+maskValue)
	s = maskKV.ReplaceAllString(s, "${1}"+maskValue)
	if maskMySQLTool.MatchString(s) {
		s = maskMySQL.ReplaceAllString(s, "${1}-p"+maskValue)
	}
	return maskAttached.ReplaceAllString(s, "${1}-p"+maskValue)
}
