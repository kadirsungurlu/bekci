package betik

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Üst sınırlar: bildirim ne derse desin ajan bunları zorlar (680 §5, §6.2).
const (
	SchemaVersion      = 1
	MaxManifestLife    = 180 * 24 * 3600 // D4
	MaxTicketLife      = 900             // sn
	MaxBody            = 64 << 10
	MaxTimeoutSec      = 3600
	MaxOutput          = 256 << 10
	MaxJitterSec       = 3600
	MaxPerDay          = 1440
	MaxVersionJump     = 1000
	MaxParams          = 16
	MaxEnumValues      = 64
	MaxRunsPerResponse = 16
	MaxRevokesPerResp  = 64
)

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	groupRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)
	paramRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	nameRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`)
	runIDRe  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	seenType = map[string]bool{"enum": true, "int": true, "bool": true, "name": true}
)

// ErrSchema bilinmeyen şema sürümü.
var ErrSchema = errors.New("bilinmeyen şema sürümü")

// IsRunID run_id biçimi: birim adına yalnız bu biçim girer.
func IsRunID(s string) bool { return runIDRe.MatchString(s) }

// IsScriptID betik kimliği biçimi.
func IsScriptID(s string) bool { return idRe.MatchString(s) }

// IsGroup grup adı biçimi.
func IsGroup(s string) bool { return groupRe.MatchString(s) }

// ValidApproval onay sınıfı geçerli mi.
func ValidApproval(s string) bool { return ApprovalRank(s) >= 0 }

// ApprovalRank onay sınıfının sırası; bilinmeyen -1.
func ApprovalRank(s string) int {
	switch s {
	case ApprovalNone:
		return 0
	case ApprovalPanel:
		return 1
	case ApprovalSigned:
		return 2
	}
	return -1
}

// MaxApproval iki sınıftan yükseği; bilinmeyen değer signed sayılır.
func MaxApproval(a, b string) string {
	ra, rb := ApprovalRank(a), ApprovalRank(b)
	if ra < 0 || rb < 0 {
		return ApprovalSigned
	}
	if ra >= rb {
		return a
	}
	return b
}

// Param bildirimdeki parametre tanımı. Serbest metin türü yoktur.
type Param struct {
	Name   string   `json:"name"`
	Type   string   `json:"type"`
	Values []string `json:"values,omitempty"`
	Min    *int64   `json:"min,omitempty"`
	Max    *int64   `json:"max,omitempty"`
}

// Manifest K_yayın ile imzalanan bildirim.
type Manifest struct {
	Schema         int      `json:"schema"`
	ID             string   `json:"id"`
	Version        int64    `json:"version"`
	SHA256         string   `json:"sha256"`
	Interpreter    string   `json:"interpreter"`
	RunAs          string   `json:"run_as"`
	Platforms      []string `json:"platforms"`
	Approval       string   `json:"approval"`
	Groups         []string `json:"groups"`
	Params         []Param  `json:"params"`
	TimeoutSec     int      `json:"timeout_sec"`
	OutputMax      int      `json:"output_max"`
	MinIntervalSec int64    `json:"min_interval_sec"`
	MaxPerDay      int      `json:"max_per_day"`
	JitterSec      int      `json:"jitter_sec"`
	NotBefore      int64    `json:"not_before"`
	NotAfter       int64    `json:"not_after"`
}

// Validate alanları ve üst sınırları denetler.
func (m *Manifest) Validate() error {
	switch {
	case !idRe.MatchString(m.ID):
		return errors.New("id geçersiz")
	case m.Version < 1:
		return errors.New("version en az 1")
	case !IsDigest(m.SHA256):
		return errors.New("sha256 geçersiz")
	case m.Interpreter != "sh":
		return errors.New("interpreter yalnız sh")
	case m.RunAs != "root" && m.RunAs != "nobody":
		return errors.New("run_as yalnız root ya da nobody")
	case !slices.Contains(m.Platforms, "linux"):
		return errors.New("platforms linux içermeli")
	case !ValidApproval(m.Approval):
		return errors.New("approval geçersiz")
	case len(m.Groups) == 0:
		return errors.New("groups boş olamaz")
	case m.TimeoutSec < 1 || m.TimeoutSec > MaxTimeoutSec:
		return errors.New("timeout_sec sınır dışı")
	case m.OutputMax < 1 || m.OutputMax > MaxOutput:
		return errors.New("output_max sınır dışı")
	case m.JitterSec < 0 || m.JitterSec > MaxJitterSec:
		return errors.New("jitter_sec sınır dışı")
	case m.MaxPerDay < 1 || m.MaxPerDay > MaxPerDay:
		return errors.New("max_per_day sınır dışı")
	case m.MinIntervalSec < 0:
		return errors.New("min_interval_sec sınır dışı")
	case m.NotBefore <= 0 || m.NotAfter <= m.NotBefore:
		return errors.New("not_before/not_after geçersiz")
	case m.NotAfter-m.NotBefore > MaxManifestLife:
		return errors.New("imza ömrü 180 günü aşıyor")
	case len(m.Params) > MaxParams:
		return errors.New("params çok fazla")
	}
	for _, g := range m.Groups {
		if !groupRe.MatchString(g) {
			return errors.New("groups geçersiz")
		}
	}
	seen := map[string]bool{}
	for i := range m.Params {
		p := &m.Params[i]
		if !paramRe.MatchString(p.Name) || seen[p.Name] || !seenType[p.Type] {
			return fmt.Errorf("parametre %q geçersiz", p.Name)
		}
		seen[p.Name] = true
		switch p.Type {
		case "enum":
			if len(p.Values) == 0 || len(p.Values) > MaxEnumValues {
				return fmt.Errorf("parametre %q: values gerekli", p.Name)
			}
			for _, v := range p.Values {
				if !nameRe.MatchString(v) {
					return fmt.Errorf("parametre %q: geçersiz enum değeri", p.Name)
				}
			}
		case "int":
			if p.Min == nil || p.Max == nil || *p.Min > *p.Max {
				return fmt.Errorf("parametre %q: min ve max gerekli", p.Name)
			}
		}
		if p.Type != "enum" && len(p.Values) > 0 || p.Type != "int" && (p.Min != nil || p.Max != nil) {
			return fmt.Errorf("parametre %q: türe uymayan alan", p.Name)
		}
	}
	return nil
}

// Ticket K_onay ile imzalanan tek çalıştırmalık bilet.
type Ticket struct {
	Schema         int    `json:"schema"`
	RunID          string `json:"run_id"`
	HostKey        string `json:"host_key"`
	ScriptID       string `json:"script_id"`
	Version        int64  `json:"version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	ParamsSHA256   string `json:"params_sha256"`
	IssuedAt       int64  `json:"issued_at"`
	ExpiresAt      int64  `json:"expires_at"`
	Approver       string `json:"approver"`
}

