package store

import (
	"context"
	"testing"
)

func TestDomainInfoAndNotices(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	a := newMonitor(t, s, "a")
	b := newMonitor(t, s, "b")
	// Yeni monitörde uyarı açık, bilgi yok.
	m, _ := s.GetMonitor(ctx, a.ID)
	if !m.DomainExpiry || m.DomainStatus != "" || m.DomainCheckedAt != 0 {
		t.Fatalf("varsayılan: %+v", m)
	}
	list, err := s.MonitorsForDomainCheck(ctx, 1000)
	if err != nil || len(list) != 2 {
		t.Fatalf("sorgulanacaklar: %d %v", len(list), err)
	}
	if err := s.UpdateDomain(ctx, a.ID, DomainInfo{Name: "ornek.com", Status: DomainOK, ExpiresAt: 2_000_000_000, Registrar: "Kayıt", CheckedAt: 900}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.GetMonitor(ctx, a.ID)
	if m.DomainName != "ornek.com" || m.DomainStatus != DomainOK || m.DomainExpiresAt != 2_000_000_000 || m.DomainRegistrar != "Kayıt" || m.DomainCheckedAt != 900 {
		t.Fatalf("yazılan bilgi: %+v", m)
	}
	// Hata: bitiş ve operatör korunur, durum ve zaman değişir.
	if err := s.UpdateDomain(ctx, a.ID, DomainInfo{Name: "", Status: DomainError, CheckedAt: 950}); err != nil {
		t.Fatal(err)
	}
	m, _ = s.GetMonitor(ctx, a.ID)
	if m.DomainName != "ornek.com" || m.DomainStatus != DomainError || m.DomainExpiresAt != 2_000_000_000 || m.DomainRegistrar != "Kayıt" || m.DomainCheckedAt != 950 {
		t.Fatalf("hata sonrası: %+v", m)
	}
	// Son sorgusu yeni olan listeye girmez; eskisi girer.
	if list, _ := s.MonitorsForDomainCheck(ctx, 940); len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("eşik 940: %+v", list)
	}
	// Uyarı kapalı monitör listeye girmez.
	m.DomainExpiry = false
	if err := s.UpdateMonitor(ctx, &m, nil, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.MonitorsForDomainCheck(ctx, 1000); len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("kapalı monitör: %+v", list)
	}
	m, _ = s.GetMonitor(ctx, a.ID)
	if m.DomainExpiry {
		t.Fatal("domain_expiry güncellenmeli")
	}
	// Hedef değişince (resetState) alan adı bilgisi sıfırlanır.
	if err := s.UpdateMonitor(ctx, &m, nil, true); err != nil {
		t.Fatal(err)
	}
	m, _ = s.GetMonitor(ctx, a.ID)
	if m.DomainName != "" || m.DomainExpiresAt != 0 || m.DomainStatus != "" || m.DomainCheckedAt != 0 {
		t.Fatalf("sıfırlama: %+v", m)
	}
	// Eşik başına bir bildirim.
	first, _ := s.MarkDomainNotice(ctx, b.ID, 5000, 14)
	second, _ := s.MarkDomainNotice(ctx, b.ID, 5000, 14)
	other, _ := s.MarkDomainNotice(ctx, b.ID, 5000, 7)
	if !first || second || !other {
		t.Fatalf("bildirim kaydı: %v %v %v", first, second, other)
	}
	// Ayarlar: alan adı eşikleri doğrulanır, sıralanır; nil → varsayılan.
	a1 := DefaultSettings()
	a1.DomainDays = nil
	if err := a1.Validate(); err != nil || len(a1.DomainDays) != len(DefaultDomainDays) {
		t.Fatalf("nil eşik: %v %v", a1.DomainDays, err)
	}
	a1.DomainDays = []int{7, 30, 7, 1}
	if err := a1.Validate(); err != nil || len(a1.DomainDays) != 3 || a1.DomainDays[0] != 30 || a1.DomainDays[2] != 1 {
		t.Fatalf("sıralama/tekilleştirme: %v %v", a1.DomainDays, err)
	}
	a1.DomainDays = []int{400}
	if err := a1.Validate(); err == nil {
		t.Fatal("365 üstü eşik reddedilmeli")
	}
	a1.DomainDays = []int{}
	if err := a1.Validate(); err != nil || len(a1.DomainDays) != 0 {
		t.Fatalf("boş liste (kapalı) korunmalı: %v %v", a1.DomainDays, err)
	}
}
