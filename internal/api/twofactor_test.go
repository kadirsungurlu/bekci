package api

import (
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/brand"
	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestTOTPVectors(t *testing.T) {
	// RFC 6238 Ek B (SHA1), 8 hanenin son 6'sı.
	secret := []byte("12345678901234567890")
	for ts, want := range map[int64]string{59: "287082", 1111111109: "081804", 1234567890: "005924", 2000000000: "279037"} {
		if got := totpCode(secret, ts/30); got != want {
			t.Errorf("T=%d: %s, %s bekleniyordu", ts, got, want)
		}
	}
	b32secret := b32.EncodeToString(secret)
	now := time.Unix(1234567890, 0)
	for d, ok := range map[time.Duration]bool{0: true, 30 * time.Second: true, -30 * time.Second: true, 61 * time.Second: false} {
		if _, got := totpMatch(b32secret, "005924", now.Add(d)); got != ok {
			t.Errorf("kayma %v: %v, %v bekleniyordu", d, got, ok)
		}
	}
}

func (f *fenv) code(secret string) string {
	raw, err := b32.DecodeString(secret)
	if err != nil {
		f.t.Fatal(err)
	}
	return totpCode(raw, f.clk.now().Unix()/totpPeriod)
}

// newClient çerezsiz yeni istemci.
func (e *env) newClient() *env {
	jar, _ := cookiejar.New(nil)
	return &env{t: e.t, srv: e.srv, client: &http.Client{Jar: jar, Timeout: 10 * time.Second}}
}

type loginResp struct {
	User              *userView `json:"user"`
	TwoFactorRequired bool      `json:"two_factor_required"`
	Challenge         string    `json:"challenge"`
	Code              string    `json:"code"`
	Error             string    `json:"error"`
}

// enable2FA kullanıcı için 2FA'yı açar; sırrı, onaylama kodunu ve kurtarma kodlarını döner.
func (f *fenv) enable2FA(c *env, password string) (secret, used string, recovery []string) {
	f.t.Helper()
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
		QR         string `json:"qr_png"`
	}
	c.mustDo("POST", "/api/auth/2fa/setup", map[string]string{"password": password}, &setup, 200)
	used = f.code(setup.Secret)
	var en struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	c.mustDo("POST", "/api/auth/2fa/enable", map[string]string{"code": used}, &en, 200)
	return setup.Secret, used, en.RecoveryCodes
}

