# Uptime — Proje Planı

> **Tarihsel belge.** Bu plan projenin ilk tasarımını (2026-09) anlatır; ürün
> adı o zaman "Uptime" idi (bugün **Bekci**). Mimari ve veri modeli büyük
> ölçüde geçerli olsa da onay kutuları ve aşamalar güncel tutulmaz; İngilizce
> arayüz, kontrol noktaları, kısmi kesinti olayları gibi sonradan eklenen
> özellikler için README ve belge sitesi (bekci.app/docs) esastır.

Uptime Kuma'nın özelliklerini UptimeRobot sadeliğinde bir arayüzle sunan, Go ile
yazılmış izleme sistemi. Yüzlerce monitörde arayüz yavaşlamamalı.

- Kurulum: Coolify, `https://uptime.kadir.app`
- Repo: `kadirsungurlu/bekci` (herkese açık, AGPL-3.0)
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
| gRPC, MQTT, SMTP, WebSocket, SNMP | 3 |
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
- ~~Aşama 1–2 tek yönetici; çoklu kullanıcı Aşama 3'te.~~ Güncellendi (2026-09-27):
  çoklu kullanıcı ve yetkiler Aşama 2'nin ilk işi (bkz. bölüm 10).
- Alan adı `uptime.kadir.app`.

## 9. İlerleme

### Aşama 1 — tamamlandı (2026-09-27)

- [x] Proje iskeleti, ayarlar, veritabanı ve migration'lar
- [x] Monitör tipleri: HTTP (keyword, JSON sorgusu dahil), TCP, Ping, DNS, Push
- [x] Kontrol motoru: zamanlayıcı, durum makinesi, olaylar, SSL uyarıları
- [x] İstatistikler: saatlik/günlük özet, uptime hesabı, veri temizliği, gece yedeği
- [x] Bildirimler: WhatsApp, Telegram, e-posta, Discord, Slack, Webhook, ntfy, Gotify, Pushover
- [x] API: oturum, monitörler, özet, olaylar, bildirimler, SSE, push, healthz
- [x] Arayüz: kurulum/giriş, liste, detay, form, olaylar, bildirimler, ayarlar
- [x] Dockerfile ve uçtan uca test
- [x] Coolify kurulumu ve canlıya çıkış (`uptime.kadir.app`)
- [x] Bağımsız inceleme (Fable): backend + arayüz bulguları düzeltildi
- [x] Tasarım kontrolü ve düzeltmeleri
- [ ] UptimeRobot monitörlerinin taşınması, bir hafta paralel çalışma

### Aşama 2 — sıradaki (öncelik sırasıyla)

- [x] **2a. Kullanıcılar ve yetkiler** (bölüm 10) — backend
- [x] **2b. Herkese açık durum sayfaları** (bölüm 11) — backend
- [x] 2c. Bakım pencereleri + grup monitörü — backend
- [x] 2d. Etiketler ve gruplar — backend
- [x] 2e. Rozetler, REST API + API anahtarları, Prometheus metrikleri — backend
- [x] 2f. Dışa/içe aktarma, Uptime Kuma ve UptimeRobot'tan içe aktarma — backend
- [x] 2g. Ek bildirim servisleri (15 yeni: Teams, Google Chat, Mattermost, Rocket.Chat,
      Matrix, Signal, PagerDuty, Opsgenie, Home Assistant, Netgsm SMS, Twilio, Pushbullet,
      Bark, LINE, Apprise)
- [ ] Aşama 2-3 özelliklerinin arayüzü (devam ediyor)

### Aşama 3

- [x] Docker ve veritabanı monitörleri (MySQL/MariaDB, PostgreSQL, MSSQL, Redis, MongoDB)
- [x] gRPC, MQTT, SMTP, WebSocket, SNMP, TLS sertifika
- [~] Gerçek tarayıcıyla kontrol: yapıldı, sonra kaldırıldı (karar 2026-09-27). Ayrı
      Chrome container'ı gerektiriyordu, pratikte HTTP + kelime kontrolü yetiyor.
      Mevcut tarayıcı monitörleri HTTP'ye çevrilir (göç 9); Kuma "real-browser" da
      HTTP olarak aktarılır.
- [x] Proxy, mTLS / OAuth2
- [x] 2FA (TOTP + kurtarma kodları)
- [x] **PostgreSQL depolama** (karar 2026-09-27: desteklenecek). Ayrıca PostgreSQL
      gömülü tek imaj: `Dockerfile.postgres`. `DATABASE_URL`
      verilirse PostgreSQL, yoksa SQLite. Tüm SQL iki veritabanında çalışır;
      migration'lar ortak bir alt kümede yazılıp PostgreSQL'e çevrilir. Testler iki
      veritabanında da çalışır. PostgreSQL'de gece yedeği veritabanı tarafında alınır.
- [x] Uzak kontrol noktaları (probe) ve çok konumlu kontrol
- [ ] İngilizce arayüz (tüm ekranlar bittikten sonra)

## 10. Aşama 2a — Kullanıcılar ve yetkiler

Roller (API her istekte rolü kontrol eder; arayüzde gizlemek tek başına yetmez):

