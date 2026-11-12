// Package denetim yerleşik (builtin) sunucu denetimlerinin tür kaydıdır
// (iş 743, docs/GUVENLIK-EKLENTI-730.md §C + G6). Panel ve ajan aynı ikilide
// olduğundan tür bir kez kaydedilir: panel şemayı forma ve doğrulamaya,
// ajan Calistir işlevini çalıştırmaya kullanır. Parametre doğrulaması iki
// tarafta da aynı kodla yapılır (ajan panele güvenmez).
//
// Pro türleri kanca.DenetimTuru ile kaydedilir (Ozellik dolu); lisans kapısı
// panelde uygulanır (iş listesine girmez, form 402 alır).
package denetim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/eklenti"
)

// Metin iki dilde (tür kaydı kendi metnini taşır; Pro türleri de).
type Metin struct {
	TR string `json:"tr"`
	EN string `json:"en"`
}

// Parametre türleri.
const (
	PString = "string"
	PInt    = "int"
	PBool   = "bool"
	PSelect = "select"
	// PTarget "host:port"; yalnız loopback, başka ana bilgisayar yalnız
	// kimlik dosyasının hosts listesinde yazılıysa (ajan denetler).
	PTarget = "target"
	// PURL http(s) URL; ana bilgisayar kuralı PTarget ile aynı.
	PURL = "url"
	// PSocket mutlak yerel soket yolu (Unix soketi ya da Windows adlı boru).
	PSocket = "socket"
	// PCredential kimlik.d dosyasının adı (içerik panelden gelmez, G6).
	PCredential = "credential"
)

// Secenek select parametresinin bir değeri.
type Secenek struct {
	Value string `json:"value"`
	Label Metin  `json:"label"`
}

// Param tür parametresinin şeması.
type Param struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Required bool      `json:"required,omitempty"`
	Default  any       `json:"default,omitempty"`
	Options  []Secenek `json:"options,omitempty"`
	Min      *float64  `json:"min,omitempty"`
	Max      *float64  `json:"max,omitempty"`
	MaxLen   int       `json:"-"` // string: 0 = 256
	Label    Metin     `json:"label"`
	Help     Metin     `json:"help"`
}

// Aralik çalışma aralığı sınırları (sn).
type Aralik struct {
	Min     int `json:"min"`
	Max     int `json:"max"`
	Default int `json:"default"`
}

// Girdi Calistir'a verilen doğrulanmış parametreler.
type Girdi struct {
	Params map[string]any
	// Kimlik params'taki credential parametresinin dosyası (yoksa nil).
	Kimlik *Kimlik
}

// String parametre (yoksa "").
func (g Girdi) String(ad string) string { s, _ := g.Params[ad].(string); return s }

// Int parametre (yoksa 0).
func (g Girdi) Int(ad string) int64 {
	switch v := g.Params[ad].(type) {
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

// Bool parametre.
func (g Girdi) Bool(ad string) bool { b, _ := g.Params[ad].(bool); return b }

// Tur bir yerleşik denetim türü.
type Tur struct {
	Kind      string        `json:"kind"`
	Ozellik   string        `json:"-"` // Pro lisans özelliği; "" = ücretsiz
	Platforms []string      `json:"platforms"`
	Interval  Aralik        `json:"interval"`
	Timeout   time.Duration `json:"-"` // 0 = 30 sn
	Label     Metin         `json:"label"`
	Help      Metin         `json:"help"`
	Params    []Param       `json:"params"`
	// Kurallar denetim eklenince panelin kurduğu varsayılan metrik eşik
	// kuralları (durum kuralı her zaman ayrıca kurulur). Kullanıcı sonra
	// düzenler ya da kapatır.
	Kurallar []Kural `json:"rules,omitempty"`
	// Calistir ajan tarafında çalışır; ctx zaman aşımı taşır. Dönen sonuç
	// panele aynen gider (eklenti.Result sınırları ajan tarafında uygulanır).
	Calistir func(ctx context.Context, g Girdi) eklenti.Result `json:"-"`
}

// Kural varsayılan metrik eşik kuralı: Metric > Threshold (Below ise <)
// Minutes dakika sürerse olay.
type Kural struct {
	Metric    string  `json:"metric"`
	Threshold float64 `json:"threshold"`
	Minutes   int     `json:"minutes"`
	Below     bool    `json:"below,omitempty"`
}

// Credential türün kimlik parametresi var mı?
func (t Tur) Credential() bool {
	return slices.ContainsFunc(t.Params, func(p Param) bool { return p.Type == PCredential })
}

// Pro tür lisans ister mi?
func (t Tur) Pro() bool { return t.Ozellik != "" }

// Zaman aşımı (varsayılan 30 sn).
func (t Tur) ZamanAsimi() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return 30 * time.Second
}

var (
	kindRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	paramRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)
	turler  = map[string]Tur{}
)

