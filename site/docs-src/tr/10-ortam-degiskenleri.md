---
id: env
title: Ortam değişkenleri
nav: Ortam değişkenleri
description: Bekci'yi ve ajanlarını yapılandıran tüm ortam değişkenleri, varsayılan değerleri ve nasıl verileceği.
section: Bakım ve başvuru
order: 10
slug: ortam-degiskenleri
---

Bekci'nin çoğu ayarı panelden yapılır. Sunucu düzeyindeki birkaç ayar ise konteyner başlatılırken ortam değişkeniyle verilir. Hiçbiri zorunlu değildir; yalnızca `BASE_URL`'i ayarlamanızı öneririz.

## Nasıl verilir? {#nasil}

:::tabs key=kurulum label="Kurulum yolu"
@tab Docker Compose
`docker-compose.yml` dosyasında `environment:` altına ekleyin, sonra `docker compose up -d` çalıştırın:

```yaml title="/opt/bekci/docker-compose.yml (parça)"
    environment:
      BASE_URL: https://⟦bekci.ornek.com⟧
      TZ: Europe/Istanbul
      LOG_LEVEL: info
```

[Caddy'li kurulumda](/docs/kurulum/caddy/) `BASE_URL`, `TZ`, `LOG_LEVEL` ve `MAX_CONCURRENT_CHECKS` değerlerini `.env` dosyasından değiştirirsiniz.
@tab docker run
Her değişken için bir `-e AD=değer` ekleyin. Değişkenler konteyner oluşturulurken okunduğu için konteyneri silip aynı birimle yeniden oluşturmanız gerekir ([örnek](/docs/kurulum/docker/#base-url)).
@tab Coolify
Uygulamanın **Environment Variables** bölümüne ekleyip yeniden dağıtın.
:::

> [!CHECK]
> `docker exec ⟦bekci⟧ printenv BASE_URL` komutu verdiğiniz değeri yazdırmalı (Compose'da konteyner adı `bekci-bekci-1`).

## Bekci (ana uygulama) {#uygulama}

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `BASE_URL` | — | Panelin dışarıdan açıldığı adres, ör. `https://bekci.ornek.com`. Bildirimlerdeki bağlantılar ve sunucu ajanı kurulum komutları bunu kullanır. Boşsa kurulum komutları isteğin geldiği adresi kullanır. Durum sayfası özel alan adı bu adresle aynı olamaz. |
| `TZ` | `Europe/Istanbul` | Saat dilimi: günlük özetler ve gece yedeğinin saati buna göredir. Ör. `Europe/Berlin`, `UTC`. |
| `LOG_LEVEL` | `info` | Günlük ayrıntısı: `debug`, `info`, `warn` ya da `error`. |
| `MAX_CONCURRENT_CHECKS` | `50` | Aynı anda çalışabilecek en fazla kontrol sayısı. Yüzlerce monitörde artırılabilir. |
| `DATABASE_URL` | — | Verilirse SQLite yerine **PostgreSQL** kullanılır: `postgres://kullanıcı:parola@sunucu:5432/veritabanı?sslmode=disable`. `postgres://` ya da `postgresql://` ile başlamalıdır. Bu durumda Bekci gece yedeği almaz; yedeği veritabanı tarafında alın. `postgres` imajında bu değişken yok sayılır (her zaman gömülü veritabanı kullanılır). |
| `DATA_DIR` | `/data` | SQLite veritabanı ve yedeklerin klasörü. Docker'da değiştirmeniz gerekmez; birimi `/data`'ya bağlayın. |
| `ADDR` | `:8080` | Konteynerin içinde dinlenecek adres ve port. |
| `TRUSTED_PROXY` | — | Güvenilir ters vekil ağları, virgülle ayrılmış CIDR listesi (ör. `172.17.0.1/32,10.0.0.0/8`). Ayarlıysa `X-Forwarded-For` başlığına yalnızca bu ağlardan gelen bağlantılarda güvenilir. Boşsa tüm özel ve yerel adresler güvenilir sayılır ([ayrıntı](/docs/kurulum/ters-vekil/#trusted-proxy)). |
| `UPTIME_LOCK_WAIT` | `600` | Aynı veri klasörünü kullanan ikinci bir kopyanın, ilki kapanana kadar en fazla kaç saniye bekleyeceği. Süre dolarsa hata ile çıkar. |
| `AGENT_DIR` | `/usr/local/share/uptime/agents` | Diğer platformların ajan programlarının klasörü. Resmi imajda hazırdır; değiştirmeyin. |
| `PROBE_IMAGE` | — | Verilirse kontrol noktası kurulum komutu programı indirmek yerine bu Docker imajını kullanır. |

## Sunucu ajanı ve kontrol noktası {#ajan}

Bu değişkenleri panelden aldığınız kurulum komutu sizin için ayarlar (`/etc/uptime-agent.env`, `/etc/uptime-probe.env` ya da Windows'ta `C:\Program Files\Uptime\agent.env`). Normalde elle değiştirmeniz gerekmez.

| Değişken | Varsayılan | Açıklama |
|---|---|---|
| `PROBE_SERVER` | — | Panelin adresi (`BASE_URL`), ör. `https://bekci.ornek.com`. |
| `PROBE_TOKEN` | — | Panelin verdiği token (`upr_` ile başlar). |
| `PROBE_ALLOW_INSECURE` | — | `1` ise şifrelenmemiş `http://` adrese bağlanmaya izin verilir (önerilmez). |
| `MAX_CONCURRENT_CHECKS` | `20` | Kontrol noktasında aynı anda en fazla kontrol sayısı. |
| `METRICS` | `1` | `0` ise sunucu ölçümleri hiç toplanmaz. |
| `ADDR` | `:8080` | Ajanın sağlık kontrolü adresi; `-` ise kapalı (kurulum komutları `-` kullanır). |
| `HOST_PROC`, `HOST_SYS`, `HOST_ETC`, `HOST_ROOT` | — | Ajan Docker'da çalışırken sunucunun `/proc`, `/sys`, `/etc` ve kök dizininin bağlandığı yerler. Docker kurulum komutu bunları ayarlar. |
| `DOCKER_HOST` | `unix:///var/run/docker.sock` | Konteyner ölçümleri için Docker API adresi. |

## Caddy'li kurulumun .env dosyası {#caddy-env}

[Caddy'li kurulumdaki](/docs/kurulum/caddy/) `/opt/bekci/.env` dosyası şu değerleri okur:

| Değişken | Açıklama |
|---|---|
| `UPTIME_DOMAIN` | Panelin alan adı (`https://` olmadan). `BASE_URL` bundan oluşturulur. Zorunlu. |
| `ACME_EMAIL` | Let's Encrypt bildirimleri için e-posta. Zorunlu. |
| `UPTIME_TAG` | İmaj etiketi: `latest`, `postgres`, `1.0.0`, `1.0.0-postgres`… Varsayılan `latest`. |
| `STATUS_DOMAIN` | İsteğe bağlı: durum sayfası için ayrı alan adı ([ayrıntı](/docs/kurulum/caddy/#durum-alan-adi)). |
| `TZ`, `LOG_LEVEL`, `MAX_CONCURRENT_CHECKS` | Yukarıdaki tabloyla aynı. |
