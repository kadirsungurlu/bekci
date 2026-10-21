package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func TestSemverCompare(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2",
		"1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.2.0", "1.10.0", "2.0.0"}
	for i := 0; i+1 < len(order); i++ {
		a, _ := parseSemver(order[i])
		b, ok := parseSemver(order[i+1])
		if !ok || a.compare(b) >= 0 || b.compare(a) <= 0 {
			t.Errorf("%s < %s olmalı", order[i], order[i+1])
		}
	}
	for _, eq := range [][2]string{{"v1.5.0", "1.5.0"}, {"1.5.0-postgres", "1.5.0"}, {"1.5.0+abc", "1.5.0"},
		{"1.5.0-rc.1-postgres", "1.5.0-rc.1"}} {
		a, ok1 := parseSemver(eq[0])
		b, ok2 := parseSemver(eq[1])
		if !ok1 || !ok2 || a.compare(b) != 0 {
			t.Errorf("%s == %s olmalı", eq[0], eq[1])
		}
	}
	for _, bad := range []string{"dev", "", "1.5", "1.5.0.1", "01.5.0", "1.5.x", "1.5.0-", "1.5.0-a..b"} {
		if _, ok := parseSemver(bad); ok {
			t.Errorf("%q geçersiz sayılmalı", bad)
		}
	}
}

// fakeReleases GitHub API benzeri sahte sürüm sunucusu.
type fakeReleases struct {
	mu   sync.Mutex
	hits atomic.Int64
	rels []ghRelease
	tags []string
}

func (f *fakeReleases) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/releases":
			json.NewEncoder(w).Encode(f.rels)
		case "/tags":
			out := []map[string]string{}
			for _, t := range f.tags {
				out = append(out, map[string]string{"name": t})
			}
			json.NewEncoder(w).Encode(out)
		default:
			http.NotFound(w, r)
		}
	})
}

func setupUpdate(t *testing.T, version string, envOff bool) (*pageEnv, *fakeReleases) {
	t.Helper()
	f := &fakeReleases{}
	ts := httptest.NewServer(f.handler())
	t.Cleanup(ts.Close)
	pe := &pageEnv{}
	pe.env = newEnv(t, func(s *Server) {
		pe.s = s
		s.version = version
		s.ConfigureUpdateCheck(ts.URL, envOff)
	})
	pe.mustDo("POST", "/api/auth/setup", map[string]string{"username": "kadir", "password": "cok-gizli-sifre"}, nil, 200)
	return pe, f
}

func (pe *pageEnv) check() updateView {
	pe.t.Helper()
	var v updateView
	pe.mustDo("POST", "/api/update/check", nil, &v, 200)
	return v
}

func TestUpdateCheckDismiss(t *testing.T) {
	pe, f := setupUpdate(t, "1.5.0", false)
	f.rels = []ghRelease{{Tag: "v1.5.0", URL: "https://notlar/1.5.0"}, {Tag: "v1.6.0-rc.1", Prerelease: true}, {Tag: "v9.0.0", Draft: true}}
	f.tags = []string{"v1.5.0", "v1.4.0", "v1.6.0-rc.1", "v1.5.0-postgres", "deneme"}
	if v := pe.check(); !v.Enabled || v.Newer || v.Show || v.Latest != "1.5.0" {
		t.Fatalf("yeni sürüm yokken: %+v", v)
	}
	// Yalnız etiketi olan (yayını olmayan) daha yeni sürüm.
	f.tags = append(f.tags, "v1.5.1", "v1.5.1-postgres")
	v := pe.check()
	if !v.Show || v.Latest != "1.5.1" || v.NotesURL != updateRepoWeb+"/releases/tag/v1.5.1" {
		t.Fatalf("1.5.1 görünmeli: %+v", v)
	}
	// Kullanıcı kapatır: o sürüm için görünmez; başka yönetici görür.
	pe.mustDo("POST", "/api/update/dismiss", map[string]string{"version": "1.5.1"}, &v, 200)
	if v.Show || v.Dismissed != "1.5.1" {
		t.Fatalf("kapatınca görünmemeli: %+v", v)
	}
	pe.mustDo("POST", "/api/users", map[string]any{"username": "ikinci", "password": "Gizli-Parola-123!", "role": "admin"}, nil, 201)
	other := pe.loginAs("ikinci", "Gizli-Parola-123!")
	other.mustDo("POST", "/api/auth/password", map[string]string{"current": "Gizli-Parola-123!", "new": "Yeni-Parola-456!"}, nil, 200)
	var ov updateView
	other.mustDo("GET", "/api/update", nil, &ov, 200)
	if !ov.Show {
		t.Fatalf("başka yönetici görmeli: %+v", ov)
	}
	// Daha yeni sürüm (yayın notuyla) -> yeniden görünür.
	f.mu.Lock()
	f.rels = append(f.rels, ghRelease{Tag: "v1.6.0", URL: "https://notlar/1.6.0"})
	f.mu.Unlock()
	if v := pe.check(); !v.Show || v.Latest != "1.6.0" || v.NotesURL != "https://notlar/1.6.0" {
		t.Fatalf("1.6.0 yeniden görünmeli: %+v", v)
	}
	// Yönetici olmayan göremez.
	pe.mustDo("POST", "/api/users", map[string]any{"username": "izleyici", "password": "Gizli-Parola-123!", "role": "viewer"}, nil, 201)
	if code := pe.loginAs("izleyici", "Gizli-Parola-123!").do("GET", "/api/update", nil, nil); code != 403 {
		t.Fatalf("izleyici: %d", code)
	}
	pe.mustDo("POST", "/api/update/dismiss", map[string]string{"version": "eski"}, nil, 400)
}

