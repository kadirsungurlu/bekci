# TimescaleDB geçişi: gerçek kullanım denetimi ve canlı geçiş planı (640)

Tarih: 2026-11-04. Tümü bu sunucuda yerel konteynerlerde yapıldı; canlıdan yalnız okundu (gece yedeğinin kopyası).
İmaj: `gelistirme` dalı, `timescale/timescaledb:2.31.0-pg18`. "Fark" = aynı zaman damgalı grafik noktalarının karşılaştırması.

## Senaryo sonuçları

| Senaryo | Durum | Ölçüm |
|---|---|---|
| (a) Varsayılan compose, gerçek Caddy, sıfırdan | ÇALIŞIYOR | İlk açılış 23 sn. Yeni ve 1.6.0 nokta çevrimiçi, 3 konumdan sonuç. 24 sa/7 g/30 g seri, sunucu metrikleri, topoloji, olay + webhook. 1920 ve 390 ekranda sayfa hatası yok |
| (b) 1.6.0 SQLite, gerçekçi veri | ÇALIŞIYOR | 1.275.333 satır, 54 tablo: 2 dk 22 sn. 100.451 noktada 0 fark. SQLite dosyası değişmedi. DB 114 MB |
| (c1) 1.6.0 + düz PG imaj değişimi | ÇALIŞIYOR | Kesinti 38 sn (dönüşüm 26 sn), 100.204 noktada 0 fark |
| (c2) 1.6.0 gömülü PG imajı | ÇALIŞIYOR | Kesinti 181 sn (dönüşüm 2 dk 35 sn), 0 fark |
| (d) Canlı yedek kopyası, internetsiz ağ, tek çekirdek, db 400 MB, app 256 MB | ÇALIŞIYOR | 199.135 satır, 9 monitör, 5 nokta. Taşıma 19-22 sn, panel dahil 21-26 sn. 18.633 noktada 0 fark. RSS db ~150 MB, app ~12 MB. Birim 261 MB. Gece dökümü 2 sn, 4,7 MB |
| (e) Düz PG 16, süper kullanıcı olmayan rol | ÇALIŞIYOR | Düz kip, uyarı logda, 98.638 noktada 0 fark, seri 9-38 ms, reset/sil/pg_dump çalışır. Önceden kurulu eklentiyle aynı rol TS kipinde açılır |
| (f) Sıkıştırma ve saklama | ÇALIŞIYOR | `bekci_now` +31 gün: 15/15 parça sıkıştı, 130 -> 55 MB, mesh saklaması 2000 -> 0, sonra 0 fark. Sıkışık parçaya canlı ve geç yazma çalışır. 180 bin satırlı monitörde reset ve silme 0,3 sn. Ham saklama: 3 ve 1 gün, 4 -> 2 parça, hatasız |
| (g) Geri dönüş | ÇALIŞIYOR | SQLite: 1 sn, 0 fark (canlı kopyada 18.340 noktada 0 fark). Düz PG: döküm + 1.6.0, 20 sn, 0 fark. Gömülü: eski konteyner, 0 fark |

Canlı kopyada SQLite'a dönüşte birkaç noktanın "yalnız eskide" görünmesi, pencerenin kaymasıdır (yedek eski, sorgu şimdiye göre).

## Bulunan ve düzeltilen hatalar

1. Saklama temizliği, sıkışık sınır parçasında 100 bin satır üstünde `tuple decompression limit exceeded` veriyordu. Artık sıkıştırmalı kipte yalnız bütün parçalar atılır; saklama en çok 1 gün uzar. Hatayı eski imajla yeniden gördüm, yeni imajla 0.
2. Tek çekirdekte 8 işçi: ilk açılışta 15 iş başlayamıyor, heartbeats sıkıştırması ertesi turu bekliyordu. Compose'a `max_worker_processes=24` eklendi; aynı canlı kopyada 0 başarısız iş, 7/15 parça ilk dakikalarda sıkıştı.
3. Tekrarlı bildirim gönderimi 15 sn'yi aşınca teslimat kaydı yazılamıyordu (1.6.0'da da var): kayıt yeni bağlamla yazılır. Yalnız kod incelemesi; bu dal için özel test yok.
4. `p95` taşınan dönemde yalnız taşıma sonrasını yansıtıyordu: histogram kapsamı %90 altındaysa -1 döner. Canlı kopyada 7 g/30 g -1 doğrulandı. Arayüzde p95 gösterilmiyor.
5. Geri dönüş sonrası tekrar `migrate` sessizce veri aktarmıyordu: uyarı ve belge eklendi. Komut hâlâ çıkış kodu 0 ile biter.
6. Ajan konteynerleri 1.6.0'dan beri `unhealthy` görünüyordu: `ADDR=-` iken sağlık denetimi panel adresini aramaz.

