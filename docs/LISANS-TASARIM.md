# Lisans ve kira tasarımı (7305; sunucu tarafı 7307)

Bu belge sözleşmedir: `lisans`, `internal/api/license*.go`, `web` Lisans ekranı, bekci-pro anahtar
üreticisi ve lisans sunucusu (lisans.bekci.app, 7307) buna uyar.

## Kavramlar

- **Panel kimliği**: ilk açılışta üretilen UUID v4, `settings` tablosunda `lisans.panel_id`. Yedekle taşınır.
- **Cihaz kimliği**: makineden türetilir, DB'de TUTULMAZ:
  `hex(sha256("bekci-cihaz-v1\n" + product_uuid + "\n" + machine_id))[:32]` (küçük harf, boşluk kırpılmış).
  Kaynak sırası: `$BEKCI_CIHAZ_DIZIN/{product_uuid,machine-id}`, `/run/bekci-host/{product_uuid,machine-id}`,
  `/sys/class/dmi/id/product_uuid` + `/etc/machine-id`. İkisi de okunamazsa cihaz kimliği YOK ve cihaza bağlı
  lisans açık hatayla etkinleşmez. Docker: compose bu iki dosyayı `/run/bekci-host/` altına salt okunur bağlar;
  `product_uuid` ana makinede 0400 root olduğundan imaj giriş betiği (`deploy/giris.sh`) root olarak okuyup
  `/run/bekci/cihaz` (tmpfs, kalıcı değil) altına kopyalar ve `uptime` kullanıcısına düşer.
  Yedeği başka VM'e dönmek cihaz kimliğini taşımaz.
- **Anahtar** (müşteriye verilen): uzun ömürlü, imzalı; tek başına (çevrimdışı ömür boyu dışında) Pro açmaz.
- **Kira**: lisans sunucusunun panel kimliği (+ cihaz) için verdiği kısa ömürlü imzalı belge (14 gün).
  Panel günde bir yeniler. Ağdan yalıtılmış müşteriye elle yüklenen uzun süreli **çevrimdışı kira** de verilebilir.

## Biçim

İkisi de `ÖNEK.<base64url(JSON)>.<base64url(imza)>` (dolgusuz base64url). İmza Ed25519, imzalanan bayt dizisi
`ÖNEK.` + JSON parçasının base64url metni (ASCII). Açık anahtarlar `lisans/acik_anahtar.go`'da gömülü
(birden çok olabilir: anahtar döndürme). Gizli anahtar yalnız `/root/.config/bekci/lisans-imza.key` (base64 32
bayt tohum, 600), hiçbir depoya girmez. Bilinmeyen JSON alanı reddedilmez (ileri uyum), `v` > 1 reddedilir.

### Anahtar: önek `BEKCI-L1`
```json
{"v":1,"kimlik":"L-<26 karakter rastgele>","musteri":"Örnek AŞ","paket":"pro",
 "tur":"abonelik|omur_cevrimici|omur_cevrimdisi","bitis":"2027-11-10T00:00:00Z",
 "guncelleme_bitis":"","cihaz":"","ozellikler":["*"],"verilis":"2026-11-10T00:00:00Z",
 "yenileme_adresi":"https://lisans.bekci.app"}
```
- `abonelik`: `bitis` zorunlu, `cihaz` boş (cihaza bağlı değil).
- `omur_cevrimici`, `omur_cevrimdisi`: `bitis` boş, `cihaz` ZORUNLU. `guncelleme_bitis` isteğe bağlı: doluysa
  bu tarihten SONRA derlenen sürümde Pro kapanır, eski sürüm çalışmayı sürdürür.
- `yenileme_adresi` boşsa `LISANS_URL` ortam değişkeni, o da yoksa `https://lisans.bekci.app`.

### Kira: önek `BEKCI-K1`
```json
{"v":1,"lisans":"L-...","panel":"<panel uuid>","cihaz":"","tur":"abonelik","musteri":"Örnek AŞ",
 "lisans_bitis":"2027-11-10T00:00:00Z","guncelleme_bitis":"","ozellikler":["*"],
 "verilis":"2026-11-10T08:00:00Z","gecerlilik":"2026-11-24T08:00:00Z","cevrimdisi":false}
```
`lisans_bitis` güncel bitiştir (uzatma buradan yansır; anahtardaki `bitis` yalnız bilgi).

