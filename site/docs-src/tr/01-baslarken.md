---
id: start
title: Başlarken
nav: Başlarken
description: Bekci nedir, neye ihtiyacınız var ve hangi kurulum yolu size uygun? Birkaç dakikada karar verin, doğru rehberle devam edin.
section: Başlarken
order: 1
slug: ""
---

Bekci; web sitelerinizi, API'lerinizi ve sunucularınızı tek panelden izleyen, bir şey ters gittiğinde size hemen haber veren bir izleme sistemidir. Kendi sunucunuzda, tek bir Docker imajıyla çalışır: arayüz, kontrol motoru, veritabanı ve sunucu ajanlarının programları aynı imajın içindedir.

Bu belgeler, Linux'a ve Docker'a çok alışkın değilseniz bile Bekci'yi baştan sona kendi başınıza kurabilmeniz için yazıldı. Her rehber numaralı adımlardan oluşur; her adımın sonunda **“Ne görmelisiniz?”** kutusu, doğru ilerleyip ilerlemediğinizi söyler.

## Neye ihtiyacınız var? {#gereksinimler}

| | En az | Önerilen |
|---|---|---|
| İşlemci | 1 vCPU | 1–2 vCPU |
| Bellek (RAM) | 512 MB | 1 GB |
| Disk | 10 GB | 10 GB ve üstü |
| İşlemci mimarisi | amd64 (x86_64) ya da arm64 (aarch64) | |
| İşletim sistemi | Docker çalıştırabilen herhangi bir Linux dağıtımı | Güncel bir Ubuntu LTS ya da Debian sürümü |

