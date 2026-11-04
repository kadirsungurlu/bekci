# Betik çalıştırma güvenlik tasarımı (iş 680)

Durum: TASARIM (2026-11-04). Kod yok. §12 kararları D1-D15 onaylandı 2026-11-04 (yönetici); 690 bu belgeye uyar.
Üst belge: `docs/TIMESCALE-TASARIM.md` (600); `scripts` / `script_runs` için orada "Ayrıntı 680" denen konu buradadır (§9).

Etiketler: **[kod]** bugünkü kodda doğrulandı (dosya verildi); **[tasarım]** henüz yok, 690'da yazılır ve denenir;
**[denendi]** bu sunucuda systemd 257 ile küçük deneme yapıldı (§6.2). Bağımsız inceleme ve işlenen bulgular: §15.

---

## 1. Neden tehlikeli, temel ilke

Bugün panel bir ajana yalnız izleme işi verebilir. Betik özelliği, panelden gelen bir isteğin sunucuda **root olarak kod
çalıştırması** demektir: tek panel, bütün filonun anahtarı olur. Tasarımın tek cümlelik amacı:

> **Panel tamamen ele geçirilse bile saldırgan, operatörün imzaladığı ve o sunucuda yerel olarak izin verilmiş içeriğin dışında
> hiçbir şey çalıştıramaz; `signed` sınıfı betikleri hiç çalıştıramaz. Bu güvence iki imza anahtarından biri sızsa da sürer;
> ikisi birden sızarsa yalnız yerelde içeriği iğnelenmiş betikler korunur (§3.5).**

Tanım: `signed` sınıfı = servis kesintisi, veri kaybı ya da erişim kaybı doğurabilen her betik. Diğerleri yalnız önbellek ve eski
kayıt temizliği gibi geri alınabilir ya da yeniden üretilebilir işlerdir. Sınıf kararı bir karardır (D11).

Sonuç olarak: **panelin elinde yetki veren hiçbir özel anahtar bulunmaz.** Panel bir posta memurudur; veritabanı, panel
ayarları ve panel arayüzü güven kökü değildir. Ajan her kararı yerel olarak verir. Panelde yapılan denetimler (izinli sunucu
listesi, panel içi onay) kolaylık ve insan hatası korumasıdır, güvenlik sınırı **değildir**; güvenlik sınırı olan her şey
ajanda ya da panelin üretemeyeceği bir imzadadır.

## 2. Bugünkü durum (kodda doğrulandı)

| Gerçek | Betik tasarımına etkisi | Kaynak |
|---|---|---|
| systemd ajanı `User=` olmadan **root** çalışır; birimde `NoNewPrivileges`, `PrivateTmp`, `MemoryMax=256M`, `CapabilityBoundingSet=CAP_NET_RAW` var | Ajanın kendi süreci root yetkisi kullanamaz, `sudo`/fork ile de veremez. Bu yüzden betik ajanın alt süreci **olamaz**; PID 1 üzerinden geçici birim gerekir (§6.2) | `api/servers.go:528-531` |
| `PROBE_TOKEN` ortam değişkenidir (`EnvironmentFile`) | Ajan alt süreci token.ı görürdü; betik PID 1 birimi olduğundan miras almaz (§6.2) | `api/servers.go:528` |
| Docker ajanı `--cap-drop ALL`, no-new-privileges; host erişimi yok | Docker ajanında betik anlamsız ve açılmamalı | `api/installcmd.go` |
| Kurulum komutunu panel üretir; ikili bütünlüğü **komuta gömülü SHA-256** ile sağlanır, özeti de panel yazar | Ele geçirilmiş panel yeni kurulumu zehirler; güven kökü kurulum komutuyla gelemez | `api/installcmd.go:17` |
| `GET /api/probe/binary` herkese açık, gövde panelden gelir | Otomatik güncellemede imza korur, elle kurulumda korumaz | `api/agentbin.go` |
| Ajan güncellemesi gömülü anahtarla Ed25519 imzalı, önekli (`bekci-agent-manifest-v1`), eski/aynı sürüm reddedilir | İyi kalıp; ama betik için **aynı anahtar ve önek kullanılmaz**. Bildirimde süre sonu yok | `agentupdate/agentupdate.go` |
| `http://` panel yalnız `PROBE_ALLOW_INSECURE=1` ile kabul edilir; ajan istemcisinde `InsecureSkipVerify` yok | TLS varsayılan; betik özelliği http'de reddedilmeli | `probe/probe.go:276` |
| Ajan saat farkını panelin `server_time` değerinden öğrenir (`skewMs`) | Süre denetimi bu değeri **kullanamaz**: panel saati geri alıp süresi geçmiş imzayı diriltir | `probe/probe.go` pollJobs |
| Ajan-panel kimliği paylaşılan token, DB'de SHA-256 özeti; ajanın kendine ait anahtarı yok | Ajanın kanıtlanabilir imzası yok; `K_ajan` eklenir (§4) | `store/probes.go` |
| `full` anahtar, sahibi ile anahtar rolünün düşüğünü alır, `s.admin` geçer; `read` izin listesindedir; `sessionOnly` `/api/auth/`, `/api/api-keys`, `/2fa/` vb. yolları anahtara kapatır | Mevcut `s.admin` ile yazılırsa sızan anahtar betik başlatır. `sessionOnly` listesine betik yolları eklenir **ve** işleyicide ikinci denetim yapılır (§7) | `api/apikeys.go:98` |
| `audit_log` saklama süresinde `DELETE` ile silinir, yazılabilir | "Değiştirilemez kayıt" için kullanılamaz | `store/cleanup.go:115` |
| Ajan güncelleme imza anahtarı GitHub Actions sırrında | Bu anahtar sızarsa otomatik güncellenen tüm ajanlarda kod çalışır. **Bu risk bugün de var**, betikten bağımsız | `agentupdate/agentupdate.go` |

