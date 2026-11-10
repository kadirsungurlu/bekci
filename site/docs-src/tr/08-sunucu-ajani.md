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
> Paneldeki kurulum penceresi bir iki dakika içinde **Bağlantı bekleniyor…** durumundan **Bağlandı** durumuna geçer. **Sunucuya git** ile sunucunun sayfasını açın; grafikler dolmaya başlar.

## Uyarı kuralları {#uyari-kurallari}

Sunucu sayfasındaki **Uyarı kuralları** bölümünde hangi durumda bildirim gideceğini belirlersiniz. Yeni sunucuya varsayılan kurallar gelir: 3 dk veri gelmezse çevrimdışı, CPU ve RAM 10 dk ortalaması %90, en dolu disk %85 ve yeniden başlatma. Her kural tek başına açılıp kapatılabilir; kuralı olmayan sunucu için hiç uyarı gönderilmez.

**Uyarı ve kritik eşik.** CPU, RAM, swap, yük, sıcaklık, ağ ve disk kurallarına isteğe bağlı bir **uyarı** eşiği ekleyebilirsiniz (kritik eşikten küçük olmalı). Değer uyarı eşiğini aşınca *uyarı*, kritik eşiği aşınca *kritik* bildirimi gider; seviye düşünce “kritikten uyarıya indi” notu, eşiğin altına inince çözüldü bildirimi gelir. Olay tek kayıt olarak sürer; **Olaylar** sayfasında seviye değişimleri zaman çizelgesinde görünür. Sunucunun **Bildirim kanalları** bölümünde her kanala bir seviye seçebilirsiniz: *hepsi*, *yalnızca uyarı* ya da *yalnızca kritik* — ör. Slack her şeyi alsın, telefon yalnızca kritikte çalsın. Çözüldü bildirimi, olayın ulaştığı en yüksek seviyeyi alan kanallara gider.

