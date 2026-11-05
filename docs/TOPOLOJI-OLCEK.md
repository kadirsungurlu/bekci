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
