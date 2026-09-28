// Ürünün görünen adı: TEK yer. Ad değişirse yalnızca burası ve Go tarafında
// internal/brand/brand.go (Name) güncellenir. index.html'deki başlık ve PWA
// manifest'indeki name/short_name derlemede buradan yazılır (web/pwa-build.js
// bu dosyadaki APP_NAME satırını okur; biçimini değiştirmeyin).
//
// Teknik adlar (program "uptime", hizmet "uptime-agent", ortam değişkenleri,
// dosya adları) bu sabite bağlı değildir.
export const APP_NAME = 'Bekci';