// Validate alanları ve bilet ömrünü denetler.
func (t *Ticket) Validate() error {
	switch {
	case !IsRunID(t.RunID), !IsDigest(t.HostKey), !idRe.MatchString(t.ScriptID), t.Version < 1,
		!IsDigest(t.ManifestSHA256), !IsDigest(t.ParamsSHA256):
		return errors.New("bilet alanı geçersiz")
	case t.IssuedAt <= 0 || t.ExpiresAt <= t.IssuedAt || t.ExpiresAt-t.IssuedAt > MaxTicketLife:
		return errors.New("bilet ömrü geçersiz")
	case t.Approver == "" || len(t.Approver) > 128:
		return errors.New("approver geçersiz")
	}
	return nil
}

// Revoke K_yayın ile imzalanan iptal: id'nin below_version altı çalışmaz.
type Revoke struct {
	Schema       int    `json:"schema"`
	ID           string `json:"id"`
	BelowVersion int64  `json:"below_version"`
}

// Validate alanları denetler.
func (r *Revoke) Validate() error {
	if !idRe.MatchString(r.ID) || r.BelowVersion < 1 {
		return errors.New("iptal alanı geçersiz")
	}
	return nil
}

// Receipt K_ajan ile imzalanan makbuz.
type Receipt struct {
	Schema          int    `json:"schema"`
	Seq             int64  `json:"seq"`
	Prev            string `json:"prev"`
	RunID           string `json:"run_id"`
	ScriptID        string `json:"script_id"`
	Version         int64  `json:"version"`
	ManifestSHA256  string `json:"manifest_sha256"`
	ParamsSHA256    string `json:"params_sha256"`
	TicketSHA256    string `json:"ticket_sha256"`
	StartedAt       int64  `json:"started_at"`
	EndedAt         int64  `json:"ended_at"`
	Status          string `json:"status"`
	ExitCode        int    `json:"exit_code"`
	OutputSHA256    string `json:"output_sha256"`
	OutputTruncated bool   `json:"output_truncated"`
}

