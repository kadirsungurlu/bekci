---
id: docker
title: Docker ile tek komutla kurulum
nav: Docker (tek komut)
description: Bekci'yi tek bir docker run komutuyla başlatın, tarayıcıdan açın ve yönetici hesabınızı oluşturun. Denemek için en kısa yol.
section: Kurulum
order: 2
slug: kurulum/docker
---

Bu rehberin sonunda Bekci sunucunuzda çalışıyor olacak ve paneli `http://SUNUCU-IP:8080` adresinden açabileceksiniz. Toplam süre: yaklaşık 3 dakika.

> [!TIP]
> Bu yol denemek için idealdir. Kalıcı bir kurulum için [Docker Compose](/docs/kurulum/docker-compose/) (ayarlar bir dosyada durur, güncellemesi kolaydır) ya da alan adınızla [HTTPS'li kurulum](/docs/kurulum/caddy/) önerilir.

## 1. Docker'ı kurun {#docker-kurun}

Önce sunucunuzda Docker olup olmadığına bakın:

```bash
docker --version
```

Bir sürüm numarası görüyorsanız (ör. `Docker version 29.x`) bu adımı atlayın. `command not found` görüyorsanız Docker'ı resmi kurulum betiğiyle kurun:

```bash
curl -fsSL https://get.docker.com -o get-docker.sh
sh get-docker.sh
```

Bu betik Docker'ın kendi hazırladığı betiktir ([get.docker.com](https://get.docker.com)); Ubuntu, Debian, Fedora, CentOS/RHEL gibi yaygın dağıtımlarda Docker Engine'i ve Compose eklentisini kurar. Dağıtımınızın paket deposuyla kurmayı tercih ederseniz [Docker'ın kurulum belgelerini](https://docs.docker.com/engine/install/) izleyin.

> [!CHECK]
> Şu komut kısa bir “Hello from Docker!” mesajı yazdırmalı:
>
> ```bash
> docker run --rm hello-world
> ```

## 2. Bekci'yi başlatın {#baslatin}

Aşağıdaki komutun tamamını kopyalayıp sunucunuzda çalıştırın:

```bash
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 \
  -v bekci-data:/data \
  kadirsungurlu/bekci:latest
```

Komutun parçaları ne işe yarar:

| Parça | Anlamı |
|---|---|
| `-d` | Konteyner arka planda çalışsın; terminali kapatsanız da durmaz. |
| `--name bekci` | Konteynerin adı. Sonraki komutlarda bu adı kullanacaksınız. |
| `--restart unless-stopped` | Sunucu yeniden başlarsa ya da Bekci çökerse otomatik yeniden başlasın. |
| `-p 8080:8080` | Sunucunun 8080 portunu Bekci'ye yönlendirir. |
| `-v bekci-data:/data` | Veriler `bekci-data` adlı Docker biriminde saklanır; konteyneri silseniz de kaybolmaz. |
| `kadirsungurlu/bekci:latest` | Docker Hub'daki Bekci imajı (amd64 ve arm64). |

> [!CHECK]
> Yaklaşık 10 saniye sonra `docker ps` komutunu çalıştırın. `bekci` satırının **STATUS** sütununda `Up 12 seconds (healthy)` gibi, sonunda **(healthy)** olan bir değer görmelisiniz. `(health: starting)` görüyorsanız birkaç saniye daha bekleyin.
>
> Sunucunun içinden sağlık kontrolü de `ok` döndürmeli:
>
> ```bash
> curl http://localhost:8080/healthz
> ```

## 3. Paneli açın ve yönetici hesabını oluşturun {#yonetici}

1. Tarayıcınızda `http://⟦SUNUCU-IP⟧:8080` adresini açın. Sunucunuzun IP adresini barındırma sağlayıcınızın panelinde görebilirsiniz; sunucuda `hostname -I` komutunun yazdığı ilk adres de genellikle budur.
2. **Hoş geldiniz** ekranında bir **kullanıcı adı** (3–32 karakter: harf, rakam, nokta, tire, alt çizgi) ve en az 8 karakterlik bir **şifre** girin, şifreyi **Şifre (tekrar)** alanına bir kez daha yazın.
3. **Hesabı oluştur** düğmesine basın.

> [!CHECK]
> Monitör listesi açılır ve “İlk monitörünüzü ekleyin” yazısını görürsünüz. Devamı için [İlk adımlar](/docs/ilk-adimlar/) sayfasına geçin.

> [!WARNING] Bu adres şifrelenmemiştir
> `http://` ile açılan panelde şifreniz ağ üzerinden şifrelenmeden gider. Denemek için sorun değildir; kalıcı kullanım için paneli bir alan adıyla HTTPS arkasına alın: [HTTPS'li kurulum (Caddy)](/docs/kurulum/caddy/) ya da [ters vekil arkasında](/docs/kurulum/ters-vekil/).

## 4. Güvenlik duvarını kontrol edin {#guvenlik-duvari}

Panel açılmıyorsa büyük olasılıkla 8080 portu kapalıdır.

- **Barındırma sağlayıcınızın güvenlik duvarı** (Hetzner, DigitalOcean, AWS “security group” vb.): 8080/TCP için gelen bağlantılara izin verin.
- **ufw:** Docker, yayınladığı portlar için kuralları kendisi ekler; bu yüzden `-p 8080:8080` ile yayınlanan port ufw kurallarından bağımsız olarak dışarıya açıktır. Tersini isterseniz, yani paneli yalnızca sunucunun kendisinden erişilebilir yapmak için, komuttaki `-p 8080:8080` kısmını `-p 127.0.0.1:8080:8080` olarak değiştirin.

> [!TIP] Portu açmadan denemek
> Paneli internete açmadan denemek için komutu `-p 127.0.0.1:8080:8080` ile çalıştırın ve kendi bilgisayarınızdan bir SSH tüneli kurun:
>
> ```bash
> ssh -L 8080:localhost:8080 ⟦root@SUNUCU-IP⟧
> ```
>
> Tünel açıkken kendi bilgisayarınızda `http://localhost:8080` adresini açın.

## 5. BASE_URL'i ayarlayın {#base-url}

Bekci, bildirimlerdeki bağlantılar ve sunucu ajanı kurulum komutları için panelin dışarıdan açıldığı adresi bilmek ister. Bunu `BASE_URL` ortam değişkeniyle verirsiniz. Ortam değişkenleri konteyner oluşturulurken verildiği için konteyneri yeniden oluşturmanız gerekir; verileriniz birimde durduğu için kaybolmaz:

```bash
docker stop bekci && docker rm bekci
docker run -d --name bekci --restart unless-stopped \
  -p 8080:8080 \
  -v bekci-data:/data \
  -e BASE_URL=http://⟦SUNUCU-IP⟧:8080 \
  -e TZ=Europe/Istanbul \
  kadirsungurlu/bekci:latest
```

`TZ` saat dilimidir (günlük özetler ve gece yedeğinin saati için). Tüm seçenekler: [Ortam değişkenleri](/docs/ortam-degiskenleri/).

> [!CHECK]
> `docker exec bekci printenv BASE_URL` komutu yazdığınız adresi göstermeli ve panele aynı hesapla giriş yapabilmelisiniz.

## Verileriniz nerede? {#veriler}

Tüm veriler (veritabanı ve otomatik yedekler) `bekci-data` adlı Docker biriminde, konteynerin içindeki `/data` klasöründedir:

```bash
docker exec bekci ls -la /data
```

Konteyneri silmek (`docker rm bekci`) verileri silmez. Birimi silmek ise **tüm verileri kalıcı olarak siler**; bunu yalnızca Bekci'yi tamamen kaldırırken yapın. Yedekleme için: [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/).

## Günlük işler {#gunluk-isler}

```bash
docker stop bekci          # durdur
docker start bekci         # başlat
docker restart bekci       # yeniden başlat
docker logs -f bekci       # günlükleri canlı izle (çıkmak için Ctrl+C)
docker logs --tail 50 bekci
```

Sağlıklı bir açılışın son satırı şöyle görünür:

```text
level=INFO msg="Bekci başladı" sürüm=… adres=:8080 veri=/data saat_dilimi=Europe/Istanbul
```

### Güncelleme {#guncelleme}

Yeni imajı indirip konteyneri aynı komutla yeniden oluşturun:

```bash
docker pull kadirsungurlu/bekci:latest
docker stop bekci && docker rm bekci
```

Ardından [5. adımdaki](#base-url) `docker run` komutunu yeniden çalıştırın. Güncellemeyi tek komuta indirmek için [Docker Compose](/docs/kurulum/docker-compose/) kullanmanızı öneririz. Güncellemeden önce yedek almayı unutmayın: [Güncelleme, yedek ve geri dönüş](/docs/guncelleme-yedek/).

## Sık karşılaşılan sorunlar {#sorunlar}

### “port is already allocated” ya da “address already in use” {#port-dolu}

8080 portunu başka bir program kullanıyor. Başka bir konteynerse hangisi olduğunu `docker ps --filter publish=8080` gösterir. **Coolify kurulu sunucularda Coolify'ın vekili de 8080 portunu kullanır.** Başka bir port seçin, ör. `-p 8081:8080`, ve paneli `http://SUNUCU-IP:8081` adresinden açın. Coolify kullanıyorsanız [Coolify rehberi](/docs/kurulum/coolify/) daha uygundur.

### Tarayıcı “bağlantı zaman aşımına uğradı” diyor {#zaman-asimi}

8080 portu sağlayıcınızın güvenlik duvarında kapalı olabilir; [4. adıma](#guvenlik-duvari) bakın.

### Konteyner sürekli yeniden başlıyor {#yeniden-basliyor}

`docker logs bekci` çıktısının son satırlarındaki `hata:` ile başlayan mesaj nedeni söyler. Çözümler için [Sorun giderme](/docs/sorun-giderme/).

## Kaldırma {#kaldirma}

```bash
docker stop bekci && docker rm bekci
docker volume rm bekci-data    # DİKKAT: tüm verileri kalıcı olarak siler
```
