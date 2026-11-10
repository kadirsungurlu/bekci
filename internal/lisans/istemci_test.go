package lisans

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKiraIste(t *testing.T) {
	var gelen map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/kira" {
			w.WriteHeader(404)
			return
		}
		gelen = nil
		_ = json.NewDecoder(r.Body).Decode(&gelen)
		switch gelen["lisans"] {
		case "L-OK":
			_, _ = w.Write([]byte(`{"kira":"BEKCI-K1.a.b"}`))
		case "L-IPTAL":
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`{"kod":"iptal","mesaj":"lisans iptal edildi"}`))
		case "L-SINIR":
			w.WriteHeader(409)
			_, _ = w.Write([]byte(`{"kod":"tasima_sinir","mesaj":"30 gün"}`))
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	c := &Istemci{Adres: srv.URL + "/"}
	ctx := context.Background()

	kira, err := c.KiraIste(ctx, Istek{Lisans: "L-OK", Panel: "P1", Surum: "1.9.0", SunucuSayisi: 3})
	if err != nil || kira != "BEKCI-K1.a.b" {
		t.Fatalf("200: %q %v", kira, err)
	}
	if _, var1 := gelen["cihaz"]; var1 {
		t.Fatal("boş cihaz gönderilmemeli")
	}
	if len(gelen) != 4 {
		t.Fatalf("gövde yalnız izinli alanları içermeli: %v", gelen)
	}

	_, err = c.KiraIste(ctx, Istek{Lisans: "L-IPTAL", Panel: "P1"})
	var ret *RetHatasi
	if !errors.As(err, &ret) || ret.Kod != "iptal" || ret.HTTP != 403 || !KesinRet(err) {
		t.Fatalf("403 iptal: %v", err)
	}
	_, err = c.KiraIste(ctx, Istek{Lisans: "L-SINIR", Panel: "P1"})
	if !errors.As(err, &ret) || KesinRet(err) {
		t.Fatalf("tasima_sinir kesin ret değil: %v", err)
	}
	_, err = c.KiraIste(ctx, Istek{Lisans: "L-X", Panel: "P1"})
	if !errors.Is(err, ErrUlasilamiyor) || KesinRet(err) {
		t.Fatalf("500 ulaşılamıyor olmalı: %v", err)
	}
	srv.Close()
	if _, err = c.KiraIste(ctx, Istek{Lisans: "L-OK"}); !errors.Is(err, ErrUlasilamiyor) {
		t.Fatalf("ağ hatası: %v", err)
	}
}

func TestAdres(t *testing.T) {
	t.Setenv("LISANS_URL", "")
	if Adres(nil) != VarsayilanAdres {
		t.Fatal("varsayılan")
	}
	t.Setenv("LISANS_URL", "https://ortam.example/")
	if Adres(&Anahtar{}) != "https://ortam.example" {
		t.Fatal("ortam")
	}
	if Adres(&Anahtar{YenilemeAdresi: "https://anahtar.example"}) != "https://anahtar.example" {
		t.Fatal("anahtar önceliği")
	}
}
