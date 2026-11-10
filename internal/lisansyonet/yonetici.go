// Package lisansyonet panelin lisans durumunu yönetir (docs/LISANS-TASARIM.md):
// saklı anahtar/kirayı çözer, lisans.Degerlendir ile durumu verir, kirayı
// lisans sunucusundan yeniler, en ileri zamanı tutar ve uyarı bildirimini
// bir kez tetikler. Katman: api -> lisansyonet -> store.
package lisansyonet

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/kanca"
	"github.com/kadirsungurlu/bekci/lisans"
)

const (
	yenilemeAraligi = 24 * time.Hour
	hataAraligi     = time.Hour
	enIleriAraligi  = 10 * time.Minute
)

// GonderilenVeri lisans sunucusuna giden alanlar (Ayarlar > Lisans ekranında yazılır).
var GonderilenVeri = []string{"lisans", "panel", "cihaz", "surum", "sunucu_sayisi"}

// Hata API'nin kodlu hata yanıtı (HTTP durum + code).
type Hata struct {
	HTTP  int
	Kod   string
	Mesaj string
}

func (h *Hata) Error() string { return h.Mesaj }

func hata(httpKod int, kod, mesaj string) *Hata { return &Hata{HTTP: httpKod, Kod: kod, Mesaj: mesaj} }

// Bildirici yöneticilere lisans uyarısı gönderir (uyari: lisans.Uyari* kodu).
type Bildirici func(ctx context.Context, uyari string, d lisans.Durum)

// Yonetici panelin lisans servisi. Sıfır değeri kullanılamaz: Yeni ile kurulur.
type Yonetici struct {
	st    *store.Store
	log   *slog.Logger
	surum string

	// Simdi saat (testler değiştirir).
	Simdi func() time.Time
	// HTTP lisans sunucusu istemcisi (nil: varsayılan).
	HTTP *http.Client
	// CihazOku cihaz kimliği kaynağı (varsayılan lisans.CihazKimligi; bir kez okunur).
	CihazOku func() (string, error)
	// SunucuSayisi istekte giden sunucu sayısı (varsayılan: store'daki sunucu ajanları).
	SunucuSayisi func(ctx context.Context) int

	cihazOnce sync.Once
	cihazID   string
	cihazHata error

	bildirMu sync.Mutex
	bildir   Bildirici

	// islemMu PUT/lease/refresh/delete ve döngü yenilemesini sıraya koyar.
	islemMu sync.Mutex

	mu      sync.RWMutex
	ayar    store.LisansAyar
	anahtar *lisans.Anahtar
	kira    *lisans.Kira
}

// Yeni servisi kurar; Hazirla çağrılmadan Durum "anahtar yok" sayar.
func Yeni(st *store.Store, log *slog.Logger, surum string) *Yonetici {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	y := &Yonetici{st: st, log: log, surum: surum, Simdi: time.Now, CihazOku: lisans.CihazKimligi}
	y.SunucuSayisi = func(ctx context.Context) int {
		n, err := st.CountProbes(ctx, store.ProbeKindServer)
		if err != nil {
			return 0
		}
		return n
	}
	return y
}

// SetBildirici uyarı bildirimini bağlar (api: e-posta + web push + işlem kaydı).
func (y *Yonetici) SetBildirici(b Bildirici) {
	y.bildirMu.Lock()
	y.bildir = b
	y.bildirMu.Unlock()
}

// Hazirla panel kimliğini/kurulum zamanını oluşturur ve saklı durumu yükler.
func (y *Yonetici) Hazirla(ctx context.Context) error {
	if _, err := y.st.LisansHazirla(ctx, y.Simdi()); err != nil {
		return err
	}
	return y.Yukle(ctx)
}

// Yukle lisans ayarlarını veritabanından yeniden okur (yedekten dönüş, testler).
func (y *Yonetici) Yukle(ctx context.Context) error {
	a, err := y.st.LisansOku(ctx)
	if err != nil {
		return err
	}
	var an *lisans.Anahtar
	if a.Anahtar != "" {
		if an, err = lisans.AnahtarCoz(a.Anahtar); err != nil {
			y.log.Error("saklı lisans anahtarı çözülemedi", "hata", err)
			an = nil
		}
	}
	var k *lisans.Kira
	if a.Kira != "" {
		if k, err = lisans.KiraCoz(a.Kira); err != nil {
			y.log.Error("saklı lisans kirası çözülemedi", "hata", err)
			k = nil
		}
	}
	y.mu.Lock()
	y.ayar, y.anahtar, y.kira = a, an, k
	y.mu.Unlock()
	return nil
}

