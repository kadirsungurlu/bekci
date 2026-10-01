---
id: proxy
title: Ters vekil arkasında
nav: Ters vekil arkasında
description: Sunucunuzda zaten Nginx, Traefik, Apache ya da Caddy çalışıyorsa Bekci'yi onun arkasına yerleştirin. Her biri için dosyanın yeri, tam içeriği ve canlı güncellemelerin çalışması için gereken ayarlar.
section: Kurulum
order: 6
slug: kurulum/ters-vekil
---

Sunucunuzda 80 ve 443 portlarını kullanan bir web sunucusu (ters vekil, *reverse proxy*) zaten varsa Bekci'yi onun arkasına koyarsınız: vekil HTTPS'i sağlar ve istekleri Bekci'ye iletir. Coolify kullanıyorsanız bu sayfa yerine [Coolify rehberini](/docs/kurulum/coolify/) izleyin.

Hangi vekili kullanırsanız kullanın, Bekci'nin düzgün çalışması için vekilin şunları yapması gerekir:

| Gereken | Neden |
|---|---|
| `Host`, `X-Forwarded-For` ve `X-Forwarded-Proto` başlıklarını iletmek | Bekci, isteğin HTTPS ile geldiğini `X-Forwarded-Proto` başlığından anlar (güvenli oturum çerezi ve HSTS için). İstemcinin gerçek IP adresini `X-Forwarded-For`'dan alır (giriş denemesi sınırı ve işlem kaydı için). |
| `/api/events` yanıtını **tamponlamadan** iletmek | Arayüzün canlı güncellemeleri bu uzun ömürlü bağlantıdan (Server-Sent Events) gelir. Vekil yanıtı biriktirirse panel güncellenmez. |
| Uzun bağlantıyı erken kesmemek | Bekci bu bağlantıda 25 saniyede bir sinyal gönderir; okuma zaman aşımı bundan uzun olmalı. |
| Büyük istek gövdelerine izin vermek | Uptime Kuma'dan içe aktarma 200 MB'a kadar dosya yükleyebilir. |

## 1. Bekci'yi yalnızca yerelde dinleyecek şekilde çalıştırın {#bekci}

Vekil aynı sunucudaysa Bekci'nin portunu yalnızca `127.0.0.1` adresine yayınlayın; böylece Bekci'ye dışarıdan vekili atlayarak erişilemez. [Docker Compose](/docs/kurulum/docker-compose/) ile:

```yaml title="/opt/bekci/docker-compose.yml"
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    ports:
      - "127.0.0.1:8080:8080"   # yalnızca bu sunucudan erişilebilir
    environment:
      BASE_URL: https://⟦bekci.ornek.com⟧
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
volumes:
  bekci-data:
```

```bash
cd /opt/bekci
docker compose up -d
```

