---
id: compose
title: Docker Compose ile kurulum
nav: Docker Compose
description: Bekci'yi /opt/bekci klasöründe bir docker-compose.yml dosyasıyla kurun. SQLite ya da harici PostgreSQL; güncelleme tek komut.
section: Kurulum
order: 3
slug: kurulum/docker-compose
---

Docker Compose ile tüm ayarlar tek bir dosyada (`docker-compose.yml`) durur. Başlatmak, durdurmak ve güncellemek tek komuttur; ayarı değiştirmek için dosyayı düzenleyip yeniden başlatmanız yeterlidir. Toplam süre: yaklaşık 5 dakika.

Bu rehberdeki kurulum paneli `http://SUNUCU-IP:8080` adresinde açar. Alan adınızla HTTPS istiyorsanız doğrudan [HTTPS'li kurulum (Caddy)](/docs/kurulum/caddy/) rehberine geçin; o rehber de Compose kullanır.

## 1. Docker ve Compose'u doğrulayın {#dogrulayin}

```bash
docker compose version
```

> [!CHECK]
> `Docker Compose version v2.…` (ya da daha yeni) gibi bir satır görmelisiniz. `docker: 'compose' is not a docker command` ya da `command not found` görüyorsanız Docker'ı [resmi betikle kurun](/docs/kurulum/docker/#docker-kurun); betik Compose eklentisini de kurar.

## 2. Bir klasör oluşturun {#klasor}

Bekci'nin dosyaları için `/opt/bekci` klasörünü kullanacağız. Başka bir yer de seçebilirsiniz; önemli olan, sonraki tüm `docker compose` komutlarını **bu klasörün içindeyken** çalıştırmanızdır.

```bash
mkdir -p /opt/bekci
cd /opt/bekci
```

## 3. docker-compose.yml dosyasını oluşturun {#dosya}

Veritabanı seçiminizi yapın. Kararsızsanız **SQLite** seçin: ek servis gerektirmez ve en hafif seçenektir ([hangi veritabanı?](/docs/#veritabani)).

Dosyayı `nano` metin düzenleyicisiyle oluşturun:

```bash
nano docker-compose.yml
```

Aşağıdaki içeriği kopyalayıp nano penceresine yapıştırın (çoğu terminalde sağ tık ya da <kbd>Ctrl</kbd>+<kbd>Shift</kbd>+<kbd>V</kbd>):

:::tabs key=db label="Veritabanı"
@tab SQLite (önerilen)
```yaml title="/opt/bekci/docker-compose.yml" download="/indir/docker-compose.yml"
# Bekci — Docker Compose ile kurulum (SQLite)
# Rehber: https://bekci.app/docs/kurulum/docker-compose/
name: bekci
services:
  bekci:
    # Sürümü sabitlemek için "latest" yerine ör. 1.0.0 yazın.
    # Gömülü PostgreSQL için: kadirsungurlu/bekci:postgres
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "8080:8080"
    environment:
      BASE_URL: http://⟦203.0.113.10⟧:8080   # panelin dışarıdan açıldığı adres
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
volumes:
  bekci-data:
```

Değiştirmeniz gereken tek yer vurgulu olan: `203.0.113.10` yerine sunucunuzun IP adresini yazın.
@tab Harici PostgreSQL
Bu dosya Bekci'nin yanında bir PostgreSQL 18 konteyneri de başlatır. Kendi PostgreSQL sunucunuzu kullanacaksanız `db` servisini silin ve `DATABASE_URL`'i o sunucunun adresiyle yazın.

```yaml title="/opt/bekci/docker-compose.yml" download="/indir/docker-compose.postgres.yml"
# Bekci — Docker Compose ile kurulum (harici PostgreSQL)
# Rehber: https://bekci.app/docs/kurulum/docker-compose/
# Parolayı İKİ yerde de aynı şekilde değiştirin (DATABASE_URL ve POSTGRES_PASSWORD).
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "8080:8080"
    environment:
      BASE_URL: http://⟦203.0.113.10⟧:8080   # panelin dışarıdan açıldığı adres
      TZ: Europe/Istanbul
      DATABASE_URL: postgres://bekci:⟦GUCLU-BIR-PAROLA⟧@db:5432/bekci?sslmode=disable
    volumes:
      - bekci-data:/data   # yedekler ve geçici dosyalar
    depends_on:
      db:
        condition: service_healthy
  db:
    image: postgres:18-alpine
    restart: unless-stopped
    environment:
      POSTGRES_USER: bekci
      POSTGRES_PASSWORD: ⟦GUCLU-BIR-PAROLA⟧
      POSTGRES_DB: bekci
    volumes:
      - db-data:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U bekci -d bekci"]
      interval: 5s
      retries: 10
volumes:
  bekci-data:
  db-data:
```

Vurgulu yerleri değiştirin: sunucunuzun IP adresi ve **iki yerde aynı** güçlü bir parola. Parolada yalnızca harf ve rakam kullanın (ör. 24 karakterlik rastgele bir dizi); `@`, `:`, `/`, `?`, `#` gibi karakterler bağlantı adresini bozar.
:::

Kaydedip çıkın: <kbd>Ctrl</kbd>+<kbd>O</kbd>, ardından <kbd>Enter</kbd> (kaydet), sonra <kbd>Ctrl</kbd>+<kbd>X</kbd> (çık).

> [!TIP] Dosyayı indirerek oluşturmak
> Yapıştırmakla uğraşmak istemiyorsanız dosyayı doğrudan indirip sonra yalnızca vurgulu yerleri düzenleyebilirsiniz:
>
> ```bash
> curl -fsSL -o docker-compose.yml https://bekci.app/indir/docker-compose.yml
> nano docker-compose.yml
> ```
>
> Harici PostgreSQL için adres `https://bekci.app/indir/docker-compose.postgres.yml` olur (indirilen dosyanın adı yine `docker-compose.yml` olmalı).

> [!CHECK]
> `docker compose config --quiet` komutu hiçbir şey yazmadan bitmeli. Hata veriyorsa büyük olasılıkla girintiler bozulmuştur: YAML dosyalarında girinti boşlukla yapılır ve satır başlarındaki boşluk sayısı önemlidir. Dosyayı silip (`rm docker-compose.yml`) yeniden oluşturun.

## 4. Başlatın {#baslatin}

```bash
docker compose up -d
```

İlk seferde imaj Docker Hub'dan indirilir (birkaç saniye ile bir dakika arası).

> [!CHECK]
> Komut `Container bekci-bekci-1  Started` satırıyla bitmeli. Birkaç saniye sonra `docker compose ps` çıktısında **STATUS** sütunu `Up … (healthy)` olmalı. PostgreSQL seçtiyseniz iki satır görürsünüz: `bekci` ve `db`.

## 5. Paneli açın {#paneli-acin}

Tarayıcıda `http://⟦SUNUCU-IP⟧:8080` adresini açın, **Hoş geldiniz** ekranında yönetici hesabınızı oluşturun ([ayrıntılar](/docs/kurulum/docker/#yonetici)). Açılmıyorsa sağlayıcınızın güvenlik duvarında 8080/TCP portuna izin verin ([güvenlik duvarı notu](/docs/kurulum/docker/#guvenlik-duvari)).

> [!WARNING]
> Bu kurulum paneli şifrelenmemiş `http://` ile açar. Kalıcı kullanım için HTTPS ekleyin: aynı sunucuda web sunucusu yoksa [Caddy'li kurulum](/docs/kurulum/caddy/), varsa [ters vekil](/docs/kurulum/ters-vekil/).

## Günlükler {#gunlukler}

```bash
cd /opt/bekci
docker compose logs -f bekci      # canlı izle, çıkmak için Ctrl+C
docker compose logs --tail 50 bekci
```

## Güncelleme {#guncelleme}

```bash
cd /opt/bekci
docker compose pull
docker compose up -d
```

`pull` yeni imajı indirir, `up -d` konteyneri yeni imajla yeniden oluşturur; verileriniz birimde kalır. Güncellemeden önce yedek alın ve sürüm sabitlemeyi öğrenin: [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/).

> [!CHECK]
> `docker compose logs bekci | grep "Bekci başladı"` komutunun son satırındaki `sürüm=` değeri (çalışan sürüm, ör. `1.3.0`; GHCR commit imajlarında kısa kod) güncellemeden önceki değerden farklı olmalı.

## Ayarı değiştirmek {#ayar-degistirmek}

`docker-compose.yml` dosyasında bir şeyi (ör. `BASE_URL`) değiştirdikten sonra:

```bash
docker compose up -d
```

Compose değişikliği fark eder ve konteyneri yeni ayarla yeniden oluşturur.

## Durdurma ve kaldırma {#durdurma}

```bash
docker compose stop     # durdur (konteyner kalır)
docker compose start    # yeniden başlat
docker compose down     # konteyneri kaldır; veriler birimde kalır
```

> [!CAUTION]
> `docker compose down -v` birimleri de siler, yani **tüm verileriniz kalıcı olarak gider**. Bekci'yi tamamen kaldırmak istemiyorsanız `-v` kullanmayın.

Verileriniz `bekci_bekci-data` adlı birimde durur (Compose, birim adının başına proje adını ekler). Birimleri `docker volume ls` ile görebilirsiniz.