- **Docker:** Bekci bir Docker imajı olarak gelir. Docker kurulu değilse [Docker rehberindeki ilk adım](/docs/kurulum/docker/#docker-kurun) nasıl kuracağınızı anlatır.
- **Alan adı (isteğe bağlı):** Paneli `https://bekci.ornek.com` gibi güvenli bir adresten açmak istiyorsanız bir alan adına (ya da alt alan adına) ihtiyacınız var. Denemek için gerekmez.
- **Bellek kullanımı:** Bekci'nin kendisi 10 monitör ve 3 sunucuda yaklaşık 15 MB RAM kullanır. Gömülü PostgreSQL'li imajı seçerseniz yaklaşık 200 MB daha ekleyin.

> [!NOTE]
> Bu belgelerdeki komutlar **root** kullanıcısıyla çalıştırıldığı varsayılarak yazıldı. Sunucuya root dışında bir kullanıcıyla bağlanıyorsanız komutların başına `sudo` ekleyin (ör. `sudo docker ps`).

## Hangi kurulum size uygun? {#hangi-kurulum}

Aşağıdaki tablodan durumunuza en yakın satırı bulun ve o rehberi açın. Emin değilseniz: **boş bir sunucunuz ve bir alan adınız varsa Caddy'li kurulumu** seçin; en az uğraşla güvenli (HTTPS) ve kalıcı bir kurulum verir.

| Durumunuz | Size uygun rehber | Süre |
|---|---|---|
| Sadece denemek istiyorum, Docker kurulu bir sunucum var | [Docker (tek komut)](/docs/kurulum/docker/) | 3&nbsp;dk |
| Tek sunucu, Docker var; ayarları bir dosyada tutmak istiyorum | [Docker Compose](/docs/kurulum/docker-compose/) | 5&nbsp;dk |
| Boş bir sunucum ve alan adım var, HTTPS istiyorum | [HTTPS'li kurulum (Caddy)](/docs/kurulum/caddy/) — **önerilen** | 10&nbsp;dk |
| Sunucumda Coolify kurulu | [Coolify](/docs/kurulum/coolify/) | 5&nbsp;dk |
| Sunucumda zaten Nginx, Traefik, Apache ya da Caddy çalışıyor | [Ters vekil arkasında](/docs/kurulum/ters-vekil/) | 10&nbsp;dk |

> [!WARNING] 80 ve 443 portları dolu mu?
> Sunucunuzda Coolify ya da başka bir web sunucusu (Nginx, Apache…) zaten çalışıyorsa 80 ve 443 portları doludur. Bu durumda Caddy'li kurulumu **kullanmayın**; [Coolify](/docs/kurulum/coolify/) ya da [ters vekil](/docs/kurulum/ters-vekil/) rehberini izleyin.

<div class="doc-cards">
<a class="doc-card" href="/docs/kurulum/docker/"><b>Docker (tek komut)</b>Tek komutla çalıştırın, tarayıcıdan açın. Denemek için en kısa yol.</a>
<a class="doc-card" href="/docs/kurulum/docker-compose/"><b>Docker Compose</b>Ayarlar bir dosyada; güncelleme ve yedek daha kolay. SQLite ya da PostgreSQL.</a>
<a class="doc-card" href="/docs/kurulum/caddy/"><b>HTTPS'li kurulum (Caddy)</b>Alan adınızla, otomatik Let's Encrypt sertifikasıyla eksiksiz kurulum.</a>
<a class="doc-card" href="/docs/kurulum/coolify/"><b>Coolify</b>Docker imajı olarak ekleyin; alan adı ve sertifikayı Coolify yönetsin.</a>
<a class="doc-card" href="/docs/kurulum/ters-vekil/"><b>Ters vekil arkasında</b>Mevcut Nginx, Traefik, Apache ya da Caddy'nizin arkasına yerleştirin.</a>
<a class="doc-card" href="/docs/ilk-adimlar/"><b>İlk adımlar</b>Kurulumdan sonra: ilk monitör, bildirim kanalı ve durum sayfası.</a>
</div>

## Hangi veritabanı? {#veritabani}

Çoğu kurulum için karar vermenize gerek yok: Bekci varsayılan olarak **SQLite** kullanır. Ek bir servis gerektirmez, en hafif seçenektir ve yüzlerce monitörde de hızlıdır.

| Seçenek | Nasıl | Ne zaman |
|---|---|---|
| **SQLite** (varsayılan) | `kadirsungurlu/bekci:latest` imajı, başka bir şey gerekmez | Çoğu kurulum |
| **Gömülü PostgreSQL** | `kadirsungurlu/bekci:postgres` imajı; PostgreSQL 18 aynı konteynerin içinde çalışır | PostgreSQL istiyorsunuz ama ayrı bir veritabanı yönetmek istemiyorsunuz |
| **Harici PostgreSQL** | `latest` imajı + `DATABASE_URL` ortam değişkeni | Zaten yönettiğiniz bir PostgreSQL sunucunuz var |

> [!IMPORTANT]
> SQLite ile PostgreSQL arasında veri **taşınmaz**. Baştan birini seçin. (Monitörleri, bildirim kanallarını ve durum sayfalarını panelin **Ayarlar → Yedekle / Geri yükle** bölümündeki JSON yedeğiyle yeni kuruluma aktarabilirsiniz; kontrol geçmişi aktarılmaz.)

## Birkaç kavram {#kavramlar}

Bekci'yi kullanırken karşılaşacağınız birkaç terim:

| Terim | Anlamı |
|---|---|
| Monitör | Düzenli olarak kontrol edilen hedef: web sitesi, API, port, veritabanı, cron işi… |
| Olay | Bir monitörün çalışmadığı süre; başlangıcı, bitişi ve nedeniyle kaydedilir. |
| Durum sayfası | Müşterilerinize açık, servislerinizin durumunu gösteren sayfa. |
| Sunucu ajanı | İzlemek istediğiniz sunucuya kurulan küçük program; CPU, RAM, disk ve ağ ölçümlerini gönderir. |
| Kontrol noktası | Monitörlerinizi başka bir konumdan (ör. başka bir şehirden) kontrol eden Bekci kopyası. |
| `BASE_URL` | Panelin dışarıdan açıldığı adres. Bildirimlerdeki bağlantılar ve ajan kurulum komutları bunu kullanır. |

## Yardım {#yardim}

Takıldığınız bir yer olursa önce [Sorun giderme](/docs/sorun-giderme/) sayfasına bakın. Belgelerde eksik ya da hatalı bir şey görürseniz ya da bir hata bulursanız [GitHub'da bildirin](https://github.com/kadirsungurlu/bekci/issues). Bekci açık kaynaklıdır ([AGPL-3.0](https://github.com/kadirsungurlu/bekci/blob/main/LICENSE)); kaynak kodu [GitHub'da](https://github.com/kadirsungurlu/bekci).
