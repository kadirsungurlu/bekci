package api

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/lisansyonet"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/kanca"
	"github.com/kadirsungurlu/bekci/lisans"
)

const proTestYolu = "/api/pro/test-lisans"

// Pro kanca rotası: lisansta filo_guvenlik yokken GET geçer, POST 402.
func init() {
	h := func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, map[string]bool{"ok": true}) }
	kanca.Rota(
		kanca.RotaTanim{Desen: "GET " + proTestYolu, Erisim: kanca.Yonetici, Ozellik: lisans.OzellikFiloGuvenlik, Isleyici: h},
		kanca.RotaTanim{Desen: "POST " + proTestYolu, Erisim: kanca.Yonetici, Ozellik: lisans.OzellikFiloGuvenlik, Isleyici: h},
	)
}

// sahteLisans sahte lisans sunucusu: yanit nil ise isteğe uyan 14 günlük kira verir.
type sahteLisans struct {
	t      *testing.T
	priv   ed25519.PrivateKey
	srv    *httptest.Server
	mu     sync.Mutex
	yanit  func(ist lisans.Istek) (int, any)
	istekl []lisans.Istek
}

func (f *sahteLisans) kira(k lisans.Kira) string {
	f.t.Helper()
	s, err := lisans.Kodla(lisans.OnekKira, k, func(b []byte) []byte { return ed25519.Sign(f.priv, b) })
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *sahteLisans) anahtar(a lisans.Anahtar) string {
	f.t.Helper()
	s, err := lisans.Kodla(lisans.OnekAnahtar, a, func(b []byte) []byte { return ed25519.Sign(f.priv, b) })
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func (f *sahteLisans) ayarla(y func(ist lisans.Istek) (int, any)) {
	f.mu.Lock()
	f.yanit = y
	f.mu.Unlock()
}

func (f *sahteLisans) istekler() []lisans.Istek {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lisans.Istek(nil), f.istekl...)
}

func (f *sahteLisans) olagan(ist lisans.Istek) lisans.Kira {
	now := time.Now().UTC().Truncate(time.Second)
	return lisans.Kira{V: 1, Lisans: ist.Lisans, Panel: ist.Panel, Cihaz: ist.Cihaz, Tur: lisans.TurAbonelik,
		Musteri: "Örnek AŞ", LisansBitis: lisans.Z(now.AddDate(1, 0, 0)), Ozellikler: []string{"*"},
		Verilis: lisans.Z(now), Gecerlilik: lisans.Z(now.Add(lisans.KiraSuresi))}
}

// lisansOrtami test anahtar çifti, Pro derlemesi, cihaz dizini ve sahte lisans sunucusu kurar.
func lisansOrtami(t *testing.T) *sahteLisans {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eskiA, eskiP := lisans.AcikAnahtarlar, lisans.ProDerleme
	lisans.AcikAnahtarlar = []ed25519.PublicKey{pub}
	lisans.ProDerleme = true
	t.Cleanup(func() { lisans.AcikAnahtarlar, lisans.ProDerleme = eskiA, eskiP })
	f := &sahteLisans{t: t, priv: priv}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ist lisans.Istek
		if r.URL.Path != "/v1/kira" || json.NewDecoder(r.Body).Decode(&ist) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.istekl = append(f.istekl, ist)
		y := f.yanit
		f.mu.Unlock()
		if y == nil {
			writeJSON(w, http.StatusOK, map[string]string{"kira": f.kira(f.olagan(ist))})
			return
		}
		kod, govde := y(ist)
		writeJSON(w, kod, govde)
	}))
	t.Cleanup(f.srv.Close)
	t.Setenv("LISANS_URL", f.srv.URL)
	cihazDizinKur(t, "uuid-A", "mid-A")
	return f
}

// cihazDizinKur BEKCI_CIHAZ_DIZIN'i verilen kimliklerle geçici dizine çevirir; cihaz kimliğini döner.
func cihazDizinKur(t *testing.T, uuid, mid string) string {
	t.Helper()
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "product_uuid"), []byte(uuid+"\n"), 0o600)
	os.WriteFile(filepath.Join(d, "machine-id"), []byte(mid+"\n"), 0o600)
	t.Setenv("BEKCI_CIHAZ_DIZIN", d)
	c, err := lisans.CihazKimligi()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// lisansPanel lisans servisli yönetici oturumu; hazirla servis kurulduktan sonra (Yukle'den önce) DB'yi düzenler.
