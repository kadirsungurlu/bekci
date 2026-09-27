//go:build windows

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// Windows hizmeti. "uptime probe" hizmet yöneticisi (SCM) tarafından
// başlatıldıysa hizmet olarak çalışır; konsoldan çalıştırılınca eskisi gibi
// ortam değişkenleriyle ön planda çalışır.
//
//	uptime service install    (Yönetici) programı %ProgramFiles%\Uptime\uptime.exe'ye
//	                          kopyalar, ayarları yazar, hizmeti kurar/günceller ve başlatır
//	uptime service uninstall  (Yönetici) hizmeti durdurup siler, ayar dosyasını (token) siler
//
// Ayarlar %ProgramData%\Uptime\agent.env dosyasındadır (KEY=DEĞER). Klasör ve
// dosya yalnızca SYSTEM ve Administrators'a açıktır: token başka kullanıcılarca
// okunamaz. (Hizmetin kayıt defterindeki Environment değeri yerine dosya
// seçildi: Services anahtarları varsayılan olarak Users grubuna okunabilir.)
// Günlük: %ProgramData%\Uptime\agent.log (5 MB'ta agent.log.1'e döner).

const (
	serviceName    = "uptime-agent"
	serviceDisplay = "Uptime agent"
	serviceDesc    = "Uptime sunucu ajanı: sunucu metriklerini ve atanan kontrollerin sonuçlarını ana sunucuya gönderir."
	logLimit       = 5 << 20
)

// Yalnızca SYSTEM ve Administrators: sahip Administrators, devralma kapalı.
const (
	adminOnlyDirSDDL  = "O:BAD:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"
	adminOnlyFileSDDL = "O:BAD:P(A;;FA;;;SY)(A;;FA;;;BA)"
)

// dataDir %ProgramData%\Uptime (ayarlar ve günlük).
func dataDir() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0); err == nil && p != "" {
		return filepath.Join(p, "Uptime")
	}
	if p := os.Getenv("ProgramData"); p != "" {
		return filepath.Join(p, "Uptime")
	}
	return `C:\ProgramData\Uptime`
}

// installPath %ProgramFiles%\Uptime\uptime.exe.
func installPath() string {
	base := os.Getenv("ProgramFiles")
	if p, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0); err == nil && p != "" {
		base = p
	}
	if base == "" {
		base = `C:\Program Files`
	}
	return filepath.Join(base, "Uptime", "uptime.exe")
}

// runProbeService hizmet olarak başlatıldıysa hizmeti çalıştırır (true döner);
// değilse hiçbir şey yapmaz.
func runProbeService() (bool, error) {
	isSvc, err := svc.IsWindowsService()
	if err != nil || !isSvc {
		return false, nil
	}
	dir := dataDir()
	var w io.Writer = io.Discard // hizmetin konsolu yok; günlük dosyası açılamazsa yazılmaz
	if f, err := openRotatingFile(filepath.Join(dir, "agent.log"), logLimit); err == nil {
		defer f.Close()
		w = f
	}
	cfgErr := loadAgentEnv(filepath.Join(dir, "agent.env"))
	log := newLogger(w)
	if cfgErr != nil {
		log.Error("ayar dosyası okunamadı", "dosya", filepath.Join(dir, "agent.env"), "hata", cfgErr)
	}
	return true, svc.Run(serviceName, &agentService{log: log})
}

// loadAgentEnv ayar dosyasındaki değerleri sürecin ortamına yazar.
func loadAgentEnv(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	for k, v := range parseEnvFile(string(b)) {
		os.Setenv(k, v)
	}
	return nil
}

type agentService struct{ log *slog.Logger }

