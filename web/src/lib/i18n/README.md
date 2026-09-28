# Arayüz dil desteği (i18n)

Diller: **tr** (varsayılan; mevcut Türkçe metin kaynaktır) ve **en**. Kütüphane yok:
`t()` + ad alanı başına sözlük dosyaları.

```
lib/i18n/
  index.ts             t(), tParts(), tOr(), intlLocale(), dil seçimi (dışa aktarımlar)
  locale.svelte.ts     geçerli dil ($state), setLocale, deviceLocale, browserLocale
  types.ts             Shape<T>: en sözlüğünün biçim denetimi
  tr/<ad>.ts           Türkçe metinler (kaynak)
  en/<ad>.ts           İngilizce karşılıklar — `satisfies Shape<typeof tr>`
  tr/index.ts, en/index.ts   ad alanlarını toplar (düzenlemeyin; tüm ad alanları hazır)
```

## Kullanım

```svelte
<script lang="ts">
  import { t } from '../lib/i18n';
</script>

<h1>{t('monitors.title')}</h1>
<button aria-label={t('common.close')}>…</button>
<p>{t('monitors.deleted', { name: m.name })}</p>          <!-- "{name}" yer tutucusu -->
<span>{t('monitors.count', { count: n })}</span>           <!-- çoğul: "tekil|çoğul" -->
```

- **Anahtar** `ad.alanı.anahtar` biçimindedir ve tiplidir: olmayan anahtar `npm run check` hatasıdır.
  Çalışma anında oluşan anahtar için `t(\`status.${kind}\`)` (tip çıkarımı yeterliyse) ya da
  `tOr(key, yedek)` kullanın.
