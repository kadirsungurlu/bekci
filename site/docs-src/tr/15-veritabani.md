---
id: database
title: Veritabanı, taşıma ve donanım
nav: Veritabanı ve taşıma
description: Bekci'nin varsayılan PostgreSQL + TimescaleDB kurulumu, SQLite ve düz PostgreSQL'den yükseltme, donanım önerisi, yedek ve geri dönüş.
section: Bakım ve başvuru
order: 15
slug: veritabani
---

Bekci'nin varsayılan veritabanı **PostgreSQL 18 + TimescaleDB**'dir. Ölçüm verisi sıkıştırılır, grafikler önceden hesaplanmış özetlerden gelir ve eski veri otomatik silinir. Bu sayfa yeni kurulumu, eski kurulumlardan yükseltmeyi ve donanım seçimini anlatır.

## Hangi kurulum? {#secenekler}

| Kurulum | Durum | Not |
|---|---|---|
| **PostgreSQL + TimescaleDB** | Varsayılan, önerilen | [Docker Compose](/docs/kurulum/docker-compose/) ile kurulur. İmaj sürümü sabittir: `timescale/timescaledb:2.31.0-pg18`. |
| **Düz PostgreSQL** (RDS, Cloud SQL vb.) | Çalışır, önerilmez | Eklenti yoksa Bekci çalışır ve uyarı verir. Büyük ölçekte yavaşlar: sıkıştırma ve özetler yoktur. |
| **SQLite** | Kullanımdan kalkıyor | Bu sürümde çalışır, açılışta uyarı verir. Sonraki büyük sürümde kaldırılır. |
| **Gömülü PostgreSQL** (`:postgres` imajı) | Kullanımdan kalkıyor | Bu sürümde çalışır, açılışta uyarı verir. Sonraki büyük sürümde kaldırılır. |

Hangi kipte çalıştığınızı **Ayarlar → Genel** gösterir: SQLite ya da düz PostgreSQL kullanıyorsanız sayfanın başında uyarı şeridi görürsünüz.

## Yeni kurulum {#yeni-kurulum}

[Docker Compose](/docs/kurulum/docker-compose/) ya da [Caddy ile HTTPS](/docs/kurulum/caddy/) rehberini izleyin; ikisi de bu kurulumu kullanır. Yönetici hesabını açtıktan sonra ek bir işlem gerekmez: Bekci eklentiyi etkinleştirir, tabloları oluşturur.

`POSTGRES_PASSWORD` yalnız harf ve rakamdan oluşmalıdır; bağlantı adresinin içine yazıldığı için `@`, `/`, `:` gibi işaretler adresi bozar. Üretmek için:

```bash
openssl rand -hex 24
```

## SQLite'tan yükseltme {#sqlite}

Taşıma `uptime migrate` komutuyla yapılır. Komut SQLite dosyasını **değiştirmez**: önce bir kopyasını alır, kopyayı yeni sürümün şemasına yükseltir ve oradan PostgreSQL'e aktarır. Bekci taşıma boyunca kapalı olmalıdır; kontroller bu sürede çalışmaz.