## Lisans sunucusu isteği (panel tarafı; sunucu 7307)

`POST <adres>/v1/kira`, JSON gövde YALNIZ:
`{"lisans":"L-...","panel":"<uuid>","cihaz":"<yalnız cihaza bağlı lisansta>","surum":"1.9.0","sunucu_sayisi":12}`.
İzleme verisi asla gönderilmez (belge + Ayarlar > Lisans ekranında yazılı).
- 200 `{"kira":"BEKCI-K1..."}` → doğrulanır (imza, lisans/panel/cihaz eşleşmesi), saklanır.
- 4xx `{"kod":"iptal|bilinmeyen|cihaz|panel_sinir|tasima_sinir","mesaj":"..."}` → kesin ret. Yenilemede
  `iptal|bilinmeyen|cihaz` kirayı hemen siler (Pro kapanır); etkinleştirmede hiçbir şey kaydedilmez.
- Ağ hatası / 5xx → elde kira varsa o bitene dek Pro sürer; `son_hata` yazılır.

Yenileme: başlangıçta ve 24 saatte bir (hata sonrası 1 saatte bir). `cevrimdisi` kira ve `omur_cevrimdisi`
anahtarda sunucuya HİÇ bağlanılmaz.

Taşıma (sunucuda uygulanır, kural `lisans.TasimaIzni` ortak saf fonksiyon): abonelik başka panel kimliğinden
istenirse 30 günde 1 kez kabul edilir (eski panelin kirası bitince orada Pro kapanır). Cihaza bağlı lisansta
kendi kendine taşıma YOK (`cihaz` uyuşmazsa `kod:"cihaz"`); yeniden verme yalnız yönetimden, taşıma geçmişiyle.

## Değerlendirme (`lisans.Degerlendir`, saf fonksiyon)

Girdi: anahtar, kira, panel kimliği, cihaz kimliği (+hata), şimdi, en ileri görülen zaman, derleme zamanı,
kurulum zamanı, Pro derlemesi mi. Sıra:
1. **Saat geri alma**: `simdi < en_ileri - 10 dk` → Pro kapalı, neden `saat_geri` (deneme dahil). Panel en ileri
   zamanı `lisans.en_ileri` ayarında tutar (10 dakikada bir, yalnız ileri yazılır).
2. Pro derlemesi değilse: `etkin=false`, neden `pro_degil` (kilitli menüler yine görünür).
3. Anahtar yoksa: `simdi < kurulum + 30 gün` → `kaynak=deneme`, tüm özellikler. Değilse `deneme_bitti`.
4. Anahtar `omur_*` → cihaz kimliği okunamazsa `cihaz_yok`, farklıysa `cihaz`. `guncelleme_bitis` dolu ve
   derleme zamanı ondan sonraysa `guncelleme_bitis`.
5. `omur_cevrimdisi` → etkin (kira gerekmez).
6. Diğerleri kira ister: yoksa `kira_yok`; `lisans/panel/cihaz` uyuşmazsa `kira_uyusmaz`; `simdi >= gecerlilik`
   → `kira_bitti`; abonelikte `simdi >= lisans_bitis` → `suresi_doldu`; derleme zamanı `lisans_bitis`'ten
   sonraysa → `suresi_doldu`; kiradaki `guncelleme_bitis` 4. madde gibi.
7. Derleme zamanı bilinmiyorsa (geliştirme derlemesi) derleme denetimleri atlanır; Pro imajı her zaman
   `-X github.com/kadirsungurlu/bekci/lisans.DerlemeZamani=<RFC3339>` ile derlenir.

