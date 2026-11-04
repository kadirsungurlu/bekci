# Betik çalıştırma güvenlik tasarımı (iş 680)

Durum: TASARIM (2026-11-04). Kod yok. 690 bu belge ve §12 kararları onaylanmadan başlamaz.
Üst belge: `docs/TIMESCALE-TASARIM.md` (600); `scripts` / `script_runs` için orada "Ayrıntı 680" denen konu buradadır (§9).

Etiketler: **[kod]** bugünkü kodda doğrulandı (dosya verildi); **[tasarım]** henüz yok, 690'da yazılır ve denenir.

---

## 1. Neden tehlikeli, temel ilke

Bugün panel bir ajana yalnız izleme işi verebilir. Betik özelliği, panelden gelen bir isteğin sunucuda **root olarak kod
çalıştırması** demektir: tek panel, bütün filonun anahtarı olur. Tasarımın tek cümlelik amacı:

> **Panel tamamen ele geçirilse bile saldırgan, operatörün önceden imzaladığı ve o sunucunun yerel ayarla izin verdiği
> betiklerin dışında hiçbir şey çalıştıramaz; yüksek riskli betikleri ise hiç çalıştıramaz.**

Sonuç olarak: **panelin elinde yetki veren hiçbir özel anahtar bulunmaz.** Panel bir posta memurudur; veritabanı, panel
ayarları ve panel arayüzü güven kökü değildir. Ajan her kararı yerel olarak verir. Panelde yapılan denetimler (izinli sunucu
listesi, panel içi onay) kolaylık ve insan hatası korumasıdır, güvenlik sınırı **değildir**; güvenlik sınırı olan her şey
ajanda ya da panelin üretemeyeceği bir imzadadır.

## 2. Bugünkü durum (kodda doğrulandı)

| Gerçek | Betik tasarımına etkisi | Kaynak |
|---|---|---|
| systemd ajanı `User=` olmadan, yani **root** çalışır; sıkılaştırma yönergesi yok | Betik varsayılan olarak root olur; süreç sınırı ayrıca kurulmalı | `api/servers.go:528` |
| `PROBE_TOKEN` ortam değişkenidir (`EnvironmentFile`) | Alt sürece miras geçerse betik token'ı görür; `cmd.Env` açık liste olmalı | `api/servers.go:528` |
| Docker ajanı `--cap-drop ALL`, no-new-privileges; host erişimi yok | Docker ajanında betik anlamsız ve açılmamalı | `api/installcmd.go` |
| Kurulum komutunu panel üretir; ikili bütünlüğü **komuta gömülü SHA-256** ile sağlanır, özeti de panel yazar | Ele geçirilmiş panel yeni kurulumu zehirler; güven kökü kurulum komutuyla gelemez | `api/installcmd.go:17` |
| `GET /api/probe/binary` herkese açık, gövde panelden gelir | Otomatik güncellemede imza korur, elle kurulumda korumaz | `api/agentbin.go` |
| Ajan güncellemesi gömülü anahtarla Ed25519 imzalı, önekli (`bekci-agent-manifest-v1`), eski/aynı sürüm reddedilir | İyi kalıp; ama betik için **aynı anahtar ve önek kullanılmaz**. Bildirimde süre sonu yok | `agentupdate/agentupdate.go` |
| `http://` panel yalnız `PROBE_ALLOW_INSECURE=1` ile kabul edilir; ajan istemcisinde `InsecureSkipVerify` yok | TLS varsayılan; betik özelliği http'de reddedilmeli | `probe/probe.go:276` |
| Ajan saat farkını panelin `server_time` değerinden öğrenir (`skewMs`) | Süre denetimi bu değeri **kullanamaz**: panel saati geri alıp süresi geçmiş imzayı diriltir | `probe/probe.go` pollJobs |
| Ajan-panel kimliği paylaşılan token, DB'de SHA-256 özeti; ajanın kendine ait anahtarı yok | Ajanın kanıtlanabilir imzası yok; `K_ajan` eklenir (§4) | `store/probes.go` |
| `full` API anahtarı sahibinin rolünü alır, `s.admin` geçer; `read` izin listesindedir | Mevcut `s.admin` ile yazılırsa sızan anahtar betik başlatır. Yeni sarmalayıcı şart (§7) | `api/apikeys.go` |
| `audit_log` saklama süresinde `DELETE` ile silinir, yazılabilir | "Değiştirilemez kayıt" için kullanılamaz | `store/cleanup.go:115` |
| Paneli indiren imza anahtarı GitHub Actions sırrında | Bu anahtar sızarsa otomatik güncellenen tüm ajanlarda kod çalışır. **Bu risk bugün de var**, betikten bağımsız | `agentupdate/agentupdate.go` |

