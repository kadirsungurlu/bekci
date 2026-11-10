//go:build unix

package eklentiajan

import (
	"io/fs"
	"syscall"
)

func ownedByRoot(st fs.FileInfo) bool {
	s, ok := st.Sys().(*syscall.Stat_t)
	return ok && s.Uid == 0
}
