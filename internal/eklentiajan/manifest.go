// Package eklentiajan özel metrik eklentilerinin ajan tarafıdır (730 §B.1, iş 739):
// imzalı periyodik manifestin katı ayrıştırılması, yerel iğne, systemd kafesi,
// zamanlayıcı ve sonuç kuyruğu. Betik çalıştırma (680) Pro'dadır; bu paket ona
// dayanmaz, aynı zarf biçimini (docs: bekci-pro BETIK-GUVENLIK-680 §5) kendisi
// doğrular. Güven kökü sunucuda root ile yazılan /etc/bekci/eklenti.json'dur;
// panel yalnız iğneli eklentiyi açıp kapatır ve aralığı manifest sınırında seçer.
package eklentiajan

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// signPrefix 680 zarfının bağlamı; tür "manifest". Eski ajanın (Pro betikajan)
// katı ayrıştırıcısı mode/class/interval_* alanlarını bilmediğinden bu
// manifesti reddeder; bu paket de 680 tek seferlik manifestini reddeder.
const (
	signPrefix   = "bekci-script-v1\n"
	kindManifest = "manifest"
)

// Sınırlar (730 §B.1). Ajan bunları manifest ne derse desin zorlar.
const (
	SchemaVersion   = 1
	MaxEnvelope     = 128 << 10
	MaxBody         = 64 << 10
	MinInterval     = 60
	MaxInterval     = 86400
	MaxTimeoutSec   = 60
	MaxManifestLife = 180 * 24 * 3600 // D4
	MaxPlugins      = 20              // sunucu başına (B.3)
)

var (
	idRe     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)
	digestRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	jsonKey  = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

// IsID eklenti kimliği biçimi (birim adına da girer).
func IsID(s string) bool { return idRe.MatchString(s) }

// IsDigest s 64 küçük harf hex mi.
func IsDigest(s string) bool { return digestRe.MatchString(s) }

// Digest hex(sha256(b)).
func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// KeyID açık anahtar kimliği: hex(sha256(ham anahtar)).
func KeyID(pub ed25519.PublicKey) string { return Digest(pub) }

// ParsePublicKey base64 ham 32 bayt açık anahtar.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("açık anahtar geçersiz")
	}
	return ed25519.PublicKey(b), nil
}

func signedBytes(kind string, payload []byte) []byte {
	b := make([]byte, 0, len(signPrefix)+len(kind)+1+len(payload))
	b = append(append(append(b, signPrefix...), kind...), '\n')
	return append(b, payload...)
}

type envelopeWire struct {
	Key     string `json:"key"`
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

// SignManifest manifest payload'ını imzalar, zarfın JSON hâlini döner
// (testler ve örnek araç için; imza anahtarı ajanda yoktur).
func SignManifest(priv ed25519.PrivateKey, payload []byte) []byte {
	pub := priv.Public().(ed25519.PublicKey)
	b, _ := json.Marshal(envelopeWire{
		Key:     KeyID(pub),
		Payload: base64.StdEncoding.EncodeToString(payload),
		Sig:     base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signedBytes(kindManifest, payload))),
	})
	return b
}

// VerifyEnvelope zarfı katı çözer ve anahtarlardan biriyle doğrular; imzalı
// payload baytlarını döner. Ayrıştırma imzadan SONRA yapılır.
func VerifyEnvelope(raw []byte, keys []ed25519.PublicKey) ([]byte, error) {
	if len(raw) == 0 || len(raw) > MaxEnvelope {
		return nil, errors.New("zarf boş ya da sınırın üstünde")
	}
	var w envelopeWire
	if err := DecodeStrict(raw, &w); err != nil {
		return nil, fmt.Errorf("zarf: %w", err)
	}
	p, err1 := base64.StdEncoding.Strict().DecodeString(w.Payload)
	s, err2 := base64.StdEncoding.Strict().DecodeString(w.Sig)
	if !IsDigest(w.Key) || err1 != nil || err2 != nil || len(s) != ed25519.SignatureSize {
		return nil, errors.New("zarf biçimi geçersiz")
	}
	for _, k := range keys {
		if len(k) == ed25519.PublicKeySize && KeyID(k) == w.Key && ed25519.Verify(k, signedBytes(kindManifest, p), s) {
			return p, nil
		}
	}
	return nil, ErrSignature
}

// ErrSignature imza güvenilen hiçbir anahtarla doğrulanmadı (imzasız dahil).
var ErrSignature = errors.New("imza doğrulanamadı")