## Denenmeyenler (DENENMEDİ)

- Canlı noktaların (ajanların) yeni panele gerçek bağlanması; a7eb703 ajanı. 1.6.0 ajan yerelde çalıştı.
- Canlı sunucunun kendisi: yalnız kapasite sınırı taklit edildi. Bellek baskısı altında uzun süreli çalışma.
- Coolify test paneli üzerinde yeni sürüm (bkz. rapor, onay gerekiyor).
- Apache lisanslı Timescale kipi (yalnız hypertable), arm64, gerçek AWS RDS, 10.000 monitör.
- Ayarlar > Genel'deki SQLite/düz PG uyarı şeridinin ekranı.
- `TS_TUNE_MEMORY=400MB`: ayar alındı (shared_buffers 128 MB, 25 bağlantı) ama bu ayarla tam taşıma koşulmadı; tam prova cgroup sınırıyla yapıldı.

## Canlı geçiş planı (uptime sunucusu, SQLite -> TimescaleDB)

Kısıtlar: 1 CPU, 967 MB RAM, swap yok, 4,3 GB boş disk. TS imajı 1,57 GB, DB birimi ~260 MB.
Bellek hesabı: mevcut konteynerler ~165 MB + db ~150-200 MB, sığar ama payı az. Sınırsız bırakılırsa imaj 967 MB'a göre ayar seçer; bu yüzden `TS_TUNE_MEMORY=400MB` zorunlu. Öneri: önce 1 GB swap ya da 2 GB RAM.

Hazırlık (kesintisiz, gün içinde):
1. `/root/docker/uptime/` altında compose + `.env` kopyası ve `bekci-data` biriminin tgz yedeği; boş disk en az 3 GB.
2. `docker pull` ile yeni Bekci ve `timescale/timescaledb:2.31.0-pg18` imajlarını önceden çek (kesintide ağ beklenmez).
3. Compose'a `db` servisi, `TS_TUNE_MEMORY: 400MB`, `max_worker_processes=24`; `.env`'e `POSTGRES_PASSWORD`; yorumları güncelle. `TRUSTED_PROXY` kalır.
4. `docker compose up -d db` ile veritabanını aç; panel 1.6.0 olarak çalışmaya devam eder.
5. `docker compose run --rm bekci migrate --dry-run` (yeni etiketle).

Geçiş (tahmini kesinti 1-2 dakika; veri taşıması 20-60 sn):
6. Bakım penceresi; `docker compose stop bekci`.
7. SQLite dosyasının SHA-256 özetini not et.
8. `docker compose run --rm bekci migrate`; çıktıda satır sayıları ve doğrulama satırı beklenir.
9. `DATABASE_URL` ekle, `UPTIME_TAG=<yeni sürüm>`, `docker compose up -d bekci`.

Doğrulama (5-10 dakika):
10. `/healthz?detail` -> `db_mode: timescale`; 5 nokta çevrimiçi; monitör sayısı 9; açık olaylar aynı.
11. Birkaç dakika sonra: yeni sonuçlar akıyor, grafikler dolu, deneme bildirimi gidiyor.
12. `timescaledb_information.job_stats` içinde başarısız iş yok. Ertesi gün: sıkıştırma ve gece `.dump` yedeği oluştu.

Geri dönüş (provada 1 sn açılış; işlemlerle birlikte tahmini 2-5 dakika):
1. `docker compose stop bekci`.
2. `DATABASE_URL` satırını sil, `UPTIME_TAG=1.6.0`, `docker compose up -d bekci`.
3. SQLite dosyası değişmemiştir; geçiş sonrası PostgreSQL'e yazılan veri geri gelmez. Durum sayfası olayları kontrol edilir.
Yeniden denemek için: veritabanını boşalt, adım 6'dan başla (yoksa `migrate` veri kopyalamaz).
SQLite dosyası ve tgz yedeği en az 30 gün saklanır.
