package rdap

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestDomainAndHost(t *testing.T) {
	cases := map[string]string{
		"www.ornek.com":    "ornek.com",
		"a.b.ornek.com.tr": "ornek.com.tr",
		"Ornek.CO.UK.":     "ornek.co.uk",
		"kadir.github.io":  "github.io",
		"localhost":        "",
		"192.168.1.5":      "",
		"::1":              "",
		"sunucu.local":     "",
		"intra.sirket.lan": "",
		"com":              "",
		// "example" PSL'de yoktur: son iki etiket alınır; RDAP önyüklemede
		// bulunamayınca "desteklenmiyor" olur.
		"ornek.example":      "ornek.example",
		"x.s3.amazonaws.com": "amazonaws.com",
	}
	for host, want := range cases {
		if got := Domain(host); got != want {
			t.Errorf("Domain(%q) = %q, %q bekleniyordu", host, got, want)
		}
	}
	hosts := map[string]string{
		"https://www.ornek.com:8443/yol?x=1": "www.ornek.com",
		"ornek.com:443":                      "ornek.com",
		"ornek.com":                          "ornek.com",
		"[2001:db8::1]:443":                  "2001:db8::1",
		"ornek.com (A @ 1.1.1.1)":            "ornek.com (A @ 1.1.1.1)",
	}
	for in, want := range hosts {
		if got := HostOf(in); got != want {
			t.Errorf("HostOf(%q) = %q, %q bekleniyordu", in, got, want)
		}
	}
}

func TestLookup(t *testing.T) {
	var bootstrapHits, rdapHits atomic.Int32
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/dns.json", func(w http.ResponseWriter, r *http.Request) {
		bootstrapHits.Add(1)
		w.Write([]byte(`{"services":[[["com","net"],["http://` + r.Host + `/rdap/","https://` + r.Host + `/rdap-tls/"]],[["tr"],["https://` + r.Host + `/tr/"]]]}`))
	})
	mux.HandleFunc("/rdap/domain/ornek.com", func(w http.ResponseWriter, r *http.Request) {
		rdapHits.Add(1)
		w.Header().Set("Content-Type", "application/rdap+json")
		w.Write([]byte(`{"ldhName":"ORNEK.COM","events":[{"eventAction":"registration","eventDate":"2010-05-01T00:00:00Z"},{"eventAction":"expiration","eventDate":"2027-05-01T04:00:00Z"}],
		  "entities":[{"roles":["registrar"],"vcardArray":["vcard",[["version",{},"text","4.0"],["fn",{},"text","Örnek Kayıt A.Ş."]]]}]}`))
	})
	mux.HandleFunc("/rdap/domain/yok.com", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(404) })
	mux.HandleFunc("/rdap/domain/tarihsiz.net", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ldhName":"TARIHSIZ.NET","events":[{"eventAction":"registration","eventDate":"2010-05-01T00:00:00Z"}]}`))
	})
	mux.HandleFunc("/rdap/domain/bozuk.net", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base = srv.URL
	// https:// adresleri önce denenir ama bu sunucu TLS konuşmaz; sıralamayı
	// test etmek için TLS adresinin hata vermesi ve HTTP'ye düşülmesi gerekir.
	c := New()
	c.HTTP = srv.Client()
	c.HTTP.Timeout = 3 * time.Second
	c.BootstrapURL = base + "/dns.json"
	ctx := context.Background()

	r, err := c.Lookup(ctx, "www.ornek.com")
	if err != nil || r.Status != StatusOK || r.Domain != "ornek.com" || r.Registrar != "Örnek Kayıt A.Ş." ||
		!r.Expires.Equal(time.Date(2027, 5, 1, 4, 0, 0, 0, time.UTC)) {
		t.Fatalf("ornek.com: %+v %v", r, err)
	}
	if r, err := c.Lookup(ctx, "yok.com"); err != nil || r.Status != StatusNotFound {
		t.Fatalf("404 kayıt yok olmalı: %+v %v", r, err)
	}
	if r, err := c.Lookup(ctx, "tarihsiz.net"); err != nil || r.Status != StatusNoExpiry {
		t.Fatalf("bitiş tarihi olmayan kayıt: %+v %v", r, err)
	}
	// Sunucu hatası: hata döner (durum yazılmaz, bildirim gitmez).
	if _, err := c.Lookup(ctx, "bozuk.net"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("500 geçici hata olmalı: %v", err)
	}
	// RDAP sunucusu olmayan TLD ve sorgulanamaz adlar: desteklenmiyor.
	if r, err := c.Lookup(ctx, "ornek.de"); err != nil || r.Status != StatusUnsupported || r.Domain != "ornek.de" {
		t.Fatalf("de: %+v %v", r, err)
	}
	if r, err := c.Lookup(ctx, "10.0.0.1"); err != nil || r.Status != StatusUnsupported {
		t.Fatalf("ip: %+v %v", r, err)
	}
	// Önyükleme dosyası bir kez indirilir.
	if bootstrapHits.Load() != 1 {
		t.Fatalf("önyükleme %d kez indirildi, 1 bekleniyordu", bootstrapHits.Load())
	}
	// TLS sunucusuna ulaşılamayınca (hata) listedeki diğer adres denenir.
	c2 := New()
	c2.HTTP = srv.Client()
	c2.BootstrapURL = base + "/dns.json"
	c2.mu.Lock()
	c2.services = map[string][]string{"com": {"https://127.0.0.1:1/rdap/", base + "/rdap/"}}
	c2.fetchedAt = time.Now()
	c2.mu.Unlock()
	if r, err := c2.Lookup(ctx, "ornek.com"); err != nil || r.Status != StatusOK {
		t.Fatalf("yedek sunucu: %+v %v", r, err)
	}
	// Önyükleme indirilemezse hata (eski kopya yoksa).
	c3 := New()
	c3.HTTP = srv.Client()
	c3.BootstrapURL = base + "/yok.json"
	if _, err := c3.Lookup(ctx, "ornek.com"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("önyükleme hatası: %v", err)
	}
}
