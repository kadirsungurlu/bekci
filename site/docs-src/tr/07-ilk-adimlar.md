---
id: first-steps
title: İlk adımlar
nav: İlk adımlar
description: Kurulumdan sonra ilk on dakika - yönetici hesabı, ilk monitör, bildirim kanalı, durum sayfası, dil ayarları ve telefona ekleme.
section: Kullanım
order: 7
slug: ilk-adimlar
---

Bekci çalışıyor ve paneli tarayıcıda açabiliyorsunuz. Bu sayfa, ilk on dakikada yapmanız gerekenleri sırayla anlatır. Masaüstünde menü solda, telefonda ekranın altındadır; telefonda **Ayarlar** gibi bazı bölümler **Daha fazla** menüsünün içindedir.

## 1. Yönetici hesabı {#yonetici}

Paneli ilk açtığınızda **Hoş geldiniz** ekranı yönetici hesabını oluşturmanızı ister:

- **Kullanıcı adı:** 3–32 karakter; harf, rakam, nokta, tire ve alt çizgi.
- **Şifre:** en az 8 karakter. **Şifre (tekrar)** alanına aynısını yazın.

**Hesabı oluştur** düğmesine basınca doğrudan panele girersiniz. Bu ekran yalnızca bir kez, hiç kullanıcı yokken görünür.

