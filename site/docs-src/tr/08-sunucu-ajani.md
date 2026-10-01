---
id: agent
title: Sunucu ajanı ve kontrol noktası
nav: Sunucu ajanı ve kontrol noktası
description: Sunucularınızın CPU, RAM, disk ve ağ ölçümlerini toplayan ajanı ve monitörleri başka konumlardan kontrol eden kontrol noktasını kurun, güncelleyin ve kaldırın.
section: Kullanım
order: 8
slug: sunucu-ajani
---

Bekci'nin programı başka sunucularda iki ayrı görevle çalışabilir. İkisi panelde ayrı kayıtlardır ve birbirinin ekranında görünmez:

| | Sunucu ajanı | Kontrol noktası |
|---|---|---|
| Ne yapar? | Kurulduğu sunucunun CPU, RAM, disk, ağ, sıcaklık ve Docker konteyner ölçümlerini dakikada bir gönderir | Kendisine atanan monitörleri kendi konumundan kontrol eder ve sonuçları gönderir |
| Panelde | **Sunucular → Sunucu ekle** | **Ayarlar → Kontrol noktaları → Yeni kontrol noktası** |
| Kurulum yolları | Docker, Linux (systemd), Windows | Docker |
| Desteklenen sistemler | Linux amd64 ve arm64, Windows amd64 | Linux amd64 ve arm64 |

Ajan veritabanı kullanmaz. Ana sunucuya (Bekci panelinize) ulaşamazsa sonuçları bekletir ve bağlantı gelince gönderir.

## Başlamadan önce {#once}

- **`BASE_URL` doğru olmalı.** Kurulum komutu, ajanın bağlanacağı adresi `BASE_URL`'den alır. Panel `https://bekci.ornek.com` adresindeyse `BASE_URL` de bu olmalı ([nasıl ayarlanır](/docs/ortam-degiskenleri/)).
- **HTTPS önerilir.** Ajan varsayılan olarak şifrelenmemiş (`http://`) adrese bağlanmaz; panel `http://` ise kurulum komutu bu izni (`PROBE_ALLOW_INSECURE=1`) kendisi ekler, ama token ağda şifresiz gider.
- **Ajanın kurulacağı sunucu, panelinizin adresine erişebilmeli** (giden HTTPS bağlantısı yeterli; ajan için gelen port açmanız gerekmez).

## Sunucu ajanını kurun {#sunucu-ajani}

1. Panelde **Sunucular** sayfasında **Sunucu ekle** düğmesine basın.
2. **Sunucu adı** alanına listede ve bildirimlerde görünecek bir ad yazın (ör. “Web sunucusu İstanbul”) ve **Oluştur** düğmesine basın.
3. **Kurulum yöntemi** olarak **Docker**, **Doğrudan (systemd)** ya da **Windows** sekmesini seçin ve komutu kopyalayın.
4. Komutu izlemek istediğiniz sunucuda aşağıdaki gibi çalıştırın.

> [!IMPORTANT] Token yalnızca bir kez gösterilir
> Komutun içindeki token bu sunucuya özel anahtardır ve pencereyi kapattıktan sonra bir daha gösterilmez. Kaybederseniz sunucunun sayfasından yeni bir komut alabilirsiniz (eski token geçersiz olur).

:::tabs key=ajan-os label="Kurulum yöntemi"
@tab Docker
Docker kurulu Linux sunucular için. Komut çok satırlıdır ve `sh <<'UPTIME_KURULUM'` satırıyla başlar. Terminale **root** olarak bağlanıp komutun **tamamını** yapıştırın. Root değilseniz komutun ilk satırını `sudo sh <<'UPTIME_KURULUM'` olarak değiştirin.

Komut şunları yapar:

- `/etc/uptime-agent.env` dosyasını yalnızca root'un okuyabileceği izinle (600) oluşturur ve token'ı buraya yazar. Token hiçbir komut satırında (`ps` çıktısında) görünmez.
- `uptime-agent` adlı bir konteyner başlatır. Konteyner sunucunun disklerini ve `/proc`, `/sys` bilgilerini **salt okunur** görür, tüm Linux yetkileri düşürülmüştür, bellek 256 MB ile sınırlıdır.
- Programı bir kez indirir, komuttaki **SHA-256** özetiyle doğrular ve `uptime-agent-bin` biriminde saklar.

