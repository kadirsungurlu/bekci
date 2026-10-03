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

> [!TIP] Yavaş yanıt uyarısı
> Site açık ama yavaşsa haberiniz olsun diye **Gelişmiş ayarlar → Yanıt süresi eşiği (ms)** alanını doldurun (ör. 2000). Son N başarılı kontrolün (**Ortalama penceresi**, varsayılan 3) ortalama yanıt süresi eşiği aşınca monitöre **Yavaş** rozeti gelir, bağlı kanallara 🟡 “yavaş yanıt veriyor” bildirimi gider ve **Olaylar** sayfasında “Yavaş yanıt” türünde bir olay açılır; ortalama eşiğin %90 altına inince 🟢 ile kapanır. Durum “Çalışıyor” kalır, uptime etkilenmez; monitör çalışmaz olursa yavaş yanıt olayı sessizce kapanıp normal kesinti olayı açılır.

### Monitör tipleri {#monitor-tipleri}

**Monitör tipi** seçicisinde 18 tip dört grupta toplanır:

| Grup | Tipler | Ne için |
|---|---|---|
| Web | HTTP(S) | Web siteleri ve API'ler; başlık, gövde, kimlik doğrulama, kelime ve JSON sorgusu kontrolü, SSL bitiş uyarısı |
| Ağ ve protokoller | TCP Port, Ping, DNS, TLS sertifikası, SMTP, WebSocket, gRPC, MQTT, SNMP | Sunucular, portlar, alan adları, posta sunucuları ve ağ cihazları |
| Veritabanı | MySQL / MariaDB, PostgreSQL, Microsoft SQL Server, Redis, MongoDB | Veritabanına bağlanıp basit bir sorgu çalıştırır |
| Sistem ve sinyaller | Docker konteyner, Push, Grup | Konteyner sağlığı; cron işlerinin düzenli sinyal göndermesi; birden çok monitörün tek durumda toplanması (grup "biri bile", "hepsi" ya da "%N'den fazlası çalışmıyorsa" kurallarıyla) |

**Push** monitörü ters yönde çalışır: Bekci bir yeri kontrol etmez, sizin cron işinizin ya da betiğinizin belirli aralıklarla kendisine haber vermesini bekler. Monitörü kaydettikten sonra size özel bir adres verilir; işiniz her çalıştığında bu adresi çağırır, çağrı gelmezse monitör **çalışmıyor** olur:

```bash
curl -fsS "https://⟦bekci.ornek.com⟧/api/push/⟦TOKEN⟧?status=up&msg=tamam&ping=120"
```

Hata bildirmek için `status=down` gönderin. İşiniz bazen gecikiyorsa (ör. 30 dakikalık yedek bazen 40 dakika sürüyorsa) formdaki **Tolerans (sn)** alanıyla beklenen aralığın üstüne ek süre tanıyın; monitör ancak aralık + tolerans dolunca çalışmıyor sayılır. Adres sızdıysa monitör formundaki **Adresi yenile** düğmesi yeni bir adres üretir; eski adres hemen geçersiz olur.

## 3. Bildirim kanalı ekleyin {#bildirim}

Bir monitör çalışmadığında haberiniz olsun diye en az bir kanal ekleyin. WhatsApp, Telegram, e-posta, Slack, Discord, Microsoft Teams, ntfy, PagerDuty, webhook ve diğerleri dahil 24 kanal vardır.

1. **Bildirimler** sayfasında **Yeni kanal** düğmesine basın.
2. **Tip** listesinden kanalı seçin ve istenen bilgileri girin (ör. Telegram için **Bot token’ı** ve **Sohbet kimliği (chat ID)**).
3. **Test gönder** ile deneyin; test mesajı birkaç saniye içinde gelmelidir.
4. Yeni monitörlerde bu kanal otomatik seçili gelsin diye **Yeni monitörlere varsayılan olarak ekle** kutusunu işaretleyin. Mevcut monitörlerinize de eklemek için **Mevcut tüm monitörlere ekle** kutusunu işaretleyin.
5. **Kaydet** düğmesine basın.

Bildirimler kısa bir başlıkla gelir: 🔴 kesinti, 🟢 düzelme (hâlâ çalışmayan konumlar varsa listelenir), 🟡 konum kesintisi, ⚠️ SSL uyarısı; e-posta hem HTML hem düz metin içerir. Kayıtlı bir kanala **Örnek bildirimleri gönder** ile her türden birer örnek gönderebilirsiniz. Geçici bir hata (ağ, HTTP 5xx/429) olursa gönderim 5 ve 20 saniye sonra yeniden denenir; sonuç olayın işlem geçmişine yazılır.

**Webhook** kanalı JSON gönderir: `event` (`down`, `up`, `reminder`, `location_down`, `location_up`, `cert`, `server_alert`, `server_resolved`, `test`), `title`, `text`, `message`, `time`, `downtime_seconds` (düzelmede), `cert_days` (SSL uyarısında), `monitor` (`id`, `name`, `type`, `target`, `url`), `incident` (`id`, `url`; olaya bağlı bildirimlerde), `locations` (çok konumlu monitörde çalışmayan konumlar: `name`, `message`) ve `server` (yalnızca sunucu uyarılarında: `id`, `name`, `metric`, `value`, `threshold`, `minutes`). Örnek gövde kanal penceresinde görünür.

### Bildirim kuralları {#bildirim-kurallari}