// Manifest periyodik metrik eklentisi bildirimi. Alanların hepsi zorunludur;
// bilinmeyen alan, yinelenen anahtar ve bilinmeyen schema reddedilir.
type Manifest struct {
	Schema      int      `json:"schema"`
	ID          string   `json:"id"`
	Version     int64    `json:"version"`
	SHA256      string   `json:"sha256"` // gövde özeti
	Interpreter string   `json:"interpreter"`
	Mode        string   `json:"mode"`  // periodic
	Class       string   `json:"class"` // metric
	RunAs       string   `json:"run_as"`
	Platforms   []string `json:"platforms"`
	IntervalMin int      `json:"interval_min"`
	IntervalMax int      `json:"interval_max"`
	TimeoutSec  int      `json:"timeout_sec"`
	Output      string   `json:"output"` // nagios | json
	Network     bool     `json:"network"`
	NotBefore   int64    `json:"not_before"`
	NotAfter    int64    `json:"not_after"`
}

// ParseManifest imzası doğrulanmış payload'ı katı çözer ve denetler.
func ParseManifest(p []byte) (*Manifest, error) {
	var m Manifest
	if err := DecodeStrict(p, &m); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	if m.Schema != SchemaVersion {
		return nil, errors.New("manifest: bilinmeyen şema sürümü")
	}
	if err := m.Validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return &m, nil
}

// Validate alanları ve üst sınırları denetler.
func (m *Manifest) Validate() error {
	linux := false
	for _, p := range m.Platforms {
		linux = linux || p == "linux"
	}
	switch {
	case !IsID(m.ID):
		return errors.New("id geçersiz")
	case m.Version < 1:
		return errors.New("version en az 1")
	case !IsDigest(m.SHA256):
		return errors.New("sha256 geçersiz")
	case m.Interpreter != "sh":
		return errors.New("interpreter yalnız sh")
	case m.Mode != "periodic":
		return errors.New("mode yalnız periodic")
	case m.Class != "metric":
		return errors.New("class yalnız metric")
	case m.RunAs != "nobody" && m.RunAs != "root":
		return errors.New("run_as yalnız nobody ya da root")
	case !linux:
		return errors.New("platforms linux içermeli")
	case m.IntervalMin < MinInterval || m.IntervalMax > MaxInterval || m.IntervalMin > m.IntervalMax:
		return errors.New("interval_min/interval_max sınır dışı")
	case m.TimeoutSec < 1 || m.TimeoutSec > MaxTimeoutSec:
		return errors.New("timeout_sec sınır dışı")
	case m.Output != "nagios" && m.Output != "json":
		return errors.New("output yalnız nagios ya da json")
	case m.NotBefore <= 0 || m.NotAfter <= m.NotBefore:
		return errors.New("not_before/not_after geçersiz")
	case m.NotAfter-m.NotBefore > MaxManifestLife:
		return errors.New("geçerlilik 180 günü aşıyor")
	}
	return nil
}

// DecodeStrict JSON'u katı çözer: yinelenen anahtar, küçük harf/alt çizgi
// dışında anahtar, bilinmeyen alan ve sondaki fazla veri reddedilir.
func DecodeStrict(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := walk(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("JSON sonrası fazla veri")
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	return d.Decode(v)
}

func walk(d *json.Decoder, depth int) error {
	if depth > 8 {
		return errors.New("JSON çok derin")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	if delim == '{' {
		seen := map[string]bool{}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return err
			}
			k, _ := kt.(string)
			if !jsonKey.MatchString(k) || seen[k] {
				return fmt.Errorf("geçersiz ya da yinelenen anahtar %q", k)
			}
			seen[k] = true
			if err := walk(d, depth+1); err != nil {
				return err
			}
		}
	} else {
		for d.More() {
			if err := walk(d, depth+1); err != nil {
				return err
			}
		}
	}
	_, err = d.Token()
	return err
}

// InspectEnvelope zarfı İMZASINI DOĞRULAMADAN çözer ve manifesti denetler.
// Yalnız panel kullanır (güven anahtarları ajandadır): kullanıcıya erken
// hata göstermek ve aralık sınırlarını okumak için. Ajan her çalıştırmadan
// önce VerifyEnvelope + yerel iğne ile ayrıca doğrular.
func InspectEnvelope(raw []byte) (*Manifest, error) {
	if len(raw) == 0 || len(raw) > MaxEnvelope {
		return nil, errors.New("zarf boş ya da sınırın üstünde")
	}
	var w envelopeWire
	if err := DecodeStrict(raw, &w); err != nil {
		return nil, fmt.Errorf("zarf: %w", err)
	}
	p, err1 := base64.StdEncoding.Strict().DecodeString(w.Payload)
	s, err2 := base64.StdEncoding.Strict().DecodeString(w.Sig)
	if !IsDigest(w.Key) || err1 != nil || err2 != nil || len(s) != ed25519.SignatureSize {
		return nil, errors.New("zarf biçimi geçersiz")
	}
	return ParseManifest(p)
}
