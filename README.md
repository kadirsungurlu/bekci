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
| `DATA_DIR` | `/data` | Veri klasörü |
| `ADDR` | `:8080` | Dinlenecek adres |

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

# Arayüz
cd web && npm install && npm run check && npm run build

# İmaj
docker build -t uptime .
```