func TestTwoFactorFlow(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	other := admin.loginAs("kadir", "cok-gizli-sifre") // aynı kullanıcının başka cihazı

	// Kurulum: şifre gerekli.
	if code := admin.do("POST", "/api/auth/2fa/setup", map[string]string{"password": "yanlis"}, nil); code != 400 {
		t.Errorf("yanlış şifreyle kurulum: %d", code)
	}
	// Kurulum başlamadan etkinleştirme olmaz.
	if code := admin.do("POST", "/api/auth/2fa/enable", map[string]string{"code": "123456"}, nil); code != 400 {
		t.Errorf("kurulumsuz etkinleştirme: %d", code)
	}
	var setup struct {
		Secret     string `json:"secret"`
		OtpauthURL string `json:"otpauth_url"`
		QR         string `json:"qr_png"`
	}
	admin.mustDo("POST", "/api/auth/2fa/setup", map[string]string{"password": "cok-gizli-sifre"}, &setup, 200)
	if len(setup.Secret) != 32 || !strings.HasPrefix(setup.OtpauthURL, "otpauth://totp/"+brand.Name+":kadir?") ||
		!strings.Contains(setup.OtpauthURL, "secret="+setup.Secret) || !strings.HasPrefix(setup.QR, "data:image/png;base64,") {
		t.Fatalf("kurulum yanıtı: %+v", setup)
	}
	// Kurulum bitmeden giriş hâlâ tek adımlı.
	var lr loginResp
	admin.newClient().mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, &lr, 200)
	if lr.TwoFactorRequired || lr.User == nil {
		t.Fatalf("kurulum tamamlanmadan 2FA istenmemeli: %+v", lr)
	}
	f.resetLimiter()

	good := f.code(setup.Secret)
	wrong := fmt.Sprintf("%06d", (atoi(good)+1)%1000000)
	if code := admin.do("POST", "/api/auth/2fa/enable", map[string]string{"code": wrong}, nil); code != 400 {
		t.Errorf("yanlış kodla etkinleştirme: %d", code)
	}
	var en struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	admin.mustDo("POST", "/api/auth/2fa/enable", map[string]string{"code": good}, &en, 200)
	if len(en.RecoveryCodes) != 10 || len(en.RecoveryCodes[0]) != 11 {
		t.Fatalf("kurtarma kodları: %v", en.RecoveryCodes)
	}
	// Diğer cihazdaki oturum kapandı, bu oturum açık.
	if code := other.do("GET", "/api/monitors", nil, nil); code != 401 {
		t.Errorf("2FA açılınca diğer oturum kapanmalı: %d", code)
	}
	var state struct {
		User userView `json:"user"`
	}
	admin.mustDo("GET", "/api/auth/state", nil, &state, 200)
	if !state.User.TwoFactorEnabled {
		t.Error("auth/state two_factor_enabled göstermeli")
	}
	var users []store.User
	admin.mustDo("GET", "/api/users", nil, &users, 200)
	if !users[0].TwoFactorEnabled {
		t.Error("kullanıcı listesi two_factor_enabled göstermeli")
	}
	// Açıkken yeniden kurulum yok; sır bir daha dönmez.
	if code := admin.do("POST", "/api/auth/2fa/setup", map[string]string{"password": "cok-gizli-sifre"}, nil); code != 409 {
		t.Errorf("açıkken kurulum: %d, 409 bekleniyordu", code)
	}

	login := func(c *env) string {
		t.Helper()
		var lr loginResp
		c.mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, &lr, 200)
		if !lr.TwoFactorRequired || lr.Challenge == "" || lr.User != nil {
			t.Fatalf("2FA istenmeli: %+v", lr)
		}
		// Oturum açılmadı.
		if code := c.do("GET", "/api/monitors", nil, nil); code != 401 {
			t.Fatalf("kod girilmeden oturum açılmamalı: %d", code)
		}
		return lr.Challenge
	}
	second := func(c *env, challenge, code string) (int, loginResp) {
		var lr loginResp
		st := c.do("POST", "/api/auth/login/2fa", map[string]string{"challenge": challenge, "code": code}, &lr)
		return st, lr
	}

	// Etkinleştirmede kullanılan kod tekrar kullanılamaz.
	c := admin.newClient()
	ch := login(c)
	if st, _ := second(c, ch, good); st != 401 {
		t.Errorf("tekrar kullanılan kod: %d, 401 bekleniyordu", st)
	}
	f.clk.advance(30 * time.Second)
	fresh := f.code(setup.Secret)
	if st, lr := second(c, ch, fresh); st != 200 || lr.User == nil || !lr.User.TwoFactorEnabled {
		t.Fatalf("doğru kodla giriş: %d %+v", st, lr)
	}
	if code := c.do("GET", "/api/monitors", nil, nil); code != 200 {
		t.Errorf("2FA sonrası oturum: %d", code)
	}
	// Challenge tek kullanımlık; aynı kod başka challenge'da da geçmez.
	if st, lr := second(c, ch, fresh); st != 401 || lr.Code != "challenge_expired" {
		t.Errorf("kullanılmış challenge: %d %+v", st, lr)
	}
	c2 := admin.newClient()
	ch2 := login(c2)
	if st, _ := second(c2, ch2, fresh); st != 401 {
		t.Errorf("aynı kod ikinci girişte: %d, 401 bekleniyordu", st)
	}
	f.resetLimiter()

	// Süresi dolan challenge.
	ch3 := login(c2)
	f.clk.advance(6 * time.Minute)
	if st, lr := second(c2, ch3, f.code(setup.Secret)); st != 401 || lr.Code != "challenge_expired" {
		t.Errorf("süresi dolan challenge: %d %+v", st, lr)
	}
	// Uydurma challenge.
	if st, lr := second(c2, "uydurma", f.code(setup.Secret)); st != 401 || lr.Code != "challenge_expired" {
		t.Errorf("uydurma challenge: %d %+v", st, lr)
	}

	// Hatalı kodlar sınırlayıcıya sayılır; yeniden giriş yapmak IP sayacını sıfırlamaz.
	f.clk.advance(time.Minute)
	bad := fmt.Sprintf("%06d", (atoi(f.code(setup.Secret))+500000)%1000000)
	ch4 := login(c2)
	for range 4 {
		if st, _ := second(c2, ch4, bad); st != 401 {
			t.Fatalf("hatalı kod: %d", st)
		}
	}
	ch5 := login(c2)
	if st, _ := second(c2, ch5, bad); st != 401 {
		t.Fatalf("5. hatalı kod: %d", st)
	}
	if st, _ := second(c2, ch5, f.code(setup.Secret)); st != 429 {
		t.Errorf("5 hatalı koddan sonra: %d, 429 bekleniyordu", st)
	}
	f.resetLimiter()
	// Challenge başına 5 hata: challenge silinir.
	ch6 := login(c2)
	for range 5 {
		second(c2, ch6, bad)
	}
	f.resetLimiter()
	if st, lr := second(c2, ch6, f.code(setup.Secret)); st != 401 || lr.Code != "challenge_expired" {
		t.Errorf("5 hatadan sonra challenge silinmeli: %d %+v", st, lr)
	}
	f.resetLimiter()

	// Kurtarma kodu: tire/büyük harf fark etmez, tek kullanımlık.
	ch7 := login(c2)
	if st, lr := second(c2, ch7, strings.ToUpper(en.RecoveryCodes[3])); st != 200 || lr.User == nil {
		t.Fatalf("kurtarma koduyla giriş: %d %+v", st, lr)
	}
	c3 := admin.newClient()
	ch8 := login(c3)
	if st, _ := second(c3, ch8, en.RecoveryCodes[3]); st != 401 {
		t.Errorf("kullanılmış kurtarma kodu: %d, 401 bekleniyordu", st)
	}
	var status struct {
		Enabled bool `json:"enabled"`
		Left    int  `json:"recovery_codes_left"`
	}
	admin.mustDo("GET", "/api/auth/2fa", nil, &status, 200)
	if !status.Enabled || status.Left != 9 {
		t.Errorf("2FA durumu: %+v", status)
	}
	f.resetLimiter()

	// Kurtarma kodlarını yenileme: eskiler geçersiz.
	f.clk.advance(30 * time.Second)
	var rc struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	admin.mustDo("POST", "/api/auth/2fa/recovery-codes", map[string]string{"password": "cok-gizli-sifre", "code": f.code(setup.Secret)}, &rc, 200)
	if len(rc.RecoveryCodes) != 10 {
		t.Fatalf("yeni kurtarma kodları: %v", rc)
	}
	ch9 := login(c3)
	if st, _ := second(c3, ch9, en.RecoveryCodes[0]); st != 401 {
		t.Errorf("eski kurtarma kodu yenilemeden sonra: %d", st)
	}
	f.resetLimiter()

	// Kapatma: şifre ve kod gerekli.
	f.clk.advance(30 * time.Second)
	if code := admin.do("POST", "/api/auth/2fa/disable", map[string]string{"password": "cok-gizli-sifre", "code": bad}, nil); code != 400 {
		t.Errorf("yanlış kodla kapatma: %d", code)
	}
	if code := admin.do("POST", "/api/auth/2fa/disable", map[string]string{"password": "yanlis", "code": f.code(setup.Secret)}, nil); code != 400 {
		t.Errorf("yanlış şifreyle kapatma: %d", code)
	}
	admin.mustDo("POST", "/api/auth/2fa/disable", map[string]string{"password": "cok-gizli-sifre", "code": f.code(setup.Secret)}, nil, 200)
	f.resetLimiter()
	var plain loginResp
	admin.newClient().mustDo("POST", "/api/auth/login", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, &plain, 200)
	if plain.TwoFactorRequired || plain.User == nil || plain.User.TwoFactorEnabled {
		t.Errorf("kapatıldıktan sonra giriş: %+v", plain)
	}

	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	seen := map[string]bool{}
	for _, e := range audit {
		seen[e.Action] = true
	}
	for _, a := range []string{"user.2fa_enable", "user.2fa_disable", "login.2fa_fail", "user.2fa_recovery_codes"} {
		if !seen[a] {
			t.Errorf("işlem kaydında %s yok", a)
		}
	}
}

