package api

// OIDC / SSO girişi (E-13). Genel OpenID Connect: issuer keşfi
// (.well-known/openid-configuration), authorization code + PKCE (S256),
// state ve nonce denetimi, id_token imza doğrulaması (go-oidc).
//
//	GET  /api/settings/oidc            ayarlar (yönetici; gizli anahtar maskeli)
//	PUT  /api/settings/oidc            ayarları kaydet (yönetici)
//	POST /api/settings/oidc/test       issuer keşfini dener (yönetici)
//	GET  /api/auth/oidc/start[?next=]  sağlayıcıya yönlendirir
//	GET  /api/auth/oidc/callback       sağlayıcıdan dönüş; oturum açar ve "/"e yönlendirir
//
// Hesap eşleme sırası: (1) daha önce bağlanmış issuer+subject, (2) e-posta
// eşleşmesi (link_email açıksa ve e-posta doğrulanmışsa) → bağ kurulur,
// (3) auto_provision açıksa yeni hesap (rol: claim eşlemesi ya da varsayılan
// rol), yoksa "hesap yok" hatası. Rol claim'i ayarlıysa rol her girişte
// eşlemeye göre güncellenir.
//
// İki adımlı doğrulama: OIDC ile giren kullanıcıda yerel TOTP SORULMAZ;
// ikinci faktör sağlayıcının (Google/Microsoft/Keycloak…) sorumluluğundadır.
// Şifreyle giriş aynı hesapta açıksa orada TOTP yine istenir.
//
// Dış adres: yönlendirme adresi BASE_URL + /api/auth/oidc/callback; BASE_URL
// yoksa isteğin şeması ve sunucu adı kullanılır (ters vekilin
// X-Forwarded-Proto göndermesi gerekir).

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/kadirsungurlu/bekci/internal/notify"
	"github.com/kadirsungurlu/bekci/internal/store"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	oidcSettingsKey = "oidc"
	oidcCookie      = "uptime_oidc"
	oidcStateTTL    = 10 * time.Minute
	oidcHTTPTimeout = 15 * time.Second
	// oidcDiscoveryTimeout ayar kaydı ve "keşfi dene" için issuer keşfinin süresi
	// (yönetici isteği bekler; kısa tutulur).
	oidcDiscoveryTimeout = 10 * time.Second
	oidcDiscoveryMaxBody = 1 << 20
)