func lisansPanel(t *testing.T, hazirla func(st *store.Store)) (*env, *Server) {
	t.Helper()
	e, srv := setupAdminSrv(t)
	y := lisansyonet.Yeni(srv.store, srv.log, "test")
	ctx := context.Background()
	if err := y.Hazirla(ctx); err != nil {
		t.Fatal(err)
	}
	if hazirla != nil {
		hazirla(srv.store)
		if err := y.Yukle(ctx); err != nil {
			t.Fatal(err)
		}
	}
	y.Cihaz() // cihaz kimliği bu "makinede" bir kez okunur
	srv.SetLisans(y)
	return e, srv
}

type lisansYanit struct {
	ProDerleme bool     `json:"pro_derleme"`
	Etkin      bool     `json:"etkin"`
	Kaynak     string   `json:"kaynak"`
	Neden      string   `json:"neden"`
	Ozellikler []string `json:"ozellikler"`
	Tur        string   `json:"tur"`
	Bitis      string   `json:"bitis"`
	KalanGun   *int     `json:"kalan_gun"`
	Uyari      string   `json:"uyari"`
	PanelID    string   `json:"panel_id"`
	CihazID    string   `json:"cihaz_id"`
	KiraBitis  string   `json:"kira_bitis"`
	SonHata    string   `json:"son_hata"`
}

func (e *env) lisans() lisansYanit {
	e.t.Helper()
	var l lisansYanit
	e.mustDo("GET", "/api/license", nil, &l, 200)
	return l
}

func (e *env) lisansDetay() lisansYanit {
	e.t.Helper()
	var l lisansYanit
	e.mustDo("GET", "/api/license/detail", nil, &l, 200)
	return l
}

func (e *env) hataKodu(method, path string, body any, want int) string {
	e.t.Helper()
	var out struct {
		Code string `json:"code"`
	}
	if got := e.do(method, path, body, &out); got != want {
		e.t.Fatalf("%s %s: %d, %d bekleniyordu (code=%s)", method, path, got, want, out.Code)
	}
	return out.Code
}

func abonelik(f *sahteLisans, kimlik string, bitis time.Time) string {
	now := time.Now().UTC().Truncate(time.Second)
	return f.anahtar(lisans.Anahtar{V: 1, Kimlik: kimlik, Musteri: "Örnek AŞ", Paket: lisans.PaketPro,
		Tur: lisans.TurAbonelik, Bitis: lisans.Z(bitis), Ozellikler: []string{"*"}, Verilis: lisans.Z(now.AddDate(-1, 0, 0))})
}