// Execute SCM'nin çağırdığı hizmet döngüsü. Durdur/kapat isteğinde bağlam
// iptal edilir ve ajanın bitmesi beklenir. Ajan kendiliğinden hatayla biterse
// (ör. eksik ayar) hizmete özgü çıkış kodu döner; kurtarma ayarı hizmeti
// yeniden başlatır.
func (a *agentService) Execute(_ []string, req <-chan svc.ChangeRequest, st chan<- svc.Status) (bool, uint32) {
	st <- svc.Status{State: svc.StartPending}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- probeMain(ctx, a.log) }()
	st <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	a.log.Info("Windows hizmeti başladı", "hizmet", serviceName, "sürüm", version)
	for {
		select {
		case err := <-done:
			if err != nil {
				a.log.Error("ajan durdu", "hata", err)
				return true, 1
			}
			return false, 0
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				st <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				st <- svc.Status{State: svc.StopPending, WaitHint: 20_000}
				cancel()
				select {
				case <-done:
				case <-time.After(15 * time.Second):
					a.log.Warn("ajan 15 sn içinde durmadı")
				}
				a.log.Info("Windows hizmeti durdu")
				return false, 0
			}
		}
	}
}

// runServiceCommand "uptime service install|uninstall".
func runServiceCommand(args []string) error {
	usage := errors.New("kullanım: uptime service install | uninstall (Yönetici olarak)")
	if len(args) != 1 {
		return usage
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("bu komut yönetici yetkisi ister: PowerShell'i \"Yönetici olarak çalıştır\" ile açın")
	}
	switch args[0] {
	case "install":
		return installService()
	case "uninstall":
		return uninstallService()
	}
	return usage
}

// installService kurar veya günceller (tekrar çalıştırmak zararsızdır):
// ayarları yazar, çalışan hizmeti durdurur, programı yerine kopyalar, hizmeti
// oluşturur/günceller, "hata olursa yeniden başlat" ayarını yapar ve başlatır.
func installService() error {
	dir := dataDir()
	envPath := filepath.Join(dir, "agent.env")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("%s oluşturulamadı: %w", dir, err)
	}
	if err := restrictToAdmins(dir, adminOnlyDirSDDL); err != nil {
		return fmt.Errorf("%s izinleri ayarlanamadı: %w", dir, err)
	}
	cur := map[string]string{}
	if b, err := os.ReadFile(envPath); err == nil {
		cur = parseEnvFile(string(b))
	}
	kv, err := mergeAgentEnv(cur, os.Getenv)
	if err != nil {
		return err
	}
	if err := writeAdminOnlyFile(envPath, []byte(formatEnvFile(kv))); err != nil {
		return fmt.Errorf("ayar dosyası yazılamadı: %w", err)
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("hizmet yöneticisine bağlanılamadı: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	exists := err == nil
	if exists {
		defer s.Close()
		if err := stopService(s); err != nil {
			return err
		}
	}

	target := installPath()
	if err := copySelf(target); err != nil {
		return err
	}
	cfg := mgr.Config{DisplayName: serviceDisplay, Description: serviceDesc, StartType: mgr.StartAutomatic, ErrorControl: mgr.ErrorNormal}
	if exists {
		c, err := s.Config()
		if err != nil {
			return fmt.Errorf("hizmet ayarları okunamadı: %w", err)
		}
		c.BinaryPathName = windows.EscapeArg(target) + " probe"
		c.DisplayName, c.Description, c.StartType, c.DelayedAutoStart = cfg.DisplayName, cfg.Description, cfg.StartType, false
		if err := s.UpdateConfig(c); err != nil {
			return fmt.Errorf("hizmet güncellenemedi: %w", err)
		}
	} else {
		s, err = m.CreateService(serviceName, target, cfg, "probe")
		if err != nil {
			return fmt.Errorf("hizmet oluşturulamadı: %w", err)
		}
		defer s.Close()
	}
	// Çökme veya hatayla çıkışta 10 sn, 30 sn, sonra her seferinde 60 sn sonra
	// yeniden başlat; sayaç bir günde sıfırlanır.
	actions := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 10 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 30 * time.Second},
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	if err := s.SetRecoveryActions(actions, 86400); err != nil {
		return fmt.Errorf("kurtarma ayarı yapılamadı: %w", err)
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("kurtarma ayarı yapılamadı: %w", err)
	}
	if err := s.Start(); err != nil {
		return fmt.Errorf("hizmet başlatılamadı: %w", err)
	}
	if err := waitState(s, svc.Running, 20*time.Second); err != nil {
		return fmt.Errorf("hizmet başlamadı (%s dosyasına bakın): %w", filepath.Join(dir, "agent.log"), err)
	}
	verb := "kuruldu"
	if exists {
		verb = "güncellendi"
	}
	fmt.Printf("Uptime ajanı %s ve çalışıyor (hizmet: %s, sürüm: %s).\n", verb, serviceName, version)
	fmt.Printf("Program: %s\nAyarlar: %s (yalnızca yöneticiler okuyabilir)\nGünlük:  %s\n", target, envPath, filepath.Join(dir, "agent.log"))
	return nil
}