// OIDCSettings sağlayıcı ayarları (settings.oidc anahtarında JSON).
type OIDCSettings struct {
	Enabled      bool   `json:"enabled"`
	Name         string `json:"name"` // giriş düğmesindeki ad (boş: "SSO")
	Issuer       string `json:"issuer"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scopes       string `json:"scopes"` // boşlukla ayrılmış; boş: openid profile email
	// Hesap eşleme.
	LinkEmail     bool   `json:"link_email"`     // e-postası eşleşen yerel hesaba bağla
	AutoProvision bool   `json:"auto_provision"` // eşleşmeyen kullanıcı için hesap aç
	DefaultRole   string `json:"default_role"`   // provision edilen hesabın rolü (claim eşleşmezse)
	UsernameClaim string `json:"username_claim"` // boş: preferred_username
	// Rol eşlemesi: claim (ör. groups, roles) içindeki değerler.
	RoleClaim    string `json:"role_claim"`
	AdminValues  string `json:"admin_values"`  // virgülle ayrılmış
	EditorValues string `json:"editor_values"` //
	ViewerValues string `json:"viewer_values"` //
	// LocalLogin kapalıysa giriş ekranında şifre formu gizlenir (bağlantıyla açılır).
	LocalLogin bool `json:"local_login"`
}

var oidcNameRe = regexp.MustCompile(`^[^\x00-\x1f]{0,60}$`)

// Validate ayarları denetler ve varsayılanları doldurur.
func (o *OIDCSettings) Validate() error {
	o.Name, o.Issuer, o.ClientID, o.ClientSecret = strings.TrimSpace(o.Name), strings.TrimRight(strings.TrimSpace(o.Issuer), "/"), strings.TrimSpace(o.ClientID), strings.TrimSpace(o.ClientSecret)
	o.Scopes = strings.Join(strings.Fields(o.Scopes), " ")
	o.UsernameClaim, o.RoleClaim = strings.TrimSpace(o.UsernameClaim), strings.TrimSpace(o.RoleClaim)
	if !oidcNameRe.MatchString(o.Name) {
		return errors.New("SSO adı en fazla 60 karakter olabilir")
	}
	if o.DefaultRole == "" {
		o.DefaultRole = store.RoleViewer
	}
	if store.RoleRank(o.DefaultRole) == 0 {
		return errors.New("Rol yönetici (admin), editör (editor) veya izleyici (viewer) olmalı")
	}
	if o.Scopes == "" {
		o.Scopes = "openid profile email"
	}
	if !strings.Contains(" "+o.Scopes+" ", " openid ") {
		return errors.New("Kapsamlar (scopes) openid içermeli")
	}
	if !o.Enabled {
		return nil
	}
	if err := validIssuer(o.Issuer); err != nil {
		return err
	}
	if o.ClientID == "" {
		return errors.New("Client ID gerekli")
	}
	if o.ClientSecret == "" {
		return errors.New("Client secret gerekli")
	}
	return nil
}

// validIssuer issuer https olmalı; yalnızca yerel/özel adreslerde http (geliştirme) kabul edilir.
func validIssuer(iss string) error {
	u, err := url.Parse(iss)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("Issuer geçerli bir https adresi olmalı (ör. https://accounts.google.com)")
	}
	if u.Scheme == "http" {
		host := u.Hostname()
		ip, perr := netip.ParseAddr(host)
		if host != "localhost" && (perr != nil || !(ip.IsLoopback() || ip.IsPrivate())) {
			return errors.New("Issuer için http yalnızca yerel ağ adreslerinde kullanılabilir; https gerekir")
		}
	}
	return nil
}

// roleFor claim değerlerinden rol: admin > editor > viewer; eşleşme yoksa "".
func (o OIDCSettings) roleFor(values []string) string {
	has := func(list string) bool {
		for _, want := range strings.Split(list, ",") {
			want = strings.TrimSpace(want)
			if want == "" {
				continue
			}
			for _, v := range values {
				if strings.EqualFold(v, want) {
					return true
				}
			}
		}
		return false
	}
	switch {
	case has(o.AdminValues):
		return store.RoleAdmin
	case has(o.EditorValues):
		return store.RoleEditor
	case has(o.ViewerValues):
		return store.RoleViewer
	}
	return ""
}

// masked gizli anahtarı maskeler (API yanıtı).
func (o OIDCSettings) masked() OIDCSettings {
	if o.ClientSecret != "" {
		o.ClientSecret = notify.Mask
	}
	return o
}

// loadOIDC kayıtlı ayarlar (yoksa kapalı varsayılanlar).
func (s *Server) loadOIDC(ctx context.Context) (OIDCSettings, error) {
	o := OIDCSettings{LinkEmail: true, DefaultRole: store.RoleViewer, Scopes: "openid profile email", LocalLogin: true}
	v, ok, err := s.store.GetSetting(ctx, oidcSettingsKey)
	if err != nil || !ok {
		return o, err
	}
	if json.Unmarshal([]byte(v), &o) != nil {
		return OIDCSettings{LinkEmail: true, DefaultRole: store.RoleViewer, Scopes: "openid profile email", LocalLogin: true}, nil
	}
	return o, nil
}

func (s *Server) saveOIDC(ctx context.Context, o OIDCSettings) error {
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	return s.store.SetSettings(ctx, map[string]string{oidcSettingsKey: string(b)})
}

// oidcPublic giriş ekranının gördüğü kısım.
type oidcPublic struct {
	Enabled    bool   `json:"enabled"`
	Name       string `json:"name"`
	LocalLogin bool   `json:"local_login"`
}

func (s *Server) oidcPublic(ctx context.Context) oidcPublic {
	o, err := s.loadOIDC(ctx)
	if err != nil || !o.Enabled {
		return oidcPublic{LocalLogin: true}
	}
	name := o.Name
	if name == "" {
		name = "SSO"
	}
	return oidcPublic{Enabled: true, Name: name, LocalLogin: o.LocalLogin}
}

func init() {
	RegisterRoutes(func(s *Server, mux *http.ServeMux) {
		mux.Handle("GET /api/settings/oidc", s.admin(s.getOIDCSettings))
		mux.Handle("PUT /api/settings/oidc", s.admin(s.putOIDCSettings))
		mux.Handle("POST /api/settings/oidc/test", s.admin(s.testOIDCSettings))
		mux.HandleFunc("GET /api/auth/oidc/start", s.oidcStart)
		mux.HandleFunc("GET /api/auth/oidc/callback", s.oidcCallback)
	})
}

func (s *Server) getOIDCSettings(w http.ResponseWriter, r *http.Request) {
	o, err := s.loadOIDC(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": o.masked(), "redirect_uri": s.oidcRedirectURL(r)})
}

func (s *Server) putOIDCSettings(w http.ResponseWriter, r *http.Request) {
	if userFrom(r).APIKeyName != "" {
		writeError(w, http.StatusForbidden, "Bu işlem API anahtarıyla yapılamaz")
		return
	}
	var in OIDCSettings
	if !readJSON(w, r, &in) {
		return
	}
	old, err := s.loadOIDC(r.Context())
	if err != nil {
		s.dbError(w, err)
		return
	}
	if in.ClientSecret == notify.Mask || in.ClientSecret == "" {
		in.ClientSecret = old.ClientSecret // maskeli / boş: kayıtlı anahtar korunur
	}
	if err := in.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Açık yapılandırma kaydedilirken issuer keşifle doğrulanır: SSO yeni
	// açılıyorsa ya da issuer değiştiyse. Yanlış issuer kaydedilip giriş
	// ekranında çalışmayan bir SSO düğmesi çıkmasın. Kapalı yapılandırma keşif
	// olmadan kaydedilir; aynı issuer'la diğer alanlar değiştirilirken de
	// sağlayıcıya gidilmez (sağlayıcı o an erişilmezse rol eşlemesi düzenlenebilsin).
	if in.Enabled && (!old.Enabled || strings.TrimRight(old.Issuer, "/") != in.Issuer) {
		meta, err := oidcDiscover(r.Context(), in.Issuer)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		in.Issuer = meta.Issuer // sağlayıcının yazımı (sondaki "/" dahil): go-oidc birebir eşleşme ister
	} else if in.Enabled {
		in.Issuer = old.Issuer
	}
	if err := s.saveOIDC(r.Context(), in); err != nil {
		s.dbError(w, err)
		return
	}
	s.oidcProviders.reset()
	detail := "kapalı"
	if in.Enabled {
		detail = "açık; issuer: " + in.Issuer
	}
	s.audit(r, store.User{}, "settings.oidc", "settings", 0, "", detail)
	writeJSON(w, http.StatusOK, map[string]any{"settings": in.masked(), "redirect_uri": s.oidcRedirectURL(r)})
}

// testOIDCSettings issuer keşfini dener (ayarları kaydetmeden; gizli anahtar maskeliyse kayıtlı olan).
func (s *Server) testOIDCSettings(w http.ResponseWriter, r *http.Request) {
	var in OIDCSettings
	if !readJSON(w, r, &in) {
		return
	}
	if err := validIssuer(strings.TrimRight(strings.TrimSpace(in.Issuer), "/")); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	meta, err := oidcDiscover(r.Context(), strings.TrimRight(strings.TrimSpace(in.Issuer), "/"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "issuer": meta.Issuer, "authorization_endpoint": meta.Authorization, "token_endpoint": meta.Token, "scopes_supported": meta.Scopes})
}

// Issuer keşfi -----------------------------------------------------------------------

// oidcMeta keşif belgesinin (/.well-known/openid-configuration) kullanılan alanları.
type oidcMeta struct {
	Issuer        string   `json:"issuer"`
	Authorization string   `json:"authorization_endpoint"`
	Token         string   `json:"token_endpoint"`
	JWKS          string   `json:"jwks_uri"`
	Scopes        []string `json:"scopes_supported"`
}

// oidcDiscover issuer'ın keşif belgesini okur ve doğrular. Hata mesajları
// yöneticiye yöneliktir (Türkçe; writeError isteğin diline çevirir) ve üç
// durumu ayırır: adrese ulaşılamadı, adres bir OIDC sağlayıcısı değil,
// sağlayıcının bildirdiği issuer girilenle uyuşmuyor. Dönen meta.Issuer
// sağlayıcının kendi yazımıdır (sondaki "/" dahil); go-oidc girişte bu değerle
// birebir eşleşme ister, çağıran onu saklamalıdır.
func oidcDiscover(ctx context.Context, issuer string) (oidcMeta, error) {
	ctx, cancel := context.WithTimeout(ctx, oidcDiscoveryTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(issuer, "/")+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcMeta{}, errors.New("Issuer geçerli bir https adresi olmalı (ör. https://accounts.google.com)")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Bekci")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return oidcMeta{}, fmt.Errorf("Issuer adresine ulaşılamadı: %s", oidcNetError(err))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, oidcDiscoveryMaxBody))
	if err != nil {
		return oidcMeta{}, fmt.Errorf("Issuer adresine ulaşılamadı: %s", oidcNetError(err))
	}
	if resp.StatusCode != http.StatusOK {
		return oidcMeta{}, fmt.Errorf("Bu adres bir OpenID Connect sağlayıcısı değil (keşif belgesi %d döndü)", resp.StatusCode)
	}
	var meta oidcMeta
	if json.Unmarshal(body, &meta) != nil || meta.Issuer == "" || meta.Authorization == "" || meta.Token == "" || meta.JWKS == "" {
		return oidcMeta{}, errors.New("Bu adres bir OpenID Connect sağlayıcısı değil (keşif belgesi okunamadı)")
	}
	if strings.TrimRight(meta.Issuer, "/") != strings.TrimRight(issuer, "/") {
		return oidcMeta{}, fmt.Errorf("Issuer uyuşmuyor: sağlayıcı kendini %s olarak tanıtıyor", meta.Issuer)
	}
	return meta, nil
}

// oidcNetError ağ hatasını kısa, adres tekrarı olmayan bir metne indirger.
func oidcNetError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Sprintf("%d sn içinde yanıt gelmedi", int(oidcDiscoveryTimeout.Seconds()))
	}
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		return ue.Err.Error()
	}
	return err.Error()
}

// oidcRedirectURL sağlayıcıya bildirilecek dönüş adresi.
func (s *Server) oidcRedirectURL(r *http.Request) string {
	return s.externalURL(r) + "/api/auth/oidc/callback"
}

// Sağlayıcı önbelleği ---------------------------------------------------------------

type oidcProviders struct {
	mu   sync.Mutex
	iss  string
	prov *oidc.Provider
}

func (c *oidcProviders) reset() {
	c.mu.Lock()
	c.iss, c.prov = "", nil
	c.mu.Unlock()
}

// get issuer için sağlayıcıyı (keşif belgesi + JWKS) döner; önbellekte tutar.
func (c *oidcProviders) get(ctx context.Context, issuer string) (*oidc.Provider, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.prov != nil && c.iss == issuer {
		return c.prov, nil
	}
	ctx, cancel := context.WithTimeout(ctx, oidcHTTPTimeout)
	defer cancel()
	p, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, err
	}
	c.iss, c.prov = issuer, p
	return p, nil
}

// Giriş akışı --------------------------------------------------------------------------

// oidcFail kullanıcıyı giriş ekranına hata koduyla döndürür (arayüz metne çevirir).
func (s *Server) oidcFail(w http.ResponseWriter, r *http.Request, code string, err error) {
	if err != nil {
		s.log.Warn("OIDC girişi başarısız", "neden", code, "hata", err)
	} else {
		s.log.Warn("OIDC girişi başarısız", "neden", code)
	}
	clearOIDCCookie(w, r)
	http.Redirect(w, r, "/#/login?sso_error="+url.QueryEscape(code), http.StatusSeeOther)
}

func clearOIDCCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: "", Path: "/api/auth/oidc", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
}

func (s *Server) oauthConfig(r *http.Request, o OIDCSettings, p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{ClientID: o.ClientID, ClientSecret: o.ClientSecret, Endpoint: p.Endpoint(),
		RedirectURL: s.oidcRedirectURL(r), Scopes: strings.Fields(o.Scopes)}
}

// oidcStart state/nonce/PKCE üretir, çerezle eşler ve sağlayıcıya yönlendirir.
func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	o, err := s.loadOIDC(r.Context())
	if err != nil || !o.Enabled {
		s.oidcFail(w, r, "disabled", err)
		return
	}
	if ok, _ := s.limiter.allowKeys(s.now(), "oidc-ip:"+clientIP(r)); !ok {
		writeError(w, http.StatusTooManyRequests, "Çok fazla istek; biraz sonra tekrar deneyin")
		return
	}
	p, err := s.oidcProviders.get(r.Context(), o.Issuer)
	if err != nil {
		s.oidcFail(w, r, "discovery", err)
		return
	}
	id := randomToken(32)
	st := store.OIDCState{State: randomToken(24), Nonce: randomToken(24), Verifier: oauth2.GenerateVerifier(),
		ExpiresAt: s.now().Add(oidcStateTTL).Unix()}
	if next := r.URL.Query().Get("next"); strings.HasPrefix(next, "/#/") && len(next) < 200 {
		st.Next = next
	}
	if err := s.store.CreateOIDCState(r.Context(), hashToken(id), st, s.now().Unix()); err != nil {
		s.dbError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oidcCookie, Value: id, Path: "/api/auth/oidc", MaxAge: int(oidcStateTTL.Seconds()),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteLaxMode})
	cfg := s.oauthConfig(r, o, p)
	http.Redirect(w, r, cfg.AuthCodeURL(st.State, oidc.Nonce(st.Nonce), oauth2.S256ChallengeOption(st.Verifier)), http.StatusFound)
}

// oidcClaims id_token / userinfo'dan okunan alanlar.
type oidcClaims struct {
	Email             string `json:"email"`
	EmailVerified     *bool  `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := s.now()
	o, err := s.loadOIDC(ctx)
	if err != nil || !o.Enabled {
		s.oidcFail(w, r, "disabled", err)
		return
	}
	c, err := r.Cookie(oidcCookie)
	if err != nil || c.Value == "" {
		s.oidcFail(w, r, "state", nil)
		return
	}
	st, err := s.store.ConsumeOIDCState(ctx, hashToken(c.Value), now.Unix())
	if err != nil {
		s.oidcFail(w, r, "state", nil)
		return
	}
	q := r.URL.Query()
	if subtle.ConstantTimeCompare([]byte(q.Get("state")), []byte(st.State)) != 1 {
		s.oidcFail(w, r, "state", nil)
		return
	}
	if e := q.Get("error"); e != "" {
		s.oidcFail(w, r, "provider", errors.New(e+": "+q.Get("error_description")))
		return
	}
	p, err := s.oidcProviders.get(ctx, o.Issuer)
	if err != nil {
		s.oidcFail(w, r, "discovery", err)
		return
	}
	xctx, cancel := context.WithTimeout(ctx, oidcHTTPTimeout)
	defer cancel()
	tok, err := s.oauthConfig(r, o, p).Exchange(xctx, q.Get("code"), oauth2.VerifierOption(st.Verifier))
	if err != nil {
		s.oidcFail(w, r, "exchange", err)
		return
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		s.oidcFail(w, r, "token", errors.New("id_token yok"))
		return
	}
	idt, err := p.Verifier(&oidc.Config{ClientID: o.ClientID}).Verify(xctx, raw)
	if err != nil {
		s.oidcFail(w, r, "token", err)
		return
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(st.Nonce)) != 1 {
		s.oidcFail(w, r, "nonce", nil)
		return
	}
	var claims oidcClaims
	idt.Claims(&claims)
	var all map[string]any
	idt.Claims(&all)
	// userinfo: id_token'da eksik alanlar (bazı sağlayıcılar e-postayı orada verir).
	if claims.Email == "" || (o.RoleClaim != "" && all[o.RoleClaim] == nil) {
		if ui, err := p.UserInfo(xctx, oauth2.StaticTokenSource(tok)); err == nil {
			var uc oidcClaims
			var um map[string]any
			ui.Claims(&uc)
			ui.Claims(&um)
			if claims.Email == "" {
				claims.Email, claims.EmailVerified = uc.Email, uc.EmailVerified
			}
			if claims.PreferredUsername == "" {
				claims.PreferredUsername = uc.PreferredUsername
			}
			if claims.Name == "" {
				claims.Name = uc.Name
			}
			for k, v := range um {
				if _, ok := all[k]; !ok {
					all[k] = v
				}
			}
		}
	}
	role := ""
	if o.RoleClaim != "" {
		role = o.roleFor(claimValues(all[o.RoleClaim]))
	}
	if o.UsernameClaim != "" {
		if v, ok := all[o.UsernameClaim].(string); ok && v != "" {
			claims.PreferredUsername = v
		}
	}
	u, err := s.oidcResolveUser(ctx, o, idt.Issuer, idt.Subject, claims, role, now)
	if err != nil {
		var fail *oidcError
		if errors.As(err, &fail) {
			s.oidcFail(w, r, fail.code, fail.err)
			return
		}
		s.dbError(w, err)
		return
	}
	if u.Disabled {
		s.oidcFail(w, r, "disabled_account", nil)
		return
	}
	if u.MustChangePassword {
		// Yöneticinin verdiği geçici şifre SSO kullanıcısı için anlamsız; sorulmaz.
		if err := s.store.ClearMustChangePassword(ctx, u.ID); err == nil {
			u.MustChangePassword = false
		}
	}
	clearOIDCCookie(w, r)
	if err := s.startSessionVia(w, r, u, store.SessionViaOIDC); err != nil {
		s.dbError(w, err)
		return
	}
	s.store.TouchLastLogin(ctx, u.ID)
	s.store.TouchOIDC(ctx, idt.Issuer, idt.Subject, now.Unix())
	s.audit(r, u, "login.oidc", "user", u.ID, u.Username, "issuer: "+idt.Issuer)
	next := "/"
	if st.Next != "" {
		next = st.Next
	}
	http.Redirect(w, r, next, http.StatusSeeOther)
}

