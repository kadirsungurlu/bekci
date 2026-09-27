package store

import (
	"context"
	"encoding/json"
	"testing"
)

func TestIncidentEventsAndCapture(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "site")

	id, err := s.StartIncident(ctx, m.ID, 1000, "HTTP 400 Bad Request")
	if err != nil || id == 0 {
		t.Fatalf("olay açılamadı: %d %v", id, err)
	}
	// Açık olay varken aynı kimlik döner.
	if again, err := s.StartIncident(ctx, m.ID, 1100, "başka"); err != nil || again != id {
		t.Fatalf("ikinci açılış yeni olay oluşturdu: %d %v", again, err)
	}
	if got, _ := s.OpenIncidentID(ctx, m.ID); got != id {
		t.Fatalf("OpenIncidentID = %d", got)
	}

	err = s.AddIncidentEvents(ctx, id,
		IncidentEvent{Time: 990, Kind: EventRetry, Location: "Ana sunucu", Message: "HTTP 400", Data: EventData(map[string]int{"attempt": 1, "max": 1})},
		IncidentEvent{Time: 1000, Kind: EventDown, Location: "Ana sunucu", Message: "HTTP 400 Bad Request"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddOpenIncidentEvent(ctx, m.ID, IncidentEvent{Time: 1000, Kind: EventNotify, Message: "Bildirim gönderildi"}); err != nil {
		t.Fatal(err)
	}
	evs, err := s.IncidentEvents(ctx, id)
	if err != nil || len(evs) != 3 {
		t.Fatalf("kayıtlar: %+v %v", evs, err)
	}
	// Yeniden eskiye; aynı saniyede sonra eklenen önce.
	if evs[0].Kind != EventNotify || evs[1].Kind != EventDown || evs[2].Kind != EventRetry || evs[1].Data != nil {
		t.Fatalf("sıra/veri yanlış: %+v", evs)
	}
	var d map[string]int
	if json.Unmarshal(evs[2].Data, &d) != nil || d["attempt"] != 1 {
		t.Fatalf("data: %s", evs[2].Data)
	}

	// Yakalama yalnızca bir kez yazılır.
	if err := s.SaveIncidentCapture(ctx, id, 1000, "Ana sunucu", []byte(`{"method":"GET","status":400}`)); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveIncidentCapture(ctx, id, 1100, "Başka", []byte(`{"method":"GET","status":500}`)); err != nil {
		t.Fatal(err)
	}
	c, ok, err := s.GetIncidentCapture(ctx, id)
	if err != nil || !ok || c.Location != "Ana sunucu" || c.Time != 1000 || string(c.Detail) != `{"method":"GET","status":400}` {
		t.Fatalf("yakalama: %+v %v %v", c, ok, err)
	}

	inc, err := s.GetIncident(ctx, id)
	if err != nil || inc.MonitorName != "site" || inc.ResolvedAt != 0 || inc.Cause != "HTTP 400 Bad Request" {
		t.Fatalf("GetIncident: %+v %v", inc, err)
	}
	if _, err := s.GetIncident(ctx, id+100); err != ErrNotFound {
		t.Fatalf("olmayan olay: %v", err)
	}

	// Çözülen olayın yakalaması saklama süresi dolunca silinir; açık olaylarınki silinmez.
	s.ResolveIncident(ctx, m.ID, 2000)
	id2, _ := s.StartIncident(ctx, m.ID, 3000, "yeni")
	s.SaveIncidentCapture(ctx, id2, 3000, "", []byte(`{}`))
	if n, err := s.DeleteIncidentCapturesBefore(ctx, 1500); err != nil || n != 0 {
		t.Fatalf("erken silindi: %d %v", n, err)
	}
	if n, err := s.DeleteIncidentCapturesBefore(ctx, 99999); err != nil || n != 1 {
		t.Fatalf("silinmedi: %d %v", n, err)
	}
	if _, ok, _ := s.GetIncidentCapture(ctx, id2); !ok {
		t.Fatal("açık olayın yakalaması silinmemeli")
	}

	// Monitör silinince olaylar, kayıtlar ve yakalamalar da gider.
	if err := s.DeleteMonitor(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	var n int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_events").Scan(&n)
	var nc int
	s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM incident_captures").Scan(&nc)
	if n != 0 || nc != 0 {
		t.Fatalf("cascade çalışmadı: %d kayıt, %d yakalama", n, nc)
	}
}

func TestIncidentEventsCap(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "site")
	id, _ := s.StartIncident(ctx, m.ID, 1000, "x")
	batch := make([]IncidentEvent, 0, MaxIncidentEvents+50)
	for i := range MaxIncidentEvents + 50 {
		batch = append(batch, IncidentEvent{Time: 1000 + int64(i), Kind: EventChange, Message: "değişti"})
	}
	if err := s.AddIncidentEvents(ctx, id, batch...); err != nil {
		t.Fatal(err)
	}
	// Sınır doluyken yenisi atılır, çözülme kaydı yine yazılır.
	s.AddIncidentEvents(ctx, id, IncidentEvent{Time: 5000, Kind: EventChange, Message: "atılmalı"})
	s.AddIncidentEvents(ctx, id, IncidentEvent{Time: 6000, Kind: EventUp, Message: "200 OK"})
	evs, _ := s.IncidentEvents(ctx, id)
	if len(evs) != MaxIncidentEvents+1 {
		t.Fatalf("%d kayıt, %d bekleniyordu", len(evs), MaxIncidentEvents+1)
	}
	if evs[0].Kind != EventUp || evs[1].Kind != EventLimit {
		t.Fatalf("son kayıtlar: %+v %+v", evs[0], evs[1])
	}
	for _, ev := range evs {
		if ev.Message == "atılmalı" {
			t.Fatal("sınırdan sonra kayıt eklendi")
		}
	}
}
