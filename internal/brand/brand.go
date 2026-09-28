// Package brand ürünün görünen adını tek yerde tutar. Ad değişirse yalnızca
// bu sabit ve arayüzdeki web/src/lib/brand.ts (APP_NAME) güncellenir; PWA
// manifest'indeki ad derlemede brand.ts'den yazılır (web/pwa-build.js).
//
// Teknik adlar (program "uptime", hizmet "uptime-agent", ortam değişkenleri,
// klasör yolları, User-Agent, çerez adları) bu sabite BAĞLI DEĞİLDİR ve ad
// değişince de aynı kalır.
package brand

// Name ürünün görünen adı: bildirim metinleri, bildirim kanallarındaki
// gönderen adı, TOTP uygulamasında görünen düzenleyici, Windows hizmetinin
// görünen adı vb.
const Name = "Bekci"