// Validate alanları denetler.
func (r *Receipt) Validate() error {
	switch {
	case r.Seq < 1, r.Prev != "" && !IsDigest(r.Prev), !IsRunID(r.RunID), !idRe.MatchString(r.ScriptID),
		!IsDigest(r.ManifestSHA256), !IsDigest(r.ParamsSHA256),
		r.TicketSHA256 != "" && !IsDigest(r.TicketSHA256), r.OutputSHA256 != "" && !IsDigest(r.OutputSHA256):
		return errors.New("makbuz alanı geçersiz")
	}
	switch r.Status {
	case StatusOK, StatusFailed, StatusTimeout, StatusCancelled, StatusLost:
		return nil
	}
	return errors.New("makbuz sonucu geçersiz")
}

type validator interface{ Validate() error }

// parseObject önce schema'yı okur: bilinmeyen schema ayrı hata (ErrSchema) verir.
func parseObject(payload []byte, v validator) error {
	if err := checkKeys(payload); err != nil {
		return err
	}
	var head struct {
		Schema json.Number `json:"schema"`
	}
	if err := json.Unmarshal(payload, &head); err != nil {
		return err
	}
	if head.Schema.String() != strconv.Itoa(SchemaVersion) {
		return ErrSchema
	}
	if err := DecodeStrict(payload, v); err != nil {
		return err
	}
	return v.Validate()
}

// ParseManifest imzası doğrulanmış payload'ı katı ayrıştırır.
func ParseManifest(p []byte) (*Manifest, error) {
	var m Manifest
	if err := parseObject(p, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// ParseTicket imzası doğrulanmış payload'ı katı ayrıştırır.
func ParseTicket(p []byte) (*Ticket, error) {
	var t Ticket
	if err := parseObject(p, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// ParseRevoke imzası doğrulanmış payload'ı katı ayrıştırır.
func ParseRevoke(p []byte) (*Revoke, error) {
	var r Revoke
	if err := parseObject(p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ParseReceipt imzası doğrulanmış payload'ı katı ayrıştırır.
func ParseReceipt(p []byte) (*Receipt, error) {
	var r Receipt
	if err := parseObject(p, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ParseParams parametre JSON baytlarını bildirime göre doğrular; ortam
// değişkeni adı -> değer döndürür. Bildirimdeki her parametre zorunludur,
// bildirimde olmayan parametre reddedilir. Değerler yeniden kodlanmaz.
func ParseParams(m *Manifest, raw []byte) (map[string]string, error) {
	if len(raw) == 0 || len(raw) > 4<<10 {
		return nil, errors.New("parametre gövdesi boş ya da büyük")
	}
	if err := checkKeys(raw); err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var in map[string]any
	if err := d.Decode(&in); err != nil {
		return nil, err
	}
	if in == nil || len(in) != len(m.Params) {
		return nil, errors.New("parametre sayısı bildirimle uyuşmuyor")
	}
	out := make(map[string]string, len(in))
	for i := range m.Params {
		p := &m.Params[i]
		v, ok := in[p.Name]
		if !ok {
			return nil, fmt.Errorf("parametre %q eksik", p.Name)
		}
		s, err := paramValue(p, v)
		if err != nil {
			return nil, fmt.Errorf("parametre %q: %w", p.Name, err)
		}
		out["BEKCI_P_"+strings.ToUpper(p.Name)] = s
	}
	return out, nil
}

func paramValue(p *Param, v any) (string, error) {
	switch p.Type {
	case "enum":
		if s, ok := v.(string); ok && slices.Contains(p.Values, s) {
			return s, nil
		}
		return "", errors.New("izinli değer değil")
	case "name":
		if s, ok := v.(string); ok && nameRe.MatchString(s) {
			return s, nil
		}
		return "", errors.New("ad biçimi geçersiz")
	case "bool":
		if b, ok := v.(bool); ok {
			return strconv.FormatBool(b), nil
		}
		return "", errors.New("bool değil")
	case "int":
		n, ok := v.(json.Number)
		if !ok {
			return "", errors.New("tamsayı değil")
		}
		i, err := strconv.ParseInt(n.String(), 10, 64)
		if err != nil || i < *p.Min || i > *p.Max {
			return "", errors.New("aralık dışı")
		}
		return strconv.FormatInt(i, 10), nil
	}
	return "", errors.New("bilinmeyen tür")
}