`BASE_URL`, panelin dışarıdan açılacağı HTTPS adresi olmalıdır. (Traefik'i Docker etiketleriyle kullanıyorsanız bu dosya yerine aşağıdaki **Traefik** sekmesindekini kullanın.)

> [!CHECK]
> Sunucuda `curl http://127.0.0.1:8080/healthz` komutu `ok` yazdırmalı.

## 2. Vekili ayarlayın {#vekil}

Kullandığınız vekilin sekmesini seçin. Her sekme dosyanın nereye gideceğini, tam içeriğini ve ayarı nasıl uygulayacağınızı anlatır.

:::tabs label="Ters vekil"
@tab Nginx
**Dosyanın yeri:** Debian ve Ubuntu'da `/etc/nginx/sites-available/bekci`. RHEL, AlmaLinux, Rocky ve nginx.org paketlerinde `sites-available` klasörü yoktur; dosyayı `/etc/nginx/conf.d/bekci.conf` adıyla oluşturun.

```bash
nano /etc/nginx/sites-available/bekci
```

```nginx title="/etc/nginx/sites-available/bekci"
server {
    listen 80;
    listen [::]:80;
    server_name ⟦bekci.ornek.com⟧;

    # Yedek geri yükleme ve Uptime Kuma içe aktarma için büyük dosyalar
    client_max_body_size 200m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Connection "";

        # Canlı güncellemeler (SSE): tamponlamadan ilet, uzun bağlantıyı kesme
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 1h;
    }
}
```

Siteyi etkinleştirin (yalnızca Debian/Ubuntu), ayarı sınayın ve Nginx'i yeniden yükleyin:

```bash
ln -s /etc/nginx/sites-available/bekci /etc/nginx/sites-enabled/bekci
nginx -t
systemctl reload nginx
```

`nginx -t` şu iki satırı yazdırmalı: `syntax is ok` ve `test is successful`.

**HTTPS:** Henüz sertifikanız yoksa en kolay yol Certbot'tur. Ubuntu/Debian'da `apt install certbot python3-certbot-nginx`, ardından `certbot --nginx -d ⟦bekci.ornek.com⟧`. Certbot bu dosyaya 443 portunu ve sertifika satırlarını kendisi ekler; yukarıdaki `location` ayarları olduğu gibi kalır.
@tab Traefik
Traefik'i Docker etiketleriyle kullanıyorsanız Bekci'yi Traefik'in bağlı olduğu Docker ağına ekleyip etiketlerle tanıtın. Bu durumda port yayınlamanız gerekmez. [1. adımdaki](#bekci) dosya yerine şunu kullanın:

```yaml title="/opt/bekci/docker-compose.yml"
name: bekci
services:
  bekci:
    image: kadirsungurlu/bekci:latest
    restart: unless-stopped
    stop_grace_period: 30s
    environment:
      BASE_URL: https://⟦bekci.ornek.com⟧
      TZ: Europe/Istanbul
    volumes:
      - bekci-data:/data
    networks:
      - ⟦proxy⟧
    labels:
      - traefik.enable=true
      - traefik.http.routers.bekci.rule=Host(`⟦bekci.ornek.com⟧`)
      - traefik.http.routers.bekci.entrypoints=⟦websecure⟧
      - traefik.http.routers.bekci.tls.certresolver=⟦letsencrypt⟧
      - traefik.http.services.bekci.loadbalancer.server.port=8080
networks:
  ⟦proxy⟧:
    external: true
volumes:
  bekci-data:
```

Vurgulu adları kendi Traefik kurulumunuzdakilerle değiştirin:

- `proxy`: Traefik konteynerinin bağlı olduğu Docker ağı (`docker network ls` ile görebilirsiniz).
- `websecure`: Traefik'in 443 portu için tanımladığınız giriş noktası (entrypoint) adı.
- `letsencrypt`: Traefik ayarınızdaki sertifika çözücüsünün (certresolver) adı.

```bash
cd /opt/bekci
docker compose up -d
```

Traefik yanıtları tamponlamaz ve `X-Forwarded-*` başlıklarını kendisi ekler; canlı güncellemeler için ek ayar gerekmez.
@tab Apache
Gerekli modülleri açın (Debian/Ubuntu):

```bash
a2enmod proxy proxy_http headers
```

**Dosyanın yeri:** Debian ve Ubuntu'da `/etc/apache2/sites-available/bekci.conf`. RHEL ailesinde `/etc/httpd/conf.d/bekci.conf`.

```bash
nano /etc/apache2/sites-available/bekci.conf
```

```apache title="/etc/apache2/sites-available/bekci.conf"
<VirtualHost *:80>
    ServerName ⟦bekci.ornek.com⟧

    ProxyPreserveHost On
    ProxyRequests Off
    RequestHeader set X-Forwarded-Proto expr=%{REQUEST_SCHEME}

    # Canlı güncellemeler (SSE): yanıtı beklemeden ilet, uzun bağlantıyı kesme
    ProxyPass        / http://127.0.0.1:8080/ flushpackets=on timeout=3600
    ProxyPassReverse / http://127.0.0.1:8080/
</VirtualHost>
```

Siteyi etkinleştirin, ayarı sınayın ve yeniden yükleyin:

```bash
a2ensite bekci
apachectl configtest
systemctl reload apache2
```

`apachectl configtest` çıktısının son satırı `Syntax OK` olmalı.

**HTTPS:** Ubuntu/Debian'da `apt install certbot python3-certbot-apache`, ardından `certbot --apache -d ⟦bekci.ornek.com⟧`. Certbot 443 için bu sitenin bir kopyasını oluşturur; `X-Forwarded-Proto` orada otomatik olarak `https` olur.
@tab Caddy
Sunucunuzda paket olarak kurulmuş bir Caddy varsa ayar dosyası `/etc/caddy/Caddyfile` olur. Dosyanın sonuna şu bloğu ekleyin:

```bash
nano /etc/caddy/Caddyfile
```

```caddyfile title="/etc/caddy/Caddyfile (sonuna ekleyin)"
⟦bekci.ornek.com⟧ {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8080 {
		# Canlı güncellemeler (SSE) tamponlanmadan iletilsin
		flush_interval -1
	}
}
```

Ayarı sınayıp Caddy'yi yeniden yükleyin:

```bash
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
```

`caddy validate` çıktısının sonunda `Valid configuration` yazmalı. Caddy sertifikayı kendisi alır; ek bir şey yapmanız gerekmez.

Caddy'niz Docker'da çalışıyorsa Bekci'yi Caddy ile aynı Docker ağına ekleyin ve `127.0.0.1:8080` yerine `bekci:8080` yazın ([Caddy'li kurulumdaki](/docs/kurulum/caddy/) dosyalar bunun tam bir örneğidir).
:::

