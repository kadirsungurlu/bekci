package store

import (
	"context"
	"errors"
	"testing"
)

// Migration 36: sayfa alanı, abone kaydı, tek kullanımlık onay, sayfa
// silinince aboneler de silinir (CASCADE), bekleyenlerin temizliği.
func TestPageSubscribers(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p := StatusPage{Slug: "abone", Title: "Abone", Published: true, Subscriptions: true}
	if err := s.CreatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if got, err := s.GetPage(ctx, p.ID); err != nil || !got.Subscriptions {
		t.Fatalf("subscriptions kaydedilmedi: %+v %v", got, err)
	}
	p.Subscriptions = false
	if err := s.UpdatePage(ctx, &p); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.GetPage(ctx, p.ID); got.Subscriptions {
		t.Fatal("subscriptions güncellenmedi")
	}

	a := PageSubscriber{PageID: p.ID, Email: "a@ornek.com", Lang: "tr", BaseURL: "https://x", CreatedAt: 100,
		ConfirmHash: "h1", ConfirmExpires: 200, ConfirmSentAt: 100, UnsubKey: "k"}
	if err := s.CreatePageSubscriber(ctx, &a); err != nil || a.ID == 0 {
		t.Fatalf("abone eklenemedi: %v", err)
	}
	dup := a
	if err := s.CreatePageSubscriber(ctx, &dup); err == nil {
		t.Fatal("aynı sayfada aynı adres ikinci kez eklenmemeli")
	}
	b := PageSubscriber{PageID: p.ID, Email: "b@ornek.com", Lang: "en", CreatedAt: 50, ConfirmHash: "h2", UnsubKey: "k2"}
	if err := s.CreatePageSubscriber(ctx, &b); err != nil {
		t.Fatal(err)
	}
	if got, err := s.PageSubscriberByConfirmHash(ctx, "h1"); err != nil || got.ID != a.ID {
		t.Fatalf("onay özetiyle bulunamadı: %+v %v", got, err)
	}
	if err := s.ConfirmPageSubscriber(ctx, a.ID, "h1", 150); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmPageSubscriber(ctx, a.ID, "h1", 151); !errors.Is(err, ErrNotFound) {
		t.Fatalf("onay belirteci tek kullanımlık olmalı: %v", err)
	}
	if _, err := s.PageSubscriberByConfirmHash(ctx, ""); !errors.Is(err, ErrNotFound) {
		t.Fatal("boş özet hiçbir kaydı bulmamalı")
	}
	conf, err := s.ConfirmedPageSubscribers(ctx, p.ID)
	if err != nil || len(conf) != 1 || conf[0].Email != "a@ornek.com" || conf[0].ConfirmedAt != 150 {
		t.Fatalf("onaylı aboneler yanlış: %+v %v", conf, err)
	}
	if n, _ := s.CountPageSubscribers(ctx, p.ID); n != 2 {
		t.Fatalf("abone sayısı = %d", n)
	}
	// Bekleyen (b, created 50) temizlenir; onaylı (a) kalır.
	if n, err := s.DeletePendingSubscribersBefore(ctx, 120); err != nil || n != 1 {
		t.Fatalf("bekleyen temizliği: %d %v", n, err)
	}
	if err := s.DeletePageSubscriber(ctx, p.ID+1, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("başka sayfanın abonesi silinmemeli")
	}
	if err := s.DeletePage(ctx, p.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetPageSubscriber(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("sayfa silinince aboneler de silinmeli: %v", err)
	}
}
