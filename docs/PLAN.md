# Uptime — Proje Planı

Uptime Kuma'nın özelliklerini UptimeRobot sadeliğinde bir arayüzle sunan, Go ile
yazılmış izleme sistemi. Yüzlerce monitörde arayüz yavaşlamamalı.

- Kurulum: Coolify, `https://uptime.kadir.app`
- Repo: `kadirsa1105/uptime-kadir-app` (private)
- Onay tarihi: 2026-09-27

## 1. Teknoloji

| Katman | Seçim | Neden |
|---|---|---|
| Backend | Go 1.27, standart `net/http` | Tek dosya, hafif, eşzamanlı kontrollerde güçlü |
| Veritabanı | SQLite (CGO'suz, `modernc.org/sqlite`); Aşama 3'te PostgreSQL seçeneği | Kurulumu sıfır, veri özetlendiği için şişmez |
| Arayüz | Svelte 5 + TypeScript + Vite, Go ikilisine gömülü (`go:embed`) | Hafif, hızlı, tek imaj |
| Canlı güncelleme | SSE (Server-Sent Events) | Sadece durum değişiklikleri gider |
| Kurulum | Tek Docker imajı, Coolify, `/data` kalıcı volume | |

Kuma'daki yavaşlamaya karşı:

1. Ham kontroller 14 gün tutulur; saatlik ve günlük özetler her kontrolde anında
   güncellenir. Liste ekranı ham veriye hiç dokunmaz.
2. Tarayıcıya sadece özet/durum değişiklikleri gider.
3. Kontroller zamana yayılır, eşzamanlı kontrol sayısı sınırlıdır.

## 2. Özellikler ve aşamalar

| Özellik | Aşama |
|---|---|
| HTTP(S): method, header, body, basic auth, kabul edilen kodlar, yönlendirme, TLS hatasını yok say | 1 |
| Keyword (kelime var/yok), JSON sorgusu | 1 |
| TCP port, Ping, DNS, Push (cron işlerinin "ben çalıştım" sinyali) | 1 |
| SSL sertifika bitiş uyarısı (21/14/7/3/1 gün) | 1 |
| Tekrar deneme, "kaç hatada alarm", kesinti sürerken hatırlatma, ters mod | 1 |
| Olay (incident) geçmişi, kesinti süreleri | 1 |
| Bildirimler: WhatsApp (WP API), Telegram, e-posta, Discord, Slack, Webhook, ntfy, Gotify, Pushover | 1 |
| Durum sayfaları (herkese açık, özel alan adı, duyurular) | 2 |
| Bakım pencereleri (tek seferlik / tekrarlayan) | 2 |
| Etiketler, gruplar (grup monitörü) | 2 |
| Uptime rozetleri, REST API + API anahtarları, Prometheus metrikleri | 2 |
| Dışa/içe aktarma, Uptime Kuma'dan içe aktarma | 2 |
| Ek bildirimler: Teams, Google Chat, Mattermost, Matrix, Signal, PagerDuty, Opsgenie, Home Assistant, Netgsm SMS | 2 |
| Docker container, veritabanı monitörleri (MariaDB, PostgreSQL, Redis, MongoDB, MSSQL) | 3 |
| gRPC, MQTT, SMTP, WebSocket, SNMP, gerçek tarayıcıyla kontrol | 3 |
| Proxy desteği, mTLS / OAuth2 | 3 |
| 2FA, çoklu kullanıcı ve yetkiler, PostgreSQL depolama, İngilizce arayüz | 3 |
| Uzak kontrol noktaları (başka sunucuya kurulan küçük ajan) | 3 |

## 3. Aşama 1 ayrıntıları

### Klasör yapısı

```
cmd/uptime/         giriş noktası (ayarlar, başlatma, düzgün kapanış)
internal/store/     veritabanı, migration'lar, sorgular
internal/check/     monitör tipleri (her tip ayrı dosya, ortak arayüz)
internal/engine/    zamanlayıcı, durum makinesi, olay yönetimi
internal/notify/    bildirim servisleri (her servis ayrı dosya)
internal/stats/     saatlik/günlük özetler, uptime hesabı, veri temizliği, yedek
internal/api/       REST API, oturum, SSE, push adresi
web/                Svelte arayüzü
docs/PLAN.md        bu plan
```

### Veritabanı tabloları

`users`, `sessions`, `monitors` (tipe özel ayarlar JSON), `heartbeats` (ham),
`stats_hourly`, `stats_daily`, `incidents`, `notifications`,
`monitor_notifications`, `cert_notices`, `settings`.

### Kontrol motoru

- Her monitör kendi aralığında çalışır; başlangıçlar rastgele kaydırılır.
- Durumlar: UP → PENDING (tekrar deneniyor) → DOWN → UP; ayrıca PAUSED.
- DOWN olunca bir kez bildirim; düzelince "X dk kesintiden sonra düzeldi";
  isteğe bağlı her N başarısız kontrolde bir hatırlatma. Yeniden başlatmada sahte
  bildirim yok.

### API

Oturum (giriş/çıkış/ilk kurulum/şifre), monitörler (CRUD, durdur/başlat, detay,
grafik verisi), özet, olaylar, bildirimler (CRUD, test gönder), canlı akış (SSE),
push adresi, `/healthz`.

### Arayüz ekranları (Türkçe)

1. İlk kurulum / giriş
2. Monitör listesi: durum, son 24 saat çubuğu, uptime %, sağda "Mevcut durum" ve
   "Son 24 saat" kutuları, arama, filtre, "Down olanlar önce"
3. Monitör detayı: uptime (24s/7g/30g/90g), yanıt süresi grafiği, SSL bilgisi, olaylar
4. Monitör ekle/düzenle: tipe göre sade form, gelişmiş ayarlar katlanır bölümde
5. Olaylar
6. Bildirimler: kanal ekle, test gönder, "yeni monitörlere varsayılan"
7. Ayarlar: şifre, veri saklama süreleri, SSL uyarı günleri

### Veri güvenliği

- Her gece otomatik yedek (`/data/backups`, son 7 gün).
- bcrypt şifreler, giriş denemesi sınırı (5 hata → 15 dk), CSRF koruması.
- Bildirim şifreleri/token'ları API'den maskeli döner.

### Veri saklama (varsayılan, ayarlardan değişir)

Ham kontroller 14 gün, saatlik özet 1 yıl, günlük özet süresiz.

## 4. Coolify kurulumu

API ile: Dockerfile build, mevcut SSH anahtarı, `uptime.kadir.app`, port 8080,
kalıcı `/data`, health check `/healthz`, `TZ=Europe/Istanbul`.

## 5. Çalışma yöntemi

- Geliştirme `gelistirme` dalında; Coolify sadece `main`'i deploy eder.
- Her adımda `go vet`, `go test -race`, arayüz tip kontrolü ve Docker build geçmeli
  (geçici container'larda; sunucuya kurulum yok).
- Testler: her monitör tipi sahte sunucularla; durum makinesi, uptime hesabı ve
  özetleme; uçtan uca (container çalıştır → monitör ekle → siteyi kapat → bildirim geldi mi).
- Her aşama sonunda Türkçe rapor → `main`'e birleştirme → deploy.

## 6. Canlıya geçiş

1. UptimeRobot'taki 12 monitör yeni sisteme eklenir.
2. Bir hafta iki sistem paralel çalışır; tutarlıysa geçiş tamamlanır.
3. UptimeRobot'ta sadece `uptime.kadir.app`'i izleyen tek monitör kalır
   (Aşama 3'teki uzak ajanla bu ihtiyaç da kalkar).

## 7. Riskler

- İzleme sistemi izlediği sunucuyla aynı yerde: yukarıdaki 3. madde.
- Container içinde ping: Docker'ın varsayılan `ping_group_range` ayarıyla yetkisiz
  ping çalışmalı; ilk testte doğrulanacak.
- Docker monitörü (Aşama 3): soket erişimi yalnızca salt okunur soket proxy'siyle.

## 8. Kararlar (onaylandı)

- Arayüz dili Türkçe; İngilizce Aşama 3'te.
- Veri saklama: ham 14 gün, saatlik 1 yıl, günlük süresiz.
- Ana bildirim kanalı WhatsApp (WP API); anahtar ve alıcı sonra girilecek.
- Aşama 1–2 tek yönetici; çoklu kullanıcı Aşama 3'te.
- Alan adı `uptime.kadir.app`.

## 9. İlerleme

### Aşama 1

- [ ] Proje iskeleti, ayarlar, veritabanı ve migration'lar
- [ ] Monitör tipleri: HTTP (keyword, JSON sorgusu dahil), TCP, Ping, DNS, Push
- [ ] Kontrol motoru: zamanlayıcı, durum makinesi, olaylar, SSL uyarıları
- [ ] İstatistikler: saatlik/günlük özet, uptime hesabı, veri temizliği, gece yedeği
- [ ] Bildirimler: WhatsApp, Telegram, e-posta, Discord, Slack, Webhook, ntfy, Gotify, Pushover
- [ ] API: oturum, monitörler, özet, olaylar, bildirimler, SSE, push, healthz
- [ ] Arayüz: kurulum/giriş, liste, detay, form, olaylar, bildirimler, ayarlar
- [ ] Dockerfile ve uçtan uca test
- [ ] Coolify kurulumu ve canlıya çıkış
- [ ] UptimeRobot monitörlerinin taşınması, bir hafta paralel çalışma

### Aşama 2

- [ ] Durum sayfaları
- [ ] Bakım pencereleri
- [ ] Etiketler ve gruplar
- [ ] Rozetler, REST API + API anahtarları, Prometheus metrikleri
- [ ] Dışa/içe aktarma, Uptime Kuma'dan içe aktarma
- [ ] Ek bildirim servisleri

### Aşama 3

- [ ] Docker ve veritabanı monitörleri
- [ ] gRPC, MQTT, SMTP, WebSocket, SNMP, gerçek tarayıcı
- [ ] Proxy, mTLS / OAuth2
- [ ] 2FA, çoklu kullanıcı, PostgreSQL depolama, İngilizce arayüz
- [ ] Uzak kontrol noktaları
