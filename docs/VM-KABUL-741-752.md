# VM kabulü 741-752 (2026-11-13)

Ortam: Proxmox test VM'i `bk` (10.0.0.36, Ubuntu 24.04, systemd 255), panel Pro yapısı (`pro-f0fc20c`) systemd hizmeti
+ TimescaleDB 2.31.0-pg18 kapsayıcısı; ajan iki biçimde: systemd hizmeti (panelin kurulum komutu, `bk-host`) ve
Docker ajanı (`bk-docker`). Windows denenmedi.

## Sonuçlar

| Madde | Denenen | Sonuç |
|---|---|---|
| 741 denetim sonuçları | migration 54-56 Timescale üzerinde; sonuç -> durum -> olay -> kapanış; Denetimler sekmesi verisi | GEÇTİ (düzeltmeyle, bulgu 3) |
| 741 eklenti uçtan uca | `scripts/eklentiler/mdadm` + loop RAID1: anahtar-uret, imzala, ajanda anahtar/izin, panele zarf+gövde yükleme; OK -> disk `--fail` -> kritik olay -> `--add` sonrası kapanış; systemd-run kafesi (systemd 255) | GEÇTİ (küçük düzeltme, bulgu 5) |
| 743 yerleşik altyapı | `noop` lisanssız panelde oluşturulup çalıştı; `/api/check-kinds` kilitli türleri gösteriyor | GEÇTİ |
| 744 posta kuyruğu | exim4, 5 ileti (1 donmuş): systemd birimi altında komut düşüyordu | GEÇTİ (düzeltmeyle, bulgu 1): total 5, hold 1, deferred 4, oldest_min |
| 745 MySQL | MariaDB 10.11 soket otomatik, bağlantı/çalışan metrikleri | GEÇTİ |
| 746 MSSQL | VM'de denenmedi (Pro kabul testi BEKCI_TEST_MSSQL var) | DENENMEDİ |
| 747 web sunucusu | Apache prefork busy_pct %97 (yavaş istemci), nginx dropped olayı | GEÇTİ (Apache busy olayının 5 dk süresi beklenmedi; metrik doğru) |
| 748 yedek klasörü | backup_file size_drop_pct olayı; dir_size 1 milyon dosyada partial uyarısı (8,3 sn); `/usr` okuma | GEÇTİ |
| 749 mount/süreç | NFS 5 sn takılma critical, OK, bağlı değil; process php-fpm | GEÇTİ |
| 750 inode/zombi | inode % = `df -i`; inode ve zombi olayları; Docker ajanında zombi sayısı host'unki | GEÇTİ |
| 751 Windows sertifika deposu | Windows VM yok | DENENMEDİ |
| 752 filo güvenlik raporu | bayraksız 404; BEKCI_FILO_RAPOR=1 ile JSON + CSV (apt, Timescale) | GEÇTİ (sistemde güvenlik paketi yoktu: ciddiyet süzgeci ve Excel'de Türkçe karakter denenmedi) |
| Lisans kapısı | deneme süresi bitmiş panel: mysql/apache/mailq PUT 402 pro_lisans, noop 200; Pro denetimleri ajana gitmiyor | GEÇTİ (bkz. açık konu) |
| Docker ajanı | sunucu dosyası isteyen yerleşik denetimler | GEÇTİ (düzeltmeyle, bulgu 2) |

## Bulgular ve düzeltmeler

1. **744 systemd birimi altında posta kuyruğu okunamıyordu.** `CapabilityBoundingSet=CAP_NET_RAW` + `NoNewPrivileges`
   ile `exim -bpc` "setgroups() failed", `postqueue` "showq: Permission denied". Düzeltme: birim
   `CAP_DAC_READ_SEARCH` (salt okuma) alır (açık depo 059e3c7); komut düşerse kuyruk klasörü doğrudan okunur
   (exim `-H` başlıkları, postfix kuyruk dosyası T kaydı; bekci-pro f0fc20c `mailq_spool.go`). Test: `TestMailqSpoolYedek`.
   Eski kurulumlar: birim dosyası elle düzeltilir (belge 08-sunucu-ajani#yetki).
2. **Docker ajanında** yedek/posta/mount denetimleri kapsayıcının dosya sistemine bakıyordu ("klasör bulunamadı").
   Düzeltme: `denetim.Tur.YalnizSunucu`; Docker ajanı bu türleri açık iletiyle unknown döner. Test: `TestYerlesikKapsayici`.
3. **KRİTİK: denetim olayları her dakika kapanıp yeniden açılıyordu.** Metrik değerlendirmesi (`servers/alerts.go evaluate`)
   `metric=check` kurallarını değeri yok sayıp kapatıyordu. Düzeltme 8173c72; test `TestCheckIncidentSurvivesMetrics`.
   VM'de düzeltme sonrası olaylar dakikalarca açık kaldı.
4. **Güvenlik duvarı kartı birim altında hep "son sorgu başarısız"** (nft/ufw CAP_NET_ADMIN ister). Yalnız belgelendi; karar bekliyor.
5. `uptime betik eklenti imzala` izin komutunu `<ad>` yer tutucusuyla yazıyordu; artık manifest id'si. Test `TestManifestID`.

## Açık konular

- Lisans bitince açık Pro denetim olayları (ör. nfs kritik) açık kalıyor, güncellenmiyor; lisans dönünce sürüyor. Kapatılsın mı karar.
- Windows (733/734/746/748 junction/751), LiteSpeed, gerçek güvenlik paketiyle 752 ciddiyet süzgeci denenmedi.
