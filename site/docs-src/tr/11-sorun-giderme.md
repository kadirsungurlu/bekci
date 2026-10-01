---
id: troubleshooting
title: Sorun giderme ve SSS
nav: Sorun giderme ve SSS
description: Şifre sıfırlama, sağlık kontrolü, günlükler, dolu portlar, gelmeyen canlı güncellemeler, saat dilimi ve sık sorulan diğer sorular.
section: Bakım ve başvuru
order: 11
slug: sorun-giderme
---

## Konteynerin adını bulun {#konteyner-adi}

Bu sayfadaki komutlarda Bekci konteynerinin adını kullanacaksınız. Kurulum yoluna göre ad değişir:

| Kurulum | Konteyner adı |
|---|---|
| [Docker (tek komut)](/docs/kurulum/docker/) | `bekci` |
| [Docker Compose](/docs/kurulum/docker-compose/) ve [Caddy'li kurulum](/docs/kurulum/caddy/) | `bekci-bekci-1` (ya da `/opt/bekci` içinde `docker compose exec bekci …`) |
| Coolify | Uygulamanın rastgele adı; `docker ps` listesinde `kadirsungurlu/bekci` imajını kullanan satır |

```bash
docker ps --filter ancestor=kadirsungurlu/bekci:latest
```

(Sabit bir sürüm kullanıyorsanız `latest` yerine o etiketi yazın, ör. `kadirsungurlu/bekci:1.0.0`.)

## Şifremi unuttum {#sifre-sifirlama}

Panele giremiyorsanız şifreyi sunucudan sıfırlayabilirsiniz. Bekci çalışırken şu komutu çalıştırın; `admin` yerine kendi kullanıcı adınızı yazın:

:::tabs key=kurulum label="Kurulum yolu"
@tab Docker Compose
```bash
cd /opt/bekci
docker compose exec bekci uptime sifre-sifirla ⟦admin⟧
```
@tab docker run
```bash
docker exec -it bekci uptime sifre-sifirla ⟦admin⟧
```
@tab Coolify
Coolify'da uygulamanın **Terminal** bölümünü açıp şunu çalıştırın:

```bash
uptime sifre-sifirla ⟦admin⟧
```

Ya da sunucuda `docker exec -it ⟦konteyner-adı⟧ uptime sifre-sifirla ⟦admin⟧`.
:::

Komut `Yeni şifre:` diye sorar; yeni şifreyi (en az 8 karakter) yazıp <kbd>Enter</kbd>'a basın. Yazdığınız şifre ekranda görünür; çevrenize dikkat edin.

> [!CHECK]
> `Şifre değiştirildi, tüm oturumlar kapatıldı.` mesajını görürsünüz ve yeni şifreyle giriş yapabilirsiniz. `hata: kullanıcı bulunamadı` görüyorsanız kullanıcı adını yanlış yazmışsınızdır.

**İki adımlı doğrulamayı da kapatmak** için (telefonunuzu ve kurtarma kodlarınızı kaybettiyseniz) komutun sonuna `--2fa-kapat` ekleyin:

```bash
docker compose exec bekci uptime sifre-sifirla ⟦admin⟧ --2fa-kapat
```

Bu komut iki imajda da (SQLite ve PostgreSQL) aynı şekilde çalışır.

## Bekci çalışıyor mu? {#saglik}

Sağlık kontrolü adresi `/healthz`, veritabanına erişilebiliyorsa `ok` döndürür:

```bash
curl http://localhost:8080/healthz          # sunucunun içinden (port yayınlıysa)
curl https://⟦bekci.ornek.com⟧/healthz          # dışarıdan
```

`docker ps` çıktısındaki **STATUS** sütunu da imajın kendi sağlık kontrolünü gösterir: `(healthy)` her şey yolunda, `(unhealthy)` sorun var demektir. Veritabanına ulaşılamıyorsa `/healthz` `503` ve `veritabanı erişilemiyor` döndürür.

## Günlükler {#gunlukler}

```bash
docker logs --tail 100 bekci                 # docker run
cd /opt/bekci && docker compose logs --tail 100 bekci   # Compose
```

Coolify'da uygulamanın **Logs** bölümüne bakın. Daha ayrıntılı günlük için `LOG_LEVEL=debug` ortam değişkenini verip konteyneri yeniden oluşturun ([ortam değişkenleri](/docs/ortam-degiskenleri/)). Hatalı bir açılışın son satırı `hata:` ile başlar ve nedeni söyler.

## Konteyner “başka bir örnekte açık” diyerek bekliyor {#kilit}

Günlükte şunu görüyorsanız:

```text
level=WARN msg="veri klasörü başka bir örnekte açık; o kapanana kadar bekleniyor" kilit=/data/uptime.db.lock en_fazla=10m0s
level=WARN msg="hâlâ bekleniyor: diğer örnek veri klasörünü bırakmadı" gecen=30s
```

Aynı veri birimini kullanan ikinci bir Bekci konteyneri başlatılmış demektir. İki kopya aynı anda çalışıp çift kontrol ve çift bildirim yapmasın diye yeni kopya, eskisi kapanana kadar bekler.

- **Coolify'da güncelleme sırasında** bu normaldir: eski konteyner kapanınca yenisi bir iki saniyede devralır.
- **Başka bir durumda** aynı birimi kullanan eski konteyneri bulup durdurun: `docker ps -a`. 10 dakika (`UPTIME_LOCK_WAIT`) içinde kilit bırakılmazsa yeni konteyner `aynı veri klasörüyle iki uygulama çalıştırılamaz` hatasıyla kapanır.

## Port zaten kullanımda {#port}

`port is already allocated` ya da `address already in use` hatası, seçtiğiniz portu başka bir programın kullandığını söyler:

```bash
docker ps --filter publish=⟦8080⟧        # portu kullanan konteyner
ss -ltnp 'sport = :⟦8080⟧'              # portu kullanan program
```

- **8080:** Coolify kurulu sunucularda Coolify'ın vekili 8080'i kullanır. Başka bir port seçin (`-p 8081:8080`) ya da [Coolify rehberini](/docs/kurulum/coolify/) izleyin.
- **80 / 443:** Sunucuda zaten bir web sunucusu var. Caddy'li kurulum yerine [ters vekil](/docs/kurulum/ters-vekil/) ya da [Coolify](/docs/kurulum/coolify/) rehberini izleyin.

## Panel açılıyor ama canlı güncellemeler gelmiyor {#canli}

Sayfanın üst kısmında sürekli **Bağlantı yok** uyarısı görüyorsanız ya da monitörlerin durumu ancak sayfayı yenileyince değişiyorsa, önünüzdeki ters vekil canlı akışı (`/api/events`) tamponluyordur. Vekilinize göre gereken ayar [ters vekil sayfasında](/docs/kurulum/ters-vekil/#canli-guncelleme-yok).

## “Çok fazla hatalı deneme” {#cok-fazla-deneme}

Aynı IP adresinden 15 dakika içinde 5 hatalı giriş denemesi yapılırsa o adres 15 dakika bekletilir. Süre dolunca yeniden deneyin ya da [şifreyi sıfırlayın](#sifre-sifirlama). Bekci bir ters vekilin arkasındaysa ve tüm kullanıcılar aynı anda engelleniyorsa vekil `X-Forwarded-For` başlığını iletmiyordur ([ters vekil ayarları](/docs/kurulum/ters-vekil/)).

## Saatler yanlış görünüyor {#saat}

Arayüz saatleri şu anda her zaman Türkiye saatiyle (`Europe/Istanbul`) gösterir. Günlük özetler, gece yedeğinin saati ve günlük satırları ise sunucudaki `TZ` ortam değişkenine göredir (varsayılan `Europe/Istanbul`). Başka bir saat dilimi için ör. `TZ=Europe/Berlin` verip konteyneri yeniden oluşturun. Günlükteki `Bekci başladı` satırı kullanılan saat dilimini gösterir (`saat_dilimi=`).

## Bildirimlerdeki bağlantılar ya da ajan komutundaki adres yanlış {#base-url}

`BASE_URL` ayarlı değil ya da yanlış. Panelin dışarıdan açıldığı adresi (`https://…`) verip konteyneri yeniden oluşturun ([ortam değişkenleri](/docs/ortam-degiskenleri/)).

## Disk doldu {#disk}

Disk dolarsa Bekci yeni ölçümleri yazamaz; SQLite kullanıyorsanız veritabanını yükselten bir güncellemeden önceki otomatik yedek de alınamaz ve Bekci açılmaz. Yer açın: eski güncelleme öncesi yedekler (`/data/backups/pre-migrate-…db`) kendiliğinden silinmez, Docker'ın kullanılmayan imajları da yer kaplar. Ham kayıtların ne kadar süre tutulacağını **Ayarlar → Genel** bölümünden kısaltabilirsiniz.

## PostgreSQL sürümü uyuşmuyor {#postgresql-surum}

`postgres` imajı `HATA: PostgreSQL ana sürümü uyuşmuyor.` diyerek başlamıyorsa [PostgreSQL ana sürüm yükseltmesi](/docs/guncelleme-yedek/#postgresql-yukseltme) adımlarını izleyin.

## Sık sorulan sorular {#sss}

### Bekci ücretsiz mi? {#ucretsiz}

Evet. Bekci açık kaynaklıdır ve [AGPL-3.0](https://github.com/kadirsungurlu/bekci/blob/main/LICENSE) lisansıyla dağıtılır; lisans ücreti, abonelik ya da ücretli plan yoktur. Kaynak kodu [GitHub'da](https://github.com/kadirsungurlu/bekci).

### İnternet bağlantısı gerekir mi? {#internet}

Kurulum ve güncelleme sırasında imaj Docker Hub'dan indirilir. Sonrasında Bekci dışarıya yalnızca sizin tanımladığınız kontroller ve bildirimler için bağlanır; iç ağdaki hedefleri de izleyebilir.

### Aynı anda iki Bekci kopyası çalıştırabilir miyim? {#iki-kopya}

Aynı verilerle hayır — SQLite'ta da harici PostgreSQL'de de (veritabanı üzerinde tek-örnek kilidi tutulur); ikinci kopya ilki kapanana kadar [bekler](#kilit). Birden çok replika çalıştırılamaz. Farklı konumlardan kontrol için [kontrol noktası](/docs/sunucu-ajani/#kontrol-noktasi) kullanın.

### SQLite'tan PostgreSQL'e geçebilir miyim? {#sqlite-postgresql}

Veritabanı doğrudan taşınmaz. Monitörleri, bildirim kanallarını, etiketleri ve durum sayfalarını **Ayarlar → Yedekle / Geri yükle** bölümündeki JSON yedeğiyle yeni kuruluma aktarabilirsiniz; kullanıcılar ve kontrol geçmişi aktarılmaz ([ayrıntı](/docs/guncelleme-yedek/#json-yedek)).

### Ne kadar disk kullanır? {#disk-kullanimi}

Monitör sayısına ve kontrol aralığına bağlıdır. Her kontrolün ayrı kaydı varsayılan olarak 14 gün tutulur, sonra saatlik ve günlük özetlere dönüştürülür; bu süreleri **Ayarlar → Genel** bölümünden değiştirebilirsiniz. Gece yedekleri de veritabanı boyutunun katları kadar yer kaplar.

### Bir hata buldum ya da bir özellik istiyorum {#hata-bildir}

[GitHub'da bir kayıt (issue) açın](https://github.com/kadirsungurlu/bekci/issues) ya da [me@kadir.app](mailto:me@kadir.app) adresine yazın.
