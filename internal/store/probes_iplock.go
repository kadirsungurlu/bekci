package store

import "context"

func init() {
	// 15: ajanı ilk bağlandığı IP'ye kilitleme (Beszel'in fingerprint'ine benzer).
	// Açıksa çalınan bir token başka bir IP'den kullanılamaz. locked_ip boşken
	// ilk istek IP'yi sabitler; farklı IP reddedilir. Yeni ajanlarda açık gelir;
	// mevcut kayıtlar (migration varsayılanı 0) değişmez.
	RegisterMigration(15, `
ALTER TABLE probes ADD COLUMN ip_lock INTEGER NOT NULL DEFAULT 0;
ALTER TABLE probes ADD COLUMN locked_ip TEXT NOT NULL DEFAULT '';`)
}

// LockProbeIP ajanın kilitli IP'si boşsa verilen IP'ye sabitler (ilk bağlantı).
// Yalnızca boşken yazar (yarış güvenli); zaten sabitse dokunmaz.
func (s *Store) LockProbeIP(ctx context.Context, id int64, ip string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE probes SET locked_ip = ? WHERE id = ? AND locked_ip = ''", ip, id)
	return err
}

// SetProbeIPLock IP kilidini açar/kapatır. Değişimde kilitli IP sıfırlanır:
// açılınca bir sonraki bağlantı yeni IP'yi sabitler.
func (s *Store) SetProbeIPLock(ctx context.Context, id int64, enabled bool) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE probes SET ip_lock = ?, locked_ip = '' WHERE id = ?", boolInt(enabled), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ResetProbeIP kilitli IP'yi sıfırlar (kilit açık kalır); bir sonraki bağlantı
// yeni IP'yi sabitler. Sunucu taşındığında ya da IP değiştiğinde kullanılır.
func (s *Store) ResetProbeIP(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET locked_ip = '' WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