// Cihaz cihaz kimliği ve okuma hatası (bir kez okunur, önbellekte).
func (y *Yonetici) Cihaz() (string, error) {
	y.cihazOnce.Do(func() {
		if y.CihazOku != nil {
			y.cihazID, y.cihazHata = y.CihazOku()
		} else {
			y.cihazHata = errors.New("cihaz kimliği kaynağı yok")
		}
	})
	return y.cihazID, y.cihazHata
}

func proDerleme() bool { return lisans.ProDerleme || kanca.ProDerlemeMi() }

// Durum anlık lisans değerlendirmesi.
func (y *Yonetici) Durum() lisans.Durum {
	y.mu.RLock()
	a, an, k := y.ayar, y.anahtar, y.kira
	y.mu.RUnlock()
	return y.degerlendir(a, an, k)
}

// degerlendir verilen (saklı ya da kaydedilmek istenen) anahtar ve kirayla durum.
func (y *Yonetici) degerlendir(a store.LisansAyar, an *lisans.Anahtar, k *lisans.Kira) lisans.Durum {
	g := lisans.Girdi{
		Anahtar: an, Kira: k, PanelID: a.PanelID, Simdi: y.Simdi(), EnIleri: a.EnIleri,
		Derleme: lisans.Derleme(), Kurulum: a.Kurulum, SonBasari: a.SonBasari, ProDerleme: proDerleme(),
	}
	if an != nil && an.Cihaz != "" {
		g.CihazID, g.CihazHata = y.Cihaz()
	}
	return lisans.Degerlendir(g)
}

// Acik lisansta özellik açık mı (api.SetProOzellik, kanca.OzellikAcik).
func (y *Yonetici) Acik(ozellik string) bool {
	if y == nil {
		return false
	}
	return y.Durum().Acik(ozellik)
}

// Detay Ayarlar > Lisans ekranının ek alanları.
type Detay struct {
	PanelID      string
	CihazID      string
	CihazHata    string
	LisansKimlik string
	Cevrimdisi   bool
	SonBasari    time.Time
	SonDeneme    time.Time
	SonHata      string
	SunucuAdresi string
	Kira         bool
}

// Detay yönetici görünümü için saklı bilgiler.
func (y *Yonetici) Detay() Detay {
	y.mu.RLock()
	a, an, k := y.ayar, y.anahtar, y.kira
	y.mu.RUnlock()
	d := Detay{PanelID: a.PanelID, SonBasari: a.SonBasari, SonDeneme: a.SonDeneme, SonHata: a.SonHata,
		SunucuAdresi: lisans.Adres(an), Kira: k != nil}
	d.CihazID, _ = y.Cihaz()
	if _, err := y.Cihaz(); err != nil {
		d.CihazHata = err.Error()
	}
	if an != nil {
		d.LisansKimlik = an.Kimlik
	}
	if k != nil {
		d.Cevrimdisi = k.Cevrimdisi
	}
	return d
}

// Anahtar saklı (çözülmüş) anahtar; yoksa nil.
func (y *Yonetici) Anahtar() *lisans.Anahtar {
	y.mu.RLock()
	defer y.mu.RUnlock()
	return y.anahtar
}

// sunucuyaGidilir bu anahtar/kira ile lisans sunucusuna bağlanılır mı.
func sunucuyaGidilir(an *lisans.Anahtar, k *lisans.Kira) bool {
	if an == nil || an.Tur == lisans.TurOmurCevrimdisi {
		return false
	}
	return k == nil || !k.Cevrimdisi
}

func (y *Yonetici) istemci(an *lisans.Anahtar) *lisans.Istemci {
	return &lisans.Istemci{Adres: lisans.Adres(an), HTTP: y.HTTP}
}