- **Reaktiflik:** `t()` ve `lib/format.ts` yardımcıları `i18n.locale` rune'unu okur; şablonda,
  `$derived` içinde veya şablondan çağrılan fonksiyonda kullanıldığında dil değişince yeniden
  çizilir (doğrulandı: Hesabım'da dil değiştirmek sayfayı anında çevirir).
  **Modül/betik düzeyinde bir kez hesaplanan sabitler çevrilmez:**
  ```ts
  const TABS = [{ label: t('x') }];               // YANLIŞ: ilk dilde donar
  const TABS = $derived([{ label: t('x') }]);     // doğru
  const TABS = [{ key: 'account' }];  … {t(`settings.tabs.${tab.key}`)}   // doğru
  export const LABELS = { get up() { return t('status.up'); } };           // .ts dosyasında doğru (getter)
  ```
  `.ts` modüllerindeki etiket tabloları (ör. `monitorTypes.ts`, `notifyTypes.ts`) getter veya
  `labelKey` + bileşende `t()` ile çevrilir. Varsayılan prop değerlerini (`label = 'Kopyala'`)
  `label ?? t('common.copy')` gibi `$derived` ile çözün.
- **Döngü değişkeni `t` olmasın** (`{#each items as t}` çeviri fonksiyonunu gölgeler).
- **Biçimli cümle** (içinde `<code>`, `<a>`, `<b>`): `{@html}` KULLANMAYIN, `tParts()`:
  ```svelte
  <!-- tr: "Anahtarı {link} bölümünden oluşturun."  en: "Create one under {link}." -->
  {#each tParts('settings.general.promText') as p, i (i)}
    {#if p.slot === 'link'}<a href="#/settings">{t('settings.general.promLink')}</a>{:else}{p.text}{/if}
  {/each}
  ```
- **Çoğul:** yalnızca `count` parametresi verilince `"tekil|çoğul"` biçimi seçilir
  (`Intl.PluralRules`). Türkçede genelde tek biçim yeter: tr `'{count} monitör'`,
  en `'{count} monitor|{count} monitors'`.
- **Türkçe ekler:** "API’ye", "14:30’da" gibi ekli kalıpları cümleye gömün; tr ve en ayrı cümle
  kursun (`timeLocative()` en'de `''` döner).

## Anahtar ekleme

1. `tr/<ad>.ts` içine Türkçe metni **birebir** taşıyın (kelime/noktalama değiştirmeyin).
2. Aynı anahtarı `en/<ad>.ts` içine İngilizce ekleyin. Eksik/fazla anahtar tip hatasıdır.
3. İç içe gruplayın: `monitors.form.intervalHelp`, `monitors.list.empty` …
4. Ortak kelimeler için önce `common.*`'a bakın (save, cancel, delete, edit, add, close, back,
   retry, copy, done, loading, noData, name, username, password, version, on/off, yes/no …),
   durumlar `status.*` (up/down/pending/paused/maintenance/retrying), roller `roles.*`.
   **Aşama 2'de `common`, `nav`, `status`, `roles`, `auth`, `account`, `settings`, `pub`
   dosyalarına EKLEME YAPMAYIN** (paralel çalışmada çakışır); gereken yeni metni kendi ad
   alanınıza ekleyin.

İngilizce terimler (UptimeRobot / Uptime Kuma dili): Monitor, Up / Down, Paused, Pending,
Maintenance (window), Incident, Status page, Response time, Check interval, Retry interval,
Check location (kontrol noktası), Server agent (sunucu ajanı), Notification channel, Tag,
Announcement, API key, Audit log, Two-factor authentication. Cümle düzeni (sentence case),
kısa ve doğal.

## Tarih, sayı, süre

`lib/format.ts` dili izler — kendi `Intl`/`toLocaleString('tr-TR')` çağrınızı yazmayın:
`fmtDate` (tr `27.09.2026 14:30`, en `Sep 27, 2026, 14:30`), `fmtDay`, `fmtShortDate`, `fmtTime`,
`fmtDateSec`, `fmtDuration` (tr `2 sa 5 dk`, en `2h 5m`), `fmtDurationLong`, `fmtDurationShort`,
`fmtRelative` (`3 dk önce` / `3m ago`), `fmtInterval`, `fmtPct` (tr `%99,95`, en `99.95%`),
`fmtPctInt`, `fmtNum`, `fmtDec`, `fmtMs`, `fmtBytes`, `fmtRate`, `fmtSize`.
Saat dilimi her dilde Europe/Istanbul'dur. Başka bir Intl API gerekiyorsa `intlLocale()`
(`tr-TR` / `en-US`). `lower()` ve `collator` veri (monitör adı) için Türkçe kalır.
`STATUS_LABELS`, `pointStatusLabel`, `ROLE_LABELS`, `ROLE_DESCS` zaten çevrilidir.

## ÇEVRİLMEYECEKLER

- Kullanıcı verisi: monitör/sunucu/sayfa/etiket adları, açıklamalar, duyurular, olay nedenleri.
- Sunucudan gelen metin: API hata mesajları sunucuda çevrilir (istek `X-Uptime-Lang` başlığı
  taşır) — `errorMessage(err)`'u olduğu gibi gösterin. Kontrol sonucu mesajları (heartbeat,
  "Bağlantı reddedildi" vb.) ve işlem kaydı ayrıntıları saklanmış veridir; olduğu gibi kalır.
- Teknik belirteçler: HTTP, JSON, TOTP, SMTP, cron ifadeleri, alan anahtarları, `upk_…`,
  örnek adresler, dosya yolları (`C:\Program Files\Uptime\…`), komut/betik parçaları
  (docker, systemctl, PowerShell, curl), ortam değişkenleri, `uptime`/`uptime-agent` adları.
- Ürün adı: `APP_NAME` (`lib/brand.ts`) kullanın, metne gömmeyin
  (`t('x', { app: APP_NAME })`). Go tarafında `internal/brand.Name`.

## Dil seçimi

- Oturum yokken: bu cihazda seçilen dil (giriş ekranındaki seçici, `localStorage`
  `uptime.lang`) yoksa tarayıcı dili (`tr*` → tr, değilse en).