type oidcError struct {
	code string
	err  error
}

func (e *oidcError) Error() string { return e.code }

// claimValues claim değerini dize listesine çevirir (string, []string, boşluk/virgülle ayrılmış).
func claimValues(v any) []string {
	switch x := v.(type) {
	case string:
		return strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ' ' })
	case []any:
		out := make([]string, 0, len(x))
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return x
	}
	return nil
}

// oidcResolveUser kimliği hesaba eşler (bkz. dosya başı).
func (s *Server) oidcResolveUser(ctx context.Context, o OIDCSettings, issuer, subject string, c oidcClaims, role string, now time.Time) (store.User, error) {
	email, _ := normalizeEmail(c.Email)
	verified := email != "" && (c.EmailVerified == nil || *c.EmailVerified)
	u, err := s.store.OIDCUser(ctx, issuer, subject)
	switch {
	case err == nil:
	case !errors.Is(err, store.ErrNotFound):
		return u, err
	default:
		linked := false
		if o.LinkEmail && verified {
			if u, err = s.store.UserByEmail(ctx, email); err == nil {
				linked = true
			} else if !errors.Is(err, store.ErrNotFound) {
				return u, err
			}
		}
		if !linked {
			if !o.AutoProvision {
				return u, &oidcError{"no_account", nil}
			}
			newRole := role
			if newRole == "" {
				if o.RoleClaim != "" && o.DefaultRole == "" {
					return u, &oidcError{"no_role", nil}
				}
				newRole = o.DefaultRole
			}
			username, err := s.oidcUsername(ctx, o, c, subject)
			if err != nil {
				return u, &oidcError{"provision", err}
			}
			dummy, _ := bcrypt.GenerateFromPassword([]byte(randomToken(24)), bcryptCost)
			nu := store.User{Username: username, DisplayName: strings.TrimSpace(c.Name), Role: newRole, AllMonitors: true}
			if verified {
				nu.Email = email
			}
			if len([]rune(nu.DisplayName)) > 100 {
				nu.DisplayName = string([]rune(nu.DisplayName)[:100])
			}
			if err := s.store.CreateUserExternal(ctx, &nu, string(dummy)); err != nil {
				return u, &oidcError{"provision", err}
			}
			s.log.Info("OIDC ile yeni hesap açıldı", "kullanıcı", nu.Username, "rol", nu.Role, "issuer", issuer)
			s.store.AddAudit(ctx, store.AuditEntry{Action: "user.oidc_provision", TargetType: "user", TargetID: nu.ID, TargetName: nu.Username, Detail: "rol: " + nu.Role})
			u = nu
		} else {
			s.store.AddAudit(ctx, store.AuditEntry{Action: "user.oidc_link", TargetType: "user", TargetID: u.ID, TargetName: u.Username, Detail: "issuer: " + issuer})
		}
		if err := s.store.LinkOIDC(ctx, store.OIDCIdentity{UserID: u.ID, Issuer: issuer, Subject: subject, Email: email, CreatedAt: now.Unix()}); err != nil {
			return u, err
		}
		if u, err = s.store.UserByID(ctx, u.ID); err != nil {
			return u, err
		}
	}
	// Rol eşlemesi her girişte uygulanır (claim ayarlıysa ve eşleşme varsa).
	if role != "" && role != u.Role {
		if err := s.store.SetUserRole(ctx, u.ID, role); err == nil {
			s.store.AddAudit(ctx, store.AuditEntry{Action: "user.oidc_role", TargetType: "user", TargetID: u.ID, TargetName: u.Username, Detail: "rol: " + role})
			u.Role = role
		} else if !errors.Is(err, store.ErrLastAdmin) {
			return u, err
		}
	}
	return u, nil
}

