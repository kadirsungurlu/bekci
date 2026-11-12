//go:build !windows

package denetim

import (
	"os"
	"syscall"
)

// beklenenSahip kimlik dosyası ve klasörünün sahibi (root; test değiştirir).
var beklenenSahip uint32 = 0

// izinDenetle Linux/Unix: klasör root sahibi ve grup/diğerleri yazamaz;
// dosya bağ değil, düz dosya, root sahibi ve grup/diğerlerine kapalı (600/400).
func izinDenetle(dir, path string) error {
	di, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !di.IsDir() {
		return hata(HataCredential, "%s klasör değil", dir)
	}
	if st, ok := di.Sys().(*syscall.Stat_t); ok && st.Uid != beklenenSahip {
		return hata(HataCredential, "%s klasörünün sahibi root değil (uid %d): chown root:root %s", dir, st.Uid, dir)
	}
	if di.Mode().Perm()&0o022 != 0 {
		return hata(HataCredential, "%s klasörüne grup ya da diğerleri yazabiliyor (%04o): chmod 700 %s", dir, di.Mode().Perm(), dir)
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 || !fi.Mode().IsRegular() {
		return hata(HataCredential, "%s düz dosya olmalı (sembolik bağ kabul edilmez)", path)
	}
	if st, ok := fi.Sys().(*syscall.Stat_t); ok && st.Uid != beklenenSahip {
		return hata(HataCredential, "%s sahibi root değil (uid %d): chown root:root %s", path, st.Uid, path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return hata(HataCredential, "%s izinleri %04o; root sahibi ve 600 olmalı: chmod 600 %s", path, fi.Mode().Perm(), path)
	}
	return nil
}
