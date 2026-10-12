package store

import (
	"context"
	"database/sql"
)

// 35: ajanların imzalı kendini güncellemesi (internal/agentupdate).
//
//   - platform: ajanın bildirdiği "os/arch" (X-Probe-Platform); boş: ajan bu
//     özellikten önceki bir sürüm (kendini güncelleyemez, bir kez yeniden
//     kurulmalı). "linux/amd64;off" gibi bir sonek ajan tarafında güncellemenin
//     kapalı olduğunu söyler (AUTO_UPDATE=0 ya da program dizini salt okunur).
//   - auto_update: ajan başına geçersiz kılma; NULL genel ayarı devralır.
//   - update_requested_at: panelden "Şimdi güncelle" istendi (genel/ajan ayarı
//     kapalı olsa da bir kez teklif edilir); hedef sürüm gelince sıfırlanır.
//   - update_status/target/note/at: ajanın bildirdiği son güncelleme durumu
//     (updating | failed) ve zamanı; sürüm hedefe ulaşınca temizlenir.
func init() {
	RegisterMigration(35, `
ALTER TABLE probes ADD COLUMN platform TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN auto_update INTEGER;
ALTER TABLE probes ADD COLUMN update_requested_at INTEGER NOT NULL DEFAULT 0;
ALTER TABLE probes ADD COLUMN update_status TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN update_target TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN update_note TEXT NOT NULL DEFAULT '';
ALTER TABLE probes ADD COLUMN update_at INTEGER NOT NULL DEFAULT 0;
`)
}

// Ajanın bildirdiği güncelleme durumları (update_status).
const (
	UpdateStatusUpdating = "updating" // teklif kabul edildi, ajan indiriyor / yeniden başlıyor
	UpdateStatusFailed   = "failed"   // doğrulama/indirme başarısız ya da yeni sürüm geri alındı
)

// ProbeUpdate ajanın güncelleme alanları (Probe içinde gömülü).
type ProbeUpdate struct {
	// Platform "os/arch[;off|;readonly]"; boş: eski ajan (platform bildirmez).
	Platform string `json:"platform"`
	// AutoUpdate ajan başına geçersiz kılma; nil: genel ayar.
	AutoUpdate *bool `json:"auto_update"`
	// UpdateRequestedAt panelden istenen tek seferlik güncelleme (unix; 0: yok).
	UpdateRequestedAt int64 `json:"update_requested_at"`
	// Son bildirilen durum.
	UpdateStatus string `json:"update_status"`
	UpdateTarget string `json:"update_target"`
	UpdateNote   string `json:"update_note"`
	UpdateAt     int64  `json:"update_at"`
}

// TouchProbe kontrol noktasının son görülme bilgisini yazar (çağıran
// seyreltir). platform boşsa (eski ajan) mevcut değer korunur: ajan yeni
// sürümle bir kez bildirdikten sonra eski sürüme dönse de bilgi kalır; kayıt
// yeniden kurulumla sıfırlanmaz.
func (s *Store) TouchProbe(ctx context.Context, id, now int64, ip, version, platform string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE probes SET last_seen_at = ?, last_ip = ?, version = ?, platform = CASE WHEN ? = '' THEN platform ELSE ? END WHERE id = ?",
		now, ip, version, platform, platform, id)
	return err
}

// SetProbeAutoUpdate ajan başına otomatik güncelleme ayarı; nil genel ayara döner.
func (s *Store) SetProbeAutoUpdate(ctx context.Context, id int64, on *bool) error {
	var v sql.NullInt64
	if on != nil {
		v = sql.NullInt64{Int64: int64(boolInt(*on)), Valid: true}
	}
	res, err := s.db.ExecContext(ctx, "UPDATE probes SET auto_update = ? WHERE id = ?", v, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RequestProbeUpdate panelden istenen güncellemeyi kaydeder ve önceki
// başarısızlık notunu temizler (teklif yeniden yapılır).
func (s *Store) RequestProbeUpdate(ctx context.Context, id, now int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE probes SET update_requested_at = ?, update_status = '', update_target = '', update_note = '', update_at = 0 WHERE id = ?", now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// SetProbeUpdateState ajanın bildirdiği durumu yazar (status boş: temizle).
// Hedef sürüme ulaşıldığında (status boş) istek de sıfırlanır.
func (s *Store) SetProbeUpdateState(ctx context.Context, id int64, status, target, note string, now int64) error {
	if status == "" {
		_, err := s.db.ExecContext(ctx,
			"UPDATE probes SET update_status = '', update_target = '', update_note = '', update_at = 0, update_requested_at = 0 WHERE id = ?", id)
		return err
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE probes SET update_status = ?, update_target = ?, update_note = ?, update_at = ? WHERE id = ?",
		status, target, note, now, id)
	return err
}
