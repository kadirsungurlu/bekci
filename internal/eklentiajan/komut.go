package eklentiajan

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
)

const usage = `kullanım: uptime betik eklenti <komut>
  izin <ad> <sha256>   eklenti gövdesini yerel olarak iğneler (yalnız bu içerik çalışır)
  kapat <ad>           iğneyi kaldırır; eklenti bir sonraki turda çalışmaz
  anahtar <base64>     güvenilen yayın anahtarı ekler (Ed25519 açık anahtar)
  liste                anahtarları, iğneleri ve kayıt zincirinin durumunu gösterir
  anahtar-uret <dosya> imza anahtarı üretir (özel anahtar dosyaya, açık anahtar ekrana)
  imzala <anahtar> <manifest.json> <govde.sh>
                       manifesti imzalar (sha256 gövdeden doldurulur), zarfı ekrana yazar`

// Komut `uptime betik eklenti ...` alt komutu. Yalnız Linux, yalnız root.
func Komut(args []string, out io.Writer) error {
	return komut(DefaultPaths(), args, out, os.Geteuid() == 0)
}

func komut(p Paths, args []string, out io.Writer, root bool) error {
	// İmzalama yayıncının kendi makinesinde çalışır: root ve Linux gerekmez.
	if len(args) > 0 && (args[0] == "anahtar-uret" || args[0] == "imzala") {
		return imzaKomut(args, out)
	}
	if runtime.GOOS != "linux" {
		return errors.New("eklenti yalnız Linux ajanında desteklenir")
	}
	if len(args) == 0 {
		return errors.New(usage)
	}
	if !root && args[0] != "liste" {
		return errors.New("root gerekli")
	}
	cfg, err := LoadConfig(p.Config)
	if errors.Is(err, ErrNoConfig) {
		cfg, err = &Config{}, nil
	}
	if err != nil {
		return err
	}
	k := &DosyaKayit{Dir: p.LogDir}
	switch {
	case args[0] == "izin" && len(args) == 3:
		id, sha := args[1], args[2]
		if !IsID(id) || !IsDigest(sha) {
			return errors.New("ad ya da sha256 geçersiz (sha256: 64 küçük harf hex, gövde özeti)")
		}
		cfg.SetPin(id, sha)
		if err := SaveConfig(p.Config, cfg); err != nil {
			return err
		}
		if err := k.Yaz(Olay{Tur: OlayIzin, ID: id, SHA256: sha}); err != nil {
			return err
		}
		fmt.Fprintf(out, "iğnelendi: %s %s\n", id, sha)
	case args[0] == "kapat" && len(args) == 2:
		if cfg.Pin(args[1]) == "" {
			return fmt.Errorf("%s için iğne yok", args[1])
		}
		cfg.SetPin(args[1], "")
		if err := SaveConfig(p.Config, cfg); err != nil {
			return err
		}
		if err := k.Yaz(Olay{Tur: OlayIgneK, ID: args[1]}); err != nil {
			return err
		}
		fmt.Fprintf(out, "kapatıldı: %s\n", args[1])
	case args[0] == "anahtar" && len(args) == 2:
		key, err := ParsePublicKey(args[1])
		if err != nil {
			return err
		}
		s := args[1]
		for _, t := range cfg.Trust {
			if t == s {
				fmt.Fprintln(out, "anahtar zaten var")
				return nil
			}
		}
		cfg.Trust = append(cfg.Trust, s)
		if err := SaveConfig(p.Config, cfg); err != nil {
			return err
		}
		fmt.Fprintf(out, "anahtar eklendi: %s\n", KeyID(key))
	case args[0] == "liste" && len(args) == 1:
		for _, t := range cfg.Trust {
			if key, err := ParsePublicKey(t); err == nil {
				fmt.Fprintf(out, "anahtar %s\n", KeyID(key))
			}
		}
		for _, pin := range cfg.Pins {
			fmt.Fprintf(out, "izin   %s %s\n", pin.ID, pin.SHA256)
		}
		ok, err := k.Saglam()
		switch {
		case err != nil:
			fmt.Fprintf(out, "kayıt  okunamadı: %v\n", err)
		case ok:
			fmt.Fprintln(out, "kayıt  zincir sağlam")
		default:
			fmt.Fprintln(out, "kayıt  ZİNCİR KIRIK")
		}
	default:
		return errors.New(usage)
	}
	return nil
}