// a) Pro derlemesi, anahtar yok, kurulum 40 gün önce: deneme bitti, kilitli
// menü yalnız okur. b) Abonelik anahtarı girilince kira alınır, kilit açılır.
func TestLicenseProDenemeVeAbonelik(t *testing.T) {
	f := lisansOrtami(t)
	e, srv := lisansPanel(t, func(st *store.Store) {
		st.LisansYaz(context.Background(), map[string]string{store.LisansKurulum: store.LisansZaman(time.Now().AddDate(0, 0, -40))})
	})
	l := e.lisans()
	if !l.ProDerleme || l.Etkin || l.Neden != lisans.NedenDenemeBitti {
		t.Fatalf("deneme bitmiş olmalı: %+v", l)
	}
	if got := e.hataKodu("POST", proTestYolu, nil, 402); got != "pro_lisans" {
		t.Fatalf("kanca POST: %s", got)
	}
	e.mustDo("GET", proTestYolu, nil, nil, 200)
	if kanca.OzellikAcik != nil && srv.ProAcik(lisans.OzellikFiloGuvenlik) {
		t.Fatal("lisanssız özellik açık")
	}

	// b) Abonelik anahtarı: sahte sunucu kira verir.
	var d lisansYanit
	e.mustDo("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-ABONE", time.Now().AddDate(1, 0, 0))}, &d, 200)
	if !d.Etkin || d.Kaynak != lisans.KaynakLisans || len(d.Ozellikler) != 1 || d.Ozellikler[0] != "*" || d.Bitis == "" || d.KiraBitis == "" {
		t.Fatalf("lisans etkin olmalı: %+v", d)
	}
	ist := f.istekler()
	if len(ist) != 1 || ist[0].Lisans != "L-ABONE" || ist[0].Panel != d.PanelID || ist[0].Cihaz != "" || ist[0].Surum != "test" {
		t.Fatalf("lisans sunucusu isteği: %+v (panel %s)", ist, d.PanelID)
	}
	e.mustDo("POST", proTestYolu, nil, nil, 200)
	if !srv.ProAcik(lisans.OzellikFiloGuvenlik) {
		t.Fatal("lisansla özellik açılmalı")
	}
	// Yenileme de kira alır; işlem kaydı yazılır.
	e.mustDo("POST", "/api/license/refresh", map[string]any{}, nil, 200)
	if len(f.istekler()) != 2 {
		t.Fatalf("yenileme isteği yok: %d", len(f.istekler()))
	}
	entries, _ := srv.store.ListAudit(context.Background(), 0, 50)
	var acts []string
	for _, a := range entries {
		acts = append(acts, a.Action)
	}
	if !strings.Contains(strings.Join(acts, ","), "license.set") || !strings.Contains(strings.Join(acts, ","), "license.refresh") {
		t.Fatalf("işlem kaydı: %v", acts)
	}

	// Yetki: izleyici okur, değiştiremez.
	viewer, _ := e.newUser("izleyici", store.RoleViewer, nil)
	if l := viewer.lisans(); !l.Etkin {
		t.Fatalf("izleyici durumu: %+v", l)
	}
	for _, c := range []struct{ m, p string }{{"PUT", "/api/license"}, {"GET", "/api/license/detail"}, {"POST", "/api/license/refresh"}, {"DELETE", "/api/license"}, {"POST", "/api/license/lease"}} {
		if got := viewer.do(c.m, c.p, map[string]string{"anahtar": "x", "kira": "x"}, nil); got != 403 {
			t.Errorf("izleyici %s %s: %d", c.m, c.p, got)
		}
	}

	// Silinince deneme bitmiş duruma döner.
	e.mustDo("DELETE", "/api/license", nil, nil, 200)
	if l := e.lisans(); l.Etkin || l.Neden != lisans.NedenDenemeBitti {
		t.Fatalf("silindikten sonra: %+v", l)
	}
	if got := e.hataKodu("PUT", "/api/license", map[string]string{"anahtar": "BEKCI-L1.bozuk.imza"}, 400); got != "lisans_gecersiz" {
		t.Fatalf("bozuk anahtar: %s", got)
	}
}

// c) Süresi geçmiş lisans ve kira.
func TestLicenseSuresiGecmis(t *testing.T) {
	f := lisansOrtami(t)
	e, _ := lisansPanel(t, nil)
	if l := e.lisans(); !l.Etkin || l.Kaynak != lisans.KaynakDeneme {
		t.Fatalf("ilk açılışta deneme: %+v", l)
	}

	// Süresi geçmiş anahtar: lisans sunucusu reddeder, hiçbir şey kaydedilmez.
	f.ayarla(func(lisans.Istek) (int, any) { return 403, map[string]string{"kod": "iptal", "mesaj": "süresi doldu"} })
	if got := e.hataKodu("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-ESKI", time.Now().AddDate(0, 0, -3))}, 409); got != "iptal" {
		t.Fatalf("ret kodu: %s", got)
	}
	if l := e.lisans(); l.Kaynak != lisans.KaynakDeneme {
		t.Fatalf("ret sonrası kaydedilmemeli: %+v", l)
	}

	// Sunucu kira verse bile lisans_bitis geçmişse etkin değil.
	f.ayarla(func(ist lisans.Istek) (int, any) {
		k := f.olagan(ist)
		k.LisansBitis = lisans.Z(time.Now().UTC().Add(-time.Hour).Truncate(time.Second))
		return 200, map[string]string{"kira": f.kira(k)}
	})
	var d lisansYanit
	e.mustDo("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-ESKI", time.Now().AddDate(0, 0, -3))}, &d, 200)
	if d.Etkin || d.Neden != lisans.NedenSuresiDoldu || d.Uyari != lisans.UyariSuresiDoldu {
		t.Fatalf("süresi dolmuş: %+v", d)
	}

	// Geçerliliği geçmiş (elle yüklenen) kira: kira_bitti.
	k := f.olagan(lisans.Istek{Lisans: "L-ESKI", Panel: d.PanelID})
	k.Cevrimdisi = true
	k.Gecerlilik = lisans.Z(time.Now().UTC().Add(-time.Minute).Truncate(time.Second))
	e.mustDo("POST", "/api/license/lease", map[string]string{"kira": f.kira(k)}, &d, 200)
	if d.Etkin || d.Neden != lisans.NedenKiraBitti {
		t.Fatalf("kira bitmiş: %+v", d)
	}
	// Çevrimdışı kirada yenileme sunucuya gitmez.
	n := len(f.istekler())
	e.mustDo("POST", "/api/license/refresh", map[string]any{}, nil, 200)
	if len(f.istekler()) != n {
		t.Fatal("çevrimdışı kirada sunucuya gidilmemeli")
	}
}