// kiraAl anahtar için sunucudan kira ister ve doğrular.
func (y *Yonetici) kiraAl(ctx context.Context, an *lisans.Anahtar, panel string) (string, *lisans.Kira, error) {
	ist := lisans.Istek{Lisans: an.Kimlik, Panel: panel, Surum: y.surum}
	if an.Cihaz != "" {
		c, err := y.Cihaz()
		if err != nil {
			return "", nil, fmt.Errorf("cihaz kimliği okunamadı: %w", err)
		}
		ist.Cihaz = c
	}
	if y.SunucuSayisi != nil {
		ist.SunucuSayisi = y.SunucuSayisi(ctx)
	}
	metin, err := y.istemci(an).KiraIste(ctx, ist)
	if err != nil {
		return "", nil, err
	}
	k, err := lisans.KiraCoz(metin)
	if err != nil {
		return "", nil, fmt.Errorf("lisans sunucusunun kirası geçersiz: %w", err)
	}
	if k.Lisans != an.Kimlik || k.Panel != panel || k.Cihaz != an.Cihaz {
		return "", nil, errors.New("lisans sunucusunun kirası bu lisans/panel için değil")
	}
	return metin, k, nil
}

// AnahtarKoy PUT /api/license: anahtarı doğrular, gerekirse ilk kirayı alır ve
// ikisini birlikte saklar (başarısızlıkta hiçbir şey kaydedilmez).
//
// kiraMetin doluysa (ağdan yalıtılmış kurulum: anahtar + elle üretilmiş
// çevrimdışı kira birlikte) sunucuya gidilmez. Kaydetmeden önce yeni anahtarla
// durum değerlendirilir; Pro'yu açmayacak anahtar (süresi dolmuş, güncelleme
// bitişi geçmiş) kaydedilmez, çalışan lisansın yerine geçmez.
func (y *Yonetici) AnahtarKoy(ctx context.Context, metin, kiraMetin string) error {
	metin = strings.TrimSpace(metin)
	kiraMetin = strings.TrimSpace(kiraMetin)
	an, err := lisans.AnahtarCoz(metin)
	if err != nil {
		return hata(http.StatusBadRequest, "lisans_gecersiz", "Lisans anahtarı geçersiz")
	}
	if an.Cihaz != "" {
		c, err := y.Cihaz()
		if err != nil || c == "" {
			return hata(http.StatusBadRequest, "cihaz_yok", "Cihaz kimliği okunamadı; cihaza bağlı lisans etkinleştirilemez")
		}
		if c != an.Cihaz {
			return hata(http.StatusBadRequest, "cihaz", "Lisans başka bir cihaza bağlı")
		}
	}
	y.islemMu.Lock()
	defer y.islemMu.Unlock()
	y.mu.RLock()
	panel := y.ayar.PanelID
	y.mu.RUnlock()
	simdi := y.Simdi()
	kv := map[string]string{store.LisansAnahtar: metin}
	sil := []string{store.LisansSonHata, store.LisansUyariSeviye}
	var yeniKira *lisans.Kira
	if an.Tur == lisans.TurOmurCevrimdisi {
		sil = append(sil, store.LisansKira, store.LisansSonBasari, store.LisansSonDeneme)
	} else if kiraMetin != "" {
		k, err := lisans.KiraCoz(kiraMetin)
		if err != nil || k.Lisans != an.Kimlik || k.Panel != panel || k.Cihaz != an.Cihaz {
			return hata(http.StatusBadRequest, "kira_gecersiz", "Kira bu panelin lisansı için değil")
		}
		yeniKira = k
		kv[store.LisansKira] = kiraMetin
		sil = append(sil, store.LisansSonBasari, store.LisansSonDeneme)
	} else {
		kiraMetin, k, err := y.kiraAl(ctx, an, panel)
		yeniKira = k
		if err != nil {
			var ret *lisans.RetHatasi
			switch {
			case errors.As(err, &ret):
				kod := ret.Kod
				if kod == "" {
					kod = "ret"
				}
				return hata(http.StatusConflict, kod, "Lisans sunucusu reddetti: "+ret.Error())
			case errors.Is(err, lisans.ErrUlasilamiyor):
				return hata(http.StatusBadGateway, "sunucu_ulasilamiyor", "Lisans sunucusuna ulaşılamıyor")
			default:
				return hata(http.StatusBadGateway, "kira_gecersiz", err.Error())
			}
		}
		kv[store.LisansKira] = kiraMetin
		kv[store.LisansSonBasari] = store.LisansZaman(simdi)
		kv[store.LisansSonDeneme] = store.LisansZaman(simdi)
	}
	y.mu.RLock()
	a := y.ayar
	y.mu.RUnlock()
	if d := y.degerlendir(a, an, yeniKira); !d.Etkin {
		return hata(http.StatusBadRequest, d.Neden, "Bu anahtar Pro'yu açmıyor: "+d.Neden)
	}
	if err := y.st.LisansYaz(ctx, kv); err != nil {
		return err
	}
	if err := y.st.LisansSil(ctx, sil...); err != nil {
		return err
	}
	return y.Yukle(ctx)
}