## 3. Tehdit modeli

Varlıklar: sunucularda root kod çalıştırma, servis sürekliliği, betik çıktısındaki gizli bilgi, kaydın doğruluğu.
Kapsam dışı: ajanın sunucusunda zaten root olan saldırgan o sunucuda her şeyi yapar; amaç oradan **başka yere** yayılmamasıdır.

### 3.1 Panel ele geçirilirse (DB, ikili, oturum, panel sırları dahil tamamı)

| # | Saldırı | Karşılık | Kalan risk |
|---|---|---|---|
| P1 | Kendi kabuk komutunu gönderir | Ajan yalnız `K_yayın` imzalı gövdeyi çalıştırır; imza özel anahtarı panelde yok | Yok |
| P2 | İmzalı betiğin gövdesini/parametre şemasını/onay sınıfını/sınırlarını değiştirir | Hepsi imzalı bildirimde; ajan gövde özetini çalıştırmadan önce doğrular | Yok |
| P3 | Betiği izin verilmemiş sunucuda çalıştırır | Sunucu izni panelde değil: bildirimdeki `groups` ile ajanın yerel grubu kesişmeli, yerel `allow` kaydı da izin vermeli | Yok |
| P4 | `signed` sınıf betiği onaysız çalıştırır, onayı sahte gösterir | Ajan `K_onay` biletini ister; bilet tek ajan + tek çalıştırma + parametre özetine bağlı | Yok |
| P5 | Eski, açığı olan imzalı sürüme geri döndürür | Ajan betik başına başarıyla çalışan en yüksek `version` değerini kalıcı tutar; iğneli kayıtta hash zaten tek içeriğe izin verir | Yeni kurulan ajan ve VM anlık görüntüsüne dönen ajan geçmişi bilmez; `not_after` sınırlar |
| P6 | Süresi geçmiş imzayı saat oynatarak diriltir | Süre denetimi yerel saatle; panel `server_time` kullanılmaz | Sunucu NTP'si de saldırgandaysa sınırlı; `last_seen` yerel kayıt saatin geri gitmesini reddeder |
| P7 | Geçerli **düşük sınıf** betiği geçerli parametreyle, filoda eşzamanlı ya da sırayla kötüye kullanır; `none`/`panel` sınıfında `run_id`, parametre, zamanlama imzasızdır | Bildirimde `min_interval_sec`, `max_per_day`, ajanda yerel sayaç; riskli betik `signed`; `none` sınıfının **tek** güvencesi budur (§5.1). Ajan `0..jitter_sec` rastgele gecikme uygular, filo eşzamanlı vurulamaz | **Kabul edilen kalan risk:** sınırlar içinde kullanım mümkündür |
| P8 | Kullanıcıya sahte onay ekranı gösterir (A sunucusu yazar, B için bilet ister) | Bilet aracı (`bekci-imza`) ekranı **biletin kendisinden** üretir, `host_key` yerel iğne listesiyle ada çevrilir, panel metnine güvenmez | Operatör dikkatine bağlı |
| P9 | İptal listesini (revoke) ajana iletmez | `not_after` en çok D4 kadar; iptal yalnız bu süreyi kısaltır | D4 süresi kadar pencere |
| P10 | Ajanı zehirli kurulum komutuyla yeniden kurdurur | Betik açma ayrı yerel komuttur, güven anahtarı panelden gelmez; açık anahtar parmak izi operatörün makinesinden girilir. Yeniden kurulumda kurulum komutu çalıştıran kişi paneli güvenilir sayıyordur | **Kalan risk:** zehirli ikili kurulduysa her şey biter; ikili imzasını kurulum betiği doğrulamalı (öneri, §11) |
| P11 | Kayıtları siler/değiştirir | Ajan zincirli imzalı makbuz + ajan yerel kaydı + dış çapa (§8) | Çapadan sonraki panel kaydı silinebilir ama ajan tarafı durur, fark görünür |
| P12 | Bildirimleri susturur; `journal-temizle` ile journald tanığını siler | Tanık `kayit.log`'dur (temizliğin dışında), journald yalnız kopyadır; `gun` alt sınırı 7 (§13); operatör uzak log'a yollayabilir | Yalnız panel kanalı varsa sessiz kalabilir |
| P13 | Çıktılardan gizli bilgi toplar | Betikler sır basmamalı (imza aracı uyarır), çıktı maskesi ve kısa saklama | Geçmiş çıktılar panel DB'sindedir |
| P14 | Ajana izleme işiyle SSRF yaptırır | Bugünkü mevcut risk, betikle büyümez | Değişmez |

### 3.2 Ajan ele geçirilirse (o sunucuda root)

| # | Saldırı | Karşılık |
|---|---|---|
| A1 | Başka sunucuya yayılmak | Ajanda imza anahtarı yok; `K_ajan` yalnız kendi makbuzunu imzalar; panel ajana yalnız kendi işlerini verir |
| A2 | Sahte çıktı/makbuz | Yalnız kendi sunucusu için mümkün; panel `run_id` eşleşmesi + makbuz imzası ister; beklenmeyen `run_id` reddedilir |
| A3 | Panele saldırı: dev çıktı, depolanmış XSS, log enjeksiyonu, CSV formül | Sunucuda boyut sınırı; çıktı **yalnız metin** olarak gösterilir (`{@html}` yok); kontrol karakteri/ANSI temizlenir; CSV dışa aktarmada kaçış; bildirim metnine çıktı **girmez** |
| A4 | Çıktıda hazırlanmış metinle operatörü kandırmak (sahte "başarılı") | Arayüz "ajanın beyanı" olarak etiketler; çıkış kodu ve çıktı doğrulanmış sayılmaz |
| A5 | Bildirim makbuzlarını başka ajana yöneltmek (`run_id` tekrarı) | `run_id` panelde tek kullanımlık ve ajana bağlı |
| A6 | Token çalıp başka IP'den bağlanmak | Mevcut IP kilidi; `K_ajan` makbuz imzası token'dan bağımsızdır |

