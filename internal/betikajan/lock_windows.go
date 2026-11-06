//go:build windows

package betikajan

import (
	"errors"
	"io/fs"
)

// Windows ajanında betik desteklenmez (D5); yapılandırma güvenilir sayılmaz.
const noFollow = 0

func ownedBySelf(fs.FileInfo) bool { return false }

func lockFile(string) (func(), error) { return nil, errors.New("desteklenmiyor") }
