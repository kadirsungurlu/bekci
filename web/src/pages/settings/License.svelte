<script lang="ts">
  // Ayarlar > Lisans (yalnız yönetici): durum, kimlikler, anahtar, çevrimdışı kira, gönderilen veri.
  // Sözleşme: docs/LISANS-TASARIM.md "API".
  import { onMount } from 'svelte';
  import { ApiError, errorMessage, licenseApi, type LicenseDetail, type LicenseTime } from '../../lib/api';
  import { license } from '../../lib/pro.svelte';
  import { confirmDialog, toast } from '../../lib/ui.svelte';
  import { fmtDate } from '../../lib/format';
  import { t, tOr } from '../../lib/i18n';
  import Icon from '../../components/Icon.svelte';
  import CopyButton from '../../components/CopyButton.svelte';

  const DEFAULT_SENT = ['lisans', 'panel', 'cihaz', 'surum', 'sunucu_sayisi'];

  let d = $state.raw<LicenseDetail | null>(null);
  let loading = $state(true);
  let loadError = $state('');
  let busy = $state<'' | 'key' | 'lease' | 'refresh' | 'remove'>('');
  let keyText = $state('');
  let keyError = $state('');
  let leaseText = $state('');
  let leaseError = $state('');
  let leaseOpen = $state(false);

  let req = 0;
  async function load() {
    const my = ++req;
    try {
      const r = await licenseApi.getLicenseDetail();
      if (my !== req) return;
      d = r;
      loadError = '';
    } catch (e) {
      if (my === req) loadError = errorMessage(e);
    } finally {
      if (my === req) loading = false;
    }
  }
  onMount(load);

  /** Değişiklikten sonra: ayrıntı + genel depo (menü, şerit) yenilenir. */
  async function changed() {
    await load();
    void license.load();
  }

  function errText(e: unknown): string {
    if (e instanceof ApiError && e.code) return tOr(`license.errors.${e.code}`, e.message);
    return errorMessage(e);
  }

  function when(v: LicenseTime | undefined): string {
    if (v === null || v === undefined || v === '' || v === 0) return '';
    if (typeof v === 'number') return fmtDate(v);
    const ms = Date.parse(v);
    return Number.isFinite(ms) ? fmtDate(Math.floor(ms / 1000)) : v;
  }

  async function activate(e: SubmitEvent) {
    e.preventDefault();
    const k = keyText.trim();
    if (!k) {
      keyError = t('license.errors.empty');
      return;
    }
    busy = 'key';
    keyError = '';
    try {
      await licenseApi.putLicense(k);
      keyText = '';
      toast.success(t('license.key.activated'));
      await changed();
    } catch (err) {
      keyError = errText(err);
    } finally {
      busy = '';
    }
  }

  async function uploadLease(e: SubmitEvent) {
    e.preventDefault();
    const k = leaseText.trim();
    if (!k) {
      leaseError = t('license.errors.empty');
      return;
    }
    busy = 'lease';
    leaseError = '';
    try {
      // Anahtar alanı doluysa (ağdan yalıtılmış ilk etkinleştirme) anahtar ve
      // kira birlikte gider; lisans sunucusuna hiç bağlanılmaz.
      const key = keyText.trim();
      if (key) await licenseApi.putLicense(key, k);
      else await licenseApi.postLicenseLease(k);
      leaseText = '';
      keyText = '';
      toast.success(t('license.lease.done'));
      await changed();
    } catch (err) {
      leaseError = errText(err);
    } finally {
      busy = '';
    }
  }

  async function refresh() {
    busy = 'refresh';
    try {
      await licenseApi.refreshLicense();
      toast.success(t('license.key.refreshed'));
    } catch (err) {
      toast.error(errText(err));
    } finally {
      busy = '';
      await changed();
    }
  }

  async function remove() {
    const ok = await confirmDialog({
      title: t('license.key.removeTitle'),
      message: t('license.key.removeText'),
      confirmText: t('license.key.remove'),
      danger: true,
    });
    if (!ok) return;
    busy = 'remove';
    try {
      await licenseApi.deleteLicense();
      toast.success(t('license.key.removed'));
      await changed();
    } catch (err) {
      toast.error(errText(err));
    } finally {
      busy = '';
    }
  }

  const statusLabel = $derived.by(() => {
    if (!d) return '';
    if (!d.pro_derleme) return t('license.status.free');
    if (d.etkin && d.kaynak === 'deneme')
      return d.kalan_gun != null ? t('license.status.trialLeft', { count: Math.max(0, Math.ceil(d.kalan_gun)) }) : t('license.status.trial');
    if (d.etkin) return t('license.status.active');
    return d.kaynak === 'yok' && d.neden !== 'deneme_bitti' ? t('license.status.free') : t('license.status.inactive');
  });
  const tone = $derived(!d ? '' : d.etkin ? (d.kaynak === 'deneme' ? 'accent' : 'up') : d.pro_derleme && d.neden && d.neden !== 'pro_degil' ? 'down' : 'paused');
  const reasonText = $derived(d?.neden && d.neden !== 'pro_degil' ? tOr(`license.reasons.${d.neden}`, d.neden) : '');
  const kindText = $derived(d?.tur ? tOr(`license.kinds.${d.tur}`, d.tur) : '');
  const features = $derived(
    !d?.ozellikler?.length ? '' : d.ozellikler.includes('*') ? t('license.status.allFeatures') : d.ozellikler.join(', '),
  );
  const hasKey = $derived(!!d?.lisans_kimlik);
  const sent = $derived(d?.gonderilen_veri?.length ? d.gonderilen_veri : DEFAULT_SENT);
