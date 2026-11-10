//go:build !unix

package eklentiajan

import "io/fs"

// Eklenti yalnız Linux'ta çalışır (D5); başka sistemde yapılandırma kabul edilmez.
func ownedByRoot(fs.FileInfo) bool { return false }
