# Bekci veritabanı: Postgres + TimescaleDB tasarımı (iş 600)

Durum: TASARIM (2026-11-03). Kod değişikliği yok. Uygulama işleri: 610-695 (bölüm 9).
Karar (kullanıcı, 2026-11-03): tek sürüm hattı, varsayılan kurulum Postgres + TimescaleDB; ayrı `-postgres`
imajı ve SQLite bu sürümde "kullanımdan kalkıyor", sonraki büyük sürümde kaldırılır; düz Postgres
(eklentisiz) uyumluluk modu olarak kalır. Kapsam: Bekci DB, sunucuların tek bilgi kaynağı.

Etiketler: **[ölçüldü]** bu belgede komutla ölçülen değer; **[denenmedi]** yalnız tasarım, 610-640'ta doğrulanacak.

---

## 1. Canlı kurulum ölçümü (uptime sunucusu, yalnız okuma, 2026-11-03) [ölçüldü]

- Veritabanı: **SQLite** (`/data/uptime.db`, `DATABASE_URL` yok, imaj `kadirsungurlu/bekci:latest` = 1.6.0).
  Dosya 77 MB + WAL 4 MB. Sunucu diski %53 dolu (9.7 GB).
- Ölçek: 9 monitör (hepsi 60 sn), 5 kontrol noktası, 4 sunucu ajanı (metrik), 1 durum sayfası.

| Tablo | Boyut | Satır | Not |
|---|---|---|---|
| server_stats | 60.4 MB | 8.706 | %78. Satır başına ~4.4 KB JSON (disk + konteyner dizisi). res=1: 24 sa, res=10: 7 gün, res=60: 90 gün |
| heartbeats (+2 dizin) | 9.6 + 5.8 MB | 181.600 | 14 gün ham saklama (varsayılan). ~53 bayt/satır + dizin |
| stats_hourly | 0.3 MB | 7.500 | Go'nun her sonuçta güncellediği saatlik özet |
| stats_daily | 16 KB | 322 | yerel saat dilimine göre gün; süresiz |
| incident_captures / incident_events / audit_log | <0.2 MB | 32 / 224 / 152 | düşük hacim |

- Günlük artış: heartbeats **12.968 satır/gün** (9 x 1440), server_stats res=1 **~4.480 satır/gün** (4 sunucu x 1440)
  ve ~19 MB/gün ham JSON (24 sa sonra silinir); kalıcı büyüme res=60 tarafında ~1 satır/sa/sunucu.
- Sonuç: canlı bugün küçük; geçişin asıl gerekçesi ölçek (yüzlerce-binlerce monitör, sunucu envanteri, uzun saklama).

## 2. Zaman serisi envanteri ve sorgular