// Kaydet tür kaydeder (init içinde; hatalı tanım panik).
func Kaydet(t Tur) {
	if !kindRe.MatchString(t.Kind) || t.Calistir == nil {
		panic(fmt.Sprintf("denetim.Kaydet: geçersiz tür %q", t.Kind))
	}
	if _, dup := turler[t.Kind]; dup {
		panic(fmt.Sprintf("denetim.Kaydet: %q iki kez kaydedildi", t.Kind))
	}
	if t.Interval.Min < 30 || t.Interval.Max < t.Interval.Min || t.Interval.Default < t.Interval.Min || t.Interval.Default > t.Interval.Max {
		panic(fmt.Sprintf("denetim.Kaydet: %q aralık sınırları hatalı", t.Kind))
	}
	seen := map[string]bool{}
	cred := 0
	for _, p := range t.Params {
		if !paramRe.MatchString(p.Name) || seen[p.Name] {
			panic(fmt.Sprintf("denetim.Kaydet: %q parametresi %q hatalı", t.Kind, p.Name))
		}
		seen[p.Name] = true
		switch p.Type {
		case PString, PInt, PBool, PTarget, PURL, PSocket:
		case PCredential:
			cred++
		case PSelect:
			if len(p.Options) == 0 {
				panic(fmt.Sprintf("denetim.Kaydet: %q.%s seçeneksiz", t.Kind, p.Name))
			}
		default:
			panic(fmt.Sprintf("denetim.Kaydet: %q.%s türü %q bilinmiyor", t.Kind, p.Name, p.Type))
		}
	}
	if cred > 1 {
		panic(fmt.Sprintf("denetim.Kaydet: %q birden çok kimlik parametresi", t.Kind))
	}
	for _, k := range t.Kurallar {
		if !eklenti.ValidName(k.Metric) || k.Minutes < 1 || k.Minutes > 1440 || math.IsNaN(k.Threshold) || math.IsInf(k.Threshold, 0) {
			panic(fmt.Sprintf("denetim.Kaydet: %q varsayılan kuralı %q hatalı", t.Kind, k.Metric))
		}
	}
	if t.Platforms == nil {
		t.Platforms = []string{}
	}
	turler[t.Kind] = t
}

// Bul türü getirir.
func Bul(kind string) (Tur, bool) { t, ok := turler[kind]; return t, ok }

