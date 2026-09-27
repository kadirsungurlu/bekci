# Uptime

Go ile yazılmış, UptimeRobot sadeliğinde arayüzü olan uptime izleme sistemi.
Uptime Kuma'nın özellik setini hedefler; yüzlerce monitörde de hızlı kalacak
şekilde tasarlanmıştır.

Plan ve ilerleme: [docs/PLAN.md](docs/PLAN.md)

## Özellikler (Aşama 1)

- Monitör tipleri: HTTP(S) (method, başlık, gövde, basic auth, kabul edilen
  kodlar, yönlendirme, TLS), kelime kontrolü, JSON sorgusu, TCP port, Ping, DNS,
  Push
- SSL sertifika bitiş uyarıları, tekrar deneme, kesinti hatırlatması, ters mod
- Bildirimler: WhatsApp (WP API), Telegram, e-posta, Discord, Slack, Webhook,
  ntfy, Gotify, Pushover
- Olay geçmişi, 24 saat / 7 / 30 / 90 gün uptime ve yanıt süresi grafikleri
- Canlı güncellenen arayüz (SSE), gece otomatik yedek

## Kurulum (Coolify)

- Build Pack: **Dockerfile**, port **8080**
- Kalıcı depolama: **/data** (veritabanı ve `backups/` klasörü burada). Coolify'da
  **Volume** türünde ekleyin. Sunucudaki bir klasörü bağlamak (Directory Mount)
  isterseniz klasörün sahibi uid 1000 olmalı (`chown 1000:1000 <klasör>`);
  uygulama root olmayan kullanıcıyla çalışır.
- Health check: `/healthz`
- İlk açılışta arayüz yönetici hesabı oluşturmanızı ister.

### Ortam değişkenleri

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `BASE_URL` | — | Bildirimlerdeki bağlantılar için dış adres, ör. `https://uptime.kadir.app` |
| `TZ` | `Europe/Istanbul` | Günlük özetler ve gece yedeği saati |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `MAX_CONCURRENT_CHECKS` | `50` | Aynı anda çalışacak en fazla kontrol |
| `DATA_DIR` | `/data` | SQLite veritabanı ve gece yedeklerinin klasörü |
| `DATABASE_URL` | — | Verilirse **PostgreSQL** kullanılır: `postgres://kullanıcı:şifre@sunucu:5432/veritabanı?sslmode=disable`. Gece yedeği bu durumda veritabanı tarafında (Coolify yedekleri / `pg_dump`) alınmalıdır |
| `ADDR` | `:8080` | Dinlenecek adres |
| `AGENT_DIR` | `/usr/local/share/uptime/agents` | Diğer platformların ajan programları (`uptime-windows-amd64.exe`); imajda hazır gelir |

## Push monitörü

Cron işi veya betik, belirlenen aralıkta şu adresi çağırır; çağrı gelmezse
monitör DOWN olur:

```bash
curl -fsS "https://uptime.kadir.app/api/push/<token>?status=up&msg=tamam&ping=120"
```

`status=down` ile hata da bildirilebilir.

## Şifre sıfırlama

Giriş yapılamıyorsa container içinde:

```bash
uptime sifre-sifirla <kullanıcı-adı>
```

Yeni şifre standart girdiden okunur.

## Geliştirme

```bash
# Go testleri (sunucuya Go kurmadan, geçici container'da)
docker run --rm -v "$PWD":/src -w /src golang:1.27 go test -race ./...

# Aynı testler PostgreSQL'e karşı (her test kendi geçici şemasında)
UPTIME_TEST_PG='postgres://postgres:parola@pg:5432/postgres?sslmode=disable' go test ./...

# Arayüz
cd web && npm install && npm run check && npm run build

# İmaj
docker build -t uptime .
```

## Uzak kontrol noktaları

Ana sunucu bir ağ sorunu yaşarsa izleme kör kalmasın diye aynı program başka
sunucularda "kontrol noktası" olarak çalıştırılabilir. Ayarlar → Kontrol
noktaları → "Yeni kontrol noktası" ile verilen komutu diğer sunucuda çalıştırın:

```bash
docker run -d --name uptime-probe --restart unless-stopped -e PROBE_SERVER=https://uptime.kadir.app -e PROBE_TOKEN=upr_… alpine:3 sh -c 'wget -qO /usr/local/bin/uptime --header "Authorization: Bearer $PROBE_TOKEN" "$PROBE_SERVER/api/probe/binary" && chmod +x /usr/local/bin/uptime && exec uptime probe'
```

Herkese açık `alpine` imajı açılışta programı ana sunucudan (token ile) indirir;
git, derleme veya kayıt deposu girişi gerekmez. Her yeniden başlatmada en güncel
sürüm alınır (`docker restart uptime-probe`). Program linux/amd64 içindir.

Kontrol noktası veritabanı kullanmaz; atanan monitörleri kendisi kontrol edip
sonuçları ana sunucuya gönderir, sunucuya ulaşamazsa sonuçları bekletir. Monitör
formundaki "Konumlar" bölümünden hangi konumlardan kontrol edileceğini ve kesinti
kuralını (herhangi biri / çoğunluk / hepsi) seçin. Ana sunucuda `PROBE_IMAGE`
verilirse kurulum komutu bunun yerine o imajı kullanır.

## Sunucu takibi: Windows sunucular

Sunucular → "Sunucu ekle" penceresindeki **Windows** sekmesindeki komutu
PowerShell'i "Yönetici olarak çalıştır" ile açıp yapıştırın. Program
`C:\Program Files\Uptime\uptime.exe` olarak kurulur ve `uptime-agent` Windows
hizmeti olarak çalışır (otomatik başlar, hata olursa yeniden başlar). Token
yalnızca yöneticilerin okuyabildiği `C:\ProgramData\Uptime\agent.env` dosyasında,
günlük aynı klasörde `agent.log`'dadır. Aynı komutu tekrar çalıştırmak günceller;
kaldırmak için `& 'C:\Program Files\Uptime\uptime.exe' service uninstall`.
Windows'ta yük ortalaması yaklaşıktır (işlemci kuyruğu), sıcaklık ve Docker
konteynerleri toplanmaz (ayrıntı: `docs/PLAN.md` §12.10).