// oidcUsername claim'den geçerli, benzersiz kullanıcı adı üretir.
func (s *Server) oidcUsername(ctx context.Context, o OIDCSettings, c oidcClaims, subject string) (string, error) {
	base := c.PreferredUsername // özel username_claim varsa oidcCallback buraya yazar
	if base == "" && c.Email != "" {
		base, _, _ = strings.Cut(c.Email, "@")
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		case unicode.IsLetter(r):
			b.WriteRune(asciiFold(r))
		}
		if b.Len() >= 28 {
			break
		}
	}
	name := strings.Trim(b.String(), "._-")
	if len(name) < 3 {
		name = "sso-" + strings.ToLower(hashToken(subject)[:8])
	}
	for i := 1; i < 100; i++ {
		cand := name
		if i > 1 {
			cand = fmt.Sprintf("%s-%d", name, i)
		}
		taken, err := s.store.UsernameTaken(ctx, cand)
		if err != nil {
			return "", err
		}
		if !taken {
			return cand, nil
		}
	}
	return "", errors.New("benzersiz kullanıcı adı üretilemedi")
}

// asciiFold Türkçe harfleri ASCII karşılığına indirger; diğer harfler '_' olur.
func asciiFold(r rune) rune {
	switch r {
	case 'ç':
		return 'c'
	case 'ğ':
		return 'g'
	case 'ı', 'i':
		return 'i'
	case 'ö':
		return 'o'
	case 'ş':
		return 's'
	case 'ü':
		return 'u'
	}
	return '_'
}
