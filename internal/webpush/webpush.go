// Package webpush tarayıcı Web Push bildirimlerini yalnızca standart
// kitaplıkla gönderir: VAPID (RFC 8292; ES256 JWT) ve şifreli içerik
// (RFC 8291 + RFC 8188, aes128gcm). Dış bağımlılık yoktur: ECDH P-256,
// HKDF-SHA256 ve AES-128-GCM crypto paketlerindedir.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Subscription tarayıcının PushSubscription.toJSON() çıktısı.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"` // istemcinin P-256 açık anahtarı (base64url, 65 bayt)
		Auth   string `json:"auth"`   // 16 baytlık kimlik doğrulama sırrı (base64url)
	} `json:"keys"`
}

// Keys VAPID anahtar çifti (base64url, dolgusuz): Private 32 bayt skaler,
// Public 65 bayt sıkıştırılmamış nokta (tarayıcıya applicationServerKey olarak verilir).
type Keys struct {
	Private string `json:"private"`
	Public  string `json:"public"`
}

var b64 = base64.RawURLEncoding

// GenerateKeys yeni bir VAPID anahtar çifti üretir.
func GenerateKeys() (Keys, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Keys{}, err
	}
	d := make([]byte, 32)
	priv.D.FillBytes(d)
	pub := elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y) //nolint:staticcheck // sıkıştırılmamış nokta biçimi gerekiyor
	return Keys{Private: b64.EncodeToString(d), Public: b64.EncodeToString(pub)}, nil
}

// parsePrivate base64url skalerden ECDSA anahtarı.
func (k Keys) parsePrivate() (*ecdsa.PrivateKey, error) {
	d, err := b64.DecodeString(k.Private)
	if err != nil || len(d) != 32 {
		return nil, errors.New("VAPID özel anahtarı geçersiz")
	}
	priv := &ecdsa.PrivateKey{PublicKey: ecdsa.PublicKey{Curve: elliptic.P256()}, D: new(big.Int).SetBytes(d)}
	priv.PublicKey.X, priv.PublicKey.Y = priv.Curve.ScalarBaseMult(d)
	pub, err := b64.DecodeString(k.Public)
	if err != nil || !bytes.Equal(pub, elliptic.Marshal(elliptic.P256(), priv.PublicKey.X, priv.PublicKey.Y)) { //nolint:staticcheck
		return nil, errors.New("VAPID açık anahtarı özel anahtarla uyuşmuyor")
	}
	return priv, nil
}

// vapidTTL JWT geçerlilik süresi (en fazla 24 saat olabilir).
const vapidTTL = 12 * time.Hour

// Authorization VAPID başlığını üretir: "vapid t=<JWT>, k=<açık anahtar>".
// aud bitiş noktasının kökü (şema + sunucu), sub iletişim adresi (mailto:…
// ya da https://…).
func (k Keys) Authorization(endpoint, subject string, now time.Time) (string, error) {
	priv, err := k.parsePrivate()
	if err != nil {
		return "", err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errors.New("push bitiş noktası geçersiz")
	}
	hdr, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims, _ := json.Marshal(map[string]any{"aud": u.Scheme + "://" + u.Host, "exp": now.Add(vapidTTL).Unix(), "sub": subject})
	signing := b64.EncodeToString(hdr) + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return "vapid t=" + signing + "." + b64.EncodeToString(sig) + ", k=" + k.Public, nil
}

// recordSize tek kayıtlık içerik için kayıt boyutu (RFC 8291: en az 4096 önerilir).
const recordSize = 4096

// Encrypt içeriği aboneliğin anahtarlarıyla aes128gcm biçiminde şifreler
// (başlık: salt(16) | rs(4) | idlen(1) | sunucu açık anahtarı(65), ardından
// tek kayıt). Her çağrıda rastgele geçici anahtar ve salt kullanılır.
func Encrypt(sub Subscription, plaintext []byte) ([]byte, error) {
	eph, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, err
	}
	return encryptWith(sub, plaintext, eph, salt)
}

// encryptWith Encrypt'in geçici anahtarı ve salt'ı dışarıdan alan biçimi (test vektörleri).
func encryptWith(sub Subscription, plaintext []byte, eph *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > recordSize-17-1 { // 16 bayt etiket + 1 bayt dolgu ayracı
		return nil, fmt.Errorf("push içeriği çok uzun (%d bayt)", len(plaintext))
	}
	uaPub, err := b64.DecodeString(sub.Keys.P256dh)
	if err != nil {
		return nil, errors.New("abonelik p256dh anahtarı geçersiz")
	}
	auth, err := b64.DecodeString(sub.Keys.Auth)
	if err != nil || len(auth) != 16 {
		return nil, errors.New("abonelik auth sırrı geçersiz")
	}
	remote, err := ecdh.P256().NewPublicKey(uaPub)
	if err != nil {
		return nil, errors.New("abonelik p256dh anahtarı geçersiz")
	}
	shared, err := eph.ECDH(remote)
	if err != nil {
		return nil, err
	}
	asPub := eph.PublicKey().Bytes()
	// RFC 8291 §3.3-3.4: IKM = HKDF(auth, ECDH, "WebPush: info" || ua_public || as_public, 32)
	info := append(append([]byte("WebPush: info\x00"), uaPub...), asPub...)
	ikm, err := hkdf.Key(sha256.New, shared, auth, string(info), 32)
	if err != nil {
		return nil, err
	}
	// RFC 8188 §2.2: CEK ve nonce.
	cek, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Key(sha256.New, ikm, salt, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Tek (son) kayıt: içerik + 0x02 ayracı (dolgu yok).
	record := append(append([]byte{}, plaintext...), 0x02)
	out := make([]byte, 0, 86+len(record)+16)
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	return gcm.Seal(out, nonce, record, nil), nil
}

// ErrGone abonelik artık geçerli değil (404/410): kayıt silinmeli.
var ErrGone = errors.New("push aboneliği artık geçerli değil")

// Options gönderim seçenekleri.
type Options struct {
	TTL     int    // saniye; push servisinin bekletme süresi (0: 24 saat)
	Urgency string // very-low | low | normal | high ("" = normal)
	Topic   string // aynı konudaki bekleyen bildirimleri değiştirir (isteğe bağlı, ≤32 karakter)
	Subject string // VAPID sub: mailto:… ya da https://… (boş: mailto:admin@localhost)
}

// Send şifreler ve push servisine gönderir. Yanıt 404/410 ise ErrGone.
func Send(ctx context.Context, client *http.Client, keys Keys, sub Subscription, payload []byte, opt Options) error {
	body, err := Encrypt(sub, payload)
	if err != nil {
		return err
	}
	subject := opt.Subject
	if subject == "" {
		subject = "mailto:admin@localhost"
	}
	auth, err := keys.Authorization(sub.Endpoint, subject, time.Now())
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	ttl := opt.TTL
	if ttl <= 0 {
		ttl = 86400
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("TTL", strconv.Itoa(ttl))
	req.Header.Set("Authorization", auth)
	if opt.Urgency != "" {
		req.Header.Set("Urgency", opt.Urgency)
	}
	if t := strings.TrimSpace(opt.Topic); t != "" {
		if len(t) > 32 {
			t = t[:32]
		}
		req.Header.Set("Topic", t)
	}
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return ErrGone
	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return fmt.Errorf("push servisi HTTP %d", resp.StatusCode)
	}
	return nil
}