> [!CHECK]
> `docker logs uptime-agent` çıktısında şu satırları görmelisiniz:
>
> ```text
> /opt/uptime/uptime.dl: OK
> level=INFO msg="kontrol noktası başladı" sunucu=https://bekci.ornek.com sürüm=…
> level=INFO msg="sunucu metrikleri gönderiliyor" aralik=60
> ```
>
> İlk satır programın SHA-256 doğrulamasından geçtiğini gösterir. (“kontrol noktası başladı” mesajı iki görevde de aynıdır.)
@tab Linux (systemd)
Docker kullanmayan Linux sunucular için (**Doğrudan (systemd)** sekmesi). Terminale **root** olarak bağlanıp komutun **tamamını** yapıştırın; root değilseniz ilk satırı `sudo sh <<'UPTIME_KURULUM'` yapın.

Komut şunları yapar:

- Sunucunun mimarisine uygun programı (`x86_64` ya da `aarch64`) indirir, SHA-256 ile doğrular ve `/usr/local/bin/uptime` olarak kaydeder.
- Token'ı `/etc/uptime-agent.env` dosyasına yalnızca root'un okuyabileceği izinle yazar.
- `uptime-agent` adında bir systemd hizmeti (`/etc/systemd/system/uptime-agent.service`) oluşturur, açılışta başlayacak şekilde etkinleştirir ve başlatır.

> [!CHECK]
> Komut `Bekci ajanı kuruldu ve başlatıldı (durum: systemctl status uptime-agent)` satırıyla bitmeli.
>
> ```bash
> systemctl status uptime-agent      # "active (running)" görmelisiniz
> journalctl -u uptime-agent -n 20   # son günlük satırları
> ```
@tab Windows
Windows sunucular için (Windows amd64).

1. **Başlat** menüsünde PowerShell'i arayın, sağ tıklayıp **Yönetici olarak çalıştır**'ı seçin.
2. Paneldeki **Windows** sekmesinden kopyaladığınız tek satırlık komutu yapıştırıp <kbd>Enter</kbd>'a basın.

Komut şunları yapar:

- Programı indirir, SHA-256 ile doğrular ve `C:\Program Files\Uptime\uptime.exe` olarak kurar.
- Token'ı `C:\Program Files\Uptime\agent.env` dosyasına yazar. Bu klasöre yalnızca SYSTEM ve Administrators erişebilir.
- `uptime-agent` adında, Windows açılınca otomatik başlayan ve hata olursa kendini yeniden başlatan bir hizmet kurar.

> [!CHECK]
> Komut hata vermeden biter. **Hizmetler** (services.msc) listesinde `uptime-agent` hizmeti **Çalışıyor** durumunda görünür. Günlük dosyası: `C:\Program Files\Uptime\agent.log`. Başlangıç hataları Windows **Olay Görüntüleyicisi**'nde de görünür (Uygulama günlüğü, kaynak `uptime-agent`).

Windows'ta yük ortalaması yaklaşık bir değerdir (işlemci kuyruğundan hesaplanır); sıcaklık ve Docker konteynerleri toplanmaz.
:::

> [!CHECK]
> Paneldeki kurulum penceresi bir iki dakika içinde **Bağlantı bekleniyor…** durumundan **Bağlandı ✓** durumuna geçer. **Sunucuya git** ile sunucunun sayfasını açın; grafikler dolmaya başlar.

## Kontrol noktası kurun {#kontrol-noktasi}

Kontrol noktası, monitörlerinizi başka bir şehirden ya da ağdan da kontrol eder. Böylece bir kesintinin yalnızca bir konumdan mı yoksa her yerden mi görüldüğünü anlarsınız.

1. **Ayarlar → Kontrol noktaları** bölümünde **Yeni kontrol noktası** düğmesine basın.
2. Konumu anlatan kısa bir ad verin (ör. “Frankfurt”) ve kaydedin.
3. Gösterilen komutu, kontrol noktası olacak sunucuda (Docker kurulu olmalı) **root** olarak, tamamını yapıştırarak çalıştırın. Komut `/etc/uptime-probe.env` dosyasını ve `uptime-probe` adlı konteyneri oluşturur.
4. **Kopyaladım, kapat** ile pencereyi kapatın.
5. Bir monitörün düzenleme sayfasında **Konumlar** bölümünden bu kontrol noktasını seçin ve **Kesinti kuralını** belirleyin: herhangi bir konum, konumların çoğunluğu ya da tüm konumlar çalışmıyorsa. İsterseniz **Konum kesintisinde de bildirim gönder** seçeneğini açın: monitör genel olarak çalışırken tek bir konum düşer ya da ulaşılamaz olursa 🟡 başlıklı ayrı bir bildirim gider, konum düzelince 🟢 ile bildirilir; uptime etkilenmez (varsayılan kapalı). Konum kesintileri **Olaylar** sayfasında “Konum kesintisi” türüyle ayrı listelenir.