- Girişte: hesap tercihi (`users.lang`, Ayarlar › Hesabım) varsa o; yoksa cihazın dili.
- Herkese açık durum sayfası sayfanın kendi dilinde (`status_pages.lang`) açılır.
- `<html lang>` her değişimde güncellenir. Bildirim dili ayrı bir genel ayardır
  (Ayarlar › Genel, `notify_lang`; Go `internal/i18n`).

## Aşama 2: kalan dosyalar

Her grup yalnızca kendi dosyalarını ve kendi ad alanı dosyalarını (`tr/<ad>.ts` + `en/<ad>.ts`)
düzenler. Ad alanı dosyaları boş olarak hazırdır ve `index.ts`'lere kayıtlıdır.

**A — Monitörler** (ad alanları: `monitors`, `monitorTypes`)
- `pages/MonitorList.svelte`, `pages/MonitorDetail.svelte`, `pages/MonitorForm.svelte`
- `components/MonitorRow.svelte`, `components/MonitorMenu.svelte`, `components/BulkEditModal.svelte`,
  `components/MonitorPicker.svelte`, `components/QuickNotifyModal.svelte`, `components/QuickTagsModal.svelte`
- `lib/monitorTypes.ts` (tip adları, alan etiketleri/yardımları), `lib/actions.ts`

**B — Sunucular, bildirimler, kullanıcılar** (ad alanları: `servers`, `alerts`, `probes`,
`notifications`, `notifyTypes`, `users`, `apiKeys`)
- `pages/ServerList.svelte`, `pages/ServerDetail.svelte`, `components/ServerSetupModal.svelte`,
  `components/ServerSettingsModal.svelte`, `components/ServerPicker.svelte`,
  `components/ContainerTable.svelte`, `components/AlertRulesEditor.svelte`, `lib/servers.svelte.ts`
- `pages/settings/Probes.svelte`
- `pages/Notifications.svelte`, `components/NotificationForm.svelte`, `components/ConfigFields.svelte`,
  `lib/notifyTypes.ts`
- `pages/settings/Users.svelte`, `pages/settings/UserForm.svelte`, `pages/settings/ApiKeys.svelte`

**C — Olaylar, bakım, durum sayfaları, yedek, işlem kaydı, etiketler** (ad alanları:
`incidents`, `maintenance`, `pages`, `backup`, `audit`, `tags`)
- `pages/Incidents.svelte`, `pages/IncidentDetail.svelte`, `components/IncidentTable.svelte`
  — olay geçmişindeki sabit Go metinlerini `kind` + `data`'dan çevirin: `maint_start`,
  `maint_end`, `reminder` (`data.downtime`), `notify` (`data.ok` / `data.none` / `data.error`),
  `location` (`data.status` up | down | unknown, `data.message` ham kontrol mesajı — yalnız yeni
  kayıtlarda; yoksa `message`'ı olduğu gibi gösterin). `message` kontrol sonucunu taşıyan
  türlerde (down/change/retry/up) olduğu gibi gösterilir.
- `pages/MaintenanceList.svelte`, `pages/MaintenanceForm.svelte`, `lib/maintenance.ts`,
  `components/QuickMaintModal.svelte`
- `pages/StatusPages.svelte`, `pages/StatusPageEditor.svelte` (dil alanı hazır, geri kalanı),
  `pages/Announcements.svelte`, `components/QuickPageModal.svelte`,
  `components/BadgeBuilder.svelte` (rozetler `?lang=en` destekler; seçenek eklenebilir)
- `pages/settings/Backup.svelte`, `components/ImportResult.svelte`, `components/FilePick.svelte`
- `pages/settings/Audit.svelte`, `lib/audit.ts`
- `pages/settings/Tags.svelte`, `components/TagDialog.svelte`, `components/TagChip.svelte`

Bitince: `npm run check` (0 hata / 0 uyarı) ve `npm run build`; İngilizce tarayıcıyla ekranda
Türkçe kalıntı ve ham anahtar (`monitors.form.x`) olmadığını kontrol edin.
