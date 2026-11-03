package probe

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"testing"
	"time"
)

func testUplink(t *testing.T) *uplink {
	t.Helper()
	u := newUplink("")
	u.resolve = func(ip, _ string) (oobBind, error) {
		if ip == "203.0.113.9" {
			return oobBind{}, ErrOOBIfaceMissing
		}
		return oobBind{Iface: "eth1", Index: 3, Local: netip.MustParseAddr(ip)}, nil
	}
	u.primary = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("p") }
	u.base = &net.Dialer{}
	u.wrap = func(d *net.Dialer) dialFunc { return d.DialContext }
	u.tr = &http.Transport{}
	return u
}

func nolog(string, ...any) {}

func TestUplinkStateMachine(t *testing.T) {
	u := testUplink(t)
	ctx := context.Background()
	t0 := time.Unix(1_700_000_000, 0)
	ok := map[bool]bool{true: true, false: false} // hat -> /healthz başarılı mı
	hz := func(_ context.Context, oob bool) error {
		if ok[oob] {
			return nil
		}
		return errors.New("yok")
	}
	h := http.Header{}
	u.headers(h)
	if h.Get(HeaderUplink) != "" {
		t.Fatal("OOB tanımsızken başlık olmamalı")
	}
	u.setConfig(OOBConfig{IP: "192.168.9.7", Name: "LTE"}, nolog)
	h = http.Header{}
	u.headers(h)
	if h.Get(HeaderUplink) != "primary" || h.Get(HeaderOOBIface) != "ok" {
		t.Fatalf("başlıklar: %v", h)
	}
	fail := func(n int) {
		for range n {
			_, lc, done := u.request(ctx)
			u.observe(lc, true)
			done()
		}
	}
	// Eşik altı: geçiş yok. 4xx/5xx gibi yanıtlar (observe false) sayacı sıfırlar.
	fail(2)
	_, lc, done := u.request(ctx)
	u.observe(lc, false)
	done()
	fail(2)
	if u.tick(ctx, t0, 3, 5*time.Minute, false, hz, nolog) {
		t.Fatal("eşik altında geçildi")
	}
	fail(1)
	// Eşlere ana hattan erişiliyor: geçilmez.
	if u.tick(ctx, t0, 3, 5*time.Minute, true, hz, nolog) {
		t.Fatal("mesh eşleri erişilebilirken geçildi")
	}
	// OOB /healthz başarısız: geçilmez.
	ok[true] = false
	if u.tick(ctx, t0, 3, 5*time.Minute, false, hz, nolog) {
		t.Fatal("OOB sağlıksızken geçildi")
	}
	ok[true] = true
	_, oldLine, done := u.request(ctx)
	if !u.tick(ctx, t0, 3, 5*time.Minute, false, hz, nolog) || !u.onOOB.Load() {
		t.Fatal("OOB'ye geçilmedi")
	}
	if oldLine.Err() == nil {
		t.Fatal("geçişte eski hattaki istekler iptal edilmeli")
	}
	// Eski hattan gelen sonuç sayılmaz.
	u.observe(oldLine, true)
	done()
	h = http.Header{}
	u.headers(h)
	if h.Get(HeaderUplink) != "oob" {
		t.Fatalf("OOB başlığı: %v", h)
	}
	// Dönüş: 5 dk dolmadan denenmez.
	ok[false] = true
	if u.tick(ctx, t0.Add(4*time.Minute), 3, 5*time.Minute, false, hz, nolog) {
		t.Fatal("en kısa süre dolmadan dönüldü")
	}
	// 5 dk sonra art arda 3 başarı; arada başarısızlık sayacı sıfırlar.
	at := t0.Add(5 * time.Minute)
	u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog)
	ok[false] = false
	u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog)
	ok[false] = true
	for i := range 2 {
		if u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog) {
			t.Fatalf("%d. başarıda erken dönüldü", i+1)
		}
	}
	if !u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog) || u.onOOB.Load() {
		t.Fatal("3 başarıdan sonra ana hatta dönülmedi")
	}

	// OOB'nin kendisi koparsa süre beklenmez, tek başarı yeter.
	fail(3)
	u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog)
	if !u.onOOB.Load() {
		t.Fatal("ikinci geçiş olmadı")
	}
	fail(3)
	if !u.tick(ctx, at.Add(time.Second), 3, 5*time.Minute, false, hz, nolog) || u.onOOB.Load() {
		t.Fatal("OOB koptuğunda ana hatta hemen dönülmeli")
	}

	// OOB'deyken panel OOB'yi kaldırırsa ana hatta dönülür.
	fail(3)
	u.tick(ctx, at, 3, 5*time.Minute, false, hz, nolog)
	u.setConfig(OOBConfig{}, nolog)
	if u.onOOB.Load() {
		t.Fatal("OOB kaldırılınca ana hatta dönülmeli")
	}
}

func TestUplinkIfaceMissing(t *testing.T) {
	u := testUplink(t)
	u.setConfig(OOBConfig{IP: "203.0.113.9"}, nolog)
	h := http.Header{}
	u.headers(h)
	if h.Get(HeaderOOBIface) != "missing" || h.Get(HeaderUplink) != "primary" {
		t.Fatalf("başlıklar: %v", h)
	}
	for range 5 {
		_, lc, done := u.request(context.Background())
		u.observe(lc, true)
		done()
	}
	called := false
	hz := func(context.Context, bool) error { called = true; return nil }
	if u.tick(context.Background(), time.Now(), 3, time.Minute, false, hz, nolog) || called {
		t.Fatal("arayüz yokken geçiş denenmemeli")
	}
}