> [!CHECK]
> Birkaç saniye içinde kontrol noktası listede **Çevrimiçi** görünür. Son 90 saniyede sonuç gönderen kontrol noktası çevrimiçi sayılır; bağlantısı kopan (durdurulan, çöken) nokta anında çevrimdışı görünür. `docker logs uptime-probe` çıktısında `msg="kontrol noktası başladı"` satırı olmalı.

### Çevrimdışı bildirimi {#kontrol-noktasi-bildirim}

Kontrol noktası düşünce haberiniz olsun diye satır menüsünden **Düzenle** → **Çevrimdışı kalınca bildir** seçeneğini açıp bildirim kanallarını seçin. Kontrol noktası 90 saniye boyunca hiç istek göndermezse “🔴 *Ad*: kontrol noktasına ulaşılamıyor” bildirimi gider ve **Olaylar** sayfasında “Kontrol noktası çevrimdışı” türünde bir olay açılır; tekrar bağlanınca “🟢 … tekrar çevrimiçi” bildirimiyle olay kapanır ve kesinti süresi yazılır. 90 saniyelik tolerans sayesinde ajanın kısa bir yeniden başlatması bildirim üretmez. Kontrol noktasını siz devre dışı bırakırsanız bildirim gitmez, açık olay sessizce kapanır. Bu bildirimler monitörlerin kendi kanallarından bağımsızdır: kontrol noktasına seçtiğiniz kanallara gider.

## Kurulumun oluşturduğu dosyalar {#dosyalar}

| Kurulum | Program | Token (ayar dosyası) | Hizmet / konteyner |
|---|---|---|---|
| Sunucu ajanı, Docker | `uptime-agent-bin` birimi | `/etc/uptime-agent.env` | `uptime-agent` konteyneri |
| Sunucu ajanı, systemd | `/usr/local/bin/uptime` | `/etc/uptime-agent.env` | `uptime-agent` hizmeti |
| Sunucu ajanı, Windows | `C:\Program Files\Uptime\uptime.exe` | `C:\Program Files\Uptime\agent.env` | `uptime-agent` hizmeti |
| Kontrol noktası, Docker | `uptime-probe-bin` birimi | `/etc/uptime-probe.env` | `uptime-probe` konteyneri |

## Güncelleme {#guncelleme}

Ajan kendini **güncellemez**: program bir kez indirilir, doğrulanır ve sonraki yeniden başlatmalarda aynı program kullanılır. Böylece panelinizin bulunduğu sunucu ele geçirilse bile sunucularınıza kendiliğinden yeni bir program inmez. Bekci'yi güncelledikten sonra ajanları da güncellemek için:

> [!IMPORTANT]
> Bekci 1.2 ile gelen **eşzamanlı konum kontrolü** (tüm konumlar aynı anda kontrol eder) ve tanınabilir **User-Agent** ajan tarafında da yeni sürümü gerektirir. 1.2 öncesi kurulmuş kontrol noktalarını aşağıdaki adımlarla yeniden kurun; eski ajanlar çalışmaya devam eder ama kendi zamanlamasıyla ve kendi User-Agent'ıyla kontrol eder. Panel, sürümü kendisinden farklı ajanları listede **Eski sürüm** rozetiyle gösterir.

1. Panelden **güncel** kurulum komutunu alın. Token yalnızca bir kez gösterildiği için bu, token'ı yeniler (eski token hemen geçersiz olur):
   - Sunucu ajanı: sunucunun sayfasında **Kurulum komutu** → **Token’ı yenile ve komutu göster**.
   - Kontrol noktası: **Ayarlar → Kontrol noktaları** listesinde satırın menüsünden **Token’ı yenile**.
2. Docker kurulumlarında önce eskisini kaldırın:

   ```bash
   docker rm -f uptime-agent; docker volume rm uptime-agent-bin     # sunucu ajanı
   docker rm -f uptime-probe; docker volume rm uptime-probe-bin     # kontrol noktası
   ```

3. Yeni komutu çalıştırın. systemd ve Windows kurulumlarında eskisini kaldırmanız gerekmez; komut programı ve hizmeti yeniler.

## Kaldırma {#kaldirma}

Önce panelden sunucuyu ya da kontrol noktasını silin (token geçersiz olur). Sonra ajanın kurulu olduğu sunucuda:

:::tabs label="Kaldırılacak kurulum"
@tab Docker — sunucu ajanı
```bash
docker rm -f uptime-agent
docker volume rm uptime-agent-bin
rm -f /etc/uptime-agent.env
```
@tab Docker — kontrol noktası
```bash
docker rm -f uptime-probe
docker volume rm uptime-probe-bin
rm -f /etc/uptime-probe.env
```
@tab Linux (systemd)
```bash
systemctl disable --now uptime-agent
rm -f /etc/systemd/system/uptime-agent.service /usr/local/bin/uptime /etc/uptime-agent.env
systemctl daemon-reload
```
@tab Windows
Yönetici olarak açılmış PowerShell'de:

```powershell
& "$env:ProgramFiles\Uptime\uptime.exe" service uninstall
Remove-Item -Recurse -Force "$env:ProgramFiles\Uptime"
```

İlk satır hizmeti durdurup siler, ayar dosyasını (token) ve Olay Günlüğü kaynağını kaldırır; ikinci satır programı ve günlükleri siler.
:::

## IP kilidi {#ip-kilidi}

Yeni ajanlarda **IP'ye kilitle** seçeneği varsayılan olarak açıktır: ajanın ilk bağlandığı IP adresi kaydedilir (IPv4'te tam adres, IPv6'da /64 blok) ve bundan sonra başka bir adresten gelen istekler reddedilir. Token çalınsa bile başka bir makineden kullanılamaz.

Sunucunun IP adresi değişirse (ör. sunucuyu taşıdınız) kilidi sıfırlayın; bir sonraki bağlantı yeni adresi kaydeder:

- **Sunucu ajanı:** sunucunun sayfasında **Ayarlar** → **Kilidi sıfırla**.
- **Kontrol noktası:** **Ayarlar → Kontrol noktaları** listesinde satırın menüsünden **Düzenle** → **Kilidi sıfırla**.

Kilidi aynı yerden tamamen kapatabilirsiniz (**IP'ye kilitle** anahtarı). Token'ın sızdığından şüpheleniyorsanız token'ı yenileyip kurulum komutunu yeniden çalıştırın ([güncelleme](#guncelleme) adımları).

## Sorun giderme {#sorun-giderme}

Günlükler: Docker'da `docker logs uptime-agent` (kontrol noktası için `uptime-probe`), systemd'de `journalctl -u uptime-agent`, Windows'ta `C:\Program Files\Uptime\agent.log`.

| Günlükte ya da panelde gördüğünüz | Nedeni ve çözümü |
|---|---|
| `ana sunucu isteği reddetti: ajan devre dışı veya başka bir IP'ye kilitli` (durum 403) | Sunucunun IP'si değişti ya da ajan panelde devre dışı. [IP kilidini sıfırlayın](#ip-kilidi) ya da ajanı yeniden etkinleştirin. |
| `ana sunucu token'ı reddetti` (durum 401) | Token geçersiz (sunucu silindi ya da token yenilendi). Panelden yeni komut alıp çalıştırın. |
| `Program özeti uyuşmuyor: kurulum komutunu panelden yenileyin` | Komut eski bir sürümden kalma; Bekci güncellenmiş. Panelden güncel komutu alın; Docker'da önce konteyneri ve birimi silin ([güncelleme](#guncelleme)). |
| Panelde “Ajan host metriklerini okuyamıyor” | Ajan Docker'da, sunucunun `/proc` ve `/sys` bağlamaları olmadan çalışıyor. Komutu panelden olduğu gibi, eksiksiz kullanın. |
| `PROBE_SERVER https olmalı; şifrelenmemiş http için PROBE_ALLOW_INSECURE=1 gerekir` | Panel `http://` adresinde. Paneli HTTPS arkasına alın ya da ayar dosyasına `PROBE_ALLOW_INSECURE=1` ekleyin. |
| `ana sunucuya ulaşılamıyor, tekrar denenecek` | Ajan panelin adresine erişemiyor. `BASE_URL`'i, DNS'i ve ajanın sunucusundan dışarı çıkan bağlantıları kontrol edin (ör. `curl -sI https://bekci.ornek.com/healthz`). |
| Panelde “Docker bulunamadı veya ajan Docker soketine erişemiyor” | Konteyner bilgisi toplanmaz; Docker yoksa bu normaldir. Docker varsa komuttaki `/var/run/docker.sock` bağlamasının yerinde olduğundan emin olun. |
| Olayda ya da bildirimde “Kontrol noktasına ulaşılamıyor” | O konumdan sonuç gelmiyor (ajan durdu, ağ kesildi). Konum kesinti hesabına katılmaz; son sonucu “çalışmıyor” olan konum, sonuç eskiyene kadar kesinti saymaya devam eder (ajanın yeniden başlaması olayı kapatıp açmaz). Ajanı ve panele erişimini kontrol edin. |
| Bildirimde “🔴 … sunucuya ulaşılamıyor”, olay listesinde “Sunucuya ulaşılamıyor” | Sunucu ajanından 3 dakikadır veri gelmiyor (çevrimdışı uyarısı). Ajan konteynerinin/hizmetinin çalıştığını ve panele bağlanabildiğini kontrol edin; veri gelince “🟢 … tekrar veri gönderiyor” bildirimi gider ve olay kapanır. |