**Konteyner alarmı.** *Konteyner* metriği bir Docker konteyneri çalışmayı bırakınca bildirir. Ad kutusunu boş bırakırsanız sunucuda daha önce görülen **herhangi bir** konteynerin durması alarmı tetikler; belirli bir ad yazarsanız (sunucudaki adlar listede önerilir) yalnızca o konteyner izlenir ve ad artık hiç görünmüyorsa da (silinmişse) alarm açılır. Süre, konteynerin kaç dakika çalışmıyor sayılacağını belirler; ajan yeniden başlatılan konteyneri saniyeler içinde “çalışıyor” görürse alarm açılmaz. Docker verisi gelmeyen sunucularda (Windows, Docker soketi bağlanmamış ajan) bu kural sessiz kalır, yanlış alarm üretmez. Durmuş konteynerler sunucu sayfasındaki konteyner tablosunda **durdu** rozetiyle listelenir; bu sürümle gelen ajan durmuş konteynerleri de bildirir; eski sürüm ajanlarla yalnızca çalışanlar görünür ve kural, ad kaybolunca tetiklenir (bkz. [güncelleme](#guncelleme)).

**Yeniden başlatma tespiti.** *Yeniden başlatma* kuralı sunucunun açılış zamanı ileri kayınca (en az 60 saniye) “*Ad* yeniden başlatıldı” bildirimi gönderir ve sunucu sayfasının **Olaylar** listesine açılış zamanıyla birlikte bir satır ekler. Bu anlık bir bildirimdir, açık olay oluşturmaz ve çözüldü mesajı gelmez. Bildirim istemiyor ama kaydı görmek istiyorsanız kuralı kapatın: yeniden başlatma yine listeye yazılır.

### Sunucu bakımı {#sunucu-bakimi}

Planlı bir yeniden başlatma ya da güncelleme öncesinde **Bakım pencereleri** sayfasında bir pencere açıp **Etkilenen sunucular** bölümünden sunucuları (ya da *Tüm sunucular*) seçin; aynı pencerede monitör seçmek zorunlu değildir. Pencere sürerken seçili sunucularda yeni uyarı açılmaz, çevrimdışı ve yeniden başlatma bildirimi gitmez; sunucu listesinde ve ayrıntı sayfasında **Bakımda** rozeti görünür. Daha önce açılmış bir uyarı bakımda da çözülebilir (bildirimi gider). Pencere bitince izleme kaldığı yerden devam eder; o sırada hâlâ eşiğin üstünde olan metrikler normal süre dolunca uyarı açar.

## Kontrol noktası kurun {#kontrol-noktasi}

Kontrol noktası, monitörlerinizi başka bir şehirden ya da ağdan da kontrol eder. Böylece bir kesintinin yalnızca bir konumdan mı yoksa her yerden mi görüldüğünü anlarsınız.

1. **Ayarlar → Kontrol noktaları** bölümünde **Yeni kontrol noktası** düğmesine basın.
2. Konumu anlatan kısa bir ad verin (ör. “Frankfurt”) ve kaydedin.
3. Gösterilen komutu, kontrol noktası olacak sunucuda (Docker kurulu olmalı) **root** olarak, tamamını yapıştırarak çalıştırın. Komut `/etc/uptime-probe.env` dosyasını ve `uptime-probe` adlı konteyneri oluşturur.
4. **Kopyaladım, kapat** ile pencereyi kapatın.
5. Bir monitörün düzenleme sayfasında **Konumlar** bölümünden bu kontrol noktasını seçin ve **Kesinti kuralını** belirleyin: herhangi bir konum, konumların çoğunluğu ya da **Kısmi erişim ayrımı** (yalnız tüm konumlar çalışmıyorsa kesinti). **Kısmi erişim kesintisinde de bildirim gönder** açıksa monitör çalışırken bir konum düşer ya da ulaşılamaz olursa ayrı bir bildirim gider, konum düzelince de bildirilir; uptime etkilenmez. Yeni monitörde ikinci konumu seçtiğiniz anda kural **Kısmi erişim ayrımı**na geçer ve bu bildirim açılır; kaydetmeden önce değiştirebilirsiniz. Ayrıntı: [Kısmi erişim kesintisi](#kismi-erisim).

> [!CHECK]
> Birkaç saniye içinde kontrol noktası listede **Çevrimiçi** görünür. Kontrol noktası panele her **panel haberleşme aralığında** (varsayılan 5 sn) bir kalp atışı gönderir; art arda **kopukluk eşiği** (varsayılan 3) kadar kalp atışı gelmezse, yani varsayılanla 15 saniye içinde, çevrimdışı sayılır. Bağlantısı kopan (durdurulan, çöken) nokta anında çevrimdışı görünür. Kalp atışı göndermeyen eski sürüm ajanlar için eski kural (son 90 saniyede istek) geçerlidir. `docker logs uptime-probe` çıktısında `msg="kontrol noktası başladı"` satırı olmalı.

### Kontrol noktaları arası bağlantı (mesh) {#mesh}

Kontrol noktaları birbirine de bağlanır ve aradaki yolu ölçer: her çift arasında tek bir kalıcı TCP bağlantısı açılır, iki yön de bu bağlantı üzerinden **mesh ping aralığında** (varsayılan 5 sn) ping atar. Art arda kopukluk eşiği kadar yanıt alınamazsa o yön kopuk sayılır. Ölçümler her panel haberleşme aralığında toplu olarak panele raporlanır. Bağlantılar çift başına panelin ürettiği anahtarla imzalanır (HMAC-SHA256); çerçevelerde gizli bilgi yoktur, bu yüzden ayrıca TLS kullanılmaz. Kontrol noktasının token'ını yenilediğinizde ya da onu devre dışı bıraktığınızda anahtarları da yenilenir.

- **Port:** Ajan varsayılan olarak `7480/TCP` portunu dinler (`MESH_ADDR`, `-` dinlemeyi kapatır). Diğer kontrol noktalarının bu porta ulaşabilmesi için güvenlik duvarında açın.
- **NAT arkasındaki nokta:** Dinleyemeyen (dışarıdan erişilemeyen) nokta yine de dinleyen eşlere kendisi bağlanır ve ölçülür. Kontrol noktası düzenleme penceresindeki **Mesh adresi** alanına `-` yazarak bunu panele bildirin; iki ucu da dinlemeyen çift “ölçülemiyor” sayılır, kopuk sayılmaz.
- **Mesh adresi:** Boşsa eşler, kontrol noktasının panele bağlandığı IP'nin 7480 portuna bağlanır. Farklı bir adres ya da port gerekiyorsa `host:port` olarak yazın.
- **Ayarlar:** **Ayarlar > Kontrol noktaları** sayfasında mesh ping aralığı (1-60 sn), kopukluk eşiği (1-10 ardışık kayıp) ve panel haberleşme aralığı (1-60 sn) bulunur. Eşik ile aralığın çarpımı en az 3 saniye olmalıdır (tek paket kaybı alarm üretmesin). Her kontrol noktası düzenleme penceresinde bu değerleri kendisi için ezebilir; **Genel değeri kullan** seçiliyse genel ayar geçerlidir.

**Karar ve bildirim.** En az iki kontrol noktası kalp atışı gönderiyorsa panel tüm bağlantıları (ana sunucu dahil) birlikte yorumlar ve tek tek "çevrimdışı" bildirimleri yerine tek bir mesh bildirimi gönderir:

- **Ağ bölünmesi:** noktalar birbirine ulaşamayan en az iki gruba ayrıldı.
- **Genel erişim sorunu:** bir nokta en az iki bağlantısında ulaşılamıyor, diğerleri birbirine ulaşıyor (sorun o noktada).
- **Yol sorunu:** iki nokta birbirine ulaşamıyor ama başka noktalar üzerinden ulaşılıyorlar.
- **Tek yönlü bağlantı:** bir yön çalışıyor, ters yön kopuk.
- **Yetersiz gözlem:** bağlantı koptu ama yorum için yeterli ölçüm yok.

Karar bir panel aralığı beklenip kalıcıysa verilir (anlık dalgalanma bildirim üretmez). Bildirim mesaj sahibini (ana sunucu), tüm bağlantıların durumunu ve yorumu içerir; ilgili kontrol noktalarının **çevrimdışında bildir** seçili kanallarına gider. Olay **Olaylar** sayfasında yalnız yöneticilere görünür; bağlantılar düzelince olay kapanır ve düzelme bildirimi gider.

**Topoloji şeması.** **Ayarlar > Kontrol noktaları** sayfasında bağlantı matrisinin üstünde canlı bir şema vardır: ana sunucu tepede, kontrol noktaları çevresinde. Düz çizgi ve onay işareti çift yönlü erişimi, çarpı kopukluğu, ok yalnız çalışan yönü (tek yönlü bağlantı), kesikli gri çizgi ölçülemeyen bağlantıyı gösterir. Bir bağlantının üzerine gelince ya da klavyeyle odaklanınca iki yönün durumu, gecikmesi (RTT), ardışık kayıp sayısı ve son başarılı ölçüm zamanı görünür. Mesh olayının ayrıntı sayfasında aynı şema olay anındaki durumla çizilir. Telegram bildirimine şema PNG olarak eklenir (gönderilemezse yalnız metin gider), e-posta bildiriminde ise satır içi görüntü olarak yer alır; düz metin okuyan istemcilerde metin değişmeden okunur.

### Kısmi erişim kesintisi {#kismi-erisim}

Hedefe bazı konumlardan ulaşılıp bazılarından ulaşılamıyorsa (ör. hedefin güvenlik duvarı bir ülkeyi engelliyor) bu tam kesinti değildir. **Kısmi erişim ayrımı** kuralında:

- Tüm konumlar başarısızsa monitör **Çalışmıyor** olur, olağan kesinti bildirimi gider.
- Yalnız bir kısmı başarısızsa monitör **Çalışıyor** kalır, uptime etkilenmez; **Olaylar** sayfasında “Kısmi erişim kesintisi” kaydı açılır. Bildirim açıksa başlığı “<monitör> için kısmi erişim kesintisi” olur ve her konum için “Başarılı” ya da “Başarısız” satırı taşır.
- Kısmi erişim kararı için en az bir konumun, bağlantısı sağlam olarak “çalışıyor” demesi gerekir. Kontrol noktalarının hepsine ulaşılamıyorsa karar yalnız ana sunucunun sonucuna dayanır; bildirimde ve olay ayrıntısında bu durum not olarak yazılır.
- Bir kısmi erişim kesintisi sürerken onu “çalışıyor” diyen kontrol noktasıyla bağlantı koparsa, kesinti 3 kontrol aralığı daha bekletilir; bağlantı dönmezse monitör **Çalışmıyor** olur.

Ana sunucunun konum adı (varsayılan “Ana sunucu”) **Ayarlar → Genel** bölümünden değiştirilebilir; bildirimlerde ve olaylarda bu ad görünür.

Mevcut monitörlerin kuralı kendiliğinden değişmez. Birkaç monitörü yeni kurala geçirmek için her birinin düzenleme sayfasında kuralı seçin; çok sayıda monitör için API'yi kullanabilirsiniz (her monitör için `PUT /api/monitors/{id}/locations`, gövdede `down_when: "all"` ve `notify_partial: true`; seçili konumlar da `include_local` ve `probe_ids` ile birlikte gönderilmelidir). API anahtarı ve örnek istekler için [Entegrasyonlar](/docs/entegrasyonlar/) belgesine bakın.

### Çevrimdışı bildirimi {#kontrol-noktasi-bildirim}

Kontrol noktası düşünce haberiniz olsun diye satır menüsünden **Düzenle** → **Çevrimdışı kalınca bildir** seçeneğini açıp bildirim kanallarını seçin. Kontrol noktasından art arda kopukluk eşiği kadar kalp atışı gelmezse (varsayılan 3 x 5 sn = 15 saniye) “*Ad*: kontrol noktasına ulaşılamıyor” bildirimi gider ve **Olaylar** sayfasında “Kontrol noktası çevrimdışı” türünde bir olay açılır; tekrar bağlanınca “… tekrar çevrimiçi” bildirimiyle olay kapanır ve kesinti süresi yazılır. Bu süreden kısa bir yeniden başlatma bildirim üretmez; daha fazla tolerans isterseniz eşiği ya da aralığı artırın. Kontrol noktasını siz devre dışı bırakırsanız bildirim gitmez, açık olay sessizce kapanır. Bu bildirimler monitörlerin kendi kanallarından bağımsızdır: kontrol noktasına seçtiğiniz kanallara gider.

### İkinci hat (OOB) {#oob}

Kontrol noktasının sunucusunda ikinci bir ağ bağlantısı (OOB, yönetim hattı, yedek hat) varsa ajan, ana hattan panele ulaşamadığında panel bağlantısını bu hatta taşır. Böylece ana hat koptuğunda kontrol noktası “çevrimdışı” görünmez; panel hangi hatta olduğunu bilir ve size bildirir.

Yalnız ajanın **panele giden bağlantısı** (iş listesi, sonuçlar, kalp atışı, metrik, güncelleme) hat değiştirir. Ajanın yaptığı denetimler (HTTP, ping, DNS vb.) ve mesh bağlantıları her zaman işletim sisteminin yönlendirmesini izler.

**OOB IP'sini girin.** **Ayarlar → Kontrol noktaları** listesinde satırın menüsünden **Düzenle** → **OOB (ikinci hat)** bölümünde **OOB IP adresi** alanına ikinci hattın IP adresini, isterseniz **Hat adı** alanına bildirimlerde görünecek adı yazın (varsayılan “OOB”, en çok 40 karakter). IP alanını boşaltmak özelliği kapatır. Döngü (`127.0.0.1`, `::1`), çoklu yayın ve `0.0.0.0` gibi adresler kabul edilmez.

**IP kilidi.** [IP kilidi](#ip-kilidi) açıksa panel, kilitli ana IP'ye ek olarak OOB IP'sinden gelen istekleri de kabul eder. OOB IP'sinden gelen istek kilidi değiştirmez; kilit yalnız ana hattan gelen ilk istekle sabitlenir. OOB IP'si tam adres olarak karşılaştırılır (IPv6'da /64 blok değil).

**Arayüzün bulunması.** Ajan, panelin verdiği OOB IP'sini sunucunun ağ arayüzlerinde arar ve panel bağlantısını o arayüze bağlar (kaynak adres = OOB IP'si). OOB IP'si sunucuda değil de önündeki bir NAT cihazındaysa (genel IP) arayüz bulunamaz: panelde **OOB arayüzü bulunamadı** uyarısı görünür ve ajan OOB'ye geçmez. Bu durumda ajanın ayar dosyasına (`/etc/uptime-probe.env`) arayüz adını ekleyin, ör. `PROBE_OOB_IFACE=eth1`; o arayüzün panel adresiyle aynı aileden (IPv4/IPv6) adresi kaynak olarak kullanılır. Ajanı yeniden başlatın; Docker'da konteyneri yeniden oluşturun (`docker restart` ayar dosyasını yeniden okumaz).

**Linux.** Bağlantı `SO_BINDTODEVICE` ile arayüze ve OOB IP'sine bağlanır. Bunun için ajanın `CAP_NET_RAW` yetkisi (ya da root) gerekir; Docker kurulum komutu bu yetkiyi zaten verir (`--cap-add NET_RAW`). Yetki yoksa yalnız kaynak adres bağlanır; bu durumda paketlerin OOB arayüzünden çıkması için aşağıdaki policy routing kuralı **şarttır**. Yetki olsa da OOB arayüzünün kendi ağ geçidi yoksa (varsayılan rota yalnız ana hatta ise) aynı kuralı kurun:

```bash
# OOB arayüzü için ayrı yönlendirme tablosu (ör. eth1, ağ geçidi 10.20.0.1, OOB IP 10.20.0.15)
echo "100 oob" >> /etc/iproute2/rt_tables
ip route add default via 10.20.0.1 dev eth1 table oob
ip rule add from 10.20.0.15 table oob
# kontrol
ip rule
ip route show table oob
curl --interface eth1 -sI https://bekci.ornek.com/healthz
```

Bu komutlar yeniden başlatmada silinir. Kalıcı yapmak için dağıtımınızın ağ yapılandırmasına ekleyin (netplan'da `routing-policy` ve `routes` altında `table: 100`, NetworkManager'da `ipv4.routing-rules` ve `ipv4.routes ... table=100`, ifupdown'da arayüzün `post-up` satırları).

**Docker.** Konteyner varsayılan köprü ağında sunucunun OOB arayüzünü görmez. OOB kullanacak kontrol noktasını sunucunun ağında çalıştırın: kurulum komutundaki `-p 7480:7480` yerine `--network host` yazın (host ağında port yayınlamaya gerek yoktur; mesh doğrudan 7480 portunu dinler). Compose kullanıyorsanız servise `network_mode: host` ekleyin ve `ports:` satırlarını kaldırın.

**Windows.** Bağlantı `IP_UNICAST_IF` ile OOB arayüzüne bağlanır; ek yetki gerekmez. OOB arayüzünde ağ geçidi tanımlı olmalıdır (yoksa Windows paketi gönderemez). NAT durumunda `C:\Program Files\Uptime\agent.env` dosyasına arayüz adını (`PROBE_OOB_IFACE=Ethernet 2`) ekleyin.

**Geçiş.** Ajan şu iki koşul birlikte sağlanınca OOB'ye geçer:

- Ana hattan panele art arda **kopukluk eşiği** (Ayarlar → Kontrol noktaları, varsayılan 3) kadar istek bağlantı hatasıyla (bağlantı kurulamadı, zaman aşımı) sonuçlandı. Panelin `4xx`/`5xx` yanıtları sayılmaz; panel yanıt veriyorsa hat sağlamdır.
- OOB hattından panelin `GET /healthz` adresi başarılı yanıt verdi.

Mesh açıksa ve ajan ana hattan en az bir eşe hâlâ ulaşıyorsa geçilmez: sorun ana hatta değil panel tarafındadır. Geçiş anında açık bağlantılar kesilir ve OOB'den yeniden kurulur. Varsayılan ayarlarla geçiş, ana hat koptuktan yaklaşık kopukluk eşiği x panel haberleşme aralığı (3 x 5 sn = 15 sn) sonra olur. Günlükte `ana hattan Main'e ulaşılamıyor; OOB hattına geçildi` satırı görünür.

**Dönüş.** Ajan OOB'de en az **5 dakika** kaldıktan sonra ana hattan art arda **3 başarılı** `/healthz` alınca ana hatta döner (günlükte `ana hatta dönüldü`). OOB hattı da koparsa beklemeden, ana hattan tek başarılı `/healthz` yeter.

**Bildirimler.** Geçişte “*Ad* ana hatta erişemiyor, şu an *OOB adı* üzerinden bağlı”, dönüşte “*Ad* ana hatta döndü” bildirimi gider (dönüşte ikinci hatta geçen süre de yazılır). Bildirimler kontrol noktasının **Çevrimdışı kalınca bildir** kanallarına gider ([Çevrimdışı bildirimi](#kontrol-noktasi-bildirim)); sunucu ajanında sunucunun bildirim kanallarına. Kontrol noktaları listesindeki rozet etkin hattı (**Ana hat** ya da “*OOB adı* üzerinden”) ve geçiş zamanını gösterir.

**OOB'deyken denetim sonuçları.** Ajan OOB'deyken denetimleri yine ana hattan (bozuk yoldan) çıkar; bu yüzden o konumun sonuçları kesinti kararına katılmaz ve konum “bilinmiyor” sayılır (konum kartında not olarak yazılır). OOB'deki bir konum “Başarısız” dese de kısmi erişim kesintisi ya da kesinti açılmaz.

Ajan her istekte etkin hattı `X-Probe-Uplink` (`primary` ya da `oob`) ve arayüz durumunu `X-Probe-OOB-Iface` (`ok` ya da `missing`) başlığıyla panele bildirir; panelin önündeki ters vekil bu başlıkları silmemelidir.

## Kurulumun oluşturduğu dosyalar {#dosyalar}

| Kurulum | Program | Token (ayar dosyası) | Hizmet / konteyner |
|---|---|---|---|
| Sunucu ajanı, Docker | `uptime-agent-bin` birimi | `/etc/uptime-agent.env` | `uptime-agent` konteyneri |
| Sunucu ajanı, systemd | `/usr/local/bin/uptime` | `/etc/uptime-agent.env` | `uptime-agent` hizmeti |
| Sunucu ajanı, Windows | `C:\Program Files\Uptime\uptime.exe` | `C:\Program Files\Uptime\agent.env` | `uptime-agent` hizmeti |
| Kontrol noktası, Docker | `uptime-probe-bin` birimi | `/etc/uptime-probe.env` | `uptime-probe` konteyneri |

### DNS kesintisinde bağlantı {#dns-onbellek}

Ajan panelin alan adını çözdüğünde IP adreslerini program dizinindeki `uptime.dns.json` dosyasına yazar (yalnızca sahibi okuyabilir; dizini `PROBE_STATE_DIR` ile değiştirebilirsiniz). Ad saatte bir yeniden çözülür. DNS sunucusu yanıt vermezse ya da ad bulunamazsa ajan **son bilinen IP** ile bağlanmaya devam eder; bunun bir süre sınırı yoktur ve ajan bu sırada yeniden başlasa da dosyadaki IP'yi kullanır. DNS düzelince yeni IP hemen alınır.

- Yalnızca ajanın panele bağlantısı için geçerlidir; ajanın yaptığı kontroller (HTTP, DNS vb.) her zamanki gibi çözülür.
- Bağlantı yine panelin alan adıyla doğrulanır (TLS sertifikası ve SNI); eski IP'de başka bir sunucu varsa bağlantı reddedilir.
- Program dizini yazılabilir değilse önbellek yalnızca bellekte tutulur (ajan yeniden başlayınca boşalır).

## Sunucu envanteri {#envanter}

Ajan beş dakikada bir sunucunun envanterini toplar; içerik değişince, değişmese de saatte bir panele gönderir. Sunucu sayfasındaki **Envanter** sekmesinde arama ve süzgeçle görünür:

- **Servisler:** systemd birimleri, çalışma durumu, açılışta başlama ayarı.
- **Konteynerler:** ad, imaj, durum, sağlık, yeniden başlama sayısı. Satırı açınca son 24 saatin CPU ve bellek grafiği gelir.
- **Portlar:** dinleyen TCP/UDP portları ve süreci. Geçici UDP portları listelenmez.
- **Sertifikalar:** yerel dosyalar (`/etc/letsencrypt/live`, `/etc/ssl/private`, web sunucusu klasörleri, Caddy Docker birimi) ve dinleyen portlardan TLS el sıkışmasıyla okunan sertifikalar.
- **Disk bölümleri:** bağlama noktası, aygıt, dosya sistemi, boyut. Satırı açınca doluluk grafiği gelir.

Ortam değişkenleri ve özel anahtarlar **toplanmaz**. Komut satırlarındaki parola, belirteç ve bağlantı adresi parolaları ajanda, gönderilmeden önce maskelenir; panel kaydederken bir kez daha maskeler.

Sertifikanın bitmesine 14, 7 ve 1 gün kala olay açılır ve sunucunun bildirim kanallarına bildirim gider. Eşikleri **Ayarlar > Genel > SSL uyarı günleri** alanından değiştirebilirsiniz; boş bırakırsanız uyarı kapanır. Sertifika yenilenince olay kendiliğinden kapanır. Uyarı istemediğiniz sertifikayı Envanter sekmesinden **Yoksay** ile dışarıda bırakabilirsiniz.

| Değişken | Varsayılan | Görevi |
|---|---|---|
| `INVENTORY` | `1` | `0` envanteri bu ajanda kapatır. |
| `SECURITY` | `1` | `0` güvenlik raporunu (bkz. [Güvenlik](#guvenlik)) bu ajanda kapatır. |
| `PROBE_CERT_DIRS` | boş | Ek sertifika klasörleri (`:` ile ayrılır). |
| `PROBE_TLS_SCAN` | `on` | `off` dinleyen portlara bağlanmayı kapatır. |
| `PROBE_TLS_SNI` | boş | Port taramasında denenecek alan adları (virgülle ayrılır). |

Windows'ta şimdilik yalnız disk bölümleri toplanır; servis ve port listesi desteklenmez. Docker içinde çalışan ajan, sunucunun dosya sistemi bağlıysa (`HOST_ROOT`, `HOST_PROC`, `HOST_SYS`) servisleri ve portları görür. Eski sürüm ajanlar envanter göndermez; sekme bu durumda ajanı güncellemenizi söyler.

## Sunucu olayları ve disk tahmini {#olaylar}

Sunucu sayfasındaki **Olaylar** sekmesi şunları tarih sırasıyla gösterir: yeniden başlatma, bellek yetmediği için kapatılan süreç (OOM), çöken ya da düzelen servis, duran, yeniden başlayan, güncellenen konteyner, açılan ya da kapanan port, başlayan ve biten uyarılar. Kayıtlar bir yıl saklanır.

OOM için ajan `/proc/vmstat` sayacını izler; süreç adını `/dev/kmsg` satırından alır. Çekirdek günlüğü okunamıyorsa olay yine kaydedilir, süreç adı boş kalır. Servis, konteyner ve port olayları envanter turuyla (5 dakikada bir) yakalanır.

**Disk tahmini:** panel her bölümün son 7 günlük saatlik doluluğundan büyüme hızını hesaplar. Sunucu sayfasının Diskler tablosunda kalan gün ve günlük büyüme, Sunucular listesinde 30 günden kısa kalan bölüm için uyarı görünür. Tahmin için en az 12 saatlik veri gerekir; o zamana kadar "Veri toplanıyor" yazar. Doluluk artmıyorsa "Sabit" yazar.

İki uyarı kuralı her sunucuda varsayılan olarak açıktır, eşikleri sunucunun Ayarlar penceresinden değiştirilir:

- **Disk dolma tahmini:** bir bölüm 7 gün içinde dolacaksa uyarı olayı açılır ve bildirim gider. Kalan süre eşiğin 1,5 katını aşınca kapanır. Düzensiz büyümede (R² 0,5 altı) yeni uyarı açılmaz.
- **Hızlı disk dolumu:** bir bölüm son 60 dakikada 5 puan ya da daha çok dolarsa uyarır. Artış eşiğin %60'ının altına inince kapanır.

Eski panel `events` alanını yok sayar; eski ajan olay göndermez, envanterden gelen olaylar ve tahmin yine çalışır.

## Güvenlik {#guvenlik}

Sunucu sayfasındaki **Güvenlik** sekmesi ajanın 15 dakikada bir gönderdiği güvenlik raporunu gösterir: bekleyen güncellemeler, yeniden başlatma gereksinimi, başarısız giriş denemeleri, açık oturumlar, güvenlik duvarı ve Windows'ta Defender. Rapor değişince hemen, değişmese de 3 saatte bir gönderilir; başarısız giriş varsa her turda kısa bir rapor gider. Ajan bunun için ek bir paket kurmaz, sistemin kendi araçlarını kullanır. Kendi güvenlik raporunu bu ajanda kapatmak için `SECURITY=0` verin (bkz. [envanter değişkenleri](#envanter)).

**Sunucular** listesinin üstündeki **Filo güvenliği** şeridi güvenlik güncellemesi bekleyen, yeniden başlatma bekleyen, güvenlik duvarı kapalı ve Defender sorunu olan sunucuları sayar; bir öğeye basınca liste yalnız o sunuculara süzülür. Satırlardaki küçük rozetler aynı durumu sunucu başına gösterir. Aynı özet salt okunur API anahtarıyla `GET /api/security/summary` adresinden de alınır (bkz. [salt okunur erişim](/docs/entegrasyonlar/#salt-okunur)).

### Ne nereden okunur {#guvenlik-kaynaklar}

| Sistem | Güncellemeler | Yeniden başlatma | Başarısız giriş | Güvenlik duvarı |
|---|---|---|---|---|
| Debian / Ubuntu | apt benzetimi (indirmez, kurmaz); güvenlik = `-security` deposu, Ubuntu Pro ESM dahil | `/run/reboot-required` ve paket listesi | SSH: journal, yoksa `/var/log/auth.log` | ufw, nftables, iptables |
| RHEL / Alma / Rocky / Fedora | `dnf` önbellekten; güvenlik ve ciddiyet errata bilgisinden | `dnf needs-restarting -r` | SSH: journal, yoksa `/var/log/secure` | firewalld, nftables, iptables |
| CentOS 7 (yum) | `yum` önbellekten; dağıtım errata vermediği için güvenlik sayısı "bilinmiyor" | `needs-restarting -r` | SSH: journal, yoksa `/var/log/secure` | firewalld, iptables |
| SUSE / openSUSE | `zypper` (depo tazelemeden); güvenlik yamaları ayrı | `zypper needs-rebooting` | SSH: journal | firewalld, nftables |
| Windows Server | Windows Update'in son taraması (ağa çıkmaz); KB, başlık, ciddiyet, son 30 günde başarısız kurulumlar | bekleyen yeniden başlatma kaydı | Security günlüğü 4625; kilitlenen hesap 4740 | profil başına (Etki alanı, Özel, Genel) |

Araç bulunamazsa yeniden başlatma için kurulu en yeni çekirdek çalışan çekirdekle karşılaştırılır. Paket listesi en çok 500 satırdır; toplam sayı ayrıca tutulur. Bir paketin "kaç gündür beklediği" panelin onu ilk gördüğü andan hesaplanır. Oturumlar Linux'ta `utmp`'den (SSH ve yerel terminal), Windows'ta Uzak Masaüstü oturumlarından okunur. Windows'ta Defender kartı gerçek zamanlı korumayı, imza yaşını, son taramaları, son 30 günün tehditlerini ve kurcalama korumasını gösterir; üçüncü taraf antivirüs kullanan sunucuda kart "Microsoft Defender bulunamadı" der.

> [!NOTE] Ajan yalnız okur
> Ajan yalnızca okur: paket kurmaz, depo dizinini kendisi tazelemez, güvenlik duvarı kuralı değiştirmez. Debian ve Ubuntu'da dizini sistemin `apt-daily` zamanlayıcısı, RHEL ailesinde `dnf-makecache.timer` tazeler. Dizin eskiyse sekmede "paket listesi N gün önce yenilenmiş" uyarısı görünür; `dnf` önbelleği hiç yoksa güncelleme sayısı yerine "paket listesi hiç indirilmemiş" notu çıkar.

### Yetki {#guvenlik-yetki}

Doğrudan (systemd) kurulumda ajan root olarak çalışır ve tüm bölümleri okur. Root olmayan bir kullanıcıyla çalıştırdıysanız:

- apt ve dnf güncelleme sayıları yetki gerektirmez; `zypper` root ister (kilit meşgulse bir sonraki turda yeniden dener).
- SSH günlüğü için kullanıcı `systemd-journal` (journal) ya da `adm` (`/var/log/auth.log`) grubunda olmalıdır; okunamazsa kart "SSH günlüğü okunamadı" der.
- ufw, nftables ve iptables kurallarını okumak root ister; sorgu başarısız olursa kart son bilinen durumu "son sorgu başarısız oldu" notuyla gösterir.

Windows'ta ajan hizmeti LocalSystem olarak kurulur; Security günlüğünü (4625/4740) okumak için bu gereklidir. Hizmeti başka bir hesapla çalıştırırsanız başarısız giriş kartında "son sorgu başarısız oldu" notu görünür, diğer bölümler çalışır.

### Docker ajanı {#guvenlik-docker}

Konteyner içindeki ajan host'un paket yöneticisini, journal'ı ve güvenlik duvarını çalıştıramaz. Bu yüzden Docker ile kurulan ajanda güncellemeler, başarısız girişler, oturumlar ve güvenlik duvarı gönderilmez; sekme bu bölümlerde "ajan konteynerde çalışıyor" notunu gösterir. Host'un kök dosya sistemi `HOST_ROOT` ile bağlıysa yalnız yeniden başlatma işareti (`/run/reboot-required`) okunur. Güvenlik sekmesinin tamamı için ajanı **Doğrudan (systemd)** yöntemiyle kurun.

### Sağlayıcı güvenlik duvarı {#guvenlik-saglayici}

Ajan yalnız sunucunun kendi güvenlik duvarını görür: gelen yönde (INPUT) engelleyen bir kural ya da varsayılan "drop/reject" politikası varsa **Açık** sayılır. Docker'ın kendi zincirleri sayılmaz. Bulut sağlayıcısının güvenlik grubu ya da ağ güvenlik duvarı (Hetzner Cloud Firewall, AWS Security Group vb.) sunucunun içinden görünmez; trafiği orada süzüyorsanız kart "Gelen trafik süzülmüyor" diyebilir. Bu nedenle **Güvenlik duvarı kapalı** kuralı varsayılan olarak kapalıdır; yalnız sunucu içi güvenlik duvarı kullandığınız sunucularda açın. Windows'ta karar sunucunun bağlı olduğu ağ profiline göre verilir; grup ilkesiyle gelen ayarlar yerine yerel ayar okunur.

### Güvenlik kuralları {#guvenlik-kurallari}

Kurallar sunucunun **Uyarı kuralları** bölümünde **Güvenlik** grubundadır; her biri açılıp kapatılır, eşikleri değiştirilir. Olaylar sunucunun bildirim kanallarına gider ve çözülünce kendiliğinden kapanır.

| Kural | Varsayılan | Ne zaman açılır |
|---|---|---|
| Bekleyen güvenlik güncellemesi | açık, 7 gün | En eski bekleyen güvenlik paketi eşikten uzun süredir kurulmadı. |
| Bekleyen yeniden başlatma | kapalı, 3 gün | Sunucu bu kadar gündür yeniden başlatılmayı bekliyor. |
| Başarısız oturum açma | kapalı, 60 dk'da 200 | SSH (Windows'ta 4625) başarısız deneme sayısı eşiği aştı; olayda en çok deneyen adres yazar. |
| Antivirüs imza yaşı | açık, 3 gün | Yalnız Defender olan sunucuda. |
| Antivirüs kapalı | açık | Gerçek zamanlı koruma ya da Defender kapalı. |
| Güvenlik duvarı kapalı | kapalı | Bkz. [sağlayıcı güvenlik duvarı](#guvenlik-saglayici). |
| Başarısız Windows güncellemesi | açık | Son 30 günde başarısız güncelleme kurulumu var. |

Sunucunun **Olaylar** sekmesine ayrıca şunlar yazılır: yeni güvenlik güncellemesi, güncellemelerin kurulması, yeniden başlatma gerekti/gerekmiyor, güvenlik duvarı ya da antivirüs durumunun değişmesi ve Windows'ta kilitlenen her hesap (4740).

## Güncelleme {#guncelleme}

Bekci 1.4.0 ve sonrasında kurulan ajanlar **kendini günceller**: Bekci'yi yeni bir sürüme yükselttiğinizde sunucu ajanları ve kontrol noktaları birkaç saniye içinde aynı sürüme geçer. Ajan, panelden gelen programı yalnızca **projenin imzası** doğrulanırsa kurar. İmzanın açık anahtarı ajanın içine gömülüdür; özel anahtar yalnızca resmi sürüm derlemesinde kullanılır. Bu yüzden panelinizin bulunduğu sunucu ele geçirilse bile sunucularınıza imzasız ya da değiştirilmiş bir program inmez ve ajan eski bir sürüme döndürülemez.

Nasıl çalışır:

- Ajan panelin sürümünü görünce imzalı programı indirir. Özeti ve imzayı doğrular, çalışan programı değiştirir ve yeniden başlar. Docker, systemd ya da Windows hizmet yöneticisi ajanı yeni programla yeniden başlatır.
- Yeni sürüm üç denemede panele bağlanamazsa ajan eski programa döner ve paneli bilgilendirir.
- Sunucu sayfasında **Ajan sürümü** satırında, kontrol noktaları listesinde **Sürüm** sütununda durum görünür: **Güncel**, **Güncelleme var**, **Güncelleniyor…**, **Güncelleme başarısız** ya da **Eski ajan**.

Ayarlar:

- **Ayarlar → Genel → Ajanları otomatik güncelle**: genel ayar, varsayılan olarak açık.
- Ajan başına: sunucunun **Ayarlar** penceresinde ya da kontrol noktasının **Düzenle** penceresinde **Otomatik güncelleme**. Seçenekler: genel ayar, açık ya da kapalı.
- Kapalıyken **Şimdi güncelle** düğmesi ajanı bir kez günceller. Sunucular sayfasında ve kontrol noktaları listesinde **Tümünü güncelle** bütün güncellenebilir ajanları günceller.
- Ajan tarafında tamamen kapatmak için ajanın ayar dosyasına (`/etc/uptime-agent.env`, `/etc/uptime-probe.env` ya da `agent.env`) `AUTO_UPDATE=0` ekleyip ajanı yeniden başlatın.

> [!NOTE]
> Yalnızca resmi sürüm imajları (`kadirsungurlu/bekci:1.5.0` gibi) imzalı ajan içerir. Kaynaktan ya da bir commit'ten derlenen panel ajanlara güncelleme sunmaz; ajan satırında **İmzasız derleme** ya da **Sürümsüz derleme** yazar. `PROBE_IMAGE` ile uygulama imajından çalışan ajanlar da kendini güncelleyemez (**Elle güncellenir**); onlarda imajı güncelleyin.

### Eski ajanları bir kez güncelleme {#eski-ajan}

1.4.0'den önce kurulmuş ajanlar kendini güncelleyemez. Listede **Eski ajan** olarak görünürler. Bunları bir kez elle yeniden kurun; sonraki sürümler kendiliğinden gelir:

1. Panelden **güncel** kurulum komutunu alın. Token yalnızca bir kez gösterildiği için bu adım token'ı yeniler ve eski token hemen geçersiz olur:
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

Kontrol noktasına [OOB IP'si](#oob) girdiyseniz o adresten gelen istekler de kabul edilir; kilit değişmez.

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
| Bildirimde “… sunucuya ulaşılamıyor”, olay listesinde “Sunucuya ulaşılamıyor” | Sunucu ajanından 3 dakikadır veri gelmiyor (çevrimdışı uyarısı). Ajan konteynerinin/hizmetinin çalıştığını ve panele bağlanabildiğini kontrol edin; veri gelince “… tekrar veri gönderiyor” bildirimi gider ve olay kapanır. |