// Ön sürüm kurulu değilse ön sürümler sayılmaz; kuruluysa sayılır.
func TestUpdateCheckPrerelease(t *testing.T) {
	pe, f := setupUpdate(t, "1.6.0-rc.1", false)
	f.tags = []string{"v1.5.0", "v1.6.0-rc.2"}
	if v := pe.check(); !v.Show || v.Latest != "1.6.0-rc.2" {
		t.Fatalf("rc.2 görünmeli: %+v", v)
	}
	pe2, f2 := setupUpdate(t, "1.5.0-postgres", false)
	f2.tags = []string{"v1.5.0", "v1.6.0-rc.2"}
	if v := pe2.check(); v.Show || v.Latest != "1.5.0" {
		t.Fatalf("kararlı kurulumda ön sürüm sayılmamalı: %+v", v)
	}
}

// Ayardan ya da ortam değişkeniyle kapalıyken dışarıya hiç istek gitmez.
func TestUpdateCheckDisabledNoRequests(t *testing.T) {
	pe, f := setupUpdate(t, "1.5.0", false)
	var st store.AppSettings
	pe.mustDo("GET", "/api/settings", nil, &st, 200)
	off := false
	st.UpdateCheck = &off
	pe.mustDo("PUT", "/api/settings", st, nil, 200)
	f.tags = []string{"v2.0.0"}
	if v := pe.check(); v.Enabled || v.Show || f.hits.Load() != 0 {
		t.Fatalf("ayar kapalı: %+v, istek %d", v, f.hits.Load())
	}
	pe.s.checkUpdate(context.Background())
	if f.hits.Load() != 0 {
		t.Fatal("ayar kapalıyken arka plan denetimi istek attı")
	}

	pe2, f2 := setupUpdate(t, "1.5.0", true)
	f2.tags = []string{"v2.0.0"}
	if v := pe2.check(); v.Enabled || f2.hits.Load() != 0 {
		t.Fatalf("UPDATE_CHECK=off: %+v, istek %d", v, f2.hits.Load())
	}
	// "dev" derlemede de istek yok.
	pe3, f3 := setupUpdate(t, "dev", false)
	if v := pe3.check(); v.Enabled || f3.hits.Load() != 0 {
		t.Fatalf("dev: %+v", v)
	}
	for in, want := range map[string]bool{"off": true, "0": true, "FALSE": true, "": false, "on": false} {
		if UpdateCheckOffEnv(in) != want {
			t.Errorf("UpdateCheckOffEnv(%q)", in)
		}
	}
}

// Sunucu hatasında sessiz: eski bilgi korunur, istek başarılı döner.
func TestUpdateCheckFailureQuiet(t *testing.T) {
	pe, f := setupUpdate(t, "1.5.0", false)
	f.tags = []string{"v1.5.2"}
	if v := pe.check(); v.Latest != "1.5.2" {
		t.Fatalf("%+v", v)
	}
	pe.s.upd.url = "http://127.0.0.1:1"
	if v := pe.check(); v.Latest != "1.5.2" || !v.Show {
		t.Fatalf("hata sonrası eski bilgi korunmalı: %+v", v)
	}
}
