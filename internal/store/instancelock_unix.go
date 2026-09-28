//go:build unix

package store

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// acquireInstanceLock SQLite dosyasının yanındaki <db>.lock dosyasına özel
// flock alır: aynı veri klasörünü iki uygulama örneği aynı anda kullanmasın
// (ör. Coolify yeni konteyneri eskisi kapanmadan başlatır; iki örnek aynı
// kontrolleri yapar, bildirimleri çift gönderir). Kilit süreç yaşadıkça tutulur;
// süreç ölünce çekirdek bırakır, bayat kilit kalmaz.
//
// Kilit başkasındaysa wait boyunca beklenir; bu sürede standby dosyası
// (boş değilse) oluşturulur ki sağlık kontrolü bekleyen örneği sağlıklı saysın
// ve eski örnek durdurulabilsin.
func (s *Store) acquireInstanceLock(path string) error {
	f, err := os.OpenFile(path+".lock", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("kilit dosyası açılamadı: %w", err)
	}
	try := func() error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) }
	err = try()
	if err == nil {
		s.lockFile = f
		return nil
	}
	if !errors.Is(err, syscall.EWOULDBLOCK) {
		f.Close()
		return fmt.Errorf("kilit alınamadı: %w", err)
	}
	wait := s.opts.LockWait
	if wait <= 0 {
		wait = 10 * time.Minute
	}
	log := s.opts.Log
	log.Warn("veri klasörü başka bir örnekte açık; o kapanana kadar bekleniyor", "kilit", path+".lock", "en_fazla", wait)
	if s.opts.StandbyFile != "" {
		if sf, err := os.Create(s.opts.StandbyFile); err == nil {
			sf.Close()
			defer os.Remove(s.opts.StandbyFile)
		}
	}
	start := time.Now()
	lastLog := start
	for time.Since(start) < wait {
		time.Sleep(time.Second)
		if err := try(); err == nil {
			log.Info("veri klasörü kilidi alındı", "beklenen", time.Since(start).Round(time.Second))
			s.lockFile = f
			return nil
		}
		if time.Since(lastLog) >= 30*time.Second {
			log.Warn("hâlâ bekleniyor: diğer örnek veri klasörünü bırakmadı", "gecen", time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
	}
	f.Close()
	return fmt.Errorf("veri klasörü %s boyunca başka bir örnekte açık kaldı; aynı veri klasörüyle iki uygulama çalıştırılamaz", wait)
}