### 3.3 API anahtarı sızarsa

| # | Saldırı | Karşılık |
|---|---|---|
| K1 | `full` anahtarla betik başlatır/onaylar/kütüphaneye yazar | Bu uçlar **hiçbir API anahtarını kabul etmez**: `sessionOnly` yolu + `s.human` sarmalayıcı (`u.APIKeyName != ""` red); çerez + Bearer birlikte gelse de red |
| K2 | `read` anahtarla çıktı okur | `readScopeAllows` varsayılan kapalı; betik uçları listeye **eklenmez**, CLI çıktı göremez |
| K3 | `full` anahtarla kullanıcı/2FA/anahtar değiştirme | `sessionOnly` bunları zaten kapatıyor [kod]; korunur |
| K4 | Ajan token'ı sızar | Token iş almaya yarar, iş üretmeye yaramaz; gelen iş yine imzaya bağlı |
| K5 | Yönetici oturumu çalınır (XSS, çerez) | **İstek başına** step-up (TOTP, önbellek yok); `signed` sınıf etkilenmez |

### 3.4 Ortadaki adam

| # | Saldırı | Karşılık |
|---|---|---|
| M1 | TLS'i kırmadan araya girer | Betik özelliği yalnız `https://`; `PROBE_ALLOW_INSECURE` açıksa ajan betiği reddeder (loopback dahil değil) |
| M2 | Geçerli isteği tekrar oynatır | TLS içinde yapılamaz. `signed`: bilet tek ajan + tek `run_id` + süreli. `none`/`panel`: `run_id` panel üretir, ajan çiftini reddeder ama yeni `run_id` üretebilen panele karşı **tek sınır hız kotasıdır** |
| M3 | Bildirim/gövde değiştirir | İmza, özet |
| M4 | Çıktıyı okur (TLS sonlandıran vekil, ör. Cloudflare) | Vekil token ve çıktıyı görür; imzayı üretemez. Karar D9 |
| M5 | DNS yönlendirir (`dnscache` son bilinen IP) | TLS doğrulaması sürer; imza katmanı bağımsız korur |

### 3.5 Ek: imza anahtarı ve tedarik zinciri

| # | Saldırı | Karşılık |
|---|---|---|
| S1 | `K_yayın` sızar | Saldırgan izinli `id` altında yeni içerik imzalayabilir. Ajan iğnesiz kaydı daima `signed` sayar (§4), iğneli kayıtta hash tutmaz; yani **yeni içerik `K_onay` olmadan çalışmaz**. Dönüşüm: yerel `uptime betik anahtar` ile yuva değiştirilir, panelden geçmez |
| S2 | `K_onay` sızar tek başına | Bilet yalnız var olan imzalı betiği, yerel izin ve şema içinde açar; `signed` sınıfı panel ele geçmişse çalışır hale gelir, yeni içerik çalıştıramaz |
| S3 | İkisi birden sızar | İğnesiz kaydı olan sunucularda root; yalnız iğneli kayıtlar korunur. Bu yüzden ayrı anahtar, ayrı parola, sunucuda ya da CI'da değil. `betik ac` iki yuvaya aynı anahtarı reddeder |
| S4 | Ajan güncelleme anahtarı sızar | Tüm tasarımı aşar (imzalı yeni ikili). Betikten bağımsız mevcut risk; yönetici yerel "otomatik güncelleme kapalı" seçeneğini kullanabilmeli (§11) |
| S5 | Başlangıç betiklerine repo üzerinden kötü kod | Repo kaynak değil güven kökü değil: operatör kendi anahtarıyla **okuyup imzalar** (D1) |
| S6 | DB tek başına yazılır (SQL enjeksiyonu, yedek geri yükleme) | Panel ele geçirilmesinin alt kümesi; imza bozulur, yerel izin değişmez |

## 4. Anahtarlar ve güven kökü

| Anahtar | Tür | Yer | Yetkisi |
|---|---|---|---|
| `K_yayın` | Ed25519 | Operatör makinesi, parola korumalı (D2) | Betik bildirimini imzalar |
| `K_onay` | Ed25519, kişi başına | Operatör makinesi / donanım anahtarı (D2) | Tek çalıştırmalık bilet imzalar |
| `K_ajan` | Ed25519 | Ajanda, açılırken üretilir, 0600, `/var/lib/bekci-betik/` | Yalnız kendi makbuzunu imzalar; kimliği `host_key = sha256(pub)` |

Betik anahtarları ajan güncelleme anahtarından **ayrıdır**. Bekci açık kaynak ve kullanıcılar kendi sunucularında kurar; proje
anahtarının betik yetkisi olması bakımcıya tüm kurulumlarda root demektir. Bu nedenle güven kökü **ikiliye gömülmez**, her kurulumun
operatörüne aittir.

Ajan yapılandırması (yalnız yerel, root):

```text
# /etc/bekci/betik.json  (0640 root; panel ve güncelleme bu dosyaya hiç yazmaz)
{
  "enabled": true,
  "trust":   { "yayin": ["<pub1>", "<pub2>"], "onay": ["<pub3>"] },
  "groups":  ["web"],
  "allow":   [
    { "id": "journal-temizle", "min_approval": "none",   "sha256": "<gövde iğnesi>" },
    { "id": "servis-yeniden",  "min_approval": "signed" }   // iğnesiz: daima bilet
  ],                                                         // joker YOK
  "max_runs_per_hour": 6
}
```

**Etkin onay = en yüksek(imzalı `approval`, yerel `min_approval`, iğne yoksa `signed`).** Yerel değerler imzayla düşürülemez.
İğne `bekci-imza` çıktısındaki gövde özetidir; değişince yerelde `uptime betik izin` yeniden çalışır. Bu bilinçli bir sürtünmedir:
`K_onay` olmadan çalışabilen içerik her sunucuda root ile tek tek onaylanmış olur (D12).

