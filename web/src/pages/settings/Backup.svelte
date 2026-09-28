<script lang="ts">
  import { onMount, tick } from 'svelte';
  import { api, ApiError, errorMessage, type ImportSummary } from '../../lib/api';
  import { live } from '../../lib/live.svelte';
  import { router } from '../../lib/router.svelte';
  import { confirmDialog, toast } from '../../lib/ui.svelte';
  import { fmtPctInt, fmtSize } from '../../lib/format';
  import { t, tParts } from '../../lib/i18n';
  import FilePick from '../../components/FilePick.svelte';
  import ImportResult from '../../components/ImportResult.svelte';
  import Icon from '../../components/Icon.svelte';

  const MB = 1024 * 1024;

  // Aynı anda tek içe aktarma (sunucu da 409 ile reddeder).
  let busy = $state<'' | 'restore' | 'kuma' | 'ur'>('');

  /** Yükleme ilerlemesi: yüzde; 100'e ulaşınca sunucunun işlemesi beklenir. */
  interface Progress {
    loaded: number;
    total: number;
  }

  // Geri yükle ----------------------------------------------------------------------
  let rFile = $state<File | null>(null);
  let rMode = $state<'merge' | 'replace'>('merge');
  let rResult = $state.raw<ImportSummary | null>(null);
  let rError = $state('');
  let rProg = $state<Progress | null>(null);

  // Dosya veya mod değişince eski önizleme geçersiz.
  $effect(() => {
    void rFile;
    void rMode;
    rResult = null;
    rError = '';
  });

  function tooBig(f: File, limitMB: number, what: string): string {
    return f.size > limitMB * MB ? t('backup.tooBig', { what, limit: limitMB, size: fmtSize(f.size) }) : '';
  }

  async function restore(dry: boolean) {
    if (!rFile) return (rError = t('backup.restore.noFile'));
    const big = tooBig(rFile, 20, t('backup.backupFile'));
    if (big) return (rError = big);
    rError = '';
    const file = rFile;
    const mode = rMode;
    busy = 'restore';
    try {
      if (!dry && mode === 'replace') {
        // Silinecek kayıtların sayısı için önce önizleme (yoksa) alınır.
        let preview = rResult?.dry_run ? rResult : null;
        if (!preview) {
          rProg = { loaded: 0, total: file.size };
          preview = await api.importBackup(file, mode, true, (loaded, total) => (rProg = { loaded, total }));
          rResult = preview;
        }
        rProg = null;
        const d = preview.deleted ?? { monitors: 0, notifications: 0, tags: 0, status_pages: 0 };
        const ok = await confirmDialog({
          title: t('backup.restore.confirmTitle'),
          message: t('backup.restore.confirmMsg', {
            monitors: d.monitors,
            notifications: d.notifications,
            tags: d.tags,
            pages: d.status_pages,
          }),
          confirmText: t('backup.restore.confirmBtn'),
          danger: true,
        });
        if (!ok) return;
      }
      rProg = { loaded: 0, total: file.size };
      const res = await api.importBackup(file, mode, dry, (loaded, total) => (rProg = { loaded, total }));
      rResult = res;
      if (!dry) done(res, t('backup.restore.done'));
    } catch (e) {
      rError = errorMessage(e);
    } finally {
      busy = '';
      rProg = null;
    }
  }

  // Uptime Kuma --------------------------------------------------------------------------
  let kFile = $state<File | null>(null);
  let kResult = $state.raw<ImportSummary | null>(null);
  let kError = $state('');
  let kProg = $state<Progress | null>(null);

  $effect(() => {
    void kFile;
    kResult = null;
    kError = '';
  });

  async function kuma(dry: boolean) {
    if (!kFile) return (kError = t('backup.kuma.noFile'));
    const big = tooBig(kFile, 200, t('backup.file'));
    if (big) return (kError = big);
    kError = '';
    busy = 'kuma';
    kProg = { loaded: 0, total: kFile.size };
    try {
      const res = await api.importKuma(kFile, dry, (loaded, total) => (kProg = { loaded, total }));
      kResult = res;
      if (!dry) done(res, t('backup.kuma.done'));
    } catch (e) {
      kError = errorMessage(e);
    } finally {
      busy = '';
      kProg = null;
    }
  }

  // UptimeRobot ------------------------------------------------------------------------------
  let urKey = $state('');
  let urResult = $state.raw<ImportSummary | null>(null);
  let urError = $state('');

  $effect(() => {
    void urKey;
    urResult = null;
    urError = '';
  });

  async function uptimeRobot(dry: boolean) {
    const key = urKey.trim();
    if (!key) return (urError = t('backup.ur.noKey'));
    if (/\s/.test(key)) return (urError = t('backup.ur.spaces'));
    urError = '';
    busy = 'ur';
    try {
      const res = await api.importUptimeRobot(key, dry);
      urResult = res;
      if (!dry) done(res, t('backup.ur.done'));
    } catch (e) {
      urError =
        e instanceof ApiError && e.status === 502
          ? t('backup.ur.connectFailed', { error: e.message })
          : errorMessage(e);
    } finally {
      busy = '';
    }
  }

  function done(res: ImportSummary, msg: string) {
    const n = res.created.monitors;
    toast.success(n > 0 ? t('backup.addedMonitors', { msg, count: n }) : msg);
    live.refresh();
  }

  const pct = (p: Progress) => (p.total > 0 ? Math.min(100, Math.round((p.loaded / p.total) * 100)) : 0);

  // "Taşıyın" bağlantısıyla gelindiyse taşıma bölümüne kaydır.
  let migrateEl: HTMLElement | undefined = $state();
  onMount(async () => {
    if (/[?&]tasi\b/.test(router.path)) {
      await tick();
      migrateEl?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }
  });
