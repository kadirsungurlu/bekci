---
id: scripts
title: Betik çalıştırma
nav: Betik çalıştırma
description: Sunucularda imzalı bakım betikleri çalıştırma: kurulum, bekci-imza aracı, onay akışı, acil durdurma ve güvenlik sınırları.
section: Bakım ve başvuru
order: 16
slug: betik-calistirma
---

Betik çalıştırma, önceden imzaladığınız küçük bakım işlerini (günlük temizliği, servis yeniden başlatma, sertifika yenileme) panelden seçili sunucularda başlatır. Özellik varsayılan olarak **kapalıdır**: yalnız sunucuda root ile açılır, panel kendi başına açamaz.

Panel bir komut satırı değildir. Panelden yalnız sizin imzaladığınız ve o sunucunun izin verdiği betik, tanımlı parametrelerle çalışır. Son kararı panel değil, sunucudaki ajan verir.

## Gereksinimler

- PostgreSQL veritabanı. SQLite ile Betikler ekranı kapalıdır (bkz. [Veritabanı](/docs/veritabani/)).
- Yönetici hesabı ve iki adımlı doğrulama. Okuma dışındaki işlemlerde doğrulama kodu sorulur.
- Sunucuda systemd ile çalışan Linux ajanı. API anahtarları betik uçlarını kullanamaz; işlemler tarayıcı oturumundan yapılır.
- İmza için panel dışında bir makine. Anahtarlar panele, sunuculara ya da CI'a girmez.

## Kurulum

### 1. İmza aracı ve anahtarlar

`bekci-imza` çevrimdışı çalışır, ağa bağlanmaz:

```sh
go build -o bekci-imza ./cmd/bekci-imza
./bekci-imza anahtar-uret --yuva yayin --cikti yayin.anahtar
./bekci-imza anahtar-uret --yuva onay --cikti onay.anahtar
```

Parola stdin'den okunur. İki anahtar iki iş görür: **yayın anahtarı** betik imzalar, **onay anahtarı** tek seferlik bilet imzalar. Dosyaları çevrimdışı yedekleyin. Ekrana basılan açık anahtarları Betikler > Güven ve denetim > Güven anahtarları bölümüne ekleyin.

### 2. Sunucuda açma

Her sunucuda root ile:

```sh
uptime betik ac --yayin-anahtar <açık anahtar> --onay-anahtar <açık anahtar> \
  --grup temizlik --izin journal-temizle=<gövde özeti>
```

Komut `/etc/bekci/betik.json` dosyasını yazar ve sunucunun `host_key` değerini gösterir. Panelde aynı sunucu için görünen değerle karşılaştırıp Güven ve denetim > Sunucular bölümünden anahtarı onaylayın.

| Komut | Görevi |
|---|---|
| `uptime betik izin <id=özet>` ya da `--sil <id>` | Sunucunun izin listesini değiştirir |
| `uptime betik anahtar --yayin-ekle`, `--onay-sil` ... | Güvenilen anahtarları değiştirir |
| `uptime betik iptal <zarf dosyası>` | İmzalı iptali panel olmadan uygular |
| `uptime betik kapat` | Özelliği anında kapatır, çalışan betiği durdurur |

`--izin` değeri `id`, `id=özet` ya da `id=özet:en_az_onay` olabilir. Özetsiz (iğnesiz) izinler her zaman imzalı bilet ister. Joker yoktur.

### 3. Betik imzalama ve yükleme

```sh
./bekci-imza imzala --anahtar yayin.anahtar \
  --bildirim scripts/betikler/journal-temizle/manifest.json \
  --govde scripts/betikler/journal-temizle/govde.sh --cikti imzali/
```

Önce gövdeyi okuyun: imza "bunu okudum, çalışabilir" demektir. Çıktı `<id>-<sürüm>.betik.json` dosyasıdır; Betikler ekranından **Betik yükle** ile ekleyin. İmza en çok 180 gün geçerlidir, varsayılan 90 gündür. Süresi dolan betik yeni sürümle yeniden imzalanır.

Başlangıç betikleri depoda `scripts/betikler/` altındadır:

