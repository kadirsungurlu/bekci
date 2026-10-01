package check

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func headerValue(hs []Header, name string) (string, bool) {
	for _, h := range hs {
		if strings.EqualFold(h.Name, name) {
			return h.Value, true
		}
	}
	return "", false
}

func TestHTTPDetailCapture(t *testing.T) {
	big := strings.Repeat("ab", DetailBodyMax) // 32 KB
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			fmt.Fprint(w, "tamam")
		case "/400":
			// Gizli değerleri yankılayan bir API.
			http.SetCookie(w, &http.Cookie{Name: "oturum", Value: "cerez-degeri"})
			w.Header().Set("X-Echo", r.Header.Get("X-Api-Key"))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			fmt.Fprintf(w, `{"hata":"geçersiz","anahtar":%q,"auth":%q}`, r.Header.Get("X-Api-Key"), r.Header.Get("Authorization"))
		case "/buyuk":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			if r.URL.Query().Has("boyut") {
				w.Header().Set("Content-Length", fmt.Sprint(len(big)))
			}
			w.WriteHeader(503)
			fmt.Fprint(w, big)
		case "/ikili":
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(500)
			w.Write([]byte{0x89, 'P', 'N', 'G', 0, 1, 2, 3})
		case "/turu-yok":
			w.Header()["Content-Type"] = nil // Go koklamasın
			w.WriteHeader(502)
			w.Write([]byte("<html><body>Bad gateway</body></html>"))
		case "/kelime":
			fmt.Fprint(w, "<html>bakımdayız</html>")
		}
	}))
	defer srv.Close()

	t.Run("başarılı kontrolde ayrıntı yok", func(t *testing.T) {
		if res := run(t, "http", map[string]any{"url": srv.URL + "/ok"}); !res.Up || res.Detail != nil {
			t.Fatalf("%+v", res)
		}
	})

	t.Run("400: istek ve yanıt maskeli", func(t *testing.T) {
		res := run(t, "http", map[string]any{
			"url": srv.URL + "/400", "headers": "X-Api-Key: cok-gizli-anahtar\nAccept: application/json",
			"basic_user": "kadir", "basic_pass": "sifre123",
		})
		d := res.Detail
		if res.Up || d == nil {
			t.Fatalf("ayrıntı yok: %+v", res)
		}
		if d.Method != "GET" || d.URL != srv.URL+"/400" || d.Status != 400 || d.StatusText != "Bad Request" || d.Proto == "" {
			t.Fatalf("istek/yanıt özeti yanlış: %+v", d)
		}
		if v, _ := headerValue(d.RequestHeaders, "X-Api-Key"); v != detailMaskedValue {
			t.Errorf("girilen başlık maskelenmedi: %q", v)
		}
		if v, _ := headerValue(d.RequestHeaders, "Authorization"); v != detailMaskedValue {
			t.Errorf("Authorization maskelenmedi: %q", v)
		}
		if v, _ := headerValue(d.RequestHeaders, "Accept"); v != "application/json" {
			t.Errorf("zararsız başlık maskelenmemeli: %q", v)
		}
		if v, _ := headerValue(d.RequestHeaders, "User-Agent"); v != UserAgent() {
			t.Errorf("User-Agent: %q", v)
		}
		if v, _ := headerValue(d.ResponseHeaders, "Set-Cookie"); v != detailMaskedValue {
			t.Errorf("Set-Cookie maskelenmedi: %q", v)
		}
		if v, _ := headerValue(d.ResponseHeaders, "X-Echo"); v != detailMaskedValue {
			t.Errorf("yankılanan gizli değer maskelenmedi: %q", v)
		}
		// İzin listesindeki standart başlık gösterilir (maskesiz).
		if v, ok := headerValue(d.ResponseHeaders, "Content-Type"); !ok || v != "application/json" {
			t.Errorf("standart başlık maskelenmemeli: %q (%v)", v, ok)
		}
		basic := base64.StdEncoding.EncodeToString([]byte("kadir:sifre123"))
		if strings.Contains(d.Body, "cok-gizli-anahtar") || strings.Contains(d.Body, basic) || !strings.Contains(d.Body, "geçersiz") {
			t.Errorf("gövde maskelenmedi: %s", d.Body)
		}
		if d.BodyBinary || d.BodyTruncated || d.BodySize == 0 {
			t.Errorf("gövde bilgisi yanlış: %+v", d)
		}
		if d.ContentType != "application/json" {
			t.Errorf("içerik türü: %q", d.ContentType)
		}
		// JSON'a çevrilip geri okunabilir (kontrol noktası protokolü).
		b, _ := json.Marshal(d)
		if strings.Contains(string(b), "sifre123") || strings.Contains(string(b), "cerez-degeri") {
			t.Errorf("gizli bilgi JSON'da: %s", b)
		}
	})

	t.Run("16 KB sınırı", func(t *testing.T) {
		d := run(t, "http", map[string]any{"url": srv.URL + "/buyuk?boyut"}).Detail
		if d == nil || len(d.Body) != DetailBodyMax || !d.BodyTruncated || d.BodySize != int64(len(big)) {
			t.Fatalf("kırpma yanlış: len=%d truncated=%v size=%d", len(d.Body), d.BodyTruncated, d.BodySize)
		}
		// Boyut bilinmiyorsa (chunked) 0.
		d = run(t, "http", map[string]any{"url": srv.URL + "/buyuk"}).Detail
		if d == nil || !d.BodyTruncated || d.BodySize != 0 {
			t.Fatalf("chunked: truncated=%v size=%d", d.BodyTruncated, d.BodySize)
		}
	})

	t.Run("ikili içerik", func(t *testing.T) {
		d := run(t, "http", map[string]any{"url": srv.URL + "/ikili"}).Detail
		if d == nil || !d.BodyBinary || d.Body != "(ikili içerik, 8 bayt)" {
			t.Fatalf("%+v", d)
		}
	})

	t.Run("tür yoksa koklanır", func(t *testing.T) {
		d := run(t, "http", map[string]any{"url": srv.URL + "/turu-yok"}).Detail
		if d == nil || d.BodyBinary || !strings.Contains(d.Body, "Bad gateway") {
			t.Fatalf("%+v", d)
		}
	})

	t.Run("kelime bulunamadı (200): gövde yakalanmaz, durum+başlık kalır", func(t *testing.T) {
		// Durum <400 ve kontrol yalnızca keyword eşleşmediği için başarısız:
		// gerçek 2xx gövdesi (hedef verisi sızdırabilir) yakalanmaz; durum ve
		// başlıklar tutulur.
		res := run(t, "http", map[string]any{"url": srv.URL + "/kelime", "keyword": "hoş geldiniz"})
		d := res.Detail
		if res.Up || d == nil || d.Status != 200 {
			t.Fatalf("%+v", d)
		}
		if d.Body != "" {
			t.Errorf("2xx gövdesi yakalanmamalı: %q", d.Body)
		}
		if len(d.ResponseHeaders) == 0 {
			t.Errorf("başlıklar tutulmalı: %+v", d)
		}
	})

	t.Run("accepted_codes uyuşmazlığı (2xx): gövde yakalanmaz", func(t *testing.T) {
		// Saldırgan seçimli accepted_codes ile gerçek bir 200 "başarısız"
		// sayılsa bile gövde alınmaz (yalnızca durum kodu hata olduğunda alınır).
		res := run(t, "http", map[string]any{"url": srv.URL + "/ok", "accepted_codes": []string{"500"}})
		d := res.Detail
		if res.Up || d == nil || d.Status != 200 {
			t.Fatalf("%+v", d)
		}
		if d.Body != "" || d.BodyBinary {
			t.Errorf("2xx gövdesi yakalanmamalı: %q (binary=%v)", d.Body, d.BodyBinary)
		}
	})

	t.Run("durum >=400: gövde yakalanır", func(t *testing.T) {
		// HTTP durumunun kendisi hata (503): gerçek bir başarısızlık, gövde alınır.
		d := run(t, "http", map[string]any{"url": srv.URL + "/buyuk?boyut"}).Detail
		if d == nil || d.Status != 503 || d.Body == "" {
			t.Fatalf("hata durumunda gövde yakalanmalı: %+v", d)
		}
	})

	t.Run("bağlantı hatası: yalnızca istek", func(t *testing.T) {
		l, _ := net.Listen("tcp", "127.0.0.1:0")
		addr := l.Addr().String()
		l.Close()
		res := run(t, "http", map[string]any{"url": "http://kadir:parola99@" + addr + "/x?token=abc"})
		d := res.Detail
		if d == nil || d.Status != 0 || d.Error == "" || d.Error != res.Message || len(d.ResponseHeaders) != 0 {
			t.Fatalf("%+v", d)
		}
		if strings.Contains(d.URL, "parola99") || !strings.Contains(d.URL, "kadir:") {
			t.Errorf("adresteki şifre gizlenmedi: %s", d.URL)
		}
	})
}