Açma yalnız sunucuda root ile: `uptime betik ac --yayin-anahtar <pub> --onay-anahtar <pub> --grup web --izin journal-temizle=<sha256>`. Yerel alt komutlar: `izin`, `iptal <zarf>`, `anahtar`, `kapat`.
Komut `host_key` yazdırır; operatör panelde gördüğü ile karşılaştırır. Panel, kurulum komutu, ortam değişkeni, güncelleme bu ayarı
açamaz; **dosya yoksa ya da bozuksa özellik kapalıdır**. Kapatma: `uptime betik kapat` (yerel, anında, çalışan betiği öldürür).

## 5. İmzalı nesneler

Kanonik JSON kullanılmaz (ayrıştırıcı farkı saldırısı). Zarf, ham baytları imzalar:

```text
imza = Ed25519(anahtar, "bekci-script-v1\n" + tür + "\n" + payload_bayt)
zarf = { "key": "<anahtar kimliği>", "payload": "<base64>", "sig": "<base64>" }
```

`tür`: `manifest` | `ticket` | `revoke` | `receipt`. Her tür ayrı bağlam önekidir; biri diğeri yerine kullanılamaz. Ajan önce imzayı
doğrular, **sonra** ayrıştırır; ayrıştırma katı (bilinmeyen alan, yinelenen anahtar, bilinmeyen `schema` = red; boyut sınırı 128 KiB).
Özetler hep ham bayttan alınır: `manifest_sha256` = zarf `payload` baytları; `params_sha256` = panelden gelen parametre JSON baytları, ajan
tam bu baytları katı ayrıştırır (yeniden kodlama yok). Ajan üst sınırları kendisi zorlar: `not_after - not_before` en çok D4, bilet ömrü 15 dk,
`version` artışı bir seferde en çok 1000 (sızan anahtar sayacı kalıcı bozamasın).

**Bildirim** (`K_yayın`):

```text
schema 1 | id "servis-yeniden" | version 7 (monoton tamsayı) | sha256 <gövde>
interpreter "sh" (sabit liste) | run_as "root" | platforms ["linux"]
approval "none" | "panel" | "signed"   | groups ["web"]
params [ {name, type, ...} ]           | timeout_sec | output_max
min_interval_sec | max_per_day | jitter_sec | not_before | not_after
```

**Bilet** (`K_onay`, yalnız `signed` sınıfında; ömrü en çok 15 dk):

```text
schema 1 | run_id <128 bit rastgele> | host_key <hedef> | script_id | version | manifest_sha256
params_sha256 | issued_at | expires_at | approver
```

**Makbuz** (`K_ajan`): `seq` (ajan başına monoton), `prev` (önceki makbuz özeti), `run_id`, `manifest_sha256`, `params_sha256`,
`ticket_sha256`, `started_at`, `ended_at`, `exit_code`, `output_sha256`, `output_truncated`.

**İptal** (`K_yayın`): `{id, below_version}`; ajan kalıcı saklar. Panel iş listesinde iletir, adım 2 uygular; iletmemesi `not_after` ile sınırlıdır. Panel güvenilmezse yerel `uptime betik iptal <zarf>`.

### 5.1 İmza kapsamı

| Alan | Üreten | `none`/`panel` | `signed` |
|---|---|---|---|
| Gövde, şema, sınırlar, `approval`, `groups` | Operatör | `K_yayın` | `K_yayın` |
| Hedef sunucu | Panel seçer | **İmzasız**; yalnız yerel grup/izin sınırlar | Bilet `host_key` |
| Parametre değeri | Panel | **İmzasız**; yalnız şema sınırlar | Bilet `params_sha256` |
| `run_id`, zamanlama | Panel | **İmzasız**; yalnız hız kotası sınırlar | Bilet (süreli) |

`run_id` yalnız `^[0-9a-f]{32}$`; birim adına başka biçimde giremez.

## 6. Ajanda karar akışı ve yürütme

Sıra önemlidir; ilk hatada reddedilir, red nedeni panele yalnız kod olarak döner ve yerel kayda yazılır.

1. Betik açık mı (yerel dosya), durum dosyaları sağlam mı (§6.3), panel `https` mi, platform uygun mu (D5), saat `last_seen` geçmişine dönmüş mü.
2. Gelen iptal zarfları doğrulanıp kalıcı yazılır. Zarf imzası `trust.yayin` ile, **yerel** saate göre `not_before`/`not_after`, iptal, `version >= en yüksek çalışan`.
3. Gövde boyutu (64 KiB) ve `sha256`; `groups` ∩ yerel `groups` boş değil; `id` yerel `allow` içinde, iğne varsa tutuyor; etkin onay hesaplanır (§4).
4. Etkin onay `signed` ise bilet: imza `trust.onay`, `host_key` kendisi, `script_id/version/manifest_sha256/params_sha256` birebir, süre yerel saatle.
5. Parametreler şemaya göre doğrulanır (§6.1). `run_id` biçimi ve daha önce görülmemiş olması.
6. Hız: `min_interval_sec`, `max_per_day`, yerel saatlik kota. **Yalnız burada sayılır**, geçersiz istek kota tüketmez. `signed` kotası ve eşzamanlılık yuvası ayrıdır: düşük sınıf iş kuyruğu acil betiği kilitleyemez.
7. `run_id`, sayaç ve `last_seen` **tek atomik yazımla** kaydedilir + fsync; ancak sonra çalışır (en çok bir kez). `run_id` ömrü bittikten sonra budanır. Yazılamazsa çalışmaz. Dolu disk temizliği için durum dizini açılışta küçük bir alan ayırır.
8. `jitter_sec` beklemesi, sonra yürütme: yalnız `systemd-run` geçici birimi (§6.2).
9. Başarılı bitişte `version` tabanı yükseltilir; makbuz imzalanır, yerel kayda, sonra panele. Panel ulaşılamazsa sınırlı disk kuyruğu; dolunca en eski kuyruktan düşer ama `kayit.log`'dan silinmez, `seq` boşluğu oradan doğrulanır.