### 2.1 Bugünkü tablolar
| Tablo | Yazan | Okuyan (store işlevi -> kullanıldığı yer) |
|---|---|---|
| `heartbeats` (monitor_id, time BIGINT, status, ping_ms, message, location) | `store/beats.go writeBeat` (+ `beats_batch.go` grup commit) | `Beats` (monitör ayrıntı ham grafik), `CountBeats`, `LastBeats` (son N), `RecentBeats` (durum sayfası çubukları "beats" modu), `RecentPings` (liste sparkline, PG'de LATERAL), `DeleteBeatsBefore` |
| `stats_hourly` (monitor_id, bucket, up, down, ping_sum, ping_count, ping_min, ping_max) | `writeBeat` her sonuçta UPSERT | `HourlyAll`, `HourlyFor` (durum sayfası), `Series(daily=false)`, `Uptime`, `AvgPing`, `UptimeWindows` (liste 7/30 g, durum sayfası pencereleri), `apikeys.go` (rozet/API uptime) |
| `stats_daily` (aynı sütunlar, yerel gün) | `writeBeat` her sonuçta UPSERT | `Series(daily=true)`, `pages.go` (90 günlük çubuklar), `UptimeWindows` (saatlik saklama dışı kısım) |
| `server_stats` (probe_id, res, time, data JSON) | `servers/servers.go` res=1, `servers/rollup.go` res=10/60 (Go içinde ortalama) | `ServerStats(res, from, to)` (sunucu grafikleri), `LastServerStat`, `stats/maintenance.go` silme |
| `server_alert_events` | eşik uyarıları | sunucu sayfası geçmişi; silme |
| `probe_links` | mesh, kalp atışı | **yalnız son durum** (satır güncellenir); zaman serisi YOK |
| `incident_events`, `incident_captures`, `notification_deliveries`, `audit_log` | olay/işlem kaydı | düşük hacim, güncellenebilir satırlar |

Yüzdelik (p95) bugün hiçbir yerde hesaplanmıyor; SLA/rapor ucu yok (uptime yüzdesi = sum(up)/sum(up+down)).

### 2.2 Yeni zaman serileri (kapsam genişlemesi)
`mesh_samples` (src, dst, time, rtt_ms, ok) · `server_metrics` · `server_disk_metrics` · `server_container_metrics` ·
`server_events` (servis çöktü, OOM, yeniden başlatma, ajan koptu, disk doluyor) · `script_runs` (betik çalıştırma geçmişi, 690).

## 3. Şema tasarımı

### 3.1 Genel ilkeler
- **Zaman sütunu BIGINT (unix sn) kalır.** Gerekçe: tüm Go kodu int64 okuyor; SQLite ve düz PG kullanımdan kalkma
  döneminde aynı kod yolunu paylaşıyor. Timescale tamsayı zamanla çalışır: `set_integer_now_func('<tablo>','bekci_now')`
  (`extract(epoch from now())::bigint`). [ölçüldü: hypertable, sıkıştırma, saklama ve cagg politikaları tamsayı aralıklarla kabul edildi]
- Hypertable'da benzersiz anahtar zaman sütununu içermeli: `heartbeats` PK `(id)` -> `(monitor_id, time, id)`.
  `id` kimlik sütunu kalır (`LastBeats` eşitlik sıralaması için).
- Kayıt tabloları (sunucular, envanter, betikler, olaylar, ayarlar) **normal tablo** kalır; yalnız ekleme yapılan, zamanla
  büyüyen tablolar hypertable olur.
- Sıkıştırma: `segmentby` = varlık kimliği, `orderby` = `time DESC`. Sıkıştırılmış parçaya UPDATE/DELETE pahalıdır;
  bu yüzden hypertable'lara yalnız INSERT yapılır (olay "kapatma" gibi güncellemeler normal tabloda kalır).

### 3.2 Hypertable listesi
| Tablo | Parça aralığı | Sıkıştırma | Ham saklama (varsayılan / ayar) | Not |
|---|---|---|---|---|
| `heartbeats` | 1 gün | 7 gün sonra, segmentby monitor_id | **30 gün** / 1-365 | bugün 14 (1-90). Sıkıştırma ile 30 gün ucuz |
| `mesh_samples` | 1 gün | 3 gün sonra, segmentby (src_id,dst_id) | 30 gün / 7-365 | yeni; `probe_links` son durum tablosu olarak kalır |
| `server_metrics` | 1 gün | 3 gün sonra, segmentby probe_id | 30 gün / 7-365 | JSON'dan sütunlara: cpu, load1/5/15, mem_*, swap_*, disk_*_bps, net_*_bps, uptime |
| `server_disk_metrics` | 1 gün | 3 gün sonra, segmentby (probe_id, mount) | 30 gün | mount, device, fs, total, used |
| `server_container_metrics` | 1 gün | 3 gün sonra, segmentby (probe_id, name) | 14 gün | name, container_id, cpu, mem, net_*, state, restart_count |
| `server_events` | 7 gün | 30 gün sonra | 365 gün | kind, subject, message, data JSON; yalnız ekleme |
| `script_runs` | 30 gün | 90 gün sonra | 365 gün | 680/690'da kesinleşir; çıktı gövdesi ayrı normal tabloda, kısaltılmış |

Parça aralığı gerekçesi: Timescale önerisi "en yeni parçalar + dizinleri RAM'in ~%25'ine sığsın". 10.000 monitör x 30 sn x 5 konum
= 144 M satır/gün x ~170 B = ~24 GB/gün sıkıştırılmamış: o ölçekte `heartbeats` parça aralığı **6 saat** olmalı.
Öneri: parça aralığını kurulumdaki monitör sayısından türet (`set_chunk_time_interval`, başlangıçta ve haftalık):
<500 monitör 1 gün, <5.000 6 sa, üstü 2 sa. [denenmedi: 620 ölçecek]

### 3.3 Sürekli özetler (continuous aggregate)
| Özet | Kaynak | Sütunlar | Yenileme politikası | Saklama |
|---|---|---|---|---|
| `stats_hourly` (cagg, adı ve sütunları bugünkü tabloyla AYNI) | heartbeats | up, down, ping_sum, ping_count, ping_min, ping_max + `h_*` histogram sayaçları | start 3 gün, end 1 sa, her 15 dk; `materialized_only=false` (gerçek zamanlı) | 400 gün / 90-3650 |
| `stats_daily_utc` (hiyerarşik cagg) | stats_hourly | aynı | her 1 sa | süresiz |
| `mesh_hourly` | mesh_samples | rtt avg/min/max, ok/fail sayısı, histogram | 15 dk | 365 gün |
| `server_metrics_10m`, `server_metrics_1h` (1h, 10m üzerine hiyerarşik) | server_metrics | cpu avg/max, mem avg/max, load avg, net/disk bps avg/max | 5 dk / 30 dk | 30 gün / 2 yıl |
| `server_disk_1h` | server_disk_metrics | used min/max/last(`max(used)` ile yaklaşık), total | 30 dk | 2 yıl |
| `server_container_1h` | server_container_metrics | cpu avg/max, mem avg/max, son durum | 30 dk | 90 gün |

- Cagg adının bugünkü tabloyla aynı olması, `Series`, `Uptime`, `UptimeWindows`, `HourlyFor`, `apikeys.go` sorgularının
  **metin değişmeden** çalışmasını sağlar; Timescale modunda yalnız `writeBeat` içindeki UPSERT atlanır (sonuç başına 4 deyimden 2ye iner: INSERT heartbeats + UPDATE monitors).
- **Yerel gün sorunu** [ölçüldü]: tamsayı zamanlı hiyerarşik cagg farklı ofset kabul etmiyor
  ("cannot create continuous aggregate with different bucket offset values"); saat dilimi + yaz saati de tamsayıda ifade edilemez.
  Karar: günlük cagg UTC (`stats_daily_utc`) yalnız uzun pencere toplamları için (365 g uptime); **yerel gün çubukları**
  (durum sayfası 90 gün, `Series(daily=true)`) saatlik caggdan yerel güne gruplanarak okunur (90 x 24 = 2.160 satır/monitör,
  durum sayfası zaten önbellekli). Yarım saatlik dilimlerde (ör. +05:30) gün sınırı saate yuvarlanır; belgede yazılır.
  Pencere toplamı (`UptimeWindows`): baştaki/sondaki kısmi günler saatlikten, aradaki tam günler `stats_daily_utc`'den.
- `stats_daily` tablosu Timescale modunda yazılmaz; taşımada mevcut satırlar saatlik saklamanın ötesindeki geçmiş için
  `stats_daily_legacy` olarak saklanır (salt okunur), `UptimeWindows` en eski kısım için ona bakar.

### 3.4 Yüzdelik (p95) [ölçüldü: imajda toolkit YOK]
- `timescale/timescaledb:2.31.0-pg18` imajında yalnız `timescaledb` var; `timescaledb_toolkit` (percentile_agg, uddsketch)
  **yok**; yalnız ~2.7 GB'lık `timescaledb-ha` imajında var. Toolkit de TSL lisanslı ve yönetilen PG'de çoğunlukla yok.
- Karar: **toolkit'e bağımlı olmayan sabit histogram.** Saatlik caggda gecikme kova sayaçları
  `count(*) FILTER (WHERE ping_ms < B)`, B = 10, 20, 30, 50, 75, 100, 150, 200, 300, 500, 750, 1000, 2000, 5000, 10000, 30000 ms.
  p95 = birikimli sayaç %95'i geçtiği kovada doğrusal ara değer. Hata payı kova genişliği kadar (~%25-50 göreli);
  izleme grafiği için yeterli. Aynı hesap düz PG / SQLite'ta Go'nun tuttuğu `stats_hourly`'ye aynı sütunlar eklenerek yapılır.
- Kısa pencerede (ham saklama içinde, tek monitör) kesin değer: `percentile_cont(0.95)` ham veriden.
  [ölçüldü: 10 M satırda tek monitör 7 gün p95 = 14-17 ms (düz tablo ve hypertable)]

### 3.5 Kayıt tabloları (sunucu bilgi kaynağı; 650-690 için iskelet, normal tablo)
- `probes` (bugün sunucu = ajan) genişler ya da `servers` ayrı tablo: ad, IP'ler, SSH kısa ad/port, OS, CPU/RAM/disk kapasitesi,
  rol, etiketler (mevcut `tags` ile), not. **Şifre/özel anahtar/token alanı YOK** (yalnız `ssh_host_alias`, `ssh_port`).
- Envanter (ajanın topladığı; son durum + değişim olayı): `inv_services`, `inv_containers` (ad, imaj, durum, restart_count),
  `inv_ports` (proto, adres, port, süreç), `inv_certs` (kaynak: dosya/port, CN/SAN, not_after), `inv_disks` (mount, device, fs, total).
  Her tablo `(server_id, anahtar)` benzersiz, `first_seen`, `last_seen`; değişiklik `server_events`'e yazılır (zaman serisi).
- `scripts` (betik kütüphanesi: ad, sürüm, gövde özeti, imza), çalıştırma kaydı `script_runs` hypertable. Ayrıntı 680.
- Tahmin: `server_disk_1h` üzerinde `regr_slope(used, bucket)` son 7 ve 30 gün -> "X gün sonra dolar"; hızlı dolum
  son 1 sa ham `server_disk_metrics`'ten. Düz PG/SQLite'ta aynı SQL Go özet tablosundan (eşdeğer fonksiyon PG'de var; SQLite'ta Go içinde).

## 4. Ölçüm: sentetik 10 M satır (500 monitör x 60 sn x 14 gün) [ölçüldü]
Ortam: bu sunucu, `timescale/timescaledb:2.31.0-pg18` (PG 18.6), varsayılan ayarlar, sabit "200 - OK" mesajı
(gerçek veride mesaj çeşitliliği sıkıştırmayı düşürür; 620 gerçekçi veriyle yineleyecek).

| Ölçüm | Düz PG tablo | Hypertable |
|---|---|---|
| Boyut (dizinlerle) | 1.640 MB | 1.842 MB sıkıştırmasız -> **400 MB** (12/14 gün sıkıştırıldı) |
| Sıkıştırma oranı (parçalar) | - | 1.479 MB -> 36 MB, **40.7x**, ~4.4 B/satır |
| 7 gün uptime, 500 monitör, ham | 1.37 sn | 1.05 sn (sıkıştırılmış) |
| 7 gün uptime, 500 monitör, saatlik cagg | - | **35 ms** |
| 365 g/tüm dönem, 500 monitör, cagg | - | 67 ms |
| Tek monitör son 90 sonuç | - | 7 ms |
| p95 tek monitör 7 g ham | 14 ms | 17 ms |
| Cagg oluşturma (10 M satır) | - | 6.7 sn; cagg boyutu 31 MB |
| Düz tabloyu hypertable'a çevirme (`migrate_data`) | - | **83 sn**, tablo boyunca ACCESS EXCLUSIVE kilit |
| 12 günü sıkıştırma | - | 24 sn |

Bugünkü Bekci zaten Go tarafında saatlik/günlük özet tuttuğu için okuma tarafındaki kazanç "ham -> özet" değil
"Go UPSERT -> cagg" farkıdır: sonuç başına 4 deyim yerine 2, sıcak özet satırlarında kilit çekişmesi yok, disk 10-40x küçük,
ham saklama uzatılabilir, p95 ve sunucu metrikleri sütunlu olur. Yük karşılaştırması (eski Go özetleri vs cagg) 620'de.

## 5. Uyumluluk kararları

### 5.1 Eklenti algılama (üç düzey)
Açılışta: `SELECT extversion FROM pg_extension WHERE extname='timescaledb'` + `SHOW timescaledb.license`.
- `timescale` lisansı (TSL): tam mod (hypertable + sıkıştırma + cagg + saklama politikası).
- `apache` (`-oss` imajlar, bazı yönetilen PG'ler): yalnız hypertable + `drop_chunks`; sıkıştırma/cagg YOK -> özetler Go'da (düz PG yolu).
- Eklenti yok: düz PG yolu. Eklenti `pg_available_extensions`'ta var ama kurulu değilse yetki varsa `CREATE EXTENSION` denenir;
  `shared_preload_libraries` eksikse açık hata metni + belge bağlantısı.
- Uyarı: Ayarlar > Genel'de ve açılış logunda "Veritabanı: düz PostgreSQL, TimescaleDB yok: çalışır ama önerilmez, büyük
  ölçekte yavaşlar" şeridi (TR/EN), `/healthz` ayrıntısında `db_mode`.
- Mod değişimi tek yönlü ve migration ile: düz PG kurulumuna sonradan eklenti gelirse yeni migration sürümü hypertable'a çevirir.

### 5.2 Düz PG yolu (eklentisiz) — kodda kalır
Önemli: "ham veriden sorgula" tek başına yetmez, çünkü ham veri 14-30 gün tutuluyor; 365 günlük uptime için özet şart.
Öneri: düz PG ve SQLite **bugünkü Go özet tablolarını** (`stats_hourly`, `stats_daily`, `server_stats` rollup) kullanmaya devam eder;
yeni zaman serileri (server_metrics vb.) aynı adlı Go-bakımlı özet tablolara yazılır. Okuma sorguları her iki modda aynı tablo/görünüm
adlarını okur; fark yalnız yazma yolunda (`store` içinde `tsMode` bayrağı). Saklama: Go `deleteBatched` (bugünkü gibi).
CI: düz PG için kısa duman testi (kur, monitör, sonuç, grafik, uptime).

### 5.3 SQLite'ın geleceği — öneri kabul edildi, bir ekle
Öneri uygun: bu sürümde "kullanımdan kalkıyor" + `bekci migrate --from /data/uptime.db --to $DATABASE_URL` + sonraki büyük sürümde kaldır.
Ek öneri: kaldırma sürümünde bile **salt okunur SQLite okuyucu yalnız taşıma komutu için** kalsın (ikili içinde, panel olarak çalışmaz);
böylece 1.x SQLite kurulumu doğrudan 2.x'e geçebilir, ara sürüm şartı olmaz. `modernc.org/sqlite` bağımlılığı bu yüzden kalır (~boyut maliyeti).
Panel SQLite ile açılınca: log + arayüz şeridi "SQLite kullanımdan kalkıyor; taşıma: belge bağlantısı".

### 5.4 Yükseltme yolları [denenmedi; 630/640]
| Başlangıç | Yol | Kesinti |
|---|---|---|
| SQLite 1.6.0 | compose'a `timescale/timescaledb:2.31.0-pg18` servisi eklenir, `DATABASE_URL` verilir; panel açılışta boş PG + `/data/uptime.db` görürse taşıma önerir; ya da `bekci migrate`. Kopya: tablo tablo, 10 bin satırlık partiler, ilerleme, kesintide kaldığı yerden (hedefte `migrate_progress`), sonda satır sayısı karşılaştırması; SQLite dosyasına dokunmaz (geri dönüş = eski imaj) | canlı ölçeğinde (~200 K satır) saniyeler; 10 M satırda tahmini birkaç dakika |
| Düz PG 1.6.0 (kendi PG'si) | Aynı ana sürümde Timescale imajına geç (PG 18 -> `2.31.0-pg18`), `shared_preload_libraries=timescaledb`, açılışta migration: eklenti, PK değişimi, `create_hypertable(migrate_data)`, cagg, politika | ölçülen: 10 M satır 83 sn tam kilit. Büyük kurulumda alternatif: yeni hypertable'a son 2 gün önce, kalan geçmiş arka planda partilerle (panel açık) |
| `-postgres` imajı 1.6.0 (gömülü PG 18, `/data/postgres`) | Önerilen: `pg_dump` (eski imajla) -> yeni Timescale konteynerine geri yükle -> migration. Hızlı yol (veri klasörünü Timescale imajına bağlamak) aynı ana sürüm ve Alpine/musl derlemesi sayesinde muhtemelen çalışır ama 640'ta denenmeden önerilmez | dump boyutuna bağlı |
| Yönetilen PG (RDS vb.) | Eklenti yoksa düz PG modunda kalır, uyarı | yok |

Ana sürüm şartı: Timescale imajının PG ana sürümü mevcut veri klasörüyle aynı olmalı (18). Farklıysa yalnız dump/restore.
Timescale eklenti sürüm güncellemesi (2.31 -> 2.32): `ALTER EXTENSION timescaledb UPDATE` (açılışta, ilk bağlantı `-X`).

## 6. İmaj ve dağıtım
- Tek Bekci imajı (yalnız uygulama). Varsayılan compose: `bekci` + `db: timescale/timescaledb:2.31.0-pg18` (sabit, `latest` yok)
  + caddy. `Dockerfile.postgres` ve `:postgres`/`-postgres` etiketleri bu sürümde son kez yayınlanır (kullanımdan kalkıyor notuyla).
- Bekci imajına Timescale gömülmez: hem imaj küçük kalır hem TSL ikilileri Bekci tarafından dağıtılmaz (bölüm 7).
- Seçim pg18: mevcut gömülü PG 18 ve CI `postgres:18-alpine` ile aynı ana sürüm. `-oss` etiketi KULLANILMAZ (sıkıştırma/cagg yok).
- PG ayarı önerisi (compose `command`): `shared_buffers` RAM'in %25'i, `timescaledb.max_background_workers` >= 8. Donanım tablosu 620'de.

## 7. Lisans notu (TSL) [belge özeti; hukuki inceleme değildir]
- TimescaleDB çekirdeği Apache 2.0; **sıkıştırma (columnstore), sürekli özetler, saklama/sıkıştırma politikaları ve toolkit
  Timescale License (TSL)** altında. Kaynak: timescale.com/legal/licenses (TSL 2.0) — 630'da metin yeniden doğrulanacak.
- TSL, yazılımı kendi sunucunda kullanmayı ve onu kullanan "katma değerli ürün/hizmet" sunmayı serbest bırakır; yasakladığı,
  TimescaleDB'yi **veritabanı hizmeti olarak** (DBaaS: müşterinin şema/DDL tanımlayabildiği barındırılan veritabanı) sunmaktır.
- Bekci'ye etkisi: Bekci veritabanını yalnız kendi şeması için kullanır, kullanıcıya SQL/DDL erişimi vermez -> kendi sunucuda
  kurulum ve "barındırılan Bekci" hizmeti ikisi de katma değerli ürün kapsamındadır. Bekci AGPL-3.0'dır; Bekci kodu TSL kodu
  içermez, yalnız ağ üzerinden SQL ile konuşur, Timescale ayrı resmi imaj olarak çekilir (Bekci dağıtmaz) -> lisans karışması yok.
- Bekci kurulum belgesine kısa not: "Varsayılan veritabanı imajı TSL lisanslı özellikler içerir; kendi sunucunuzda kullanım
  serbesttir. Yalnız Apache sürümü isteyenler `-oss` imajı ya da düz PG kullanabilir (özetler Go tarafında hesaplanır)."
- Toolkit kullanılmadığı için (bölüm 3.4) ek lisans yüzeyi yok.

## 8. Riskler
- Sıkıştırılmış parçaya geç gelen veri (ajan tamponundan saatler sonra gelen sonuç): Timescale 2.x sıkıştırılmış parçaya INSERT'e
  izin verir (yavaş); `compress_after` 7 gün olduğundan pratikte sorun yok.
- `ResetStats` (monitör istatistiklerini sıfırlama) ve monitör silme: hypertable'da DELETE sıkıştırılmış parçada pahalı ->
  monitör silmede ham satırlar saklama süresiyle düşer, okuma `monitors` JOIN'i ile filtrelenir; `ResetStats` için
  `stats_reset_at` sütunu (okuma alt sınırı) önerilir. Cagg'da da aynı sınır.
- Yedek/dışa aktarma (`internal/backup`): `pg_dump` hypertable ile `timescaledb_pre_restore()/post_restore()` ister;
  Bekci'nin JSON dışa aktarması etkilenmez.
- Testler: `storetest` PG'yi Timescale imajıyla da çalıştırmalı; CI matrisi sqlite / pg / pg+ts.

## 9. Uygulama planı ve parçalara uyum
Mevcut 610-640 bölümlemesi uyuyor; öneriler:
- **610 şema**: algılama (3 düzey), `tsMode`, heartbeats hypertable + `stats_hourly` cagg (aynı ad) + `stats_daily_utc`,
  histogram sütunları (Go özetine de), saklama/sıkıştırma politikaları ve `AppSettings` yeni aralıkları, `ResetStats` sınırı,
  `mesh_samples`. **`server_metrics*` sütunlu şemayı da 610'a al** (JSON -> sütun dönüşümü şema işidir; 660 hazır tabloyla başlar).
  CI: pg+ts matrisi.
- **620 sorgular + performans**: okuma yolları (yerel gün gruplama, pencere birleştirme, p95), eski/yeni sonuç eşitlik testi,
  100/1.000/10.000 monitör ölçümü, parça aralığı kuralı.
- **630 kurulum/taşıma/belgeler**: compose, `bekci migrate` (SQLite -> PG, devam edebilir), `-postgres` imajı için dump yolu,
  kullanımdan kalkma uyarıları, belgeler TR/EN, lisans notu, sürüm notu taslağı, CI etiketleri.
- **640 gerçek kullanım denetimi**: değişiklik yok.
- **650 envanter**: kayıt tabloları (3.5) + ajan toplama. `servers` kayıt tablosu kararı (probes genişler mi, ayrı mı) 650 başında.
- **660 olaylar + tahmin**: `server_events` hypertable, `server_disk_1h` + regr_slope tahmini, uyarı.
- **670-695**: değişiklik yok. 680 (güvenlik) onayı olmadan 690 başlamaz.
Öneri (farklılık): 610 büyük; yük dengesi için "610a heartbeats/özetler" ve "610b sunucu metrikleri sütunlu şema + mesh_samples"
olarak iki commit grubu, tek iş numarası altında yeterli. Ayrı iş numarası gerekmez.

## 10. Açık sorular (kullanıcı)
1. Ham kontrol kaydı varsayılan saklaması 14 -> 30 gün olsun mu? (Öneri: 30; 7 gün sonrası sıkıştırıldığı için ek disk küçük, 620 ölçecek.)
2. Yerel gün çubuklarında yarım saatlik saat dilimi yuvarlaması kabul mü? (Öneri: kabul, belgeye yaz.)
3. `-postgres` imajı kullanıcıları için hızlı yol (veri klasörünü Timescale imajına bağlama) desteklensin mi, yoksa yalnız dump/restore?
