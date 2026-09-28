package store

import (
	"context"
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
func (s *Store) DeleteBeatsBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM heartbeats WHERE id IN
		(SELECT id FROM heartbeats WHERE time < ? LIMIT ?)`, t)
}

// DeleteHourlyBefore t'den eski saatlik özetleri siler.
func (s *Store) DeleteHourlyBefore(ctx context.Context, t int64) (int64, error) {
	return s.deleteBatched(ctx, deleteBatchSize, `DELETE FROM stats_hourly WHERE (monitor_id, bucket) IN
		(SELECT monitor_id, bucket FROM stats_hourly WHERE bucket < ? LIMIT ?)`, t)
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
