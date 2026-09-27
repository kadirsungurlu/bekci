package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPageCRUD(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, b := newMonitor(t, s, "A"), newMonitor(t, s, "B")

	p := StatusPage{Slug: "genel", Title: "Genel", Published: true, CustomDomain: "durum.example.com",
		PasswordHash: "hash", Sections: []PageSection{{Title: "Web", Monitors: []PageMonitor{{ID: a.ID, Name: "Site"}, {ID: b.ID}}}}}
	if err := s.CreatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetPage(ctx, p.ID)
	if err != nil || got.Slug != "genel" || !got.HasPassword || got.PasswordHash != "hash" || got.HasLogo ||
		len(got.Sections) != 1 || got.Sections[0].Monitors[0].Name != "Site" || !got.Published {
		t.Fatalf("sayfa yanlış: %+v %v", got, err)
	}
	if ids := got.MonitorIDs(); len(ids) != 2 || ids[0] != a.ID || ids[1] != b.ID {
		t.Errorf("MonitorIDs = %v", ids)
	}
	if bySlug, err := s.PageBySlug(ctx, "genel"); err != nil || bySlug.ID != p.ID {
		t.Errorf("PageBySlug: %+v %v", bySlug, err)
	}
	if byDomain, err := s.PageByDomain(ctx, "DURUM.example.com"); err != nil || byDomain.ID != p.ID {
		t.Errorf("PageByDomain: %+v %v", byDomain, err)
	}
	if _, err := s.PageBySlug(ctx, "yok"); !errors.Is(err, ErrNotFound) {
		t.Errorf("olmayan sayfa ErrNotFound vermeli: %v", err)
	}

	// Benzersizlik: aynı kısa ad veya alan adı ikinci sayfada kullanılamaz.
	dup := StatusPage{Slug: "genel", Title: "X"}
	if err := s.CreatePage(ctx, &dup); PageConflict(err) != "slug" {
		t.Errorf("kısa ad çakışması bekleniyordu: %v", err)
	}
	dup = StatusPage{Slug: "diger", Title: "X", CustomDomain: "durum.example.com"}
	if err := s.CreatePage(ctx, &dup); PageConflict(err) != "custom_domain" {
		t.Errorf("alan adı çakışması bekleniyordu: %v", err)
	}
	// Alan adı boşsa NULL yazılır: birden fazla sayfa alan adsız olabilir.
	p2 := StatusPage{Slug: "iki", Title: "Iki"}
	p3 := StatusPage{Slug: "uc", Title: "Uc"}
	if err := s.CreatePage(ctx, &p2); err != nil {
		t.Fatal(err)
	}
	if err := s.CreatePage(ctx, &p3); err != nil {
		t.Fatal(err)
	}
	domains, err := s.CustomDomains(ctx)
	if err != nil || len(domains) != 1 || domains["durum.example.com"] != p.ID {
		t.Errorf("CustomDomains = %v %v", domains, err)
	}

	// Güncelleme: şifre kaldırılır, alan adı boşaltılır.
	got.PasswordHash, got.CustomDomain, got.Title = "", "", "Yeni"
	if err := s.UpdatePage(ctx, &got); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetPage(ctx, p.ID)
	if got.HasPassword || got.CustomDomain != "" || got.Title != "Yeni" {
		t.Errorf("güncelleme yanlış: %+v", got)
	}
	if err := s.UpdatePage(ctx, &StatusPage{ID: 9999, Slug: "x", Title: "x"}); !errors.Is(err, ErrNotFound) {
		t.Errorf("olmayan sayfa güncellemesi ErrNotFound vermeli: %v", err)
	}

	list, err := s.ListPages(ctx)
	if err != nil || len(list) != 3 || list[0].Title != "Iki" {
		t.Errorf("liste yanlış: %+v %v", list, err)
	}

	// Logo yaz, oku, kaldır.
	if err := s.SetPageLogo(ctx, p.ID, []byte{1, 2, 3}, "image/png"); err != nil {
		t.Fatal(err)
	}
	data, typ, err := s.PageLogo(ctx, p.ID)
	if err != nil || len(data) != 3 || typ != "image/png" {
		t.Fatalf("logo: %v %q %v", data, typ, err)
	}
	if got, _ := s.GetPage(ctx, p.ID); !got.HasLogo {
		t.Error("has_logo true olmalıydı")
	}
	if err := s.SetPageLogo(ctx, p.ID, nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.PageLogo(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("logo kaldırılmalıydı: %v", err)
	}
	if err := s.SetPageLogo(ctx, 9999, []byte{1}, "image/png"); !errors.Is(err, ErrNotFound) {
		t.Errorf("olmayan sayfa: %v", err)
	}

	// Silinen sayfanın duyuruları da silinir.
	ann := Announcement{PageID: p.ID, Title: "Bakım", Severity: "info", StartsAt: 1}
	if err := s.CreateAnnouncement(ctx, &ann); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePage(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAnnouncement(ctx, ann.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("duyuru sayfayla silinmeliydi: %v", err)
	}
	if err := s.DeletePage(ctx, p.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ikinci silme ErrNotFound vermeli: %v", err)
	}
}

func TestAnnouncements(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := StatusPage{Slug: "a", Title: "A"}
	if err := s.CreatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	mk := func(title string, start, end int64) Announcement {
		a := Announcement{PageID: p.ID, Title: title, Severity: "warning", StartsAt: start, EndsAt: end}
		if err := s.CreateAnnouncement(ctx, &a); err != nil {
			t.Fatal(err)
		}
		return a
	}
	mk("geçmiş", 100, 200)
	open := mk("süresiz", 150, 0)
	mk("gelecek", 1000, 0)
	window := mk("süreli", 250, 400)

	active, err := s.ActiveAnnouncements(ctx, p.ID, 300)
	if err != nil || len(active) != 2 || active[0].ID != window.ID || active[1].ID != open.ID {
		t.Fatalf("aktif duyurular yanlış: %+v %v", active, err)
	}
	// Bitiş anı dahil değildir.
	if active, _ := s.ActiveAnnouncements(ctx, p.ID, 400); len(active) != 1 {
		t.Errorf("400'de sadece süresiz duyuru kalmalı: %+v", active)
	}
	all, _ := s.ListAnnouncements(ctx, p.ID)
	if len(all) != 4 || all[0].Title != "gelecek" {
		t.Errorf("liste yanlış: %+v", all)
	}

	open.Title, open.EndsAt = "güncel", 500
	if err := s.UpdateAnnouncement(ctx, &open); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAnnouncement(ctx, open.ID)
	if err != nil || got.Title != "güncel" || got.EndsAt != 500 || got.PageID != p.ID {
		t.Errorf("güncelleme yanlış: %+v %v", got, err)
	}
	if err := s.DeleteAnnouncement(ctx, open.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAnnouncement(ctx, open.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("ikinci silme ErrNotFound vermeli: %v", err)
	}
	if err := s.UpdateAnnouncement(ctx, &open); !errors.Is(err, ErrNotFound) {
		t.Errorf("silinmiş duyuru güncellemesi ErrNotFound vermeli: %v", err)
	}
}

func TestPublicPageData(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a, b, c := newMonitor(t, s, "A"), newMonitor(t, s, "B"), newMonitor(t, s, "C")

	mons, err := s.MonitorsByIDs(ctx, []int64{a.ID, c.ID, 9999})
	if err != nil || len(mons) != 2 || mons[a.ID].Name != "A" || mons[c.ID].Name != "C" {
		t.Fatalf("MonitorsByIDs = %v %v", mons, err)
	}
	if m, _ := s.MonitorsByIDs(ctx, nil); len(m) != 0 {
		t.Error("boş liste boş sonuç vermeli")
	}

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, s.loc).Unix()
	for _, beat := range []Beat{
		{MonitorID: a.ID, Time: now - 86400, Status: StatusUp, PingMs: 10},
		{MonitorID: a.ID, Time: now, Status: StatusDown, PingMs: -1},
		{MonitorID: b.ID, Time: now, Status: StatusUp, PingMs: 10},
		{MonitorID: a.ID, Time: now - 100*86400, Status: StatusUp, PingMs: 10},
	} {
		if err := s.RecordBeat(ctx, BeatUpdate{Beat: beat}); err != nil {
			t.Fatal(err)
		}
	}
	days := s.DayStarts(now, 90)
	daily, err := s.DailyFor(ctx, []int64{a.ID}, days[0])
	if err != nil || len(daily) != 1 || len(daily[a.ID]) != 2 {
		t.Fatalf("DailyFor = %v %v", daily, err)
	}
	if d := daily[a.ID]; d[0].Time != days[88] || d[0].Up != 1 || d[1].Time != days[89] || d[1].Down != 1 {
		t.Errorf("günlük kovalar yanlış: %+v", d)
	}

	// Olaylar: sadece istenen monitörler; son 14 gün + hâlâ süren eski olay.
	s.OpenIncident(ctx, a.ID, now-30*86400, "eski ama sürüyor")
	s.OpenIncident(ctx, b.ID, now-20*86400, "eski")
	s.ResolveIncident(ctx, b.ID, now-19*86400)
	s.OpenIncident(ctx, b.ID, now-3600, "yeni")
	s.OpenIncident(ctx, c.ID, now-60, "başka monitör")
	inc, err := s.IncidentsFor(ctx, []int64{a.ID, b.ID}, now-14*86400, 100)
	if err != nil || len(inc) != 2 || inc[0].MonitorID != b.ID || inc[1].MonitorID != a.ID || inc[1].ResolvedAt != 0 {
		t.Fatalf("IncidentsFor = %+v %v", inc, err)
	}
	if inc, _ := s.IncidentsFor(ctx, nil, 0, 10); len(inc) != 0 {
		t.Error("boş liste boş sonuç vermeli")
	}
}

func TestDayStarts(t *testing.T) {
	s := openTest(t)
	// İstanbul UTC+3: 22:30 UTC yerel saatle ertesi gün.
	now := time.Date(2026, 9, 26, 22, 30, 0, 0, time.UTC).Unix()
	days := s.DayStarts(now, 90)
	if len(days) != 90 {
		t.Fatalf("90 gün bekleniyordu: %d", len(days))
	}
	if want := time.Date(2026, 9, 27, 0, 0, 0, 0, s.loc).Unix(); days[89] != want {
		t.Errorf("bugün = %d, %d bekleniyordu", days[89], want)
	}
	if want := time.Date(2026, 6, 30, 0, 0, 0, 0, s.loc).Unix(); days[0] != want {
		t.Errorf("ilk gün = %v", time.Unix(days[0], 0).In(s.loc))
	}
	for i := 1; i < len(days); i++ {
		if days[i] <= days[i-1] || days[i] != s.dayStart(days[i]+3600) {
			t.Fatalf("gün başları sıralı ve gün başında olmalı: %d", i)
		}
	}
}
