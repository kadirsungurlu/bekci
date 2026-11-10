// Package kanca, açık depoyu Go modülü olarak içe alan Pro modülünün
// (bekci-pro, ayrı depo) kendi kodunu panele init() içinden bağladığı
// kayıt noktalarıdır. internal/ paketleri modül dışından içe alınamadığı için
// bu paket iç olmayan yoldadır (bkz. docs/LISANS-TASARIM.md "Kancalar").
//
// Kayıtlar yalnızca init() sırasında yapılır; okuma (Rotalar, Baslangiclar,
// AjanKomutlari) program başladıktan sonradır, bu yüzden kilit yoktur.
package kanca

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/kadirsungurlu/bekci/internal/check"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Erisim bir Pro rotasının yetki düzeyi.
type Erisim int

const (
	// Genel oturum istemez (genel uç).
	Genel Erisim = iota
	// Oturum giriş yapmış her kullanıcı (izleyici dahil).
	Oturum
	// Yonetici yalnızca yönetici.
	Yonetici
)

// RotaTanim panele eklenecek bir HTTP rotası.
type RotaTanim struct {
	// Desen net/http.ServeMux deseni, ör. "GET /api/pro/filo".
	Desen  string
	Erisim Erisim
	// Ozellik doluysa lisansta bu özellik yokken yalnızca GET/HEAD geçer,
	// değiştiren istekler 402 {"code":"pro_lisans"} alır.
	Ozellik  string
	Isleyici http.HandlerFunc
}

var rotalar []RotaTanim

// Rota Pro rotası kaydeder (init içinde).
func Rota(r ...RotaTanim) {
	for _, t := range r {
		if t.Desen == "" || t.Isleyici == nil {
			panic("kanca.Rota: desen ve işleyici zorunlu")
		}
	}
	rotalar = append(rotalar, r...)
}

// Rotalar kayıtlı Pro rotaları (api paketi bağlar).
func Rotalar() []RotaTanim { return append([]RotaTanim(nil), rotalar...) }

// Panel başlangıç kancalarına verilen panel bağlamı.
type Panel struct {
	// DB panelin veritabanı bağlantısı. Sorgular sürücünün yer tutucusunu
	// kullanmalıdır (postgres: $1, sqlite: ?).
	DB *sql.DB
	// Dialect "postgres" ya da "sqlite".
	Dialect string
	Log     *slog.Logger
	// Ozellik lisansta özellik açık mı?
	Ozellik func(string) bool
	// Bildir yöneticilere sistem bildirimi gönderir.
	Bildir func(ctx context.Context, konu, metin string) error
}

var baslangiclar []func(context.Context, *Panel) error

// Baslangic panel açıldıktan sonra çalışacak işlev kaydeder (init içinde).
// Hata günlüğe yazılır, panel düşmez. Uzun süren iş kendi goroutine'ini açmalı.
func Baslangic(f func(context.Context, *Panel) error) {
	if f == nil {
		panic("kanca.Baslangic: nil işlev")
	}
	baslangiclar = append(baslangiclar, f)
}

// Baslangiclar kayıtlı başlangıç işlevleri.
func Baslangiclar() []func(context.Context, *Panel) error {
	return append([]func(context.Context, *Panel) error(nil), baslangiclar...)
}

// ProMigrationBaslangic Pro migration numaralarının alt sınırı; açık depo
// bunun altındaki numaraları kullanır.
const ProMigrationBaslangic = 1000

// Migration Pro migration'ı kaydeder (n >= 1000; init içinde).
func Migration(n int, sql string) {
	if n < ProMigrationBaslangic {
		panic(fmt.Sprintf("kanca.Migration: Pro migration numarası %d'den başlar (verilen %d)", ProMigrationBaslangic, n))
	}
	store.RegisterMigration(n, sql)
}

// OzellikAcik lisansta özellik açık mı? Panel başlarken (kontrol motorundan
// önce) bir kez atanır; nil ya da false ise Pro izleyicileri çalışmaz.
var OzellikAcik func(string) bool

func ozellikAcik(ad string) bool {
	f := OzellikAcik
	return f != nil && f(ad)
}

// LisansGerekliMesaj lisans yokken Pro izleyicisinin sonuç mesajı.
const LisansGerekliMesaj = "Pro lisansı gerekli / Pro license required"

// IzleyiciTuru Pro izleyici türü kaydeder: check.Register üstüne lisans
// sarmalayıcı. Lisansta özellik yoksa kontrol yapılmaz, sonuç down.
func IzleyiciTuru(ad, ozellik string, c check.Checker) {
	if ad == "" || c == nil {
		panic("kanca.IzleyiciTuru: ad ve denetleyici zorunlu")
	}
	check.Register(ad, &lisansliChecker{Checker: c, ozellik: ozellik})
}

type lisansliChecker struct {
	check.Checker
	ozellik string
}

func (l *lisansliChecker) Check(ctx context.Context, cfg json.RawMessage) check.Result {
	if !ozellikAcik(l.ozellik) {
		return check.Result{Up: false, Message: LisansGerekliMesaj}
	}
	return l.Checker.Check(ctx, cfg)
}

// CertExpiryEnabled isteğe bağlı arayüzü içteki denetleyiciye iletir
// (uygulamıyorsa varsayılan: açık).
func (l *lisansliChecker) CertExpiryEnabled(cfg json.RawMessage) bool {
	if c, ok := l.Checker.(check.CertExpiryChecker); ok {
		return c.CertExpiryEnabled(cfg)
	}
	return true
}

// AjanKomutuTanim ajan tarafında çalışan Pro komutu.
type AjanKomutuTanim struct {
	Ad       string
	Ozellik  string
	Calistir func(ctx context.Context, girdi json.RawMessage) (json.RawMessage, error)
}

var ajanKomutlari = map[string]AjanKomutuTanim{}

// AjanKomutu ajan tarafı Pro komutu kaydeder (iş listesinde pro_jobs; init içinde).
func AjanKomutu(ad, ozellik string, fn func(ctx context.Context, girdi json.RawMessage) (json.RawMessage, error)) {
	ad = strings.TrimSpace(ad)
	if ad == "" || fn == nil {
		panic("kanca.AjanKomutu: ad ve işlev zorunlu")
	}
	if _, dup := ajanKomutlari[ad]; dup {
		panic(fmt.Sprintf("kanca.AjanKomutu: %q iki kez kaydedildi", ad))
	}
	ajanKomutlari[ad] = AjanKomutuTanim{Ad: ad, Ozellik: ozellik, Calistir: fn}
}

// AjanKomutlari kayıtlı ajan komutları (ada göre).
func AjanKomutlari() map[string]AjanKomutuTanim {
	out := make(map[string]AjanKomutuTanim, len(ajanKomutlari))
	for k, v := range ajanKomutlari {
		out[k] = v
	}
	return out
}

var proDerleme bool

// ProDerleme Pro modülü kendini işaretler (init içinde).
func ProDerleme() { proDerleme = true }

// ProDerlemeMi bu ikili Pro modülüyle mi derlendi?
func ProDerlemeMi() bool { return proDerleme }
