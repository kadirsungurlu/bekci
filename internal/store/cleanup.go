package store

import (
	"context"
	"fmt"
	"time"
)

// Parçalı silme ---------------------------------------------------------------------
//
// Eski kayıtlar tek bir büyük DELETE ile silinmez: SQLite'ta tek yazıcı vardır
// ve milyonlarca satırlık bir DELETE sürdükçe kontrol sonuçları dahil tüm
// yazmalar bekler (tek bağlantı). Silme deleteBatchSize'lık parçalarla yapılır;
// her parça kendi kısa işlemi ve süresiyle çalışır, parçalar arasında
// bağlantı bekleyen işlere bırakılır. Aynı biçim (IN (SELECT … LIMIT ?))
// PostgreSQL'de de çalışır.

var (
	// deleteBatchSize bir parçada silinen en fazla satır (testlerde küçültülür).
	deleteBatchSize = 5000
	// incidentBatchSize olay silmede parça boyu: her olay işlem geçmişiyle
	// (en fazla MaxIncidentEvents kayıt) birlikte silinir (ON DELETE CASCADE).
	incidentBatchSize = 200
	// deleteBatchTimeout tek bir parçanın süre sınırı.
	deleteBatchTimeout = 30 * time.Second
	// deleteYield parçalar arasındaki bekleme.
	deleteYield = 20 * time.Millisecond
)

// deleteBatched q'yu (son parametresi LIMIT olan parçalı bir DELETE) silinecek
// satır kalmayana kadar tekrarlar ve toplam silinen satırı döner.
func (s *Store) deleteBatched(ctx context.Context, batch int, q string, args ...any) (int64, error) {
	args = append(args[:len(args):len(args)], batch)
	var total int64
	for {
		bctx, cancel := context.WithTimeout(ctx, deleteBatchTimeout)
		res, err := s.db.ExecContext(bctx, q, args...)
		cancel()
		if err != nil {
			return total, err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, err
		}
		total += n
		if n < int64(batch) {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		case <-time.After(deleteYield):
		}
	}
}

// DeleteBeatsBefore t'den eski ham kontrol kayıtlarını siler.
// TimescaleDB'de t'den tamamen eski parçalar atılır (drop_chunks). Sıkıştırma
// varsa sınır parçaya dokunulmaz: satır satır silmek parçayı açar, büyük
// parçada açma sınırına takılıp her turda hata verirdi. Saklama süresi bu
// yüzden en çok bir parça (1 gün) uzayabilir.
func (s *Store) DeleteBeatsBefore(ctx context.Context, t int64) (int64, error) {
	if s.tsHyper() {
		if s.tsFull() {
			t -= t % tsChunkDay
		}
		var n int64
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM heartbeats WHERE time < ?", t).Scan(&n); err != nil || n == 0 {
			return 0, err
		}
		if s.tsFull() {
			// Atılacak aralık saatlik sürekli özete işlenmiş olsun (normalde
			// yenileme politikası çoktan işlemiştir; işlem dışında çalışmalı).
			// Arka plan yenileme işiyle çakışırsa kısa aralıkla yeniden dene.
			var err error
			for try := range 3 {
				if _, err = s.db.ExecContext(ctx, "CALL "+s.tsExt+".refresh_continuous_aggregate('stats_hourly_ts', NULL, CAST(? AS BIGINT))", t-t%3600); err == nil {
					break
				}
				time.Sleep(time.Duration(try+1) * time.Second)
			}
			if err != nil {
				return 0, fmt.Errorf("saatlik özet yenilenemedi (ham kayıt silinmedi): %w", err)
			}
		}
		if _, err := s.tsDropChunks(ctx, "heartbeats", t); err != nil {
			return 0, fmt.Errorf("drop_chunks: %w", err)
		}
		if s.tsFull() {
			return n, nil
		}
		_, err := s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM heartbeats WHERE id IN
			(SELECT id FROM heartbeats WHERE time < ? LIMIT ?)`, t)
		return n, err
	}
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM heartbeats WHERE id IN
		(SELECT id FROM heartbeats WHERE time < ? LIMIT ?)`, t)
}

// DeleteHourlyBefore t'den eski saatlik özetleri siler.
func (s *Store) DeleteHourlyBefore(ctx context.Context, t int64) (int64, error) {
	table := s.hourlyTable()
	n, err := s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM `+table+` WHERE (monitor_id, bucket) IN
		(SELECT monitor_id, bucket FROM `+table+` WHERE bucket < ? LIMIT ?)`, t)
	if err != nil || !s.tsFull() {
		return n, err
	}
	c, err := s.tsDropChunks(ctx, "stats_hourly_ts", t)
	return n + c, err
}

// DeleteAuditBefore t'den eski işlem kayıtlarını siler.
func (s *Store) DeleteAuditBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM audit_log WHERE id IN
		(SELECT id FROM audit_log WHERE time < ? LIMIT ?)`, t)
}

// DeleteServerStatsBefore res çözünürlüğündeki eski satırları siler.
func (s *Store) DeleteServerStatsBefore(ctx context.Context, res int, before int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM server_stats WHERE (probe_id, res, time) IN
		(SELECT probe_id, res, time FROM server_stats WHERE res = ? AND time < ? LIMIT ?)`, res, before)
}

// DeleteServerAlertEventsBefore bitmiş eski uyarı kayıtlarını siler.
func (s *Store) DeleteServerAlertEventsBefore(ctx context.Context, before int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM server_alert_events WHERE id IN
		(SELECT id FROM server_alert_events WHERE started_at < ? AND ended_at IS NOT NULL LIMIT ?)`, before)
}

// DeleteIncidentCapturesBefore t'den önce çözülmüş olayların istek/yanıt kayıtlarını siler.
func (s *Store) DeleteIncidentCapturesBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM incident_captures WHERE incident_id IN
		(SELECT c.incident_id FROM incident_captures c JOIN incidents i ON i.id = c.incident_id
		 WHERE i.resolved_at IS NOT NULL AND i.resolved_at < ? LIMIT ?)`, t)
}

// IncidentKeepDays çözülmüş olayların (işlem geçmişi ve istek/yanıt kaydıyla
// birlikte) saklama süresi.
const IncidentKeepDays = 365

// DeleteResolvedIncidentsBefore t'den önce çözülmüş olayları siler; işlem
// geçmişi ve istek/yanıt kaydı ON DELETE CASCADE ile gider. Süren olaylara
// dokunulmaz. (started_at koşulu gereksiz görünür ama started_at dizinini
// kullandırır: çözülme başlangıçtan önce olamaz.)
func (s *Store) DeleteResolvedIncidentsBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, incidentBatchSize, `DELETE FROM incidents WHERE id IN
		(SELECT id FROM incidents WHERE started_at < ? AND resolved_at IS NOT NULL AND resolved_at < ? LIMIT ?)`, t, t)
}
