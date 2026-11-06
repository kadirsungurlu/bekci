# Topoloji API ölçek ölçümü (iş 702)

Ölçüm: `BEKCI_PERF_TOPO=1 UPTIME_TEST_PG=<timescale> go test -v -run TestTopologyScale ./internal/api/`
(`internal/api/topology_perf_test.go`; ek: `BEKCI_PERF_PARTS=1` sorgu başına süre, `BEKCI_PERF_MON/SRV/LOC/REPS`).

Ortam: Timescale 2.31 (pg18, tmpfs) aynı makinede, 8 çekirdek, başka işlerle yük ortalaması ~8.
Tohum: 1000 http monitör (3 nokta + yerel, 2 kanal, 1 etiket, 30 sonuç, %3 açık olay), 100 sunucu (son metrik),
30 nokta (870 mesh bağı), 20 kanal, 30 etiket, 20 müşteri. Yönetici oturumu, gzip açık, 40 istek.

| Görünüm | Medyan | p95 | Ham | gzip |
|---|---|---|---|---|
| `/api/topology` (tam) | 137 ms | 210 ms | 678 KB | 59 KB |
| `/api/topology?view=summary` | 109 ms | 148 ms | 333 KB | 37 KB |

İlk istek (soğuk) 394 ms: sunucu son metrikleri bellekte yokken DB'den okunur, sonra önbellektedir.

## Kararlar
- Sıralı okuma 181 ms idi; okumalar `errgroup` ile en çok 4 paralel (SQLite'ta tek bağlantı, sıralı kalır).
  En pahalı iki sorgu: `ListUsers` 48 ms (kısıtlı kullanıcı başına 4 sorgu), `ListMonitors` 25 ms. Yalnız yönetici
  `ListUsers` öder. Müşteri sayısı çok artarsa önce bu toplu sorguya çevrilir.
- Son 30 değer istek başına DB'den okunmaz: motorda bellek halkası (`engine/pinghist.go`), açılışta `RecentPings` ile dolar.
- Panel kendisi sıkıştırır (`writeTopology`, BestSpeed): her kurulum Caddy arkasında değil.
- SSE ile yalnız değişenleri göndermek gerekmedi: özet görünüm 5 sn yoklamada 37 KB. Arayüz `view=summary` kullanır,
  konum dökümü (`locations`) ve mesh gerektiğinde tam görünüm alınır.

## İlişki sayfası arayüz ölçümü (703, 2026-11-05)

Tohum: 1000 monitör, 100 sunucu, 30 nokta, 6 kanal (`web/e2e/topotohum.mjs`, 472 KB JSON, tarayıcıda verilir).
Ortam: bu makinedeki Chrome, başsız, yazılımsal çizim; makinede başka işler de çalışıyor, sonuçlar turdan tura değişiyor.

| Ölçüm | Hedef | Sonuç (sanallaştırma sonrası) | Önce |
|---|---|---|---|
| Çizilen satır (DOM) | - | ~58 | 1000+ |
| İskelet boyası (first-paint, tam yenileme) | algılanan < 300 ms | 156-216 ms | yok |
| İlk satır, uygulama içi geçiş | < 300 ms | 250-298 ms, soğuk ilk açılış 417 ms | 246-294 / 504 ms |
| İlk satır, tam yenileme | - | 1050-1200 ms | ölçülmedi |
| Seçim, tıklama işleyicisi + tepki (ana iş parçacığı) | < 50 ms | 32-38 ms | 16-193 ms (yalnız JS) |
| Seçim, tıklamadan Commit'e (trace) | - | 48-118, ortanca ~68 ms | 90-338 ms |
| Seçim, PerformanceEventTiming süresi (sunum dahil, 8 ms kuantum) | - | 96-160 ms | - |
| Kaydırma, seçili durumda kare süresi | takılma yok | ortanca 18 ms, p95 28 ms, en çok 70 ms | 20 / 33 / 63 ms |

Sonuç: ana iş parçacığındaki iş hedefin altında (<50 ms). Ekrana yansıma süresinin kalanı bu ortamın yazılımsal çizimidir (Paint ~10, Layerize ~7, Commit ~4 ms, ayrıca rasterleştirme); GPU'lu tarayıcıda ayrıca ÖLÇÜLMEDİ. Tam yenilemede satır süresi paketin indirilip çalışmasına bağlı, iskelet bu aralığı karşılar.

Kararlar:
- Sütunlar sanal liste (`lib/topovirt.ts`): satır yükseklikleri hesaplı (`METRICS` tek kaynak, CSS değişkeni olarak aktarılır), konum önek toplamından, kaydırma 120 px adımla nicelenir; çizgi uçları DOM'dan değil bu konumdan gelir, görünmeyen satır da ölçülebilir.
- Kanallar sanal değil (en çok birkaç satır, sütun sonunda gerçek DOM).
- Kapalı grup hücreleri satır başına `<i>` yerine durum başına tek SVG yol; seçimde binlerce öğenin stil hesabını kaldırdı.
- Satır geçiş animasyonları kaldırıldı; geniş ekranda `.sutun`, `.sorun` için `contain:content`.
- Açılış: `index.html` içinde `public/boot.js` + CSS iskeleti (CSP satır içi betiğe izin vermez). `main.ts` yalnız CSS yükler, uygulama ilk kareden sonra `start.ts` ile gelir; böylece iskelet paket çalışmadan boyanır.
- Doğrulama: `web/e2e/topoloji.mjs` büyük tohum bölümü (satır sınırı, çizgi ucu, ok tuşu/End/Home pencere dışı, kapalı grup hücresi).