// KiraKoy POST /api/license/lease: elle (çevrimdışı) kira; saklı anahtar ve panelle eşleşmeli.
func (y *Yonetici) KiraKoy(ctx context.Context, metin string) error {
	metin = strings.TrimSpace(metin)
	k, err := lisans.KiraCoz(metin)
	if err != nil {
		return hata(http.StatusBadRequest, "kira_gecersiz", "Kira geçersiz")
	}
	y.islemMu.Lock()
	defer y.islemMu.Unlock()
	y.mu.RLock()
	an, panel := y.anahtar, y.ayar.PanelID
	y.mu.RUnlock()
	if an == nil || k.Lisans != an.Kimlik || k.Panel != panel || k.Cihaz != an.Cihaz {
		return hata(http.StatusBadRequest, "kira_gecersiz", "Kira bu panelin lisansı için değil")
	}
	if err := y.st.LisansYaz(ctx, map[string]string{store.LisansKira: metin}); err != nil {
		return err
	}
	if err := y.st.LisansSil(ctx, store.LisansSonHata, store.LisansUyariSeviye); err != nil {
		return err
	}
	return y.Yukle(ctx)
}

// Sil DELETE /api/license: anahtar ve kira (ve yenileme kayıtları) silinir.
func (y *Yonetici) Sil(ctx context.Context) error {
	y.islemMu.Lock()
	defer y.islemMu.Unlock()
	if err := y.st.LisansSil(ctx, store.LisansAnahtar, store.LisansKira, store.LisansSonBasari,
		store.LisansSonDeneme, store.LisansSonHata, store.LisansUyariSeviye); err != nil {
		return err
	}
	return y.Yukle(ctx)
}

// ErrGidilmez yenileme gerekmez (anahtar yok, çevrimdışı anahtar ya da kira).
var ErrGidilmez = errors.New("lisans sunucusuna bağlanılmaz")

// Yenile kirayı lisans sunucusundan yeniler. Kesin retde (iptal|bilinmeyen|cihaz)
// kira silinir; ağ hatasında elde kira sürer, son_hata yazılır.
func (y *Yonetici) Yenile(ctx context.Context) error {
	y.islemMu.Lock()
	defer y.islemMu.Unlock()
	y.mu.RLock()
	an, k, panel := y.anahtar, y.kira, y.ayar.PanelID
	y.mu.RUnlock()
	if !sunucuyaGidilir(an, k) {
		return ErrGidilmez
	}
	simdi := y.Simdi()
	kiraMetin, _, err := y.kiraAl(ctx, an, panel)
	if err != nil {
		kv := map[string]string{store.LisansSonDeneme: store.LisansZaman(simdi), store.LisansSonHata: err.Error()}
		if werr := y.st.LisansYaz(ctx, kv); werr != nil {
			y.log.Error("lisans durumu yazılamadı", "hata", werr)
		}
		if lisans.KesinRet(err) {
			y.log.Warn("lisans sunucusu kesin reddetti; kira silindi", "hata", err)
			if werr := y.st.LisansSil(ctx, store.LisansKira); werr != nil {
				y.log.Error("lisans kirası silinemedi", "hata", werr)
			}
		}
		if lerr := y.Yukle(ctx); lerr != nil {
			y.log.Error("lisans ayarları okunamadı", "hata", lerr)
		}
		return err
	}
	if err := y.st.LisansYaz(ctx, map[string]string{store.LisansKira: kiraMetin,
		store.LisansSonBasari: store.LisansZaman(simdi), store.LisansSonDeneme: store.LisansZaman(simdi)}); err != nil {
		return err
	}
	if err := y.st.LisansSil(ctx, store.LisansSonHata); err != nil {
		return err
	}
	return y.Yukle(ctx)
}

// EnIleriYaz en ileri görülen zamanı ileri yazar (saat geri alma denetimi).
func (y *Yonetici) EnIleriYaz(ctx context.Context) error {
	t, err := y.st.LisansEnIleriYaz(ctx, y.Simdi())
	if err != nil {
		return err
	}
	y.mu.Lock()
	y.ayar.EnIleri = t
	y.mu.Unlock()
	return nil
}

