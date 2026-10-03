package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// Şifreli yedek zarfı: dosya JSON kalır (ayırt edilebilir, metin olarak
// taşınır) ama içerik AES-256-GCM ile şifrelidir; anahtar parolanın
// Argon2id ile türetilmiş halidir. Parola olmadan içerik (ve içindeki
// bildirim token'ları, şifre özetleri, 2FA sırları) okunamaz.
const EncryptedFormat = "bekci-encrypted"

// LegacyEncryptedFormat ürünün eski adıyla yazılmış şifreli zarf; okunur.
const LegacyEncryptedFormat = "uptime-kadir-encrypted"

func isEncryptedFormat(f string) bool { return f == EncryptedFormat || f == LegacyEncryptedFormat }

// Argon2id parametreleri (OWASP önerisinin üstünde; 64 MiB bellek, 3 tur).
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
	keyLen       = 32
)

// Envelope şifreli yedek dosyası.
type Envelope struct {
	Format     string `json:"format"`
	Version    int    `json:"version"`
	KDF        string `json:"kdf"` // argon2id
	Time       uint32 `json:"time"`
	Memory     uint32 `json:"memory"` // KiB
	Threads    uint8  `json:"threads"`
	Salt       []byte `json:"salt"`       // 16 bayt (base64)
	Nonce      []byte `json:"nonce"`      // 12 bayt
	Ciphertext []byte `json:"ciphertext"` // AES-256-GCM (etiket dahil)
	// Note kullanıcıya dosyanın ne olduğunu söyler; şifreleme etkilemez.
	Note string `json:"note,omitempty"`
}

// ErrWrongPassword parola yanlış ya da dosya değişmiş.
var ErrWrongPassword = errors.New("yedek şifresi hatalı ya da dosya bozuk")

// MinPasswordLen şifreli yedek parolasının en kısa uzunluğu.
const MinPasswordLen = 8

// Encrypt düz yedek JSON'unu parolayla şifreler.
func Encrypt(plain []byte, password string) ([]byte, error) {
	return encryptAs(plain, password, EncryptedFormat)
}

// encryptAs zarfı verilen biçim adıyla yazar; biçim adı AEAD ek verisidir
// (eski adlı dosyalar kendi adlarıyla açılır).
func encryptAs(plain []byte, password, format string) ([]byte, error) {
	if len(password) < MinPasswordLen {
		return nil, fmt.Errorf("yedek şifresi en az %d karakter olmalı", MinPasswordLen)
	}
	salt := make([]byte, 16)
	nonce := make([]byte, 12)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, keyLen)
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	env := Envelope{Format: format, Version: 1, KDF: "argon2id", Time: argonTime, Memory: argonMemory, Threads: argonThreads,
		Salt: salt, Nonce: nonce, Ciphertext: gcm.Seal(nil, nonce, plain, []byte(format)),
		Note: "Bu dosya şifreli bir Bekci yedeğidir; geri yüklemek için Ayarlar → Yedekle / Geri yükle bölümünde yedek şifresini girin."}
	return json.MarshalIndent(env, "", "  ")
}

// IsEncrypted dosya şifreli zarf mı?
func IsEncrypted(data []byte) bool {
	var probe struct {
		Format string `json:"format"`
	}
	return json.Unmarshal(data, &probe) == nil && isEncryptedFormat(probe.Format)
}

// Decrypt zarfı parolayla açar; parola yanlışsa ErrWrongPassword.
func Decrypt(data []byte, password string) ([]byte, error) {
	var env Envelope
	if err := json.Unmarshal(data, &env); err != nil || !isEncryptedFormat(env.Format) {
		return nil, errors.New("şifreli yedek dosyası okunamadı")
	}
	if env.Version != 1 || env.KDF != "argon2id" || len(env.Salt) < 8 || len(env.Nonce) != 12 {
		return nil, fmt.Errorf("şifreli yedek sürümü (%d) desteklenmiyor", env.Version)
	}
	// Parametreler dosyadan okunur ama bellek sınırlanır (kötü niyetli dosya 8 GB istemesin).
	if env.Memory > 256*1024 || env.Time > 10 || env.Threads == 0 || env.Time == 0 {
		return nil, errors.New("şifreli yedek parametreleri kabul edilebilir aralığın dışında")
	}
	key := argon2.IDKey([]byte(password), env.Salt, env.Time, env.Memory, env.Threads, keyLen)
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	plain, err := gcm.Open(nil, env.Nonce, env.Ciphertext, []byte(env.Format))
	if err != nil {
		return nil, ErrWrongPassword
	}
	return plain, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
