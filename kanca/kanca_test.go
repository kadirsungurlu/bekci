package kanca

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/check"
)

type sahteChecker struct{ cagrildi int }

func (s *sahteChecker) Normalize(cfg json.RawMessage) (json.RawMessage, error) { return cfg, nil }
func (s *sahteChecker) Check(context.Context, json.RawMessage) check.Result {
	s.cagrildi++
	return check.Result{Up: true, Message: "tamam"}
}
func (s *sahteChecker) Target(json.RawMessage) string { return "hedef" }

func TestKancaKayit(t *testing.T) {
	once := len(Rotalar())
	Rota(RotaTanim{Desen: "GET /api/pro/kanca-test", Erisim: Yonetici, Ozellik: "x", Isleyici: func(http.ResponseWriter, *http.Request) {}})
	if r := Rotalar(); len(r) != once+1 || r[len(r)-1].Desen != "GET /api/pro/kanca-test" || r[len(r)-1].Erisim != Yonetici {
		t.Fatalf("rota kaydı: %+v", r)
	}
	n := len(Baslangiclar())
	Baslangic(func(context.Context, *Panel) error { return nil })
	if len(Baslangiclar()) != n+1 {
		t.Fatal("başlangıç kaydı yok")
	}
	AjanKomutu("kanca-test", "x", func(_ context.Context, g json.RawMessage) (json.RawMessage, error) { return g, nil })
	k, ok := AjanKomutlari()["kanca-test"]
	if !ok || k.Ozellik != "x" {
		t.Fatalf("ajan komutu kaydı: %+v", k)
	}
	if out, err := k.Calistir(context.Background(), json.RawMessage(`1`)); err != nil || string(out) != "1" {
		t.Fatalf("ajan komutu: %s %v", out, err)
	}
	if ProDerlemeMi() {
		t.Fatal("açık derleme Pro sayılmamalı")
	}
	ProDerleme()
	if !ProDerlemeMi() {
		t.Fatal("ProDerleme işaretlemedi")
	}
	proDerleme = false
}

func TestKancaMigrationPanik(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("1000 altı migration panik vermeli")
		}
	}()
	Migration(999, "SELECT 1")
}

func TestKancaIzleyiciLisans(t *testing.T) {
	c := &sahteChecker{}
	IzleyiciTuru("kanca_test_turu", "ozel", c)
	got, ok := check.Get("kanca_test_turu")
	if !ok {
		t.Fatal("tür kaydedilmedi")
	}
	defer func() { OzellikAcik = nil }()

	OzellikAcik = nil
	if r := got.Check(context.Background(), nil); r.Up || r.Message != LisansGerekliMesaj || c.cagrildi != 0 {
		t.Fatalf("lisans yok (nil): %+v çağrı=%d", r, c.cagrildi)
	}
	OzellikAcik = func(string) bool { return false }
	if r := got.Check(context.Background(), nil); r.Up || c.cagrildi != 0 {
		t.Fatalf("lisans kapalı: %+v", r)
	}
	OzellikAcik = func(ad string) bool { return ad == "ozel" }
	if r := got.Check(context.Background(), nil); !r.Up || c.cagrildi != 1 {
		t.Fatalf("lisans açık: %+v", r)
	}
	if got.Target(nil) != "hedef" {
		t.Fatal("Target iletilmedi")
	}
}
