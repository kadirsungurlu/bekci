package lisans

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Önekler.
const (
	OnekAnahtar = "BEKCI-L1"
	OnekKira    = "BEKCI-K1"
)

// Lisans türleri.
const (
	TurAbonelik       = "abonelik"
	TurOmurCevrimici  = "omur_cevrimici"
	TurOmurCevrimdisi = "omur_cevrimdisi"
)

// PaketPro anahtardaki tek geçerli paket adı.
const PaketPro = "pro"

// Özellik bayrakları (docs/PRO-PAKET.md).
const (
	OzellikBetik           = "betik"
	OzellikTopluGuncelleme = "toplu_guncelleme"
	OzellikFiloGuvenlik    = "filo_guvenlik"
	OzellikHostingDenetim  = "hosting_denetim"
	OzellikTopoloji        = "topoloji"
	OzellikMusteri         = "musteri"
	OzellikRapor           = "rapor"
	OzellikMarka           = "marka"
	OzellikSaml            = "saml"
	OzellikNobet           = "nobet"
	OzellikHA              = "ha"
	OzellikCokluPanel      = "coklu_panel"
	OzellikDenetimDisa     = "denetim_disa"
	// OzellikHepsi listede bulunursa tüm özellikler açıktır.
	OzellikHepsi = "*"
)

// Ozellikler bilinen tüm Pro özellikleridir.
var Ozellikler = []string{
	OzellikBetik, OzellikTopluGuncelleme, OzellikFiloGuvenlik, OzellikHostingDenetim, OzellikTopoloji,
	OzellikMusteri, OzellikRapor, OzellikMarka, OzellikSaml, OzellikNobet, OzellikHA, OzellikCokluPanel,
	OzellikDenetimDisa,
}

// Zaman, JSON'da RFC3339 metni olarak yazılan zamandır; sıfır değer "" olarak yazılır ve "" sıfır okunur.
type Zaman struct{ time.Time }

// Z, time.Time'dan Zaman üretir.
func Z(t time.Time) Zaman { return Zaman{t} }

// MarshalJSON sıfırsa "" yazar, değilse UTC RFC3339.
func (z Zaman) MarshalJSON() ([]byte, error) {
	if z.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(z.UTC().Format(time.RFC3339))
}

// UnmarshalJSON "" ve null değerini sıfır kabul eder.
func (z *Zaman) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		z.Time = time.Time{}
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		z.Time = time.Time{}
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("zaman RFC3339 değil: %q", s)
	}
	z.Time = t.UTC()
	return nil
}

// Anahtar müşteriye verilen uzun ömürlü imzalı lisans anahtarıdır (önek BEKCI-L1).
type Anahtar struct {
	V               int      `json:"v"`
	Kimlik          string   `json:"kimlik"`
	Musteri         string   `json:"musteri"`
	Paket           string   `json:"paket"`
	Tur             string   `json:"tur"`
	Bitis           Zaman    `json:"bitis"`
	GuncellemeBitis Zaman    `json:"guncelleme_bitis"`
	Cihaz           string   `json:"cihaz"`
	Ozellikler      []string `json:"ozellikler"`
	Verilis         Zaman    `json:"verilis"`
	YenilemeAdresi  string   `json:"yenileme_adresi"`
}

// Kira lisans sunucusunun panel (+cihaz) için verdiği kısa ömürlü imzalı belgedir (önek BEKCI-K1).
type Kira struct {
	V               int      `json:"v"`
	Lisans          string   `json:"lisans"`
	Panel           string   `json:"panel"`
	Cihaz           string   `json:"cihaz"`
	Tur             string   `json:"tur"`
	Musteri         string   `json:"musteri"`
	LisansBitis     Zaman    `json:"lisans_bitis"`
	GuncellemeBitis Zaman    `json:"guncelleme_bitis"`
	Ozellikler      []string `json:"ozellikler"`
	Verilis         Zaman    `json:"verilis"`
	Gecerlilik      Zaman    `json:"gecerlilik"`
	Cevrimdisi      bool     `json:"cevrimdisi"`
}

// Biçim hataları.
var (
	ErrBicim  = errors.New("lisans: biçim bozuk")
	ErrOnek   = errors.New("lisans: önek yanlış")
	ErrImza   = errors.New("lisans: imza geçersiz")
	ErrSurum  = errors.New("lisans: desteklenmeyen biçim sürümü")
	ErrIcerik = errors.New("lisans: içerik geçersiz")
)

var b64 = base64.RawURLEncoding

// Kodla payload'ı JSON'a çevirip `ÖNEK.<b64url(JSON)>.<b64url(imza)>` metnini üretir.
// İmza `ÖNEK.<b64url(JSON)>` baytları üzerinde imzala ile alınır (gizli anahtar bu pakete girmez).
func Kodla(onek string, payload any, imzala func([]byte) []byte) (string, error) {
	if onek == "" || strings.Contains(onek, ".") {
		return "", fmt.Errorf("%w: önek boş ya da nokta içeriyor", ErrBicim)
	}
	if imzala == nil {
		return "", errors.New("lisans: imzalama fonksiyonu yok")
	}
	j, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	govde := onek + "." + b64.EncodeToString(j)
	imza := imzala([]byte(govde))
	if len(imza) == 0 {
		return "", errors.New("lisans: imza boş")
	}
	return govde + "." + b64.EncodeToString(imza), nil
}

