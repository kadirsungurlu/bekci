package engine

import "testing"

// TestHubSubscribeCaps kullanıcı sınırında en eski akışın kapatıldığını ve
// yeni aboneliğin kabul edildiğini doğrular.
func TestHubSubscribeCaps(t *testing.T) {
	h := NewHub()
	var chans []<-chan []byte
	var stops []func()
	for i := 0; i < MaxSubsPerUser; i++ {
		ch, stop, ok := h.Subscribe(1)
		if !ok {
			t.Fatalf("%d. abonelik reddedildi (sınır %d)", i+1, MaxSubsPerUser)
		}
		chans, stops = append(chans, ch), append(stops, stop)
	}
	// Sınır üstü: kabul edilir, en eski (ilk) akış kapatılır.
	newest, stopNewest, ok := h.Subscribe(1)
	if !ok {
		t.Fatal("kullanıcı sınırında yeni abonelik reddedilmemeli")
	}
	if _, open := <-chans[0]; open {
		t.Fatal("en eski akışın kanalı kapatılmalıydı")
	}
	select {
	case <-chans[1]:
		t.Fatal("ikinci akış kapatılmamalıydı")
	default:
	}
	// Kapatılan akışın unsubscribe'ı sayaçları bozmaz (idempotent).
	stops[0]()
	stops[0]()
	h.Publish("x", 1)
	if msg := <-newest; msg == nil {
		t.Fatal("yeni akış olay almalı")
	}
	// Başka kullanıcı etkilenmez.
	if _, stop, ok := h.Subscribe(2); !ok {
		t.Fatal("başka kullanıcı sınırlanmamalı")
	} else {
		stop()
	}
	h.mu.Lock()
	n, total := len(h.perUser[1]), h.total
	h.mu.Unlock()
	if n != MaxSubsPerUser || total != MaxSubsPerUser {
		t.Fatalf("sayaçlar: kullanıcı=%d toplam=%d, ikisi de %d bekleniyordu", n, total, MaxSubsPerUser)
	}
	for _, s := range stops[1:] {
		s()
	}
	stopNewest()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.total != 0 || len(h.perUser) != 0 {
		t.Fatalf("hepsi kapanınca sayaçlar sıfırlanmalı: toplam=%d", h.total)
	}
}

// TestHubGlobalCap tüm kullanıcılar için toplam abone sınırını doğrular.
func TestHubGlobalCap(t *testing.T) {
	h := NewHub()
	var stops []func()
	// Her biri farklı kullanıcı kimliği: kullanıcı sınırına takılmadan genel
	// sınıra ulaşılır.
	for i := 0; i < MaxSubsGlobal; i++ {
		_, stop, ok := h.Subscribe(int64(i + 1))
		if !ok {
			t.Fatalf("%d. abonelik reddedildi (genel sınır %d)", i+1, MaxSubsGlobal)
		}
		stops = append(stops, stop)
	}
	if _, _, ok := h.Subscribe(999999); ok {
		t.Fatal("genel sınır aşıldığında abonelik kabul edildi")
	}
	for _, s := range stops {
		s()
	}
}
