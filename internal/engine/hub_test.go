package engine

import "testing"

// TestHubSubscribeCaps kullanıcı başına eşzamanlı abone sınırını doğrular.
func TestHubSubscribeCaps(t *testing.T) {
	h := NewHub()
	var stops []func()
	for i := 0; i < MaxSubsPerUser; i++ {
		_, stop, ok := h.Subscribe(1)
		if !ok {
			t.Fatalf("%d. abonelik reddedildi (sınır %d)", i+1, MaxSubsPerUser)
		}
		stops = append(stops, stop)
	}
	if _, _, ok := h.Subscribe(1); ok {
		t.Fatal("kullanıcı sınırı aşıldığında abonelik kabul edildi")
	}
	// Başka kullanıcı etkilenmez.
	if _, stop, ok := h.Subscribe(2); !ok {
		t.Fatal("başka kullanıcı sınırlanmamalı")
	} else {
		stops = append(stops, stop)
	}
	// Bir abonelik kapanınca aynı kullanıcıya yer açılır (defer decrement).
	stops[0]()
	if _, stop, ok := h.Subscribe(1); !ok {
		t.Fatal("abonelik kapandıktan sonra yer açılmalı")
	} else {
		stops = append(stops, stop)
	}
	// unsubscribe iki kez çağrılınca sayaç bir kez düşer (idempotent).
	stops[1]()
	stops[1]()
	if _, _, ok := h.Subscribe(1); !ok {
		t.Fatal("çift unsubscribe sayaçları bozdu")
	}
	for _, s := range stops {
		s()
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