| Rol | Yetki |
|---|---|
| Yönetici | Her şey: kullanıcılar, ayarlar, işlem kaydı, monitörler, bildirimler, durum sayfaları |
| Editör | Monitör, bildirim, durum sayfası ve duyuru yönetimi; kullanıcılar/ayarlar yok |
| İzleyici | Sadece görüntüleme. İsteğe bağlı **monitör kısıtı**: yalnızca seçilen monitörleri görür (müşteri erişimi) |

- Kullanıcı yönetimi (Ayarlar → Kullanıcılar): ekle, rol değiştir, monitör kısıtı,
  şifre sıfırla, devre dışı bırak, sil.
- Yönetici tarafından verilen şifreyle ilk girişte şifre değiştirme zorunlu.
- Devre dışı bırakılan/silinen kullanıcının oturumları anında kapanır.
- Son aktif yönetici silinemez, devre dışı bırakılamaz, rolü düşürülemez; kişi
  kendini devre dışı bırakamaz/silemez.
- İzleyiciye monitör ayarları (başlıklar, gövde, şifreler), push token'ı ve
  bildirim kanalları gösterilmez; kısıtlıysa listede, detayda, özetlerde,
  olaylarda ve canlı akışta sadece izinli monitörler görünür.
- İşlem kaydı: kim, ne zaman, neyi değiştirdi (monitör, bildirim, kullanıcı,
  ayar, durum sayfası, giriş). Yöneticiler görür, 1 yıl saklanır.

## 11. Aşama 2b — Herkese açık durum sayfaları

