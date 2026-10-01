package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"os"
	"time"
)

// pgInstanceLockKey PostgreSQL'de tek-örnek kilidinin (pg_advisory_lock) ilk
// anahtarı ("upt2"); ikinci anahtar şema adının özetidir: aynı sunucuda ayrı
// şemalardaki kurulumlar birbirini engellemez. Migration kilidinden (upt1) ayrıdır.
const pgInstanceLockKey int32 = 0x75707432

// acquirePGInstanceLock PostgreSQL'de süreç boyunca tutulan tek-örnek kilidini
// ayrı (havuza dönmeyen) bir bağlantıda alır — SQLite'taki flock'un karşılığı.
// Aynı veritabanını iki uygulama örneği aynı anda kullanmasın: yuvarlanan
// dağıtımda (Coolify yeni konteyneri eskisi kapanmadan başlatır) iki motor
// aynı kontrolleri yapar, kontrol kayıtlarını çift yazar ve bildirimleri iki
// kez gönderirdi. Kilit oturuma bağlıdır: süreç ölünce sunucu bırakır, bayat
// kilit kalmaz.
//
// Kilit başkasındaysa LockWait boyunca saniyede bir yeniden denenir; bu sürede
// standby dosyası (boş değilse) oluşturulur ki sağlık kontrolü bekleyen örneği
// sağlıklı saysın ve eski örnek durdurulabilsin.
func (s *Store) acquirePGInstanceLock(ctx context.Context, db *sql.DB) error {
	c, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("tek-örnek kilidi için bağlantı alınamadı: %w", err)
	}
	const key2 = "hashtext(COALESCE(current_schema(), ''))"
	try := func() (bool, error) {
		tctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		var got bool
		err := c.QueryRowContext(tctx, "SELECT pg_try_advisory_lock($1, "+key2+")", pgInstanceLockKey).Scan(&got)
		return got, err
	}
	got, err := try()
	if err != nil {
		c.Close()
		return fmt.Errorf("tek-örnek kilidi alınamadı: %w", err)
	}
	if got {
		s.pgLock = c
		return nil
	}
	wait := s.opts.LockWait
	if wait <= 0 {
		wait = 10 * time.Minute
	}
	log := s.opts.Log
	log.Warn("veritabanı başka bir örnekte açık; o kapanana kadar bekleniyor", "veritabanı", "PostgreSQL", "en_fazla", wait)
	if s.opts.StandbyFile != "" {
		if sf, err := os.Create(s.opts.StandbyFile); err == nil {
			sf.Close()
			defer os.Remove(s.opts.StandbyFile)
		}
	}
	start := time.Now()
	lastLog := start
	for time.Since(start) < wait {
		select {
		case <-ctx.Done():
			c.Close()
			return ctx.Err()
		case <-time.After(time.Second):
		}
		got, err := try()
		if err != nil {
			c.Close()
			return fmt.Errorf("tek-örnek kilidi alınamadı: %w", err)
		}
		if got {
			log.Info("veritabanı kilidi alındı", "beklenen", time.Since(start).Round(time.Second))
			s.pgLock = c
			return nil
		}
		if time.Since(lastLog) >= 30*time.Second {
			log.Warn("hâlâ bekleniyor: diğer örnek veritabanını bırakmadı", "gecen", time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
	}
	c.Close()
	return fmt.Errorf("veritabanı %s boyunca başka bir örnekte açık kaldı; aynı PostgreSQL veritabanıyla iki uygulama (replika) çalıştırılamaz", wait)
}

// releasePGLock tek-örnek kilidini bırakır ve bağlantıyı kapatır. Kilit
// bırakılamazsa bağlantı havuza dönmesin diye bozuk işaretlenir (oturum
// kapanınca sunucu kilidi düşürür).
func (s *Store) releasePGLock() {
	c := s.pgLock
	if c == nil {
		return
	}
	s.pgLock = nil
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.ExecContext(ctx, "SELECT pg_advisory_unlock($1, hashtext(COALESCE(current_schema(), '')))", pgInstanceLockKey); err != nil {
		c.Raw(func(any) error { return driver.ErrBadConn })
	}
	c.Close()
}
