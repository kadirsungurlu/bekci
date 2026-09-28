---
id: caddy
title: HTTPS'li kurulum (Caddy)
nav: HTTPS'li kurulum (Caddy)
description: Boş bir sunucuya, kendi alan adınızla ve otomatik Let's Encrypt sertifikasıyla Bekci kurun. Hangi dosyanın nereye gideceği adım adım.
section: Kurulum
order: 4
slug: kurulum/caddy
---

Bu rehberin sonunda Bekci, `https://bekci.ornek.com` gibi kendi alan adınızda, geçerli bir HTTPS sertifikasıyla çalışıyor olacak. Sertifikayı [Caddy](https://caddyserver.com) web sunucusu Let's Encrypt'ten kendisi alır ve süresi dolmadan yeniler; sizin bir şey yapmanız gerekmez. Toplam süre: yaklaşık 10 dakika (DNS kaydının yayılmasını beklemek hariç).

> [!CAUTION] Bu rehber boş bir sunucu içindir
> Caddy, sunucunun **80 ve 443** portlarını kullanır. Sunucunuzda Coolify, Nginx, Apache ya da başka bir web sunucusu zaten çalışıyorsa bu rehberi **izlemeyin**: [Coolify](/docs/kurulum/coolify/) ya da [ters vekil arkasında](/docs/kurulum/ters-vekil/) rehberine geçin. Emin değilseniz [3. adımdaki kontrolü](#portlar) yapın.

## Neye ihtiyacınız var? {#gerekenler}

- Genel (public) bir IPv4 adresi olan bir Linux sunucu ([gereksinimler](/docs/#gereksinimler)).
- Bir alan adı ya da alt alan adı, ör. `bekci.ornek.com`, ve bu alan adının DNS kayıtlarını düzenleyebileceğiniz panel (alan adını aldığınız firma ya da Cloudflare).
- Sertifika bildirimleri için bir e-posta adresi.

## Kurulumun sonunda dosyalar {#dosya-duzeni}

Sunucunuzda bir klasör ve içinde üç dosya olacak. Yalnızca birini (`.env`) düzenleyeceksiniz:

```text title="Klasör düzeni"
/opt/bekci/
├── docker-compose.yml   ← Bekci + Caddy tanımı      (olduğu gibi kullanın)
├── Caddyfile            ← Caddy'nin ayarı            (olduğu gibi kullanın)
└── .env                 ← alan adınız ve e-postanız  (düzenleyeceğiniz tek dosya)
```

`.env` dosyasının adı noktayla başlar; bu yüzden `ls` komutu onu göstermez, `ls -la` gösterir.

## 1. DNS kaydını ekleyin {#dns}

Alan adınızın DNS panelinde yeni bir **A** kaydı oluşturun:

| Alan | Değer |
|---|---|
| Tür (Type) | `A` |
| Ad (Name / Host) | `bekci` — `bekci.ornek.com` için. Alan adının kendisi (`ornek.com`) için `@` |
| Değer (Value / IPv4 address) | Sunucunuzun genel IPv4 adresi, ör. `203.0.113.10` |
| TTL | Otomatik (Auto) ya da 300 |

> [!IMPORTANT] Cloudflare kullanıyorsanız
> Kaydı **DNS only** (gri bulut) olarak ekleyin; turuncu bulut (**Proxied**) kapalı olsun. Sertifika alındıktan sonra isterseniz proxy'yi açabilirsiniz; ayrıntılar [aşağıda](#cloudflare).

Sunucunun genel IP adresini barındırma sağlayıcınızın panelinde görebilirsiniz; sunucuda `hostname -I` komutunun yazdığı ilk adres de genellikle budur. IPv6 kullanıyorsanız aynı adla bir **AAAA** kaydı da ekleyebilirsiniz; eklerseniz sunucunun IPv6 üzerinden de erişilebilir olduğundan emin olun.

> [!CHECK]
> DNS kaydının yayılması birkaç dakika sürebilir. Sunucuda şu komut sunucunuzun IP adresini göstermeli:
>
> ```bash
> getent ahostsv4 ⟦bekci.ornek.com⟧ | head -n 1
> ```
>
> Hiçbir şey yazmıyorsa ya da başka bir adres görüyorsanız birkaç dakika bekleyip tekrar deneyin. Cloudflare'de proxy açıksa burada Cloudflare'in adreslerini görürsünüz.

## 2. Docker'ı kurun {#docker}

Docker kurulu değilse [Docker rehberindeki ilk adımı](/docs/kurulum/docker/#docker-kurun) izleyin (resmi betik Compose eklentisini de kurar).

> [!CHECK]
> `docker compose version` bir sürüm numarası yazdırmalı.

## 3. 80 ve 443 portlarını kontrol edin {#portlar}

Önce portların boş olduğundan emin olun:

```bash
ss -ltnp 'sport = :80'
ss -ltnp 'sport = :443'
```

> [!CHECK]
> Her iki komut da yalnızca başlık satırını (`State  Recv-Q  Send-Q …`) yazdırmalı. Altında bir satır varsa o portu başka bir program kullanıyordur: `users:(("nginx",…))` gibi bir ifade hangisi olduğunu söyler. Bu durumda [sorunlar bölümüne](#port-dolu) bakın.

Sonra portların dışarıdan erişilebilir olduğundan emin olun:

- **Barındırma sağlayıcınızın güvenlik duvarı** (Hetzner, DigitalOcean, AWS “security group” vb.): gelen **80/TCP**, **443/TCP** ve isteğe bağlı olarak **443/UDP** (HTTP/3) bağlantılarına izin verin.
- **ufw** kullanıyorsanız (`ufw status` “Status: active” diyorsa) kuralları ekleyin; SSH kuralını unutmayın:

```bash
ufw allow OpenSSH
ufw allow 80/tcp
ufw allow 443/tcp
ufw allow 443/udp
```

> [!NOTE]
> Docker, yayınladığı portlar için güvenlik duvarı kurallarını kendisi ekler; bu yüzden ufw kapalıysa onu bu kurulum için açmanız gerekmez. Asıl önemli olan, sağlayıcınızın güvenlik duvarının bu portlara izin vermesidir.

## 4. Klasörü oluşturun {#klasor}

```bash
mkdir -p /opt/bekci
cd /opt/bekci
```

Bundan sonraki tüm komutları bu klasörün içindeyken çalıştırın. Terminali kapatıp yeniden bağlanırsanız önce `cd /opt/bekci` yazın.

## 5. Üç dosyayı oluşturun {#dosyalar}

Dosyaları iki yoldan biriyle oluşturabilirsiniz. İndirmek daha hızlıdır ve kopyala-yapıştır hatası riski yoktur.

:::tabs key=olusturma label="Dosyaları oluşturma yolu"
@tab İndirerek (önerilen)
```bash
cd /opt/bekci
curl -fsSL -o docker-compose.yml https://bekci.app/indir/caddy/docker-compose.yml
curl -fsSL -o Caddyfile https://bekci.app/indir/caddy/Caddyfile
curl -fsSL -o .env https://bekci.app/indir/caddy/env.example
ls -la
```

Üçüncü komut örnek ayar dosyasını `.env` adıyla kaydeder; adın başındaki nokta önemlidir.
@tab Elle oluşturarak
Her dosya için `nano` ile boş bir dosya açın, aşağıdaki içeriği yapıştırın, <kbd>Ctrl</kbd>+<kbd>O</kbd> ve <kbd>Enter</kbd> ile kaydedip <kbd>Ctrl</kbd>+<kbd>X</kbd> ile çıkın.

**docker-compose.yml** — `nano /opt/bekci/docker-compose.yml`

```yaml title="/opt/bekci/docker-compose.yml" download="/indir/caddy/docker-compose.yml"
# Bekci + Caddy: otomatik HTTPS ile kurulum (Let's Encrypt).
# Adım adım rehber: https://bekci.app/docs/kurulum/caddy/
#
# Bu dosyayı değiştirmeniz gerekmez: alan adı, e-posta ve sürüm .env
# dosyasından okunur. Üç dosya aynı klasörde durur (ör. /opt/bekci):
#   docker-compose.yml   bu dosya
#   Caddyfile            Caddy ayarı
#   .env                 alan adınız ve e-postanız
#
# Başlatma:     docker compose up -d
# Günlükler:    docker compose logs -f bekci      (Caddy için: caddy)
# Güncelleme:   docker compose pull && docker compose up -d
# Yedek, sürüm sabitleme, geri dönüş: https://bekci.app/docs/guncelleme-yedek/

name: bekci

services:
  bekci:
    # UPTIME_TAG: latest (SQLite, önerilen) | postgres (gömülü PostgreSQL)
    # ya da sabit bir sürüm: 1.0.0 / 1.0.0-postgres. İki türün verisi
    # birbirine taşınmaz: baştan birini seçin.
    image: kadirsungurlu/bekci:${UPTIME_TAG:-latest}
    restart: unless-stopped
    # Kapanışta uygulama (ve gömülü PostgreSQL) düzgünce kapansın.
    stop_grace_period: 30s
    environment:
      BASE_URL: https://${UPTIME_DOMAIN:?UPTIME_DOMAIN .env dosyasında tanımlı olmalı}
      TZ: ${TZ:-Europe/Istanbul}
      LOG_LEVEL: ${LOG_LEVEL:-info}
      MAX_CONCURRENT_CHECKS: ${MAX_CONCURRENT_CHECKS:-50}
    volumes:
      - bekci-data:/data
    # Dışarıya doğrudan açılmaz; yalnızca Caddy üzerinden erişilir.
    expose:
      - "8080"
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"

  caddy:
    # caddy:2-alpine (digest ile sabit)
    image: caddy:2-alpine@sha256:6aeddd44c3078b0f9a35206472a11420648a79c184603ef95957d0a20044cb2b
    restart: unless-stopped
    depends_on:
      bekci:
        condition: service_healthy
    ports:
      - "80:80"
      - "443:443"
      - "443:443/udp" # HTTP/3
    environment:
      UPTIME_DOMAIN: ${UPTIME_DOMAIN}
      ACME_EMAIL: ${ACME_EMAIL:?ACME_EMAIL .env dosyasında tanımlı olmalı}
      STATUS_DOMAIN: ${STATUS_DOMAIN:-}
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy-data:/data
      - caddy-config:/config
    logging:
      driver: json-file
      options:
        max-size: 10m
        max-file: "3"

volumes:
  bekci-data:
  caddy-data:
  caddy-config:
```

**Caddyfile** — `nano /opt/bekci/Caddyfile` (dosyanın adı tam olarak `Caddyfile`; uzantısı yok, C büyük)

```caddyfile title="/opt/bekci/Caddyfile" download="/indir/caddy/Caddyfile"
# Caddy: HTTPS sertifikasını otomatik alır ve Bekci'ye yönlendirir.
# Alan adları .env dosyasından gelir; bu dosyayı değiştirmeniz gerekmez.
# Rehber: https://bekci.app/docs/kurulum/caddy/

{
	email {$ACME_EMAIL}
}

{$UPTIME_DOMAIN} {
	encode zstd gzip
	# Tarayıcı bu alan adına 1 yıl boyunca yalnızca HTTPS ile bağlansın.
	# ">" işareti: uygulama da aynı başlığı gönderirse çift olmasın, bu geçerli olsun.
	header >Strict-Transport-Security "max-age=31536000"
	reverse_proxy bekci:8080 {
		# Canlı akış (Server-Sent Events) tamponlanmadan iletilsin.
		flush_interval -1
	}
}

# İsteğe bağlı: durum sayfası için ayrı alan adı (ör. durum.ornek.com).
# .env'de STATUS_DOMAIN tanımlayıp aşağıdaki bloğun başındaki # işaretlerini
# kaldırın; ardından panelde durum sayfasının "Özel alan adı" alanına aynı
# adı yazın. Bu adresten yalnızca durum sayfası açılır, yönetim paneli açılmaz.
#
# {$STATUS_DOMAIN} {
# 	encode zstd gzip
# 	header >Strict-Transport-Security "max-age=31536000"
# 	reverse_proxy bekci:8080
# }
```

**.env** — `nano /opt/bekci/.env` (içeriğini bir sonraki adımda düzenleyeceksiniz)

```ini title="/opt/bekci/.env" download="/indir/caddy/env.example"
# Bekci + Caddy ayarları. Bu dosyanın adı sunucuda ".env" olmalı ve
# docker-compose.yml ile aynı klasörde durmalı (ör. /opt/bekci/.env).
# Rehber: https://bekci.app/docs/kurulum/caddy/

# Panelin alan adı (http:// ve / olmadan). DNS A kaydı bu sunucuyu göstermeli.
UPTIME_DOMAIN=⟦bekci.ornek.com⟧

# Let's Encrypt sertifika bildirimleri için e-posta adresiniz.
ACME_EMAIL=⟦admin@ornek.com⟧

# latest: SQLite (önerilen) | postgres: gömülü PostgreSQL
# Sürümü sabitlemek için: 1.0.0 (SQLite) ya da 1.0.0-postgres.
UPTIME_TAG=latest

TZ=Europe/Istanbul
LOG_LEVEL=info

# Aynı anda en fazla kontrol sayısı (yüzlerce monitörde artırılabilir).
MAX_CONCURRENT_CHECKS=50

# İsteğe bağlı: durum sayfası için ayrı alan adı (Caddyfile'daki bloğu da açın).
# STATUS_DOMAIN=durum.ornek.com
```
:::

> [!CHECK]
> `ls -la /opt/bekci` çıktısında üç dosyayı da görmelisiniz: `.env`, `Caddyfile` ve `docker-compose.yml`.

## 6. .env dosyasına alan adınızı yazın {#env}

```bash
nano /opt/bekci/.env
```

Yalnızca iki satırı değiştirin; geri kalanlar olduğu gibi kalabilir:

```ini title="/opt/bekci/.env (değişen satırlar)"
UPTIME_DOMAIN=⟦bekci.ornek.com⟧
ACME_EMAIL=⟦siz@ornek.com⟧
```

- `UPTIME_DOMAIN`: 1. adımda DNS kaydını eklediğiniz alan adı. Başına `https://` koymayın, sonuna `/` eklemeyin.
- `ACME_EMAIL`: Let's Encrypt'in sertifikayla ilgili bildirimleri göndereceği e-posta adresiniz.

Diğer satırların anlamı: `UPTIME_TAG` hangi Bekci sürümünün kullanılacağı ([sürüm sabitleme](/docs/guncelleme-yedek/#surum-sabitleme)), `TZ` saat dilimi, `MAX_CONCURRENT_CHECKS` aynı anda yapılacak en fazla kontrol sayısı ([tüm değişkenler](/docs/ortam-degiskenleri/)).

Kaydedip çıkın (<kbd>Ctrl</kbd>+<kbd>O</kbd>, <kbd>Enter</kbd>, <kbd>Ctrl</kbd>+<kbd>X</kbd>).

> [!CHECK]
> ```bash
> cd /opt/bekci
> docker compose config --quiet
> ```
>
> Komut hiçbir şey yazmadan bitmeli. `required variable UPTIME_DOMAIN is missing a value` görüyorsanız `.env` dosyası yanlış adla kaydedilmiş (ör. `env.example` ya da `.env.txt`) ya da başka bir klasörde duruyordur.

## 7. Başlatın {#baslatin}

```bash
cd /opt/bekci
docker compose up -d
```

İlk seferde imajlar indirilir. Caddy, Bekci sağlıklı olana kadar bekler, sonra başlar ve sertifikayı almaya çalışır.

> [!CHECK]
> Birkaç saniye sonra `docker compose ps` iki satır göstermeli: `bekci` için **STATUS** `Up … (healthy)`, `caddy` için `Up …`. `caddy` satırının **PORTS** sütununda `0.0.0.0:80->80/tcp` ve `0.0.0.0:443->443/tcp` görünür.

## 8. Sertifikayı doğrulayın {#sertifika}

Caddy'nin sertifikayı aldığını günlüğünden görebilirsiniz:

```bash
docker compose logs caddy | grep -i "certificate obtained"
```

> [!CHECK]
> `"msg":"certificate obtained successfully","identifier":"bekci.ornek.com"` içeren bir satır görmelisiniz. Tarayıcıda `https://⟦bekci.ornek.com⟧` adresi adres çubuğunda kilit simgesiyle açılmalı. Sunucudan da deneyebilirsiniz:
>
> ```bash
> curl -sI https://⟦bekci.ornek.com⟧/healthz | head -n 1
> ```
>
> Çıktı `HTTP/2 200` olmalı.

Satır yoksa bir dakika bekleyip tekrar bakın. Hâlâ yoksa `docker compose logs caddy` çıktısındaki `"level":"error"` satırları nedeni söyler; [sık karşılaşılan sorunlara](#sorunlar) bakın.

## 9. Yönetici hesabını oluşturun {#yonetici}

`https://⟦bekci.ornek.com⟧` adresini açın. **Hoş geldiniz** ekranında kullanıcı adı ve en az 8 karakterlik şifrenizi girip **Hesabı oluştur**'a basın.

> [!CHECK]
> Monitör listesi açılır. Kurulum tamamlandı; [İlk adımlar](/docs/ilk-adimlar/) ile ilk monitörünüzü ekleyin.

`BASE_URL` bu kurulumda `.env`'deki alan adından otomatik oluşur (`https://` + `UPTIME_DOMAIN`); ayrıca ayarlamanız gerekmez.

## Sık karşılaşılan sorunlar {#sorunlar}

### Port 80 ya da 443 kullanımda {#port-dolu}

`docker compose up -d` şu hatalardan biriyle durur:

```text
Bind for 0.0.0.0:80 failed: port is already allocated
```

```text
failed to bind host port 0.0.0.0:80/tcp: address already in use
```

İlki portu **başka bir konteynerin** kullandığını söyler; hangisi olduğunu `docker ps --filter publish=80` gösterir (Coolify kurulu sunucularda bu `coolify-proxy` olur). İkincisi portu sunucuda doğrudan çalışan bir programın (çoğunlukla Nginx ya da Apache) kullandığını söyler; `ss -ltnp 'sport = :80'` hangisi olduğunu gösterir.

- O web sunucusunu kullanıyorsanız Caddy'li kurulum yerine Bekci'yi onun arkasına yerleştirin: [ters vekil arkasında](/docs/kurulum/ters-vekil/). Coolify varsa: [Coolify](/docs/kurulum/coolify/).
- Kullanmıyorsanız durdurup kapatabilirsiniz, ör. `systemctl disable --now nginx` (Apache için `apache2`), sonra `docker compose up -d` komutunu tekrarlayın.

### DNS henüz yayılmadı ya da yanlış {#dns-sorunu}

Caddy günlüğünde sertifika alınamadığını söyleyen hatalar görürsünüz ve tarayıcı güvenlik uyarısı verir. [1. adımdaki](#dns) `getent` komutu sunucunuzun IP'sini göstermiyorsa DNS henüz hazır değildir. Caddy aralıklarla kendisi yeniden dener; DNS doğru adresi gösterdikten sonra hızlandırmak için:

```bash
docker compose restart caddy
```

### Cloudflare kullanıyorum {#cloudflare}

- İlk kurulumda kayıt **DNS only** (gri bulut) olmalı; aksi halde Let's Encrypt doğrulaması Cloudflare'e takılabilir.
- Sertifika alındıktan sonra proxy'yi (turuncu bulut) açarsanız Cloudflare'in **SSL/TLS** ayarını **Full (strict)** yapın. **Flexible** modu sonsuz yönlendirme döngüsüne (`ERR_TOO_MANY_REDIRECTS`) yol açar.
- Canlı güncellemeler Cloudflare arkasında da çalışır: Bekci bağlantıyı açık tutmak için 25 saniyede bir sinyal gönderir.
- Sertifika yenilemesi ileride sorun çıkarırsa kaydı geçici olarak yeniden **DNS only** yapın.

### Tarayıcı “güvenli değil” uyarısı veriyor {#guvenli-degil}

Sertifika henüz alınmamıştır. [8. adımdaki](#sertifika) kontrolü yapın. Let's Encrypt, çok sayıda başarısız denemeden sonra bir süre yeni istek kabul etmez; bu yüzden DNS'i ve portları düzelttikten sonra `docker compose up -d` komutunu art arda çok kez çalıştırmak yerine günlüğü izleyin: `docker compose logs -f caddy`.

### .env'deki değişiklik uygulanmadı {#env-degisikligi}

`.env` dosyasını değiştirdikten sonra `docker compose up -d` çalıştırın; Compose değişen konteynerleri yeniden oluşturur. `Caddyfile` dosyasını değiştirdiyseniz ayrıca `docker compose restart caddy` gerekir.

## Durum sayfası için ayrı alan adı (isteğe bağlı) {#durum-alan-adi}

Müşterilerinize açık durum sayfasını `durum.ornek.com` gibi ayrı bir adresten yayınlayabilirsiniz. Bu adreste yalnızca durum sayfası açılır, yönetim paneli açılmaz.

1. `durum` adıyla, aynı sunucuyu gösteren bir **A** kaydı ekleyin ([1. adım](#dns) gibi).
2. `.env` dosyasında son satırın başındaki `#` işaretini kaldırıp alan adını yazın: `STATUS_DOMAIN=⟦durum.ornek.com⟧`
3. `Caddyfile` dosyasının sonundaki bloğun satır başlarındaki `#` işaretlerini kaldırın. Blok şöyle görünmeli:

   ```caddyfile title="/opt/bekci/Caddyfile (dosyanın sonu)"
   {$STATUS_DOMAIN} {
   	encode zstd gzip
   	header >Strict-Transport-Security "max-age=31536000"
   	reverse_proxy bekci:8080
   }
   ```

4. Uygulayın:

   ```bash
   cd /opt/bekci
   docker compose up -d --force-recreate caddy
   ```

5. Panelde **Durum sayfaları** bölümünden sayfanızı açın ve **Özel alan adı** alanına `durum.ornek.com` yazıp kaydedin.

> [!CHECK]
> `https://⟦durum.ornek.com⟧` adresi durum sayfanızı açmalı.

## Sonraki adımlar {#sonraki}

- [İlk adımlar](/docs/ilk-adimlar/): ilk monitör, bildirim kanalı ve durum sayfası.
- [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/): güncelleme `cd /opt/bekci && docker compose pull && docker compose up -d` kadar basittir, ama önce yedek almayı öğrenin.
