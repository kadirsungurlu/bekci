package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func errText(t *testing.T, body []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("yanıt çözülemedi: %s", body)
	}
	s, _ := m["error"].(string)
	return s
}

// TestErrorLocalization API hata mesajları isteğin diline çevrilir: oturum
// kullanıcısının tercihi > X-Uptime-Lang > Accept-Language > tr.
func TestErrorLocalization(t *testing.T) {
	e := newEnv(t)
	hdr := func(kv ...string) map[string]string {
		m := map[string]string{"Content-Type": "application/json", "X-Uptime": "1"}
		for i := 0; i+1 < len(kv); i += 2 {
			m[kv[i]] = kv[i+1]
		}
		return m
	}

	// Varsayılan Türkçe.
	_, _, body := e.rawReq("GET", "/api/monitors", nil, nil)
	if got := errText(t, body); got != "Oturum açmanız gerekiyor" {
		t.Errorf("tr: %q", got)
	}
	// Accept-Language.
	_, _, body = e.rawReq("GET", "/api/monitors", hdr("Accept-Language", "en-US,en;q=0.9"), nil)
	if got := errText(t, body); got != "You need to sign in" {
		t.Errorf("Accept-Language: %q", got)
	}
	// X-Uptime-Lang Accept-Language'den önce gelir.
	_, _, body = e.rawReq("GET", "/api/monitors", hdr("Accept-Language", "en", "X-Uptime-Lang", "tr"), nil)
	if got := errText(t, body); got != "Oturum açmanız gerekiyor" {
		t.Errorf("X-Uptime-Lang: %q", got)
	}
	// Bilinmeyen yol (mux'un kendi 404'ü) ve kalıplı mesaj.
	_, _, body = e.rawReq("GET", "/api/yok-boyle-bir-sey", hdr("X-Uptime-Lang", "en"), nil)
	if got := errText(t, body); got != "Not found" {
		t.Errorf("404: %q", got)
	}
	_, _, body = e.rawReq("POST", "/api/auth/login", hdr("X-Uptime-Lang", "en"), map[string]any{"username": 5})
	if got := errText(t, body); !strings.HasPrefix(got, "Invalid request body: ") {
		t.Errorf("kalıp: %q", got)
	}

	// Kullanıcı tercihi başlıklardan önce gelir.
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var pref struct {
		User userView `json:"user"`
	}
	e.mustDo("PUT", "/api/auth/preferences", map[string]any{"lang": "en"}, &pref, 200)
	if pref.User.Lang != "en" {
		t.Fatalf("tercih kaydedilmedi: %+v", pref.User)
	}
	var st struct {
		User userView `json:"user"`
	}
	e.mustDo("GET", "/api/auth/state", nil, &st, 200)
	if st.User.Lang != "en" {
		t.Errorf("auth/state dil tercihi: %+v", st.User)
	}
	var raw json.RawMessage
	if code := e.do("PUT", "/api/monitors/999999", map[string]any{}, &raw); code == 200 {
		t.Fatalf("beklenmeyen başarı: %s", raw)
	}
	if got := errText(t, raw); strings.ContainsAny(got, "çğıöşüÇĞİÖŞÜ") {
		t.Errorf("kullanıcı tercihi en iken Türkçe hata: %q", got)
	}
	// Geçersiz dil reddedilir (hata da kullanıcının dilinde).
	if code := e.do("PUT", "/api/auth/preferences", map[string]any{"lang": "de"}, &raw); code != 400 || errText(t, raw) != "Language must be tr or en" {
		t.Errorf("geçersiz dil: %d %s", code, raw)
	}
	// "" tarayıcı diline döner.
	e.mustDo("PUT", "/api/auth/preferences", map[string]any{"lang": ""}, &pref, 200)
	if pref.User.Lang != "" {
		t.Errorf("tercih silinmedi: %+v", pref.User)
	}
}