// d) Yenilemede kesin ret kirayı siler; ulaşılamazsa kira sürer, uyarı verilir.
func TestLicenseYenilemeRetVeUlasilamaz(t *testing.T) {
	f := lisansOrtami(t)
	e, srv := lisansPanel(t, nil)
	var d lisansYanit
	e.mustDo("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-D", time.Now().AddDate(1, 0, 0))}, &d, 200)
	if !d.Etkin {
		t.Fatalf("etkin olmalı: %+v", d)
	}

	// Ulaşılamaz: son başarı 37 saat önce, kira sürer.
	f.ayarla(func(lisans.Istek) (int, any) { return 503, map[string]string{} })
	srv.store.LisansYaz(context.Background(), map[string]string{store.LisansSonBasari: store.LisansZaman(time.Now().Add(-37 * time.Hour))})
	srv.lisans.Yukle(context.Background())
	if got := e.hataKodu("POST", "/api/license/refresh", map[string]any{}, 502); got != "sunucu_ulasilamiyor" {
		t.Fatalf("ulaşılamaz: %s", got)
	}
	d = e.lisansDetay()
	if !d.Etkin || d.Uyari != lisans.UyariSunucuUlasilamiyor || d.SonHata == "" {
		t.Fatalf("kira sürmeli, uyarı verilmeli: %+v", d)
	}
	uyarilar := func() int {
		entries, _ := srv.store.ListAudit(context.Background(), 0, 100)
		n := 0
		for _, a := range entries {
			if a.Action == "license.warning" {
				n++
			}
		}
		return n
	}
	if uyarilar() != 1 {
		t.Fatalf("uyarı işlem kaydı: %d", uyarilar())
	}
	// Aynı uyarı ikinci kez bildirilmez.
	e.hataKodu("POST", "/api/license/refresh", map[string]any{}, 502)
	if uyarilar() != 1 {
		t.Fatalf("uyarı bir kez bildirilmeli: %d", uyarilar())
	}

	// Kesin ret (iptal): kira silinir, Pro kapanır.
	f.ayarla(func(lisans.Istek) (int, any) { return 403, map[string]string{"kod": "iptal"} })
	if got := e.hataKodu("POST", "/api/license/refresh", map[string]any{}, 409); got != "iptal" {
		t.Fatalf("iptal: %s", got)
	}
	if l := e.lisans(); l.Etkin || l.Neden != lisans.NedenKiraYok {
		t.Fatalf("iptal sonrası: %+v", l)
	}
	if srv.ProAcik(lisans.OzellikFiloGuvenlik) {
		t.Fatal("iptal sonrası özellik kapalı olmalı")
	}
}