</script>

{#snippet progress(p: Progress, what: string)}
  {@const v = pct(p)}
  <div class="prog" role="status">
    <div class="prog-t small">
      <span class="spinner sm-spin"></span>
      {#if v < 100}
        {t('backup.uploading', { what, pct: fmtPctInt(v) })} <span class="muted">({fmtSize(p.loaded)} / {fmtSize(p.total)})</span>
      {:else}
        {t('backup.processing')}
      {/if}
    </div>
    <div class="bar" aria-hidden="true"><span style="width:{v}%"></span></div>
  </div>
{/snippet}

<div class="stack">
  <section class="card">
    <div class="sec-head">
      <span class="sic"><Icon name="download" size={18} /></span>
      <div>
        <h2 class="card-title">{t('backup.download.title')}</h2>
        <p class="text-2 small sub">{t('backup.download.text')}</p>
      </div>
    </div>
    <a class="btn primary" href="/api/export" download><Icon name="download" size={16} /> {t('backup.download.title')}</a>
  </section>

  <section class="card">
    <div class="sec-head">
      <span class="sic"><Icon name="upload" size={18} /></span>
      <div>
        <h2 class="card-title">{t('backup.restore.title')}</h2>
        <p class="text-2 small sub">{t('backup.restore.text')}</p>
      </div>
    </div>
    <div class="stack">
      <FilePick bind:file={rFile} accept=".json,application/json" id="rs-file" label={t('backup.restore.pick')} disabled={busy !== ''} />
      <div class="modes" role="radiogroup" aria-label={t('backup.restore.modeLabel')}>
        <label class="mode" class:on={rMode === 'merge'}>
          <input type="radio" name="rmode" value="merge" bind:group={rMode} />
          <span><b>{t('backup.restore.merge')}</b><small>{t('backup.restore.mergeHelp')}</small></span>
        </label>
        <label class="mode danger" class:on={rMode === 'replace'}>
          <input type="radio" name="rmode" value="replace" bind:group={rMode} />
          <span>
            <b>{t('backup.restore.replace')}</b>
            <small>{t('backup.restore.replaceHelp')}</small>
          </span>
        </label>
      </div>
      {#if rProg}{@render progress(rProg, t('backup.backupShort'))}{/if}
      {#if rError}<div class="alert error" role="alert">{rError}</div>{/if}
      <div class="acts">
        <button class="btn" onclick={() => restore(true)} disabled={busy !== '' || !rFile}>
          {#if busy === 'restore' && !rResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} {t('backup.preview')}
        </button>
        <button class="btn {rMode === 'replace' ? 'danger solid' : 'primary'}" onclick={() => restore(false)} disabled={busy !== '' || !rFile}>
          {#if busy === 'restore' && rResult}<span class="spinner"></span>{/if}
          {t('backup.restore.title')}
        </button>
      </div>
      {#if rResult}<ImportResult result={rResult} />{/if}
    </div>
  </section>

  <div class="migrate" bind:this={migrateEl}>
    <h2 class="mig-title">{t('backup.migrate.title')}<span class="dot">.</span></h2>
    <p class="text-2 small mig-sub">{t('backup.migrate.text')}</p>
  </div>

  <div class="mig-grid">
    <section class="card">
      <div class="sec-head">
        <span class="sic brand"><Icon name="log-in" size={18} /></span>
        <div>
          <h2 class="card-title">{t('backup.ur.title')}</h2>
          <p class="text-2 small sub">
            {#each tParts('backup.ur.text') as p, i (i)}
              {#if p.slot === 'path'}<b>Integrations &amp; API → API</b>{:else}{p.text}{/if}
            {/each}
          </p>
        </div>
      </div>
      <div class="stack">
        <div class="field">
          <label for="ur-key">{t('backup.ur.key')}</label>
          <input
            id="ur-key"
            class="input mono"
            type="password"
            autocomplete="off"
            spellcheck="false"
            bind:value={urKey}
            placeholder="ur1234567-…"
            disabled={busy !== ''}
          />
        </div>
        {#if urError}<div class="alert error" role="alert">{urError}</div>{/if}
        <div class="acts">
          <button class="btn" onclick={() => uptimeRobot(true)} disabled={busy !== '' || !urKey.trim()}>
            {#if busy === 'ur' && !urResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} {t('backup.preview')}
          </button>
          <button class="btn primary" onclick={() => uptimeRobot(false)} disabled={busy !== '' || !urResult?.dry_run}>
            {#if busy === 'ur' && urResult}<span class="spinner"></span>{/if} {t('backup.import')}
          </button>
        </div>
        {#if !urResult}<p class="help nomargin">{t('backup.previewFirst')}</p>{/if}
        {#if urResult}<ImportResult result={urResult} />{/if}
      </div>
    </section>

    <section class="card">
      <div class="sec-head">
        <span class="sic brand"><Icon name="archive" size={18} /></span>
        <div>
          <h2 class="card-title">{t('backup.kuma.title')}</h2>
          <p class="text-2 small sub">
            {#each tParts('backup.kuma.text') as p, i (i)}
              {#if p.slot === 'path'}<b>{t('backup.kuma.path')}</b>{:else if p.slot === 'db'}<code>kuma.db</code>{:else}{p.text}{/if}
            {/each}
          </p>
        </div>
      </div>
      <div class="stack">
        <FilePick
          bind:file={kFile}
          accept=".json,.db,.sqlite,application/json,application/vnd.sqlite3,application/x-sqlite3"
          id="kuma-file"
          label={t('backup.kuma.pick')}
          disabled={busy !== ''}
        />
        {#if kProg}{@render progress(kProg, t('backup.file'))}{/if}
        {#if kError}<div class="alert error" role="alert">{kError}</div>{/if}
        <div class="acts">
          <button class="btn" onclick={() => kuma(true)} disabled={busy !== '' || !kFile}>
            {#if busy === 'kuma' && !kResult}<span class="spinner"></span>{:else}<Icon name="eye" size={15} />{/if} {t('backup.preview')}
          </button>
          <button class="btn primary" onclick={() => kuma(false)} disabled={busy !== '' || !kResult?.dry_run}>
            {#if busy === 'kuma' && kResult}<span class="spinner"></span>{/if} {t('backup.import')}
          </button>
        </div>
        {#if !kResult}<p class="help nomargin">{t('backup.previewFirst')}</p>{/if}
        {#if kResult}<ImportResult result={kResult} />{/if}
      </div>
    </section>
  </div>
</div>

<style>
  .sec-head {
    display: flex;
    align-items: flex-start;
    gap: 14px;
    margin-bottom: 16px;
  }
  .sec-head > div {
    min-width: 0;
    flex: 1;
  }
  .sic {
    width: 36px;
    height: 36px;
    border-radius: 10px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: var(--accent-soft);
    color: var(--accent-text);
    flex-shrink: 0;
  }
  .card-title {
    margin-bottom: 4px;
  }
  .sub {
    margin: 0;
    max-width: 780px;
  }
  .modes {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 10px;
  }
  .mode {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: var(--input);
    cursor: pointer;
    font-size: 0.9rem;
  }
  .mode.on {
    border-color: var(--accent);
    box-shadow: 0 0 0 1px var(--accent) inset;
  }
  .mode.danger.on {
    border-color: var(--down);
    box-shadow: 0 0 0 1px var(--down) inset;
  }
  .mode input {
    appearance: none;
    -webkit-appearance: none;
    width: 18px;
    height: 18px;
    margin: 1px 0 0;
    border-radius: 50%;
    border: 1.5px solid var(--border-hover);
    background: var(--input);
    flex-shrink: 0;
    cursor: pointer;
  }
  .mode input:checked {
    border: 5px solid var(--accent);
  }
  .mode.danger input:checked {
    border-color: var(--down);
  }
  .mode input:focus-visible {
    outline: 2px solid var(--accent);
    outline-offset: 2px;
  }
  .mode small {
    display: block;
    color: var(--muted);
    font-size: 0.8rem;
    line-height: 1.4;
    margin-top: 2px;
  }
  .acts {
    display: flex;
    gap: 10px;
    flex-wrap: wrap;
  }
  .nomargin {
    margin: 0;
  }
  .prog {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .prog-t {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--text-2);
  }
  .sm-spin {
    width: 14px;
    height: 14px;
  }
  .bar {
    height: 6px;
    border-radius: 999px;
    background: var(--progress-track);
    border: 1px solid var(--border);
    overflow: hidden;
  }
  .bar span {
    display: block;
    height: 100%;
    background: var(--accent);
    transition: width 0.2s;
  }
  .migrate {
    margin-top: 10px;
    scroll-margin-top: 70px;
  }
  .mig-title {
    font-size: 1.15rem;
  }
  .mig-sub {
    margin: 4px 0 0;
  }
  .mig-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px;
    align-items: start;
  }
  @media (max-width: 1100px) {
    .mig-grid {
      grid-template-columns: minmax(0, 1fr);
    }
  }
  @media (max-width: 640px) {
    .modes {
      grid-template-columns: minmax(0, 1fr);
    }
    .sec-head {
      gap: 12px;
    }
    .acts > :global(*) {
      flex: 1;
    }
  }
</style>
