package api

// Lisans uçları (docs/LISANS-TASARIM.md "API"):
//
//	GET    /api/license          (oturum)   şerit ve menü durumu
//	GET    /api/license/detail   (yönetici) + kimlikler, kira, yenileme kayıtları
//	PUT    /api/license          (yönetici) {anahtar}
//	POST   /api/license/lease    (yönetici) {kira} elle çevrimdışı kira
//	POST   /api/license/refresh  (yönetici) kirayı şimdi yenile
//	DELETE /api/license          (yönetici) anahtar + kira silinir
//
// Hata yanıtları {"error","code"}: lisans_gecersiz, cihaz_yok, cihaz (400),
// sunucu reddi kodu (409), sunucu_ulasilamiyor / kira_gecersiz (502), kira_gecersiz (400, lease).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/lisansyonet"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"github.com/kadirsungurlu/bekci/internal/webpush"
	"github.com/kadirsungurlu/bekci/kanca"
	"github.com/kadirsungurlu/bekci/lisans"
)

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/license", s.auth(s.getLicense))
		mux.Handle("GET /api/license/detail", s.admin(s.getLicenseDetail))
		mux.Handle("PUT /api/license", s.admin(s.putLicense))
		mux.Handle("POST /api/license/lease", s.admin(s.postLicenseLease))
		mux.Handle("POST /api/license/refresh", s.admin(s.refreshLicense))
		mux.Handle("DELETE /api/license", s.admin(s.deleteLicense))
	})
}

// SetLisans lisans servisini bağlar: Pro özellik kapısı, kanca.Panel.Bildir ve
// uyarı bildirimi bu servise gider. Sunucu istek almadan önce bir kez çağrılır.
func (s *Server) SetLisans(y *lisansyonet.Yonetici) {
	s.lisans = y
	if y == nil {
		s.SetProOzellik(nil)
		return
	}
	s.SetProOzellik(y.Acik)
	y.SetBildirici(s.lisansUyari)
}

// Lisans bağlı lisans servisi (nil olabilir).
func (s *Server) Lisans() *lisansyonet.Yonetici { return s.lisans }

func zamanMetni(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (s *Server) licenseState() map[string]any {
	var d lisans.Durum
	if s.lisans != nil {
		d = s.lisans.Durum()
	} else {
		d = lisans.Durum{Kaynak: lisans.KaynakYok, Neden: lisans.NedenProDegil}
	}
	oz := d.Ozellikler
	if oz == nil {
		oz = []string{}
	}
	return map[string]any{
		"pro_derleme": lisans.ProDerleme || kanca.ProDerlemeMi(),
		"etkin":       d.Etkin,
		"kaynak":      d.Kaynak,
		"neden":       d.Neden,
		"ozellikler":  oz,
		"tur":         d.Tur,
		"bitis":       zamanMetni(d.Bitis),
		"kalan_gun":   d.KalanGun,
		"uyari":       d.Uyari,
		// yönetici ayrıntısı bunları da kullanır
		"musteri":      d.Musteri,
		"kira_bitis":   zamanMetni(d.KiraBitis),
		"deneme_bitis": zamanMetni(d.DenemeBitis),
	}
}

func (s *Server) getLicense(w http.ResponseWriter, r *http.Request) {
	m := s.licenseState()
	delete(m, "musteri")
	delete(m, "kira_bitis")
	delete(m, "deneme_bitis")
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) licenseDetail() map[string]any {
	m := s.licenseState()
	var d lisansyonet.Detay
	if s.lisans != nil {
		d = s.lisans.Detay()
	}
	m["panel_id"] = d.PanelID
	m["cihaz_id"] = d.CihazID
	m["cihaz_hata"] = d.CihazHata
	m["lisans_kimlik"] = d.LisansKimlik
	m["cevrimdisi"] = d.Cevrimdisi
	m["son_basari"] = zamanMetni(d.SonBasari)
	m["son_deneme"] = zamanMetni(d.SonDeneme)
	m["son_hata"] = d.SonHata
	m["derleme"] = zamanMetni(lisans.Derleme())
	m["sunucu_adresi"] = d.SunucuAdresi
	m["gonderilen_veri"] = lisansyonet.GonderilenVeri
	return m
}

func (s *Server) getLicenseDetail(w http.ResponseWriter, r *http.Request) {
	if !s.lisansHazir(w) {
		return
	}
	writeJSON(w, http.StatusOK, s.licenseDetail())
}

func (s *Server) lisansHazir(w http.ResponseWriter) bool {
	if s.lisans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Lisans servisi hazır değil", "code": "lisans_yok"})
		return false
	}
	return true
}