// e) Ömür boyu çevrimdışı anahtar cihaza bağlıdır.
func TestLicenseOmurCevrimdisiCihaz(t *testing.T) {
	f := lisansOrtami(t)
	cihazA := cihazDizinKur(t, "uuid-A", "mid-A")
	anahtar := f.anahtar(lisans.Anahtar{V: 1, Kimlik: "L-OMUR", Musteri: "Kapalı Ağ AŞ", Paket: lisans.PaketPro,
		Tur: lisans.TurOmurCevrimdisi, Cihaz: cihazA, Ozellikler: []string{lisans.OzellikFiloGuvenlik},
		Verilis: lisans.Z(time.Now().UTC().Truncate(time.Second))})

	e, _ := lisansPanel(t, nil)
	var d lisansYanit
	e.mustDo("PUT", "/api/license", map[string]string{"anahtar": anahtar}, &d, 200)
	if !d.Etkin || d.Tur != lisans.TurOmurCevrimdisi || d.CihazID != cihazA {
		t.Fatalf("doğru cihazda etkin olmalı: %+v", d)
	}
	if len(f.istekler()) != 0 {
		t.Fatal("çevrimdışı anahtarda sunucuya gidilmemeli")
	}
	e.mustDo("POST", proTestYolu, nil, nil, 200)

	// Başka cihaz (farklı product_uuid): PUT 400 cihaz.
	cihazDizinKur(t, "uuid-B", "mid-A")
	e2, _ := lisansPanel(t, nil)
	if got := e2.hataKodu("PUT", "/api/license", map[string]string{"anahtar": anahtar}, 400); got != "cihaz" {
		t.Fatalf("başka cihaz: %s", got)
	}

	// Yedekten başka VM'e dönülmüş panel: saklı anahtar var, cihaz farklı.
	e3, _ := lisansPanel(t, func(st *store.Store) {
		st.LisansYaz(context.Background(), map[string]string{store.LisansAnahtar: anahtar})
	})
	if l := e3.lisans(); l.Etkin || l.Neden != lisans.NedenCihaz {
		t.Fatalf("başka VM'de kapalı olmalı: %+v", l)
	}
	if got := e3.hataKodu("POST", proTestYolu, nil, 402); got != "pro_lisans" {
		t.Fatalf("başka VM kanca: %s", got)
	}
}

// f) Saat geri alınmış: en ileri zaman gelecekte.
func TestLicenseSaatGeri(t *testing.T) {
	lisansOrtami(t)
	e, srv := lisansPanel(t, func(st *store.Store) {
		st.LisansYaz(context.Background(), map[string]string{store.LisansEnIleri: store.LisansZaman(time.Now().Add(2 * time.Hour))})
	})
	if l := e.lisans(); l.Etkin || l.Neden != lisans.NedenSaatGeri || l.Uyari != lisans.UyariSaatGeri {
		t.Fatalf("saat geri: %+v", l)
	}
	// En ileri zaman geri yazılmaz.
	if err := srv.lisans.EnIleriYaz(context.Background()); err != nil {
		t.Fatal(err)
	}
	if l := e.lisans(); l.Neden != lisans.NedenSaatGeri {
		t.Fatalf("en ileri geri yazıldı: %+v", l)
	}
}

// g) Başka panel kimliğine verilmiş kira yüklenemez.
func TestLicenseKiraBaskaPanel(t *testing.T) {
	f := lisansOrtami(t)
	e, _ := lisansPanel(t, nil)
	// Anahtar yokken kira yüklenemez.
	k := f.olagan(lisans.Istek{Lisans: "L-G", Panel: "baska-panel"})
	k.Cevrimdisi = true
	if got := e.hataKodu("POST", "/api/license/lease", map[string]string{"kira": f.kira(k)}, 400); got != "kira_gecersiz" {
		t.Fatalf("anahtarsız kira: %s", got)
	}
	e.mustDo("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-G", time.Now().AddDate(1, 0, 0))}, nil, 200)
	if got := e.hataKodu("POST", "/api/license/lease", map[string]string{"kira": f.kira(k)}, 400); got != "kira_gecersiz" {
		t.Fatalf("başka panel kirası: %s", got)
	}
	if got := e.hataKodu("POST", "/api/license/lease", map[string]string{"kira": "BEKCI-K1.x.y"}, 400); got != "kira_gecersiz" {
		t.Fatalf("bozuk kira: %s", got)
	}
	// Ulaşılamayan sunucuda PUT 502, hiçbir şey kaydedilmez.
	f.ayarla(func(lisans.Istek) (int, any) { return 500, map[string]string{} })
	if got := e.hataKodu("PUT", "/api/license", map[string]string{"anahtar": abonelik(f, "L-YENI", time.Now().AddDate(1, 0, 0))}, 502); got != "sunucu_ulasilamiyor" {
		t.Fatalf("ulaşılamaz PUT: %s", got)
	}
	if d := e.lisansDetay(); !d.Etkin {
		t.Fatalf("eski lisans sürmeli: %+v", d)
	}
}
