# Performans ölçümü: Postgres + TimescaleDB okuma yolları (iş 620)

Tarih: 2026-11-03/04. Araçlar: `scripts/perf/` (tohum, ölçüm, HTTP), `internal/store/perf_test.go`.
Tasarım: `TIMESCALE-TASARIM.md`.

## Kısa cevap: "Grafik ve uptime hesaplarında sunucu yükü azalacak mı?"

**Karşılaştırmanın neye göre yapıldığına bağlı; bugünkü sürüme göre okuma yükü AZALMIYOR.**

- Bekci bugün ham veriden hesaplamıyor: Go kendi `stats_hourly` / `stats_daily` tablolarını tutuyor. Asıl karşılaştırma bu.
- **Ham veriden hesaba göre:** evet, 10-100 kat azalıyor (grafik 90 g 740 -> 21 ms; durum sayfası 90 g 24 sn -> 0,2 sn).
- **Bugünkü Go tablolarına göre okuma artıyor:** tek monitör grafiği 1 -> 10-25 ms; tüm monitör okumaları 2-10 kat CPU; durum sayfasının ilk açılışı 4-13 kat yavaş. Mutlak değerler çoğu ekranda kullanıcının fark etmeyeceği düzeyde, ama yük azalmıyor.
- **Yazma tarafı biraz azalıyor:** sonuç başına veritabanı CPU'su %3-23 düşük (ölçek büyüdükçe fark açılıyor), en yüksek yazma hızı %38 yüksek (483/350 sn).
- **Asıl kazanç disk:** ham veri 20 kat küçülüyor (151 -> 7,3 B/satır). Ham veriyi aylarca saklamayı mümkün kılıyor; bugünkü tabloyla aynı süre saklamak 1.000 monitörde yaklaşık 220 GB olurdu.
- **Arka plan işleri ihmal edilebilir:** 1.000 monitörde özet + sıkıştırma ortalama %0,07 çekirdek (ayrıntı aşağıda).

Sonuç: geçişin gerekçesi okuma hızı değil, disk ve uzun ham veri saklama. Okuma tarafında bedel ödeniyor; bu bedel kabul edilebilir, ama "yük azalır" denemez.

## Yöntem ve sınırlar