Red kayıtları hız ve boyutla sınırlıdır; panele yalnız red kodu döner, yerel kayıt dolmaz.

### 6.1 Parametreler

- Keyfi kabuk komutu yok; "komut" türü parametre yok; gövde şablonlanmaz (metne hiçbir değer gömülmez).
- Türler: `enum` (tercih edilen), `int` (min/max zorunlu), `bool`, `name` (yalnız `^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`, **tire ile başlayamaz**; seçenek enjeksiyonu önlenir). Serbest metin türü **yok**.
- Değerler yalnız ortam değişkeni olarak geçer: `BEKCI_P_<AD>`; argv'ye eklenmez. NUL ve satır sonu tür gereği geçemez.
- İmza aracı gövdeyi denetler: `shellcheck`, tırnaksız `$BEKCI_P_` ve `eval` / `sh -c "$..."` varsa imzalamayı reddeder.

### 6.2 Yürütme kafesi

Ajanın kendi birimi `NoNewPrivileges` ve dar yetenek kümesiyle çalıştığından, betik ajanın alt süreci olamaz; PID 1 birimi başlatır:

```text
systemd-run --collect --wait --pipe --expand-environment=no --unit=bekci-run-<run_id> \
  --setenv=BEKCI_RUN_ID --setenv=BEKCI_P_<AD> ... \
  -p RuntimeMaxSec=<timeout> -p TimeoutStopSec=10 -p MemoryMax=... -p CPUQuota=... -p TasksMax=... \
  -p PrivateTmp=yes -p ProtectHome=read-only -p PrivatePIDs=yes \
  -p InaccessiblePaths=-/etc/uptime-agent.env -p InaccessiblePaths=-/var/lib/bekci-betik \
  /bin/sh -c 'b=$(cat); exec </dev/null; eval "$b"'
```

- **Çalışan birimin ortamı çağırandan gelmez** [denendi]: `cmd.Env` birime geçmez; `--setenv=AD` (değersiz) çağıranın değerini alır, değer argv'ye girmez [denendi]. Token birime hiç geçmez.
- **Gövde** doğrulanan bellekten stdin ile verilir, diske yazılmaz. `--pipe` yalnız 0/1/2 geçirir; ek fd ve mühürlü `memfd` yürümez [denendi: 203/EXEC], ilk taslaktaki plan bu yüzden kaldırıldı. Sarmalayıcı önce gövdenin tamamını okur ve stdin'i kapatır; alt komutlar kalan gövdeyi yiyemez [denendi].
- `--expand-environment=no` şart: yoksa systemd `$$`, `$AD` ifadelerini ExecStart'ta açar [denendi: `$$` -> `$`]. En az systemd 254; `PrivatePIDs` için 257 gerekir. Eski sürümde `PrivatePIDs` düşürülür ve `/proc/<ajan>/environ` açık kalır (D5).
- `InaccessiblePaths` joker kabul etmez, deseni ile birim hiç başlamaz (226/NAMESPACE) [denendi]; açık yol + `-` öneki çalışır, dosya gizlenir [denendi, kontrollü].
- Kafes **hataya** karşıdır: root betik isterse aşabilir. Betik güvenilen koddur; imzanın ve iğnenin anlamı budur.
- Çıktı: stdout+stderr birleşik, `output_max` (üst sınır 256 KiB) baş + son tutulur, boru hep boşaltılır, betik tıkanmaz. `output_sha256` tam akışın özetidir.
- Zaman aşımı: `timeout_sec` (üst sınır 3600) -> SIGTERM -> `TimeoutStopSec=10` sonra SIGKILL [denendi: TERM yok sayan süreç sınırda öldü]. Cgroup arka plandaki süreçleri de öldürür; `docker prune` gibi dockerd içinde süren iş kapsam dışıdır, sınırı `--filter` ile küçültülür.
- **Yaşam döngüsü:** birim PID 1'dedir, ajanın ölümüyle ölmez [denenmedi]. Ajan kapanırken ve açılışta `bekci-run-*` birimlerini arar, `systemctl stop` ile durdurur, makbuzu `lost` yazar. Ajan yeni bir "betik çalışıyor" bayrağıyla kendi güncellemesini erteler (mevcut `errUpdateBusy` panel tarafı indirme reddidir, bu amaçla kullanılmaz).

### 6.3 Yerel durum

`/var/lib/bekci-betik/durum.json` (`version` tabanı, iptaller, sayaçlar, `run_id`, `last_seen`, `seq`) atomik yazılır (geçici dosya + rename + fsync) ve kendi özetini taşır.
Dosya eksik, bozuk ya da yazılamıyorsa **betik kapalıdır**; yeniden açma yerel `uptime betik onar` ister, panelden gelemez. Saat `last_seen`'den geri gitmişse red; ileri atlama reddedilmez ama yerel kayda ve panele yazılır (yanlış ileri saatle kilitlenme için `onar`).
**Kalan risk:** VM anlık görüntüsüne dönüş tüm durumu tutarlı biçimde geri alır, algılanamaz; bilet penceresi (15 dk) ve iptal etkilenebilir. Sunucu sahibinin kontrolündedir.

## 7. Panel tarafı

Panel kontrolleri güvenlik sınırı değildir (§1) ama çalınan oturumu, kaza ve içeriden kötü niyeti yakalar.