## 3. Doğrulayın {#dogrulayin}

1. Tarayıcıda `https://⟦bekci.ornek.com⟧` adresini açın ve giriş yapın (ilk kez açıyorsanız **Hoş geldiniz** ekranında yönetici hesabınızı oluşturun).
2. Canlı güncellemeleri sınayın: kontrol aralığı kısa (ör. 20 saniye) bir monitör ekleyin ve monitör listesinde bekleyin. Her kontrolden sonra monitörün durum çubukları ve yanıt süresi sayfayı yenilemeden güncellenmelidir.

> [!CHECK]
> Giriş yapabiliyorsunuz ve sayfanın üst kısmında **Bağlantı yok** uyarısı görünmüyor. Bu uyarı sürekli görünüyorsa canlı akış vekilde takılıyordur; [aşağıya](#canli-guncelleme-yok) bakın.

## Güvenilir vekil (TRUSTED_PROXY) {#trusted-proxy}

Bekci, `X-Forwarded-For` başlığındaki istemci adresine yalnızca isteği ileten bağlantı güvenilir bir adresten geliyorsa inanır. Varsayılan olarak tüm özel (`10.x`, `172.16–31.x`, `192.168.x`) ve yerel (`127.x`) adresler güvenilir sayılır; vekil aynı sunucuda ya da aynı Docker ağında çalışıyorsa ayar gerekmez.

Daha sıkı olmak isterseniz güvenilir ağları `TRUSTED_PROXY` ortam değişkeniyle virgülle ayırarak verin, ör. `TRUSTED_PROXY=172.17.0.1/32`. Ayarlıysa yalnızca bu ağlardan gelen başlıklara güvenilir.

Değişken boşken Bekci açılışta bir kez uyarı yazar (`TRUSTED_PROXY ayarlı değil …`). Varsayılan, mevcut Coolify/Traefik/Caddy kurulumları bozulmasın diye gevşek bırakılmıştır. **Ters vekilsiz**, doğrudan `-p 8080:8080` ile yayınlanan bir kurulumda ise Docker NAT yüzünden her istek özel bir adresten (Docker ağ geçidi) gelir; o zaman bir istemci `X-Forwarded-For` başlığıyla adres uydurup giriş sınırlarını ve IP kilidini aşabilir, işlem kaydına sahte adres yazdırabilir. Böyle kurulumlarda `TRUSTED_PROXY=127.0.0.1/32` verin: başlıklara güvenilmez, bağlanan adres kullanılır.

## Sık karşılaşılan sorunlar {#sorunlar}

### Canlı güncellemeler gelmiyor, “Bağlantı yok” uyarısı {#canli-guncelleme-yok}

Vekil `/api/events` yanıtını tamponluyor ya da bağlantıyı erken kesiyor. Yukarıdaki ayarlardaki şu satırları kontrol edin: Nginx'te `proxy_buffering off` ve `proxy_read_timeout`, Apache'de `flushpackets=on`, Caddy'de `flush_interval -1`. Arada Cloudflare gibi başka bir katman varsa o da bağlantıyı tamponlamamalıdır.

### 502 Bad Gateway {#502}

Vekil Bekci'ye ulaşamıyor. Bekci çalışıyor mu (`docker compose ps`), port doğru mu (`curl http://127.0.0.1:8080/healthz`)? Traefik'te Bekci ile Traefik aynı Docker ağında olmalı.

### İçe aktarmada “413 Request Entity Too Large” {#413}

Vekilin istek gövdesi sınırı düşük. Nginx'te `client_max_body_size 200m;` satırını ekleyin.

### Giriş yapıyorum ama hemen çıkış yapılıyor ya da sonsuz yönlendirme {#yonlendirme}

`X-Forwarded-Proto` başlığı iletilmiyor ya da yanlış. Vekil HTTPS'i sonlandırıyorsa bu başlık `https` olmalıdır. Cloudflare kullanıyorsanız SSL/TLS modunu **Full (strict)** yapın; **Flexible** modu yönlendirme döngüsüne yol açar.

### Bildirimlerdeki ve ajan komutlarındaki adres yanlış {#yanlis-adres}

`BASE_URL` ayarlı değil ya da yanlış. Panelin dışarıdan açıldığı `https://` adresini yazın ve konteyneri yeniden oluşturun (`docker compose up -d`).
