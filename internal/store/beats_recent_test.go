package store

import (
	"context"
	"testing"
)

func TestLastBeatsAndAuditFilter(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	m := newMonitor(t, s, "Site")
	for i := int64(1); i <= 60; i++ {
		b := Beat{MonitorID: m.ID, Time: 1000 + i, Status: StatusUp, PingMs: i, Message: "200 OK"}
		if i%10 == 0 {
			b.Status, b.PingMs, b.Message, b.Location = StatusDown, -1, "zaman aşımı", "Yerel, Frankfurt"
		}
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: b}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := s.LastBeats(ctx, m.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 50 || list[0].Time != 1060 || list[49].Time != 1011 {
		t.Fatalf("son 50 yeniden eskiye olmalı: %d %d..%d", len(list), list[0].Time, list[len(list)-1].Time)
	}
	// 1060: çalışmıyor, ping yok (-1), konum dolu.
	if list[0].Status != StatusDown || list[0].PingMs != -1 || list[0].Location != "Yerel, Frankfurt" || list[0].Message != "zaman aşımı" {
		t.Fatalf("kayıt: %+v", list[0])
	}
	if list[1].PingMs != 59 || list[1].Location != "" {
		t.Fatalf("kayıt: %+v", list[1])
	}
	if l, _ := s.LastBeats(ctx, m.ID, 5); len(l) != 5 {
		t.Fatalf("limit 5: %d", len(l))
	}
	if l, _ := s.LastBeats(ctx, m.ID, 1000); len(l) != 60 {
		t.Fatalf("limit üst sınırı 200 (60 kayıt var) -> hepsi: %d", len(l))
	}

	// İşlem kaydı süzgeçleri.
	entries := []AuditEntry{
		{Time: 100, Username: "kadir", Action: "monitor.create", TargetType: "monitor", TargetName: "Site", IP: "10.0.0.1"},
		{Time: 200, Username: "ayse", Action: "monitor.delete", TargetType: "monitor", TargetName: "Blog", IP: "10.0.0.2"},
		{Time: 300, Username: "kadir", Action: "login.fail", Detail: "yanlış şifre", IP: "10.0.0.3"},
		{Time: 400, Username: "ayse", Action: "user.update", TargetType: "user", TargetName: "kadir", IP: "10.0.0.2"},
	}
	for _, e := range entries {
		if err := s.AddAudit(ctx, e); err != nil {
			t.Fatal(err)
		}
	}
	count := func(f AuditFilter) int {
		l, err := s.ListAuditFiltered(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		return len(l)
	}
	if n := count(AuditFilter{}); n != 4 {
		t.Fatalf("süzgeçsiz: %d", n)
	}
	if n := count(AuditFilter{User: "kadir"}); n != 2 {
		t.Fatalf("kullanıcı: %d", n)
	}
	if n := count(AuditFilter{Action: "monitor."}); n != 2 {
		t.Fatalf("alan öneki: %d", n)
	}
	if n := count(AuditFilter{Action: "monitor.delete"}); n != 1 {
		t.Fatalf("tam eylem: %d", n)
	}
	if n := count(AuditFilter{From: 200, To: 300}); n != 2 {
		t.Fatalf("aralık: %d", n)
	}
	if n := count(AuditFilter{Query: "BLOG"}); n != 1 {
		t.Fatalf("hedef araması harf duyarsız: %d", n)
	}
	if n := count(AuditFilter{Query: "şifre"}); n != 1 {
		t.Fatalf("ayrıntı araması: %d", n)
	}
	if n := count(AuditFilter{Query: "10.0.0.2"}); n != 2 {
		t.Fatalf("ip araması: %d", n)
	}
	if n := count(AuditFilter{Query: "kadir"}); n != 3 {
		t.Fatalf("kullanıcı+hedef araması: %d", n)
	}
	l, _ := s.ListAuditFiltered(ctx, AuditFilter{User: "ayse", Limit: 1})
	if len(l) != 1 || l[0].Action != "user.update" {
		t.Fatalf("limit ve sıra: %+v", l)
	}
	if l2, _ := s.ListAuditFiltered(ctx, AuditFilter{User: "ayse", Before: l[0].ID}); len(l2) != 1 || l2[0].Action != "monitor.delete" {
		t.Fatalf("sayfalama: %+v", l2)
	}
	users, actions, err := s.AuditFacets(ctx)
	if err != nil || len(users) != 2 || users[0] != "ayse" || len(actions) != 4 || actions[0] != "login.fail" {
		t.Fatalf("yüzler: %v %v %v", users, actions, err)
	}
}
