---
id: coolify
title: Coolify ile kurulum
nav: Coolify
description: Bekci'yi Coolify v4'te Docker imajı olarak ekleyin; alan adı, HTTPS sertifikası ve güncellemeleri Coolify yönetsin.
section: Kurulum
order: 5
slug: kurulum/coolify
---

Sunucunuzda [Coolify](https://coolify.io) kuruluysa Bekci'yi bir **Docker Image** kaynağı olarak eklemek en kolay yoldur. Alan adını, HTTPS sertifikasını ve yönlendirmeyi Coolify'ın kendi vekili (Traefik) yapar. Toplam süre: yaklaşık 5 dakika.

> [!NOTE]
> Bu rehber Coolify v4'ü anlatır. Coolify'ın arayüzü sürümden sürüme biraz değişir; bir düğmenin ya da alanın adı sizde farklıysa aynı işi yapan seçeneği arayın. Aşağıda iki farklı yazılışın olduğu yerleri belirttik.

## Başlamadan önce {#once}

- Coolify'da bir proje ve bir sunucu (destination) hazır olmalı.
- Bekci için kullanacağınız alan adının (ör. `bekci.ornek.com`) **A** kaydı Coolify sunucusunun IP adresini göstermeli ([DNS kaydı nasıl eklenir?](/docs/kurulum/caddy/#dns)).

## 1. Yeni kaynak oluşturun {#kaynak}

1. Coolify'da projenizi ve ortamı (ör. **production**) açın.
2. **New resource** düğmesine basın (bazı sürümlerde **+ New** ya da **+ Add Resource**).
3. Listeden **Docker Image** seçeneğini seçin.
4. İmajı girin: **Image name** alanına `kadirsungurlu/bekci`, **Tag** alanına `latest` yazın. (Tek alan varsa: `kadirsungurlu/bekci:latest`.) Belirli bir sürüme sabitlemek için `latest` yerine `1.0.0` yazın ([sürüm etiketleri](/docs/guncelleme-yedek/#surum-etiketleri)).
5. Sorulursa sunucuyu seçin ve **Create application** (ya da **Save**) ile kaydedin.

> [!CHECK]
> Uygulamanın yapılandırma sayfası açılır. Sol tarafta **General**, **Environment Variables**, **Persistent Storage**, **Healthcheck** gibi bölümler görürsünüz.

## 2. Portu ayarlayın {#port}

**General** bölümünde **Ports Exposes** alanına `8080` yazın (başka bir değer varsa silin). Bekci konteynerin içinde 8080 portunu dinler; Coolify'ın vekili trafiği bu porta yönlendirir. **Port mappings** alanını boş bırakın.

## 3. Alan adını ekleyin {#alan-adi}

Alan adınızı `https://` ile birlikte girin: `https://⟦bekci.ornek.com⟧`

- Yeni Coolify sürümlerinde bu iş ayrı bir **Domains** sayfasında yapılır (**Add domain** düğmesi).
- Eski sürümlerde **General** bölümündeki **Domains** alanına yazılır.

`https://` ile yazdığınızda Coolify, Let's Encrypt sertifikasını kendisi alır.

## 4. Kalıcı depolamayı ekleyin {#depolama}

> [!CAUTION] Bu adımı atlamayın
> Kalıcı depolama eklemezseniz tüm veriler (monitörler, geçmiş, kullanıcılar) konteynerin içinde kalır ve **her yeniden dağıtımda silinir**.

1. **Persistent Storage** bölümünü açın.
2. Yeni bir **volume** ekleyin (**Add volume mount**; eski sürümlerde **+ Add** → **Volume**).
3. **Name**: `bekci-data` (istediğiniz bir ad), **Destination Path**: `/data`.
4. Kaydedin.

Birim yerine sunucudaki bir klasörü bağlamak isterseniz (**directory mount**), o klasörün sahibi uid 1000 olmalıdır: `chown 1000:1000 ⟦/klasör/yolu⟧`. Bekci root olmayan bir kullanıcıyla çalışır ve başka bir sahibin klasörüne yazamaz.

## 5. Ortam değişkenlerini ekleyin {#ortam}

**Environment Variables** bölümünde şu değişkenleri ekleyin:

```ini title="Environment Variables"
BASE_URL=https://⟦bekci.ornek.com⟧
TZ=Europe/Istanbul
```

`BASE_URL`, 3. adımdaki alan adıyla aynı olmalı: bildirimlerdeki bağlantılar ve sunucu ajanı kurulum komutları bu adresi kullanır. Diğer seçenekler: [Ortam değişkenleri](/docs/ortam-degiskenleri/).

## 6. Sağlık kontrolüne dokunmayın {#saglik}

**Healthcheck** bölümündeki Coolify sağlık kontrolünü **kapalı** (varsayılan) bırakın. Bekci imajının kendi sağlık kontrolü vardır ve güncellemelerde doğru çalışacak şekilde tasarlanmıştır.

> [!WARNING] Neden kapalı kalmalı?
> Güncellemede Coolify yeni konteyneri eskisi kapanmadan başlatır. Yeni Bekci konteyneri, aynı verileri iki kopya aynı anda kullanmasın diye eskisinin kapanmasını bekler; imajın kendi sağlık kontrolü bu bekleme sırasında “sağlıklı” der. Coolify'ın sağlık kontrolünü açarsanız imajınkinin yerine geçer, bekleyen yeni konteyner “sağlıksız” görünür ve Coolify güncellemeyi geri alır.

## 7. Dağıtın {#dagitin}

Sağ üstteki **Deploy** düğmesine basın. Coolify imajı indirir ve konteyneri başlatır.

> [!CHECK]
> Dağıtım günlüğü başarıyla biter ve uygulamanın durumu **Running** olur. Uygulamanın günlüklerinde (**Logs**) şu satırı görürsünüz:
>
> ```text
> level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data saat_dilimi=Europe/Istanbul
> ```
>
> `https://⟦bekci.ornek.com⟧` adresini açın; **Hoş geldiniz** ekranında yönetici hesabınızı oluşturun. Sertifikanın alınması ilk seferde bir dakika kadar sürebilir.

## PostgreSQL ile {#postgresql}

Varsayılan SQLite çoğu kurulum için yeterlidir ([hangi veritabanı?](/docs/#veritabani)). PostgreSQL istiyorsanız iki yol var:

:::tabs key=coolify-db label="PostgreSQL seçeneği"
@tab Gömülü PostgreSQL
En kolayı: 1. adımda etiketi `latest` yerine `postgres` yazın (sabit sürüm için `1.0.0-postgres`). PostgreSQL 18 aynı konteynerin içinde çalışır; veritabanı yine `/data` biriminde durur ve gece yedekleri `/data/backups` altına alınır. Diğer adımlar aynıdır. Yaklaşık 200 MB ek bellek kullanır.
@tab Coolify'daki PostgreSQL
1. Aynı projede **New resource** → **PostgreSQL** ile bir veritabanı oluşturun ve başlatın.
2. Veritabanının sayfasında iç bağlantı adresini kopyalayın (**Postgres URL (internal)** gibi bir adla gösterilir). Adres `postgres://kullanıcı:parola@…:5432/…` biçimindedir.
3. Bekci uygulamasının **Environment Variables** bölümüne ekleyin:

   ```ini title="Environment Variables"
   DATABASE_URL=⟦kopyaladığınız-iç-adres⟧
   ```

   Değer `postgres://postgres:…@…:5432/postgres` gibi görünür.

4. Etiketi `latest` (SQLite imajı) olarak bırakın; `DATABASE_URL` verildiğinde bu imaj PostgreSQL'e bağlanır. **Deploy** ile yeniden dağıtın.

Bekci ile veritabanı aynı Coolify sunucusunda olmalıdır (iç adres yalnızca orada çalışır). Bu durumda Bekci gece yedeğini **almaz**; yedeği veritabanının sayfasındaki **Backups** bölümünden zamanlayın.
:::

> [!CHECK]
> Günlükte `msg=veritabanı tür="PostgreSQL …"` satırını görmelisiniz (parola `xxxxx` olarak gizlenir).

## Güncelleme {#guncelleme}

- **`latest` etiketiyle:** Uygulamanın sayfasında **Redeploy** (ya da **Deploy**) düğmesine basın; Coolify imajın yenisini indirip konteyneri değiştirir.
- **Sabit sürümle (ör. `1.0.0`):** Etiketi yeni sürümle değiştirip dağıtın.

Güncelleme sırasında yeni konteynerin günlüğünde bir süre şu satırı görmeniz **normaldir**; eski konteyner kapanınca yenisi bir iki saniye içinde devralır. Bu sayede iki kopya aynı anda kontrol yapıp çift bildirim göndermez:

```text
level=WARN msg="veri klasörü başka bir örnekte açık; o kapanana kadar bekleniyor" kilit=/data/uptime.db.lock en_fazla=10m0s
```

Güncellemeden önce yedek almayı unutmayın: [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/).

## Sık karşılaşılan sorunlar {#sorunlar}

### Sayfa “no available server” ya da 404 veriyor {#no-available-server}

Coolify'ın vekili Bekci'ye ulaşamıyor. **Ports Exposes** değerinin `8080` olduğunu ve alan adının `https://` ile yazıldığını kontrol edin, sonra yeniden dağıtın.

### Her dağıtımdan sonra kurulum ekranı geliyor {#veri-kayboluyor}

Kalıcı depolama eksik ya da hedef yolu yanlış. **Persistent Storage** bölümünde hedefin tam olarak `/data` olduğundan emin olun ([4. adım](#depolama)).

### Güncelleme “unhealthy” hatasıyla geri alınıyor {#unhealthy}

Coolify'ın sağlık kontrolü açıktır; [6. adımdaki](#saglik) gibi kapatıp yeniden dağıtın.

### Konteyner “permission denied” ile kapanıyor {#izin}

Günlükte şuna benzer bir satır görürsünüz:

```text
hata: veritabanı açılamadı: kilit dosyası açılamadı: open /data/uptime.db.lock: permission denied
```

Sunucudaki bir klasörü bağladıysanız sahibi uid 1000 değildir: `chown -R 1000:1000 ⟦/klasör/yolu⟧` ile düzeltip yeniden dağıtın.