func TestDetailSanitizeAndMask(t *testing.T) {
	long := strings.Repeat("x", 5000)
	d := &Detail{
		Method: "GET\n\x00", URL: "https://a.example/" + long, Status: 12345,
		Body:           strings.Repeat("ç", DetailBodyMax), // 2 bayt/karakter: 32 KB
		RequestHeaders: []Header{{Name: "", Value: "ad yok"}, {Name: "X-Token", Value: "abc123def"}},
		BodySize:       -5,
	}
	for range 100 {
		d.ResponseHeaders = append(d.ResponseHeaders, Header{Name: "X-A", Value: long})
	}
	d.Sanitize()
	if d.Method != "GET" || d.Status != 0 || d.BodySize != 0 || len([]rune(d.URL)) > detailMaxURL+1 {
		t.Fatalf("temizlenmedi: %q %d %d", d.Method, d.Status, d.BodySize)
	}
	if len(d.Body) > DetailBodyMax || !d.BodyTruncated || !strings.HasPrefix(d.Body, "çç") || strings.ContainsRune(d.Body, '�') {
		t.Fatalf("gövde sınırı: %d %v", len(d.Body), d.BodyTruncated)
	}
	if len(d.RequestHeaders) != 1 || len(d.ResponseHeaders) != detailMaxHeaders || len([]rune(d.ResponseHeaders[0].Value)) > detailMaxValue+1 {
		t.Fatalf("başlık sınırları: %d %d", len(d.RequestHeaders), len(d.ResponseHeaders))
	}

	// Sunucu tarafında monitör ayarıyla yeniden maskeleme.
	cfg, _ := json.Marshal(map[string]any{"url": "https://a.example", "headers": "X-Token: abc123def"})
	d2 := &Detail{RequestHeaders: []Header{{Name: "x-token", Value: "abc123def"}},
		ResponseHeaders: []Header{{Name: "X-Debug", Value: "istek abc123def ile geldi"}}, Body: "token=abc123def"}
	MaskDetail("http", cfg, d2)
	if d2.RequestHeaders[0].Value != detailMaskedValue || strings.Contains(d2.ResponseHeaders[0].Value, "abc123def") || strings.Contains(d2.Body, "abc123def") {
		t.Fatalf("maskelenmedi: %+v", d2)
	}
	MaskDetail("tcp", cfg, nil) // nil güvenli
}

func TestIsTextBody(t *testing.T) {
	cases := []struct {
		ct   string
		data string
		want bool
	}{
		{"text/html; charset=utf-8", "", true},
		{"application/problem+json", "", true},
		{"application/xml", "", true},
		{"image/png", "", false},
		{"application/octet-stream", "", false},
		{"", "düz metin", true},
		{"", "\x00\x01\x02\x03", false},
		{"", "yarım karakter \xc3", true}, // sınırda bölünmüş
	}
	for _, c := range cases {
		if got := isTextBody(c.ct, []byte(c.data)); got != c.want {
			t.Errorf("isTextBody(%q, %q) = %v", c.ct, c.data, got)
		}
	}
}
