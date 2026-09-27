package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestServerStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := Probe{Name: "CP", Active: true, CreatedAt: 100, Hash: "h1"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetProbe(ctx, p.ID)
	if !got.Metrics || got.MetricsAt != 0 || got.HostInfo != "" || got.MetricsNote != "" {
		t.Fatalf("yeni ajan: %+v", got)
	}
	if err := s.SetProbeMetrics(ctx, p.ID, false); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.GetProbe(ctx, p.ID); got.Metrics {
		t.Fatal("metrik kapatılamadı")
	}
	if err := s.SetProbeMetrics(ctx, 999, true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("olmayan ajan: %v", err)
	}

	// "Toplayamıyorum" notu metrics_at'e dokunmaz; host bilgisini yazar.
	if err := s.SetProbeMetricsNote(ctx, p.ID, "host bağlı değil", `{"hostname":"a"}`); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetProbe(ctx, p.ID)
	if got.MetricsNote != "host bağlı değil" || got.MetricsAt != 0 || got.HostInfo != `{"hostname":"a"}` {
		t.Fatalf("not: %+v", got)
	}

	// İlk örnek: first=true, not temizlenir; host boş gelirse eskisi kalır.
	first, err := s.SaveServerSample(ctx, ServerSample{ProbeID: p.ID, Time: 600, At: 610, Data: []byte(`{"cpu":1}`)})
	if err != nil || !first {
		t.Fatal(first, err)
	}
	got, _ = s.GetProbe(ctx, p.ID)
	if got.MetricsAt != 610 || got.MetricsNote != "" || got.HostInfo != `{"hostname":"a"}` {
		t.Fatalf("örnek sonrası: %+v", got)
	}
	// Aynı dakikada ikinci örnek öncekinin yerine geçer.
	first, err = s.SaveServerSample(ctx, ServerSample{ProbeID: p.ID, Time: 600, At: 650, Data: []byte(`{"cpu":2}`), HostInfo: `{"hostname":"b"}`})
	if err != nil || first {
		t.Fatal(first, err)
	}
	if _, err := s.SaveServerSample(ctx, ServerSample{ProbeID: p.ID, Time: 660, At: 665, Data: []byte(`{"cpu":3}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveServerSample(ctx, ServerSample{ProbeID: 999, Time: 660, At: 665, Data: []byte(`{}`)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("olmayan ajan: %v", err)
	}
	got, _ = s.GetProbe(ctx, p.ID)
	if got.HostInfo != `{"hostname":"b"}` {
		t.Fatalf("host: %q", got.HostInfo)
	}
	rows, err := s.ServerStats(ctx, p.ID, ServerRes1, 0, 10_000)
	if err != nil || len(rows) != 2 || rows[0].Time != 600 || string(rows[0].Data) != `{"cpu":2}` || rows[1].Time != 660 {
		t.Fatalf("satırlar: %+v %v", rows, err)
	}
	if rows, _ := s.ServerStats(ctx, p.ID, ServerRes1, 600, 660); len(rows) != 1 {
		t.Fatalf("aralık sonu hariç olmalı: %+v", rows)
	}
	last, err := s.LastServerStat(ctx, p.ID, 660)
	if err != nil || last.Time != 600 {
		t.Fatal(last, err)
	}
	if _, err := s.LastServerStat(ctx, p.ID, 600); !errors.Is(err, ErrNotFound) {
		t.Fatalf("önceki satır yok: %v", err)
	}
	if err := s.UpsertServerStats(ctx, p.ID, ServerRes10, 600, []byte(`{"cpu":2.5}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertServerStats(ctx, p.ID, ServerRes10, 600, []byte(`{"cpu":2.6}`)); err != nil {
		t.Fatal(err)
	}
	if rows, _ := s.ServerStats(ctx, p.ID, ServerRes10, 0, 10_000); len(rows) != 1 || string(rows[0].Data) != `{"cpu":2.6}` {
		t.Fatalf("özet: %+v", rows)
	}
	// Saklama: yalnızca verilen çözünürlükteki eski satırlar.
	if n, err := s.DeleteServerStatsBefore(ctx, ServerRes1, 660); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if rows, _ := s.ServerStats(ctx, p.ID, ServerRes10, 0, 10_000); len(rows) != 1 {
		t.Fatal("başka çözünürlük silinmemeli")
	}
}

func TestServerAlertsStore(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := Probe{Name: "CP", Active: true, CreatedAt: 100, Hash: "h1"}
	if err := s.CreateProbe(ctx, &p); err != nil {
		t.Fatal(err)
	}
	defaults := []ServerAlert{
		{Metric: "offline", Minutes: 3, Active: true},
		{Metric: "cpu", Threshold: 90, Minutes: 10, Active: true},
	}
	// Varsayılan kanallar: yalnızca is_default olanlar, yalnızca hiç kanal yoksa.
	def := Notification{Name: "Varsayılan", Type: "webhook", Config: json.RawMessage(`{}`), IsDefault: true, Active: true}
	other := Notification{Name: "Diğer", Type: "webhook", Config: json.RawMessage(`{}`), Active: true}
	for _, n := range []*Notification{&def, &other} {
		if err := s.CreateNotification(ctx, n, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InitServerDefaults(ctx, p.ID, defaults); err != nil {
		t.Fatal(err)
	}
	rules, _ := s.ServerAlerts(ctx, p.ID)
	if len(rules) != 2 || rules[0].Metric != "offline" || rules[1].Threshold != 90 || rules[1].Minutes != 10 || !rules[1].Active {
		t.Fatalf("varsayılan kurallar: %+v", rules)
	}
	if ids, _ := s.ProbeNotificationIDs(ctx, p.ID); len(ids) != 1 || ids[0] != def.ID {
		t.Fatalf("varsayılan kanallar: %v", ids)
	}
	// İkinci kez: kural veya kanal varsa dokunulmaz.
	if err := s.SetProbeNotifications(ctx, p.ID, []int64{other.ID}); err != nil {
		t.Fatal(err)
	}
	if err := s.InitServerDefaults(ctx, p.ID, defaults[:1]); err != nil {
		t.Fatal(err)
	}
	if rules, _ := s.ServerAlerts(ctx, p.ID); len(rules) != 2 {
		t.Fatalf("kurallar tekrar eklenmemeli: %+v", rules)
	}
	if ids, _ := s.ProbeNotificationIDs(ctx, p.ID); len(ids) != 1 || ids[0] != other.ID {
		t.Fatalf("kanallar değişmemeli: %v", ids)
	}
	chans, _ := s.NotificationsForProbe(ctx, p.ID)
	if len(chans) != 1 || chans[0].ID != other.ID {
		t.Fatalf("ajan kanalları: %+v", chans)
	}
	names, _ := s.NotificationNames(ctx, []int64{def.ID, 999})
	if len(names) != 1 || names[def.ID] != "Varsayılan" {
		t.Fatalf("adlar: %v", names)
	}

	// Tetikleme bir kez olur; geçmişe kayıt açılır.
	cpu := rules[1]
	cpu.ProbeID = p.ID
	if ok, err := s.FireServerAlert(ctx, cpu, 95.5, "", 1000); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, _ := s.FireServerAlert(ctx, cpu, 97, "", 1060); ok {
		t.Fatal("tetiklenmiş kural tekrar tetiklenmemeli")
	}
	evs, _ := s.ServerAlertEvents(ctx, p.ID, 0, 10)
	if len(evs) != 1 || evs[0].Value != 95.5 || evs[0].Threshold != 90 || evs[0].StartedAt != 1000 || evs[0].EndedAt != 0 || evs[0].AlertID != cpu.ID {
		t.Fatalf("geçmiş: %+v", evs)
	}

	// Değiştirme: aynı metrik yerinde güncellenir, tetiklenmiş durum korunur.
	saved, err := s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{
		{Metric: "cpu", Threshold: 80, Minutes: 5, Active: true},
		{Metric: "mem", Threshold: 70, Minutes: 2, Active: false},
	}, 1100)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].ID != cpu.ID || !saved[0].Firing || saved[0].FiredAt != 1000 || saved[0].Threshold != 80 ||
		saved[1].Metric != "mem" || saved[1].Active {
		t.Fatalf("değiştirme: %+v", saved)
	}
	// Tetiklenmiş kural pasifleşince kapanır, geçmiş kaydı biter.
	saved, _ = s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{{Metric: "cpu", Threshold: 80, Minutes: 5, Active: false}}, 1200)
	if len(saved) != 1 || saved[0].Firing || saved[0].FiredAt != 0 {
		t.Fatalf("pasif: %+v", saved)
	}
	if evs, _ := s.ServerAlertEvents(ctx, p.ID, 0, 10); evs[0].EndedAt != 1200 {
		t.Fatalf("geçmiş kapanmalı: %+v", evs)
	}

	// Tekrar tetiklenip bitirme; bitmemiş kural bitirilemez.
	saved, _ = s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{{Metric: "cpu", Threshold: 80, Minutes: 5, Active: true}}, 1300)
	cpu = saved[0]
	cpu.ProbeID = p.ID
	s.FireServerAlert(ctx, cpu, 85, "", 1400)
	if ok, err := s.ResolveServerAlert(ctx, cpu.ID, 1500); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if ok, _ := s.ResolveServerAlert(ctx, cpu.ID, 1600); ok {
		t.Fatal("bitmiş kural tekrar bitmemeli")
	}
	evs, _ = s.ServerAlertEvents(ctx, p.ID, 0, 10)
	if len(evs) != 2 || evs[0].StartedAt != 1400 || evs[0].EndedAt != 1500 {
		t.Fatalf("yeniden eskiye: %+v", evs)
	}
	if evs, _ := s.ServerAlertEvents(ctx, p.ID, 1300, 10); len(evs) != 1 {
		t.Fatalf("since: %+v", evs)
	}
	// Silinen tetiklenmiş kuralın geçmişi kapanır ama kalır.
	s.FireServerAlert(ctx, cpu, 99, "", 1700)
	if saved, _ := s.ReplaceServerAlerts(ctx, p.ID, nil, 1800); len(saved) != 0 {
		t.Fatalf("hepsi silinmeli: %+v", saved)
	}
	evs, _ = s.ServerAlertEvents(ctx, p.ID, 0, 10)
	if len(evs) != 3 || evs[0].EndedAt != 1800 {
		t.Fatalf("silinen kuralın geçmişi: %+v", evs)
	}
	all, _ := s.AllServerAlerts(ctx)
	if len(all[p.ID]) != 0 {
		t.Fatalf("tüm kurallar: %+v", all)
	}
	// Saklama: yalnızca bitmiş eski kayıtlar silinir.
	s.ReplaceServerAlerts(ctx, p.ID, []ServerAlert{{Metric: "mem", Threshold: 50, Minutes: 1, Active: true}}, 1900)
	mem, _ := s.ServerAlerts(ctx, p.ID)
	mem[0].ProbeID = p.ID
	s.FireServerAlert(ctx, mem[0], 60, "", 100) // eski ama sürüyor
	if n, err := s.DeleteServerAlertEventsBefore(ctx, 1300); err != nil || n != 1 {
		t.Fatal(n, err)
	}

	// Kanal silinince bağlantısı, ajan silinince her şeyi silinir.
	if err := s.DeleteNotification(ctx, other.ID); err != nil {
		t.Fatal(err)
	}
	if ids, _ := s.ProbeNotificationIDs(ctx, p.ID); len(ids) != 0 {
		t.Fatalf("silinen kanal: %v", ids)
	}
	s.SetProbeNotifications(ctx, p.ID, []int64{def.ID})
	s.SaveServerSample(ctx, ServerSample{ProbeID: p.ID, Time: 60, At: 60, Data: []byte(`{}`)})
	if _, err := s.DeleteProbe(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"server_stats", "server_alerts", "server_alert_events", "probe_notifications"} {
		var n int
		if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != 0 {
			t.Errorf("%s: %d satır kaldı (%v)", table, n, err)
		}
	}
}
