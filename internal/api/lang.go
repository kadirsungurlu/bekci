package api

import (
	"net/http"

	"github.com/kadirsungurlu/bekci/internal/i18n"
	"github.com/kadirsungurlu/bekci/internal/store"
)

// Yanıt dili ---------------------------------------------------------------------
//
// API hata mesajları Türkçe yazılır ve merkezi yazıcıda (writeJSON/writeError)
// isteğin diline çevrilir; işleyicilerin değişmesi gerekmez. Dil sırası:
//  1. Oturumdaki (veya API anahtarındaki) kullanıcının dil tercihi (users.lang),
//  2. arayüzün gönderdiği X-Uptime-Lang başlığı (giriş ekranında seçilen dil),
//  3. Accept-Language, 4. tr.
// Herkese açık durum sayfası uç noktaları sayfanın kendi dilini zorlar.

// langHeader arayüzün geçerli dilini bildirdiği istek başlığı.
const langHeader = "X-Uptime-Lang"

// langWriter isteğin yanıt dilini taşır; withLang tüm isteklere ekler.
type langWriter struct {
	http.ResponseWriter
	r    *http.Request
	lang string // setLang ile belirlenen (kullanıcı tercihi / sayfa dili)
}

func (w *langWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func withLang(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(&langWriter{ResponseWriter: w, r: r}, r)
	})
}

// requestLang istek başlıklarından dil: X-Uptime-Lang, Accept-Language, tr.
func requestLang(r *http.Request) string {
	if l := i18n.Normalize(r.Header.Get(langHeader)); l != "" {
		return l
	}
	if l := i18n.FromAcceptLanguage(r.Header.Get("Accept-Language")); l != "" {
		return l
	}
	return i18n.Default
}

// userLang oturumdaki kullanıcının dili; tercihi yoksa istek başlıklarından.
// (Kimliği doğrulanmış uç noktalarda responseLang ile aynı sonucu verir;
// ResponseWriter'a erişmeyen yardımcılar için.)
func userLang(r *http.Request) string {
	if l := i18n.Normalize(userFrom(r).Lang); l != "" {
		return l
	}
	return requestLang(r)
}

// streamLang canlı akış (SSE) bağlantısının dili. EventSource özel başlık
// gönderemediği için X-Uptime-Lang yerine ?lang= de kabul edilir:
// kullanıcı tercihi, X-Uptime-Lang, ?lang=, Accept-Language, tr.
func streamLang(u store.User, r *http.Request) string {
	if l := i18n.Normalize(u.Lang); l != "" {
		return l
	}
	if l := i18n.Normalize(r.Header.Get(langHeader)); l != "" {
		return l
	}
	if l := i18n.Normalize(r.URL.Query().Get("lang")); l != "" {
		return l
	}
	return requestLang(r)
}

func findLangWriter(w http.ResponseWriter) *langWriter {
	for i := 0; w != nil && i < 10; i++ {
		if lw, ok := w.(*langWriter); ok {
			return lw
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return nil
		}
		w = u.Unwrap()
	}
	return nil
}

// setResponseLang yanıtın dilini belirler (geçersiz/boş dil yok sayılır).
func setResponseLang(w http.ResponseWriter, lang string) {
	if l := i18n.Normalize(lang); l != "" {
		if lw := findLangWriter(w); lw != nil {
			lw.lang = l
		}
	}
}

// responseLang yanıtın dili: belirlenmişse o, değilse istek başlıklarından.
func responseLang(w http.ResponseWriter) string {
	lw := findLangWriter(w)
	if lw == nil {
		return i18n.Default
	}
	if lw.lang != "" {
		return lw.lang
	}
	return requestLang(lw.r)
}

// localized içe aktarma raporundaki uyarı ve kayıt mesajlarını yanıt diline
// çevirir (mesajlar Türkçe üretilir; bkz. i18n.Error).
func (sum *importSummary) localized(lang string) *importSummary {
	if lang == i18n.TR {
		return sum
	}
	c := *sum
	c.Warnings = make([]string, len(sum.Warnings))
	for i, w := range sum.Warnings {
		c.Warnings[i] = i18n.Error(lang, w)
	}
	c.Items = make([]importItem, len(sum.Items))
	for i, it := range sum.Items {
		if len(it.Messages) > 0 {
			msgs := make([]string, len(it.Messages))
			for j, m := range it.Messages {
				msgs[j] = i18n.Error(lang, m)
			}
			it.Messages = msgs
		}
		c.Items[i] = it
	}
	return &c
}

// localizeError JSON yanıtındaki "error" alanını yanıt diline çevirir.
func localizeError(w http.ResponseWriter, v any) any {
	switch m := v.(type) {
	case map[string]string:
		if msg, ok := m["error"]; ok {
			if lang := responseLang(w); lang != i18n.TR {
				c := make(map[string]string, len(m))
				for k, x := range m {
					c[k] = x
				}
				c["error"] = i18n.Error(lang, msg)
				return c
			}
		}
	case map[string]any:
		if msg, ok := m["error"].(string); ok {
			if lang := responseLang(w); lang != i18n.TR {
				c := make(map[string]any, len(m))
				for k, x := range m {
					c[k] = x
				}
				c["error"] = i18n.Error(lang, msg)
				return c
			}
		}
	}
	return v
}