1. **Yedek alın.** [Tam yedek](/docs/guncelleme-yedek/#tam-yedek) en güvenlisidir.
2. **`docker-compose.yml` dosyanıza `db` servisini ekleyin**, `bekci` servisine `DATABASE_URL` yazın ve `.env` dosyasına `POSTGRES_PASSWORD` tanımlayın. Örnek: [Docker Compose rehberi](/docs/kurulum/docker-compose/). `bekci` servisinin `/data` birimini **olduğu gibi bırakın**; SQLite dosyası orada.
3. **Veritabanını başlatın, Bekci'yi durdurun:**

   ```bash
   docker compose pull
   docker compose stop bekci
   docker compose up -d db
   ```

4. **Önce deneyin.** Bu komut hiçbir şey yazmaz; ne kadar veri taşınacağını gösterir ve engel varsa söyler:

   ```bash
   docker compose run --rm bekci migrate --dry-run
   ```

5. **Taşıyın:**

   ```bash
   docker compose run --rm bekci migrate
   ```

6. **Başlatın:**

   ```bash
   docker compose up -d
   ```

> [!CHECK]
> Komut `Taşıma tamam: … satır, kip timescale` satırıyla bitmeli. Panelde monitörleri, geçmiş grafikleri ve olayları görmelisiniz. Her tablonun satır sayısı komutun sonunda karşılaştırılır; fark varsa komut hata verir ve taşıma tamamlanmış sayılmaz.

**Ne kadar sürer?** Ölçümde 3,1 milyon satır saniyede yaklaşık 31.000 satır hızla aktarıldı; sıkıştırmalı tablolara dönüşümle birlikte toplam 5 dakika 24 saniye sürdü (paylaşımlı bir sunucuda). Dönüşüm aşaması ilerleme göstermez; büyük veride dakikalarca sessiz kalabilir, komutu kesmeyin.

**Kesilirse:** aynı komutu yeniden çalıştırın; kaldığı yerden devam eder. Yarım taşımayla panel açılmaz, `SQLite verisi taşınmadı` hatası verir. Bu bir korumadır: yarım veriyle çalışmayı önler.

**Hedef boş değilse:** komut başlamaz, çünkü var olan veriyi ezmez. Bu durum genelde `DATABASE_URL` ile eskiden boş bir kurulum açıldığında olur. Eski veriyi istemiyorsanız ve yeni bir veritabanıyla başlamak istiyorsanız `.env` dosyasına `SQLITE_IGNORE=1` yazın; panel SQLite dosyasını yok sayarak açılır.

### Komut seçenekleri {#migrate-secenekleri}

| Seçenek | Açıklama |
|---|---|
| `--from` | SQLite dosyası. Varsayılan: `/data/uptime.db` |
| `--to` | Hedef adres. Varsayılan: `DATABASE_URL` |
| `--batch` | Tek işlemde kopyalanan satır sayısı |
| `--work-dir` | Geçici kopyanın klasörü. `/data`'da yer yoksa başka bir birim verin; SQLite dosyası kadar boş alan gerekir. |
| `--restart` | Yarım kalan taşımayı silip baştan başlar |
| `--dry-run` | Yazmadan denetler |

## Düz PostgreSQL'den yükseltme {#duz-postgresql}

Bekci'ye `DATABASE_URL` ile bağlı bir PostgreSQL'iniz varsa veriyi taşımanız gerekmez: sunucuya eklentiyi kurmanız yeterlidir. Bekci açılışta tabloları kendisi dönüştürür.

**Compose'daki `postgres:18-alpine` konteyneri:** `db` servisinin imajını değiştirin ve var olan veri klasörü eklentiyi yüklesin diye `command` ekleyin:

```yaml
  db:
    image: timescale/timescaledb:2.31.0-pg18
    command: postgres -c shared_preload_libraries=timescaledb -c max_worker_processes=24
```

```bash
docker compose pull
docker compose up -d
```

Ana sürüm aynı olmalıdır (18). Eski ana sürümde ya da Debian tabanlı `postgres:18` imajında bu yolu kullanmayın; dil sıralama kütüphanesi farkı dizinleri bozabilir. Bu durumda aşağıdaki döküm yolunu izleyin.

**Döküm ve geri yükleme:** eski sunucudan döküm alın, yeni `db` servisini boş başlatın, geri yükleyin, Bekci'yi başlatın:

```bash
pg_dump --format=custom --no-owner --file=bekci.dump "⟦postgres://kullanıcı:parola@eski-sunucu:5432/bekci⟧"
docker compose up -d db
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < bekci.dump
docker compose up -d
```

**Yönetilen PostgreSQL (RDS, Cloud SQL vb.):** servis TimescaleDB sunmuyorsa Bekci düz kipte çalışmaya devam eder. Çalışır ama önerilmez; büyük ölçekte yavaşlar.

Mevcut tablolar panelin ilk açılışında hypertable'a dönüşür. Bu sırada panel kapalıdır; süre veri miktarına bağlıdır (ölçüm: 1,2 milyon satır, paylaşımlı sunucu, 26 saniye ile 2,5 dakika arası).

## Gömülü PostgreSQL'den yükseltme {#gomulu-postgresql}

`:postgres` imajı kullanımdan kalkıyor. Veriyi dökümle ayrı `db` servisine aktarın:

```bash
docker exec ⟦bekci⟧ pg_dump -h /run/postgresql -U postgres --format=custom --no-owner --file=/data/backups/tasima.dump uptime
docker cp ⟦bekci⟧:/data/backups/tasima.dump ./tasima.dump
docker stop ⟦bekci⟧
```

Compose dosyanızı [varsayılan kuruluma](/docs/kurulum/docker-compose/) çevirin (imaj etiketi `postgres` yerine sürüm numarası), sonra:

```bash
docker compose up -d db
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < tasima.dump
docker compose up -d
```

Eski `/data/postgres` klasörü silinmez; yeni kurulumun sorunsuz çalıştığından emin olana kadar saklayın.

## Donanım önerisi {#donanim}

Varsayım: 5 konum, 30 saniye kontrol aralığı. 100 ve 1.000 monitör satırları ölçüme dayanır; **10.000 satırı hesaptır, tam yükle denenmedi.**

| Monitör | RAM | `shared_buffers` | CPU | Disk (ham veri 90 gün) | Disk (ham veri 14 gün) |
|---|---|---|---|---|---|
| 100 | 4 GB (alt sınır 2 GB) | 1 GB | 2 çekirdek | 15 GB | 5 GB |
| 1.000 | 8 GB, rahat 16 GB | 2-4 GB | 4 çekirdek | 40 GB | 20 GB |
| 10.000 | 32 GB alt sınır, öneri 64 GB, NVMe | 8-16 GB | 8+ çekirdek | 200 GB | 80 GB |

> [!IMPORTANT]
> Tek panel ölçümde saniyede en çok yaklaşık 480 sonuç yazabildi; bu değer 5 konum ve 30 saniyeyle yaklaşık 2.900 monitöre denk gelir. Bu sınırın üstü bu sürümde desteklenmez: daha büyük ölçekte kontrol aralığını uzatın ya da konum sayısını azaltın.

`shared_buffers` değerini `db` servisinde ayarlayın:

```yaml
  db:
    command: postgres -c shared_preload_libraries=timescaledb -c max_worker_processes=24 -c shared_buffers=⟦2GB⟧
```

Bellek sınırı olan sunucuda `db` servisine `TS_TUNE_MEMORY: 400MB` ortam değişkenini verin; imaj ilk açılışta ayarları makinenin tüm belleğine göre seçer. Salt kurulu `timescale` imajı yaklaşık 1,6 GB yer kaplar.

## Yedek ve geri yükleme {#yedek}

Bekci her gece `/data/backups/` altına `uptime-TARİH.dump` dosyası yazar. Elle almak için:

```bash
docker compose exec -T db pg_dump -U bekci --format=custom --no-owner bekci > bekci.dump
```

TimescaleDB dökümünü geri yüklerken iki ek adım gerekir. Hedef veritabanı boş olmalıdır:

```bash
docker compose stop bekci
docker compose exec db psql -U bekci -d postgres -c 'DROP DATABASE bekci' -c 'CREATE DATABASE bekci'
docker compose exec db psql -U bekci -d bekci -c 'SELECT timescaledb_pre_restore()'
docker compose exec -T db pg_restore -U bekci -d bekci --no-owner < bekci.dump
docker compose exec db psql -U bekci -d bekci -c 'SELECT timescaledb_post_restore()'
docker compose start bekci
```

Ölçümde 3 milyon satırlık veritabanının dökümü 7 saniye sürdü ve 532 MB yer kapladı.

## Geri dönüş {#geri-donus}

**SQLite'a dönmek:** `uptime migrate` SQLite dosyasını değiştirmediği için eski kurulum olduğu gibi durur.

1. `docker compose stop bekci`
2. `docker-compose.yml` içindeki `DATABASE_URL` satırını silin.
3. `.env` dosyasında `UPTIME_TAG=⟦1.6.0⟧` yazın.
4. `docker compose up -d`

Taşımadan sonra PostgreSQL'e yazılan ölçümler ve olaylar SQLite dosyasında yoktur; dönünce kaybolur. Sorunun geçtiğine emin olmadan SQLite dosyasını silmeyin.

SQLite ile bir süre çalıştıktan sonra yeniden taşımak için hedef veritabanını boşaltın (`docker compose down` ve veritabanı birimini silmek ya da `DROP DATABASE`). Aksi halde `uptime migrate` "daha önce tamamlanmış" der ve yeni veriyi aktarmaz.

**Düz PostgreSQL'e dönmek:** TimescaleDB'ye dönüşen tablolar düz PostgreSQL imajında açılmaz. Yükseltmeden önce aldığınız dökümü eski sunucuya geri yükleyin.

**Gömülü imaja dönmek:** eski etiketi ve `/data/postgres` klasörünü kullanın; klasör yükseltme sırasında değiştirilmez.