// TestNotifyLangSetting bildirim dili ayarı kaydedilir, doğrulanır; eski
// istemcinin göndermediği alan tr sayılır.
func TestNotifyLangSetting(t *testing.T) {
	e := newEnv(t)
	e.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	var st map[string]any
	e.mustDo("GET", "/api/settings", nil, &st, 200)
	if st["notify_lang"] != "tr" {
		t.Errorf("varsayılan bildirim dili: %v", st["notify_lang"])
	}
	st["notify_lang"] = "en"
	e.mustDo("PUT", "/api/settings", st, nil, 200)
	e.mustDo("GET", "/api/settings", nil, &st, 200)
	if st["notify_lang"] != "en" {
		t.Errorf("kaydedilmedi: %v", st["notify_lang"])
	}
	st["notify_lang"] = "fr"
	e.mustDo("PUT", "/api/settings", st, nil, 400)
	delete(st, "notify_lang")
	e.mustDo("PUT", "/api/settings", st, &st, 200)
	if st["notify_lang"] != "tr" {
		t.Errorf("eksik alan tr olmalı: %v", st["notify_lang"])
	}
}

// TestStatusPageLang sayfa dili kaydedilir, herkese açık yanıtta döner ve
// sayfanın hata mesajları o dilde gelir.
func TestStatusPageLang(t *testing.T) {
	pe := setupPages(t)
	a := pe.seedMonitor("A", "https://a.example.com")
	pageBody := func(extra map[string]any) map[string]any {
		b := map[string]any{"slug": "genel", "title": "Durum", "sections": []map[string]any{{"monitors": []map[string]any{{"id": a.ID}}}}}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	var p pageResp
	pe.mustDo("POST", "/api/status-pages", pageBody(nil), &p, 201)
	if p.Lang != "tr" {
		t.Errorf("yeni sayfa tr olmalı: %q", p.Lang)
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), pageBody(map[string]any{"lang": "de"}), nil, 400)
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), pageBody(map[string]any{"lang": "en", "password": "gizli-sifre"}), &p, 200)
	if p.Lang != "en" {
		t.Fatalf("dil kaydedilmedi: %q", p.Lang)
	}
	// Şifreli sayfa: kilit yanıtı sayfanın dilinde (tarayıcı Türkçe olsa da).
	code, _, body := pe.rawReq("GET", "/api/public/pages/genel", map[string]string{"Accept-Language": "tr"}, nil)
	var locked map[string]any
	json.Unmarshal(body, &locked)
	if code != 401 || locked["lang"] != "en" || locked["error"] != "This page is password protected" {
		t.Errorf("kilitli sayfa: %d %s", code, body)
	}
	code, _, body = pe.rawReq("POST", "/api/public/pages/genel/unlock", map[string]string{"X-Uptime": "1", "Content-Type": "application/json"}, map[string]string{"password": "yanlis"})
	if code != 401 || errText(t, body) != "Incorrect password" {
		t.Errorf("hatalı şifre: %d %s", code, body)
	}
	// Şifre kaldırılınca yanıt dili taşır.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), pageBody(map[string]any{"password": ""}), nil, 200)
	var pub map[string]any
	pe.mustDo("GET", "/api/public/pages/genel", nil, &pub, 200)
	if pub["lang"] != "en" {
		t.Errorf("herkese açık yanıtta dil yok: %v", pub["lang"])
	}
}

// TestBadgeLang rozet metinleri ?lang=en ile İngilizce; varsayılan Türkçe.
func TestBadgeLang(t *testing.T) {
	f := newFeatureEnv(t)
	m := f.push("Site")
	f.page(true, "", m.ID)
	_, _, tr := f.rawReq("GET", fmt.Sprintf("/api/badge/%d/status.svg", m.ID), nil, nil)
	_, _, en := f.rawReq("GET", fmt.Sprintf("/api/badge/%d/status.svg?lang=en", m.ID), nil, nil)
	if !strings.Contains(string(tr), "durum") || !strings.Contains(string(en), "status") || strings.Contains(string(en), "durum") {
		t.Errorf("rozet dili:\ntr: %s\nen: %s", tr, en)
	}
	_, _, up := f.rawReq("GET", fmt.Sprintf("/api/badge/%d/uptime.svg?lang=en&duration=7d", m.ID), nil, nil)
	if !strings.Contains(string(up), "uptime (7d)") {
		t.Errorf("uptime rozeti: %s", up)
	}
}