| Betik | Grup | Onay | Sınır |
|---|---|---|---|
| `journal-temizle` | `temizlik` | none | 6 saatte 1 |
| `paket-onbellek` | `temizlik` | none | 6 saatte 1 |
| `docker-derleme-temizle` | `docker` | panel | 6 saatte 1 |
| `docker-imaj-temizle` | `docker` | panel | Günde 1 |
| `servis-yeniden` | `sistem` | signed | 10 dakikada 1 |
| `servis-yeniden-web` | `web` | signed | 10 dakikada 1 |
| `sertifika-yenile` | `web` | signed | Günde 1 |

## Onay akışı

Kütüphanede **Çalıştır** ile sunucu ve parametreleri seçip doğrulama kodunu girersiniz. Devamı betiğin onay sınıfına bağlıdır. Etkin sınıf; imzadaki sınıf, sunucudaki yerel `min_approval` ve iğne durumundan en yükseğidir, panelden düşürülemez.

| Sınıf | Ne gerekir |
|---|---|
| `none` | Başlatanın doğrulama kodu yeterlidir |
| `panel` | Başlatan dışında ikinci bir yönetici onaylar. Tek yönetici varsa imzalı bilet gerekir |
| `signed` | Çevrimdışı imzalı, tek sunucu ve tek çalıştırmaya bağlı bilet gerekir |

### Bilet

Bilet bekleyen işin ayrıntı sayfasındaki istek JSON'unu `istek.json` olarak kaydedin:

```sh
./bekci-imza bilet --anahtar onay.anahtar --istek istek.json --onaylayan kadir --igneler igneler.json
```

`igneler.json` sizin tuttuğunuz `{"<host_key>": "sunucu adı"}` dosyasıdır; panelden gelmez. Ekrandaki betik, sürüm, parametre ve sunucuyu okuyup `evet` yazın. Bilinmeyen sunucu uyarısını geçiştirmeyin. Çıkan bileti aynı sayfada **Bilet yapıştır** ile girin.

### Sonuç

Ayrıntı sayfası durumu canlı izler. Ajan beyanı ajanın kendi anahtarıyla imzalıdır. Çıktı en sonda gösterilir ve metin olarak işlenir. Ajan işi reddederse nedeni okunur metinle görünür.

## Acil durdurma

Güven ve denetim sayfasındaki **Acil durdur** doğrulama kodu istemez: bekleyen işler iptal olur, yeni çalıştırma başlatılamaz. **Devam ettir** kod ister. Tek sunucuda panelsiz durdurmak için `uptime betik kapat` kullanın.

## İptal ve kayıt doğrulama

Bir betiği geri çekmek için iptal zarfı imzalayıp yükleyin:

```sh
./bekci-imza iptal --anahtar yayin.anahtar --id journal-temizle --alt-surum 2
```

Panel her işlemi zincirli kayda yazar. Panelin kendi "tutarlı" beyanı kanıt değildir: kaydı dışa aktarıp panel ağı dışında denetleyin ve zamanlı işe bağlayın:

```sh
./bekci-imza dogrula --kayit bekci-betik-kayit.json --ajan-kayit kayit.log \
  --bekle zincir-basi.json --en-eski-kontrol 25h
```

Çıkış kodu 0 sağlam, 1 kırıktır. `--bekle` önceki zincir başını saklar, geri almayı ve çatallanmayı yakalar. Sunucudaki tanık kayıt `/var/log/bekci-betik/kayit.log` dosyasıdır.

## Güvenlik sınırları

- **Korunan:** ele geçirilmiş panel kendi betiğini yazamaz, gövdeyi değiştiremez, imza olmadan `signed` iş başlatamaz, izinli olmayan betiği çalıştıramaz, sınırları aşamaz.
- **Korunmayan:** çalınan bir yönetici oturumu ve kodu, sunucuda izinli `none` ve `panel` betiklerini kendi sınırları içinde çalıştırabilir. Bu yüzden `none` betikleri servis kesmemeli, tanık kayıt silmemelidir.
- **Betikler root çalışır.** Bu özellik sunucuda root yetkisi vermenin bir biçimidir; izin listesini dar tutun.
- `sshd`, ajan, `docker` ve veritabanı servisleri yeniden başlatma listelerine eklenmez.
- Yayın anahtarı sızarsa yeni anahtar üretin, eskisini her sunucuda `uptime betik anahtar` ile silin. Panel bu listeyi değiştiremez.
