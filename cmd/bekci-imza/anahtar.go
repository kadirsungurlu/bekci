package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/bits"
	"os"
	"runtime"

	"golang.org/x/crypto/scrypt"

	"github.com/kadirsungurlu/bekci/internal/betik"
)

const (
	keyFormat  = "bekci-imza-anahtar-v1"
	slotYayin  = "yayin"
	slotOnay   = "onay"
	minParola  = 10
	seedSize   = ed25519.SeedSize
	scryptR    = 8
	scryptP    = 1
	scryptNMin = 1 << 14
	scryptNMax = 1 << 21
)

// scryptN yeni anahtarın maliyeti (128 MiB); testler düşürür.
var scryptN = 1 << 17

// keyFile şifreli özel anahtar dosyası. Tohum AES-256-GCM ile, anahtar
// scrypt(parola) ile şifrelidir; biçim, yuva ve açık anahtar ek doğrulanan
// veridir, yuva dosyada değiştirilerek anahtar başka amaçla kullanılamaz.
type keyFile struct {
	Format     string `json:"format"`
	Slot       string `json:"slot"`
	PublicKey  string `json:"public_key"`
	KDF        string `json:"kdf"`
	N          int    `json:"n"`
	R          int    `json:"r"`
	P          int    `json:"p"`
	Salt       []byte `json:"salt"`
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
}

func (k *keyFile) gcm(parola []byte) (cipher.AEAD, error) {
	dk, err := scrypt.Key(parola, k.Salt, k.N, k.R, k.P, 32)
	if err != nil {
		return nil, err
	}
	defer clear(dk)
	b, err := aes.NewCipher(dk)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(b)
}

func (k *keyFile) aad() []byte { return []byte(k.Format + "\n" + k.Slot + "\n" + k.PublicKey) }

func (a *app) anahtarUret(args []string) error {
	fs := newFlags("anahtar-uret", a.err)
	slot := fs.String("yuva", "", "yayin ya da onay")
	path := fs.String("cikti", "", "şifreli anahtar dosyası")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *slot != slotYayin && *slot != slotOnay {
		return errors.New("--yuva yayin ya da onay olmalı")
	}
	if *path == "" {
		return errors.New("--cikti gerekli")
	}
	f, err := os.OpenFile(*path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // var olan anahtarın üstüne yazılmaz
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		f.Close()
		if !ok {
			os.Remove(*path)
		}
	}()
	parola, err := a.parola("Yeni parola: ")
	if err != nil {
		return err
	}
	defer clear(parola)
	if len(parola) < minParola {
		return fmt.Errorf("parola en az %d bayt olmalı", minParola)
	}
	if a.tty {
		tekrar, err := a.parola("Yeni parola (tekrar): ")
		if err != nil {
			return err
		}
		defer clear(tekrar)
		if string(tekrar) != string(parola) {
			return errors.New("parolalar aynı değil")
		}
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	defer clear(priv)
	k := keyFile{Format: keyFormat, Slot: *slot, PublicKey: betik.EncodePublicKey(pub), KDF: "scrypt",
		N: scryptN, R: scryptR, P: scryptP, Salt: make([]byte, 16)}
	if _, err := rand.Read(k.Salt); err != nil {
		return err
	}
	g, err := k.gcm(parola)
	if err != nil {
		return err
	}
	k.Nonce = make([]byte, g.NonceSize())
	if _, err := rand.Read(k.Nonce); err != nil {
		return err
	}
	k.Ciphertext = g.Seal(nil, k.Nonce, priv.Seed(), k.aad())
	b, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	ok = true
	fmt.Fprintf(a.out, "yuva: %s\nacik_anahtar: %s\nanahtar_kimligi: %s\n", *slot, k.PublicKey, betik.KeyID(pub))
	return nil
}

// anahtarOku dosyayı okur ve yuvayı denetler; parola sormadan önce çağrılır.
func (a *app) anahtarOku(path, slot string) (*keyFile, error) {
	if path == "" {
		return nil, errors.New("--anahtar gerekli")
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(a.err, "UYARI: %s başkalarınca okunabilir (%o), 0600 yapın\n", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var k keyFile
	if err := betik.DecodeStrict(b, &k); err != nil {
		return nil, fmt.Errorf("anahtar dosyası: %w", err)
	}
	switch {
	case k.Format != keyFormat, k.KDF != "scrypt":
		return nil, errors.New("anahtar dosyası biçimi bilinmiyor")
	case k.R != scryptR, k.P != scryptP, k.N < scryptNMin, k.N > scryptNMax, bits.OnesCount(uint(k.N)) != 1,
		len(k.Salt) < 16:
		return nil, errors.New("anahtar dosyası KDF değerleri kabul edilmiyor")
	case k.Slot != slot:
		return nil, fmt.Errorf("bu iş %s anahtarı ister, dosya %s yuvasında", slot, k.Slot)
	}
	return &k, nil
}

// ac parolayla tohumu çözer ve açık anahtarla eşleştiğini denetler.
func (k *keyFile) ac(parola []byte) (ed25519.PrivateKey, error) {
	pub, err := betik.ParsePublicKey(k.PublicKey)
	if err != nil {
		return nil, err
	}
	g, err := k.gcm(parola)
	if err != nil {
		return nil, err
	}
	if len(k.Nonce) != g.NonceSize() {
		return nil, errors.New("anahtar dosyası bozuk")
	}
	seed, err := g.Open(nil, k.Nonce, k.Ciphertext, k.aad())
	if err != nil || len(seed) != seedSize {
		return nil, errors.New("parola yanlış ya da anahtar dosyası bozuk")
	}
	defer clear(seed)
	priv := ed25519.NewKeyFromSeed(seed)
	if !pub.Equal(priv.Public()) {
		return nil, errors.New("anahtar dosyası bozuk: açık anahtar tutmuyor")
	}
	return priv, nil
}

// anahtarAc parolayı sorar ve anahtarı çözer.
func (a *app) anahtarAc(k *keyFile) (ed25519.PrivateKey, error) {
	parola, err := a.parola(fmt.Sprintf("Parola (%s anahtarı): ", k.Slot))
	if err != nil {
		return nil, err
	}
	defer clear(parola)
	return k.ac(parola)
}

// imzaliUret payload'ı imzalar, zarfı çözüp aynı anahtarla doğrular.
func imzaliUret(priv ed25519.PrivateKey, kind string, payload []byte) ([]byte, error) {
	env, err := betik.Sign(priv, kind, payload)
	if err != nil {
		return nil, err
	}
	e, err := betik.ParseEnvelope(env)
	if err != nil {
		return nil, err
	}
	pub, _ := priv.Public().(ed25519.PublicKey)
	if !e.Verify(kind, pub) {
		return nil, errors.New("üretilen imza doğrulanamadı")
	}
	return env, nil
}