// bildirilen uyarı seviyeleri: bitis_14 < bitis_7 < bitis_1 (aynı ya da daha
// hafif eşik tekrar bildirilmez); sunucu_ulasilamiyor ayrı tutulur.
var bitisSira = map[string]int{lisans.UyariBitis14: 1, lisans.UyariBitis7: 2, lisans.UyariBitis1: 3}

// UyariDenetle durumdaki uyarıyı ilk görüldüğünde bir kez bildirir
// (lisans.uyari_seviye: bildirilen kodlar, virgülle).
func (y *Yonetici) UyariDenetle(ctx context.Context) {
	d := y.Durum()
	y.mu.RLock()
	eski := y.ayar.UyariSeviye
	y.mu.RUnlock()
	var kodlar []string
	for _, k := range strings.Split(eski, ",") {
		if k != "" {
			kodlar = append(kodlar, k)
		}
	}
	yeni := slices.Clone(kodlar)
	bildirilecek := ""
	switch {
	case d.Uyari == lisans.UyariSunucuUlasilamiyor:
		if !slices.Contains(yeni, d.Uyari) {
			yeni = append(yeni, d.Uyari)
			bildirilecek = d.Uyari
		}
	case bitisSira[d.Uyari] > 0:
		yeni = slices.DeleteFunc(yeni, func(k string) bool { return k == lisans.UyariSunucuUlasilamiyor })
		enYuksek := 0
		for _, k := range yeni {
			enYuksek = max(enYuksek, bitisSira[k])
		}
		if bitisSira[d.Uyari] > enYuksek {
			yeni = slices.DeleteFunc(yeni, func(k string) bool { return bitisSira[k] > 0 })
			yeni = append(yeni, d.Uyari)
			bildirilecek = d.Uyari
		}
	case d.Etkin && d.Uyari == "":
		yeni = nil // sağlıklı (yenilendi/uzatıldı): sonraki eşikler yeniden bildirilir
	}
	if strings.Join(yeni, ",") != eski {
		var err error
		if len(yeni) == 0 {
			err = y.st.LisansSil(ctx, store.LisansUyariSeviye)
		} else {
			err = y.st.LisansYaz(ctx, map[string]string{store.LisansUyariSeviye: strings.Join(yeni, ",")})
		}
		if err != nil {
			y.log.Error("lisans uyarı seviyesi yazılamadı", "hata", err)
			return
		}
		y.mu.Lock()
		y.ayar.UyariSeviye = strings.Join(yeni, ",")
		y.mu.Unlock()
	}
	if bildirilecek == "" {
		return
	}
	y.bildirMu.Lock()
	b := y.bildir
	y.bildirMu.Unlock()
	y.log.Warn("lisans uyarısı", "uyari", bildirilecek)
	if b != nil {
		b(ctx, bildirilecek, d)
	}
}

// Calistir yenileme döngüsü: başlangıçta ve 24 saatte bir (hata sonrası 1
// saatte bir); 10 dakikada bir en ileri zaman ve uyarı denetimi. ctx bitince döner.
func (y *Yonetici) Calistir(ctx context.Context) {
	sonraki := y.dongu(ctx)
	yenileT := time.NewTimer(sonraki)
	defer yenileT.Stop()
	tik := time.NewTicker(enIleriAraligi)
	defer tik.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tik.C:
			if err := y.EnIleriYaz(ctx); err != nil {
				y.log.Error("lisans en ileri zamanı yazılamadı", "hata", err)
			}
			y.UyariDenetle(ctx)
		case <-yenileT.C:
			yenileT.Reset(y.dongu(ctx))
		}
	}
}

// dongu bir yenileme turu; sonraki tura kalan süreyi döner.
func (y *Yonetici) dongu(ctx context.Context) time.Duration {
	if err := y.EnIleriYaz(ctx); err != nil {
		y.log.Error("lisans en ileri zamanı yazılamadı", "hata", err)
	}
	ara := yenilemeAraligi
	if err := y.Yenile(ctx); err != nil && !errors.Is(err, ErrGidilmez) {
		y.log.Warn("lisans kirası yenilenemedi", "hata", err)
		ara = hataAraligi
	}
	y.UyariDenetle(ctx)
	return ara
}
