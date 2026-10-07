// Package agentupdate ajanın (uptime probe) kendini imzalı sürümle
// güncellemesinin ortak parçalarıdır: imza doğrulama, sürüm karşılaştırma,
// programın atomik değişimi ve başarısız güncellemeden geri dönüş.
//
// Güvenlik modeli: güncelleme panele güvenmez. Sürüm derlemelerinde her ajan
// programı projenin Ed25519 anahtarıyla imzalanır (CI sırrı; bkz.
// .github/workflows/imajlar.yml ve cmd/ajanimza). Ajan, panelin sunduğu
// programı yalnızca gömülü AÇIK anahtarla doğrulanan bir imza eşlik ediyorsa,
// SHA-256'sı tutuyorsa ve sürümü kendisinden yeniyse kurar. Ele geçirilen bir
// panel ajanlara imzasız ya da başka anahtarla imzalı program itemez; eski bir
// sürüme de döndüremez. İmza anahtarı olmadan derlenen panel (Coolify test
// paneli, yerel derleme) ajanlara güncelleme sunmaz.
//
// İmzalanan şey programın kendisi değil küçük bir bildirimdir (Manifest:
// sürüm, işletim sistemi, mimari, SHA-256). Program özetle bildirime
// bağlanır; sürüm ve platform bildirimde olduğu için bir platformun imzası
// başka platform için, bir sürümün imzası başka sürüm adıyla kullanılamaz.
package agentupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// PublicKeyBase64 projenin ajan imza anahtarının AÇIK yarısı (ham 32 bayt,
// base64). Özel anahtar yalnızca GitHub Actions sırrında (AGENT_SIGNING_KEY)
// ve bakımcının çevrimdışı yedeğinde bulunur.
//
// Değişken olarak tutulur: kendi çatalınızı kendi anahtarınızla imzalamak
// için derlemede -ldflags "-X …/internal/agentupdate.PublicKeyBase64=…" ile
// değiştirilebilir (Dockerfile: AGENT_SIGNING_PUBKEY). Bu derleme zamanı bir
// seçimdir; çalışan programın güvendiği anahtar sonradan değiştirilemez.
var PublicKeyBase64 = "S7qw7yVdZmxwtYKuumgfUgcnZE694bzlDsvj9Dd0rQQ="

// TestPublicKey YALNIZCA testler için: boş değilse doğrulamada gömülü anahtar
// yerine bu anahtar kullanılır. Üretim kodu hiçbir yerde atamaz; ortam
// değişkeniyle de ayarlanamaz.
var TestPublicKey ed25519.PublicKey

// PublicKey doğrulamada kullanılan açık anahtar.
func PublicKey() (ed25519.PublicKey, error) {
	if TestPublicKey != nil {
		return TestPublicKey, nil
	}
	return ParsePublicKey(PublicKeyBase64)
}

// ParsePublicKey base64 kodlu ham 32 baytlık Ed25519 açık anahtarı çözer.
func ParsePublicKey(b64 string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("açık anahtar base64 değil: %w", err)
	}
	if len(b) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("açık anahtar %d bayt, %d olmalı", len(b), ed25519.PublicKeySize)
	}
	return ed25519.PublicKey(b), nil
}

// Manifest imzalanan bildirim: hangi sürümün, hangi platform için, hangi
// özetle derlendiği.
type Manifest struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	SHA256  string `json:"sha256"` // hex, küçük harf
}

var (
	platformRe = regexp.MustCompile(`^[a-z0-9]{1,16}$`)
	sha256Re   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	versionRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+\-]{0,49}$`)
)

// Validate alanların biçimini denetler (satır sonu, boşluk, tırnak yok;
// kanonik gösterim tek anlamlı kalsın).
func (m Manifest) Validate() error {
	switch {
	case !versionRe.MatchString(m.Version):
		return errors.New("geçersiz sürüm")
	case !platformRe.MatchString(m.OS) || !platformRe.MatchString(m.Arch):
		return errors.New("geçersiz platform")
	case !sha256Re.MatchString(m.SHA256):
		return errors.New("geçersiz SHA-256 (64 küçük harf hex olmalı)")
	}
	return nil
}

// canonicalPrefix kanonik baytların başı: imza başka bir bağlamda (başka bir
// mesaj türü için) yeniden kullanılamasın.
const canonicalPrefix = "bekci-agent-manifest-v1\n"

// Canonical imzalanan baytlar: önek + satır başına bir alan. Alanlar Validate
// ile sınırlı karakter kümesinde olduğu için gösterim tek anlamlıdır.
func (m Manifest) Canonical() []byte {
	return []byte(canonicalPrefix + m.Version + "\n" + m.OS + "\n" + m.Arch + "\n" + m.SHA256 + "\n")
}

// Sign bildirimi özel anahtarla imzalar (CI ve testler).
func Sign(priv ed25519.PrivateKey, m Manifest) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return ed25519.Sign(priv, m.Canonical()), nil
}

// ErrBadSignature imza bildirime ya da anahtara uymuyor.
var ErrBadSignature = errors.New("imza doğrulanamadı")

// Verify imzayı verilen açık anahtarla doğrular.
func Verify(pub ed25519.PublicKey, m Manifest, sig []byte) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		return ErrBadSignature
	}
	if !ed25519.Verify(pub, m.Canonical(), sig) {
		return ErrBadSignature
	}
	return nil
}

// Signed imza dosyasının (uptime-<os>-<arch>.sig, JSON) içeriği ve panelin
// ajana gönderdiği güncelleme teklifinin imza kısmı.
type Signed struct {
	Manifest
	Sig string `json:"sig"` // base64 (standart), 64 bayt
}

// Signature imzayı çözer.
func (s Signed) Signature() ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s.Sig))
	if err != nil {
		return nil, fmt.Errorf("imza base64 değil: %w", err)
	}
	if len(b) != ed25519.SignatureSize {
		return nil, fmt.Errorf("imza %d bayt, %d olmalı", len(b), ed25519.SignatureSize)
	}
	return b, nil
}

// Verify imzayı verilen anahtarla doğrular.
func (s Signed) Verify(pub ed25519.PublicKey) error {
	sig, err := s.Signature()
	if err != nil {
		return err
	}
	return Verify(pub, s.Manifest, sig)
}

// Offer panelin iş listesi yanıtında ajana sunduğu güncelleme. URL panelin
// kendi program uç noktasıdır (göreli yol); ajan yalnızca kendi paneline
// gider, başka adresi kabul etmez.
type Offer struct {
	Signed
	URL string `json:"url"`
}

// Durum raporu (POST /api/probe/update) ----------------------------------------------

// Ajanın panele bildirdiği güncelleme durumları.
const (
	StatusStarted    = "started"     // teklif kabul edildi, indirme başlıyor
	StatusFailed     = "failed"      // indirme/doğrulama/değişim başarısız ya da yeni sürüm geri alındı
	StatusRolledBack = "rolled_back" // yeni sürüm art arda başlatılamadı, eskisine dönüldü
)

// Report ajanın panele gönderdiği durum raporu.
type Report struct {
	Status  string `json:"status"`
	Version string `json:"version"` // hedef sürüm
	Error   string `json:"error,omitempty"`
}
