// Package betik imzalı betik nesnelerinin (bildirim, bilet, iptal, makbuz)
// biçimi, imzası ve katı ayrıştırmasıdır. Saf koddur: ağ, disk ve veritabanı
// kullanmaz; ajan, panel ve imza aracı aynı paketi kullanır.
// Biçim docs/BETIK-SOZLESME.md, gerekçe docs/BETIK-GUVENLIK-680.md §5.
//
// Kanonik JSON kullanılmaz: imza ham payload baytlarının üstündedir, alıcı
// önce imzayı doğrular, sonra tam o baytları katı ayrıştırır.
package betik

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

// signPrefix imza bağlamı; ajan güncelleme imzasından (bekci-agent-manifest-v1)
// ayrıdır, türler de birbirinin yerine geçemez.
const signPrefix = "bekci-script-v1\n"

// MaxEnvelope zarfın kodlanmış hâlinin üst sınırı.
const MaxEnvelope = 128 << 10

// Envelope imzalı zarf. Payload ve Sig base64 çözülmüş baytlardır.
type Envelope struct {
	Key     string
	Payload []byte
	Sig     []byte
}

type envelopeWire struct {
	Key     string `json:"key"`
	Payload string `json:"payload"`
	Sig     string `json:"sig"`
}

var keyIDRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsDigest s 64 küçük harf hex mi.
func IsDigest(s string) bool { return keyIDRe.MatchString(s) }

// KeyID açık anahtarın kimliği: hex(sha256(ham anahtar)). host_key de budur.
func KeyID(pub ed25519.PublicKey) string { return Digest(pub) }

// Digest hex(sha256(b)).
func Digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ParsePublicKey base64 ham 32 bayt açık anahtarı çözer.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("açık anahtar base64 değil: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("açık anahtar %d bayt, %d olmalı", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// EncodePublicKey açık anahtarın metin biçimi.
func EncodePublicKey(pub ed25519.PublicKey) string {
	return base64.StdEncoding.EncodeToString(pub)
}

func signedBytes(kind string, payload []byte) []byte {
	b := make([]byte, 0, len(signPrefix)+len(kind)+1+len(payload))
	b = append(b, signPrefix...)
	b = append(b, kind...)
	b = append(b, '\n')
	return append(b, payload...)
}

// Sign payload'ı verilen türde imzalar, zarfın JSON hâlini döndürür.
func Sign(priv ed25519.PrivateKey, kind string, payload []byte) ([]byte, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok || len(priv) != ed25519.PrivateKeySize {
		return nil, errors.New("geçersiz özel anahtar")
	}
	return json.Marshal(envelopeWire{
		Key:     KeyID(pub),
		Payload: base64.StdEncoding.EncodeToString(payload),
		Sig:     base64.StdEncoding.EncodeToString(ed25519.Sign(priv, signedBytes(kind, payload))),
	})
}

// ParseEnvelope zarfı katı çözer. İmza denetlenmez.
func ParseEnvelope(raw []byte) (*Envelope, error) {
	if len(raw) == 0 || len(raw) > MaxEnvelope {
		return nil, errors.New("zarf boş ya da sınırın üstünde")
	}
	var w envelopeWire
	if err := decodeStrict(raw, &w); err != nil {
		return nil, err
	}
	if !IsDigest(w.Key) {
		return nil, errors.New("zarf anahtar kimliği geçersiz")
	}
	p, err := base64.StdEncoding.Strict().DecodeString(w.Payload)
	if err != nil {
		return nil, errors.New("zarf payload base64 değil")
	}
	s, err := base64.StdEncoding.Strict().DecodeString(w.Sig)
	if err != nil || len(s) != ed25519.SignatureSize {
		return nil, errors.New("zarf imzası geçersiz")
	}
	return &Envelope{Key: w.Key, Payload: p, Sig: s}, nil
}

// Verify imzayı verilen tür ve anahtarla doğrular.
func (e *Envelope) Verify(kind string, pub ed25519.PublicKey) bool {
	if len(pub) != ed25519.PublicKeySize || e.Key != KeyID(pub) {
		return false
	}
	return ed25519.Verify(pub, signedBytes(kind, e.Payload), e.Sig)
}

// VerifyAny zarfı anahtar kümesinden biriyle doğrular.
func (e *Envelope) VerifyAny(kind string, keys []ed25519.PublicKey) bool {
	for _, k := range keys {
		if e.Verify(kind, k) {
			return true
		}
	}
	return false
}

// PayloadDigest payload baytlarının özeti (manifest_sha256, ticket_sha256).
func (e *Envelope) PayloadDigest() string { return Digest(e.Payload) }

var jsonKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// decodeStrict JSON'u katı çözer: yinelenen anahtar, küçük harf ve alt çizgi
// dışında anahtar (encoding/json büyük-küçük harf ayırmaz, "Approval" da
// eşleşirdi), bilinmeyen alan ve sondaki fazla veri reddedilir.
func decodeStrict(data []byte, v any) error {
	if err := checkKeys(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("JSON sonrası fazla veri")
	}
	return nil
}

// checkKeys belgedeki tüm nesne anahtarlarını yürür.
func checkKeys(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := walkValue(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); !errors.Is(err, io.EOF) {
		return errors.New("JSON sonrası fazla veri")
	}
	return nil
}

const maxJSONDepth = 16

func walkValue(d *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
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
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			kt, err := d.Token()
			if err != nil {
				return err
			}
			k, _ := kt.(string)
			if !jsonKeyRe.MatchString(k) {
				return fmt.Errorf("geçersiz JSON anahtarı %q", k)
			}
			if seen[k] {
				return fmt.Errorf("yinelenen JSON anahtarı %q", k)
			}
			seen[k] = true
			if err := walkValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := walkValue(d, depth+1); err != nil {
				return err
			}
		}
	}
	_, err = d.Token() // kapanış
	return err
}
