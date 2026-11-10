# 730 — Güvenlik sekmesi, özel metrik eklentisi, hosting servis sağlığı, dosya/sistem denetimleri

Durum: tasarım (kod yok), 2026-11-10. Kararlar bu belgede verildi (yönetici yetkisiyle); uygulama 731-752 kuyruğu (§7).
Fikir kaynağı: site24x7/plugins. O repoda LICENSE yok: **kod, sorgu metni, çıktı biçimi kopyalanmaz**; ajan tarafı Go ile
sıfırdan yazılır, yalnız "hangi sayıya bakılır" fikri alınır. Tüm metinler TR+EN, emoji yok, renk paleti mevcut tema (mor/indigo yok).

## 0. Ortak kararlar

| # | Karar |
|---|---|
| G1 | Dört bölüm sırasıyla yapılır: A (güvenlik) > B (eklenti) > C (hosting servisleri) > D (dosya/sistem). |
| G2 | Ajan **salt okur**: paket dizinini güncellemez (`apt update`/`dnf makecache` çalıştırmaz), servis başlatmaz, dosya içeriği okumaz (yalnız üst veri). Dizin bayatlığı (`index_age`) raporlanır. |
| G3 | Yeni veri iki kanaldan gelir: **güvenlik raporu** (A, sabit şema) ve **denetim sonucu** (B+C+D, ortak `Result` şekli: durum + metrikler). C ve D yerleşik Go denetimleridir, eklentiyle aynı depolama/uyarı/arayüzü kullanır. |
| G4 | Panel->ajan ayarı mevcut kalıpla: jobs/kalp atışı yanıtında alan (`security_interval`, `checks`). Alan yoksa ajan o işi hiç çalıştırmaz (eski panel uyumu); eski ajan alanı yok sayar (yeni panel uyumu). |
| G5 | Uyarılar mevcut `server_alerts` tablosu + `servers/alerts.go` metrik listesi; olay `kind=server_alert`, `metric=<yeni>`, `mount=<konu>`. Zaman çizelgesi `servers/changes.go` olay türleri. Bildirim `notify.KindServerAlert/Resolved`. Yeni sistem yok. |
| G6 | Kimlik bilgisi (DB parolası vb.) **panelden gelmez**: ajanda yerel dosya `/etc/bekci/kimlik.d/<ad>.json` (root 600) ya da `C:\ProgramData\Bekci\kimlik.d\<ad>.json` (SYSTEM+Administrators ACL). Panel yalnız adı söyler. |
| G7 | Docker ajanı (kapsayıcıda çalışan) ana makinenin paket veritabanını/journal'ını görmez: bölüm `Notes["..."]="container"` ile boş gelir, arayüz "kapsayıcı ajanında yok" der. |
| G8 | SQLite kipinde yeni tablolar oluşur ama eklenti/denetim zaman serisi kapalıdır (betik D8 ile aynı); güvenlik raporu SQLite'ta da çalışır (küçük). |
| G9 | Ücret: ücretli plan yalnız self-host. **Sunucu başına tüm sayılar, sekmeler ve uyarılar ücretsiz.** Filo özet sayaçları ücretsiz. Ücretli adayı: filo güvenlik raporu sayfası (paket x sunucu matrisi, CSV/PDF, haftalık özet e-postası) ve çok sunuculu denetim panosu. Lisans kapısı bu kuyrukta yazılmaz; 752 bayrak arkasında hazırlanır. |
| G10 | Platform: A ve C/D Linux + Windows; B (eklenti) ilk aşamada yalnız Linux + systemd >= 254 (680 D5). Windows eklentisi ayrı tasarım ister (kafes karşılığı yok). |

Linux yerli ajan root ile çalışır (`api/servers.go` birim dosyasında `User=` yok); Windows ajanı LocalSystem hizmetidir. Root/yetki
gereği aşağıda "yetki" olarak her kaynakta belirtildi; yetki yoksa bölüm `Notes` ile boş gelir, hata saymaz.

## A. Güvenlik sekmesi

### A.1 Veri kaynakları — Linux

