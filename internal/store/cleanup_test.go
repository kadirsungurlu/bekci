package store

import (
	"context"
	"errors"
	"testing"
)

func countRows(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// smallBatches parça boyunu küçültür: silme döngüsü birden çok tur döner.
func smallBatches(t *testing.T) {
	t.Helper()
	oldB, oldI, oldY := deleteBatchSize, incidentBatchSize, deleteYield
	deleteBatchSize, incidentBatchSize, deleteYield = 3, 2, 0
	t.Cleanup(func() { deleteBatchSize, incidentBatchSize, deleteYield = oldB, oldI, oldY })
}

// Bulgu: eski kayıtlar tek büyük DELETE ile silinince (SQLite) tüm yazmalar
// bekliyordu. Silme parça parça yapılır; toplam sayı ve sonuç aynı kalmalı.
func TestBatchedDeletes(t *testing.T) {
	smallBatches(t)
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "site")

	// 10 eski (her biri ayrı saatte → 10 saatlik özet) + 2 yeni kontrol kaydı.
	for i := range 10 {
		ts := int64(1_000_000 + i*3600)
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: m.ID, Time: ts, Status: StatusUp, PingMs: 5}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, ts := range []int64{2_000_000, 2_000_060} {
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: Beat{MonitorID: m.ID, Time: ts, Status: StatusUp, PingMs: 5}}); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.DeleteBeatsBefore(ctx, 1_500_000); err != nil || n != 10 {
		t.Fatalf("kontrol kayıtları: %d %v", n, err)
	}
	if got := countRows(t, s, "heartbeats"); got != 2 {
		t.Fatalf("kalan kontrol kaydı %d, 2 bekleniyordu", got)
	}
	if n, err := s.DeleteHourlyBefore(ctx, 1_500_000); err != nil || n != 10 {
		t.Fatalf("saatlik özetler: %d %v", n, err)
	}
	if got := countRows(t, s, "stats_hourly"); got != 1 {
		t.Fatalf("kalan saatlik özet %d, 1 bekleniyordu", got)
	}

	for i := range 7 {
		if err := s.AddAudit(ctx, AuditEntry{Time: int64(100 + i), Action: "test"}); err != nil {
			t.Fatal(err)
		}
	}
	s.AddAudit(ctx, AuditEntry{Time: 5000, Action: "test"})
	if n, err := s.DeleteAuditBefore(ctx, 1000); err != nil || n != 7 || countRows(t, s, "audit_log") != 1 {
		t.Fatalf("işlem kayıtları: %d %v", n, err)
	}

	p := Probe{Kind: ProbeKindServer, Name: "cp", Active: true, CreatedAt: 1, Hash: "h"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	for i := range 8 {
		if err := s.UpsertServerStats(ctx, p.ID, ServerRes1, int64(60*i), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertServerStats(ctx, p.ID, ServerRes10, int64(60*i), []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := s.DeleteServerStatsBefore(ctx, ServerRes1, 60*6); err != nil || n != 6 {
		t.Fatalf("sunucu metrikleri: %d %v", n, err)
	}
	if got := countRows(t, s, "server_stats"); got != 10 { // 2 (res1) + 8 (res10)
		t.Fatalf("kalan sunucu metriği %d, 10 bekleniyordu", got)
	}
}

// Çözülmüş eski olaylar işlem geçmişi ve istek/yanıt kaydıyla silinir;
// süren (eski de olsa) ve yeni olaylar kalır.
func TestDeleteResolvedIncidents(t *testing.T) {
	smallBatches(t)
	s := openTest(t)
	ctx := context.Background()
	var ids []int64
	for i := range 5 { // her monitörün tek açık olayı olabilir: ayrı monitörler
		m := newMonitor(t, s, "eski")
		id, err := s.StartIncident(ctx, m.ID, int64(1000+i), "500")
		if err != nil {
			t.Fatal(err)
		}
		if err := s.AddIncidentEvents(ctx, id, IncidentEvent{Time: int64(1000 + i), Kind: EventDown, Message: "500"}); err != nil {
			t.Fatal(err)
		}
		if err := s.SaveIncidentCapture(ctx, id, int64(1000+i), "Ana sunucu", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ResolveIncident(ctx, m.ID, int64(2000+i)); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	open := newMonitor(t, s, "suren")
	openID, _ := s.StartIncident(ctx, open.ID, 500, "hâlâ kapalı")
	recent := newMonitor(t, s, "yeni")
	recentID, _ := s.StartIncident(ctx, recent.ID, 9000, "503")
	s.ResolveIncident(ctx, recent.ID, 9500)

	n, err := s.DeleteResolvedIncidentsBefore(ctx, 5000)
	if err != nil || n != 5 {
		t.Fatalf("silinen olay: %d %v", n, err)
	}
	list, _ := s.ListIncidents(ctx, IncidentFilter{})
	if len(list) != 2 || list[0].ID != recentID || list[1].ID != openID {
		t.Fatalf("kalan olaylar: %+v", list)
	}
	if got := countRows(t, s, "incident_events"); got != 0 {
		t.Fatalf("silinen olayların işlem geçmişi kalmamalı: %d", got)
	}
	if got := countRows(t, s, "incident_captures"); got != 0 {
		t.Fatalf("silinen olayların istek/yanıt kaydı kalmamalı: %d", got)
	}
}

// Bulgu: işlem fonksiyonu panik yaparsa işlem açık kalıyor, SQLite'ın tek
// bağlantısı kilitleniyordu.
func TestTxRollbackOnPanic(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	func() {
		defer func() {
			if p := recover(); p != "test paniği" {
				t.Fatalf("panik yeniden fırlatılmalı: %v", p)
			}
		}()
		s.tx(ctx, func(tx *Tx) error {
			if _, err := tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('panik', 'x')"); err != nil {
				t.Fatal(err)
			}
			panic("test paniği")
		})
	}()
	// Geri alındı ve bağlantı serbest: sonraki yazma beklemeden çalışır.
	if _, ok, err := s.GetSetting(ctx, "panik"); err != nil || ok {
		t.Fatalf("panikteki yazma geri alınmalı: %v %v", ok, err)
	}
	if err := s.tx(ctx, func(tx *Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO settings (key, value) VALUES ('sonra', 'y')")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.tx(ctx, func(*Tx) error { return errors.New("hata") }); err == nil {
		t.Fatal("hata dönmeli")
	}
}