// lisansHatasi servis hatasını kodlu yanıta çevirir.
func (s *Server) lisansHatasi(w http.ResponseWriter, err error) {
	var h *lisansyonet.Hata
	if errors.As(err, &h) {
		writeJSON(w, h.HTTP, map[string]string{"error": h.Mesaj, "code": h.Kod})
		return
	}
	s.dbError(w, err)
}

func lisansOzet(a *lisans.Anahtar) string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("tür: %s, kimlik: %s", a.Tur, a.Kimlik)
}

func (s *Server) putLicense(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Anahtar string `json:"anahtar"`
		Kira    string `json:"kira"` // isteğe bağlı: çevrimdışı kira (sunucuya gidilmez)
	}
	if !readJSON(w, r, &in) || !s.lisansHazir(w) {
		return
	}
	if err := s.lisans.AnahtarKoy(r.Context(), in.Anahtar, in.Kira); err != nil {
		s.lisansHatasi(w, err)
		return
	}
	a := s.lisans.Anahtar()
	name := ""
	if a != nil {
		name = a.Musteri
	}
	s.audit(r, store.User{}, "license.set", "settings", 0, name, lisansOzet(a))
	s.lisans.UyariDenetle(r.Context())
	writeJSON(w, http.StatusOK, s.licenseDetail())
}

func (s *Server) postLicenseLease(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kira string `json:"kira"`
	}
	if !readJSON(w, r, &in) || !s.lisansHazir(w) {
		return
	}
	if err := s.lisans.KiraKoy(r.Context(), in.Kira); err != nil {
		s.lisansHatasi(w, err)
		return
	}
	d := s.lisans.Durum()
	s.audit(r, store.User{}, "license.lease", "settings", 0, d.Musteri, "geçerlilik: "+zamanMetni(d.KiraBitis))
	s.lisans.UyariDenetle(r.Context())
	writeJSON(w, http.StatusOK, s.licenseDetail())
}

func (s *Server) refreshLicense(w http.ResponseWriter, r *http.Request) {
	if !s.lisansHazir(w) {
		return
	}
	err := s.lisans.Yenile(r.Context())
	if err != nil && !errors.Is(err, lisansyonet.ErrGidilmez) {
		var ret *lisans.RetHatasi
		switch {
		case errors.As(err, &ret):
			kod := ret.Kod
			if kod == "" {
				kod = "ret"
			}
			s.audit(r, store.User{}, "license.refresh", "settings", 0, "", "ret: "+kod)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "Lisans sunucusu reddetti: " + ret.Error(), "code": kod})
		case errors.Is(err, lisans.ErrUlasilamiyor):
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "Lisans sunucusuna ulaşılamıyor", "code": "sunucu_ulasilamiyor"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error(), "code": "kira_gecersiz"})
		}
		s.lisans.UyariDenetle(r.Context())
		return
	}
	if err == nil {
		s.audit(r, store.User{}, "license.refresh", "settings", 0, "", "")
	}
	s.lisans.UyariDenetle(r.Context())
	writeJSON(w, http.StatusOK, s.licenseDetail())
}