// coz önek ve imzayı denetleyip JSON gövdesini döndürür.
func coz(onek, metin string) ([]byte, error) {
	metin = strings.TrimSpace(metin)
	parca := strings.Split(metin, ".")
	if len(parca) != 3 {
		return nil, ErrBicim
	}
	if parca[0] != onek {
		return nil, ErrOnek
	}
	j, err := b64.DecodeString(parca[1])
	if err != nil || len(j) == 0 {
		return nil, ErrBicim
	}
	imza, err := b64.DecodeString(parca[2])
	if err != nil || len(imza) != ed25519.SignatureSize {
		return nil, ErrImza
	}
	imzali := []byte(parca[0] + "." + parca[1])
	gecerli := false
	for _, k := range AcikAnahtarlar {
		if len(k) == ed25519.PublicKeySize && ed25519.Verify(k, imzali, imza) {
			gecerli = true
			break
		}
	}
	if !gecerli {
		return nil, ErrImza
	}
	return j, nil
}

func jsonCoz(j []byte, hedef any) error {
	d := json.NewDecoder(bytes.NewReader(j)) // bilinmeyen alan kabul (ileri uyum)
	if err := d.Decode(hedef); err != nil {
		return fmt.Errorf("%w: %v", ErrBicim, err)
	}
	return nil
}

func surumDenetle(v int) error {
	if v < 1 {
		return fmt.Errorf("%w: v eksik", ErrSurum)
	}
	if v > 1 {
		return fmt.Errorf("%w: v=%d", ErrSurum, v)
	}
	return nil
}

func icerikHata(f string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrIcerik, fmt.Sprintf(f, a...))
}

// turDenetle tür kurallarını uygular: abonelikte bitiş zorunlu + cihaz boş; ömür türlerinde cihaz zorunlu + bitiş boş.
func turDenetle(tur string, bitis Zaman, bitisAd, cihaz string) error {
	switch tur {
	case TurAbonelik:
		if bitis.IsZero() {
			return icerikHata("abonelikte %s zorunlu", bitisAd)
		}
		if cihaz != "" {
			return icerikHata("abonelik cihaza bağlı olamaz")
		}
	case TurOmurCevrimici, TurOmurCevrimdisi:
		if cihaz == "" {
			return icerikHata("%s türünde cihaz zorunlu", tur)
		}
		if !bitis.IsZero() {
			return icerikHata("%s türünde %s boş olmalı", tur, bitisAd)
		}
	default:
		return icerikHata("bilinmeyen tür %q", tur)
	}
	return nil
}

// AnahtarCoz BEKCI-L1 metnini imza ve içerik denetimiyle çözer.
func AnahtarCoz(metin string) (*Anahtar, error) {
	j, err := coz(OnekAnahtar, metin)
	if err != nil {
		return nil, err
	}
	var a Anahtar
	if err := jsonCoz(j, &a); err != nil {
		return nil, err
	}
	if err := surumDenetle(a.V); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Kimlik) == "" {
		return nil, icerikHata("kimlik boş")
	}
	if a.Paket != PaketPro {
		return nil, icerikHata("paket %q desteklenmiyor", a.Paket)
	}
	if err := turDenetle(a.Tur, a.Bitis, "bitis", a.Cihaz); err != nil {
		return nil, err
	}
	return &a, nil
}

// KiraCoz BEKCI-K1 metnini imza ve içerik denetimiyle çözer. Lisans/panel/cihaz eşleşmesi Degerlendir'dedir.
func KiraCoz(metin string) (*Kira, error) {
	j, err := coz(OnekKira, metin)
	if err != nil {
		return nil, err
	}
	var k Kira
	if err := jsonCoz(j, &k); err != nil {
		return nil, err
	}
	if err := surumDenetle(k.V); err != nil {
		return nil, err
	}
	if strings.TrimSpace(k.Lisans) == "" || strings.TrimSpace(k.Panel) == "" {
		return nil, icerikHata("kirada lisans ya da panel boş")
	}
	if k.Gecerlilik.IsZero() {
		return nil, icerikHata("kirada gecerlilik zorunlu")
	}
	if err := turDenetle(k.Tur, k.LisansBitis, "lisans_bitis", k.Cihaz); err != nil {
		return nil, err
	}
	return &k, nil
}

// Acik özellik açık mı ("*" hepsini açar). Etkin değilse hiçbir özellik açık değildir.
func (d Durum) Acik(ozellik string) bool {
	if !d.Etkin {
		return false
	}
	return slices.Contains(d.Ozellikler, OzellikHepsi) || slices.Contains(d.Ozellikler, ozellik)
}
