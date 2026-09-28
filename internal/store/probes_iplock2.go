package store

import "context"

// SwapProbeLockedIP ajanın kilitli IP değerini yalnızca hâlâ old ise new yapar
// (karşılaştır-ve-yaz). Değer arada değiştiyse (eşzamanlı ilk bağlantı ya da
// kilit sıfırlama) false döner; çağıran kaydı yeniden okuyup karşılaştırır.
// Kilit adres ailesi başına tutulduğundan (IPv4 + IPv6 /64, bkz.
// api/probe_guard.go) yalnızca boşken yazmak yetmez: ikinci aile de yarışsız
// eklenmelidir.
func (s *Store) SwapProbeLockedIP(ctx context.Context, id int64, old, new string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE probes SET locked_ip = ? WHERE id = ? AND locked_ip = ?", new, id, old)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
