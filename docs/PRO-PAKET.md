# Bekci paketleri: Ücretsiz ve Pro

Karar (2026-11-10): iki paket. **Ücretsiz** açık depo `kadirsungurlu/bekci` (AGPL). **Pro** gizli depo
`kadirsungurlu/bekci-pro`; açık depoyu Go modülü olarak içe alır, Pro kodunu `kanca` paketi üzerinden
bağlar ve tek bir Pro imajı (`ghcr.io/kadirsungurlu/bekci-pro`, özel) üretir. Pro imajı lisanssız
çalıştırılınca Ücretsiz gibi davranır (ilk kurulumda 30 gün deneme). Lisans ayrıntısı: `docs/LISANS-TASARIM.md`.
Pro henüz sitede duyurulmaz.

## Ayrım tablosu

| Alan | Ücretsiz | Pro |
|---|---|---|
| İzleme | Tüm monitör türleri, çok konum, kontrol noktaları, mesh | — |
| Durum sayfası | Durum sayfaları, özel alan adı + otomatik HTTPS, e-posta aboneliği | Marka (logo/renk kaldırma), özel alan adlı e-posta gönderimi |
| Bildirim | Tüm bildirim kanalları, kurallar, sessize alma, yükseltme | Nöbet çizelgesi |
| Sunucu ajanı | Metrikler, Docker, systemd, envanter, sertifika, disk dolma tahmini, OOB | — |
| Güvenlik | Sunucu başına güvenlik ÖZETİ (sayılar + uyarı): güncellemeler, yeniden başlatma, SSH/oturum, güvenlik duvarı, Defender | Filo güvenlik görünümü ve raporu, panelden toplu güncelleme ve planlı yeniden başlatma |
| Denetimler | Özel metrik eklentisi (Nagios/JSON), denetim altyapısı, inode/zombi | Hosting servis denetimleri: posta kuyruğu, MSSQL, MySQL, web sunucu durumu, yedek/klasör, Windows görev/sertifika, bağlantı noktası/süreç |
| Otomasyon | — | Betik çalıştırma: panel uçları, sayfalar, imza aracı ve ajan yürütücüsü (7306 ile taşındı) |
| Görünüm | Panel, olaylar, sunucu sayfaları | Canlı topoloji (7308 ile taşındı), çoklu panel görünümü |
| Hesaplar | Kullanıcılar, roller, 2FA, OIDC, API anahtarları (salt okunur dahil) | Müşteri hesapları (çok kiracılı), SAML/SCIM |
| Raporlar | /metrics, rozetler | Zamanlı SLA/PDF raporları |
| İşletim | Yedekleme/içe aktarma, işlem kaydı | İşlem kaydını dışa aktarma, iki panelli HA |

## Özellik bayrakları (`lisans`)

`betik`, `toplu_guncelleme`, `filo_guvenlik`, `hosting_denetim`, `topoloji`, `musteri`, `rapor`, `marka`,
`saml`, `nobet`, `ha`, `coklu_panel`, `denetim_disa`. Anahtardaki `ozellikler` listesi `["*"]` ise hepsi.

## Kurallar

- Pro kodu açık depoya YAZILMAZ; açık depoda yalnız kancalar (`kanca/`), lisans doğrulama (yalnız açık anahtar)
  ve kilitli menü/Lisans ekranı bulunur.
- Lisans bitince yalnız Pro kapanır: izleme, bildirim, durum sayfası hiç durmaz; Pro verisi salt okunur
  (GET açık, değiştiren istekler 402 `pro_lisans`).
- Kuyruk işlerinde `TARAF: ücretsiz/pro` notu bu tabloya göre; pro olanların kodu bekci-pro'ya.
- Betik çalıştırmanın tamamı Pro'da (7306): panel uçları, kayıt zinciri, imza aracı (bekci-imza), ajan
  yürütücüsü (betikajan) ve arayüz. Açık depoda yalnız genel eklenti noktaları ve kilitli menü kalır.
  Ücretsiz eklenti zamanlayıcı (739) betikajan'a dayanmaz: açık depoda kendi dar yürütücüsüyle yazıldı
  (`internal/eklentiajan`; zarfı kendisi doğrular, iğne/kafes/zamanlayıcı/CLI `uptime betik eklenti`).
- Canlı topolojinin tamamı Pro'da (7308): GET /api/topology (bekci-pro `ic/internal/api/topology.go`),
  sayfa `#/pro/topoloji` (eski `#/topology` bağlantısı oraya gider) ve e2e. Bayrak `topoloji`; saklanan Pro
  verisi olmayan salt görünüm olduğundan lisans yoksa GET de 402 `pro_lisans` döner (salt okunur kuralının
  istisnası). Mesh matrisi ve şeması (GET /api/mesh, Ayarlar > Kontrol noktaları, mesh olay kartı) ve
  topolojinin beslendiği veri (monitör-sunucu bağı, kanal gönderim sayaçları, nokta etiketleri) ücretsizde kalır.

## Derin eklentiler (Pro örtüsü)
Go `internal` kuralı yüzünden Pro modülü açık deponun iç paketlerini içe alamaz. Pro deposunda
`ic/<yol>` altındaki dosyalar `scripts/overlay.sh` ile `ust/<yol>` paketlerine sanal olarak eklenir
(`GOFLAGS=-overlay=.overlay.json`; Dockerfile, birlestir.sh ve CI bunu kullanır). Açık depoda aynı adlı
dosya varsa örtü hata verir. Yeni bağımsız paketler (betik, betikajan, betikkayit) Pro modülünün gerçek
paketleridir. Açık depodaki genel noktalar:
- `internal/api/eklenti.go`: `probeJobExtras` (ajan iş yanıtına ek alan), `sessionOnlyExt` (API anahtarına
  kapalı yollar), `backgroundTasks` (`RunExtensions`), `extState` (sunucu başına ek durum).
- `internal/store` `tsExtras` (TimescaleDB dönüşümüne ek tablolar); migration numaraları korunur, açık
  derlemede boşluk atlanır (`migrate_skipped`).
- `internal/notify` `RegisterKind` (ek olay türü: kanal seçimi + başlık).
- `internal/probe` `AgentExtension` (başlık, iş, meşgul, arka plan döngüsü), `uygulama` `ekKomutlar`
  (ek CLI alt komutu) ve `ajanEklentisi`.

## Ajan dağıtımı (karar, 2026-11-10)
Pro ajanı ayrı bir ürün değildir: Pro imajındaki aynı `bekci` ikilisi probe kipinde çalışır. Pro panel
`/api/probe/binary` ve kendi kendine güncelleme ile ajanlara Pro derlemesini dağıtır (imajda
`/usr/local/share/uptime/agents/`, aynı imza anahtarı). Açık ajanda yürütücü yoktur, betik yetenek
başlığı gönderilmez; panel böyle bir ajana betik işi vermez. Lisans yoksa Pro panel yeni çalıştırma
göndermez; iptal, geri alma ve acil durdurma lisanssız da çalışır (risk azaltan işlemler).
