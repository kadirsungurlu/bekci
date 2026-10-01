package check

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUserAgent(t *testing.T) {
	defer SetUserAgent("")
	SetVersion("v1.2.3")
	if got := DefaultUserAgent(); got != "Mozilla/5.0 (compatible; Bekci/1.2.3; +https://bekci.app/bot)" {
		t.Fatalf("varsayılan: %q", got)
	}
	SetVersion("30ddabe") // commit kimliği: sürüm değişmez
	if !strings.Contains(UserAgent(), "Bekci/1.2.3;") {
		t.Fatalf("sürüm korunmalı: %q", UserAgent())
	}

	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("User-Agent"))
	}))
	defer srv.Close()
	run := func(cfg string) {
		c, _ := Get("http")
		norm, err := c.Normalize([]byte(cfg))
		if err != nil {
			t.Fatal(err)
		}
		if res := c.Check(t.Context(), norm); !res.Up {
			t.Fatalf("kontrol: %+v", res)
		}
	}
	run(`{"url":"` + srv.URL + `"}`)
	SetUserAgent("Ozel-Ajan/9")
	run(`{"url":"` + srv.URL + `"}`)
	run(`{"url":"` + srv.URL + `","headers":"User-Agent: Monitor-Ozel"}`)
	if len(seen) != 3 || !strings.Contains(seen[0], "Bekci/") || seen[1] != "Ozel-Ajan/9" || seen[2] != "Monitor-Ozel" {
		t.Fatalf("istek User-Agent'ları: %q", seen)
	}
}