// Turler kayıtlı türler (ada göre sıralı).
func Turler() []Tur {
	out := make([]Tur, 0, len(turler))
	for _, t := range turler {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// Platformda çalışır mı (goos: runtime.GOOS)?
func (t Tur) Platformda(goos string) bool {
	return len(t.Platforms) == 0 || slices.Contains(t.Platforms, goos)
}

// Hata türleri: ajan sonucunda `error` kodu.
const (
	HataKind       = "kind"       // ajan türü tanımıyor (eski ajan)
	HataParams     = "params"     // parametre doğrulaması
	HataTarget     = "target"     // loopback dışı hedef
	HataCredential = "credential" // kimlik dosyası okunamadı / izin
	HataPlatform   = "platform"
	HataBuiltin    = "builtin" // çalışma hatası (panik, zaman aşımı)
)

// Hata kodlu doğrulama hatası.
type Hata struct {
	Kod string
	Msg string
}

func (e *Hata) Error() string { return e.Msg }

func hata(kod, f string, a ...any) error { return &Hata{Kod: kod, Msg: fmt.Sprintf(f, a...)} }

// MaxParamsBytes params JSON üst sınırı.
const MaxParamsBytes = 8 << 10

// Dogrula params'ı şemaya göre denetler ve normalleştirir: bilinmeyen alan
// red, eksik zorunlu red, varsayılan doldurulur, int int64 olur. Hedef
// kuralı: target/url ana bilgisayarı loopback değilse yalnız kimlik
// parametresi doluysa geçer (asıl liste denetimi HedefIzinli ile ajanda).
func (t Tur) Dogrula(raw json.RawMessage) (map[string]any, error) {
	if len(raw) > MaxParamsBytes {
		return nil, hata(HataParams, "parametreler çok büyük")
	}
	in := map[string]any{}
	if len(raw) > 0 && string(raw) != "null" {
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(&in); err != nil {
			return nil, hata(HataParams, "parametreler JSON nesnesi olmalı")
		}
	}
	known := map[string]Param{}
	for _, p := range t.Params {
		known[p.Name] = p
	}
	for k := range in {
		if _, ok := known[k]; !ok {
			return nil, hata(HataParams, "bilinmeyen parametre: %s", k)
		}
	}
	out := map[string]any{}
	for _, p := range t.Params {
		v, ok := in[p.Name]
		if ok && (v == nil || v == "") {
			ok = false
		}
		if !ok {
			if p.Default != nil {
				out[p.Name] = normDefault(p.Default)
			} else if p.Required {
				return nil, hata(HataParams, "%s zorunlu", p.Name)
			}
			continue
		}
		nv, err := p.dogrula(v)
		if err != nil {
			return nil, err
		}
		out[p.Name] = nv
	}
	cred, _ := credName(t, out)
	for _, p := range t.Params {
		s, _ := out[p.Name].(string)
		if s == "" {
			continue
		}
		switch p.Type {
		case PTarget, PURL:
			host, err := hedefHost(p.Type, s)
			if err != nil {
				return nil, err
			}
			if !Loopback(host) && cred == "" {
				return nil, hata(HataTarget, "%s: %s loopback değil; başka ana bilgisayar yalnız kimlik dosyasının hosts listesinde yazılıysa kullanılabilir", p.Name, host)
			}
		}
	}
	return out, nil
}

func normDefault(v any) any {
	switch x := v.(type) {
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return v
}

func (p Param) dogrula(v any) (any, error) {
	switch p.Type {
	case PBool:
		b, ok := v.(bool)
		if !ok {
			return nil, hata(HataParams, "%s true/false olmalı", p.Name)
		}
		return b, nil
	case PInt:
		var f float64
		switch x := v.(type) {
		case json.Number:
			n, err := x.Float64()
			if err != nil {
				return nil, hata(HataParams, "%s sayı olmalı", p.Name)
			}
			f = n
		case float64:
			f = x
		case int64:
			f = float64(x)
		default:
			return nil, hata(HataParams, "%s sayı olmalı", p.Name)
		}
		if f != math.Trunc(f) || math.Abs(f) > 1<<53 {
			return nil, hata(HataParams, "%s tam sayı olmalı", p.Name)
		}
		if (p.Min != nil && f < *p.Min) || (p.Max != nil && f > *p.Max) {
			return nil, hata(HataParams, "%s sınır dışında", p.Name)
		}
		return int64(f), nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, hata(HataParams, "%s metin olmalı", p.Name)
	}
	max := p.MaxLen
	if max == 0 {
		max = 256
	}
	if len(s) > max || strings.ContainsAny(s, "\x00\r\n") {
		return nil, hata(HataParams, "%s çok uzun ya da geçersiz karakter içeriyor", p.Name)
	}
	switch p.Type {
	case PSelect:
		if !slices.ContainsFunc(p.Options, func(o Secenek) bool { return o.Value == s }) {
			return nil, hata(HataParams, "%s geçersiz seçim", p.Name)
		}
	case PCredential:
		if !KimlikAdiGecerli(s) {
			return nil, hata(HataParams, "%s: kimlik adı küçük harf, rakam, - ve _ (1-64)", p.Name)
		}
	case PSocket:
		if err := soketDogrula(s); err != nil {
			return nil, hata(HataTarget, "%s: %v", p.Name, err)
		}
	case PTarget, PURL:
		if _, err := hedefHost(p.Type, s); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func credName(t Tur, params map[string]any) (string, bool) {
	for _, p := range t.Params {
		if p.Type == PCredential {
			s, _ := params[p.Name].(string)
			return s, true
		}
	}
	return "", false
}

// Hazirla ajan tarafı: parametreleri yeniden doğrular, kimlik dosyasını
// okur, hedef izin listesini uygular. Dönen hata *Hata (kodlu).
func (t Tur) Hazirla(raw json.RawMessage, kimlikDir string) (Girdi, error) {
	params, err := t.Dogrula(raw)
	if err != nil {
		return Girdi{}, err
	}
	g := Girdi{Params: params}
	if ad, _ := credName(t, params); ad != "" {
		k, err := KimlikOku(kimlikDir, ad)
		if err != nil {
			return Girdi{}, err
		}
		g.Kimlik = k
	}
	for _, p := range t.Params {
		s, _ := params[p.Name].(string)
		if s == "" || (p.Type != PTarget && p.Type != PURL) {
			continue
		}
		host, _ := hedefHost(p.Type, s)
		if Loopback(host) {
			continue
		}
		if g.Kimlik == nil || !g.Kimlik.HostIzinli(host) {
			return Girdi{}, hata(HataTarget, "%s: %s loopback değil ve kimlik dosyasının hosts listesinde yok", p.Name, host)
		}
	}
	return g, nil
}

// Yurut ajanda türü çalıştırır (zaman aşımı + panik koruması). code boşsa
// sonuç türündür; doluysa sonuç unknown ve code Hata* kodudur.
func (t Tur) Yurut(ctx context.Context, raw json.RawMessage, kimlikDir, goos string) (res eklenti.Result, code string) {
	fail := func(c, msg string) (eklenti.Result, string) {
		r := []rune(msg)
		if len(r) > eklenti.MaxMessage {
			msg = string(r[:eklenti.MaxMessage])
		}
		return eklenti.Result{Status: eklenti.Unknown, Message: msg, Metrics: []eklenti.Metric{}}, c
	}
	if !t.Platformda(goos) {
		return fail(HataPlatform, "bu denetim türü bu işletim sisteminde çalışmaz: "+goos)
	}
	g, err := t.Hazirla(raw, kimlikDir)
	if err != nil {
		var h *Hata
		if errors.As(err, &h) {
			return fail(h.Kod, h.Msg)
		}
		return fail(HataParams, err.Error())
	}
	ctx, cancel := context.WithTimeout(ctx, t.ZamanAsimi())
	defer cancel()
	type cikti struct {
		r    eklenti.Result
		code string
	}
	done := make(chan cikti, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				r, c := fail(HataBuiltin, fmt.Sprintf("denetim çöktü: %v", p))
				done <- cikti{r, c}
			}
		}()
		done <- cikti{r: t.Calistir(ctx, g)}
	}()
	select {
	case o := <-done:
		if o.code != "" {
			return o.r, o.code
		}
		r := o.r
		if r.Status == "" {
			r.Status = eklenti.Unknown
		}
		if r.Metrics == nil {
			r.Metrics = []eklenti.Metric{}
		}
		return r, ""
	case <-ctx.Done():
		return fail(HataBuiltin, fmt.Sprintf("zaman aşımı (%s)", t.ZamanAsimi()))
	}
}