func TestTwoFactorAdminReset(t *testing.T) {
	f := newFeatureEnv(t)
	admin := f.env
	editor, eu := admin.newUser("editor1", store.RoleEditor, nil)
	f.enable2FA(editor, "kalici-sifre-1")
	f.resetLimiter()

	var lr loginResp
	admin.newClient().mustDo("POST", "/api/auth/login", map[string]string{"username": "editor1", "password": "kalici-sifre-1"}, &lr, 200)
	if !lr.TwoFactorRequired {
		t.Fatal("editöre 2FA sorulmalı")
	}
	// Yönetici olmayan sıfırlayamaz; yönetici kendi 2FA'sını buradan sıfırlayamaz.
	if code := editor.do("POST", fmt.Sprintf("/api/users/%d/2fa/reset", eu.ID), nil, nil); code != 403 {
		t.Errorf("editör sıfırlama: %d, 403 bekleniyordu", code)
	}
	if code := admin.do("POST", "/api/users/1/2fa/reset", nil, nil); code != 400 {
		t.Errorf("yönetici kendi 2FA'sı: %d, 400 bekleniyordu", code)
	}
	if code := admin.do("POST", "/api/users/9999/2fa/reset", nil, nil); code != 404 {
		t.Errorf("olmayan kullanıcı: %d", code)
	}
	// Yönetici anahtarıyla da olmaz (gerçek oturum gerekir).
	ak := admin.newKey("y", store.RoleAdmin, 0)
	if code, _, _ := admin.rawReq("POST", fmt.Sprintf("/api/users/%d/2fa/reset", eu.ID), bearer(ak.Secret), nil); code != 401 {
		t.Errorf("anahtarla 2FA sıfırlama: %d, 401 bekleniyordu", code)
	}
	admin.mustDo("POST", fmt.Sprintf("/api/users/%d/2fa/reset", eu.ID), nil, nil, 200)
	lr = loginResp{}
	admin.newClient().mustDo("POST", "/api/auth/login", map[string]string{"username": "editor1", "password": "kalici-sifre-1"}, &lr, 200)
	if lr.TwoFactorRequired || lr.User == nil {
		t.Errorf("sıfırlamadan sonra 2FA sorulmamalı: %+v", lr)
	}
	var audit []store.AuditEntry
	admin.mustDo("GET", "/api/audit", nil, &audit, 200)
	if audit[0].Action != "login.success" || audit[1].Action != "user.2fa_reset" || audit[1].TargetName != "editor1" {
		t.Errorf("işlem kaydı: %+v", audit[:2])
	}
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}
