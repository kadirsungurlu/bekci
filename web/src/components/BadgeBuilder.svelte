<script lang="ts">
  // Monitör rozeti oluşturucu: README veya web sitesine eklenecek SVG rozet adresi.
  import CopyButton from './CopyButton.svelte';
  import { LOCALES, i18n, intlLocale, t, tIn, type Locale } from '../lib/i18n';

  let { id, https = false }: { id: number; https?: boolean } = $props();

  type Kind = 'status' | 'uptime' | 'ping' | 'cert-exp';
  const KINDS: Kind[] = ['status', 'uptime', 'ping', 'cert-exp'];
  const DURATIONS = ['24h', '7d', '30d', '90d'] as const;
  const STYLES = ['flat', 'flat-square', 'for-the-badge'] as const;

  let kind = $state<Kind>('status');
  let duration = $state<(typeof DURATIONS)[number]>('30d');
  let style = $state<(typeof STYLES)[number]>('flat');
  // Rozet metinlerinin dili (sunucu ?lang=tr|en; varsayılan tr): başlangıçta arayüz dili.
  let lang = $state<Locale>(i18n.locale);
  let label = $state('');
  let labelColor = $state('');
  let color = $state('');
  let upColor = $state('');
  let downColor = $state('');

  const COLOR_RE = /^(#?[0-9a-fA-F]{3}|#?[0-9a-fA-F]{6}|[a-zA-Z]{3,20})$/;
  const shownColors = $derived(kind === 'status' ? [labelColor, upColor, downColor] : kind === 'ping' ? [labelColor, color] : [labelColor]);
  const bad = $derived(shownColors.some((c) => c.trim() && !COLOR_RE.test(c.trim())));

  const query = $derived.by(() => {
    const q = new URLSearchParams();
    if ((kind === 'uptime' || kind === 'ping') && duration !== '24h') q.set('duration', duration);
    if (style !== 'flat') q.set('style', style);
    if (label.trim()) q.set('label', label.trim());
    if (lang !== 'tr') q.set('lang', lang);
    const add = (k: string, v: string) => {
      const c = v.trim();
      if (c && COLOR_RE.test(c)) q.set(k, c.replace(/^#/, ''));
    };
    add('labelColor', labelColor);
    if (kind === 'status') {
      add('upColor', upColor);
      add('downColor', downColor);
    } else if (kind === 'ping') add('color', color);
    // Uptime ve sertifika rozetlerinin değer rengi eşiğe göre otomatik seçilir.
    const s = q.toString();
    return s ? `?${s}` : '';
  });

  const path = $derived(`/api/badge/${id}/${kind}.svg${query}`);
  const url = $derived(`${location.origin}${path}`);
  const alt = $derived(label.trim() || tIn(lang, `pages.badge.kinds.${kind}`).toLocaleLowerCase(intlLocale(lang)));
  const md = $derived(`![${alt}](${url})`);
  const html = $derived(`<img src="${url}" alt="${alt.replace(/"/g, '&quot;')}">`);

  // Önizleme yazarken her tuşta istek atmasın.
  let preview = $state('');
  let failed = $state(false);
  $effect(() => {
    const p = path;
    const timer = setTimeout(() => {
      failed = false;
      preview = p;
    }, 350);
    return () => clearTimeout(timer);
  });

</script>

<div class="bb">
  <p class="help nomargin">
    {t('pages.badge.intro')}
  </p>
  <div class="grid">
    <div class="field">
      <label for="bb-kind">{t('pages.badge.kind')}</label>
      <select id="bb-kind" class="input" bind:value={kind}>
        {#each KINDS as k (k)}<option value={k}
            >{t(`pages.badge.kinds.${k}`)}{k === 'cert-exp' && !https ? t('pages.badge.httpsOnly') : ''}</option
          >{/each}
      </select>
    </div>
    {#if kind === 'uptime' || kind === 'ping'}
      <div class="field">
        <label for="bb-dur">{t('pages.badge.duration')}</label>
        <select id="bb-dur" class="input" bind:value={duration}>
          {#each DURATIONS as d (d)}<option value={d}>{t(`pages.badge.durations.${d}`)}</option>{/each}
        </select>
      </div>
    {/if}
    <div class="field">
      <label for="bb-style">{t('pages.badge.style')}</label>
      <select id="bb-style" class="input" bind:value={style}>
        {#each STYLES as s (s)}<option value={s}>{t(`pages.badge.styles.${s}`)}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="bb-lang">{t('pages.badge.lang')}</label>
      <select id="bb-lang" class="input" bind:value={lang}>
        {#each LOCALES as l (l)}<option value={l} lang={l}>{t(`common.languages.${l}`)}</option>{/each}
      </select>
    </div>
    <div class="field">
      <label for="bb-label">{t('pages.badge.label')} <span class="muted">{t('pages.badge.labelDefault')}</span></label>
      <input id="bb-label" class="input" maxlength="64" bind:value={label} placeholder={alt} />
    </div>
  </div>
  <fieldset>
    <legend class="label">{t('pages.badge.colors')} <span class="muted">{t('pages.badge.colorsHint')}</span></legend>
    <div class="grid">
      <div class="field">
        <label for="bb-lc">{t('pages.badge.labelColor')}</label>
        <input id="bb-lc" class="input mono" bind:value={labelColor} placeholder="#555" autocapitalize="none" spellcheck="false" />
      </div>
      {#if kind === 'status'}
        <div class="field">
          <label for="bb-uc">{t('pages.badge.upColor')}</label>
          <input id="bb-uc" class="input mono" bind:value={upColor} placeholder="#4c1" autocapitalize="none" spellcheck="false" />
        </div>
        <div class="field">
          <label for="bb-dc">{t('pages.badge.downColor')}</label>
          <input id="bb-dc" class="input mono" bind:value={downColor} placeholder="#e05d44" autocapitalize="none" spellcheck="false" />
        </div>
      {:else if kind === 'ping'}
        <div class="field">
          <label for="bb-c">{t('pages.badge.valueColor')}</label>
          <input id="bb-c" class="input mono" bind:value={color} placeholder={t('pages.badge.auto')} autocapitalize="none" spellcheck="false" />
        </div>
      {/if}
    </div>
    {#if kind === 'uptime' || kind === 'cert-exp'}
      <div class="help">{t('pages.badge.autoHelp')}</div>
    {/if}
    {#if bad}<div class="help c-down">{t('pages.badge.badColor')}</div>{/if}
  </fieldset>

  <div class="prev">
    <span class="label">{t('pages.badge.preview')}</span>
    <div class="prev-box">
      {#if preview && !failed}
        <img src={preview} alt={t('pages.badge.previewAlt')} onerror={() => (failed = true)} />
      {:else if failed}
        <span class="muted small">{t('pages.badge.loadFailed')}</span>
      {/if}
    </div>
  </div>

  <div class="out">
    <div class="label">Markdown</div>
    <div class="copybox">
      <code>{md}</code>
      <CopyButton text={md} />
    </div>
    <div class="label">HTML</div>
    <div class="copybox">
      <code>{html}</code>
      <CopyButton text={html} />
    </div>
  </div>
</div>

<style>
  .bb {
    display: flex;
    flex-direction: column;
    gap: 14px;
  }
  .nomargin {
    margin: 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(170px, 1fr));
    gap: 12px;
  }
  fieldset {
    border: none;
    margin: 0;
    padding: 0;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  legend {
    padding: 0;
    margin-bottom: 8px;
  }
  .prev {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .prev-box {
    flex: 1;
    min-height: 48px;
    display: flex;
    align-items: center;
    padding: 10px 14px;
    border-radius: var(--radius-sm);
    border: 1px dashed var(--border-strong);
    background: var(--input);
  }
  .prev-box img {
    max-width: 100%;
  }
  .out {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .out .label {
    margin-top: 4px;
  }
  .out code {
    font-size: 0.8rem;
  }
</style>