> [!TIP] İki adımlı doğrulamayı açın
> **Ayarlar → Hesabım → İki adımlı doğrulama → Etkinleştir** ile girişte telefonunuzdaki doğrulama uygulamasının (Google Authenticator, Authy, 1Password…) ürettiği kodu da isteyebilirsiniz. Kurtarma kodlarını güvenli bir yere kaydedin. Şifrenizi unutursanız: [şifre sıfırlama](/docs/sorun-giderme/#sifre-sifirlama).

## 2. İlk monitörünüzü ekleyin {#ilk-monitor}

1. **Monitörler** sayfasında **Monitör ekle** (ya da sağ üstteki **Yeni**) düğmesine basın.
2. **Monitör tipi** olarak **HTTP(S)** seçin (bir web sitesini ya da API'yi izlemek için).
3. **Ad** alanına anlaşılır bir ad yazın (ör. “Şirket web sitesi”), **Adres (URL)** alanına sitenin adresini yazın (ör. `https://ornek.com`).
4. **Kontrol aralığı** ne sıklıkla kontrol edileceğidir: en az 20 saniye, en fazla 24 saat. Başlangıç için 60 saniye iyi bir seçimdir.
5. **Monitörü ekle** düğmesine basın.

![Yeni monitör formu: web, ağ, veritabanı ve sistem kategorilerinde monitör tipi seçici](/img/yeni-monitor-1600.webp)

> [!CHECK]
> Monitör listede görünür ve birkaç saniye içinde ilk kontrol yapılır. Site çalışıyorsa durumu yeşile döner; yanıt süresi ve durum çubukları her kontrolde sayfayı yenilemeden güncellenir.

### Monitör tipleri {#monitor-tipleri}

**Monitör tipi** seçicisinde 18 tip dört grupta toplanır:

| Grup | Tipler | Ne için |
|---|---|---|
| Web | HTTP(S) | Web siteleri ve API'ler; başlık, gövde, kimlik doğrulama, kelime ve JSON sorgusu kontrolü, SSL bitiş uyarısı |
| Ağ ve protokoller | TCP Port, Ping, DNS, TLS sertifikası, SMTP, WebSocket, gRPC, MQTT, SNMP | Sunucular, portlar, alan adları, posta sunucuları ve ağ cihazları |
| Veritabanı | MySQL / MariaDB, PostgreSQL, Microsoft SQL Server, Redis, MongoDB | Veritabanına bağlanıp basit bir sorgu çalıştırır |
| Sistem ve sinyaller | Docker konteyner, Push, Grup | Konteyner sağlığı; cron işlerinin düzenli sinyal göndermesi; birden çok monitörün tek durumda toplanması |

**Push** monitörü ters yönde çalışır: Bekci bir yeri kontrol etmez, sizin cron işinizin ya da betiğinizin belirli aralıklarla kendisine haber vermesini bekler. Monitörü kaydettikten sonra size özel bir adres verilir; işiniz her çalıştığında bu adresi çağırır, çağrı gelmezse monitör **çalışmıyor** olur:

```bash
curl -fsS "https://⟦bekci.ornek.com⟧/api/push/⟦TOKEN⟧?status=up&msg=tamam&ping=120"
```

Hata bildirmek için `status=down` gönderin.

## 3. Bildirim kanalı ekleyin {#bildirim}

Bir monitör çalışmadığında haberiniz olsun diye en az bir kanal ekleyin. WhatsApp, Telegram, e-posta, Slack, Discord, Microsoft Teams, ntfy, PagerDuty, webhook ve diğerleri dahil 24 kanal vardır.

1. **Bildirimler** sayfasında **Yeni kanal** düğmesine basın.
2. **Tip** listesinden kanalı seçin ve istenen bilgileri girin (ör. Telegram için **Bot token’ı** ve **Sohbet kimliği (chat ID)**).
3. **Test gönder** ile deneyin; test mesajı birkaç saniye içinde gelmelidir.
4. Yeni monitörlerde bu kanal otomatik seçili gelsin diye **Yeni monitörlere varsayılan olarak ekle** kutusunu işaretleyin. Mevcut monitörlerinize de eklemek için **Mevcut tüm monitörlere ekle** kutusunu işaretleyin.
5. **Kaydet** düğmesine basın.

Bildirimler kısa bir başlıkla gelir: 🔴 kesinti, 🟢 düzelme (hâlâ çalışmayan konumlar varsa listelenir), 🟡 konum kesintisi, ⚠️ SSL uyarısı; e-posta hem HTML hem düz metin içerir. Kayıtlı bir kanala **Örnek bildirimleri gönder** ile her türden birer örnek gönderebilirsiniz. Geçici bir hata (ağ, HTTP 5xx/429) olursa gönderim 5 ve 20 saniye sonra yeniden denenir; sonuç olayın işlem geçmişine yazılır.

**Webhook** kanalı JSON gönderir: `event` (`down`, `up`, `reminder`, `location_down`, `location_up`, `cert`, `server_alert`, `server_resolved`, `test`), `title`, `text`, `message`, `time`, `downtime_seconds` (düzelmede), `cert_days` (SSL uyarısında), `monitor` (`id`, `name`, `type`, `target`, `url`), `incident` (`id`, `url`; olaya bağlı bildirimlerde), `locations` (çok konumlu monitörde çalışmayan konumlar: `name`, `message`) ve `server` (yalnızca sunucu uyarılarında: `id`, `name`, `metric`, `value`, `threshold`, `minutes`). Örnek gövde kanal penceresinde görünür.

> [!CHECK]
> Test bildirimi kanalınıza ulaştı ve kanal **Bildirimler** listesinde görünüyor. Bir monitörün kanallarını monitörün düzenleme sayfasından da değiştirebilirsiniz.

## 4. Durum sayfası oluşturun (isteğe bağlı) {#durum-sayfasi}

Durum sayfası, müşterilerinize servislerinizin durumunu, planlı bakımları ve geçmiş olayları gösteren herkese açık bir sayfadır.

1. **Durum sayfaları** bölümünde **Yeni sayfa** düğmesine basın.
2. **Başlık** yazın (ör. “Acme Servis Durumu”) ve **Adres (kısa ad)** alanına küçük harf, rakam ve tireden oluşan bir ad verin (ör. `acme`).
3. **Gruplar ve monitörler** bölümünde bir grup adı yazın (ör. “Web siteleri”) ve **Monitör ekle** ile monitörlerinizi seçin.
4. **Yayında** anahtarını açın. İsterseniz **Sayfa şifresi** ile sayfayı şifreleyebilirsiniz.
5. **Sayfayı oluştur** düğmesine basın.

> [!CHECK]
> Sayfanız `https://⟦bekci.ornek.com⟧/durum/⟦acme⟧` adresinde açılır. Giriş yapmamış bir tarayıcıda (ör. gizli pencerede) açıp deneyin.

Sayfayı `durum.ornek.com` gibi ayrı bir alan adından yayınlamak için sayfanın **Özel alan adı** alanını kullanın; alan adının DNS kaydı sunucunuzu göstermeli ve vekiliniz bu adı Bekci'ye yönlendirmelidir ([Caddy'li kurulumda nasıl yapılır](/docs/kurulum/caddy/#durum-alan-adi)).

## 5. Dil ayarları {#dil}

Bekci'nin arayüzü Türkçe ve İngilizcedir. Üç ayrı dil ayarı vardır:

| Ne | Nereden | Kimi etkiler |
|---|---|---|
| Arayüz dili | **Ayarlar → Hesabım → Dil → Arayüz dili** (giriş ekranında da dil seçici var) | Yalnızca sizin hesabınızı; giriş yaptığınız her cihazda geçerlidir |
| Durum sayfası dili | Durum sayfası düzenleyicisinde **Sayfa dili** | O sayfanın ziyaretçilerini (durum metinleri, tarihler, süreler) |
| Bildirim dili | **Ayarlar → Genel → Bildirim dili** | Tüm kanallara giden bildirim mesajlarını |

## 6. Telefonunuza ekleyin {#telefon}

Bekci'yi telefonunuza bir uygulama gibi ekleyebilirsiniz; mağazadan bir şey indirmeniz gerekmez:

- **iPhone ve iPad:** Paneli **Safari** ile açın → **Paylaş** düğmesi → **Ana Ekrana Ekle**.
- **Android:** Paneli **Chrome** ile açın → sağ üstteki menü → **Uygulamayı yükle** (bazı sürümlerde **Ana ekrana ekle**).

Ana ekrandaki simgeden açıldığında tam ekran çalışır ve canlı güncellenir. Android'de **Uygulamayı yükle** seçeneği yalnızca panel HTTPS ile açıldığında görünür; `http://SUNUCU-IP:8080` gibi bir adreste görünmez.

## Sonra ne yapabilirsiniz? {#sonra}

- **Sunucularınızı izleyin:** CPU, RAM, disk ve ağ ölçümleri için [sunucu ajanını kurun](/docs/sunucu-ajani/).
- **Başka konumlardan kontrol edin:** Monitörlerinizi başka bir şehirden ya da ağdan da kontrol etmek için bir [kontrol noktası](/docs/sunucu-ajani/#kontrol-noktasi) ekleyin.
- **Ekibinizi ekleyin:** **Ayarlar → Kullanıcılar** bölümünden **Yönetici**, **Editör** ya da **İzleyici** rolünde hesaplar açın; müşterileriniz için yalnızca kendilerine atanan monitörleri gören hesaplar oluşturabilirsiniz.
- **Planlı bakımlarda bildirimleri susturun:** **Bakım** bölümü.
- **Başka bir servisten taşıyın:** **Ayarlar → Yedekle / Geri yükle** bölümünden UptimeRobot hesabınızı ya da Uptime Kuma yedeğinizi içe aktarın.
- **Yedeklemeyi ayarlayın:** [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/).
- **Prometheus/Grafana'ya bağlayın:** `GET /metrics` ucu monitör düzeyinde durum, yanıt süresi ve uptime verir. **Ayarlar → API anahtarları** bölümünden bir anahtar alın ve `Authorization: Bearer upk_…` başlığıyla ya da Basic kimlikle (kullanıcı `metrics`, şifre anahtar) çağırın; anahtarın izleyici yetkisi yeter.
