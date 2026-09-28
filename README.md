# Uptime

Go ile yazılmış, UptimeRobot sadeliğinde arayüzü olan izleme sistemi: web
siteleri ve servisler için uptime kontrolü (Uptime Kuma'nın özellik seti) ve
sunucular için kaynak takibi (Beszel benzeri). Tek Docker imajı, SQLite veya
PostgreSQL; yüzlerce monitörde de hızlı kalacak şekilde tasarlanmıştır.

Plan ve teknik ayrıntılar: [docs/PLAN.md](docs/PLAN.md)

- [Özellikler](#özellikler)
- [Kurulum](#kurulum)
- [Güncelleme, yedek ve geri dönüş](#güncelleme-yedek-ve-geri-dönüş)
- [Sunucu ajanı ve kontrol noktası](#sunucu-ajanı-ve-kontrol-noktası)
  - [Kurulum komutları](#kurulum-komutları)
  - [Ajan güncelleme](#ajan-güncelleme)
  - [Ajanı kaldırma](#ajanı-kaldırma)
  - [IP kilidi ve token](#ip-kilidi-ve-token)
  - [Sorun giderme](#sorun-giderme)
- [Push monitörü](#push-monitörü)
- [Şifre sıfırlama](#şifre-sıfırlama)
- [Güvenlik notları](#güvenlik-notları)
- [Geliştirme](#geliştirme)

## Özellikler

**Monitörler**
- HTTP(S) (method, başlık, gövde, basic auth / OAuth, kabul edilen kodlar,
  yönlendirme, mTLS, proxy), kelime ve JSON sorgusu kontrolü, TCP port, Ping,
  DNS, Push, Docker konteyneri, WebSocket, gRPC, veritabanları (PostgreSQL,
  MySQL/MariaDB, MSSQL, MongoDB, Redis), MQTT, SNMP ve diğerleri — tam liste
  "Yeni monitör" formunda
- SSL sertifika bitiş uyarıları, tekrar deneme, kesinti hatırlatması, ters mod
- Monitör grupları, etiketler, bakım pencereleri
- Olay geçmişi ve ayrıntısı (kesinti anındaki istek/yanıt yakalaması — yalnızca
  yöneticiler görür), 24 saat / 7 / 30 / 90 gün uptime ve yanıt süresi grafikleri
- Uzak **kontrol noktaları**: monitörler birden çok konumdan kontrol edilir,
  kesinti kuralı (herhangi biri / çoğunluk / hepsi)

**Sunucu takibi**
- CPU, RAM, disk (bağlama noktası başına), swap, yük, sıcaklık, ağ (Mbit/s),
  Docker konteynerleri; Linux ve Windows
- Uyarılar: çevrimdışı, CPU, RAM, disk, swap, yük, sıcaklık, ağ — pencere
  ortalamasıyla ve histerezisle (eşiğe ulaşınca tetiklenir, eşiğin biraz altına
  inince kapanır)

**Bildirimler**
- WhatsApp (WP API), Telegram, e-posta, Discord, Slack, Teams, Google Chat,
  Mattermost, Rocket.Chat, Webhook, ntfy, Gotify, Pushover, PagerDuty,
  Opsgenie ve diğerleri; her kanal için "örnek bildirim gönder"

**Durum sayfaları**
- Gruplar (katlanabilir), hedef adres gösterimi (yalnızca alan adı), son
  olaylar (gizlenebilir), şifre koruması, özel alan adı, rozetler

**Kullanıcılar**
- Roller (yönetici / düzenleyici / izleyici), yalnızca kendisine atanan
  monitörleri ve sunucuları gören **müşteri hesapları**, iki adımlı doğrulama
  (2FA), API anahtarları, işlem kaydı

**Diğer**
- Canlı güncellenen arayüz (SSE), içe/dışa aktarma, gece otomatik yedek
- Telefonda uygulama gibi kullanım (PWA): iPhone'da Safari → Paylaş → **Ana
  Ekrana Ekle**; Android'de Chrome → **Uygulamayı yükle**

## Kurulum

### Coolify

- Build Pack: **Dockerfile**, port **8080**, health check `/healthz`
- Kalıcı depolama: **/data** (veritabanı ve `backups/` klasörü). Coolify'da
  **Volume** türünde ekleyin. Sunucudaki bir klasörü bağlamak (Directory Mount)
  isterseniz sahibi uid 1000 olmalı (`chown 1000:1000 <klasör>`); uygulama root
  olmayan kullanıcıyla çalışır.
- İlk açılışta arayüz yönetici hesabı oluşturmanızı ister.
- Güncellemede Coolify yeni konteyneri eskisi kapanmadan başlatır. Yeni
  konteyner veri klasörü kilidini bekler (bu sırada sağlıklı görünür), eskisi
  kapanınca 1–2 saniyede devralır; iki kopya aynı anda kontrol yapıp çift
  bildirim göndermez.

### Docker Compose (bağımsız sunucu)

`deploy/compose/` klasöründe uygulama + Caddy (Let's Encrypt ile otomatik
HTTPS, HSTS) hazır:

```bash
cd deploy/compose
cp .env.example .env        # UPTIME_DOMAIN ve ACME_EMAIL'i yazın
docker compose up -d
```

- İmaj GitHub Container Registry'de özelse önce `docker login ghcr.io`.
- Ayrıntılar (yedek, sürüm sabitleme, şifre sıfırlama) `docker-compose.yml`
  başındaki açıklamada.
- Taşıma: eski sunucudaki `/data` içeriğini yeni birime kopyalayıp DNS'i yeni
  sunucuya çevirmek yeterli; adres aynı kaldığı için ajanlar yeniden kurulmadan
  bağlanır (IP kilidi ana sunucunun değil ajanın IP'sine bakar).

### İmajlar ve sistem gereksinimi

| Etiket | İçerik |
|---|---|
| `:latest` | SQLite (önerilen, en hafif) |
| `:postgres` | Gömülü PostgreSQL 18 (veriler yine `/data` altında) |
| `:<sha>` / `:postgres-<sha>` | Belirli bir sürüm (sabitleme / geri dönüş için) |

İki imajın verisi birbirine taşınmaz; baştan birini seçin. En az 1 vCPU,
512 MB RAM (1 GB önerilir), 10 GB disk. Uygulama 10 monitör ve 3 sunucuda
~15 MB RAM kullanır; gömülü PostgreSQL ~200 MB ekler. İmaj Linux amd64
içindir; içinde Windows (amd64) ve Linux arm64 ajan programları da hazır gelir.

### Ortam değişkenleri

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `BASE_URL` | — | Dış adres, ör. `https://uptime.kadir.app`. Bildirim bağlantıları ve ajan kurulum komutları bunu kullanır |
| `TZ` | `Europe/Istanbul` | Günlük özetler ve gece yedeği saati |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `MAX_CONCURRENT_CHECKS` | `50` | Aynı anda çalışacak en fazla kontrol |
| `DATA_DIR` | `/data` | SQLite veritabanı ve yedeklerin klasörü |
| `DATABASE_URL` | — | Verilirse **PostgreSQL** kullanılır: `postgres://kullanıcı:şifre@sunucu:5432/veritabanı?sslmode=disable`. Yedek bu durumda veritabanı tarafında alınmalıdır (Coolify yedekleri / `pg_dump`) |
| `ADDR` | `:8080` | Dinlenecek adres |
| `AGENT_DIR` | `/usr/local/share/uptime/agents` | Diğer platformların ajan programları (`uptime-windows-amd64.exe`, `uptime-linux-arm64`); imajda hazır gelir |
| `PROBE_IMAGE` | — | Verilirse kontrol noktası kurulum komutu programı indirmek yerine bu Docker imajını kullanır |
| `UPTIME_LOCK_WAIT` | `600` | İkinci bir kopya veri klasörü kilidini en fazla kaç saniye bekler (sonra hata ile çıkar) |

## Güncelleme, yedek ve geri dönüş

- **Gece yedeği** (SQLite): `/data/backups/` altında her gece alınır, eskiler
  döndürülür. Sunucu dışına da kopyalayın.
- **Güncelleme öncesi otomatik yedek** (SQLite): yeni sürüm veritabanı yapısını
  değiştirecekse önce `/data/backups/pre-migrate-v<eski>-to-v<yeni>-<zaman>.db`
  kopyası alınır. Yedek alınamazsa (ör. disk dolu) güncelleme uygulanmaz ve
  uygulama açılmaz; yer açılınca açılır. Bu dosyalar kendiliğinden silinmez.
- **Sürüm sabitleme / geri dönüş** (Compose): `.env`'de `UPTIME_TAG=<sha>`
  (ya da `postgres-<sha>`). Sorun olursa eski etikete dönün; yeni sürüm
  veritabanını yükselttiyse önce yedeği geri yükleyin.
- **PostgreSQL ana sürüm yükseltmesi** (`:postgres` imajı): veri klasörü başka
  bir ana sürümle oluşturulduysa konteyner açılmaz ve adımları günlüğe yazar.
  Özetle: eski imaja dönün →
  `docker exec <c> pg_dump -h /run/postgresql -U postgres --format=custom --file=/data/backups/tasima.dump uptime`
  → `/data/postgres` klasörünü yeniden adlandırın → yeni imajı başlatın →
  `docker exec <c> pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/tasima.dump`
  → yeniden başlatın.
- `:postgres` imajında aynı `/data`'yı iki konteyner aynı anda kullanamaz;
  PostgreSQL ya da uygulama beklenmedik şekilde kapanırsa konteyner de kapanır
  ve yeniden başlatılır.

## Sunucu ajanı ve kontrol noktası

Aynı `uptime` programı iki ayrı görevde çalışır; ikisi panelde ayrı kayıtlardır
ve birbirinin ekranında görünmez:

| | Sunucu ajanı | Kontrol noktası |
|---|---|---|
| Panelde | **Sunucular** → Sunucu ekle | **Ayarlar → Kontrol noktaları** → Yeni kontrol noktası |
| Görevi | Sunucunun CPU/RAM/disk/ağ/Docker metriklerini gönderir | Atanan monitörleri kendi konumundan kontrol edip sonuçları gönderir |
| Kurulum | Docker, Linux (systemd), Windows | Docker |
| Docker konteyneri / birimi | `uptime-agent` / `uptime-agent-bin` | `uptime-probe` / `uptime-probe-bin` |
| Ayar dosyası (token) | `/etc/uptime-agent.env` | `/etc/uptime-probe.env` |

Ajan veritabanı kullanmaz; ana sunucuya ulaşamazsa sonuçları bekletip sonra
gönderir. Desteklenen platformlar: Linux amd64 ve arm64, Windows amd64.

### Kurulum komutları

Komutu her zaman **panelden** kopyalayın: sunucu adresi, token ve programın
SHA-256 özeti komutun içine gömülüdür.

**Linux (Docker ve systemd):** komut çok satırlıdır ve **root** olarak
yapıştırılır (root değilseniz ilk satırı `sudo sh <<'UPTIME_KURULUM'` yapın).
Yapısı:

```bash
sh <<'UPTIME_KURULUM'
set -e
# root kontrolü
install -m 600 /dev/null /etc/uptime-agent.env      # token yalnızca root'a okunur
cat > /etc/uptime-agent.env <<'UPTIME_ENV'
PROBE_SERVER=https://uptime.kadir.app
PROBE_TOKEN=upr_…
UPTIME_ENV
docker run -d --name uptime-agent --restart unless-stopped … --env-file /etc/uptime-agent.env -v uptime-agent-bin:/opt/uptime alpine:3 sh -c '…'
UPTIME_KURULUM
```

- Token hiçbir sürecin komut satırında (`ps`) görünmez; yalnızca 0600 izinli
  ayar dosyasındadır.
- Program sunucunun mimarisine göre (`uname -m`: x86_64 / aarch64) indirilir
  ve komuttaki **SHA-256** ile doğrulanır; uyuşmazsa kurulmaz.
- Docker konteynerleri ayrıcalıkları düşürülmüş çalışır (`--cap-drop ALL`,
  `no-new-privileges`, 256 MB bellek, süreç sınırı).
- **systemd** kurulumu: program `/usr/local/bin/uptime`, hizmet
  `uptime-agent` (`/etc/systemd/system/uptime-agent.service`), ayar
  `/etc/uptime-agent.env`. Durum: `systemctl status uptime-agent`, günlük:
  `journalctl -u uptime-agent`.
- Ana sunucu `http://` ise ayar dosyasına `PROBE_ALLOW_INSECURE=1` eklenir
  (ajan aksi halde şifresiz bağlantıyı reddeder).

**Windows:** PowerShell'i **Yönetici olarak çalıştır** ile açıp panelin
Windows sekmesindeki komutu yapıştırın.

- Program `C:\Program Files\Uptime\uptime.exe`, hizmet `uptime-agent`
  (otomatik başlar, hata olursa yeniden başlar).
- Ayar (token) `C:\Program Files\Uptime\agent.env`, günlük aynı klasörde
  `agent.log`; klasöre yalnızca SYSTEM ve Administrators erişebilir.
  Başlangıç hataları Windows Olay Günlüğü'ne de yazılır (Uygulama, kaynak
  `uptime-agent`).
- Windows'ta yük ortalaması yaklaşıktır (işlemci kuyruğu); sıcaklık ve Docker
  konteynerleri toplanmaz.

### Ajan güncelleme

Ajan kendini **güncellemez** (sürüm sabit): program bir kez indirilir,
doğrulanır ve yeniden başlatmalarda aynı program kullanılır. Böylece ana sunucu
ele geçirilse bile sunuculara kendiliğinden yeni program inmez. Güncellemek
için panelden **güncel** kurulum komutunu alıp tekrar çalıştırın:

- **Docker:** önce eskisini kaldırın, sonra yeni komutu çalıştırın:
  ```bash
  docker rm -f uptime-agent; docker volume rm uptime-agent-bin     # sunucu ajanı
  docker rm -f uptime-probe; docker volume rm uptime-probe-bin     # kontrol noktası
  ```
- **systemd** ve **Windows:** yeni komutu doğrudan çalıştırın (program ve
  hizmet yenilenir).

Token yalnızca bir kez gösterildiği için komutu yeniden görmek için panelde
**yeni token** almanız gerekir; eski token geçersiz olur.

### Ajanı kaldırma

Önce panelden sunucuyu / kontrol noktasını silin (token geçersiz olur), sonra
sunucuda:

**Docker — sunucu ajanı**
```bash
docker rm -f uptime-agent
docker volume rm uptime-agent-bin
rm -f /etc/uptime-agent.env
```

**Docker — kontrol noktası**
```bash
docker rm -f uptime-probe
docker volume rm uptime-probe-bin
rm -f /etc/uptime-probe.env
```

**Linux (systemd)**
```bash
systemctl disable --now uptime-agent
rm -f /etc/systemd/system/uptime-agent.service /usr/local/bin/uptime /etc/uptime-agent.env
systemctl daemon-reload
```

**Windows** (Yönetici PowerShell)
```powershell
& "$env:ProgramFiles\Uptime\uptime.exe" service uninstall
Remove-Item -Recurse -Force "$env:ProgramFiles\Uptime"
```

`service uninstall` hizmeti durdurup siler, ayar dosyasını (token) ve Olay
Günlüğü kaynağını kaldırır; ikinci satır programı ve günlükleri siler. Eski
sürümlerden kalma `C:\ProgramData\Uptime\agent.env` dosyası da kurulum ve
kaldırmada silinir.

### IP kilidi ve token

- Ajan yalnızca kendi token'ı (`upr_…`) ile bağlanır; token sunucuda özet
  (hash) olarak saklanır.
- **IP kilidi** (yeni ajanlarda varsayılan açık): ilk bağlantının IP'si
  sabitlenir — IPv4 tam adres, IPv6 /64 blok olarak (ikisi ayrı ayrı; çift
  yığınlı sunucular sorun yaşamaz). Başka bir IP'den gelen istek reddedilir
  (403); token çalınsa bile başka makineden kullanılamaz.
- Sunucunun IP'si değişirse panelden ilgili sunucunun / kontrol noktasının
  ayarlarında **IP kilidini sıfırlayın**; bir sonraki bağlantı yeni IP'yi
  sabitler. Kilit aynı yerden kapatılabilir.
- Token sızdıysa panelden **yeni token** alın ve kurulum komutunu yeniden
  çalıştırın.

### Sorun giderme

| Belirti | Neden / çözüm |
|---|---|
| Ajan günlüğünde `403` | IP kilidi (sunucunun IP'si değişti → panelden IP kilidini sıfırlayın) ya da ajan panelde devre dışı |
| Ajan günlüğünde `401` | Token geçersiz (silinmiş ya da yenilenmiş) → panelden yeni token alıp komutu yeniden çalıştırın |
| `Program özeti uyuşmuyor` | Komut eski bir sürümden kalma (ana sunucu güncellendi) → panelden güncel komutu alın; Docker'da önce konteyneri ve birimi silin |
| Metrik yerine "konteyner içinde /proc bağlanmamış" | Ajan Docker'da host bağlamaları olmadan çalışıyor → komutu panelden olduğu gibi kullanın |
| Ajan `http://` adresi reddediyor | Ana sunucuyu HTTPS arkasına alın ya da ayar dosyasına `PROBE_ALLOW_INSECURE=1` ekleyin |

Günlükler: `docker logs uptime-agent` (ya da `uptime-probe`),
`journalctl -u uptime-agent`, Windows'ta `C:\Program Files\Uptime\agent.log`.

## Push monitörü

Cron işi veya betik, belirlenen aralıkta şu adresi çağırır; çağrı gelmezse
monitör DOWN olur:

```bash
curl -fsS "https://uptime.kadir.app/api/push/<token>?status=up&msg=tamam&ping=120"
```

`status=down` ile hata da bildirilebilir.

## Şifre sıfırlama

Giriş yapılamıyorsa (iki imajda da aynı; uygulama çalışırken kullanılabilir):

```bash
docker exec -it <container> uptime sifre-sifirla <kullanıcı-adı>
```

Compose'da: `docker compose exec -it uptime uptime sifre-sifirla <kullanıcı-adı>`.
Yeni şifre standart girdiden okunur; tüm oturumlar kapatılır. İki adımlı
doğrulamayı da kapatmak için sona `--2fa-kapat` ekleyin.

## Güvenlik notları

- Şifreler bcrypt ile, ajan token'ları ve API anahtarları özet (hash) olarak
  saklanır; ajanlarda IP kilidi ve SHA-256 ile sürüm sabitleme vardır.
- HTTPS isteklerinde HSTS gönderilir; Compose'daki Caddy de gönderir.
- Monitörler iç ağ adreslerini de kontrol edebilir (bilerek; iç servisleri
  izlemek için). Bunun yerine veri sızıntısı kapatılmıştır: kesinti
  yakalamaları (istek/yanıt) yalnızca yöneticilere görünür, yanıt başlıkları
  sınırlıdır, hata mesajlarında uzak sunucunun yanıt gövdesi gösterilmez.
- Kayıtlı gizli alanlar (şifre, token, webhook adresi) arayüze maskeli döner;
  hedef adres ya da ona bağlı alanlar değişirse gizli değerin yeniden girilmesi
  istenir (mevcut şifre başka bir hedefe taşınamasın).
- Müşteri hesapları yalnızca kendisine atanan monitörleri ve sunucuları görür;
  canlı akış (SSE) erişimi düzenli olarak yeniden doğrular.

## Geliştirme

```bash
# Go testleri (sunucuya Go kurmadan, geçici container'da)
docker run --rm -v "$PWD":/src -w /src golang:1.27 go test -race ./...

# Aynı testler PostgreSQL'e karşı (her test kendi geçici şemasında)
UPTIME_TEST_PG='postgres://postgres:parola@pg:5432/postgres?sslmode=disable' go test -race ./...

# Arayüz
cd web && npm ci && npm run check && npm run build

# İmajlar
docker build -t uptime .
docker build -f Dockerfile.postgres -t uptime:postgres .
```

CI (`.github/workflows/imajlar.yml`) her push'ta (`gelistirme`, `main`) ve
PR'da çalışır: Go testleri (SQLite ve PostgreSQL, `-race`), Windows derleme
kontrolü, `svelte-check`, iki imajın duman testi. İmajlar yalnızca `main`'de ve
hepsi geçince yayınlanır.
