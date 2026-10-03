---
id: updates
title: Güncelleme, yedek ve geri dönüş
nav: Güncelleme, yedek, geri dönüş
description: Bekci'yi güvenle güncelleyin, sürümünü sabitleyin, otomatik yedekleri bulun, yedekleri sunucu dışına kopyalayın, geri yükleyin ve gerekirse eski sürüme dönün.
section: Bakım ve başvuru
order: 9
slug: guncelleme-yedek
---

## Sürüm etiketleri {#surum-etiketleri}

Bekci imajları Docker Hub'da [`kadirsungurlu/bekci`](https://hub.docker.com/r/kadirsungurlu/bekci/tags) adıyla (aynısı `ghcr.io/kadirsungurlu/bekci` adıyla GitHub Container Registry'de) amd64 ve arm64 için yayınlanır. Docker Hub'a yalnızca yayınlanmış sürümler gider; geliştirme derlemeleri (commit kısa kodu) yalnızca GHCR'dedir:

| Etiket | İçerik | Ne zaman |
|---|---|---|
| `latest` | En son yayınlanmış kararlı sürüm, SQLite | Her zaman son kararlı sürümü istiyorsanız |
| `postgres` | En son yayınlanmış kararlı sürüm, gömülü PostgreSQL 18 | Gömülü PostgreSQL ile son kararlı sürüm |
| `1.0.0`, `1.0` | Belirli bir sürüm, SQLite | Sürümü sabitlemek ve geri dönmek için (önerilen) |
| `1.0.0-postgres`, `1.0-postgres` | Belirli bir sürüm, gömülü PostgreSQL | Aynısı, PostgreSQL için |
| `ghcr.io/kadirsungurlu/bekci:2718268`, `…:postgres-2718268` | Belirli bir geliştirme derlemesi (commit kısa kodu); yalnızca GHCR'de, ana dalın her derlemesinde | Günlükte gördüğünüz bir geliştirme derlemesine dönmek için |

`1.0` gibi iki parçalı etiketler o serinin en son yamasını izler (ör. `1.0.1` çıkınca `1.0` onu gösterir).

> [!IMPORTANT]
> SQLite (`latest`, `1.0.0`) ile PostgreSQL (`postgres`, `1.0.0-postgres`) etiketleri arasında geçiş **yapmayın**: veriler birbirine taşınmaz.

## Güncellemeden önce {#once}