- Sunucu: paylaşımlı, 8 çekirdek / 15 GB, başka projeler çalışıyor. Sonuçlar gürültülüdür; ±%30 fark anlamlı sayılmamalı.
- Veritabanı: `timescale/timescaledb:2.31.0-pg18`, konteyner sınırı 8 GB, `shared_buffers=2GB`, `work_mem=32MB`.
- Her ölçek için 5 konum, 30 sn hedefi. Sütunlar: **ham** (heartbeats'ten doğrudan), **Go** (bugünkü özet tabloları, `DB_TIMESCALE=off` yolu), **yeni** (sürekli özet).
- Süre: `EXPLAIN ANALYZE` ortalaması (planlama dahil), CPU: konteyner cgroup sayacı, blok: paylaşılan arabellek okuması.

| Ölçek | Tam yoğunluk | Geri kalanı | Ham satır | Eski yol ham verisi |
|---|---|---|---|---|
| 100 | 90 gün | yok | 130 M | son 7 gün |
| 1.000 | 7 gün | 83 gün seyrek | ~100 M | son 2 gün |
| 10.000 | son ~1,5 saat | 89 gün seyrek | ~30 M | son 3 saat |

"Seyrek" gün: monitör başına saatte 1 sonuç. Saatlik/günlük özet satır sayısı tam yoğunlukla aynıdır, yalnız ham satır azdır; özet okumaları bu yüzden temsilîdir.

**Denenmedi (ölçülmüş sanılmasın):**

- 1.000 ve 10.000 monitörde ham sorgular; ölçek bu sütunda doğrusaldır.
- 10.000 monitörde gerçek tam yük, 90 günlük tam ham veri, 90 günlük pencere okumaları. Bu sunucunun diski yetmedi (aşağıda "Olay").
- 10.000 ölçeğinde günlük özet yalnız son 31 gündür (ilk doldurma bitirilmedi); durum sayfası 90 g satırı bu yüzden gerçeğinden düşük olabilir.
- 10.000 ölçeğinin `yeni` tekil sorgu satırları (grafik, uptime) çıktı ayıklanırken kayboldu; bu satırlarda HTTP süreleri ve 100/1.000 satırları geçerlidir.
- Süren saat içindeki ham yük ölçüm anına bağlıdır: 1.000'de ~250-500 bin satır; en kötü durum 60 dk ile 600 bin satır.

## Sorgu karşılaştırması (100 monitör)

süre ms / CPU ms / blok / satır

| Sorgu | Ham | Go tablosu | Yeni |
|---|---|---|---|
| Grafik 7 g | 78,8 / 75 / 14168 / 13311 | 0,7 / 1 / 28 / 161 | 8,3 / 17 / 54 / 181 |
| Grafik 30 g | 281 / 298 / 17388 / 13656 | 1,1 / 2 / 105 / 690 | 11,8 / 14 / 139 / 710 |
| Grafik 90 g günlük | 740 / 525 / 25776 / 14555 | 1,0 / 1 / 15 / 90 | 21 / 13 / 361 / 2090 |
| Uptime 30 g tek | 155 / 503 / 14775 / 13656 | 3,0 / 4 / 15 / 32 | 12,5 / 32 / 62 / 99 |
| Uptime 90 g tek | 324 / 958 / 17766 / 14555 | 3,4 / 1 / 21 / 92 | 33 / 12 / 81 / 159 |
| Durum sayfası 50 monitör 90 g | 24368 / 21003 / 1281097 / 727768 | 3,4 / 11 / 638 / 4500 | 205 / 822 / 7422 / 208418 |
| Durum sayfası 50 monitör 24 s | - | 4,5 / 6 / 608 / 2300 | 11,9 / 27 / 1351 / 2218 |
| Liste uptime tümü 30 g | 730 / 1900 / 153121 / 1365630 | 13,9 / 14 / 218 / 18200 | 179 / 435 / 6310 / 17862 |
| Metrikler 30 g | - | 65,7 / 56 / 18497 / 69000 | 137 / 336 / 4485 / 73630 |
| Liste 24 s (HourlyAll) | - | 2,4 / 5 / 608 / 2300 | 22,4 / 25 / 2448 / 4330 |

## Sorgu karşılaştırması (1.000 ve 10.000 monitör)

süre ms / CPU ms

| Sorgu | 1.000 Go | 1.000 yeni | 10.000 Go | 10.000 yeni |
|---|---|---|---|---|
| Grafik 7 g | 0,6 / 2 | 9,2 / 24 | 0,3 / 1 | kayıp |
| Grafik 30 g | 1,2 / 3 | 11,5 / 18 | 1,2 / 3 | kayıp |
| Grafik 90 g günlük | 0,7 / 2 | 24,5 / 29 | 0,5 / 1 | kayıp |
| Uptime 30 g tek | 2,7 / 4 | 24,5 / 44 | 2,4 / 3 | kayıp |
| Uptime 90 g tek | 3,7 / 2 | 30,4 / 36 | 3,3 / 1 | kayıp |
| Durum 50 monitör 90 g | 8,9 / 22 | 272 / 657 | 5,7 / 15 | kayıp |
| Liste uptime tümü 30 g | 98 / 314 | 799 / 1810 | 529 / 1513 | 1006 / 2967 |
| Metrikler 30 g | 294 / 1022 | 524 / 1590 | 1606 / 6684 | 3092 / 7888 |
| Liste 24 s (HourlyAll) | 30 / 78 | 535 / 589 | 460 / 720 | 422 / 1332 |

## Sayfa süreleri (HTTP, ms, Go -> yeni, önbellek kapalı)

| Uç | 100 | 1.000 | 10.000 |
|---|---|---|---|
| Durum sayfası ilk açılış | 78 -> 329 | 80 -> 318 | 98 -> 1304 |
| Durum sayfası önbellekli | 2,2 -> 2,2 | 1,7 -> 2,2 | 2,2 -> 2,2 |
| `/api/summary` | 33 -> 52 | 241 -> 765 | 1964 -> 1747 |
| Monitör listesi | 81 -> 325 | 557 -> 2118 | 4041 -> 6046 |
| Monitör ayrıntı | 62 -> 251 | 248 -> 896 | 1693 -> 1997 |
| Olaylar | 8 -> 3,7 | 8,9 -> 11 | 8,9 -> 9,3 |
| Topoloji | 23 -> 19 | 54 -> 54 | 278 -> 353 |
| Grafik 7 g | 10,5 -> 28 | 9,1 -> 31 | 11,7 -> 28 |
| Grafik 30 g | 16 -> 36 | 13,6 -> 38 | 17,4 -> 34 |
| Grafik 90 g | 13,9 -> 51 | 10,3 -> 41 | 11,2 -> 33 |

## Yavaşlığın nedenleri ve yapılan düzeltmeler

`EXPLAIN` sonucu (1.000 monitör, HourlyAll, 395 ms):

- 280 ms: **süren saatin ham satırları** (245 bin satır) gerçek zamanlı toplanıyor. Maliyet monitör sayısıyla doğrusal, her tüm-monitör okumasında yeniden ödeniyor.
- 62 ms: **planlama** (görünüm + hypertable parçaları). Tek monitör grafiğinin 10 ms'inin de çoğu bu.
- Özet satırlarının kendisi ucuz.

Ölçümle bulunan ve düzeltilen tasarım hatası: özet politikası `end_offset 1 sa, 15 dk`ydı, yani her okuma 1-2,25 saatlik ham veriyi yeniden topluyordu (1.000 monitörde 2,7 M, 10.000'de 27 M satıra kadar). Şimdi `end_offset 0, 5 dk`: yalnız süren saat ham okunur.

Eklenen paylaşım: tüm monitörleri tarayan okumalar (liste, özet, `/metrics`) `READ_CACHE_SEC` (varsayılan 15) süre paylaşılır. Tablolar önbellek kapalı ölçümdür; önbellekle bu sorgular en çok 15 sn'de bir çalışır, veri en çok 15 sn gecikir. Seçili monitör süzgeçli okumalar (müşteri rolü) paylaşılmaz.

Durum sayfası ilk açılışı yerel gün için saatlik özetten topluyor (tamsayı zamanlı hiyerarşik özet saat dilimi kabul etmiyor). Genel sayfa önbellekli olduğu için bu maliyet önbellek süresinde bir kez ödenir.

## Yazma

Gerçek yol (`RecordBeat`, 250 ms grup commit):

| Ölçek, hedef | CPU çekirdek (Go / yeni) | CPU ms / 1000 sonuç (Go / yeni) |
|---|---|---|
| 100, 16/sn | 0,060 / 0,059 | 3780 / 3659 |
| 1.000, 166/sn | 0,304 / 0,268 | 1832 / 1616 |
| 10.000, 1666/sn (ulaşılamadı) | 0,541 / 0,619 | 1987 / 1526 |

**En yüksek hız: yeni 483/sn, Go 316-350/sn.** Bu değer DB boyutundan bağımsızdır: `internal/store/beats_batch.go` tek gorutinde, işlem içinde satır satır yazıyor.

**Kritik bulgu: 10.000 monitör x 5 konum x 30 sn = 1.667 sonuç/sn. Bugünkü yazma yolu buna hiçbir kipte yetişmez** (yeni ~2.900, Go ~2.000 monitör x 5 konum sınırı). 10.000 ölçeği için çok satırlı INSERT/COPY ile toplu yazım gerekir; sonra ölçüm tekrarlanmalı.

## Disk

| Ölçek | Ham (sıkıştırma öncesi -> sonrası) | Oran | Saatlik özet | Günlük özet | DB toplam |
|---|---|---|---|---|---|
| 100 (90 g tam) | 19,68 -> 0,95 GB | 20,7x | 59 MB | 2,9 MB | 1,24 GB |
| 1.000 (seyrek dahil) | 13,44 -> 0,69 GB | 19,6x | 577 MB | 25 MB | 3,39 GB |
| 10.000 (seyrek) | 3,25 -> 1,55 GB | 2,1x (seyrek veri sıkışmaz) | 5,68 GB | ~250 MB (hesap) | 7,88 GB |

Düz PG ham satırı dizinle ~175 B, sıkışık 7,3 B. Go özet tabloları sürekli özetle aynı boyutta (5,73 GB).

## Bellek (RAM)

| Ölçek | Bir günlük açık parça | Sıcak veri (arabellekte) | PG belleği | Panel RSS |
|---|---|---|---|---|
| 100 | 193 MB | 866 MB | 2,2 GB | 37 MB |
| 1.000 | ~2 GB | 393 MB | 2,8 GB | 37 MB |
| 10.000 | ~21 GB (hesap) | 1,4 GB (seyrek) | 2,7 GB | 67 MB |

## Arka plan işleri

Doğrudan `run_job` ölçümü betik hatası yüzünden geçersiz çıktı (betik düzeltildi, yeniden denenmedi). Değerler tohum adımlarından:

- Saatlik özet: 1.000 monitörde bir tam gün 29-32 sn tek çekirdek.
- Sıkıştırma: 14,4 M satır 31 sn (31 sn CPU), yaklaşık 460 bin satır/sn.
- Günlük özet ilk doldurma: 1.000 monitör 90 gün 568 sn; 10.000 monitör 90 gün ~2 saat sürer (yalnız ilk kurulum / dönüşüm; sonrası yalnız değişen günler).
- Toplam (1.000 monitör): günde ~65 sn = **%0,07 çekirdek**. 10.000'de doğrusal ~%0,7.

## Önerilen kaynak

Kullanıcı tahmini: 100 -> 4 GB, 1.000 -> 16 GB, 10.000 -> 32-64 GB. **Ölçüm tahmini doğruluyor; 1.000 için 8 GB da yeter.** 5 konum, 30 sn, ham saklama 90 gün varsayımı.

| Ölçek | RAM | `shared_buffers` | CPU | Disk (90 g ham) | Disk (14 g ham) | Dayanak |
|---|---|---|---|---|---|---|
| 100 | 4 GB (2 GB alt sınır) | 1 GB | 2 çekirdek | 15 GB | 5 GB | ölçüldü |
| 1.000 | 8 GB, rahat 16 GB | 2-4 GB | 4 çekirdek | 40 GB | 20 GB | ölçüldü (seyrek veri + hesap) |
| 10.000 | 32 GB alt sınır, öneri 64 GB, NVMe | 8-16 GB | 8+ çekirdek | 200 GB | 80 GB | **hesap, denenmedi**; yazma yolu düzeltilmeden çalışmaz |

Disk hesabı: sıkışık ham 7,3 B/satır + açık parçalar 150 B/satır + özetler. Açık parça sayısı `tsChunkPlan` ile sınırlı.

## tsChunkPlan

Sunucu günlük satır tahminine göre parça ve sıkıştırma süresini seçer: 1 parça ~50 M satır; günde >=50 M satır: sıkıştırma 1 gün; >=5 M: 2 gün; aksi 7 gün. Gerekçe: 1.000 monitörde 7 günlük açık veri 15 GB eder ve 8 GB RAM'e sığmaz; 10.000'de 150 GB. Yalnız açılışta çalışır.

## Olay: disk doldu

İlk 10.000 ölçümünde sunucu diski %100 doldu, Postgres ölçüm konteyneri düştü (2026-11-04 gecesi, 03:00-03:30 yerel arası; tam süre bilinmiyor). Neden: UTC gün dönümünde başlayan tohum 1 tam gün (144 M satır, ~22 GB) yazdı, üstüne iki veritabanında 5,7 GB özet ve 8 GB WAL sınırı eklendi. Kendi konteyner ve volume'um silinerek yer açıldı, sonraki ölçümler disk korumasıyla (`boş < 8 GB` -> ölçümü sil) yapıldı. **10.000 ölçeği paylaşımlı sunucuda yapılmamalı.**

## Eşitlik testi

`TestTimescaleReadParity` (`internal/store/timescale_read_test.go`): düz yol, taze Timescale, dönüşmüş Timescale aynı veride birebir karşılaştırılır. Üç saat dilimi (İstanbul, New York, Kolkata), özet yenilenmeden, yenilenince, sıfırlama sonrası. Süren saat dahil geçti.