Kanal penceresinin **Kurallar** bölümü kanalın ne zaman ve neyi alacağını belirler. Varsayılan: tüm olaylar, her saat, gecikmesiz (mevcut kanallar değişmez).

| Kural | Ne yapar |
|---|---|
| **Alınacak olaylar** | İşareti kaldırılan türler (ör. düzelme, hatırlatma, SSL) bu kanala gitmez. 🔴 ile 🟢 ayrı seçilir: "yalnızca kesintiyi istiyorum" diyen bir kanal kurabilirsiniz. |
| **Sessiz saatler** | Başlangıç-bitiş (kanalın saat diliminde; `22:00`–`07:00` gibi gece yarısını aşan pencere olabilir). **Yalnızca kritik olanlar geçsin** kipinde 🔴 kesinti, sunucu uyarısı ve kontrol noktası bildirimleri ile bunların 🟢 düzelmesi hemen gider; 🟡 yavaş yanıt, konum kesintisi ve ⚠️ SSL uyarısı pencerenin bitimine ertelenir. **Hiçbir bildirim gitmesin** kipinde her şey ertelenir. Ertelenen sorun bildirimi pencere bitince yalnızca sorun hâlâ sürüyorsa gönderilir ("🌙 Sessiz saatler bitti" notuyla); hatırlatmalar atılır. |
| **Gecikme** | "Yalnızca N dakika sürerse bildir": kısa kesintilerde (ör. iki dakikalık yeniden başlatma) bildirim gelmez. 🔴 süre dolunca sorun hâlâ sürüyorsa "⏳ Gecikmeli bildirim" notuyla gider; gitmediyse o olayın 🟢 düzelmesi ve hatırlatmaları da gitmez. |
| **Eskalasyon** | "N dakikadır süren her olayı buraya da bildir": kanal monitöre ya da sunucuya bağlı olmasa da bu kadar dakikadır açık kalan her kesinti, sunucu uyarısı ve kontrol noktası kesintisi "⏫ Eskalasyon" notuyla gelir; olay kapanınca 🟢 de gelir. Nöbetçi yöneticinin kanalı için uygundur. |
| **Bildirim dili** | Kanal başına dil (ayarlardaki genel dili geçersiz kılar); test ve örnek bildirimleri de bu dilde gider. |

Atlanan ve ertelenen bildirimler nedeniyle birlikte olayın **işlem geçmişine** yazılır ("kanal bu olay türünü almıyor", "sessiz saatler", "gecikme kuralı"…). Ertelenmiş bildirimler veritabanında bekler; uygulama yeniden başlasa da kaybolmaz. **Test gönder** ve **Örnek bildirimleri gönder** kurallardan etkilenmez.

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

**Pencereler:** **Uptime pencereleri** ile her monitör satırında hangi yüzdelerin yazılacağını seçin (24 saat, 7, 30, 90 gün; birden fazlası seçilirse yan yana gösterilir, hiçbiri seçili değilse çubuk görünümüne göre tek pencere). **Olay penceresi** (7/14/30/90 gün) "Son olaylar" bölümünün ve RSS akışının kaç günlük geçmişi göstereceğini belirler.

**Elle açılan olaylar ve güncellemeler:** Otomatik tespit edilmeyen bir sorunu (ör. ödeme sağlayıcısında yavaşlama) **Olaylar → Olay aç** ile duyurabilirsiniz: durum sayfası, başlık, önem (küçük/büyük/kritik), etkilenen monitörler ve ilk açıklama. Olay sayfada başlığı, önemi ve aşamasıyla görünür; **Güncelleme yaz** ile aşama (İnceleniyor → Neden bulundu → İzleniyor → Çözüldü) ve metin eklersiniz, her güncelleme sayfada ve RSS akışında ayrı kayıt olur; **Çözüldü** aşaması olayı kapatır. Otomatik açılan olaylara da güncelleme yazabilirsiniz ("nedeni bulduk, düzeltiyoruz"); onların kapanışını monitör belirler. İzleyici ve müşteri hesapları olayları yalnızca okur.

**Planlı bakım bloğu:** Sayfadaki monitörleri etkileyen (ya da tüm monitörleri kapsayan) etkin bakım pencereleri — süren ve 7 gün içinde başlayacak olanlar — sayfanın **Planlı bakım** bölümünde ve RSS akışında görünür. Bölümün yeri ve görünürlüğü **Dizilim → Bölümler** listesinden ayarlanır; pencere yoksa bölüm çizilmez.

**Duyurular:** Sayfa düzenleyicisinin altındaki **Duyurular** kartından planlı bakım ya da bilgi notu ekleyin. Duyuru eklerken **Diğer sayfalara da ekle** ile aynı duyurunun kopyasını seçtiğiniz sayfalara ya da tüm sayfalara tek seferde bırakabilirsiniz; kopyalar sonradan her sayfada ayrı düzenlenir.

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
- **Prometheus/Grafana'ya bağlayın:** `GET /metrics` ucu monitör durumu, yanıt süresi ve uptime ile sunucu ajanlarının CPU, RAM, disk ve ağ ölçümlerini verir ([metrik listesi](/docs/entegrasyonlar/#prometheus)). **Ayarlar → API anahtarları** bölümünden bir anahtar alın ve `Authorization: Bearer upk_…` başlığıyla ya da Basic kimlikle (kullanıcı `metrics`, şifre anahtar) çağırın; anahtarın izleyici yetkisi yeter.
