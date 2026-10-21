package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/kadirsungurlu/bekci/internal/store"
)

func (pe *pageEnv) domainPage(slug, domain string, published bool) store.StatusPage {
	pe.t.Helper()
	var p store.StatusPage
	pe.mustDo("POST", "/api/status-pages", map[string]any{"slug": slug, "title": slug, "published": published,
		"custom_domain": domain, "sections": []any{}}, &p, 201)
	return p
}

func (pe *pageEnv) ask(domain string) int {
	pe.t.Helper()
	return pe.anon().do("GET", "/api/tls/ask?domain="+url.QueryEscape(domain), nil, nil)
}

// Ask ucu yalnız yayındaki bir sayfaya tanımlı alan adını (ve uygulamanın
// kendi adresini) onaylar; harf büyüklüğü, sondaki nokta ve punycode eşdeğer.
func TestTLSAsk(t *testing.T) {
	pe := setupPages(t)
	p := pe.domainPage("acik", "Durum.Ornek.com.", true)
	if p.CustomDomain != "durum.ornek.com" {
		t.Fatalf("alan adı biçimi: %q", p.CustomDomain)
	}
	pe.domainPage("kapali", "kapali.ornek.com", false)
	u := pe.domainPage("unicode", "ödeme.ornek.com", true)
	if u.CustomDomain != "xn--deme-4qa.ornek.com" {
		t.Fatalf("unicode alan adı punycode'a çevrilmedi: %q", u.CustomDomain)
	}
	for d, want := range map[string]int{
		"durum.ornek.com":        200,
		"DURUM.Ornek.COM.":       200,
		"xn--deme-4qa.ornek.com": 200,
		"ödeme.ornek.com":        200,
		"ÖDEME.ornek.com.":       200,
		"uptime.test":            200, // uygulamanın kendi adresi (BASE_URL)
		"kapali.ornek.com":       404, // yayında değil
		"yok.ornek.com":          404,
		"ornek.com":              404,
		"":                       404,
		"durum.ornek.com:443":    404,
		"a b.ornek.com":          404,
		"..":                     404,
	} {
		if got := pe.ask(d); got != want {
			t.Errorf("ask %q: %d, %d bekleniyordu", d, got, want)
		}
	}

	// Yayından kaldırılınca ve alan adı silinince onay geri çekilir.
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", p.ID), map[string]any{"slug": "acik", "title": "acik",
		"published": false, "custom_domain": "durum.ornek.com", "sections": []any{}}, nil, 200)
	if got := pe.ask("durum.ornek.com"); got != 404 {
		t.Fatalf("yayından kalkan sayfa: %d", got)
	}
	pe.mustDo("PUT", fmt.Sprintf("/api/status-pages/%d", u.ID), map[string]any{"slug": "unicode", "title": "unicode",
		"published": true, "custom_domain": "", "sections": []any{}}, nil, 200)
	if got := pe.ask("xn--deme-4qa.ornek.com"); got != 404 {
		t.Fatalf("alan adı silinen sayfa: %d", got)
	}
	// Özel alan adının kendisinden çağrılamaz (yönetim dışı uç değil).
	pe.domainPage("baska", "baska.ornek.com", true)
	code, _, _ := pe.anon().sendHost("GET", "/api/tls/ask?domain=baska.ornek.com", "baska.ornek.com")
	if code != 404 {
		t.Fatalf("özel alan adından ask: %d", code)
	}
}

func TestNormalizeDomain(t *testing.T) {
	for in, want := range map[string]string{
		" Durum.Ornek.Com. ":    "durum.ornek.com",
		"bücher.example":        "xn--bcher-kva.example",
		"xn--bcher-kva.example": "xn--bcher-kva.example",
		"http://x.com":          "",
		"localhost":             "",
		"-a.com":                "",
	} {
		got, ok := normalizeDomain(in)
		if (want == "") == ok || (ok && got != want) {
			t.Errorf("normalizeDomain(%q) = %q, %v; %q bekleniyordu", in, got, ok, want)
		}
	}
}

// Panel alan adı durumu: DNS bekleniyor -> sertifika bekleniyor -> etkin.
func TestPageDomainStatus(t *testing.T) {
	pe := setupPages(t)
	p := pe.domainPage("durum", "durum.ornek.com", true)
	dns := map[string][]string{"uptime.test": {"203.0.113.5"}, "durum.ornek.com": {"198.51.100.9"}}
	probe := func(context.Context, string) (string, error) { return "", errors.New("tls: sertifika yok") }
	oldL, oldP := lookupHost, domainProbe
	t.Cleanup(func() { lookupHost, domainProbe = oldL, oldP })
	lookupHost = func(_ context.Context, h string) ([]string, error) {
		if ips, ok := dns[h]; ok {
			return ips, nil
		}
		return nil, errors.New("yok")
	}
	domainProbe = func(ctx context.Context, d string) (string, error) { return probe(ctx, d) }
	get := func() domainStatus {
		var st domainStatus
		pe.mustDo("GET", fmt.Sprintf("/api/status-pages/%d/domain", p.ID), nil, &st, 200)
		return st
	}
	if st := get(); st.State != "dns" || st.DNS || st.Target != "uptime.test" || st.TargetIPs[0] != "203.0.113.5" {
		t.Fatalf("DNS bekleniyor olmalı: %+v", st)
	}
	dns["durum.ornek.com"] = []string{"203.0.113.5"}
	if st := get(); st.State != "cert" || !st.DNS || st.HTTPSError == "" {
		t.Fatalf("sertifika bekleniyor olmalı: %+v", st)
	}
	probe = func(context.Context, string) (string, error) { return "baska", nil }
	if st := get(); st.State != "cert" || st.HTTPS {
		t.Fatalf("başka sayfa sunan alan etkin sayılmamalı: %+v", st)
	}
	probe = func(context.Context, string) (string, error) { return "durum", nil }
	if st := get(); st.State != "active" || !st.HTTPS {
		t.Fatalf("etkin olmalı: %+v", st)
	}
	// Yalnız yönetici.
	if code := pe.anon().do("GET", fmt.Sprintf("/api/status-pages/%d/domain", p.ID), nil, nil); code != 401 {
		t.Fatalf("oturumsuz: %d", code)
	}
}

// sendHost isteği verilen Host başlığıyla gönderir (özel alan adı benzetimi).
func (e *env) sendHost(method, path, host string) (int, []byte, http.Header) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, nil)
	req.Host = host
	resp, err := e.client.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, resp.Header
}

func TestHTTPSErrCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := probeDomainHTTPS(ctx, "yok.invalid")
	if c := httpsErrCode(err); c != "no_host" {
		t.Errorf("çözülmeyen ad: %q (%v)", c, err)
	}
	_, err = probeDomainHTTPS(ctx, "127.0.0.1:1")
	if c := httpsErrCode(err); c != "refused" {
		t.Errorf("kapalı port: %q (%v)", c, err)
	}
	for err, want := range map[error]string{errNotBekci: "not_bekci", errUnbound: "unbound", httpStatusError(502): "http",
		context.DeadlineExceeded: "timeout", errors.New("x509: certificate signed by unknown authority"): "tls", errors.New("baska"): "other"} {
		if c := httpsErrCode(err); c != want {
			t.Errorf("%v: %q, %q bekleniyordu", err, c, want)
		}
	}
}
