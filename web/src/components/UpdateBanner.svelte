<script lang="ts">
  // Yeni Bekci sürümü şeridi (yalnız yönetici): sürüm, "Neler yeni", "Nasıl
  // güncellenir" (compose komutu + belge) ve bu sürüm için gizle. Saatte bir
  // yeniden sorulur; sunucu denetimi kendi takviminde yapar (bkz. updatecheck.go).
  import { onDestroy, onMount } from 'svelte';
  import { api, type UpdateInfo } from '../lib/api';
  import { i18n, t } from '../lib/i18n';
  import Icon from './Icon.svelte';
  import CopyButton from './CopyButton.svelte';

  const CMD = 'docker compose pull && docker compose up -d';

  let info = $state.raw<UpdateInfo | null>(null);
  let howOpen = $state(false);
  let timer: ReturnType<typeof setInterval> | undefined;

  async function load() {
    try {
      info = await api.updateInfo();
    } catch {
      // sessiz: şerit yalnızca bilgi verir
    }
  }

  async function dismiss() {
    if (!info) return;
    const v = info.latest;
    info = { ...info, show: false };
    try {
      info = await api.updateDismiss(v);
    } catch {
      /* bir sonraki yüklemede yeniden denenir */
    }
  }

  const docsUrl = $derived(
    i18n.locale === 'en' ? 'https://bekci.app/en/docs/updates-backups/#update' : 'https://bekci.app/docs/guncelleme-yedek/#guncelleme',
  );

  onMount(() => {
    load();
    timer = setInterval(load, 3_600_000);
  });
  onDestroy(() => clearInterval(timer));
</script>

{#if info?.show}
  <div class="upd" role="status" data-testid="update-banner" data-version={info.latest}>
    <div class="row">
      <Icon name="download" size={15} />
      <span class="msg">
        <strong>{t('nav.update.available', { latest: info.latest })}</strong>
        <span class="cur">{t('nav.update.installed', { current: info.current })}</span>
      </span>
      <span class="acts">
        {#if info.notes_url}
          <a href={info.notes_url} target="_blank" rel="noopener noreferrer">{t('nav.update.notes')}</a>
        {/if}
        <button type="button" class="linkbtn" aria-expanded={howOpen} onclick={() => (howOpen = !howOpen)} data-testid="update-how">
          {t('nav.update.how')}
        </button>
      </span>
      <button type="button" class="x" aria-label={t('nav.update.dismiss')} title={t('nav.update.dismiss')} onclick={dismiss} data-testid="update-dismiss">
        <Icon name="x" size={15} />
      </button>
    </div>
    {#if howOpen}
      <div class="how">
        <code>{CMD}</code>
        <CopyButton text={CMD} />
        <a href={docsUrl} target="_blank" rel="noopener noreferrer">bekci.app/docs</a>
      </div>
    {/if}
  </div>
{/if}

<style>
  .upd {
    margin: 0 0 16px;
    padding: 6px 6px 6px 14px;
    border: 1px solid var(--border);
    border-radius: 10px;
    background: var(--accent-soft, var(--bg-elev));
    font-size: 0.86rem;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    min-height: 32px;
  }
  .row > :global(svg) {
    flex: none;
    color: var(--accent);
  }
  .msg {
    flex: 1 1 auto;
    min-width: 0;
  }
  .cur {
    color: var(--text-2);
    margin-left: 4px;
  }
  .acts {
    display: inline-flex;
    gap: 14px;
    align-items: center;
  }
  .acts a,
  .linkbtn,
  .how a {
    color: var(--accent);
    font-weight: 600;
  }
  .linkbtn {
    border: 0;
    background: none;
    padding: 0;
    font: inherit;
    cursor: pointer;
  }
  .x {
    display: grid;
    place-items: center;
    width: 36px;
    height: 36px;
    flex: none;
    border: 0;
    border-radius: 8px;
    background: none;
    color: var(--muted);
    cursor: pointer;
  }
  .x:hover {
    color: var(--text);
    background: var(--bg-hover, rgba(127, 127, 127, 0.12));
  }
  .how {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
    padding: 6px 0 4px;
  }
  .how code {
    padding: 4px 8px;
    border-radius: 6px;
    background: var(--bg);
    border: 1px solid var(--border);
    font-size: 0.8rem;
    white-space: nowrap;
    max-width: 100%;
    overflow-x: auto;
  }
  .acts a,
  .linkbtn {
    text-decoration: none;
  }
  .acts a:hover,
  .linkbtn:hover {
    text-decoration: underline;
  }
  @media (max-width: 640px) {
    .acts {
      order: 3;
      flex-basis: 100%;
      padding-left: 25px;
    }
    .x {
      width: 44px;
      height: 44px;
      margin: -6px -2px -6px auto;
    }
  }
</style>
