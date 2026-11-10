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
| Otomasyon | — | Betik çalıştırma (açık depodan TAŞINACAK: .sira 7306) |
| Görünüm | Panel, olaylar, sunucu sayfaları | Canlı topoloji (açık depodan taşınacak: .sira 7308) , çoklu panel görünümü |
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
- Betik altyapısının ajan tarafı (betikajan, imza, yerel iğne) açık depoda kalır: ücretsiz eklenti
  zamanlayıcı (739) onu kullanır. 7306 yalnız panel tarafı betik çalıştırmayı (uçlar, sayfalar) taşır.
