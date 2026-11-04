# Canlı veri provası ve kesin geçiş planı (2026-11-04)

Amaç: canlı uptime sunucusunun verisiyle SQLite -> TimescaleDB geçişini son imajla denemek ve canlı adımları kesinleştirmek. Canlıya yazılmadı. Bu belge DENETIM-695 ve DENETIM-640'taki canlı geçiş planlarının yerine geçer.

## Prova düzeni
- Veri: canlının gece yedeği `uptime-2026-11-04.db` (77,7 MB, 00:07), `ssh uptime cat` ile kopyalandı. SHA-256 iki uçta aynı (`abda5d24...`), `integrity_check` ok. Veri 1.6.0 şeması: 9 monitör, 5 nokta, 3 kullanıcı, 181.535 heartbeat, 42 olay.
- İmaj: `kadirsungurlu/bekci:gelistirme` (`c5f7ed33`, commit 45ab408). Ajan güncellemesi için aynı kaynaktan test anahtarıyla imzalı yerel imaj.
- Ağ: internetsiz Docker ağı. Bildirim çıkamaz, canlı ajanlar bağlanamaz (log'daki tek ERROR "abone bildirimi gönderilemedi" bunu doğruluyor). Sınır: tek çekirdek, db 400 MB, panel 256 MB, `TS_TUNE_MEMORY=400MB`.
- Kopya ve test anahtarı prova sonunda silindi. Ham çıktı repo dışı `prova-canli/sonuc/`.

## Sonuçlar
| Konu | Durum | Bulgu |
|---|---|---|
| Taşıma | ÇALIŞIYOR | 62 tablo, 199.163 satır, kip `timescale`, migrate 25,6 sn |
| Kesinti | ÇALIŞIYOR | durdurma 0,6 + migrate 25,6 + açılış 2,0 = 28,2 sn |
| Fark | ÇALIŞIYOR | Araç satır sayılarını doğruladı. API karşılaştırması: 14.425 hizalı grafik noktasında 0 fark |
| SQLite | ÇALIŞIYOR | Taşıma öncesi ve sonrası özet aynı |
| Belirteçler | ÇALIŞIYOR | 5 ajanın `token_hash` özeti iki tarafta aynı; canlı API anahtarı yeni panelde çalıştı |
| Panel | ÇALIŞIYOR | `db_mode: timescale`; giriş 200; özet, topoloji, sunucular, olaylar 200; zamanlı işlerde hata 0/25 |
| Ajan: resmi 1.6.0 -> yeni panel | ÇALIŞIYOR | Sunucu ajanı bağlandı, metrik gönderdi |
| Ajan: yeni sürüm | ÇALIŞIYOR | Sunucu ajanı bağlandı, envanter geldi |
| Otomatik güncelleme (test anahtarı) | ÇALIŞIYOR | Konum ajanı 1.6.0 -> 1.7.0-prova, ~1 sn, durum `current`, 9 işi geri aldı |
| Geri dönüş | ÇALIŞIYOR | 1.6.0 SQLite ile 2,6 sn'de açıldı, 9 monitör |
| Bellek | ÇALIŞIYOR | Migrate 40-94 MB, db RSS ~150 MB (400 MB sınıra dayanmadı, OOM yok), panel 18 MB. DB birimi ~243 MB |

Rig kaynaklı, canlıda olmayacak: provada ajan belirteçleri yenilendi ve IP kilidi silindi (rig IP'si farklı). Bu yüzden geri dönüşte iki test ajanı 401 aldı; canlı belirteçler değişmediği için canlıda beklenmez. İnternetsiz ağda monitörler düştüğünden provadaki olay ve "down" sonuçları yapaydır.

## Planda düzeltilen iki hata
1. `migrate --dry-run` panel çalışırken reddedilir (kaynak kilidi). Önceki planın 4. adımı çalışmazdı. Kesintisiz deneme gece yedeği üzerinden yapılır (`--from`); 1.6.0 açıkken denendi.
2. `docker compose run` 1.6.0 imajıyla çalışsaydı `migrate` komutu olmazdı. `UPTIME_TAG` önce yeni sürüme alınır; çalışan konteyner `up` komutuna kadar etkilenmez.

## Kesin canlı geçiş
Klasör: `uptime:/root/docker/uptime`. `<yeni>`: yayınlanan sürüm. Önkoşul: sürüm gerçek `AGENT_SIGNING_KEY` ile yayınlanmış olmalı.

Hazırlık (kesintisiz, ~10 dk):
```sh
cd /root/docker/uptime; T=$(date +%F-%H%M)
cp docker-compose.yml docker-compose.yml.$T.bak; cp .env .env.$T.bak
docker run --rm -v bekci_bekci-data:/d:ro alpine tar -czf - -C /d . > bekci-data-$T.tgz
df -h /                                 # en az 3 GB boş olmalı
fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile
echo '/swapfile none swap sw 0 0' >> /etc/fstab
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
```
`docker-compose.yml` eklemeleri:
```yaml
services:
  bekci:
    environment:
      DATABASE_URL: ${DATABASE_URL:-}
    depends_on:
      db: {condition: service_healthy}
  db:
    image: timescale/timescaledb:2.31.0-pg18
    command: postgres -c max_worker_processes=24
    restart: unless-stopped
    stop_grace_period: 60s
    mem_limit: 400m
    environment:
      POSTGRES_USER: bekci
      POSTGRES_DB: bekci
      POSTGRES_PASSWORD: ${POSTGRES_PASSWORD}
      TIMESCALEDB_TELEMETRY: "off"
      TS_TUNE_MEMORY: 400MB
    volumes:
      - bekci-db:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -h 127.0.0.1 -U bekci -d bekci"]
      interval: 5s
      timeout: 5s
      retries: 30
      start_period: 30s
volumes:
  bekci-db: {}
```
```sh
sed -i 's/^UPTIME_TAG=.*/UPTIME_TAG=<yeni>/' .env
docker compose pull bekci db
docker compose up -d db                 # panel 1.6.0 olarak sürer
PW=$(grep ^POSTGRES_PASSWORD= .env | cut -d= -f2)
DB="postgres://bekci:$PW@db:5432/bekci?sslmode=disable"
Y=/data/backups/uptime-$(date +%F).db
docker compose run --rm --no-deps -e DATABASE_URL="$DB" bekci migrate --from $Y --dry-run
```
Geçiş:
```sh
docker compose stop bekci
docker compose run --rm --no-deps -e DATABASE_URL="$DB" bekci migrate
echo "DATABASE_URL=$DB" >> .env
docker compose up -d bekci
```
Kesinti tahmini: 1-2 dk, üst sınır 3 dk. Prova 28 sn sürdü; canlı işlemci daha yavaş ve o an veri biraz daha büyük olacak. `migrate` hata verirse `DATABASE_URL` eklenmez; geri dönüş 1. yola gidilir.

Doğrulama:
```sh
docker compose exec bekci wget -qO- 'http://127.0.0.1:8080/healthz?detail'   # db_mode: timescale
docker compose exec db psql -U bekci -d bekci -c "select count(*) filter (where last_run_status='Failed'), count(*) from timescaledb_information.job_stats"
```
Panelde: 5 nokta çevrimiçi, 9 monitör, açık olaylar aynı, deneme bildirimi gidiyor. Ertesi gün: gece `.dump` yedeği ve sıkıştırma. SQLite ve tgz 30 gün saklanır.

Geri dönüş (ölçülen 2,6 sn, işlemle ~1 dk):
```sh
docker compose stop bekci
sed -i '/^DATABASE_URL=/d; s/^UPTIME_TAG=.*/UPTIME_TAG=1.6.0/' .env
docker compose up -d --no-deps bekci
```
Geçişten sonra PostgreSQL'e yazılan veri 1.6.0'a dönmez. Güncellenmiş ajanlar 1.6.0 panelle çalışır (DENETIM-695). Yeniden deneme: yalnız DB birimi yenilenir (`docker compose stop db`, `docker compose rm -f db`, `docker volume rm bekci_bekci-db`, `docker compose up -d db`), `UPTIME_TAG` yeniden `<yeni>` yapılır, sonra geçiş adımları.

Ajanlar: 5 ajan otomatik güncellemeyle geçer. Önce tek ajanda görmek için panelde otomatik güncelleme kapalı başlatılıp tek ajan elle güncellenir.

## Denenmedi
- Canlı sunucunun kendisinde geçiş; tek çekirdekli yavaş disk gerçek süresi.
- Gerçek `AGENT_SIGNING_KEY` ile imza ve canlı ajanların güncellenmesi (yalnız test anahtarı).
- Gerçek bildirim gönderimi (ağ internetsiz).
- `depends_on` ve `mem_limit` birleşimi: provada `depends_on` yoktu, varsayılan compose'da var; swap provada yoktu.
- Prova verisi gece yedeği, canlı WAL'ın son 12 saati değil.
- Birden fazla gece süren sıkıştırma ve `.dump` yedeği bu veride.

## Test paneli (Coolify)
Hazır imaja geçti: yeni uygulama `gfq8lhuo8184cnnbxscdkr13` (`kadirsungurlu/bekci:gelistirme`), veritabanı `pxhenyufxw7kjlptvzen59b3` (`timescale/timescaledb:2.31.0-pg18`, 640 MB). Coolify API mevcut uygulamayı `dockerimage` türüne çeviremediği için yeni uygulama kuruldu; eski `zkepfwrizsywfzsfdwapnkud` durduruldu, otomatik dağıtımı kapatıldı. Test verisi taşındı: 381.841 satır, 13 sn. Uçtan uca: giriş, 22 monitör, monitör ayrıntısı, sunucular, envanter (geçici ajanla), topoloji 1920 ve 390 px, olaylar, ayarlar çalışıyor, API hatası yok.
Coolify notu: PG18 imajında veri birimi `/var/lib/postgresql` olmalı; Coolify `/var/lib/postgresql/data` oluşturur, API ile düzeltilir.