func (s *Server) deleteLicense(w http.ResponseWriter, r *http.Request) {
	if !s.lisansHazir(w) {
		return
	}
	a := s.lisans.Anahtar()
	if err := s.lisans.Sil(r.Context()); err != nil {
		s.lisansHatasi(w, err)
		return
	}
	s.audit(r, store.User{}, "license.remove", "settings", 0, "", lisansOzet(a))
	writeJSON(w, http.StatusOK, s.licenseDetail())
}

// Yönetici bildirimi ------------------------------------------------------------

// lisansUyari lisans uyarısını (eşik ya da sunucuya ulaşılamıyor) yöneticilere bildirir.
func (s *Server) lisansUyari(ctx context.Context, uyari string, d lisans.Durum) {
	gun := 0
	if d.KalanGun != nil {
		gun = *d.KalanGun
	}
	metin := func(lang string) (konu, govde string) {
		if uyari == lisans.UyariSunucuUlasilamiyor {
			return i18n.T(lang, "license.warn.unreachable"), i18n.T(lang, "license.warn.unreachable.body")
		}
		return i18n.TN(lang, "license.warn.expiring", gun, gun), i18n.T(lang, "license.warn.expiring.body")
	}
	if err := s.store.AddAudit(ctx, store.AuditEntry{Action: "license.warning", TargetType: "settings", Detail: "uyarı: " + uyari}); err != nil {
		s.log.Error("işlem kaydı yazılamadı", "hata", err)
	}
	s.yoneticiBildir(ctx, metin)
}

// kancaBildir Pro kodunun sistem bildirimi: yöneticilere e-posta + web push (metin olduğu gibi).
func (s *Server) kancaBildir(ctx context.Context, konu, metin string) error {
	s.log.Warn("pro bildirimi", "konu", konu, "metin", metin)
	s.yoneticiBildir(ctx, func(string) (string, string) { return konu, metin })
	return nil
}

// yoneticiBildir etkin yöneticilere kendi dillerinde sistem e-postası (sistem
// e-posta kanalı seçiliyse ve adresi varsa) ve web push gönderir. Gönderim
// arka plandadır; hatalar günlüğe yazılır.
func (s *Server) yoneticiBildir(ctx context.Context, metin func(lang string) (konu, govde string)) {
	users, err := s.store.ListUsers(ctx)
	if err != nil {
		s.log.Error("yönetici listesi okunamadı", "hata", err)
		return
	}
	var admins []store.User
	for _, u := range users {
		if u.Role == store.RoleAdmin && !u.Disabled {
			admins = append(admins, u)
		}
	}
	if len(admins) == 0 {
		return
	}
	mailer, mailOK := s.systemMailer(ctx)
	link := strings.TrimRight(s.BaseURL, "/") + "/#/settings/license"
	go func() {
		bctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		keys, kerr := s.vapid(bctx)
		for _, u := range admins {
			lang := i18n.Or(u.Lang)
			konu, govde := metin(lang)
			if mailOK && u.Email != "" {
				html := notify.RenderMail(lang, i18n.T(lang, "license.warn.status"), konu, []string{govde}, nil, link, i18n.T(lang, "license.warn.open"))
				if err := s.mailer(bctx, mailer.Config, u.Email, konu, konu+"\n\n"+govde+"\n\n"+link+"\n", html); err != nil {
					s.log.Error("lisans e-postası gönderilemedi", "kullanıcı", u.Username, "hata", notify.SanitizeSendError("email", mailer.Config, err))
				}
			}
			if kerr != nil {
				continue
			}
			subs, err := s.store.PushSubscriptionsForUser(bctx, u.ID)
			if err != nil || len(subs) == 0 {
				continue
			}
			payload, _ := json.Marshal(pushPayload{Title: konu, Body: govde, URL: "/#/settings/license", Kind: "license", Tag: "license"})
			s.pushSend(bctx, keys, subs, payload, webpush.Options{TTL: pushTTLOther, Urgency: "normal", Topic: "license"})
		}
	}()
}
