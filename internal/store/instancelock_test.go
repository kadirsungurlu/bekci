//go:build unix

package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// PostgreSQL'de de ikinci örnek kilidi bekler (pg_advisory_lock); birinci
// kapanınca devralır. Yalnızca UPTIME_TEST_PG ile çalışır.
func TestInstanceLockPostgres(t *testing.T) {
	if os.Getenv("UPTIME_TEST_PG") == "" {
		t.Skip("UPTIME_TEST_PG yok")
	}
	target := testTarget(t)
	standby := filepath.Join(t.TempDir(), "bekliyor")

	a, err := OpenWith(target, nil, Options{InstanceLock: true})
	if err != nil {
		t.Fatal(err)
	}
	// Kilitsiz açılış (komutlar, testler) engellenmez.
	c, err := OpenWith(target, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	// Süre dolunca hata.
	if _, err := OpenWith(target, nil, Options{InstanceLock: true, LockWait: 1500 * time.Millisecond}); err == nil {
		t.Fatal("kilit tutulurken ikinci örnek açılmamalıydı")
	}

	done := make(chan error, 1)
	var b *Store
	go func() {
		var err error
		b, err = OpenWith(target, nil, Options{InstanceLock: true, LockWait: 20 * time.Second, StandbyFile: standby})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(standby); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bekleme dosyası oluşmadı")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("ikinci örnek kilit bırakılmadan açıldı")
	case <-time.After(1200 * time.Millisecond):
	}
	a.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kilit bırakılınca ikinci örnek açılmadı")
	}
	defer b.Close()
	if _, err := os.Stat(standby); !os.IsNotExist(err) {
		t.Fatal("bekleme dosyası kaldırılmadı")
	}
}

// İkinci örnek, birincisi açıkken kilidi bekler; birinci kapanınca devralır.
func TestInstanceLock(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "uptime.db")
	standby := filepath.Join(dir, "bekliyor")

	a, err := OpenWith(db, nil, Options{InstanceLock: true})
	if err != nil {
		t.Fatal(err)
	}

	// Süre dolunca hata.
	if _, err := OpenWith(db, nil, Options{InstanceLock: true, LockWait: 1500 * time.Millisecond}); err == nil {
		t.Fatal("kilit tutulurken ikinci örnek açılmamalıydı")
	}

	done := make(chan error, 1)
	var b *Store
	go func() {
		var err error
		b, err = OpenWith(db, nil, Options{InstanceLock: true, LockWait: 20 * time.Second, StandbyFile: standby})
		done <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(standby); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bekleme dosyası oluşmadı")
		}
		time.Sleep(50 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("ikinci örnek kilit bırakılmadan açıldı")
	case <-time.After(1200 * time.Millisecond):
	}
	a.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("kilit bırakılınca ikinci örnek açılmadı")
	}
	defer b.Close()
	if _, err := os.Stat(standby); !os.IsNotExist(err) {
		t.Fatal("bekleme dosyası kaldırılmadı")
	}
}