- **Yetki:** `sessionOnly` listesine betik yolları + yeni sarmalayıcı `s.human`: yönetici + gerçek oturum + API anahtarı değil + 2FA açık. Başlatma, onay, kütüphane, iptal, güven değişikliği bunu kullanır. Yalnız yönetici çıktı görür. Kısıtlı kullanıcı (`ServerIDs`) betik göremez.
- **Step-up:** her başlatma ve onayda TOTP, kod tek kullanımlık; önbellek yok. OIDC kullanıcısı TOTP kurmadan kullanamaz.
- **Kütüphane:** panel betik düzenlemez, yalnız imzalı zarf yükler. Yüklemede imza panelin bildiği açık anahtarlarla doğrulanır (yanlış dosyayı erken yakalamak için; ajan yine doğrular).
- **Onay sınıfları:**

| `approval` | Akış | Neye karşı korur | Panel ele geçirilirse |
|---|---|---|---|
| `none` | Başlatan + step-up | Çalınan oturum | Çalıştırılabilir (sınırlı, P7) |
| `panel` | İkinci yönetici panelde onaylar, kendi isteğini onaylayamaz (D3) | Tek kişinin hatası, çalınan tek hesap | **Atlatılabilir**, ajan bunu denetleyemez |
| `signed` | `bekci-imza bilet` çıktısı panele yapıştırılır, ajan doğrular | Panel dahil her şey | **Atlatılamaz** |

- **Toplu çalıştırma yok:** tek istek = tek sunucu (panel sınırıdır; ele geçmiş panele karşı ajan kotası + `jitter_sec`). Bir kullanıcıya saatte en çok N başlatma; sınır aşılırsa yeni başlatma durur ve bildirim gider.
- **Acil durdurma:** Ayarlar'da "Tüm betikleri durdur": yeni iş yok, çalışanlara iptal. Durdurma bir yetki vermediği için paneldedir; ajanda `uptime betik kapat`.
- **Bildirim:** her başlatma, onay, red, zaman aşımı, imza hatası ve bilinmeyen `host_key` değişimi bildirim kanalına gider. Çıktı gövdesi gönderilmez.
- **`host_key` değişimi:** yeni anahtar otomatik kabul edilmez; yönetici step-up ile onaylar. Ele geçirilmiş ajan yeniden kayıtla kimlik değiştiremez.

## 8. Değiştirilemez kayıt

Tek makinede "değiştirilemez" dürüstçe mümkün değildir. DB'ye yazabilen, kaydı da yazabilir. Hedef: **sessizce değiştirilemez** (kurcalama belli olur).