- Birden fazla sayfa; adres `uptime.kadir.app/durum/<ad>`, isteğe bağlı özel alan
  adı (ör. `durum.kadir.app`; Coolify'a alan adı eklenir, DNS kaydı açılır).
- İçerik: başlık, açıklama, logo (PNG/JPEG/WebP, en fazla 512 KB), alt bilgi,
  gruplar halinde monitörler (her monitör için görünen ad), genel durum özeti,
  monitör başına 90 günlük uptime çubuğu ve yüzde, son 14 günün olayları.
- Gizlilik: hedef adresleri varsayılan olarak gizli; olay nedenleri (iç IP, hata
  ayrıntısı içerebilir) herkese açık sayfada gösterilmez. İsteğe bağlı şifre.
- Duyurular: sayfa başına bilgi/uyarı/sorun/çözüldü duyuruları, başlangıç-bitiş
  zamanıyla.
- Güvenlik ve yük: herkese açık uç noktalar giriş gerektirmez, sonuçlar kısa süre
  önbelleğe alınır; özel alan adından sadece durum sayfası ve herkese açık API
  erişilebilir (yönetim paneli değil). Sayfa 60 saniyede bir kendini yeniler.
- Yönetim: arayüzde "Durum sayfaları" bölümü (Yönetici ve Editör): oluştur,
  grupları ve monitörleri düzenle, önizle, yayınla/gizle, duyuru ekle.

### Uygulama sırası (2a + 2b)

1. Backend: migration, rol/kapsam kontrolü, kullanıcı ve durum sayfası API'leri,
   herkese açık uç noktalar, özel alan adı yönlendirmesi, işlem kaydı, testler.
2. Arayüz (ajan, tasarım düzeltmeleri bittikten sonra): kullanıcı yönetimi, zorunlu
   şifre değişimi, rol bazlı menü, durum sayfası yönetimi ve herkese açık sayfa.
3. Bağımsız inceleme (Fable, özellikle yetki atlatma denemeleri) → düzeltmeler →
   canlıya çıkış.

## 12. Aşama 4 — Sunucu takibi (Beszel benzeri)

Karar: 2026-09-27, kullanıcı onayladı. Amaç: siteler ve sunucular tek panelde,
aynı bildirim kanallarıyla. Beszel seviyesi: CPU, RAM, disk, ağ, yük, sıcaklık,
Docker konteynerleri, geçmiş grafikleri, eşik uyarıları.

### 12.1 Mimari kararlar

- **Tek program, iki ayrı kayıt türü** (karar 2026-09-27, kullanıcı: "uptime
  peer'ı için kullanacağımız alan ayrı, sunucu takibi ayrı olmalı"). Program aynıdır
  (`uptime probe`), ama `probes.kind` ile kayıtlar ayrılır:
  - `location` (kontrol noktası): atanan monitörleri kontrol eder, metrik göndermez;
    Ayarlar → Kontrol noktaları'nda ve monitörün Konumlar seçiminde görünür.
  - `server` (takip edilen sunucu): metrik gönderir, monitör kontrolü almaz, konum
    olarak atanamaz; yalnızca Sunucular sayfasında görünür ve ayarları oradadır.
  Aynı makine ikisi de olacaksa iki kayıt, iki token, iki kurulum. Ad çakışması tür
  içinde denetlenir (aynı makine iki listede aynı adı taşıyabilir).
- **Ana sunucunun kendi metrikleri** için de ajan kurulur (Coolify'da ayrı bir
  Docker Compose kaynağı). Uygulama konteyneri host'u görmediği için kendi kendini
  ölçmez.
- **Host görünürlüğü.** Konteyner içinde çalışan ajan, host `/proc`, `/sys`, `/`
  bağlanmadıysa metrik göndermez (konteynerin kendi değerleri yanıltıcı olur);
  sunucuya "metrics_unavailable" nedeni bildirilir, arayüzde "kurulum komutunu
  güncelleyin" uyarısı çıkar. Algılama: `/.dockerenv` veya `/proc/1/cgroup` ile
  konteyner tespiti + `HOST_PROC` ortam değişkeni.
- **Toplama kütüphanesi:** `github.com/shirou/gopsutil/v4` (saf Go, `HOST_PROC`,
  `HOST_SYS`, `HOST_ETC`, `HOST_ROOT` destekler). Docker konteyner istatistikleri
  Docker API'sinden (`/containers/json`, `/containers/{id}/stats?stream=false&one-shot=true`),
  mevcut `check/docker.go`'daki unix soket istemcisi yeniden kullanılır. CPU yüzdesi
  ardışık iki örnekteki kümülatif sayaç farkından hesaplanır (tek atış, ucuz).
- **Örnekleme:** ajan 60 sn'de bir örnek alır (sunucu `metrics_interval` ile
  değiştirebilir; 0 = kapalı) ve hemen gönderir. CPU, disk G/Ç ve ağ hızları iki
  örnek arasındaki farktır; ilk örnek hız alanları olmadan gider.

### 12.2 Kurulum komutları

Docker (varsayılan, arayüzde gösterilir):

    docker run -d --name uptime-agent --restart unless-stopped \
      --network host --pid host \
      -v /:/host:ro,rslave -v /var/run/docker.sock:/var/run/docker.sock:ro \
      -e HOST_PROC=/host/proc -e HOST_SYS=/host/sys -e HOST_ETC=/host/etc -e HOST_ROOT=/host \
      -e ADDR=- -e PROBE_SERVER=https://uptime.kadir.app -e PROBE_TOKEN=upr_… \
      alpine:3 sh -c 'wget -qO /usr/local/bin/uptime --header "Authorization: Bearer $PROBE_TOKEN" "$PROBE_SERVER/api/probe/binary" && chmod +x /usr/local/bin/uptime && exec uptime probe'

`--network host`: ağ sayaçları host'un olsun diye; sağlık uç noktası bu yüzden
kapalı (`ADDR=-`, host'ta 8080 çakışmasın). Doğrudan kurulum (systemd) için
arayüzde ikinci sekme: ikiliyi indirip `/etc/systemd/system/uptime-agent.service`
yazan tek satırlık betik; üçüncü sekme Windows (PowerShell, §12.10). Mevcut
kontrol noktaları eski komutla çalışmaya devam eder, sadece metrik göndermez.

### 12.3 Protokol

    POST /api/probe/metrics   (probe token, gövde ≤ 256 KB)
    ← {"time": ms, "host": {...}, "stats": {...}}   veya
    ← {"time": ms, "unavailable": "neden"}

`GET /api/probe/jobs` yanıtına `metrics_interval` (sn) eklenir. Veri tipleri tek
yerde tanımlıdır: `internal/metrics/types.go` (ajan ve sunucu aynı tipleri kullanır).
Sınırlar: en fazla 32 disk, 32 sıcaklık sensörü, 200 konteyner; ağ toplam olarak
gönderilir; metinler kırpılır (`Sample.Sanitize`).

### 12.4 Veri saklama

- `probes` tablosuna: `metrics` (açık/kapalı, varsayılan açık), `host_info` (JSON),
  `metrics_at` (son örnek zamanı), `metrics_note` (unavailable nedeni).
- `server_stats(probe_id, res, time, data)`, birincil anahtar `(probe_id, res, time)`.
  `data` JSON'dur (iki veritabanında da aynı çalışır, konteyner listesi esnek kalır).
  - `res=1`: 1 dakikalık örnekler, **24 saat** saklanır (1 sa / 24 sa grafikleri)
  - `res=10`: 10 dakikalık ortalamalar, **7 gün** (7 günlük grafik)
  - `res=60`: saatlik ortalamalar, **90 gün** (30 günlük grafik)
- Toplama (rollup) yeniden başlatmaya dayanıklıdır: yeni 1 dk örneği bir 10 dk
  sınırını geçince önceki 10 dk'lık kova veritabanındaki 1 dk satırlarından
  hesaplanıp yazılır (upsert, tekrar çalışması zararsız); 60 dk aynı şekilde 10 dk
  satırlarından. Ortalama alınır; CPU için ayrıca tepe değer (`cpu_max`) tutulur.
  Diskler ve sıcaklıklar için son/ortalama değer.
- Eski satırları mevcut günlük bakım işi (`stats/maintenance.go`) siler.
- Son örnek bellekte de tutulur (liste ekranı veritabanına gitmez).

### 12.5 Uyarılar

- `server_alerts(id, probe_id, metric, threshold, minutes, active, firing, fired_at)`:
  metrikler `cpu`, `mem`, `swap`, `disk` (en dolu bölüm), `load` (1 dk yük / çekirdek),
  `temp` (en sıcak sensör), `offline` (veri gelmiyor).
- Değerlendirme: her örnek geldiğinde, son `minutes` dakikadaki örneklerin
  ortalaması eşiği geçiyorsa uyarı başlar (bir kez bildirim); ortalama eşiğin
  altına inince biter (bir kez "düzeldi" bildirimi). `offline`: son örnekten beri
  `max(3 dk, 3 × aralık)` geçtiyse; 30 sn'de bir kontrol edilir.
- Yeni sunucuya varsayılan kurallar: çevrimdışı 3 dk, CPU %90 / 10 dk, RAM %90 / 10 dk,
  disk %85. Bildirim kanalları: `probe_notifications(probe_id, notification_id)`,
  yeni sunucuda varsayılan (is_default) kanallar seçili gelir.
- `notify.Event`'e `ProbeID` ve sunucu olay türleri (`server_alert`,
  `server_resolved`) eklenir; dağıtıcı `ProbeID` doluysa kanalları
  `probe_notifications`'tan alır. Mesaj örneği: "🔴 CP Server İstanbul: CPU %94
  (10 dk ortalama, eşik %90)".
- Uyarı geçmişi: `server_alert_events(id, probe_id, alert_id, metric, value, started_at, ended_at)`,
  90 gün; sunucu detayında gösterilir.
- Bakım pencereleri, durum sayfasında sunucu gösterimi, GPU, S.M.A.R.T. ve süreç
  listesi bu aşamada **yok**.

### 12.6 API

| Uç nokta | Yetki | Açıklama |
|---|---|---|
| `GET /api/servers` | izleyici (müşteri kısıtlı hariç) | ajan listesi + son örnek + uyarı durumu |
| `GET /api/servers/{id}` | aynı | host bilgisi, son örnek, kurallar, kanallar |
| `GET /api/servers/{id}/stats?range=1h\|24h\|7d\|30d` | aynı | grafik serisi |
| `GET /api/servers/{id}/events` | aynı | uyarı geçmişi |
| `PUT /api/servers/{id}/alerts` | editör | kuralların tamamını değiştirir |
| `PUT /api/servers/{id}/notifications` | editör | kanal seçimi |
| `PUT /api/probes/{id}` | yönetici | mevcut; `metrics` alanı eklenir |
| `POST /api/probe/metrics` | ajan token'ı | örnek gönderimi |

Canlı akışa `server` olayı (son örnek özeti) eklenir; müşteri kısıtlı
izleyicilere gönderilmez. Tüm değişiklikler işlem kaydına yazılır.

### 12.7 Arayüz

- Üst menüde yeni **Sunucular** sekmesi (`#/servers`). Liste: durum noktası
  (çevrimiçi/çevrimdışı/uyarı), ad, host adı ve işletim sistemi, CPU/RAM/disk
  doluluk çubukları (%), ağ ↓↑, yük, çalışma süresi. Mobilde kart görünümü.
  Metrik göndermeyen ajanlar altta sönük ve "kurulum komutunu güncelleyin"
  bağlantısıyla.
- Sunucu detayı (`#/servers/{id}`): üstte host bilgisi ve anlık değerler; aralık
  seçici (1 sa / 24 sa / 7 gün / 30 gün); grafikler: CPU (ortalama + tepe), RAM ve
  swap, disk G/Ç, ağ, yük, sıcaklık (varsa); disk bölümleri tablosu (doluluk
  çubuklarıyla); konteyner tablosu (CPU, RAM, ağ; sıralanabilir) ve en çok CPU
  kullanan 5 konteynerin grafiği; uyarı kuralları düzenleyici; bildirim kanalları;
  uyarı geçmişi.
- **Sunucu ekle** düğmesi: ad sorar, ajan oluşturur, Docker / systemd sekmeli
  kurulum komutunu gösterir. Ayarlar → Kontrol noktaları aynı ajanları konum
  olarak yönetmeye devam eder; iki ekran birbirine bağlantı verir.
- Grafikler için genel bir `LineChart.svelte` (SVG, çok seri, dokunmatik uyumlu
  ipucu, birim biçimleyici); mevcut görsel dil (teal vurgu) korunur.

### 12.8 Uygulama sırası ve iş bölümü

0. (ben) Plan + `internal/metrics/types.go` sözleşmesi.
1. Paralel ajanlar (Opus; dosya sahipliği ayrık):
   - **A — Sunucu tarafı:** migration 10, store, alım uç noktası, rollup, saklama,
     uyarı motoru, bildirim uzantısı, API, canlı olay, testler (SQLite + PostgreSQL).
   - **B — Ajan:** `internal/metrics` toplayıcı (gopsutil + Docker), host
     görünürlüğü tespiti, probe istemcisine metrik döngüsü, kurulum komutları
     (Docker + systemd), testler.
   - **C — Arayüz:** Sunucular listesi, detay, grafikler, uyarı düzenleyici,
     kurulum penceresi (bu plandaki API sözleşmesine göre).
2. (ben) Birleştirme, uçtan uca test (yerelde ajan + sunucu), ekran görüntüleri.
3. Fable ile kısa son inceleme (yetki, ajan token'ı ile erişim, veri boyutu sınırları).
4. Canlıya çıkış: Coolify deploy; ana sunucuya Coolify üzerinden ajan; CP Server
   İstanbul'daki konteyner yeni komutla güncellenir (kullanıcı çalıştırır).

### 12.9 JSON sözleşmesi (arayüz ↔ sunucu)

Zamanlar unix **saniye**; `Host` ve `Stats` alanları `internal/metrics/types.go`
ile aynıdır.

`GET /api/servers` → `{"servers": [ServerView]}`

    ServerView = {
      "id": 1, "name": "CP Server İstanbul",
      "active": true,            // ajan etkin mi (probes.active)
      "metrics": true,           // metrik toplama açık mı
      "state": "online",         // online | offline | unavailable | waiting | disabled
                                 //   waiting: hiç örnek gelmedi; unavailable: ajan "toplayamıyorum" dedi;
                                 //   disabled: ajan veya metrik kapalı; offline: son örnek çok eski
      "note": "",                // unavailable nedeni
      "interval": 60,            // örnek aralığı (sn)
      "last_seen_at": 0,         // ajanın son isteği (jobs/results/metrics)
      "metrics_at": 0,           // son örnek
      "version": "…",
      "host": Host | null,
      "latest": Stats | null,    // listede containers ve temps boş gelir, yerine:
      "container_count": 12,
      "temp_max": 54.5 | null,
      "firing": ["cpu"]          // şu an tetiklenmiş uyarı metrikleri
    }

`GET /api/servers/{id}` → ServerView (latest tam: containers, temps dahil) +
`"alerts": [AlertRule]`, `"notification_ids": [1,2]`.

    AlertRule = {"id": 3, "metric": "cpu", "threshold": 90, "minutes": 10,
                 "active": true, "firing": false, "fired_at": 0}
    metric: cpu | mem | swap | disk | load | temp | offline
    threshold: yüzde (cpu, mem, swap, disk), çekirdek başına yük (load), °C (temp);
               offline için yok sayılır. minutes: 1-60 (offline: çevrimdışı kalma süresi).

`GET /api/servers/{id}/stats?range=1h|24h|7d|30d` →

    {"range": "24h", "res": 1, "from": 0, "to": 0, "interval": 60,
     "points": [{"t": 0, "cpu": 12.5, "cpu_max": 40, "load1": 0.4, "load5": 0.3, "load15": 0.2,
                 "mem_used": 0, "mem_cache": 0, "mem_total": 0, "swap_used": 0, "swap_total": 0,
                 "disk_read_bps": 0, "disk_write_bps": 0, "net_rx_bps": 0, "net_tx_bps": 0,
                 "disk_pct": 71.2, "temp": 48 | null,
                 "containers": [{"name": "x", "cpu": 1.2, "mem": 0}]}]}

res: 1h ve 24h → 1 (dk), 7d → 10, 30d → 60. Boşluklar (ajan kapalıyken) nokta
olmadan gelir; arayüz `interval`'in 2 katından büyük aralıkta çizgiyi keser.

`GET /api/servers/{id}/events` → `{"events": [{"id", "metric", "value", "threshold",
"started_at", "ended_at" | null}]}` (son 90 gün, yeniden eskiye, en fazla 200).

`PUT /api/servers/{id}/alerts` ← `{"alerts": [{"metric", "threshold", "minutes", "active"}]}`
→ `{"alerts": [AlertRule]}`. Aynı metrik iki kez olamaz.

`PUT /api/servers/{id}/notifications` ← `{"notification_ids": [1,2]}`.

Sunucu ekleme `POST /api/servers` (yönetici) ile yapılır; yanıtta `docker_agent` ve
`systemd` komutları vardır. `POST /api/probes` yalnızca kontrol noktası ekler
(`docker_command`). Ad/etkinlik/metrik/token/silme için iki tür de mevcut
`/api/probes/{id}` uçlarını kullanır. Canlı akış olayı:
`{"type": "server", "data": ServerView (liste biçimi)}`.

### 12.10 Windows sunucular

Karar: 2026-09-27. Windows Server'lar Linux'takiyle aynı ajanla (`uptime probe`)
izlenir; ayrı program yok.

- **Toplayıcı** (`internal/metrics/source_windows.go`, saf seçimler
  `winfilters.go`'da, Linux'ta test edilir): CPU tüm işlemcilerin
  kullanıcı/çekirdek/boşta sürelerinden; RAM kullanılan = toplam − kullanılabilir,
  önbellek = bekleme listesi (PDH `\Memory\Standby Cache *`, okunamazsa 0); swap =
  sayfa dosyaları; diskler yalnızca yerel sabit birimler (sürücü harfi `C:` ve
  klasöre bağlanmış birimler; USB, ağ, CD, RAM diski atlanır; aynı birim bir kez);
  disk G/Ç sabit sürücü harflerinin toplamı; ağ sanal olmayan bağdaştırıcılar
  (loopback, `vEthernet`/Hyper-V, isatap, Teredo, WAN Miniport, Bluetooth,
  VirtualBox/VMware host-only hariç; VPN'ler Linux'taki gibi sayılır). Host:
  "Microsoft Windows Server 2022 Datacenter 21H2", çekirdek `10.0.20348.2340`.
- **Yük:** Windows'ta yük ortalaması yok. gopsutil'in (psutil ile aynı) taklidi
  kullanılır: `\System\Processor Queue Length` 5 sn'de bir örneklenip 1/5/15 dk
  üstel ortalaması alınır. Yalnızca işlemci *bekleyen* iş parçacıklarını sayar,
  çalışanları saymaz: değer Linux yükünden düşük çıkar, ilk dakikalarda 0'dan
  yükselir. `load` uyarısı Windows'ta ancak ağır doygunlukta tetiklenir.
- **Yok:** sıcaklık (WMI termal bölgeleri çoğu sunucuda yok/anlamsız), Docker
  konteynerleri (Windows'ta kapalı), konteyner/host görünürlüğü tespiti (yalnızca Linux).
- **Hizmet:** `uptime probe` hizmet yöneticisi başlattıysa Windows hizmeti olarak
  çalışır (`golang.org/x/sys/windows/svc`; Durdur/Kapat'ta bağlam iptal edilir).
  `uptime service install` (Yönetici): ayarları yazar, çalışan hizmeti durdurur,
  kendini `%ProgramFiles%\Uptime\uptime.exe`'ye kopyalar, `uptime-agent` hizmetini
  (otomatik başlangıç, LocalSystem) kurar/günceller, kurtarma: 10 sn / 30 sn / 60 sn'de
  yeniden başlat (hatayla çıkışta da), başlatır. `uptime service uninstall` hizmeti
  ve token dosyasını siler.
- **Ayarlar ve token:** `%ProgramFiles%\Uptime\agent.env` (KEY=DEĞER). Klasör ve
  dosyanın sahibi Administrators, DACL korumalı: yalnızca SYSTEM ve Administrators.
  Hizmetin kayıt defterindeki `Environment` değeri seçilmedi (Services anahtarları
  Users'a okunur). Günlük `%ProgramFiles%\Uptime\agent.log`, 5 MB'ta `agent.log.1`.
  Sağlık uç noktası kapalı (`ADDR=-`).
- **Dağıtım:** `GET /api/probe/binary?os=windows&arch=amd64` imajdaki
  `/usr/local/share/uptime/agents/uptime-windows-amd64.exe` dosyasını verir
  (`AGENT_DIR` ile değiştirilebilir; parametresiz istek eskisi gibi sunucunun kendi
  programı; dosya yoksa açıklamalı 404). Dockerfile'lar `AGENT_PLATFORMS`
  (varsayılan `windows/amd64`, ör. `"windows/amd64 linux/arm64"`) için aynı sürümle
  çapraz derler; her platform imaja ~38 MB (sıkıştırılmış ~13 MB) ekler.
- **Kurulum komutu:** `POST /api/servers` yanıtında `windows` alanı; arayüzde
  "Windows" sekmesi. Yönetici PowerShell'de tek satır: TLS 1.2 açılır, ilerleme
  çubuğu kapatılır, `PROBE_SERVER`/`PROBE_TOKEN` oturum ortamına yazılır (token
  süreç argümanı olmaz), program `uptime-setup.exe` olarak indirilir,
  `service install` çalışır, geçici dosya ve ortam değişkeni silinir. Tekrar
  çalıştırmak günceller. PSReadLine 2.2+ "token" geçen satırı geçmiş dosyasına
  yazmaz; daha eski sürümlerde satır kullanıcının geçmiş dosyasında kalabilir.
- Windows'ta ping kontrolü (ajan kontrol noktası olarak kullanılırsa) ayrıcalıklı
  ICMP ile yapılır (hizmet LocalSystem).

## 13. Olay ayrıntıları (UptimeRobot benzeri olay sayfası)

Karar: 2026-09-27. Bugün bir olay yalnızca "çözüldü, X sürdü" gösteriyor. Hedef:
her olayın kendi sayfası (`#/incidents/{id}`): kök neden, durum, süre, konumlar,
işlem geçmişi ve (HTTP'de) hatayı üreten isteğin ve yanıtın kendisi.

### 13.1 Yakalama (capture)

- `check.Result`'a isteğe bağlı `Detail *check.Detail` eklenir. Yalnızca **HTTP**
  denetçisi doldurur ve yalnızca **başarısız** kontrolde (başarılı kontrolde ek
  maliyet yok). Diğer tiplerde sadece hata mesajı vardır; arayüzde İstek/Yanıt
  kartları gizlenir.
- İçerik: istek (metot, adres, başlıklar), yanıt (durum kodu ve metni, protokol,
  başlıklar, gövde, son adres — yönlendirme sonrası), yanıt alınamadıysa hata.
- **Gövde:** en fazla 16 KB; yalnızca metin türleri (`text/*`, JSON, XML,
  JavaScript, form, YAML, CSV; tür yoksa içerikten koklanır ve geçerli UTF-8
  olmalı). İkili içerikte gövde yerine `(ikili içerik, N bayt)`. Kırpıldıysa
  `body_truncated` ve bilinen toplam boyut (`body_size`, Content-Length veya
  okunan).
- **Maskeleme (yakalama anında, saklanmadan önce):**
  - İstek başlıkları: `Authorization`, `Proxy-Authorization`, `Cookie` her zaman;
    kullanıcının girdiği başlıkların değerleri (`headers` ayarı monitorSecrets'ta
    gizli alan) zararsız bilinen adlar (`Accept*`, `Content-Type`, `User-Agent`,
    `Cache-Control`, `Host`, `Origin`, `Referer`…) dışında maskelenir.
  - Yanıt başlıkları: `Set-Cookie`, `Authorization`, `Proxy-Authorization`,
    `Cookie` maskelenir.
  - Gizli değerler (maskelenen başlık değerleri, basic auth şifresi, proxy şifresi,
    OAuth istemci sırrı ve alınan erişim token'ı; ≥ 4 karakter) yanıt
    başlıklarında ve gövdede geçiyorsa `••••••` ile değiştirilir (yankılayan API'ler).
  - Adresteki kullanıcı şifresi gizlenir (`url.Redacted`).
- Sınırlar (`Detail.Sanitize`): en fazla 64 başlık, ad 128 / değer 2048 karakter,
  adres 2048, hata 500 karakter, geçersiz UTF-8 temizlenir.

### 13.2 Uzak kontrol noktaları

- Sonuç protokolüne isteğe bağlı `detail` alanı eklenir (`POST /api/probe/results`).
  Eski ajanlar göndermez, sorun olmaz; eski sunucu bilinmeyen alanı yok sayar.
- Ajan tek partide en fazla 256 KB ayrıntı gönderir (fazlası o parti için atılır,
  sonuç yine gider): böylece eski sunucunun 1 MB gövde sınırı aşılmaz.
- Sunucu gelen ayrıntıyı yeniden `Sanitize` eder ve monitörün kendi ayarıyla
  maskelemeyi tekrar uygular (ajana tam güvenilmez). Gövde sınırı 1 → 2 MB.
- Motor her konumun son başarısız ayrıntısını bellekte tutar; olay açılınca
  kullanılır. Öncelik: ana sunucu, sonra kimliğe göre ilk çalışmayan konum.

### 13.3 Veri modeli (migration 11)

    incident_events(id, incident_id → incidents ON DELETE CASCADE, time, kind,
                    location, message, data)          -- data: JSON ('' = yok)
    incident_captures(incident_id PK → incidents ON DELETE CASCADE, time,
                      location, data)                  -- data: check.Detail JSON

- **Yalnızca ilk** başarısız yakalama saklanır (olayı açan kontrol); olay sürerken
  güncellenmez — kök nedeni o gösterir, yazma yükü olmaz. Hata değişirse işlem
  geçmişine "hata değişti" olayı düşer.
- Olay başına en fazla **500** olay kaydı; sınıra gelince tek bir "sınıra
  ulaşıldı" kaydı yazılır, sonrası atılır (çözülme kaydı her zaman yazılır).
  Çok sık kesilip düzelen monitör sınırsız büyüyemez.
- Saklama: olay kayıtları olayla yaşar (olaylar silinmez; monitör silinince
  cascade). Yakalamalar büyük olduğu için çözülmesinden **90 gün** sonra
  bakım işinde silinir (olay sayfası o zaman İstek/Yanıt'sız görünür).
- SQLite + PostgreSQL: ortak DDL alt kümesi, `?` yer tutucular, `insertID`.

### 13.4 İşlem geçmişi türleri (`kind`)

| kind | Ne zaman | Mesaj / data |
|---|---|---|
| `retry` | Olay açılmadan önceki başarısız denemeler (bellekte tutulur, olay açılınca kendi zamanlarıyla yazılır; en fazla 20) | hata; `{attempt, max}` |
| `down` | Olay başladı | kök neden; `location` gözlendiği yer; `{locations:[{probe_id,name,status,message}]}` |
| `change` | Olay sürerken hata mesajı değişti (çok konumluda birleşik mesaj değil, konum başına: `location` dolu) | yeni hata |
| `location` | Çok konumluda olay sürerken bir konumun durumu değişti | durum + mesaj |
| `reminder` | Hatırlatma bildirimi tetiklendi | `{downtime}` |
| `maint_start` / `maint_end` | Olay sürerken bakım penceresi başladı/bitti | — |
| `notify` | Her kanala gönderim sonucu (down/up/reminder) veya "bağlı kanal yok" | `{event, channel, type, ok, error}` |
| `edited` / `paused` | Olay sürerken monitör düzenlendi / durduruldu (durdurma olayı kapatır) | `{user}` |
| `up` | Olay çözüldü | son kontrol mesajı; `{downtime}` |
| `limit` | 500 sınırına ulaşıldı | — |

Bildirim kaydı: `notify.Event`'e `IncidentID` eklenir; dağıtıcı (zaten arka
planda) her kanalın sonucunu `incident_events`'e yazar. Hata metni temizlenir:
`redactURLError`, kanal ayarındaki gizli değerler maskelenir, adreslerin yolu
atılır, 200 karakter. Sunucu takibi uyarıları (`ProbeID != 0`) kaydedilmez.
Bildirimlerdeki "Detay" bağlantısı down/up/hatırlatma için olay sayfasıdır.

### 13.5 Yetki

| | Editör / yönetici | İzleyici | Müşteri kısıtlı izleyici |
|---|---|---|---|
| Kök neden, durum, süre, konumlar | ✓ | ✓ (mesajlar `viewerMessage` ile temizlenir) | yalnızca izinli monitörler; aksi 404 |
| İşlem geçmişi | tam | bildirim kayıtları yok, kullanıcı adı yok | aynı |
| İstek / yanıt, hedef adresin tamamı | ✓ | ✗ (`publicTarget`) | ✗ |

Herkese açık durum sayfaları değişmez (olay nedeni zaten gösterilmiyor).

### 13.6 API

- `GET /api/incidents/{id}` →

      {"incident": Incident,
       "monitor": {"id","name","type","target","active","status"},
       "location": "Ana sunucu",             // kök nedenin gözlendiği yer
       "locations": [{"probe_id","name","status","message"}],  // olay başında
       "events": [{"id","time","kind","location","message","data"}],  // yeniden eskiye
       "capture": null | {"time","location","detail": Detail},  // yalnızca editör+
       "details": true}                       // kullanıcı istek/yanıtı görebilir mi

  Eski olaylarda kayıt yoksa başlangıç/çözülme kayıtları olay satırından üretilir.
- `GET /api/monitors/{id}`: `open_incident_id` eklenir (detaydaki uyarıdan bağlantı).
- Mevcut uç noktalar değişmez (liste zaten `id` içeriyor).

### 13.7 Arayüz

- Yeni rota `#/incidents/{id}` (`IncidentDetail.svelte`). Masaüstünde iki sütun:
  solda kartlar (Kök neden; Durum + Süre yan yana; Konumlar; İşlem geçmişi
  zaman çizelgesi), sağda İstek (URL / Başlıklar sekmeleri) ve Yanıt (Gövde /
  Başlıklar sekmeleri, kopyala düğmeleri). ≤ 960 px tek sütun.
- Başlık: durum noktası, "Süren olay: <monitör>" / "Çözülen olay: <monitör>",
  tip rozeti + hedef, "Monitöre git" ve "Yanıtı indir" (yakalamayı JSON dosyası
  olarak indirir; tarayıcıda üretilir).
- Süre sürerken canlı artar; monitörün durumu değişince (canlı akış) sayfa
  yeniden yüklenir.
- Bağlantılar: Olaylar listesi ve monitör detayındaki olay tablosu (satır
  tıklanabilir), monitör detayındaki "devam eden kesinti" uyarısı.
- Teal vurgu, mevcut kart/rozet stilleri; mor yok.

## 14. Yeni konumun ilk sonucu (uzun yoklama)

Karar: 2026-09-29. Sorun: uzak konumlu yeni monitörde konum ~30–45 sn "Sonuç
yok" görünüyor, mesaj "200 OK (sonuç gelmeyen: …)" oluyordu. Nedenleri: ajan
iş listesini 30 sn'de bir yokluyordu (yeni iş 30 sn'ye kadar gecikiyordu) ve
başlangıç payı yalnızca hiçbir konumdan sonuç yokken geçerliydi (ana sunucu
hemen sonuç verdiği için uzak konum anında "bilinmiyor" sayılıyordu).

- **Konum başına "ilk sonuç bekleniyor" (`waiting`):** hiç sonuç vermemiş konum,
  runner'a eklendiği andan itibaren `grace` süresince `waiting`. Süre
  `max(staleAfter, ProbePollAfter + min(aralık, 10) + zaman aşımı + 15)` birim:
  ajanın işi en geç öğrenmesi + ilk kontrol kaydırması + kontrol + gönderim payı;
  işi zaten çalıştıran ajan (sunucu güncellemesi, iş değişmedi) için sıradaki
  planlı kontrole kadar 3 aralık. `waiting` "sonuç gelmeyen" listesine girmez;
  kurala oy vermez ama DOWN kararında paydada kalır (yalnızca oy vermemiş konum
  yüzünden DOWN denmez: "all"da ana sunucu çalışmıyor, uzak konum bekleniyorsa
  PENDING). Süre dolunca `unknown` (eski davranış). Olay geçmişine yazılmaz
  (ara durum). Konum durumu değişince canlı akışa `locations` olayı gider.
- **Yeniden yüklemede sonuçlar korunur:** düzenleme / konum ayarı / ajan
  değişikliğinde (≤ 10 sn içinde yeniden başlayan runner, kontrol ayarı aynı)
  mevcut konumlar son sonuçlarını korur, yalnızca yeni konum bekler.
- **Uzun yoklama:** `GET /api/probe/jobs?since=N` → `{"version": N, "poll_after": 1, …}`.
  Motor iş listesini etkileyen her değişiklikte sürümü artırır
  (`Engine.JobsChanged`; Reload/Remove ve API'de durdurma, silme, toplu işlem,
  içe aktarma, ajan ayarı/token/silme). Ajanın listesi güncelse istek en fazla
  20 sn bekletilir, değişiklikte (250 ms birleştirme ile) hemen yanıtlanır.
  Eski ajanlar `since` göndermez: sunucu ajan başına son verdiği sürümü ve
  zamanı bellekte tutar; sürüm aynıysa ve önceki istek 60 sn içindeyse bekletir.
  Bekletilen istek kimlik doğrulaması/IP kilidi/istek sınırından geçmiştir
  (sınıra bir kez sayılır), uyanınca ajan yeniden okunur (devre dışı → 403,
  token yenilendi/silindi → 401). Kapanışta context iptal olur, güncel liste
  verilir. Boşta ajan dakikada ~3 istek atar; çevrimdışı sayılma (90 sn) etkilenmez.
- **Yeni ajan:** `since` gönderir, iş listesi isteğinin zaman aşımı 60 sn;
  sonradan eklenen (≤ 20) iş ilk kontrolünü ~1 sn içinde yapar ve ilk sonucu
  5 sn'lik gönderim aralığını beklemeden gönderilir (ilk liste ve toplu
  değişiklik eskisi gibi min(aralık, 10) içine yayılır).
