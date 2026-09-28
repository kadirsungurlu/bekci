package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

// Arayüz dosyaları: eksik dosya HTML ile değil 404 ile döner, manifest ve servis
// çalışanı doğru tür ve önbellek başlıklarıyla sunulur.
func TestServeStatic(t *testing.T) {
	s := &Server{static: fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>Uptime</title>")},
		"sw.js":                {Data: []byte("self.addEventListener('fetch', () => {});")},
		"manifest.webmanifest": {Data: []byte(`{"name":"Uptime"}`)},
		"icons/icon-192.png":   {Data: []byte("\x89PNG\r\n\x1a\n")},
		"assets/app-abc123.js": {Data: []byte("console.log(1)")},
	}}

	get := func(p string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		s.serveStatic(w, httptest.NewRequest(http.MethodGet, p, nil))
		return w
	}

	cases := []struct {
		path, ctype, cache string
		code               int
	}{
		{"/", "text/html", "no-cache", 200},
		{"/durum/acme", "text/html", "no-cache", 200}, // uzantısız yol: tek sayfalık uygulama
		{"/manifest.webmanifest", "application/manifest+json", "no-cache", 200},
		{"/sw.js", "text/javascript", "no-cache", 200},
		{"/icons/icon-192.png", "image/png", "no-cache", 200},
		{"/assets/app-abc123.js", "text/javascript", "immutable", 200},
		{"/assets/app-eski.js", "", "", 404},
		{"/assets/eski", "", "", 404},
		{"/icons/yok.png", "", "", 404},
		{"/favicon.ico", "", "", 404},
	}
	for _, c := range cases {
		w := get(c.path)
		if w.Code != c.code {
			t.Errorf("%s: durum %d, %d bekleniyordu", c.path, w.Code, c.code)
			continue
		}
		ct := w.Header().Get("Content-Type")
		if c.code == 404 {
			if strings.Contains(ct, "text/html") || strings.Contains(w.Body.String(), "<title>Uptime") {
				t.Errorf("%s: eksik dosya için arayüz HTML'i döndü (%s)", c.path, ct)
			}
			continue
		}
		if !strings.HasPrefix(ct, c.ctype) {
			t.Errorf("%s: Content-Type %q, %q bekleniyordu", c.path, ct, c.ctype)
		}
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, c.cache) {
			t.Errorf("%s: Cache-Control %q, %q içermeliydi", c.path, cc, c.cache)
		}
	}

	w := httptest.NewRecorder()
	s.serveStatic(w, httptest.NewRequest(http.MethodPost, "/sw.js", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: durum %d, 405 bekleniyordu", w.Code)
	}
}
