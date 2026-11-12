---
id: plugins
title: Eklentiler (özel denetimler)
nav: Eklentiler
description: Sunucu ajanına kendi denetimlerinizi ekleyin: imzalı küçük sh betikleri, yerel izin, Nagios ve JSON çıktısı, kurallar ve sorun giderme.
section: Kullanım
order: 16
slug: eklentiler
---

Eklenti, sunucu ajanının belirli aralıklarla çalıştırdığı imzalı küçük bir `sh` betiğidir. RAID durumu, disk sağlığı ya da kendi uygulamanızın bir ölçümü gibi Bekci'nin kendiliğinden toplamadığı bilgileri panele taşır. Betik bir durum (sağlıklı, uyarı, kritik, bilinmiyor), kısa bir ileti ve isteğe bağlı sayısal metrikler döndürür; panel bunları sunucunun **Denetimler** sekmesinde gösterir, grafiğe döker ve kurallarla olaya çevirir. Eklentiler ücretsizdir.

## Güvenlik modeli {#guvenlik}

Bir eklenti sunucunuzda kod çalıştırdığı için Bekci üç kilit ister:

- **İmza:** Manifest sizin Ed25519 anahtarınızla imzalanır. Ajan yalnız kendisine eklediğiniz açık anahtarlarla doğrulanan manifesti kabul eder.
- **Yerel izin:** Her eklentinin gövdesi sunucuda root ile ayrıca iğnelenir (`uptime betik eklenti izin <ad> <sha256>`). Ajan yalnız izin verilen sha256'ya sahip gövdeyi çalıştırır. Panel ele geçirilse, hatta imza anahtarı sızsa bile sunucuda izin verilmemiş yeni içerik çalışmaz. Panelin yetkisi yalnız izinli eklentiyi açıp kapatmak ve aralığı manifestin sınırları içinde seçmektir.
- **Kafes:** Her çalıştırma ayrı bir systemd geçici biriminde olur: varsayılan kullanıcı `nobody`, ağ kapalı (`PrivateNetwork`), dosya sistemi salt okunur (`ProtectSystem=strict`), özel `/tmp`, 128 MB bellek, %20 CPU, 32 süreç, zaman aşımı. Ajanın ortam dosyası ve `/etc/bekci` kafesin içinden görünmez.

Ajan imzayı, izni ve süreyi her çalıştırmadan önce yeniden denetler; izni kaldırdığınızda ya da manifestin süresi dolduğunda bir sonraki tur çalışmaz.

> [!WARNING] Eklenti çıktısı panelde görünür
> İleti, metrikler ve hata çıktısının ilk 1 KiB'ı panele gönderilir ve sunucuyu görebilen herkes tarafından okunur. Parola, belirteç ya da başka bir sır basan eklenti yazmayın, imzalamayın.

## Gereksinimler {#gereksinimler}

- Ajan **Doğrudan (systemd)** yöntemiyle kurulmuş Linux sunucuda çalışmalı; systemd 254 ya da daha yeni olmalı (Debian 13, Ubuntu 24.04, RHEL 10 ve sonrası).
- Docker ile kurulan ajanda eklenti çalışmaz (`platform` hatası). Windows'ta eklenti yoktur.
- Ajanın ortamında `PLUGINS=0` verirseniz eklentiler tamamen kapanır.
- Sunucu başına en çok 20 denetim.

## Adım adım {#adimlar}

### 1. İmza anahtarı üretin {#anahtar}

Kendi makinenizde, bir kez (root gerekmez). Özel anahtar dosyaya, açık anahtar ekrana yazılır:

```sh
uptime betik eklenti anahtar-uret ~/bekci-eklenti.key
```

Özel anahtarı gizli tutun ve ajanın çalıştığı sunucuya koymayın; sunucuya yalnız açık anahtar gider.

### 2. Gövdeyi ve manifesti yazın {#manifest}