## 3. Tehdit modeli

Varlıklar: sunucularda root kod çalıştırma, servis sürekliliği, betik çıktısındaki gizli bilgi, kaydın doğruluğu.
Kapsam dışı: ajanın sunucusunda zaten root olan saldırgan o sunucuda her şeyi yapar; amaç oradan **başka yere** yayılmamasıdır.

### 3.1 Panel ele geçirilirse (DB, ikili, oturum, panel sırları dahil tamamı)

| # | Saldırı | Karşılık | Kalan risk |
|---|---|---|---|
| P1 | Kendi kabuk komutunu gönderir | Ajan yalnız `K_yayın` imzalı gövdeyi çalıştırır; imza özel anahtarı panelde yok | Yok |
| P2 | İmzalı betiğin gövdesini/parametre şemasını/onay sınıfını/sınırlarını değiştirir | Hepsi imzalı bildirimde; ajan gövde özetini çalıştırmadan önce doğrular | Yok |
| P3 | Betiği izin verilmemiş sunucuda çalıştırır | Sunucu izni panelde değil: bildirimdeki `groups` ile ajanın yerel grubu kesişmeli, yerel `allow` listesi de izin vermeli | Yok |
| P4 | `imzalı` sınıf betiği onaysız çalıştırır, onayı sahte gösterir | Ajan `K_onay` biletini ister; bilet tek ajan + tek çalıştırma + parametre özetine bağlı | Yok |
| P5 | Eski, açığı olan imzalı sürüme geri döndürür | Ajan betik başına görülen en yüksek `version` değerini kalıcı tutar | Yeni kurulan ajan geçmişi bilmez; `not_after` sınırlar |
| P6 | Süresi geçmiş imzayı saat oynatarak diriltir | Süre denetimi yerel saatle; panel `server_time` kullanılmaz | Sunucu NTP'si de saldırgandaysa sınırlı; `last_seen` yerel kayıt saatin geri gitmesini reddeder |
| P7 | Geçerli **düşük sınıf** betiği geçerli parametreyle kötüye kullanır (hizmet döngüsel yeniden başlatma, önbellek silme) | Bildirimde `min_interval_sec` ve `max_per_day`, ajanda yerel sayaçla zorlanır; risk taşıyan betik `imzalı` sınıftadır | **Kabul edilen kalan risk:** sınırlar içinde kullanım mümkündür |
| P8 | Kullanıcıya sahte onay ekranı gösterir (A sunucusu yazar, B için bilet ister) | Bilet aracı (`bekci-imza`) ekranı **biletin kendisinden** üretir, `host_key` yerel iğne listesiyle ada çevrilir, panel metnine güvenmez | Operatör dikkatine bağlı |
| P9 | İptal listesini (revoke) ajana iletmez | `not_after` en çok D4 kadar; iptal yalnız bu süreyi kısaltır | D4 süresi kadar pencere |
| P10 | Ajanı zehirli kurulum komutuyla yeniden kurdurur | Betik açma ayrı yerel komuttur, güven anahtarı panelden gelmez; açık anahtar parmak izi operatörün makinesinden girilir. Yeniden kurulumda kurulum komutu çalıştıran kişi paneli güvenilir sayıyordur | **Kalan risk:** zehirli ikili kurulduysa her şey biter; ikili imzasını kurulum betiği doğrulamalı (öneri, §11) |
| P11 | Kayıtları siler/değiştirir | Ajan zincirli imzalı makbuz + ajan yerel kaydı + dış çapa (§8) | Çapadan sonraki panel kaydı silinebilir ama ajan tarafı durur, fark görünür |
| P12 | Bildirimleri susturur (çalıştırma haberi gitmez) | Ajan yerel kaydı (journald; operatör uzak log'a yollayabilir) | Yalnız panel kanalı varsa sessiz kalabilir |
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
| K1 | `full` anahtarla betik başlatır/onaylar/kütüphaneye yazar | Bu uçlar **hiçbir API anahtarını kabul etmez** (`s.human` sarmalayıcı; `u.APIKeyName != ""` red) |
| K2 | `read` anahtarla çıktı okur | `readScopeAllows` varsayılan kapalı; betik uçları listeye **eklenmez**, CLI çıktı göremez |
| K3 | `full` anahtarla kendine kullanıcı/2FA kapatma | Mevcut risk; step-up ve bildirim hesabı ayrı; anahtar yönetim uçları da `s.human` olmalı (§11 öneri) |
| K4 | Ajan token'ı sızar | Token iş almaya yarar, iş üretmeye yaramaz; gelen iş yine imzaya bağlı |
| K5 | Yönetici oturumu çalınır (XSS, çerez) | Başlatma anında step-up (TOTP, 5 dk geçerli değil, **istek başına**); `imzalı` sınıf etkilenmez |

### 3.4 Ortadaki adam

| # | Saldırı | Karşılık |
|---|---|---|
| M1 | TLS'i kırmadan araya girer | Betik özelliği yalnız `https://`; `PROBE_ALLOW_INSECURE` açıksa ajan betiği reddeder (loopback dahil değil) |
| M2 | Geçerli isteği tekrar oynatır | `run_id` tek kullanımlık; ajan **çalıştırmadan önce** diske yazar ve fsync yapar; süreli |
| M3 | Bildirim/gövde değiştirir | İmza, özet |
| M4 | Çıktıyı okur (TLS sonlandıran vekil, ör. Cloudflare) | Vekil token ve çıktıyı görür; imzayı üretemez. Karar D9 |
| M5 | DNS yönlendirir (`dnscache` son bilinen IP) | TLS doğrulaması sürer; imza katmanı bağımsız korur |

### 3.5 Ek: imza anahtarı ve tedarik zinciri

| # | Saldırı | Karşılık |
|---|---|---|
| S1 | `K_yayın` sızar | Ajanda 2 güven yuvası (dönüşüm); yerel `allow` + grup + hız sınırı + `imzalı` sınıf yine korur; acil iptal: yerel kapatma |
| S2 | `K_onay` sızar tek başına | Betik imzası olmadan işe yaramaz; bilet yalnız var olan imzalı betiği açar |
| S3 | İkisi birden sızar | O gruptaki sunucularda root. Bu yüzden ayrı anahtar, parola korumalı, sunucuda değil; hiçbiri bu sunucuda ya da CI'da tutulmaz |
| S4 | Ajan güncelleme anahtarı sızar | Tüm tasarımı aşar (imzalı yeni ikili). Betikten bağımsız mevcut risk; yönetici yerel "otomatik güncelleme kapalı" seçeneğini kullanabilmeli (§11) |
| S5 | Başlangıç betiklerine repo üzerinden kötü kod | Repo kaynak değil güven kökü değil: operatör kendi anahtarıyla **okuyup imzalar** (D1) |
| S6 | DB tek başına yazılır (SQL enjeksiyonu, yedek geri yükleme) | Panel ele geçirilmesinin alt kümesi; imza bozulur, yerel izin değişmez |

## 4. Anahtarlar ve güven kökü

| Anahtar | Tür | Yer | Yetkisi |
|---|---|---|---|
| `K_yayın` | Ed25519 | Operatör makinesi, parola korumalı (D2) | Betik bildirimini imzalar |
| `K_onay` | Ed25519, kişi başına | Operatör makinesi / donanım anahtarı (D2) | Tek çalıştırmalık bilet imzalar |
| `K_ajan` | Ed25519 | Ajanda, açılırken üretilir, 0600, `/var/lib/bekci-betik/` | Yalnız kendi makbuzunu imzalar; kimliği `host_key = sha256(pub)` |
| `K_kayıt` | Yok (panel imzası yok) | - | Panelin imzası güvenli sayılmaz; hash zinciri dışarı çapalanır |

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
  "allow":   ["journal-temizle", "paket-onbellek"],   // ["*"] YOK: joker desteklenmez
  "max_runs_per_hour": 6
}
```

Açma yalnız sunucuda root ile: `uptime betik ac --yayin-anahtar <pub> --onay-anahtar <pub> --grup web --izin journal-temizle`.
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

**Bildirim** (`K_yayın`):

```text
schema 1 | id "servis-yeniden" | version 7 (monoton tamsayı) | sha256 <gövde>
interpreter "sh" (sabit liste) | run_as "root" | platforms ["linux"]
approval "none" | "panel" | "signed"   | groups ["web"]
params [ {name, type, ...} ]           | timeout_sec | output_max
min_interval_sec | max_per_day         | not_before | not_after
```

**Bilet** (`K_onay`, yalnız `signed` sınıfında; ömrü en çok 15 dk):

```text
schema 1 | run_id <128 bit rastgele> | host_key <hedef> | script_id | version | manifest_sha256
params_sha256 | issued_at | expires_at | approver
```

**Makbuz** (`K_ajan`): `seq` (ajan başına monoton), `prev` (önceki makbuz özeti), `run_id`, `manifest_sha256`, `params_sha256`,
`ticket_sha256`, `started_at`, `ended_at`, `exit_code`, `output_sha256`, `output_truncated`.

**İptal** (`K_yayın`): `{id, below_version}`; ajan kalıcı saklar. Panel iletir ama iletmemesi `not_after` ile sınırlıdır.

## 6. Ajanda karar akışı ve yürütme

Sıra önemlidir; ilk hatada reddedilir, red nedeni panele yalnız kod olarak döner ve yerel kayda yazılır.

1. Betik açık mı (yerel dosya), panel `https` mi, platform uygun mu (D5), aynı anda başka betik var mı (ajan başına 1).
2. Zarf imzası `trust.yayin` ile, ajanın **yerel** saatine göre `not_before`/`not_after`, iptal, `version >= görülen en yüksek`.
3. Gövde boyutu (64 KiB) ve `sha256`; `groups` ∩ yerel `groups` boş değil; `id` yerel `allow` içinde.
4. Hız: `min_interval_sec`, `max_per_day`, yerel `max_runs_per_hour`; sayaçlar diskte, yeniden başlatmaya dayanıklı.
5. `approval=signed` ise bilet: imza `trust.onay`, `host_key` kendisi, `script_id/version/manifest_sha256/params_sha256` birebir, süre yerel saatle.
6. Parametreler şemaya göre doğrulanır (§6.1).
7. `run_id` daha önce görülmemiş olmalı; görülmüş listesine **yazılır + fsync**, sonra çalışır (en çok bir kez). Kilitlenme sonrası yeniden çalıştırma yok; kayıt `lost` olur.
8. Yürütme: yalnız `systemd-run` geçici birimi (§6.2).
9. Makbuz imzalanır, yerel kayda, sonra panele. Panel ulaşılamazsa disk kuyruğu (sınırlı) ile sonra gönderilir.

### 6.1 Parametreler

- Keyfi kabuk komutu yok; "komut" türü parametre yok; gövde şablonlanmaz (metne hiçbir değer gömülmez).
- Türler: `enum` (tercih edilen), `int` (min/max zorunlu), `bool`, `name` (yalnız `^[A-Za-z0-9][A-Za-z0-9_.@-]{0,63}$`, **tire ile başlayamaz**; seçenek enjeksiyonu önlenir). Serbest metin türü **yok**.
- Değerler yalnız ortam değişkeni olarak geçer: `BEKCI_P_<AD>`; argv'ye eklenmez. NUL ve satır sonu tür gereği geçemez.
- İmza aracı gövdeyi denetler: `shellcheck`, tırnaksız `$BEKCI_P_` ve `eval` / `sh -c "$..."` varsa imzalamayı reddeder.

### 6.2 Yürütme kafesi

- `systemd-run --collect --wait --pipe --unit=bekci-run-<run_id>`; `RuntimeMaxSec`, `MemoryMax`, `CPUQuota`, `TasksMax`, `InaccessiblePaths=/etc/uptime-*.env /var/lib/bekci-betik`, `ProtectHome=read-only`, `PrivateTmp=yes`. Cgroup sayesinde arka plana kaçan süreçler de zaman aşımında ölür.
- `PrivateTmp` ve `Inaccessible` kuralı `K_ajan` ile token'ı root betikten saklar; root betik isterse kafesi aşabilir, bu yüzden betik **güvenilen koddur** (imzanın anlamı budur), kafes hataya karşıdır.
- Gövde diske yazılmaz: mühürlü `memfd` (`F_SEAL_WRITE`), doğrulanan bayt ile çalışan bayt aynıdır (TOCTOU yok).
- `cmd.Env` boş başlar: `PATH=/usr/sbin:/usr/bin:/sbin:/bin`, `LANG=C`, `BEKCI_RUN_ID`, `BEKCI_P_*`. Ajan ortamı devralınmaz, `PATH` panelden gelmez.
- Çıktı: stdout+stderr birleşik, `output_max` (üst sınır 256 KiB) baş + son tutulur, boru hep boşaltılır, betik tıkanmaz. `output_sha256` tam akışın özetidir.
- Zaman aşımı: `timeout_sec` (üst sınır 3600), SIGTERM, 10 sn sonra SIGKILL.
- Otomatik güncelleme betik çalışırken beklemeye alınır (`errUpdateBusy` kalıbı). Ajanın kapanması betiği öldürür; açılınca `lost` makbuzu yazılır.

## 7. Panel tarafı

Panel kontrolleri güvenlik sınırı değildir (§1) ama çalınan oturumu, kaza ve içeriden kötü niyeti yakalar.

- **Yetki:** yeni sarmalayıcı `s.human`: yönetici + gerçek oturum + API anahtarı değil + 2FA açık. Başlatma, onay, kütüphane, iptal, güven değişikliği bunu kullanır. Yalnız yönetici çıktı görür. Kısıtlı kullanıcı (`ServerIDs`) betik göremez.
- **Step-up:** her başlatma ve onayda TOTP, kod tek kullanımlık; önbellek yok. OIDC kullanıcısı TOTP kurmadan kullanamaz.
- **Kütüphane:** panel betik düzenlemez, yalnız imzalı zarf yükler. Yüklemede imza panelin bildiği açık anahtarlarla doğrulanır (yanlış dosyayı erken yakalamak için; ajan yine doğrular).
- **Onay sınıfları:**

| `approval` | Akış | Neye karşı korur | Panel ele geçirilirse |
|---|---|---|---|
| `none` | Başlatan + step-up | Çalınan oturum | Çalıştırılabilir (sınırlı, P7) |
| `panel` | İkinci yönetici panelde onaylar, kendi isteğini onaylayamaz (D3) | Tek kişinin hatası, çalınan tek hesap | **Atlatılabilir**, ajan bunu denetleyemez |
| `signed` | `bekci-imza bilet` çıktısı panele yapıştırılır, ajan doğrular | Panel dahil her şey | **Atlatılamaz** |

- **Toplu çalıştırma yok:** tek istek = tek sunucu. Bir kullanıcıya saatte en çok N başlatma; sınır aşılırsa yeni başlatma durur ve bildirim gider.
- **Acil durdurma:** Ayarlar'da "Tüm betikleri durdur": yeni iş yok, çalışanlara iptal. Durdurma bir yetki vermediği için paneldedir; ajanda `uptime betik kapat`.
- **Bildirim:** her başlatma, onay, red, zaman aşımı, imza hatası ve bilinmeyen `host_key` değişimi bildirim kanalına gider. Çıktı gövdesi gönderilmez.
- **`host_key` değişimi:** yeni anahtar otomatik kabul edilmez; yönetici step-up ile onaylar. Ele geçirilmiş ajan yeniden kayıtla kimlik değiştiremez.

## 8. Değiştirilemez kayıt

Tek makinede "değiştirilemez" dürüstçe mümkün değildir. DB'ye yazabilen, kaydı da yazabilir. Hedef: **sessizce değiştirilemez** (kurcalama belli olur).

1. **Ajan kaydı:** `/var/log/bekci-betik/kayit.log` satır satır makbuz + journald. Panel erişemez. Sunucu ele geçmedikçe doğrudur.
2. **Makbuz zinciri:** `seq` + `prev`. Silinen makbuz boşluk bırakır, sahte makbuz imza tutmaz.
3. **Panel kaydı:** `script_runs` yalnız eklenir: `hash = sha256(önceki_hash || olay)`; UPDATE/DELETE tetikleyici ile reddedilir (uygulama hatası ve basit SQLi'ye karşı; DB sahibi tetikleyiciyi kaldırabilir).
4. **Dış çapa:** günlük zincir başı bildirim kanalına ve (D7) panel dışı bir yere yazılır. Çapadan önceki değişiklik doğrulamada görünür.
5. **Doğrulama:** `bekci-imza dogrula <dışa aktarım>` makbuz imzası, zincir ve çapayı denetler. Panel arayüzündeki "doğrulandı" rozeti ele geçirilmiş panelde yalan söyleyebilir, bu yüzden tek otorite çevrimdışı araçtır.
6. **Saklama:** süre dolunca parça düşürülmeden önce son hash `script_log_checkpoint` tablosuna yazılır; zincir oradan sürer.

## 9. 600 belgesiyle uyum (şema)

Betik özelliği PostgreSQL ister (Timescale varsayılan, düz PG uyumlu). SQLite kullanımdan kalktığı için orada **kapalıdır** (D8).

| Tablo | Tür | Not |
|---|---|---|
| `scripts` | normal | `id`, `version`, zarf, gövde, yüklenme; satır değiştirilmez, yeni sürüm yeni satır |
| `script_hosts` | normal | `probe_id`, `host_key`, onay zamanı/kişi |
| `script_runs` | **hypertable** (30 gün parça, 90 gün sonra sıkıştırma, 365 gün) | 600 §3.2'deki satır korunur; ama satır güncellenmez: `(run_id, seq, kind, time, hash, prev, data)` olay akışı, son durum görünümle okunur. Yalnız ekleme hypertable sıkıştırmasıyla uyumludur |
| `script_run_outputs` | normal | Kısaltılmış çıktı, 90 gün (D10); sır içerebilir, çapaya yalnız özeti girer |
| `script_log_checkpoint` | normal | Saklama sonrası zincir sürekliliği |

Düz PG'de `script_runs` normal tablo olur, saklama aynı `checkpoint` adımıyla yapılır.

## 10. Eski/yeni uyumu (ajanlar otomatik güncelleniyor)

| Eşleşme | Davranış |
|---|---|
| Eski ajan, yeni panel | Yetenek başlığı yok (`X-Probe-Scripts`), panel `script_runs` alanı **hiç göndermez**, arayüz "desteklenmiyor" der |
| Yeni ajan, eski panel | Ajan başlık gönderir, eski panel yok sayar; iş gelmez. Betik kodu hiç devreye girmez |
| Yeni ajan betik kapalı, yeni panel | Yetenek `kapalı` bildirilir, panel iş üretmez; yine gelse ajan reddeder |
| Ajan geri dönerse | Yerel dosya durur, eski ajan onu okumaz; sayaç/`run_id` dosyaları korunur |
| Ajan güncellemesi | `/etc/bekci/betik.json` hiç değişmez; özellik güncellemeyle **kendiliğinden açılamaz** |
| Bilinmeyen `schema` | Ajan reddeder (kapalı kalır); panel eski ajana yeni şema teklif etmez |

Tek yeni alan `omitempty`; `POST /api/probe/script-result` yalnız yeni ajanda çağrılır. 690'da gerçek ikililerle test edilir (v1.6.0 ajan + yeni panel, yeni ajan + v1.6.0 panel).

## 11. Bu işin dışında ama önerilen sertleştirme

- Kurulum betiği ikiliyi ajan imzasıyla da doğrulasın (bugün özet paneldendir, P10).
- `uptime` ajanında yerel "otomatik güncellemeyi reddet" ayarı (S4); bugün yalnız panel ayarı var.
- API anahtarı yönetimi ve 2FA kapatma uçları API anahtarıyla çağrılamasın (K3).
- `systemd` biriminde `NoNewPrivileges` ve sıkılaştırma (ajan için; mevcut ajanları bozmadan).

## 12. Açık kararlar (ONAY GEREKİYOR)

| # | Karar | Öneri |
|---|---|---|
| D1 | Güven kökü: yalnız operatör anahtarı mı, proje başlangıç anahtarı da mı? | Yalnız operatör; başlangıç betikleri repoda, operatör okuyup imzalar |
| D2 | `K_yayın` ve `K_onay` nerede, ayrı mı? | Ayrı; ikisi de kullanıcının kendi makinesinde parola korumalı, bu sunucuda ve CI'da değil; yedek çevrimdışı. Donanım anahtarı sonra |
| D3 | Tek yöneticili kurulumda `panel` onayı | Aynı kişi TOTP ile kendi isteğini onaylayamaz; tek yöneticide `panel` sınıfı yerine `signed` kullanılır |
| D4 | `not_after` azami süre | 180 gün; süresi dolunca yeniden imza gerekir (iptal penceresi de budur) |
| D5 | Platform kapsamı | Yalnız Linux + systemd + yerli ajan. Docker ajanı, Windows, OpenRC reddeder |
| D6 | `run_as: root` izni | İzinli (temizlik root ister); kafes §6.2, sınıf §13 |
| D7 | Dış çapa hedefi | İlk aşama bildirim kanalı + `bekci-imza` ile yerel dosya; S3 Object Lock sonra |
| D8 | SQLite'ta betik | Kapalı |
| D9 | TLS sonlandıran vekil (canlı panel Cloudflare arkasında): çıktı vekilde görünür | İlk sürümde kabul, sır basan betik imzalanmaz; uçtan uca şifreli çıktı sonraki iş |
| D10 | Saklama | Kayıt 365 gün, çıktı 90 gün |
| D11 | Başlangıç betik sınıfları (§13) | Tablo gibi |

## 13. Başlangıç betikleri (690)

| Betik | Parametre | `approval` | Sınır | Not |
|---|---|---|---|---|
| `journal-temizle` | `gun` int 1..30 | none | 6 saatte 1 | `journalctl --vacuum-time` |
| `paket-onbellek` | yok | none | 6 saatte 1 | `apt-get clean` / `dnf clean packages` |
| `docker-derleme-temizle` | `saat` int 24..720 | panel | 6 saatte 1 | `docker builder prune -f --filter until=` |
| `docker-imaj-temizle` | `saat` int 168..720 | panel | Günde 1 | `docker image prune -f --filter until=`; `-a` yok, **`system prune` ve volume asla** |
| `servis-yeniden` | `servis` enum (sunucu grubuna göre imzalı liste) | signed | 10 dk'da 1 | `sshd`, ajan, `docker`, veritabanı listeye alınmaz |
| `sertifika-yenile` | `arac` enum certbot/caddy | panel | Günde 1 | `certbot renew` / `systemctl reload caddy` |

## 14. 690 kabul testleri (saldırı)

Her biri gerçek ikili ile, red nedeni kodu doğrulanır; hiçbirinde betik başlamamalı (işaret dosyası oluşmaz):

imzasız · yanlış anahtar · değiştirilmiş gövde · değiştirilmiş bildirim alanı · başka tür zarf (`ticket` yerine `manifest`, ajan güncelleme imzası) · süresi geçmiş / henüz başlamamış · panel saati oynatılmış · sürüm geri alma · iptal edilmiş · izinsiz grup · yerel `allow` dışı · bilet yok / başka ajana / başka parametre / süresi geçmiş / tekrar · `run_id` tekrarı (kilitlenme sonrası dahil) · hız sınırı aşımı · özellik kapalı · dosya yok/bozuk · `http://` panel · Docker ajan · şema dışı parametre, tire ile başlayan ad, NUL, satır sonu, `$(...)` · API anahtarı (full ve read) başlatma · step-up'sız başlatma · kendi isteğini onaylama · bilinmeyen `schema` · bilinmeyen alan, yinelenen anahtar, büyük zarf.

Kabul: ayrıca token/`K_ajan` betik ortamında görünmez; dev çıktı ve zaman aşımı çocuk süreçleriyle ölür; panelde XSS çıktısı metin kalır; kayıt satırı silinince `dogrula` hata verir.