1. **Yedek alın.** Aşağıdaki [tam yedek](#tam-yedek) en güvenlisidir. SQLite kullanıyorsanız Bekci veritabanı yapısını değiştiren güncellemelerden önce kendisi de yedek alır ([otomatik yedekler](#otomatik-yedekler)).
2. **Sürüm notlarına bakın:** [GitHub'daki sürümler](https://github.com/kadirsungurlu/bekci/releases).

## Güncelleme {#guncelleme}

:::tabs key=kurulum label="Kurulum yolu"
@tab Docker Compose
```bash
cd /opt/bekci
docker compose pull
docker compose up -d
```

[Caddy'li kurulumda](/docs/kurulum/caddy/) da komutlar aynıdır.
@tab docker run
```bash
docker pull kadirsungurlu/bekci:latest
docker stop bekci && docker rm bekci
```

Ardından konteyneri ilk kurulumdaki `docker run` komutuyla (aynı `-v bekci-data:/data` ve `-e` değerleriyle) yeniden başlatın ([Docker rehberi](/docs/kurulum/docker/#base-url)).
@tab Coolify
Uygulamanın sayfasında **Redeploy** (ya da **Deploy**) düğmesine basın. Sabit bir sürüm kullanıyorsanız önce etiketi yeni sürümle değiştirin ([Coolify rehberi](/docs/kurulum/coolify/#guncelleme)).
:::

> [!CHECK]
> Günlükte yeni bir başlama satırı görmelisiniz (`docker compose logs bekci | grep "Bekci başladı"`). `sürüm=` alanı çalışan sürümdür (ör. `1.2.1`; GHCR commit imajlarında kısa kod, ör. `2718268`); güncellemeden sonra değişmiş olmalı:
>
> ```text
> level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data
> ```
>
> Veritabanı yapısı değiştiyse hemen üstünde `msg="migration öncesi yedek alındı"` ve `msg="migration uygulandı"` satırları da olur.

## Sürümü sabitleme {#surum-sabitleme}

`latest` etiketi her `pull` komutunda en yeni sürümü getirir. Güncellemenin ne zaman yapılacağına siz karar vermek istiyorsanız belirli bir sürüm etiketi kullanın:

- **Docker Compose:** `docker-compose.yml` dosyasında `image: kadirsungurlu/bekci:latest` satırını `image: kadirsungurlu/bekci:⟦1.0.0⟧` yapın.
- **Caddy'li kurulum:** `/opt/bekci/.env` dosyasında `UPTIME_TAG=⟦1.0.0⟧` yazın (gömülü PostgreSQL için `1.0.0-postgres`).
- **Coolify:** **Tag** alanına `1.0.0` yazın.

Yeni sürüme geçmek için etiketi değiştirip `docker compose up -d` çalıştırın (Coolify'da yeniden dağıtın).

## Otomatik yedekler {#otomatik-yedekler}

Bekci iki tür yedeği kendisi alır. İkisi de veri biriminin içinde, `/data/backups/` klasöründedir.

| Yedek | Dosya adı | Ne zaman |
|---|---|---|
| Gece yedeği (SQLite) | `uptime-2026-09-28.db` | Günde bir kez, saat 03:00'ten (`TZ` saat dilimine göre) sonra; varsayılan olarak son 7 tanesi tutulur. Bekci 03:00'ten sonra başlatıldıysa o günün yedeği birkaç dakika içinde alınır |
| Gece yedeği (gömülü PostgreSQL) | `uptime-2026-09-28.dump` | Aynı şekilde, `pg_dump` biçiminde |
| Güncelleme öncesi yedek (SQLite) | `pre-migrate-v16-to-v17-20260928-030000.db` | Yeni sürüm veritabanı yapısını değiştirecekse, değiştirmeden hemen önce |

- Kaç gece yedeği tutulacağını **Ayarlar → Genel → Gece yedeği sayısı** ile değiştirebilirsiniz (0 gece yedeğini kapatır).
- Güncelleme öncesi yedek alınamazsa (ör. disk dolu) güncelleme uygulanmaz ve Bekci açılmaz; yer açınca açılır. Bu dosyalar kendiliğinden silinmez; eski olanları elle silebilirsiniz.
- **Harici PostgreSQL** (`DATABASE_URL`) kullanıyorsanız Bekci yedek **almaz**; yedeği veritabanı tarafında alın ([aşağıda](#harici-postgresql)).

Yedekleri listelemek için:

```bash
docker exec bekci ls -la /data/backups
```

Konteynerin adı Compose kurulumunda `bekci-bekci-1`'dir; ya da `/opt/bekci` içindeyken `docker compose exec bekci ls -la /data/backups`.

> [!WARNING] Yedekler sunucu dışına da kopyalanmalı
> Otomatik yedekler aynı diskte durur: sunucu ya da disk giderse yedekler de gider. Düzenli olarak başka bir yere kopyalayın.

## Yedekleri sunucu dışına kopyalama {#disari-kopyalama}

Yedek klasörünü konteynerden sunucuya kopyalayın:

```bash
cd /opt/bekci
docker compose cp bekci:/data/backups ./bekci-yedekler
```

(`docker run` kurulumunda: `docker cp bekci:/data/backups ./bekci-yedekler`.)

Sonra **kendi bilgisayarınızdan** sunucudan indirin:

```bash
scp -r ⟦root@SUNUCU-IP⟧:/opt/bekci/bekci-yedekler ./
```

Bu adımları bir cron işiyle ya da yedekleme aracınızla (restic, rsync, sağlayıcınızın yedekleme hizmeti…) otomatikleştirmenizi öneririz.

### Tam yedek (tüm veri birimi) {#tam-yedek}

Veritabanıyla birlikte tüm veri birimini tek bir dosyaya almak için Bekci'yi kısa süreliğine durdurun (veritabanı açıkken kopyalanan dosya tutarsız olabilir):

```bash
cd /opt/bekci
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data -v "$PWD":/yedek alpine \
  tar czf /yedek/bekci-data-$(date +%F).tgz -C /data .
docker compose start bekci
```

Birim adı Compose kurulumunda `bekci_bekci-data`, `docker run` kurulumunda `bekci-data`'dır (`docker volume ls` ile görebilirsiniz). Komut `/opt/bekci/bekci-data-2026-09-28.tgz` gibi bir dosya oluşturur.

## Geri yükleme {#geri-yukleme}

:::tabs key=geri-yukleme label="Yedek türü"
@tab SQLite yedeğinden
Bir gece yedeğini ya da güncelleme öncesi yedeği geri yüklemek için:

```bash
cd /opt/bekci
docker compose exec bekci ls /data/backups          # dosya adını seçin
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data alpine sh -c \
  'cd /data && cp backups/⟦uptime-2026-09-28.db⟧ uptime.db && rm -f uptime.db-wal uptime.db-shm && chown 1000:1000 uptime.db'
docker compose start bekci
```

`uptime.db-wal` ve `uptime.db-shm` dosyaları eski veritabanına aittir; silinmeleri gerekir. `docker run` kurulumunda birim adı `bekci-data`, durdurma ve başlatma komutları `docker stop bekci` ve `docker start bekci`'dir.
@tab Tam yedekten (.tgz)
```bash
cd /opt/bekci
docker compose stop bekci
docker run --rm -v bekci_bekci-data:/data -v "$PWD":/yedek alpine sh -c \
  'find /data -mindepth 1 -delete && tar xzf /yedek/⟦bekci-data-2026-09-28.tgz⟧ -C /data'
docker compose start bekci
```

Bu, birimin mevcut içeriğini tamamen siler ve yedektekiyle değiştirir.
@tab Gömülü PostgreSQL
`postgres` imajında gece yedekleri `uptime-…dump` dosyalarıdır. Konteyner çalışırken:

```bash
docker exec ⟦bekci⟧ pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/⟦uptime-2026-09-28.dump⟧
docker restart ⟦bekci⟧
```
@tab Harici PostgreSQL {#harici-postgresql}
Bekci harici veritabanında yedek almaz; yedeği `pg_dump` ile siz alırsınız. [Docker Compose rehberindeki](/docs/kurulum/docker-compose/) PostgreSQL kurulumu için yedek:

```bash
cd /opt/bekci
docker compose exec -T db pg_dump -U bekci --format=custom bekci > bekci-$(date +%F).dump
```

Geri yükleme:

```bash
cd /opt/bekci
docker compose stop bekci
docker compose exec -T db pg_restore -U bekci --clean --if-exists --no-owner -d bekci < ⟦bekci-2026-09-28.dump⟧
docker compose start bekci
```
:::

> [!CHECK]
> Geri yüklemeden sonra panele girin: monitörleriniz yedeğin alındığı andaki hâliyle görünmeli.

### Ayarların JSON yedeği {#json-yedek}

**Ayarlar → Yedekle / Geri yükle → Yedeği indir**, monitörleri, bildirim kanallarını, etiketleri, durum sayfalarını ve ayarları tek bir JSON dosyasına aktarır. Kontrol geçmişi ve işlem kaydı dahil değildir. Bu dosyayı başka bir Bekci kurulumuna (SQLite ya da PostgreSQL fark etmez) aynı bölümdeki **Geri yükle** ile aktarabilirsiniz.

İki seçenek vardır:

- **Kullanıcıları dahil et:** hesaplar rolleri, e-postaları, müşteri kısıtları (monitörler ve etiket kuralları) ve **şifre özetleriyle** (bcrypt; şifrenin kendisi değil) dosyaya girer. Geri yüklemede kullanıcılar her zaman birleştirilir: var olan kullanıcı adı atlanır, yenisi eklenir ve eski şifresiyle giriş yapar. **2FA sırlarını da dahil et** seçilirse TOTP sırrı ve kurtarma kodu özetleri de taşınır; seçilmezse aktarılan kullanıcılar iki adımlı doğrulaması kapalı gelir. Değiştir modu kullanıcıları silmez. Oturumlar ve API anahtarları yedeğe girmez.
- **Dosyayı şifrele:** dosya parola ile şifrelenir (Argon2id ile türetilen anahtar, AES-256-GCM). Şifreli dosya yine JSON'dur ama içerik okunamaz; geri yüklerken **Yedek şifresi** alanına aynı parolayı yazarsınız. Parolayı kaybederseniz yedek kullanılamaz. Şifrelenmemiş yedek bildirim token'larını, SMTP şifrelerini ve (seçtiyseniz) şifre özetlerini açık hâlde içerir; güvenli bir yerde saklayın.

Komut satırından aynı yedek `curl -H "Authorization: Bearer upk_…" -X POST -d '{"users":true,"password":"…"}' https://⟦bekci.ornek.com⟧/api/export` ile alınabilir (`GET /api/export` kullanıcısız düz yedeği verir).

## Eski sürüme dönme {#geri-donus}

Yeni sürümde bir sorun çıkarsa eski sürümün etiketine dönebilirsiniz:

1. Döneceğiniz etiketi bulun: yayınlanan sürümler (ör. `1.0.0`) [Docker Hub'da](https://hub.docker.com/r/kadirsungurlu/bekci/tags) listelenir. Günlükteki eski `sürüm=` değeri yayınlanmış bir sürümse (ör. `1.2.0`) doğrudan etiket olarak kullanılır; bir geliştirme derlemesine (kısa kod, ör. `0ae7b6d`) dönmek için `ghcr.io/kadirsungurlu/bekci:0ae7b6d` (yalnızca GHCR).
2. **Yeni sürüm veritabanını yükselttiyse** (günlükte `migration uygulandı` satırı varsa) eski sürüm yeni veritabanını açamayabilir. Önce güncelleme öncesi yedeği (`pre-migrate-…db`) [geri yükleyin](#geri-yukleme). Bu yedekten sonraki değişiklikler kaybolur.
3. Etiketi eski sürüme çevirip başlatın, ör. `image: kadirsungurlu/bekci:⟦1.0.0⟧` ve `docker compose up -d`.

## PostgreSQL ana sürüm yükseltmesi {#postgresql-yukseltme}

Bu bölüm yalnızca gömülü PostgreSQL'li `postgres` imajı içindir. Bekci'nin ileride PostgreSQL'in yeni bir ana sürümüne (ör. 18'den 19'a) geçmesi durumunda veri klasörü doğrudan açılamaz. Bu durumda konteyner başlamaz ve günlüğüne `HATA: PostgreSQL ana sürümü uyuşmuyor.` başlıklı, adım adım bir açıklama yazar. Özetle:

1. Konteyneri durdurun ve **eski** imaja (eski PostgreSQL sürümünü içeren etikete) dönün.
2. Eski imaj çalışırken yedek alın ve dosyayı sunucu dışına da kopyalayın:

   ```bash
   docker exec ⟦bekci⟧ pg_dump -h /run/postgresql -U postgres --format=custom --file=/data/backups/tasima.dump uptime
   ```

3. Konteyneri durdurun ve `/data/postgres` klasörünü silmek yerine yeniden adlandırın (ör. `/data/postgres-18-eski`).
4. Yeni imajı başlatın; boş bir veritabanı oluşur.
5. Yedeği geri yükleyip konteyneri yeniden başlatın:

   ```bash
   docker exec ⟦bekci⟧ pg_restore -h /run/postgresql -U postgres --clean --if-exists --no-owner -d uptime /data/backups/tasima.dump
   docker restart ⟦bekci⟧
   ```

6. Her şey yolundaysa eski klasörü silin.

## Başka bir sunucuya taşıma {#tasima}

1. Eski sunucuda [tam yedek](#tam-yedek) alın ve dosyayı yeni sunucuya kopyalayın.
2. Yeni sunucuya Bekci'yi aynı yolla kurun, başlatmadan önce ya da durdurup yedeği [geri yükleyin](#geri-yukleme).
3. Alan adının DNS kaydını yeni sunucuya çevirin.

Adres aynı kaldığı için sunucu ajanları yeniden kurulmadan bağlanır: IP kilidi panelin değil, ajanın IP adresine bakar.