// uninstallService hizmeti durdurup siler; token içeren ayar dosyasını da siler.
func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("hizmet yöneticisine bağlanılamadı: %w", err)
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("%s hizmeti bulunamadı", serviceName)
	}
	defer s.Close()
	if err := stopService(s); err != nil {
		return err
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("hizmet silinemedi: %w", err)
	}
	dir := dataDir()
	if err := os.Remove(filepath.Join(dir, "agent.env")); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "uyarı: ayar dosyası silinemedi:", err)
	}
	fmt.Printf("Uptime ajanı kaldırıldı. İsterseniz %s ve %s klasörlerini silebilirsiniz.\n", filepath.Dir(installPath()), dir)
	return nil
}

// stopService çalışıyorsa durdurur ve durana kadar bekler.
func stopService(s *mgr.Service) error {
	q, err := s.Query()
	if err != nil {
		return fmt.Errorf("hizmet durumu okunamadı: %w", err)
	}
	if q.State == svc.Stopped {
		return nil
	}
	if q.State != svc.StopPending {
		if _, err := s.Control(svc.Stop); err != nil {
			return fmt.Errorf("hizmet durdurulamadı: %w", err)
		}
	}
	if err := waitState(s, svc.Stopped, 30*time.Second); err != nil {
		return fmt.Errorf("hizmet durdurulamadı: %w", err)
	}
	return nil
}

func waitState(s *mgr.Service, want svc.State, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		q, err := s.Query()
		if err != nil {
			return err
		}
		if q.State == want {
			return nil
		}
		if want == svc.Running && q.State == svc.Stopped {
			return errors.New("hizmet başlar başlamaz durdu")
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s içinde beklenen duruma geçmedi (durum %d)", timeout, q.State)
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// copySelf çalışan programı hedefe kopyalar (zaten oradaysa bir şey yapmaz).
// Önce yanına geçici dosya yazılır, sonra yerine taşınır; durdurulan hizmetin
// dosyası kısa süre kilitli kalabildiği için birkaç kez denenir.
func copySelf(target string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("program yolu bulunamadı: %w", err)
	}
	if a, err := filepath.EvalSymlinks(self); err == nil {
		self = a
	}
	if strings.EqualFold(filepath.Clean(self), filepath.Clean(target)) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("%s oluşturulamadı: %w", filepath.Dir(target), err)
	}
	src, err := os.Open(self)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp := target + ".new"
	dst, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("%s yazılamadı: %w", tmp, err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		os.Remove(tmp)
		return fmt.Errorf("%s yazılamadı: %w", tmp, err)
	}
	if err := dst.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(tmp, target)
		if err == nil {
			return nil
		}
		if i == 20 {
			os.Remove(tmp)
			return fmt.Errorf("%s değiştirilemedi (dosya kullanımda olabilir): %w", target, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// restrictToAdmins yola SDDL'deki sahip ve korumalı (devralmayan) DACL'yi uygular.
func restrictToAdmins(path, sddl string) error {
	sd, err := windows.SecurityDescriptorFromString(sddl)
	if err != nil {
		return err
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		owner, nil, dacl, nil)
}

// writeAdminOnlyFile dosyayı geçici adla yazar, yalnızca yöneticilere açar ve
// yerine taşır: token hiçbir an başkalarına okunur bir dosyada durmaz (klasör
// de korumalı olduğu için geçici dosya zaten yalnızca yöneticilere açıktır).
func writeAdminOnlyFile(path string, data []byte) error {
	tmp := path + ".tmp"
	os.Remove(tmp)
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := restrictToAdmins(tmp, adminOnlyFileSDDL); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