1. **Ajan kaydı:** `/var/log/bekci-betik/kayit.log` satır satır makbuz + journald. Panel erişemez. Sunucu ele geçmedikçe doğrudur.
2. **Makbuz zinciri:** `seq` + `prev`. Silinen makbuz boşluk bırakır, sahte makbuz imza tutmaz.
3. **Panel kaydı:** `script_runs` yalnız eklenir: `hash = sha256(önceki_hash || olay)`; zincir başı `pg_advisory_xact_lock` altında okunur, eklemeler sıralıdır. Bölüm sütunu panelin **ekleme anıdır**; ajanın zamanı `data` içindedir, geç gelen makbuz eski parçaya düşmez. UPDATE/DELETE/TRUNCATE tetikleyiciyle reddedilir (uygulama hatası ve basit SQLi'ye karşı; DB sahibi tetikleyiciyi kaldırabilir, `drop_chunks` satır tetikleyicisine takılmaz).
4. **Dış çapa:** günlük zincir başı + son `checkpoint`, panelden bildirim kanalına ve (D7) panel dışı bir yere yazılır. Kaynak panel olduğundan çapa **susturulabilir**; `bekci-imza dogrula --bekle` çapa gelmeyince uyarı veren panel dışı zamanlı denetimdir (operatörün makinesi). İlk sürümde bu kurulmazsa çapa yalnız geçmişe dönük kanıttır, canlı alarm değildir.
5. **Doğrulama:** `bekci-imza dogrula <dışa aktarım> [ajan kayit.log ...]` makbuz imzası, zincir, çapayı denetler ve ajan `seq`/`prev` başını panel kaydıyla karşılaştırır (son makbuzların kırpılması yalnız böyle görünür). Panel arayüzündeki "doğrulandı" rozeti ele geçirilmiş panelde yalan söyleyebilir, tek otorite çevrimdışı araçtır.
6. **Saklama:** yalnız zincirin en eski öneki düşürülür; önce son hash `script_log_checkpoint` tablosuna yazılır ve çapaya girer. `dogrula`, checkpoint yaşının saklama süresinden (D10) küçük olmamasını denetler; erken checkpoint "yasal saklama" gibi görünemez. Sıkıştırılmış parçada tetikleyici ve sıra davranışı 690'da denenir [denenmedi].
7. **Panel yedek geri yüklemesi** zinciri geriye alır: yeniden başlayan zincir çapayla çatallanır. Geri yükleme `chain_fork` olayı yazar ve yeni çapa üretir, `dogrula` bunu gösterir.

## 9. 600 belgesiyle uyum (şema)

Betik özelliği PostgreSQL ister (Timescale varsayılan, düz PG uyumlu). SQLite kullanımdan kalktığı için orada **kapalıdır** (D8).

| Tablo | Tür | Not |
|---|---|---|
| `scripts` | normal | `id`, `version`, zarf, gövde, yüklenme; satır değiştirilmez, yeni sürüm yeni satır |
| `script_hosts` | normal | `probe_id`, `host_key`, onay zamanı/kişi |
| `script_runs` | **hypertable** (30 gün parça, 90 gün sonra sıkıştırma, 365 gün) | 600 §3.2'deki satır korunur; ama satır güncellenmez: `(run_id, seq, kind, time, hash, prev, data)` olay akışı, son durum görünümle okunur. Yalnız ekleme hypertable sıkıştırmasıyla uyumludur |
| `script_run_outputs` | normal | Kısaltılmış çıktı, 90 gün (D10); sır içerebilir, çapaya yalnız özeti girer |
| `script_log_checkpoint` | normal | Saklama sonrası zincir sürekliliği |

Düz PG'de `script_runs` normal tablo olur; saklama aynı `checkpoint` adımıyla, ayrı dar yetkili bir `SECURITY DEFINER` işleviyle önek siler (tetikleyici yalnız ona izin verir).

## 10. Eski/yeni uyumu (ajanlar otomatik güncelleniyor)

| Eşleşme | Davranış |
|---|---|
| Eski ajan, yeni panel | Yetenek başlığı yok (`X-Probe-Scripts: schemas=1;state=on\|off`), panel `script_jobs` alanı **hiç göndermez**, arayüz "desteklenmiyor" der |
| Yeni ajan, eski panel | Ajan başlık gönderir, eski panel yok sayar; iş gelmez. Betik kodu hiç devreye girmez |
| Yeni ajan betik kapalı, yeni panel | Yetenek `kapalı` bildirilir, panel iş üretmez; yine gelse ajan reddeder |
| Ajan geri dönerse | Yerel dosya durur, eski ajan onu okumaz; sayaç/`run_id` dosyaları korunur |
| Ajan güncellemesi | `/etc/bekci/betik.json` hiç değişmez; özellik güncellemeyle **kendiliğinden açılamaz** |
| Bilinmeyen `schema` | Ajan reddeder; panel yalnız başlıktaki `schemas` ile uyumlu sürümü teklif eder |
| Panel eski sürüme dönerse | `script-result` 404 alır; makbuz sınırlı kuyrukta bekler, dolunca `kayit.log`'da kalır (§6 adım 9) |

Tek yeni alan `script_jobs` (`omitempty`); `POST /api/probe/script-result` yalnız yeni ajanda çağrılır. 690'da gerçek ikililerle test edilir (v1.6.0 ajan + yeni panel, yeni ajan + v1.6.0 panel).

## 11. Bu işin dışında ama önerilen sertleştirme

- Kurulum betiği ikiliyi ajan imzasıyla da doğrulasın (bugün özet paneldendir, P10).
- `uptime` ajanında yerel "otomatik güncellemeyi reddet" ayarı (S4); bugün yalnız panel ayarı var.

## 12. Kararlar (D1-D15 onaylandı 2026-11-04 (yönetici))

Kullanıcı karar yetkisini yöneticiye verdi; aşağıdaki öneriler aynen onaylandı. Fable yeniden incelemesi yok (filtre kesiyor); yerine 690 sonunda bağımsız bir Opus incelemesi yapılır.

| # | Karar | Onaylanan |
|---|---|---|
| D1 | Güven kökü: yalnız operatör anahtarı mı, proje başlangıç anahtarı da mı? | Yalnız operatör; başlangıç betikleri repoda, operatör okuyup imzalar |
| D2 | `K_yayın` ve `K_onay` nerede, ayrı mı? | Ayrı; ikisi de kullanıcının kendi makinesinde parola korumalı, bu sunucuda ve CI'da değil; yedek çevrimdışı. Donanım anahtarı sonra |
| D3 | Tek yöneticili kurulumda `panel` onayı (bugünkü kurulum tek yönetici) | Aynı kişi kendi isteğini onaylayamaz; ikinci yönetici yoksa `panel` sınıfı betik `signed` akışına düşer |
| D4 | `not_after` azami süre | 180 gün; süresi dolunca yeniden imza gerekir (iptal penceresi de budur) |
| D5 | Platform kapsamı | Yalnız Linux + systemd >= 254 + yerli ajan; `PrivatePIDs` olmayan sürümde (254-256) token gizliliği zayıf, kabul. Docker ajanı, Windows, OpenRC reddeder |
| D6 | `run_as` değerleri | Yalnız `root` ve `nobody`; temizlik root ister, kafes §6.2 |
| D7 | Dış çapa hedefi | İlk aşama bildirim kanalı + `bekci-imza` ile yerel dosya; S3 Object Lock sonra |
| D8 | SQLite'ta betik | Kapalı |
| D9 | TLS sonlandıran vekil (canlı panel Cloudflare arkasında): çıktı vekilde görünür | İlk sürümde kabul, sır basan betik imzalanmaz; uçtan uca şifreli çıktı sonraki iş |
| D10 | Saklama | Kayıt 365 gün, çıktı 90 gün |
| D11 | Risk sınıfı tanımı (§1) ve başlangıç betik sınıfları (§13) | Tablo gibi |
| D12 | Yerel içerik iğnesi: `signed` olmayan her betik için sunucuda root ile `sha256` kaydı (betik güncellemesi her sunucuda yerel komut ister) | Kabul; filoda yayma SSH/Ansible ile, panelden değil |
| D13 | Yerel durum bozulursa kapalı kalsın, `uptime betik onar` yerel ister | Kabul |
| D14 | `K_ajan` yenileme: sunucu yeniden kurulunca yeni `host_key` panelde step-up ile onaylanır, eski makbuz zinciri kapanır | Kabul |
| D15 | `sertifika-yenile` sınıfı (`caddy reload` ve `certbot renew` servis kesebilir) | `signed` |

## 13. Başlangıç betikleri (690)

| Betik | Parametre | `approval` | Sınır | Not |
|---|---|---|---|---|
| `journal-temizle` | `gun` int 7..90 | none | 6 saatte 1 | `journalctl --vacuum-time` |
| `paket-onbellek` | yok | none | 6 saatte 1 | `apt-get clean` / `dnf clean packages` |
| `docker-derleme-temizle` | `saat` int 24..720 | panel | 6 saatte 1 | `docker builder prune -f --filter until=` |
| `docker-imaj-temizle` | `saat` int 168..720 | panel | Günde 1 | `docker image prune -f --filter until=`; `-a` yok, **`system prune` ve volume asla** |
| `servis-yeniden` | `servis` enum (grup başına ayrı imzalı bildirim: `servis-yeniden-web` vb.) | signed | 10 dk'da 1 | `sshd`, ajan, `docker`, veritabanı listeye alınmaz |
| `sertifika-yenile` | `arac` enum certbot/caddy | signed | Günde 1 | `certbot renew` / `systemctl reload caddy` |

Zincirleme: `none` betikleri tek başına ya da birlikte servis kesmemeli, tanık kayıt silmemeli; her yeni betik imzalanmadan önce bu ölçütle gözden geçirilir.

## 14. 690 kabul testleri (saldırı)

Her biri gerçek ikili ile, red nedeni kodu doğrulanır; hiçbirinde betik başlamamalı (işaret dosyası oluşmaz):

imzasız · yanlış anahtar · değiştirilmiş gövde · değiştirilmiş bildirim alanı · başka tür zarf (`ticket` yerine `manifest`, ajan güncelleme imzası) · süresi geçmiş / henüz başlamamış · panel saati oynatılmış · sürüm geri alma · iptal edilmiş · izinsiz grup · yerel `allow` dışı · bilet yok / başka ajana / başka parametre / süresi geçmiş / tekrar · `run_id` tekrarı (kilitlenme sonrası dahil) · hız sınırı aşımı · özellik kapalı · dosya yok/bozuk · `http://` panel · Docker ajan · şema dışı parametre, tire ile başlayan ad, NUL, satır sonu, `$(...)` · API anahtarı (full ve read) başlatma · step-up'sız başlatma · kendi isteğini onaylama · bilinmeyen `schema` · bilinmeyen alan, yinelenen anahtar, büyük zarf · `K_yayın` ile aynı `id`de yeni gövde (iğneli ve iğnesiz) · imzalı `approval` yerel tabanın altında · durum dosyası silinmiş/bozuk/disk dolu · saat geri · geçersiz `run_id` biçimi · dev `version` · iki yuvada aynı anahtar · reddedilen istek kota tüketmez · çerez + Bearer birlikte.

Kabul: ayrıca gerçek birimle yürütme (parametre ortamda, gövde stdin'de, `$` açılmıyor); token/`K_ajan` betik ortamında görünmez; dev çıktı ve zaman aşımı çocuk süreçleriyle ölür; ajan betik sırasında ölürse `lost` + birim durur; panelde XSS çıktısı metin kalır; kayıt satırı silinince, son makbuz kırpılınca ve erken checkpoint'te `dogrula` hata verir; sıkıştırılmış parçada zincir doğrulanır.

## 15. Bağımsız inceleme kaydı (2026-11-04)

**Fable incelemesi TAMAMLANMADI.** İstenen `model: fable` ajanı okumayı ve denemeleri bitirdi, ama rapor yazılırken Fable güvenlik filtresi `[cyber]` isteği kesti; elde yalnız birinci bulgunun başlığı ve §2 düzeltmesi kaldı. Filtre aşılmaya çalışılmadı. Aynı kapsam **Opus** ile yeniden incelendi (yazardan bağımsız bağlam; Fable incelemesinin yerine geçmez). Aşağıdaki bulgular Opus'undur; systemd denemeleri bu sunucuda tekrarlandı.

| # | Önem | Bulgu | Durum |
|---|---|---|---|
| 1 | Kritik | `allow` içeriğe bağlı değil, tek `K_yayın` tüm güvenceyi bozuyor (Fable da aynı başlıkla başlamıştı) | Düzeltildi: §1, §3.5, §4 (iğne, etkin onay) |
| 2 | Kritik | `cmd.Env` ve `--pipe` + memfd planı çalışmıyor | Düzeltildi, denendi: §6.2 (`--setenv`, stdin sarmalayıcı) |
| 3 | Yüksek | `InaccessiblePaths` joker kabul etmiyor | Düzeltildi, denendi: §6.2 |
| 4 | Yüksek | `none`/`panel` alanları imzasız, M2 fazla iddialı, `run_id` biçimi | Düzeltildi: §5.1, M2, P7 |
| 5 | Yüksek | Yerel durum kaybında davranış tanımsız | Düzeltildi: §6.3; anlık görüntü kalan risk |
| 6 | Yüksek | Kota sırası ve ortak kuyruk DoS'a dönüşebilir | Düzeltildi: §6 adım 6-7 |
| 7 | Yüksek | Meşru betik zinciri (journald tanığı, filo eşzamanı, gruba göre enum) | Düzeltildi: P7, P12, §13 |
| 8 | Yüksek | Saklama ile hash zinciri, geç makbuz, TRUNCATE | Tasarımda düzeltildi: §8, §9; davranış 690'da denenecek |
| 9 | Yüksek | Çapa panelden geliyor, `dogrula` ajanı görmüyor | Düzeltildi: §8; panel dışı denetim D7'ye bağlı |
| 10 | Orta | Anahtar iptali, üst sınırlar ajanda yok, aynı anahtar iki yuvada | Düzeltildi: §4, §5 |
| 11 | Orta | `params_sha256` bayt tanımı yok | Düzeltildi: §5 |
| 12 | Orta | `last_seen`, iptal alımı, sayaç zamanlaması akışta yok | Düzeltildi: §6 adım 1, 2, 9 |
| 13 | Orta | §2 birim sıkılaştırması yanlış, K3 ve §11 eski | Düzeltildi: §2, K3, §11 |
| 14 | Orta | `errUpdateBusy` uymuyor, `TimeoutStopSec` yok, birim ömrü | Düzeltildi: §6.2; birim ömrü denenmedi |
| 15 | Orta | Yetenek başlığı, karma filo, D3 çelişkisi, eksik kararlar | Düzeltildi: §10, §12 |

Doğrulanmamış: düzeltilmiş belge yeniden bağımsız incelenmedi; Fable incelemesi kullanıcı onayıyla ayrıca denenebilir.