Gövde (`govde.sh`) POSIX `sh` betiğidir, en çok 64 KiB. Manifest (`manifest.json`) katı JSON'dur: tüm alanlar zorunludur, bilinmeyen ya da yinelenen alan reddedilir.

```json
{
  "schema": 1,
  "id": "mdadm",
  "version": 1,
  "sha256": "",
  "interpreter": "sh",
  "mode": "periodic",
  "class": "metric",
  "run_as": "nobody",
  "platforms": ["linux"],
  "interval_min": 60,
  "interval_max": 3600,
  "timeout_sec": 10,
  "output": "nagios",
  "network": false,
  "not_before": 0,
  "not_after": 0
}
```

| Alan | Değer |
|---|---|
| `schema` | `1` |
| `id` | Eklenti adı: küçük harf, rakam ve `-`, en çok 64 karakter. Panelde denetim adı olur, `izin` komutunda da bu ad kullanılır. |
| `version` | 1'den başlayan tam sayı; her yeni gövdede artırın. |
| `sha256` | Boş bırakın (`""`); imzalama aracı gövdeden doldurur. |
| `interpreter` | `sh` |
| `mode` | `periodic` |
| `class` | `metric` |
| `run_as` | `nobody` (önerilen) ya da `root` (yalnız gerçekten gerekiyorsa, ör. SMART). |
| `platforms` | `["linux"]` |
| `interval_min`, `interval_max` | Panelde seçilebilecek aralığın sınırları, saniye (60-86400). |
| `timeout_sec` | Bir çalıştırmanın üst süresi, saniye (1-60). |
| `output` | `nagios` ya da `json` (bkz. [çıktı biçimleri](#cikti)). |
| `network` | `false` önerilir; `true` ağ erişimi açar. |
| `not_before`, `not_after` | Geçerlilik (Unix saniye). İkisi de `0` ise araç imzalama anından 90 gün verir; en çok 180 gün. |

### 3. İmzalayın {#imzala}

```sh
uptime betik eklenti imzala ~/bekci-eklenti.key manifest.json govde.sh > mdadm.zarf.json
```

Araç gövdenin sha256'sını manifeste yazar, manifesti denetler ve imzalı zarfı ekrana basar. Gövde sha256'sı ve ajanda çalıştırılacak `izin` komutu hata akışına yazılır.

### 4. Ajanda anahtar ve izin {#izin}

Ajanın çalıştığı sunucuda root ile. Açık anahtar bir kez eklenir; izin her eklenti ve her sürüm için verilir:

```sh
sudo uptime betik eklenti anahtar <açık anahtar base64>
sudo uptime betik eklenti izin mdadm <gövde sha256>
```

Ayarlar `/etc/bekci/eklenti.json` dosyasına (root, 0600) yazılır.

### 5. Panele yükleyin {#panel}

**Sunucular** > sunucu > **Denetimler** sekmesi > **Eklenti ekle**. İmzalı manifest (zarf, `.json`) ve eklenti gövdesi (`.sh`) dosyalarını seçin, çalışma aralığını manifest sınırları içinde belirleyin, **Açık** kutusunu işaretleyip kaydedin. Denetim adı manifestteki `id` değerinden alınır. Panel zarfı ve gövdeyi ajana iletir; ajan bir sonraki yoklamada eklentiyi zamanlar.

Sekmede her denetim için durum, ileti, son çalışma zamanı ve süresi, metriklerin küçük grafikleri görünür; bir metriğe basınca 24 saat, 7 gün ya da 30 günlük grafik açılır. Ajan bir eklentiyi reddederse nedeni sekmede yazar (bkz. [sorun giderme](#sorun-giderme)). Metrik geçmişi PostgreSQL gerektirir; SQLite kurulumunda yalnız son değerler görünür.

## Kurallar {#kurallar}

Kurallar aynı sekmede denetimin düzenle penceresinden tanımlanır. Olaylar sunucunun bildirim kanallarına gider ve düzelince kendiliğinden kapanır.

- **Durum kuralı:** Seçtiğiniz durumlar (kritik, uyarı, bilinmiyor) belirtilen dakika boyunca sürerse olay açılır; denetim sağlıklıya dönünce kapanır.
- **Metrik eşik kuralı:** Bir metrik için uyarı ve kritik eşiği. Varsayılan olarak değer eşiği aşınca tetiklenir; **Küçükse kötü** seçeneği yönü çevirir (ör. boş alan, yedek disk sayısı).

Bir denetim 3 tur üst üste bilinmiyor dönerse sunucunun **Olaylar** sekmesindeki zaman çizelgesine yazılır; bu tek başına olay açmaz.

## Çıktı biçimleri {#cikti}

### Nagios {#nagios}

Dönüş kodu durumu verir:

| Kod | Durum |
|---|---|
| 0 | sağlıklı |
| 1 | uyarı |
| 2 | kritik |
| 3 | bilinmiyor |
| diğer, zaman aşımı, sinyal | bilinmiyor |

Çıktının ilk satırı `İLETİ | perfdata` biçimindedir; sonraki satırlarda `|` işaretinden sonra gelenler ek perfdata sayılır. Perfdata öğeleri boşlukla ayrılır:

```text
RAID OK: 2 dizi, 0 bozuk | degraded=0;;0;0 md0.active=2 md0.total=2 'yanit suresi'=12ms;200;500;0
```

Söz dizimi `'etiket'=değer[birim];[uyarı];[kritik];[en az];[en çok]`. Birimler `s ms us % B KB MB GB TB c`; süreler saniyeye, boyutlar bayta (1024 tabanlı) çevrilir, tanınmayan birim olduğu gibi kalır. `U` değeri atılır. Eşik aralıkları:

| Yazım | Uyarı ne zaman |
|---|---|
| `10` | değer 0'dan küçük ya da 10'dan büyük |
| `10:` | değer 10'dan küçük |
| `~:10` | değer 10'dan büyük |
| `10:20` | değer 10-20 dışında |
| `@10:20` | değer 10-20 içinde |

Etiketteki izin verilmeyen karakterler `_` olur.

### JSON {#json}

Çıktı tek bir JSON nesnesidir; dönüş kodu kullanılmaz (zaman aşımı yine bilinmiyor sayılır):

```json
{
  "status": "warning",
  "message": "sda yeniden atanmış sektör 8",
  "metrics": {
    "sda.temp_c": {"value": 34, "unit": "C"},
    "sda.reallocated": {"value": 8, "unit": "", "warn": "0", "crit": "50"},
    "sda.pending": 0
  }
}
```

`status` değerleri `ok`, `warning`, `critical`, `unknown`; eksik ya da tanınmayan değer bilinmiyor sayılır. Metrik değeri nesne ya da doğrudan sayı olabilir; `warn` ve `crit` yukarıdaki aralık yazımını kullanır.

Düz biçim de kabul edilir (`metrics` yoksa): üst düzeydeki sayısal alanlar metriktir, `units` nesnesi birimleri verir, `status` `1` sağlıklı ve `0` kritik demektir (yoksa sağlıklı), ileti `msg` alanındadır:

```json
{"status": 1, "msg": "kuyruk normal", "kuyruk": 12, "gecikme": 0.4, "units": {"gecikme": "s"}}
```

Geçersiz JSON bilinmiyor durumuna düşer.

### Sınırlar {#sinirlar}

- Metrik adı `^[a-zA-Z0-9_.-]{1,64}$`; JSON'da kurala uymayan ad atılır.
- Eklenti başına en çok 50 metrik; fazlası, yinelenen ad, NaN ve sonsuz değerler atılır, sekmede atılan sayısı görünür.
- İleti en çok 512 karakter.
- Çıktı en çok 64 KiB: Nagios çıktısı kesilir, JSON reddedilir. Hata çıktısının (stderr) ilk 1 KiB'ı saklanır.

## Güncelleme ve kaldırma {#guncelleme}

Gövdeyi değiştirdiğinizde:

1. Manifestte `version` değerini artırın.
2. Yeniden imzalayın (`imzala`), yeni gövde sha256'sını not edin.
3. Ajanda yeni sha256 ile `sudo uptime betik eklenti izin <ad> <sha256>` çalıştırın (eski izin değişir).
4. Panelde denetimin düzenle penceresinden yeni zarf ve gövdeyi yükleyin.

Panelde yeni sürüm yüklenmeden ajandaki izni değiştirirseniz ya da tersini yaparsanız eklenti `pin_mismatch` hatasıyla çalışmaz; ikisi aynı sürüme gelince düzelir. Manifest süresi dolmadan önce de yeniden imzalayıp yükleyin.

Eklentiyi sunucuda tamamen durdurmak için root ile izni kaldırın; bir sonraki tur çalışmaz:

```sh
sudo uptime betik eklenti kapat mdadm
```

Panelde denetimi yalnız kapatmak için düzenle penceresindeki **Açık** kutusunu kaldırın.

## Sorun giderme {#sorun-giderme}

Ajan bir eklentiyi çalıştırmazsa sekmede şu kodlardan biri görünür:

| Kod | Anlamı | Ne yapılır |
|---|---|---|
| `signature` | İmza ajandaki anahtarlarla doğrulanamadı. | `uptime betik eklenti anahtar` ile açık anahtarı ekleyin ya da doğru anahtarla imzalayın. |
| `manifest` | Manifest geçersiz (eksik/bilinmeyen alan, sınır dışı değer). | Manifesti düzeltip yeniden imzalayın. |
| `name` | Manifest kimliği denetim adıyla aynı değil. | Doğru zarfı yükleyin. |
| `body` | Gövde manifestteki sha256 ile uyuşmuyor. | İmzaladığınız gövdeyi yükleyin. |
| `not_pinned` | Ajanda bu eklenti için izin yok. | `sudo uptime betik eklenti izin <ad> <sha256>` |
| `pin_mismatch` | Ajandaki izin başka bir sürümün. | Yeni sha256 ile yeniden izin verin. |
| `expired` | Manifestin süresi doldu. | Yeniden imzalayıp yükleyin. |
| `not_yet_valid` | `not_before` ileride. | Sunucu saatini ve manifesti denetleyin. |
| `interval` | Aralık manifest sınırlarının dışında. | Panelde aralığı sınırlar içinde seçin. |
| `disabled` | Ajanda eklenti yapılandırması yok ya da bozuk. | Önce `uptime betik eklenti anahtar` ile yayın anahtarını ekleyin. |
| `platform` | Bu ajan eklenti çalıştıramaz. | Linux + systemd 254, doğrudan (systemd) kurulum gerekir. |

Ajandaki anahtarları, izinleri ve kayıt zincirinin durumunu görmek için:

```sh
sudo uptime betik eklenti liste
```

Açma, kapatma, sürüm değişimi, izin ve günlük özet kayıtları `/var/log/bekci-eklenti/kayit.jsonl` dosyasında zincirli tutulur; her çalıştırma yazılmaz. Bir eklentiyi elle denemek için gövdeyi doğrudan çalıştırın (`sh govde.sh; echo $?`); kafeste ağın kapalı ve dosya sisteminin salt okunur olduğunu unutmayın.

## Örnekler {#ornekler}

Depodaki [scripts/eklentiler](https://github.com/kadirsungurlu/bekci/tree/main/scripts/eklentiler) klasöründe iki hazır örnek var:

- `mdadm`: `/proc/mdstat`'tan yazılım RAID dizilerini okur (etkin/toplam disk, bozuk dizi, eşitleme yüzdesi); Nagios çıktısı, `nobody` ile çalışır.
- `smart`: `smartctl` ile disk sağlığı, sıcaklık, yeniden atanmış ve bekleyen sektörler, NVMe aşınma ve ortam hataları; JSON çıktısı, `smartmontools` paketi ve `run_as: root` gerektirir.
