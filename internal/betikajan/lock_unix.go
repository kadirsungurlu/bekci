//go:build !windows

package betikajan

import (
	"io/fs"
	"os"
	"syscall"
)

const noFollow = syscall.O_NOFOLLOW

func ownedBySelf(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == os.Geteuid()
}

// lockFile durum dosyası için özel kilit: yerel komutlar ve ajan aynı dosyaya
// yazar. Dönen işlev kilidi bırakır.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
