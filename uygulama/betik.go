package uygulama

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/betikajan"
)

const betikUsage = `Betik çalıştırma, yerel yönetim (root, yalnız sunucuda; panel bunları yapamaz):
  uptime betik ac --yayin-anahtar <pub> --onay-anahtar <pub> --grup web [--izin id=<sha256>[:min]] [--izin id] [--saat-kotasi 6]
  uptime betik izin <id>[=<sha256>[:min]] ...      izin ekle/değiştir (iğnesiz kayıt daima bilet ister)
  uptime betik izin --sil <id> ...                 izni sil
  uptime betik iptal <zarf dosyası>                imzalı iptali yerelde uygula
  uptime betik anahtar --yayin-ekle <pub> --yayin-sil <pub> --onay-ekle <pub> --onay-sil <pub>
  uptime betik kapat                               özelliği kapat, çalışan betikleri durdur
  uptime betik onar                                bozuk durum ya da yanlış ileri saatten sonra yeniden aç
  uptime betik durum                               yerel durumu göster`

type listFlag []string

func (l *listFlag) String() string     { return strings.Join(*l, ",") }
func (l *listFlag) Set(v string) error { *l = append(*l, v); return nil }

func runBetik(args []string) error {
	if len(args) == 0 {
		fmt.Println(betikUsage)
		return nil
	}
	p := betikajan.DefaultPaths()
	plat := betikajan.DetectPlatform()
	cmd, rest := args[0], args[1:]
	if cmd == "durum" {
		printBetikStatus(betikajan.Status(p, plat), plat)
		return nil
	}
	if os.Geteuid() != 0 {
		return errors.New("bu komut root ister")
	}
	switch cmd {
	case "ac":
		if !plat.OK {
			return fmt.Errorf("bu ortamda betik desteklenmiyor: %s", plat.Detail)
		}
		return betikEnable(p, rest)
	case "izin":
		return betikAllow(p, rest)
	case "iptal":
		if len(rest) != 1 {
			return errors.New("kullanım: uptime betik iptal <zarf dosyası>")
		}
		raw, err := os.ReadFile(rest[0])
		if err != nil {
			return err
		}
		if err := betikajan.RevokeFile(p, []byte(strings.TrimSpace(string(raw)))); err != nil {
			return err
		}
		fmt.Println("iptal uygulandı")
		return nil
	case "anahtar":
		return betikKeys(p, rest)
	case "kapat":
		return betikDisable(p, plat)
	case "onar":
		rebuilt, err := betikajan.Repair(p, time.Now())
		if err != nil {
			return err
		}
		if rebuilt {
			fmt.Println("durum yeniden kuruldu: sürüm tabanı, iptaller ve sayaçlar sıfırlandı, makbuz zinciri kayit.log'dan sürüyor")
		} else {
			fmt.Println("durum sağlam: last_seen şimdiye alındı")
		}
		return nil
	}
	return errors.New(betikUsage)
}

func betikEnable(p betikajan.Paths, args []string) error {
	fs := flag.NewFlagSet("betik ac", flag.ContinueOnError)
	var yayin, onay, groups, allow listFlag
	fs.Var(&yayin, "yayin-anahtar", "K_yayın açık anahtarı (base64)")
	fs.Var(&onay, "onay-anahtar", "K_onay açık anahtarı (base64)")
	fs.Var(&groups, "grup", "bu sunucunun grubu")
	fs.Var(&allow, "izin", "id, id=sha256 ya da id=sha256:min")
	quota := fs.Int("saat-kotasi", 0, "saatlik çalıştırma kotası")
	if err := fs.Parse(args); err != nil {
		return err
	}
	o := betikajan.EnableOptions{Yayin: yayin, Onay: onay, Groups: groups, MaxRunsPerHour: *quota}
	for _, s := range allow {
		a, err := betikajan.ParseAllow(s)
		if err != nil {
			return err
		}
		o.Allow = append(o.Allow, a)
	}
	key, err := betikajan.Enable(p, o)
	if err != nil {
		return err
	}
	fmt.Println("betik açıldı; ajan bir sonraki yoklamada bildirir")
	fmt.Println("host_key:", key)
	fmt.Println("Bu değeri panelde gördüğünüzle karşılaştırın.")
	return nil
}

func betikAllow(p betikajan.Paths, args []string) error {
	fs := flag.NewFlagSet("betik izin", flag.ContinueOnError)
	var del listFlag
	fs.Var(&del, "sil", "silinecek izin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(del) > 0 {
		if err := betikajan.AllowDelete(p, del); err != nil {
			return err
		}
	}
	var in []betikajan.Allow
	for _, s := range fs.Args() {
		a, err := betikajan.ParseAllow(s)
		if err != nil {
			return err
		}
		in = append(in, a)
	}
	if len(in) > 0 {
		return betikajan.AllowSet(p, in)
	}
	return nil
}

func betikKeys(p betikajan.Paths, args []string) error {
	fs := flag.NewFlagSet("betik anahtar", flag.ContinueOnError)
	var ay, dy, ao, do listFlag
	fs.Var(&ay, "yayin-ekle", "eklenecek K_yayın")
	fs.Var(&dy, "yayin-sil", "silinecek K_yayın")
	fs.Var(&ao, "onay-ekle", "eklenecek K_onay")
	fs.Var(&do, "onay-sil", "silinecek K_onay")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return betikajan.EditKeys(p, betikajan.SlotEdit{AddYayin: ay, DelYayin: dy, AddOnay: ao, DelOnay: do})
}

func betikDisable(p betikajan.Paths, plat betikajan.Platform) error {
	// Yazılamazsa birimler yine durdurulur ama komut başarı bildirmez: ajan
	// açık kalmıştır, bekleyen iş çalışabilir.
	disableErr := betikajan.Disable(p)
	if disableErr != nil {
		fmt.Fprintln(os.Stderr, "uyarı: yapılandırma yazılamadı:", disableErr)
	}
	stopped := 0
	if plat.OK {
		e := &betikajan.Systemd{}
		units, err := e.Units()
		if err != nil {
			return err
		}
		for _, id := range units {
			if err := e.Stop(id); err == nil {
				stopped++
			}
		}
	}
	if disableErr != nil {
		return fmt.Errorf("betik kapatılamadı, durdurulan birim: %d: %w", stopped, disableErr)
	}
	fmt.Println("betik kapatıldı; durdurulan birim:", stopped)
	return nil
}

func printBetikStatus(r *betikajan.Report, plat betikajan.Platform) {
	state := "açık"
	if r.Reason != "" {
		state = "kapalı (" + r.Reason + ")"
	}
	fmt.Println("durum:", state)
	if !plat.OK {
		fmt.Println("platform:", plat.Detail)
	}
	if r.HostKey != "" {
		fmt.Println("host_key:", r.HostKey)
	}
	if c := r.Config; c != nil {
		fmt.Println("gruplar:", strings.Join(c.Groups, ", "))
		fmt.Println("saatlik kota:", strconv.Itoa(c.MaxRunsPerHour))
		for _, a := range c.Allow {
			pin := "iğnesiz"
			if a.SHA256 != "" {
				pin = a.SHA256
			}
			fmt.Printf("izin: %s  en az %s  %s\n", a.ID, a.MinApproval, pin)
		}
	}
	if s := r.State; s != nil {
		fmt.Println("son makbuz:", s.Seq, " yarım iş:", len(s.Active), " last_seen:", time.Unix(s.LastSeen, 0).Format(time.RFC3339))
	}
}