Çıktı `Durum`: `etkin, kaynak (yok|deneme|lisans), neden, ozellikler, tur, musteri, bitis (abonelikte
lisans_bitis), kira_bitis, kalan_gun, uyari`. Uyarı önceliği: `saat_geri` > `suresi_doldu|kira_bitti` >
`sunucu_ulasilamiyor` (son başarılı yenileme 36 saatten eski ve kira geçerli) > `bitis_1|bitis_7|bitis_14`
(abonelik bitişine ya da çevrimdışı kira bitişine kalan gün ≤ 1/7/14) > `deneme` (son 7 gün).

## Uyarılar

Panel şeridi: `GET /api/license` `uyari` alanından (tüm oturumlar). Yönetici bildirimi: 14/7/1 gün eşiği ilk
geçildiğinde ve `sunucu_ulasilamiyor` ilk görüldüğünde bir kez (seviye `lisans.uyari_seviye` ayarında);
yöneticilere sistem e-postası (yapılandırılmışsa) + işlem kaydı + web push.

## API

- `GET /api/license` (oturum): `{pro_derleme, etkin, kaynak, neden, ozellikler, tur, bitis, kalan_gun, uyari}`
- `GET /api/license/detail` (yönetici): yukarıdakiler + `panel_id, cihaz_id, cihaz_hata, musteri, lisans_kimlik,
  kira_bitis, cevrimdisi, son_basari, son_deneme, son_hata, deneme_bitis, derleme, sunucu_adresi,
  gonderilen_veri:["lisans","panel","cihaz","surum","sunucu_sayisi"]`
- `PUT /api/license` `{anahtar}` (yönetici): imza + tür + cihaz denetimi; `omur_cevrimdisi` hemen kaydedilir,
  diğerleri ilk kira alınınca. Hata: 400 `{code:"lisans_gecersiz|cihaz_yok|cihaz"}`, 409 sunucu reddi
  (`code` = sunucu `kod`), 502 `sunucu_ulasilamiyor`.
- `POST /api/license/lease` `{kira}` (yönetici): elle çevrimdışı kira; saklı anahtar ve panelle eşleşmeli.
- `POST /api/license/refresh` (yönetici), `DELETE /api/license` (yönetici; anahtar + kira silinir).
- İşlem kaydı: `license.set`, `license.lease`, `license.remove`.

## Ayarlar anahtarları (`settings` tablosu, migration yok)

`lisans.panel_id`, `lisans.kurulum`, `lisans.en_ileri`, `lisans.anahtar`, `lisans.kira`, `lisans.son_basari`,
`lisans.son_deneme`, `lisans.son_hata`, `lisans.uyari_seviye`.

## Kancalar (`kanca` paketi, açık depo, iç olmayan yol)

bekci-pro `internal/` içe alamaz; bu yüzden `github.com/kadirsungurlu/bekci/kanca` ve `.../uygulama` (eski
`cmd/uptime` main paketi, `uygulama.Main()`) iç olmayan paketlerdir. Pro kodu `init()` içinde:
- `kanca.Rota(kanca.RotaTanim{Desen:"GET /api/pro/...", Erisim:kanca.Yonetici, Ozellik:"filo_guvenlik", Isleyici:h})`
  — Pro derlemesinde lisans yoksa GET geçer (veri salt okunur), değiştiren istek 402 `{code:"pro_lisans"}`.
- `kanca.Baslangic(func(ctx, *kanca.Panel) error)` — Panel: `DB *sql.DB`, `Dialect`, `Log`, `Ozellik(ad) bool`, `Bildir`.
- `kanca.Migration(n, sql)` — Pro migration'ları 1000'den başlar.
- `kanca.IzleyiciTuru(ad, ozellik, denetleyici)` — `check.Register` üstünde; lisans yoksa sonuç "Pro lisansı gerekli".
- `kanca.AjanKomutu(ad, ozellik, fn)` — ajan tarafı komut kaydı (iş listesinde `pro_jobs`).
- `kanca.ProDerleme()` — Pro modülü kendini işaretler (`lisans.ProDerleme = true`).
Arayüz: `web/src/pro/sayfalar/*.svelte` (açık depoda boş; bekci-pro derlemesi kopyalar) `import.meta.glob` ile
yüklenir; menü listesi `web/src/lib/pro.svelte.ts`.