**Bekleyen güncellemeler** (varsayılan 3600 sn'de bir, ayrı önbellekli; rapor döngüsü 900 sn):
- apt (Debian/Ubuntu): `apt-get -s -o Debug::NoLocking=true dist-upgrade` çıktısındaki `Inst <paket> [eski] (yeni <origin listesi> [arch])`
  satırları. Güvenlik = origin `*-security` (Ubuntu) ya da `Debian-Security`/`*-security` (Debian 12+). Yetki: gerekmez. Dizin yaşı:
  `/var/lib/apt/lists/*InRelease` en yeni mtime. Ubuntu Pro ESM satırları (`esm-apps`, `esm-infra`) güvenlik sayılır, `esm` işaretli.
- dnf (RHEL/Alma/Rocky 8-10, Fedora): `dnf -C -q check-update` (çıkış 100 = var) + `dnf -C -q updateinfo list --security --available`.
  dnf5 (Fedora 41+): aynı komut adı takma ad olarak çalışır; çalışmazsa `dnf5 -C advisory list --security --contains-pkgs` denenir.
  `-C` önbellek yoksa hata verir: `Notes["updates"]="cache_missing"`. Önbelleği `dnf-makecache.timer` tazeler. Ciddiyet (Critical/Important/
  Moderate/Low) updateinfo'dan alınır.
- yum (EL7): `yum -C -q check-update` + `yum -C -q updateinfo list security`. CentOS 7'de errata verisi yoktur: güvenlik sayısı
  **-1 = bilinmiyor** (0 değil); arayüz "dağıtım güvenlik bilgisi vermiyor" der.
- zypper (SLES/openSUSE): `zypper -n --no-refresh -x list-updates` ve `zypper -n --no-refresh -x list-patches --category security` (XML).
  Yetki: root (kilit). Kilit meşgulse `Notes["updates"]="locked"`, bir sonraki turda yeniden.
- Paket listesi en çok 500 kayıt (ad, eski, yeni sürüm, güvenlik, ciddiyet); toplam sayı ayrı tutulur, kesilen kısım `truncated`.

**Yeniden başlatma gerekli:**
- Debian/Ubuntu: `/var/run/reboot-required` varsa evet; paketler `/var/run/reboot-required.pkgs`.
- RHEL ailesi: `dnf needs-restarting -r` (EL8+), `needs-restarting -r` (EL7, yum-utils). Çıkış 1 = gerekli; çıktıdaki "* paket" satırları.
- SUSE: `zypper needs-rebooting` (çıkış 102) ya da `/run/reboot-needed`.
- Araç yoksa yedek kural: kurulu en yeni çekirdek (`/boot/vmlinuz-*` sürüm sıralaması, rpm'de `rpm -q --last kernel-core kernel`) !=
  `uname -r` ise "çekirdek güncellendi" gerekçesiyle evet.
- "Ne zamandan beri" panelde ilk görülme zamanıdır (ajan saati değil).

**Başarısız SSH girişleri** (her rapor döngüsünde son rapordan beri fark):
- journal: `journalctl -q -o json --after-cursor=<imleç> -t sshd -t sshd-session` (OpenSSH 9.8+ kayıtları `sshd-session` adıyla yazar).
  İmleç `PROBE_STATE_DIR/uptime.sec.json`'da. Yetki: root ya da `systemd-journal` grubu.
- journal yoksa dosya: `/var/log/auth.log` (Debian), `/var/log/secure` (RHEL); inode + konum saklanır, döndürmede baştan.
- Sayılan kalıplar (kendi düzenli ifadelerimiz): "Failed password/publickey for [invalid user] U from IP", "Invalid user U from IP",
  "authentication failure ... rhost=IP", "maximum authentication attempts exceeded ... IP". Rapor: toplam, IP başına sayı (ilk 10), denenen
  kullanıcı (ilk 5), "invalid user" oranı. Başarılı giriş sayısı da (Accepted) bilgi olarak.

**Oturum açmış kullanıcılar:** `/var/run/utmp` Go ile doğrudan ayrıştırılır (USER_PROCESS kayıtları; `who` gerekmez): kullanıcı, tty,
uzak adres, giriş zamanı; en çok 50. systemd 257+ utmp yazmayabilir: `loginctl list-sessions -o json` yedek.

**Güvenlik duvarı** (yalnız INPUT yönü sayılır; Docker'ın DOCKER*/FORWARD zincirleri güvenlik duvarı sayılmaz):
- firewalld: `firewall-cmd --state` = running -> açık; bölge/servis sayısı bilgi.
- ufw: `ufw status` "Status: active" -> açık (root).
- nftables: `nft -j list ruleset`; `hook input` zincirlerinde kural sayısı ve politika (root).
- iptables-legacy: `iptables-legacy -S INPUT` (nft ile yoksa).
- Karar: herhangi bir arka uçta INPUT politikası drop/reject **ya da** INPUT'ta en az bir drop/reject kuralı varsa `active=true`.
  Sağlayıcı (bulut) güvenlik duvarı ajan tarafından görülemez; bu yüzden "kapalı" uyarısı varsayılan **pasif**.

### A.2 Veri kaynakları — Windows

Ajan zaten `yusufpapurcu/wmi` + `go-ole` (dolaylı) ve `x/sys/windows` içeriyor; doğrudan bağımlılığa çevrilir. PowerShell çağrılmaz
(başlatma maliyeti, yürütme ilkesi ve yerel dil farkı).
- **Defender:** WMI `root\Microsoft\Windows\Defender` `MSFT_MpComputerStatus`: `AMServiceEnabled`, `AntivirusEnabled`,
  `RealTimeProtectionEnabled`, `AntivirusSignatureLastUpdated` (saat cinsinden imza yaşı), `QuickScanAge`/`FullScanAge` (gün;
  4294967295 = hiç), `IsTamperProtected`. Tehdit: `MSFT_MpThreatDetection` son 30 gün sayısı + `MSFT_MpThreat` etkin sayısı.
  Ad alanı yoksa (üçüncü taraf AV, Server Core'da kaldırılmış) `Notes["av"]="no_defender"`.
- **Windows Update:** COM `Microsoft.Update.Session` -> `CreateUpdateSearcher`, `Online=false` (ağa çıkmaz, WU hizmetinin son taramasını
  kullanır), sorgu `IsInstalled=0 and IsHidden=0`. Güvenlik = `MsrcSeverity` dolu ya da kategori "Security Updates"; KB + başlık + ciddiyet,
  en çok 200. Başarısız: `QueryHistory` son 30 gün `ResultCode` 4/5. 6 saatte bir, 3 dk zaman aşımı, önbellekli.
- **Yeniden başlatma bekliyor:** kayıt defteri `...\WindowsUpdate\Auto Update\RebootRequired` ve
  `...\Component Based Servicing\RebootPending`. `PendingFileRenameOperations` yalnız bilgi (çok gürültülü, uyarı üretmez).
- **Başarısız oturum açma 4625 / kilitlenen hesap 4740:** `wevtapi.dll` `EvtQuery` (x/sys ile syscall), Security günlüğü, XPath
  `EventRecordID > <son>`; XML'den `TargetUserName`, `IpAddress`, `LogonType`, 4740'ta `TargetUserName` + çağıran bilgisayar. Son kayıt
  kimliği durum dosyasında. Yetki: LocalSystem (SeSecurityPrivilege). Linux'taki IP/kullanıcı özetinin aynısı.
- **RDP oturumları:** `wtsapi32` `WTSEnumerateSessionsW` + `WTSQuerySessionInformationW` (kullanıcı, istemci adı/adresi, durum
  Active/Disconnected, başlangıç). `query session` çıktısı yerel dile bağlı olduğu için kullanılmaz.
- **Güvenlik duvarı profilleri:** WMI `root\StandardCimv2` `MSFT_NetFirewallProfile`: Domain/Private/Public `Enabled`,
  `DefaultInboundAction`. `active` = ağın bağlı olduğu profil açık.

### A.3 Ajan tarafı yapı
- `internal/metrics/security.go` (ortak tipler, `Sanitize`, `Hash`), `security_linux.go`, `security_windows.go`, `security_other.go` (boş).
  Komut çalıştırma `timeout.go` yardımcısıyla (komut başına 20 sn; WU 3 dk).
- Ayrıştırıcılar saf fonksiyon, girdi örnekleri `testdata/sec/` altında **bizim ürettiğimiz** çıktılarla (her dağıtım için bir örnek).
- `internal/probe/security.go`: envanter döngüsünün kopyası; panel `security_interval` verirse çalışır (varsayılan 900, 0 = kapalı,
  en az 300). Tam rapor özet (hash) değişince ya da 12 turda bir; SSH/4625 farkı > 0 ise her turda kısa rapor.
- Tel şekli (`metrics.Security`): `time`, `updates{manager,total,security,index_age,packages[],truncated}`,
  `reboot{required,reason,packages[]}`, `auth{since,failed,accepted,top_ips[{ip,count}],top_users[],locked[{user,time,source}]}`,
  `sessions[{user,from,since,kind:ssh|tty|rdp,state}]`, `firewall{active,backend,rules,profiles[{name,enabled,inbound}]}`,
  `av{product,realtime,enabled,sig_age_h,quick_age_d,full_age_d,threats_30d,active_threats,tamper}`, `wu{failed_30d}`, `notes`.
  Boyut sınırı 256 KiB (envanterle aynı); panel `Sanitize` ile yeniden sınırlar.

### A.4 Şema (migration 51, `store/security.go`)
- `sec_state(server_id PK, report TEXT/JSONB, hash, received_at, upd_total, upd_security, reboot_required, reboot_since, fw_active,
  av_ok, av_sig_age_h)` — filo sorguları için düz sütunlar, ayrıntı JSON'da.
- `sec_pkgs(server_id, name, new_version, security, severity, first_seen, PK(server_id,name))` — "X gündür bekliyor" panelin ilk
  görme zamanından hesaplanır; listeden çıkan satır silinir (güncellendi olayı üretir).
- `sec_auth_hourly(server_id, hour, failed, accepted, PK)` ve `sec_auth_ip_daily(server_id, day, ip, count, PK)` (gün başına ilk 20 IP).
  Saklama: saatlik 90 gün, IP 30 gün (`cleanup.go`). Timescale'de de düz tablo (küçük); hypertable gereksiz.
- Sunucu silinince `ON DELETE CASCADE` (probes).

### A.5 API
- `POST /api/probe/security` (ajan kimliği, envanter ucuyla aynı koruma, `api/security.go`).
- `GET /api/servers/{id}/security` (sunucu erişimi olan izleyici): durum + paketler (`first_seen` ile) + 7 gün saatlik başarısız giriş +
  24 sa / 7 gün ilk IP'ler + oturumlar.
- `GET /api/security/summary` (oturum ya da salt okunur anahtar, `readScopeAllows`'a eklenir): `{security_updates:{count,servers[]},
  reboot:{...}, firewall_off:{...}, av_problem:{...}, stale_index:{...}}` — kullanıcının görebildiği sunucularla süzülür.
- `openapi.yaml` güncellenir.

### A.6 Arayüz
- ServerDetail yeni sekme **Güvenlik** (`components/ServerSecurity.svelte`, i18n `servers.security`): üstte dört kart (güncellemeler
  toplam/güvenlik + en eski bekleme günü, yeniden başlatma rozeti + paket, başarısız giriş 24 sa + saatlik sütun grafiği, güvenlik duvarı),
  altında paket tablosu (süzgeç: yalnız güvenlik), en çok deneyen IP tablosu, oturumlar. Windows'ta güncellemeler kartı KB listesi,
  ayrıca Defender kartı (gerçek zamanlı, imza yaşı, tarama yaşları, tehdit, kurcalama).
- ServerList: ad sütununun yanında küçük rozetler ("12 güvenlik", "yeniden başlat"); liste üstünde filo şeridi:
  "Güvenlik güncellemesi bekleyen N sunucu · Yeniden başlatma bekleyen N sunucu · Güvenlik duvarı kapalı N" — tıklanınca liste süzülür.
- Topoloji ve genel durum sayfası etkilenmez.

### A.7 Uyarı kuralları (yeni `servers/alerts.go` metrikleri)
| Metrik | Eşik birimi | Varsayılan | Not |
|---|---|---|---|
| `sec_updates_age` | gün | 7, etkin | en eski bekleyen güvenlik paketinin `first_seen` yaşı; konu = paket sayısı |
| `reboot_required` | gün | 3, pasif | yeniden başlatma bu kadar gündür bekliyor |
| `auth_failed` | sayı / `minutes` | 200 / 60 dk, pasif | IP konusu olay ayrıntısında |
| `av_signature_age` | gün | 3, etkin | yalnız Defender olan sunucuda değerlendirilir |
| `av_off` | eşiksiz | etkin | gerçek zamanlı koruma ya da AV kapalı |
| `firewall_off` | eşiksiz | pasif | bkz. A.1 son madde |
| `wu_failed` | eşiksiz | etkin | son 30 günde başarısız güncelleme |
Varsayılan satırlar migration'da `disk_forecast` kalıbıyla (her sunucuya) eklenir. Zaman çizelgesi olayları: `sec_updates_new`
(yeni güvenlik güncellemesi, N paket), `sec_updates_installed`, `reboot_required`/`reboot_cleared`, `firewall_changed`, `av_changed`,
`account_locked` (her 4740). Olay metni `store.ServerIncidentCause` + `i18n` (TR+EN).

### A.8 Ücret
Ücretsiz: sekme, filo şeridindeki sayılar, tüm kurallar. Ücretli adayı (752): filo güvenlik raporu sayfası (paket -> hangi sunucular,
CVE/ciddiyet süzgeci), CSV/PDF, haftalık özet e-postası.

## B. Özel metrik eklentisi

### B.1 Güvenlik modeli (680'e uyum)
- Eklenti, 680'deki imzalı `manifest`tir; yeni alan `mode: "periodic"` ve yeni sınıf `metric`. Eski ajanlar katı ayrıştırıcı nedeniyle
  bilinmeyen alanı **reddeder** (kapalı kalır, güvenli).
- Çalıştırma için **bilet ve panel onayı yok** (periyodik iş 15 dk biletle yürümez); bunun yerine her eklenti, 680 D12 yerel iğnesini
  zorunlu ister: sunucuda root ile `uptime betik eklenti izin <ad> <sha256>`. Böylece panel ele geçse de yeni içerik çalıştıramaz;
  iki imza anahtarı sızsa bile iğnesiz içerik çalışmaz (680 §1 güvencesi korunur, daha da sıkı).
- Panelin yetkisi yalnız: iğneli eklentiyi aç/kapat, aralığı manifestin `[interval_min, interval_max]` içinde seçmek.
- Manifest alanları (katı): `name`, `version`, `interval_min/max` (60-86400), `timeout_sec` (1-60), `output` (`nagios`|`json`),
  `run_as` (`nobody` varsayılan; `root` yalnız manifestte açıkça ve iğneyle), `network` (false varsayılan -> `PrivateNetwork=yes`),
  `not_after` (D4, 180 gün).
- Kafes: 680 §6.2 systemd geçici birimi + `ProtectSystem=strict`, `ReadWritePaths` yok, `PrivateTmp`, `MemoryMax=128M`,
  `CPUQuota=20%`, `TasksMax=32`. Çıktı 64 KiB, stderr ilk 1 KiB (hata iletisi için).
- Kurcalama belli kayıt (`betikkayit`): her çalıştırma değil (dakikada bir gürültü), yalnız aç/kapat/sürüm değişimi/iğne ve günlük
  özet makbuzu (çalışma sayısı, son sonuç) zincire girer.
- D9: çıktı vekilde görünür; sır basan eklenti imzalanmaz (belgede uyarı).
- Platform: Linux + systemd >= 254 (D5). Windows'ta `betik` alt komutu "desteklenmiyor" der.

### B.2 Çıktı biçimleri (kendi ayrıştırıcımız, `internal/eklenti`)
- **Nagios:** dönüş kodu 0/1/2/3 = ok/warning/critical/unknown; diğer kod, zaman aşımı, sinyal = unknown. İlk satır `METİN | perfdata`,
  sonraki satırlarda `|` sonrası ek perfdata. Perfdata: `'etiket'=değer[UOM];[warn];[crit];[min];[max]`; UOM `s ms us % B KB MB GB TB c`
  (taban birime çevrilir: saniye, bayt), aralık söz dizimi `10`, `10:`, `~:10`, `10:20`, `@10:20` desteklenir.
- **JSON (bizim şemamız):** `{"status":"ok|warning|critical|unknown","message":"...","metrics":{"ad":{"value":1.5,"unit":"ms",
  "warn":"..","crit":".."}}}`. Ek olarak düz biçim kabul edilir: üst düzey sayısal anahtarlar = metrik, `units` nesnesi = birimler,
  `status` 1 = ok / 0 = critical, `msg` = ileti (yaygın eklenti yazım alışkanlığı; şema bizimdir).
- Sınırlar: eklenti başına 50 metrik, ad `^[a-zA-Z0-9_.\-]{1,64}$`, NaN/Inf atılır, ileti 512 karakter.
- Eşikler: eklentinin kendi durumu esastır; panelde metrik başına ek eşik kuralı tanımlanabilir (B.5).

### B.3 Şema (migration 53, `store/checks.go`; 52 = 732 sec_auth tabloları)
- `check_state(server_id, name, source:plugin|builtin, kind, version, status, message, last_run, last_ok, duration_ms, PK(server_id,name))`.
- `check_samples(server_id, name, metric, t, value)`: Timescale'de hypertable (1 gün parça, 7 gün sonra sıkıştırma, 30 gün saklama) +
  `check_hourly` sürekli özet (avg/min/max, 365 gün); düz PG'de tablo + saatlik özet işi `cleanup.go` içinde; SQLite: yazılmaz (G8).
- `server_alerts`'e `below BOOLEAN DEFAULT false` sütunu (küçük değer kötü kuralları için); eklenti kuralı `metric='check'`,
  `mount='<ad>/<metrik>'` ya da `'<ad>'` (yalnız durum).
- Sınırlar: sunucu başına 20 eklenti/denetim.

### B.4 API
- `POST /api/probe/checks` (ajan; toplu sonuç, en çok 20 sonuç/istek).
- `GET /api/servers/{id}/checks`, `GET /api/servers/{id}/checks/{name}/series?metric=&range=` (izleyici).
- `PUT /api/servers/{id}/checks/{name}` (yönetici; aç/kapat, aralık). İşlem kaydına (`audit`) girer.
- Ajan jobs yanıtı: `checks:[{name,source,kind,interval,params}]`. Eklentinin içeriği yine 680 kütüphane ucundan (`/api/scripts/lib`).

### B.5 Uyarı ve olay
- Durum `critical` (kural seçeneğiyle `warning`/`unknown` da) `minutes` boyunca sürerse `server_alert`, `metric=check`, `mount=<ad>`;
  ok'a dönünce çözülür. Metrik eşik kuralı (`<ad>/<metrik>`, `below` ile yön) aynı kalıpla.
- `unknown` 3 tur üst üste: zaman çizelgesine `check_unknown` (olay değil).

### B.6 Arayüz
- ServerDetail sekmesi **Denetimler** (`components/ServerChecks.svelte`, i18n `servers.checks`): her denetim için durum, ileti, son
  çalışma, metrik kıvılcım grafikleri; tıklanınca `LineChart`. Kaynak rozeti "eklenti" / "yerleşik".
- Betikler sayfasında manifest kütüphanesinde `metric` sınıfı rozeti; eklenti başlangıç örnekleri `scripts/eklentiler/` (kendimiz yazarız:
  mdadm durumu, SMART özet).

### B.7 Ücret
Ücretsiz (genişleme noktası; güvenlik ağır olduğu için kısıtlamak yanlış teşvik). Ücretli adayı: filo denetim panosu (aynı eklentinin
tüm sunuculardaki karşılaştırması).

## C. Hosting servis sağlığı (yerleşik denetimler)

Ortak: panelde "Denetim ekle" formu (tür + parametre), sonuç `Result` (B ile aynı depolama/uyarı/sekme). Ajan parametreleri kendisi
doğrular. Bağlantı hedefi yalnız loopback / yerel soket; başka ana bilgisayar yalnız yerel kimlik dosyası (G6) içinde yazılıysa.
Sorgular sabittir (panelden SQL gelmez).

### C.1 Posta kuyruğu (`mailq`, Linux)
- postfix: `postqueue -j` (3.1+, JSON satırları: `queue_name`, `arrival_time`) -> kuyruk başına sayı + en eski yaş; eski postfix `mailq`
  ayrıştırma. Yetki: varsayılan `authorized_mailq_users=static:anyone`, root ile sorun yok.
- exim (cPanel): `exim -bpc` sayı, `exim -bp` yaş sütunu (`25m`, `2h`, `3d`) -> en eski.
- qmail: `qmail-qstat` sayı; en eski = `/var/qmail/queue/mess` altında en eski mtime (root).
- Otomatik algılama: hangi ikili varsa. Metrikler: `total`, `deferred`, `hold`, `oldest_min`.
- Varsayılan kurallar (denetim eklenince): `total > 500` 15 dk, `oldest_min > 360`.

### C.2 MSSQL sağlığı (Windows + Linux; sürücü `go-mssqldb` zaten var)
- Bağlantı: Windows'ta tümleşik kimlik (ajan LocalSystem -> SQL'de `NT AUTHORITY\SYSTEM` girişi + `VIEW SERVER STATE`; belge adımı),
  ya da yerel kimlik dosyası. Örnek adı parametre (`.\SQLEXPRESS`).
- DMV'ler (sabit sorgular, kendi yazımımız): `sys.dm_os_sys_memory` (boş fiziksel bellek, `system_memory_state_desc`),
  `sys.dm_os_process_memory` (`physical_memory_in_use_kb`, `process_physical_memory_low`), `sys.dm_os_sys_info` (committed / target),
  `sys.configurations` max server memory, `sys.dm_os_performance_counters` "Page life expectancy" (Buffer Manager),
  `sys.dm_exec_query_memory_grants WHERE grant_time IS NULL` (bellek bekleyen sorgu = RESOURCE_SEMAPHORE kuyruğu),
  `sys.dm_os_wait_stats` RESOURCE_SEMAPHORE ve THREADPOOL bekleme ms farkı (tur arası), `sys.dm_os_schedulers` `work_queue_count` toplamı,
  `sys.dm_exec_requests` engellenen istek sayısı ve 30 sn'den uzun süren istek sayısı, `SERVERPROPERTY('Edition')`.
- Express: arabellek havuzu üst sınırı (1410 MB) sabit bilgi olarak gösterilir; "4 GB Express sunucuda 30 sn zaman aşımı" olayı tam olarak
  PLE düşüşü + bellek bekleyen sorgu + 30 sn+ istek üçlüsüyle görünür olur; ayrıntı kartında "Express bellek sınırına dayandı" ipucu.
- Varsayılan kurallar: PLE < 300 10 dk (uyarı), bellek bekleyen sorgu > 0 5 dk, THREADPOOL bekleme > 0, 30 sn+ istek > 0 5 dk.

### C.3 MySQL / MariaDB (Linux + Windows; sürücü var)
- Yerel soket otomatik (`/var/run/mysqld/mysqld.sock`, `/var/lib/mysql/mysql.sock`, `/tmp/mysql.sock`) ya da `127.0.0.1:3306`;
  kimlik dosyası (Debian'da `/etc/mysql/debian.cnf` okunmaz, kullanıcı kendi en az yetkili hesabını yazar: `USAGE` yeterli).
- `SHOW GLOBAL STATUS`: `Threads_connected`, `Threads_running`, `Max_used_connections`, `Slow_queries` (dk başına fark),
  `Aborted_connects` farkı, `Questions` hızı, `Innodb_row_lock_current_waits`; `max_connections` -> bağlantı %.
- Varsayılan kurallar: bağlantı % > 85 5 dk, `Threads_running` > 2 x çekirdek 5 dk; yavaş sorgu/dk pasif.

### C.4 Web sunucu durum sayfası
- Apache: `mod_status` `?auto` (BusyWorkers, IdleWorkers, ReqPerSec, Scoreboard -> doluluk %).
- nginx: `stub_status` (aktif, accepts/handled/requests farkı -> düşürülen bağlantı, istek/sn, Reading/Writing/Waiting).
- LiteSpeed / OpenLiteSpeed: `/tmp/lshttpd/.rtreport*` dosyası (root): PLAINCONN/SSLCONN/MAXCONN, REQ_PROCESSING, REQ_PER_SEC.
- URL yalnız loopback (`http://127.0.0.1/server-status?auto` vb.); zaman aşımı 5 sn. Windows IIS bu kuyrukta yok (perf sayaçları ayrı iş).
- Varsayılan kurallar: Apache doluluk > 90% 5 dk, nginx düşürülen > 0, LiteSpeed bağlantı % > 90.

### C.5 Ücret
Ücretsiz (hosting kitlesinin asıl çekim noktası).

## D. Dosya/klasör ve sistem

- **Yedek dosyası yaşı/boyutu** (`backup_file`, Linux+Windows): parametre klasör, glob (`*.tar.gz`), alt klasör evet/hayır. Metrikler:
  en yeni dosyanın yaşı (sa), boyutu, eşleşen sayı; önceki en yeniye göre boyut düşüşü %. Kurallar: yaş > X sa (varsayılan 26),
  boyut düşüşü > %50. Yalnız `stat`; içerik okunmaz.
- **Klasör boyutu ve dosya sayısı** (`dir_size`): aynı dosya sistemi içinde yürüme, en çok 1 milyon kayıt / 60 sn (aşılırsa `partial`),
  varsayılan 3600 sn aralık.
- **NFS/CIFS bağlantı noktası** (`mount`, Linux): `/proc/self/mountinfo`'da yol + beklenen tür (nfs, nfs4, cifs, smb3). Takılı kalma:
  `statfs` ayrı goroutine'de 5 sn zaman aşımıyla; aşarsa critical "yanıt vermiyor" (ajan bloklanmaz, takılan goroutine sayısı 1 ile sınırlı).
- **inode doluluğu**: denetim değil, mevcut disk örneğine eklenir (`statfs` Files/Ffree, Linux); yeni uyarı metriği `inode` (%, 90 varsayılan,
  etkin). Disk tablosunda yeni sütun.
- **Süreç var mı / sayısı** (`process`): ad (çalıştırılabilir taban adı) ya da komut satırı içerir; en az/en çok sayı; Linux `/proc`,
  Windows mevcut gopsutil.
- **Zombi süreçler**: `/proc/*/stat` durum Z sayısı metrik örneğine eklenir; uyarı `zombies` (sayı, 50 varsayılan, pasif).
- **Windows Zamanlanmış Görev** (`win_task`): COM `Schedule.Service` -> görev yolu ya da klasör; `LastRunTime`, `LastTaskResult`
  (0 = başarılı, 0x41301 çalışıyor, 0x41303 hiç çalışmadı), `NextRunTime`, `State`. Kural: sonuç != 0, son çalışma > X sa.
- **Windows sertifika deposu**: denetim değil, **Windows envanterinin ilk bölümü** olur: `CertOpenStore` LocalMachine `My` + `WebHosting`
  -> `metrics.InvCert`; IIS bağlantısı `HTTP_SERVICE_CONFIG_SSL` (httpapi) ile ip:port eşlemesi, MSSQL sertifikası kayıt defteri
  `SuperSocketNetLib\Certificate` parmak izi. Böylece mevcut `inv_certs` + sertifika uyarıları (`server_cert_days`) Windows'ta da çalışır;
  Windows envanterinde servisler/portlar `Notes` ile "unsupported" (ayrı iş).
- Ücret: hepsi ücretsiz.

## E. Alınmayanlar
- Mesos, Hadoop, SAP, JD Edwards, Oracle RAC, WebLogic, IBM MQ: hedef kitle (hosting, KOBİ, self-host) dışında, bakım maliyeti yüksek; gerekirse kullanıcı B ile kendi eklentisini yazar.
- Redis, MongoDB, PostgreSQL, Docker, gRPC, MQTT: zaten izleyici türü olarak var (`internal/check`), tekrar edilmez.
- AWS/Azure/GCP servis eklentileri: bulut API kimliği ister, self-host ajan modeline uymaz.
- VMware/Hyper-V ana makine, Exchange, AD çoğaltma: ayrı tasarım işi; şimdilik yok.
- IIS performans sayaçları: Windows perf sayaç altyapısı ayrı iş; C.4 yalnız Linux web sunucuları.

## F. Riskler ve notlar
- Komut çıktıları yerel dile bağlı olabilir: Linux komutları `LC_ALL=C` ile çalıştırılır; Windows'ta metin ayrıştırma yok (WMI/COM/API).
- `apt-get -s` büyük sistemlerde 2-5 sn sürer; 3600 sn önbellek yeterli. `dnf -C` önbellek yoksa ağa çıkmaz (bilerek).
- Başarısız giriş sayımında IP'ler kişisel veri sayılabilir: 30 gün saklama, yalnız yönetici + sunucu erişimi olan izleyici görür.
- Eski ajan + yeni panel: Güvenlik sekmesi "ajan güncellenince gelir" der (ajan sürümü < özelliği getiren sürüm).
- Kod kopyalama yasağı: ayrıştırıcı testleri kendi ürettiğimiz örnek çıktılarla; site24x7 eklenti metni ya da JSON anahtar kümesi alınmaz.

## 7. Uygulama kuyruğu (.sira, her biri 1-2 saat)

Sıra bağımlılığı: 731 -> 732..737 (A); 738 -> 739 -> 741 -> 742 (B); 743 (C/D ortak, 741'e bağlı) -> 744..749; 750 ve 751 bağımsız;
752 en son. 740 numarası kullanılmış (bitti), atlandı.

| No | Parça | Kabul ölçütü (özet; tam metin .sira dosyasında) |
|---|---|---|
| 731 | Linux bekleyen güncellemeler + yeniden başlatma (A ortak altyapısı: tipler, migration 51, ajan döngüsü, probe ucu, sekme iskeleti, `sec_updates_age`) | Ubuntu/Debian + RHEL ailesinde gerçek ajanla sayı + liste panelde, X gün uyarısı olay, yeniden başlatma rozeti |
| 732 | Linux başarısız SSH, oturumlar, güvenlik duvarı | journal + dosya yedeği testli, ilk IP'ler ve saatlik grafik sekmede |
| 733 | Windows Defender + güvenlik duvarı profilleri + RDP oturumları | Windows ajanında WMI/WTS verisi sekmede; ad alanı yoksa not |
| 734 | Windows Update + yeniden başlatma bekliyor + 4625/4740 | KB listesi, başarısız güncelleme, kilitlenen hesap olayı |
| 735 | Kalan güvenlik uyarıları + zaman çizelgesi olayları | 6 kural + 7 olay türü testli, TR+EN metin |
| 736 | Filo güvenlik özeti (şerit + `GET /api/security/summary` + salt okunur anahtar + openapi) | Sayılar ve süzme çalışıyor, read anahtarla 200 |
| 737 | Güvenlik belgesi + e2e | site belgesi TR+EN, `guvenlik.mjs` CI'da |
| 738 | Eklenti çıktı ayrıştırıcı (`internal/eklenti`) | Nagios perfdata/aralık + iki JSON biçimi tablolu testler |
| 739 | Eklenti ajan tarafı (manifest `periodic`, yerel iğne, kafes, zamanlayıcı) | iğnesiz/imzasız/süresi dolmuş red testleri, systemd testi |
| 741 | Denetim sonuçları panel tarafı (migration 53, ucu, depolama, uyarı) | Timescale + düz PG testleri, olay açılıp kapanıyor |
| 742 | Denetimler sekmesi + belge + örnek eklentiler | gerçek eklenti uçtan uca panelde grafik |
| 743 | Yerleşik denetim altyapısı (panel formu, jobs `checks`, kimlik.d, ajan çalıştırıcı) | boş bir `noop` denetimi uçtan uca |
| 744 | Posta kuyruğu | postfix/exim/qmail ayrıştırma testli, gerçek postfix'te sayı |
| 745 | MySQL/MariaDB sağlığı | gerçek MariaDB kapsayıcısında metrikler + kural |
| 746 | MSSQL sağlığı | gerçek MSSQL (Linux kapsayıcı, Express) DMV metrikleri |
| 747 | Web sunucu durum sayfası | Apache/nginx/LiteSpeed ayrıştırma + gerçek nginx |
| 748 | Yedek dosyası + klasör boyutu | yaş/boyut düşüşü kuralı olay üretir |
| 749 | Bağlantı noktası + süreç | takılan NFS zaman aşımı testi, süreç sayısı |
| 750 | inode + zombi (metrik örneği + uyarı) | disk tablosunda inode %, iki kural |
| 751 | Windows zamanlanmış görev + sertifika deposu (Windows envanteri) | Windows sertifikaları mevcut sertifika uyarısına düşer |
| 752 | Filo güvenlik raporu (ücretli adayı, bayrak arkasında) | paket x sunucu, CSV; bayrak kapalıyken görünmez |
