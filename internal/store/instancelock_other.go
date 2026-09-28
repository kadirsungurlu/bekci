//go:build !unix

package store

// acquireInstanceLock unix dışı sistemlerde kilit yok (sunucu yalnızca
// Linux'ta konteyner olarak çalıştırılır).
func (s *Store) acquireInstanceLock(string) error { return nil }
