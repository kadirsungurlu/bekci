// ajanimza: ajan programlarını imzalama aracı (derleme zamanı; imaja girmez).
//
// Sürüm derlemesinde (Dockerfile, build aşaması) her ajan programı için
// imzalı bir bildirim üretir: uptime-<os>-<arch>.sig (JSON: version, os, arch,
// sha256, sig). Panel bu dosyayı /usr/local/share/uptime/agents altından okur,
// kendi açık anahtarıyla doğrular ve ajanlara güncelleme teklifinde iletir;
// ajan aynı imzayı gömülü açık anahtarla doğrular (internal/agentupdate).
//
//	ajanimza anahtar-uret [-cikti anahtar.pem]
//	    Yeni Ed25519 anahtar çifti: özel anahtar PKCS#8 PEM (0600), açık
//	    anahtar base64 standart çıktıya (PublicKeyBase64 / AGENT_SIGNING_PUBKEY).
//	ajanimza imzala -anahtar anahtar.pem -surum 1.3.1 -cikti /out/agents linux/amd64=/out/uptime …
//	    Her os/arch=yol için yol'un SHA-256'sını alır, bildirimi imzalar,
//	    cikti/uptime-<os>-<arch>.sig yazar.
//	ajanimza dogrula -surum 1.3.1 -cikti /out/agents [-acik-anahtar BASE64] linux/amd64=/out/uptime …
//	    .sig dosyalarını gömülü (ya da verilen) açık anahtarla doğrular; biri
//	    bile tutmuyorsa 1 ile çıkar (imaj duman testi).
//
// Özel anahtar yalnızca dosyadan okunur (BuildKit sırrı /run/secrets/…);
// argüman ya da ortam değişkeniyle verilmez, hiçbir yere yazılmaz.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/agentupdate"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("kullanım: ajanimza anahtar-uret | imzala | dogrula (ayrıntı: kaynak dosyadaki açıklama)")
	}
	switch args[0] {
	case "anahtar-uret":
		return keygen(args[1:], out)
	case "imzala":
		return sign(args[1:], out)
	case "dogrula":
		return verify(args[1:], out)
	}
	return fmt.Errorf("bilinmeyen komut: %s", args[0])
}

func keygen(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("anahtar-uret", flag.ContinueOnError)
	path := fs.String("cikti", "ajan-imza.pem", "özel anahtar dosyası (PKCS#8 PEM)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	pemData, err := agentupdate.KeyToPEM(priv)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(*path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("anahtar dosyası oluşturulamadı: %w", err)
	}
	if _, err := f.Write(pemData); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintln(out, base64.StdEncoding.EncodeToString(pub))
	return nil
}

// targets "os/arch=yol" argümanlarını çözer.
func targets(args []string) (map[[2]string]string, error) {
	if len(args) == 0 {
		return nil, errors.New("en az bir os/arch=yol verin (ör. linux/amd64=/out/uptime)")
	}
	out := map[[2]string]string{}
	for _, a := range args {
		plat, path, ok := strings.Cut(a, "=")
		goos, arch, ok2 := strings.Cut(plat, "/")
		if !ok || !ok2 || goos == "" || arch == "" || path == "" {
			return nil, fmt.Errorf("geçersiz hedef %q (os/arch=yol olmalı)", a)
		}
		out[[2]string{goos, arch}] = path
	}
	return out, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func sigPath(dir, goos, arch string) string {
	return filepath.Join(dir, "uptime-"+goos+"-"+arch+".sig")
}

func sign(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("imzala", flag.ContinueOnError)
	keyFile := fs.String("anahtar", "", "özel anahtar (PKCS#8 PEM) dosyası")
	version := fs.String("surum", "", "imzalanan sürüm (main.version ile aynı)")
	dir := fs.String("cikti", "", ".sig dosyalarının yazılacağı klasör")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *keyFile == "" || *version == "" || *dir == "" {
		return errors.New("-anahtar, -surum ve -cikti zorunlu")
	}
	pemData, err := os.ReadFile(*keyFile)
	if err != nil {
		return fmt.Errorf("anahtar okunamadı: %w", err)
	}
	priv, err := agentupdate.KeyFromPEM(pemData)
	if err != nil {
		return err
	}
	tg, err := targets(fs.Args())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		return err
	}
	for plat, path := range tg {
		sum, err := fileSHA256(path)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		m := agentupdate.Manifest{Version: *version, OS: plat[0], Arch: plat[1], SHA256: sum}
		sig, err := agentupdate.Sign(priv, m)
		if err != nil {
			return fmt.Errorf("%s/%s: %w", plat[0], plat[1], err)
		}
		b, _ := json.Marshal(agentupdate.Signed{Manifest: m, Sig: base64.StdEncoding.EncodeToString(sig)})
		sp := sigPath(*dir, plat[0], plat[1])
		if err := os.WriteFile(sp, append(b, '\n'), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "imzalandı: %s/%s %s sha256=%s → %s\n", plat[0], plat[1], *version, sum[:12], sp)
	}
	fmt.Fprintf(out, "açık anahtar: %s\n", base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)))
	return nil
}

func verify(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("dogrula", flag.ContinueOnError)
	version := fs.String("surum", "", "beklenen sürüm")
	dir := fs.String("cikti", "", ".sig dosyalarının klasörü")
	pubB64 := fs.String("acik-anahtar", "", "açık anahtar (base64); boş: programa gömülü anahtar")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *version == "" || *dir == "" {
		return errors.New("-surum ve -cikti zorunlu")
	}
	tg, err := targets(fs.Args())
	if err != nil {
		return err
	}
	pub, err := agentupdate.PublicKey()
	if *pubB64 != "" {
		pub, err = agentupdate.ParsePublicKey(*pubB64)
	}
	if err != nil {
		return err
	}
	var failed int
	for plat, path := range tg {
		sp := sigPath(*dir, plat[0], plat[1])
		err := verifyOne(pub, *version, plat[0], plat[1], path, sp)
		if err != nil {
			failed++
			fmt.Fprintf(out, "HATA %s/%s: %v\n", plat[0], plat[1], err)
			continue
		}
		fmt.Fprintf(out, "geçerli: %s/%s %s (%s)\n", plat[0], plat[1], *version, sp)
	}
	if failed > 0 {
		return fmt.Errorf("%d imza doğrulanamadı", failed)
	}
	return nil
}

func verifyOne(pub ed25519.PublicKey, version, goos, arch, bin, sigFile string) error {
	b, err := os.ReadFile(sigFile)
	if err != nil {
		return err
	}
	var s agentupdate.Signed
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("imza dosyası çözülemedi: %w", err)
	}
	sum, err := fileSHA256(bin)
	if err != nil {
		return err
	}
	switch {
	case s.Version != version:
		return fmt.Errorf("imza %s sürümü için, beklenen %s", s.Version, version)
	case s.OS != goos || s.Arch != arch:
		return fmt.Errorf("imza %s/%s için", s.OS, s.Arch)
	case s.SHA256 != sum:
		return errors.New("programın SHA-256'sı imzadakiyle uyuşmuyor")
	}
	return s.Verify(pub)
}