</script>

{#if loading}
  <div class="center"><span class="spinner"></span></div>
{:else if loadError && !d}
  <div class="alert error">{loadError}</div>
  <button class="btn" onclick={load}>{t('common.retry')}</button>
{:else if d}
  <div class="stack" data-testid="license-page">
    {#if !d.pro_derleme}
      <div class="alert info note" data-testid="license-free-build">
        <Icon name="info" size={16} />
        <span>{t('license.freeBuildNote')}</span>
      </div>
    {/if}

    <div class="grid">
      <section class="card stack">
        <div class="head">
          <h2 class="card-title">{t('license.status.title')}</h2>
          <span class="pill {tone}" data-testid="license-status">{statusLabel}</span>
        </div>
        {#if reasonText}
          <div class="alert warning">{reasonText}</div>
        {/if}
        <dl>
          {#if kindText}<dt>{t('license.status.kind')}</dt><dd>{kindText}</dd>{/if}
          {#if d.musteri}<dt>{t('license.status.customer')}</dt><dd>{d.musteri}</dd>{/if}
          {#if d.lisans_kimlik}<dt>{t('license.status.licenseId')}</dt><dd class="mono">{d.lisans_kimlik}</dd>{/if}
          {#if when(d.bitis)}<dt>{t('license.status.expires')}</dt><dd>{when(d.bitis)}</dd>{/if}
          {#if when(d.kira_bitis)}
            <dt>{t('license.status.leaseExpires')}</dt>
            <dd>{when(d.kira_bitis)}{d.cevrimdisi ? ` (${t('license.status.offlineLease')})` : ''}</dd>
          {/if}
          {#if d.kaynak === 'deneme' && when(d.deneme_bitis)}
            <dt>{t('license.status.trialEnds')}</dt><dd>{when(d.deneme_bitis)}</dd>
          {/if}
          {#if features}<dt>{t('license.status.features')}</dt><dd>{features}</dd>{/if}
          {#if hasKey && d.tur !== 'omur_cevrimdisi' && !d.cevrimdisi}
            <dt>{t('license.status.lastSuccess')}</dt><dd>{when(d.son_basari) || t('license.status.never')}</dd>
            {#if when(d.son_deneme)}<dt>{t('license.status.lastAttempt')}</dt><dd>{when(d.son_deneme)}</dd>{/if}
            {#if d.sunucu_adresi}<dt>{t('license.status.server')}</dt><dd class="mono">{d.sunucu_adresi}</dd>{/if}
          {/if}
          {#if d.son_hata}<dt>{t('license.status.lastError')}</dt><dd class="err">{d.son_hata}</dd>{/if}
          {#if d.derleme}<dt>{t('license.status.build')}</dt><dd class="mono">{d.derleme}</dd>{/if}
        </dl>
        {#if hasKey}
          <div class="acts">
            <button class="btn sm" onclick={refresh} disabled={!!busy} data-testid="license-refresh">
              <Icon name="refresh" size={15} />
              {t('license.key.refresh')}
            </button>
            <button class="btn sm danger" onclick={remove} disabled={!!busy} data-testid="license-remove">
              <Icon name="trash" size={15} />
              {t('license.key.remove')}
            </button>
          </div>
        {/if}
      </section>

      <section class="card stack">
        <h2 class="card-title">{t('license.ids.title')}</h2>
        <div class="idrow">
          <div class="label">{t('license.ids.panel')}</div>
          <div class="copybox"><code data-testid="license-panel-id">{d.panel_id || '—'}</code>{#if d.panel_id}<CopyButton text={d.panel_id} />{/if}</div>
        </div>
        <div class="idrow">
          <div class="label">{t('license.ids.device')}</div>
          {#if d.cihaz_id}
            <div class="copybox"><code data-testid="license-device-id">{d.cihaz_id}</code><CopyButton text={d.cihaz_id} /></div>
          {:else}
            <div class="alert error" data-testid="license-device-error">
              {t('license.ids.deviceNone')}{d.cihaz_hata ? `: ${d.cihaz_hata}` : ''}
            </div>
          {/if}
        </div>
        <p class="help nomargin">{t('license.ids.hint')}</p>
      </section>
    </div>

    <form class="card stack" onsubmit={activate} novalidate>
      <h2 class="card-title">{t('license.key.title')}</h2>
      <div class="field">
        <label for="lic-key">{t('license.key.label')}</label>
        <textarea
          id="lic-key"
          class="input mono"
          rows="4"
          spellcheck="false"
          autocomplete="off"
          placeholder={t('license.key.placeholder')}
          bind:value={keyText}
          aria-invalid={keyError ? 'true' : undefined}
          aria-describedby={keyError ? 'lic-key-err' : undefined}
          data-testid="license-key"
        ></textarea>
      </div>
      {#if keyError}<div class="alert error" id="lic-key-err" role="alert">{keyError}</div>{/if}
      <div>
        <button class="btn primary" type="submit" disabled={!!busy} data-testid="license-activate">
          <Icon name="key" size={16} />
          {t('license.key.activate')}
        </button>
      </div>

    </form>

    <details class="card lease" bind:open={leaseOpen}>
      <summary>{t('license.lease.title')}</summary>
      <form class="stack" onsubmit={uploadLease} novalidate data-testid="license-lease-form">
        <p class="help nomargin">{t('license.lease.hint')}</p>
        <div class="field">
          <label for="lic-lease">{t('license.lease.label')}</label>
          <textarea
            id="lic-lease"
            class="input mono"
            rows="4"
            spellcheck="false"
            autocomplete="off"
            placeholder={t('license.lease.placeholder')}
            bind:value={leaseText}
            aria-invalid={leaseError ? 'true' : undefined}
            data-testid="license-lease"
          ></textarea>
        </div>
        {#if leaseError}<div class="alert error" role="alert">{leaseError}</div>{/if}
        <div>
          <button class="btn" type="submit" disabled={!!busy}>
            <Icon name="upload" size={16} />
            {t('license.lease.submit')}
          </button>
        </div>
      </form>
    </details>

    <section class="card stack sent" data-testid="license-sent">
      <h2 class="card-title"><Icon name="shield" size={18} /> {t('license.sent.title')}</h2>
      <p class="nomargin">{t('license.sent.intro')}</p>
      <ul>
        {#each sent as k (k)}<li>{tOr(`license.sent.${k}`, k)}</li>{/each}
      </ul>
      <p class="nomargin"><strong>{t('license.sent.never')}</strong></p>
      <p class="help nomargin">{t('license.sent.schedule')} {t('license.sent.offline')}</p>
    </section>
  </div>
{/if}

<style>
  .center {
    display: flex;
    justify-content: center;
    padding: 48px 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 16px;
    align-items: start;
  }
  .note {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .head .card-title {
    margin: 0;
  }
  dl {
    display: grid;
    grid-template-columns: max-content 1fr;
    gap: 6px 16px;
    margin: 0;
    font-size: 0.9rem;
  }
  dt {
    color: var(--text-2);
  }
  dd {
    margin: 0;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  dd.err {
    color: var(--down-text-2, var(--down));
  }
  .acts {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
  .idrow {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .copybox code {
    overflow-wrap: anywhere;
  }
  textarea {
    resize: vertical;
    font-size: 0.82rem;
    word-break: break-all;
  }
  .lease summary {
    cursor: pointer;
    min-height: 28px;
    font-weight: 600;
    color: var(--text-2);
  }
  .lease form {
    margin-top: 12px;
  }
  .sent .card-title {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .sent :global(svg) {
    color: var(--accent-text);
  }
  .sent ul {
    margin: 0;
    padding-left: 20px;
  }
  @media (max-width: 640px) {
    dl {
      grid-template-columns: 1fr;
      gap: 2px;
    }
    dd {
      margin-bottom: 6px;
    }
  }
</style>
