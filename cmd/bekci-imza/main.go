// bekci-imza: betik sistemi için çevrimdışı imza ve doğrulama aracı
// (docs/BETIK-GUVENLIK-680.md, docs/BETIK-SOZLESME.md). Operatörün kendi
// makinesinde çalışır, hiçbir alt komut ağa bağlanmaz; özel anahtar panele,
// bu sunucuya ya da CI'ya gitmez (D2).
//
//	bekci-imza anahtar-uret --yuva yayin|onay --cikti <dosya>
//	bekci-imza imzala --anahtar <dosya> --bildirim <manifest.json> --govde <betik.sh> [--cikti dizin]
//	bekci-imza iptal --anahtar <dosya> --id <betik> --alt-surum <n>
//	bekci-imza bilet --anahtar <dosya> --istek <ticket_request.json> --onaylayan <ad> [--sure 10m] [--igneler dosya]
//	bekci-imza dogrula --kayit <dışa aktarım.json> [--ajan-kayit kayit.log]... [--bekle dosya] [--en-eski-kontrol 25h]
//
// Parola yalnız stdin'den okunur (terminalde yankısız); argüman ya da ortam
// değişkeniyle verilmez.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const kullanim = "kullanım: bekci-imza anahtar-uret | imzala | iptal | bilet | dogrula (ayrıntı: scripts/betikler/README.md)"

// app komutların dış dünyası; testler stdin, saat ve shellcheck adını değiştirir.
type app struct {
	in         *bufio.Reader
	out, err   io.Writer
	tty        bool
	now        func() time.Time
	shellcheck string
}

func main() {
	a := &app{in: bufio.NewReader(os.Stdin), out: os.Stdout, err: os.Stderr, tty: isTerminal(os.Stdin),
		now: time.Now, shellcheck: "shellcheck"}
	if err := a.run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "hata:", err)
		os.Exit(1)
	}
}

func (a *app) run(args []string) error {
	if len(args) == 0 {
		return errors.New(kullanim)
	}
	switch args[0] {
	case "anahtar-uret":
		return a.anahtarUret(args[1:])
	case "imzala":
		return a.imzala(args[1:])
	case "iptal":
		return a.iptal(args[1:])
	case "bilet":
		return a.bilet(args[1:])
	case "dogrula":
		return a.dogrula(args[1:])
	}
	return fmt.Errorf("bilinmeyen komut: %s\n%s", args[0], kullanim)
}

func newFlags(name string, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(w)
	return fs
}

// satir stdin'den bir satır okur; son satırda satır sonu olmayabilir.
func (a *app) satir() (string, error) {
	s, err := a.in.ReadString('\n')
	if err != nil && (!errors.Is(err, io.EOF) || s == "") {
		return "", errors.New("stdin'den okunamadı")
	}
	return strings.TrimRight(s, "\r\n"), nil
}

// parola terminalde yankısız, boruda düz satır okur.
func (a *app) parola(prompt string) ([]byte, error) {
	fmt.Fprint(a.err, prompt)
	if a.tty {
		if restore := echoOff(); restore != nil {
			defer func() {
				restore()
				fmt.Fprintln(a.err)
			}()
		} else {
			fmt.Fprintln(a.err, "\nUYARI: yankı kapatılamadı (stty yok), parola ekranda görünecek")
		}
	}
	s, err := a.satir()
	if err != nil {
		return nil, err
	}
	if s == "" {
		return nil, errors.New("parola boş")
	}
	return []byte(s), nil
}

// isTerminal x/term bağımlılığı eklememek için karakter aygıtına bakar;
// /dev/null gibi durumda stty başarısız olur, parola düz satır okunur.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// echoOff yankıyı kapatır, geri açan işlevi döndürür; olmazsa nil. Ctrl-C ile
// çıkışta da terminal yankısız kalmaz.
func echoOff() func() {
	stty := func(arg string) error {
		c := exec.Command("stty", arg)
		c.Stdin = os.Stdin
		return c.Run()
	}
	if stty("-echo") != nil {
		return nil
	}
	sig := make(chan os.Signal, 1)
	done := make(chan struct{})
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sig:
			_ = stty("echo")
			os.Exit(130)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(sig)
		close(done)
		_ = stty("echo")
	}
}
